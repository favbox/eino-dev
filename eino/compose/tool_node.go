package compose

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/components"
	"github.com/favbox/eino/components/tool"
	"github.com/favbox/eino/internal/safe"
	"github.com/favbox/eino/schema"
)

/*
 * tool_node.go - 工具节点实现
 *
 * 核心组件：
 *   - ToolsNode: 工具执行节点，支持 InvokableTool 和 StreamableTool
 *   - ToolsNodeConfig: 工具节点配置，包含工具列表和处理器
 *   - toolsTuple: 工具元数据封装（索引、执行器、可运行包）
 *   - toolCallTask: 单个工具调用任务
 *   - ToolsInterruptAndRerunExtra: 中断重运行扩展信息
 *
 * 设计特点：
 *   - 双模式支持：Invoke（同步）和 Stream（流式）
 *   - 执行控制：支持顺序和并行执行
 *   - 鲁棒性：未知工具处理器优雅降级
 *   - 参数处理：工具参数预处理器
 *   - 容错机制：Panic 恢复和错误处理
 *
 * 与其他文件关系：
 *   - 为图编排提供工具执行能力
 *   - 集成 callbacks 系统支持回调
 *   - 与 interrupt.go 协作支持中断重运行
 *   - 封装 components/tool 的工具组件
 *
 * 使用场景：
 *   - 工具调用执行：LLM 触发的函数调用
 *   - 并发工具执行：多个工具同时运行提高效率
 *   - 未知工具处理：优雅处理 LLM 幻觉的工具调用
 *   - 参数预处理：动态修改工具参数
 */

// ====== 工具节点配置选项 ======

type toolsNodeOptions struct {
	ToolOptions   []tool.Option
	ToolList      []tool.BaseTool
	executedTools map[string]string
}

// ToolsNodeOption 工具节点选项函数类型
type ToolsNodeOption func(o *toolsNodeOptions)

// WithToolOption 添加工具选项
func WithToolOption(opts ...tool.Option) ToolsNodeOption {
	return func(o *toolsNodeOptions) {
		o.ToolOptions = append(o.ToolOptions, opts...)
	}
}

// WithToolList 设置工具列表
func WithToolList(tool ...tool.BaseTool) ToolsNodeOption {
	return func(o *toolsNodeOptions) {
		o.ToolList = tool
	}
}

// withExecutedTools 设置已执行工具映射（内部使用）
func withExecutedTools(executedTools map[string]string) ToolsNodeOption {
	return func(o *toolsNodeOptions) {
		o.executedTools = executedTools
	}
}

// ====== 工具节点核心定义 ======

// ToolsNode 工具执行节点 - 在图中执行工具调用的节点
// 图节点接口定义：
//
//	Invoke(ctx context.Context, input *schema.Message, opts ...ToolsNodeOption) ([]*schema.Message, error)
//	Stream(ctx context.Context, input *schema.Message, opts ...ToolsNodeOption) (*schema.StreamReader[[]*schema.Message], error)
//
// 输入：包含 ToolCalls 的 AssistantMessage
// 输出：ToolMessage 数组，顺序对应输入中的 ToolCalls 顺序
type ToolsNode struct {
	tuple                *toolsTuple
	unknownToolHandler   func(ctx context.Context, name, input string) (string, error)
	executeSequentially  bool
	toolArgumentsHandler func(ctx context.Context, name, input string) (string, error)
}

// ToolsNodeConfig 工具节点配置 - 定义工具节点的行为和特性
type ToolsNodeConfig struct {
	// Tools 工具列表 - 可调用的 BaseTool 列表（需实现 InvokableTool 或 StreamableTool）
	Tools []tool.BaseTool

	// UnknownToolsHandler 未知工具处理器 - 处理 LLM 幻觉的工具调用
	// 可选字段：未设置时调用不存在工具将返回错误
	// 设置后，当 LLM 调用 Tools 列表中不存在的工具时，将调用此处理器而非返回错误
	// 实现优雅降级，处理 LLM 的幻觉问题
	UnknownToolsHandler func(ctx context.Context, name, input string) (string, error)

	// ExecuteSequentially 执行顺序控制 - 顺序执行或并行执行工具调用
	// true：按输入消息中的顺序逐个执行
	// false（默认）：并行执行所有工具调用
	ExecuteSequentially bool

	// ToolArgumentsHandler 工具参数处理器 - 执行前预处理工具参数
	// 为每个工具调用提供参数处理和转换能力
	ToolArgumentsHandler func(ctx context.Context, name, arguments string) (string, error)
}

// NewToolNode 创建工具节点实例 - 根据配置创建工具执行节点
// 示例：支持混合同步和流式工具
func NewToolNode(ctx context.Context, conf *ToolsNodeConfig) (*ToolsNode, error) {
	tuple, err := convTools(ctx, conf.Tools)
	if err != nil {
		return nil, err
	}

	return &ToolsNode{
		tuple:                tuple,
		unknownToolHandler:   conf.UnknownToolsHandler,
		executeSequentially:  conf.ExecuteSequentially,
		toolArgumentsHandler: conf.ToolArgumentsHandler,
	}, nil
}

// ====== 中断重运行扩展信息 ======

// ToolsInterruptAndRerunExtra 工具节点中断重运行扩展信息
// 存储工具调用的执行状态和重运行所需数据
type ToolsInterruptAndRerunExtra struct {
	ToolCalls     []schema.ToolCall
	ExecutedTools map[string]string
	RerunTools    []string
	RerunExtraMap map[string]any
}

// 注册中断重运行扩展类型
func init() {
	schema.RegisterName[*ToolsInterruptAndRerunExtra]("_eino_compose_tools_interrupt_and_rerun_extra") // TODO: check if this is really needed when refactoring adk resume
}

// ====== 工具元数据封装 ======

// toolsTuple 工具元数据封装 - 统一管理工具的索引、执行器和元数据
type toolsTuple struct {
	indexes map[string]int
	meta    []*executorMeta
	rps     []*runnablePacker[string, string, tool.Option]
}

// convTools 转换工具列表 - 将 BaseTool 列表转换为内部可执行的元数据结构
// 支持同时处理 InvokableTool 和 StreamableTool 两种类型
func convTools(ctx context.Context, tools []tool.BaseTool) (*toolsTuple, error) {
	ret := &toolsTuple{
		indexes: make(map[string]int),
		meta:    make([]*executorMeta, len(tools)),
		rps:     make([]*runnablePacker[string, string, tool.Option], len(tools)),
	}
	for idx, bt := range tools {
		tl, err := bt.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("(NewToolNode) failed to get tool info at idx= %d: %w", idx, err)
		}

		toolName := tl.Name
		var (
			st tool.StreamableTool
			it tool.InvokableTool

			invokable  func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error)
			streamable func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error)

			ok   bool
			meta *executorMeta
		)

		// 检查流式工具接口
		if st, ok = bt.(tool.StreamableTool); ok {
			streamable = st.StreamableRun
		}

		// 检查同步工具接口
		if it, ok = bt.(tool.InvokableTool); ok {
			invokable = it.InvokableRun
		}

		// 工具必须实现至少一种执行接口
		if st == nil && it == nil {
			return nil, fmt.Errorf("tool %s is not invokable or streamable", toolName)
		}

		// 提取执行器元数据（用于回调和组件信息）
		if st != nil {
			meta = parseExecutorInfoFromComponent(components.ComponentOfTool, st)
		} else {
			meta = parseExecutorInfoFromComponent(components.ComponentOfTool, it)
		}

		// 构建工具索引和执行器包装
		ret.indexes[toolName] = idx
		ret.meta[idx] = meta
		ret.rps[idx] = newRunnablePacker(invokable, streamable,
			nil, nil, !meta.isComponentCallbackEnabled)
	}
	return ret, nil
}

// ====== 工具调用任务定义 ======

// toolCallTask 工具调用任务 - 封装单个工具调用的输入输出
type toolCallTask struct {
	// 输入参数
	r      *runnablePacker[string, string, tool.Option]
	meta   *executorMeta
	name   string
	arg    string
	callID string

	// 输出结果
	executed bool
	output   string
	sOutput  *schema.StreamReader[string]
	err      error
}

// ====== 工具调用任务生成 ======

// genToolCallTasks 生成工具调用任务 - 解析输入消息并创建执行任务列表
// 处理已执行工具的重用、未知工具的优雅处理和参数预处理
func (tn *ToolsNode) genToolCallTasks(ctx context.Context, tuple *toolsTuple,
	input *schema.Message, executedTools map[string]string, isStream bool) ([]toolCallTask, error) {

	// 验证输入消息角色：必须是 Assistant 消息
	if input.Role != schema.Assistant {
		return nil, fmt.Errorf("expected message role is Assistant, got %s", input.Role)
	}

	// 验证工具调用数量：至少包含一个 ToolCall
	n := len(input.ToolCalls)
	if n == 0 {
		return nil, errors.New("no tool call found in input message")
	}

	// 初始化任务数组
	toolCallTasks := make([]toolCallTask, n)

	// 逐个处理 ToolCall
	for i := 0; i < n; i++ {
		toolCall := input.ToolCalls[i]

		// 重用已执行的工具调用：直接从缓存返回结果
		if result, executed := executedTools[toolCall.ID]; executed {
			toolCallTasks[i].name = toolCall.Function.Name
			toolCallTasks[i].arg = toolCall.Function.Arguments
			toolCallTasks[i].callID = toolCall.ID
			toolCallTasks[i].executed = true
			if isStream {
				// 流式模式：包装为流读取器
				toolCallTasks[i].sOutput = schema.StreamReaderFromArray([]string{result})
			} else {
				// 同步模式：直接赋值输出
				toolCallTasks[i].output = result
			}
			continue
		}

		// 查找工具索引：验证工具是否存在
		index, ok := tuple.indexes[toolCall.Function.Name]
		if !ok {
			// 工具不存在：使用未知工具处理器或返回错误
			if tn.unknownToolHandler == nil {
				return nil, fmt.Errorf("tool %s not found in toolsNode indexes", toolCall.Function.Name)
			}
			// 创建未知工具任务：优雅处理 LLM 幻觉
			toolCallTasks[i] = newUnknownToolTask(toolCall.Function.Name, toolCall.Function.Arguments, toolCall.ID, tn.unknownToolHandler)
		} else {
			// 正常工具调用：绑定执行器和元数据
			toolCallTasks[i].r = tuple.rps[index]
			toolCallTasks[i].meta = tuple.meta[index]
			toolCallTasks[i].name = toolCall.Function.Name
			toolCallTasks[i].callID = toolCall.ID

			// 参数预处理：允许动态修改工具参数
			if tn.toolArgumentsHandler != nil {
				arg, err := tn.toolArgumentsHandler(ctx, toolCall.Function.Name, toolCall.Function.Arguments)
				if err != nil {
					return nil, fmt.Errorf("failed to executed tool[name:%s arguments:%s] arguments handler: %w", toolCall.Function.Name, toolCall.Function.Arguments, err)
				}
				toolCallTasks[i].arg = arg
			} else {
				toolCallTasks[i].arg = toolCall.Function.Arguments
			}
		}
	}

	return toolCallTasks, nil
}

// newUnknownToolTask 创建未知工具任务 - 为未知工具调用创建特殊处理任务
// 用于优雅处理 LLM 幻觉的工具调用
func newUnknownToolTask(name, arg, callID string, unknownToolHandler func(ctx context.Context, name, input string) (string, error)) toolCallTask {
	return toolCallTask{
		// 包装未知工具处理器为可执行器
		r: newRunnablePacker(func(ctx context.Context, input string, opts ...tool.Option) (output string, err error) {
			return unknownToolHandler(ctx, name, input)
		}, nil, nil, nil, false),
		meta: &executorMeta{
			component:                  components.ComponentOfTool,
			isComponentCallbackEnabled: false,
			componentImplType:          "UnknownTool",
		},
		name:   name,
		arg:    arg,
		callID: callID,
	}
}

// ====== 工具调用执行器 ======

// runToolCallTaskByInvoke 同步执行工具调用任务
func runToolCallTaskByInvoke(ctx context.Context, task *toolCallTask, opts ...tool.Option) {
	if task.executed {
		return
	}
	// 设置回调上下文：集成 callbacks 系统
	ctx = callbacks.ReuseHandlers(ctx, &callbacks.RunInfo{
		Name:      task.name,
		Type:      task.meta.componentImplType,
		Component: task.meta.component,
	})

	// 设置工具调用上下文信息：用于获取当前调用 ID
	ctx = setToolCallInfo(ctx, &toolCallInfo{toolCallID: task.callID})
	task.output, task.err = task.r.Invoke(ctx, task.arg, opts...)
	if task.err == nil {
		task.executed = true
	}
}

// runToolCallTaskByStream 流式执行工具调用任务
func runToolCallTaskByStream(ctx context.Context, task *toolCallTask, opts ...tool.Option) {
	// 设置回调上下文
	ctx = callbacks.ReuseHandlers(ctx, &callbacks.RunInfo{
		Name:      task.name,
		Type:      task.meta.componentImplType,
		Component: task.meta.component,
	})

	// 设置工具调用上下文信息
	ctx = setToolCallInfo(ctx, &toolCallInfo{toolCallID: task.callID})
	task.sOutput, task.err = task.r.Stream(ctx, task.arg, opts...)
	if task.err == nil {
		task.executed = true
	}
}

// ====== 执行模式控制 ======

// sequentialRunToolCall 顺序执行工具调用 - 按顺序逐个执行
func sequentialRunToolCall(ctx context.Context,
	run func(ctx2 context.Context, callTask *toolCallTask, opts ...tool.Option),
	tasks []toolCallTask, opts ...tool.Option) {

	for i := range tasks {
		// 跳过已执行的任务
		if tasks[i].executed {
			continue
		}
		run(ctx, &tasks[i], opts...)
	}
}

// parallelRunToolCall 并行执行工具调用 - 多个任务同时执行
// 使用 WaitGroup 同步，并内置 Panic 恢复机制
func parallelRunToolCall(ctx context.Context,
	run func(ctx2 context.Context, callTask *toolCallTask, opts ...tool.Option),
	tasks []toolCallTask, opts ...tool.Option) {

	// 单一任务：无需并行，直接执行
	if len(tasks) == 1 {
		run(ctx, &tasks[0], opts...)
		return
	}

	var wg sync.WaitGroup
	// 并行执行除第一个外的其他任务
	for i := 1; i < len(tasks); i++ {
		if tasks[i].executed {
			continue
		}
		wg.Add(1)
		go func(ctx_ context.Context, t *toolCallTask, opts ...tool.Option) {
			defer wg.Done()
			// Panic 恢复：防止单个任务 Panic 影响整个执行
			defer func() {
				panicErr := recover()
				if panicErr != nil {
					t.err = safe.NewPanicErr(panicErr, debug.Stack())
				}
			}()
			run(ctx_, t, opts...)
		}(ctx, &tasks[i], opts...)
	}

	// 执行第一个任务（同步等待其他任务完成）
	run(ctx, &tasks[0], opts...)
	wg.Wait()
}

// ====== 工具节点执行接口 ======

// Invoke 同步执行工具调用 - 收集并返回所有工具调用的结果
// 多工具调用时默认并行执行（除非配置为顺序执行）
func (tn *ToolsNode) Invoke(ctx context.Context, input *schema.Message,
	opts ...ToolsNodeOption) ([]*schema.Message, error) {

	// 解析执行选项
	opt := getToolsNodeOptions(opts...)
	tuple := tn.tuple

	// 支持动态工具列表：运行时替换工具列表
	if opt.ToolList != nil {
		var err error
		tuple, err = convTools(ctx, opt.ToolList)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool list from call option: %w", err)
		}
	}

	// 生成工具调用任务
	tasks, err := tn.genToolCallTasks(ctx, tuple, input, opt.executedTools, false)
	if err != nil {
		return nil, err
	}

	// 执行工具调用：根据配置选择顺序或并行执行
	if tn.executeSequentially {
		sequentialRunToolCall(ctx, runToolCallTaskByInvoke, tasks, opt.ToolOptions...)
	} else {
		parallelRunToolCall(ctx, runToolCallTaskByInvoke, tasks, opt.ToolOptions...)
	}

	n := len(tasks)
	output := make([]*schema.Message, n)

	// 处理执行结果和错误
	rerunExtra := &ToolsInterruptAndRerunExtra{
		ToolCalls:     input.ToolCalls,
		ExecutedTools: make(map[string]string),
		RerunExtraMap: make(map[string]any),
	}
	rerun := false
	for i := 0; i < n; i++ {
		if tasks[i].err != nil {
			// 检查是否是中断重运行错误
			extra, ok := IsInterruptRerunError(tasks[i].err)
			if !ok {
				return nil, fmt.Errorf("failed to invoke tool[name:%s id:%s]: %w", tasks[i].name, tasks[i].callID, tasks[i].err)
			}
			rerun = true
			rerunExtra.RerunTools = append(rerunExtra.RerunTools, tasks[i].callID)
			rerunExtra.RerunExtraMap[tasks[i].callID] = extra
			continue
		}
		if tasks[i].executed {
			rerunExtra.ExecutedTools[tasks[i].callID] = tasks[i].output
		}
		// 正常执行：构造 ToolMessage 输出
		if !rerun {
			output[i] = schema.ToolMessage(tasks[i].output, tasks[i].callID, schema.WithToolName(tasks[i].name))
		}
	}

	// 中断重运行处理
	if rerun {
		return nil, NewInterruptAndRerunErr(rerunExtra)
	}

	return output, nil
}

// Stream 流式执行工具调用 - 收集并返回流式工具调用的结果
// 多工具调用时默认并行执行，支持流式数据实时输出
func (tn *ToolsNode) Stream(ctx context.Context, input *schema.Message,
	opts ...ToolsNodeOption) (*schema.StreamReader[[]*schema.Message], error) {

	// 解析执行选项
	opt := getToolsNodeOptions(opts...)
	tuple := tn.tuple

	// 支持动态工具列表
	if opt.ToolList != nil {
		var err error
		tuple, err = convTools(ctx, opt.ToolList)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool list from call option: %w", err)
		}
	}

	// 生成工具调用任务（流式模式）
	tasks, err := tn.genToolCallTasks(ctx, tuple, input, opt.executedTools, true)
	if err != nil {
		return nil, err
	}

	// 执行工具调用（流式）
	if tn.executeSequentially {
		sequentialRunToolCall(ctx, runToolCallTaskByStream, tasks, opt.ToolOptions...)
	} else {
		parallelRunToolCall(ctx, runToolCallTaskByStream, tasks, opt.ToolOptions...)
	}

	n := len(tasks)

	// 检查中断重运行错误
	rerun := false
	rerunExtra := &ToolsInterruptAndRerunExtra{
		ToolCalls:     input.ToolCalls,
		RerunExtraMap: make(map[string]any),
		ExecutedTools: make(map[string]string),
	}

	for i := 0; i < n; i++ {
		if tasks[i].err != nil {
			extra, ok := IsInterruptRerunError(tasks[i].err)
			if !ok {
				return nil, fmt.Errorf("failed to stream tool call %s: %w", tasks[i].callID, tasks[i].err)
			}
			rerun = true
			rerunExtra.RerunTools = append(rerunExtra.RerunTools, tasks[i].callID)
			rerunExtra.RerunExtraMap[tasks[i].callID] = extra
			continue
		}
	}

	// 中断重运行：合并并保存工具输出
	if rerun {
		for _, t := range tasks {
			if t.executed {
				o, err_ := concatStreamReader(t.sOutput)
				if err_ != nil {
					return nil, fmt.Errorf("failed to concat tool[name:%s id:%s]'s stream output: %w", t.name, t.callID, err_)
				}
				rerunExtra.ExecutedTools[t.callID] = o
			}
		}
		return nil, NewInterruptAndRerunErr(rerunExtra)
	}

	// 正常返回：构造流式输出
	sOutput := make([]*schema.StreamReader[[]*schema.Message], n)
	for i := 0; i < n; i++ {
		index := i
		callID := tasks[i].callID
		callName := tasks[i].name
		// 转换函数：将字符串输出转换为 ToolMessage 数组
		cvt := func(s string) ([]*schema.Message, error) {
			ret := make([]*schema.Message, n)
			ret[index] = schema.ToolMessage(s, callID, schema.WithToolName(callName))
			return ret, nil
		}

		sOutput[i] = schema.StreamReaderWithConvert(tasks[i].sOutput, cvt)
	}
	// 合并多个流读取器为一个
	return schema.MergeStreamReaders(sOutput), nil
}

// ====== 辅助接口方法 ======

func (tn *ToolsNode) GetType() string {
	return ""
}

// getToolsNodeOptions 解析工具节点选项
func getToolsNodeOptions(opts ...ToolsNodeOption) *toolsNodeOptions {
	o := &toolsNodeOptions{
		ToolOptions: make([]tool.Option, 0),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// ====== 工具调用上下文信息 ======

// toolCallInfoKey 工具调用信息上下文键（内部使用）
type toolCallInfoKey struct{}

// toolCallInfo 工具调用上下文信息
type toolCallInfo struct {
	toolCallID string
}

// setToolCallInfo 设置工具调用上下文信息
func setToolCallInfo(ctx context.Context, toolCallInfo *toolCallInfo) context.Context {
	return context.WithValue(ctx, toolCallInfoKey{}, toolCallInfo)
}

// GetToolCallID 获取当前工具调用 ID - 从上下文中提取工具调用的唯一标识符
// 用于在工具执行过程中获取当前的调用 ID
func GetToolCallID(ctx context.Context) string {
	v := ctx.Value(toolCallInfoKey{})
	if v == nil {
		return ""
	}

	info, ok := v.(*toolCallInfo)
	if !ok {
		return ""
	}

	return info.toolCallID
}
