package compose

/*
 * tool_node.go - 工具节点实现，支持在图中执行工具调用
 *
 * 核心组件：
 *   - ToolsNode: 工具执行节点，支持并行/顺序执行、流式/非流式调用
 *   - ToolMiddleware: 工具调用中间件，用于拦截和增强工具执行
 *   - ToolsNodeConfig: 工具节点配置，支持未知工具处理、参数预处理
 *
 * 设计特点：
 *   - 自动适配 InvokableTool 和 StreamableTool 两种工具类型
 *   - 支持工具调用中断与重试机制
 *   - 通过 ToolCallID 追踪工具调用上下文
 *
 * 执行模式：
 *   - Invoke: 收集所有工具调用结果后返回
 *   - Stream: 以流式方式返回工具调用结果
 */

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

type toolsNodeOptions struct {
	ToolOptions   []tool.Option
	ToolList      []tool.BaseTool
	executedTools map[string]string
}

// ToolsNodeOption 配置 ToolsNode 的选项函数类型
type ToolsNodeOption func(o *toolsNodeOptions)

// WithToolOption 为 ToolsNode 添加工具选项
func WithToolOption(opts ...tool.Option) ToolsNodeOption {
	return func(o *toolsNodeOptions) {
		o.ToolOptions = append(o.ToolOptions, opts...)
	}
}

// WithToolList 设置 ToolsNode 的工具列表
func WithToolList(tool ...tool.BaseTool) ToolsNodeOption {
	return func(o *toolsNodeOptions) {
		o.ToolList = tool
	}
}

func withExecutedTools(executedTools map[string]string) ToolsNodeOption {
	return func(o *toolsNodeOptions) {
		o.executedTools = executedTools
	}
}

// ToolsNode 表示在图中执行工具调用的节点。
//
// Graph Node 接口定义如下：
//
//	Invoke(ctx context.Context, input *schema.Message, opts ...ToolsNodeOption) ([]*schema.Message, error)
//	Stream(ctx context.Context, input *schema.Message, opts ...ToolsNodeOption) (*schema.StreamReader[[]*schema.Message], error)
//
// 输入为包含 []schema.ToolCall 的 schema.AssistantMessage，
// 输出为 schema.ToolMessage 数组，元素顺序与输入中的 []schema.ToolCall 顺序一致
type ToolsNode struct {
	tuple                     *toolsTuple
	unknownToolHandler        func(ctx context.Context, name, input string) (string, error)
	executeSequentially       bool
	toolArgumentsHandler      func(ctx context.Context, name, input string) (string, error)
	toolCallMiddlewares       []InvokableToolMiddleware
	streamToolCallMiddlewares []StreamableToolMiddleware
}

// ToolInput 工具调用的输入参数
type ToolInput struct {
	Name        string        //  待执行的工具名称
	Arguments   string        //  工具调用的参数
	CallID      string        //  工具调用的唯一标识符
	CallOptions []tool.Option //  执行时的工具选项
}

// ToolOutput 非流式工具调用的执行结果
type ToolOutput struct {
	Result string //  工具执行的字符串输出
}

// StreamToolOutput 流式工具调用的执行结果
type StreamToolOutput struct {
	Result *schema.StreamReader[string] // 工具流式输出
}

type InvokableToolEndpoint func(ctx context.Context, input *ToolInput) (*ToolOutput, error)

type StreamableToolEndpoint func(ctx context.Context, input *ToolInput) (*StreamToolOutput, error)

// InvokableToolMiddleware 包装 InvokableToolEndpoint 以添加自定义处理逻辑的函数。
// 可用于拦截、修改或增强非流式工具的执行
type InvokableToolMiddleware func(InvokableToolEndpoint) InvokableToolEndpoint

// StreamableToolMiddleware 包装 StreamableToolEndpoint 以添加自定义处理逻辑的函数。
// 可用于拦截、修改或增强流式工具的执行
type StreamableToolMiddleware func(StreamableToolEndpoint) StreamableToolEndpoint

// ToolMiddleware 工具中间件，支持同步和流式工具调用
type ToolMiddleware struct {
	// Invokable 非流式工具调用的中间件函数
	// 注意：仅适用于实现了 InvokableTool 接口的工具
	Invokable InvokableToolMiddleware

	// Streamable 流式工具调用的中间件函数
	// 注意：仅适用于实现了 StreamableTool 接口的工具
	Streamable StreamableToolMiddleware
}

// ToolsNodeConfig ToolsNode 的配置
type ToolsNodeConfig struct {
	// Tools 可调用的工具列表，类型为 BaseTool 但必须实现 InvokableTool 或 StreamableTool
	Tools []tool.BaseTool

	// UnknownToolsHandler 处理 LLM 幻觉时调用不存在工具的情况。
	// 此字段可选，未设置时调用不存在的工具会返回错误。
	// 设置后，当 LLM 尝试调用 Tools 列表中不存在的工具时，
	// 会调用此处理器而非返回错误，从而优雅地处理幻觉工具。
	//
	// 参数 ctx 为工具调用的上下文，name 为不存在的工具名称，input 为 LLM 生成的工具调用输入。
	// 返回模拟工具执行的响应字符串和处理过程中的错误
	UnknownToolsHandler func(ctx context.Context, name, input string) (string, error)

	// ExecuteSequentially 决定工具调用是顺序执行还是并行执行。
	// 设为 true 时，工具调用按输入消息中出现的顺序依次执行。
	// 设为 false（默认）时，工具调用并行执行
	ExecuteSequentially bool

	// ToolArgumentsHandler 在执行前处理工具参数。
	// 设置后，每次工具调用都会调用此函数处理参数。
	//
	// 参数 ctx 为工具调用的上下文，name 为被调用工具的名称，arguments 为原始参数字符串。
	// 返回用于工具执行的处理后参数字符串和预处理过程中的错误
	ToolArgumentsHandler func(ctx context.Context, name, arguments string) (string, error)

	// ToolCallMiddlewares 配置工具调用的中间件。
	// 每个元素可包含 Invokable 和/或 Streamable 中间件。
	// Invokable 中间件仅适用于实现 InvokableTool 接口的工具，
	// Streamable 中间件仅适用于实现 StreamableTool 接口的工具
	ToolCallMiddlewares []ToolMiddleware
}

// NewToolNode 创建新的 ToolsNode。
//
// 示例：
//
//	conf := &ToolsNodeConfig{
//		Tools: []tool.BaseTool{invokableTool1, streamableTool2},
//	}
//	toolsNode, err := NewToolNode(ctx, conf)
func NewToolNode(ctx context.Context, conf *ToolsNodeConfig) (*ToolsNode, error) {
	var middlewares []InvokableToolMiddleware
	var streamMiddlewares []StreamableToolMiddleware
	for _, m := range conf.ToolCallMiddlewares {
		if m.Invokable != nil {
			middlewares = append(middlewares, m.Invokable)
		}
		if m.Streamable != nil {
			streamMiddlewares = append(streamMiddlewares, m.Streamable)
		}
	}

	tuple, err := convTools(ctx, conf.Tools, middlewares, streamMiddlewares)
	if err != nil {
		return nil, err
	}

	return &ToolsNode{
		tuple:                     tuple,
		unknownToolHandler:        conf.UnknownToolsHandler,
		executeSequentially:       conf.ExecuteSequentially,
		toolArgumentsHandler:      conf.ToolArgumentsHandler,
		toolCallMiddlewares:       middlewares,
		streamToolCallMiddlewares: streamMiddlewares,
	}, nil
}

// ToolsInterruptAndRerunExtra 工具节点中断和重试额外信息
type ToolsInterruptAndRerunExtra struct {
	ToolCalls     []schema.ToolCall // 工具调用列表
	ExecutedTools map[string]string // 已执行工具映射
	RerunTools    []string          // 需要重试的工具
	RerunExtraMap map[string]any    // 重试额外信息映射
}

// init 注册类型名称，用于序列化和反序列化
func init() {
	schema.RegisterName[*ToolsInterruptAndRerunExtra]("_eino_compose_tools_interrupt_and_rerun_extra") // TODO: check if this is really needed when refactoring adk resume
}

// toolsTuple 工具元组，包含工具的索引、元数据、执行端点
type toolsTuple struct {
	indexes         map[string]int           // 工具名称到索引的映射
	meta            []*executorMeta          // 执行器元数据列表
	endpoints       []InvokableToolEndpoint  // 可调用工具端点列表
	streamEndpoints []StreamableToolEndpoint // 可流式工具端点列表
}

// convTools 转换工具列表为工具元组
func convTools(ctx context.Context, tools []tool.BaseTool, ms []InvokableToolMiddleware, sms []StreamableToolMiddleware) (*toolsTuple, error) {
	ret := &toolsTuple{
		indexes:         make(map[string]int),
		meta:            make([]*executorMeta, len(tools)),
		endpoints:       make([]InvokableToolEndpoint, len(tools)),
		streamEndpoints: make([]StreamableToolEndpoint, len(tools)),
	}
	for idx, bt := range tools {
		// 获取工具信息
		tl, err := bt.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("(NewToolNode) failed to get tool info at idx= %d: %w", idx, err)
		}

		toolName := tl.Name
		var (
			st tool.StreamableTool
			it tool.InvokableTool

			invokable  InvokableToolEndpoint
			streamable StreamableToolEndpoint

			ok   bool
			meta *executorMeta
		)

		meta = parseExecutorInfoFromComponent(components.ComponentOfTool, bt)

		if st, ok = bt.(tool.StreamableTool); ok {
			streamable = wrapStreamToolCall(st, sms, !meta.isComponentCallbackEnabled)
		}

		if it, ok = bt.(tool.InvokableTool); ok {
			invokable = wrapToolCall(it, ms, !meta.isComponentCallbackEnabled)
		}

		if st == nil && it == nil {
			return nil, fmt.Errorf("tool %s is not invokable or streamable", toolName)
		}

		if streamable == nil {
			streamable = invokableToStreamable(invokable)
		}
		if invokable == nil {
			invokable = streamableToInvokable(streamable)
		}

		ret.indexes[toolName] = idx
		ret.meta[idx] = meta
		ret.endpoints[idx] = invokable
		ret.streamEndpoints[idx] = streamable
	}
	return ret, nil
}

// wrapToolCall 包装可调用工具为端点，应用中间件和回调
func wrapToolCall(it tool.InvokableTool, middlewares []InvokableToolMiddleware, needCallback bool) InvokableToolEndpoint {
	middleware := func(next InvokableToolEndpoint) InvokableToolEndpoint {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
	if needCallback {
		it = &invokableToolWithCallback{it: it}
	}
	return middleware(func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		result, err := it.InvokableRun(ctx, input.Arguments, input.CallOptions...)
		if err != nil {
			return nil, err
		}
		return &ToolOutput{Result: result}, nil
	})
}

// wrapStreamToolCall 包装可流式工具为端点，应用中间件和回调
func wrapStreamToolCall(st tool.StreamableTool, middlewares []StreamableToolMiddleware, needCallback bool) StreamableToolEndpoint {
	middleware := func(next StreamableToolEndpoint) StreamableToolEndpoint {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
	if needCallback {
		st = &streamableToolWithCallback{st: st}
	}
	return middleware(func(ctx context.Context, input *ToolInput) (*StreamToolOutput, error) {
		result, err := st.StreamableRun(ctx, input.Arguments, input.CallOptions...)
		if err != nil {
			return nil, err
		}
		return &StreamToolOutput{Result: result}, nil
	})
}

// invokableToolWithCallback 带回调的可调用工具包装器
type invokableToolWithCallback struct {
	it tool.InvokableTool
}

// Info 获取工具信息
func (i *invokableToolWithCallback) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return i.it.Info(ctx)
}

// InvokableRun 执行工具调用（带回调）
func (i *invokableToolWithCallback) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	return invokeWithCallbacks(i.it.InvokableRun)(ctx, argumentsInJSON, opts...)
}

// streamableToolWithCallback 带回调的可流式工具包装器
type streamableToolWithCallback struct {
	st tool.StreamableTool
}

// Info 获取工具信息
func (s *streamableToolWithCallback) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return s.st.Info(ctx)
}

// StreamableRun 执行流式工具调用（带回调）
func (s *streamableToolWithCallback) StreamableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
	return streamWithCallbacks(s.st.StreamableRun)(ctx, argumentsInJSON, opts...)
}

// streamableToInvokable 将流式工具端点转换为可调用工具端点
func streamableToInvokable(e StreamableToolEndpoint) InvokableToolEndpoint {
	return func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		so, err := e(ctx, input)
		if err != nil {
			return nil, err
		}
		o, err := concatStreamReader(so.Result)
		if err != nil {
			return nil, fmt.Errorf("failed to concat StreamableTool output message stream: %w", err)
		}
		return &ToolOutput{Result: o}, nil
	}
}

// invokableToStreamable 将可调用工具端点转换为流式工具端点
func invokableToStreamable(e InvokableToolEndpoint) StreamableToolEndpoint {
	return func(ctx context.Context, input *ToolInput) (*StreamToolOutput, error) {
		o, err := e(ctx, input)
		if err != nil {
			return nil, err
		}
		return &StreamToolOutput{Result: schema.StreamReaderFromArray([]string{o.Result})}, nil
	}
}

// toolCallTask 工具调用任务，封装工具调用的输入输出信息
type toolCallTask struct {
	// 输入参数
	endpoint       InvokableToolEndpoint  // 可调用工具端点
	streamEndpoint StreamableToolEndpoint // 流式工具端点
	meta           *executorMeta          // 执行器元数据
	name           string                 // 工具名称
	arg            string                 // 工具参数
	callID         string                 // 调用ID

	// 输出结果
	executed bool                         // 是否已执行
	output   string                       // 工具输出（非流式）
	sOutput  *schema.StreamReader[string] // 工具输出（流式）
	err      error                        // 执行错误
}

// genToolCallTasks 生成工具调用任务列表
func (tn *ToolsNode) genToolCallTasks(ctx context.Context, tuple *toolsTuple,
	input *schema.Message, executedTools map[string]string, isStream bool) ([]toolCallTask, error) {

	if input.Role != schema.Assistant {
		return nil, fmt.Errorf("expected message role is Assistant, got %s", input.Role)
	}

	n := len(input.ToolCalls)
	if n == 0 {
		return nil, errors.New("no tool call found in input message")
	}

	toolCallTasks := make([]toolCallTask, n)

	for i := 0; i < n; i++ {
		toolCall := input.ToolCalls[i]
		if result, executed := executedTools[toolCall.ID]; executed {
			toolCallTasks[i].name = toolCall.Function.Name
			toolCallTasks[i].arg = toolCall.Function.Arguments
			toolCallTasks[i].callID = toolCall.ID
			toolCallTasks[i].executed = true
			if isStream {
				toolCallTasks[i].sOutput = schema.StreamReaderFromArray([]string{result})
			} else {
				toolCallTasks[i].output = result
			}
			continue
		}
		index, ok := tuple.indexes[toolCall.Function.Name]
		if !ok {
			if tn.unknownToolHandler == nil {
				return nil, fmt.Errorf("tool %s not found in toolsNode indexes", toolCall.Function.Name)
			}
			toolCallTasks[i] = newUnknownToolTask(toolCall.Function.Name, toolCall.Function.Arguments, toolCall.ID, tn.unknownToolHandler)
		} else {
			toolCallTasks[i].endpoint = tuple.endpoints[index]
			toolCallTasks[i].streamEndpoint = tuple.streamEndpoints[index]
			toolCallTasks[i].meta = tuple.meta[index]
			toolCallTasks[i].name = toolCall.Function.Name
			toolCallTasks[i].callID = toolCall.ID
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

// newUnknownToolTask 创建未知工具调用任务
func newUnknownToolTask(name, arg, callID string, unknownToolHandler func(ctx context.Context, name, input string) (string, error)) toolCallTask {
	endpoint := func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		result, err := unknownToolHandler(ctx, input.Name, input.Arguments)
		if err != nil {
			return nil, err
		}
		return &ToolOutput{
			Result: result,
		}, nil
	}
	return toolCallTask{
		endpoint:       endpoint,
		streamEndpoint: invokableToStreamable(endpoint),
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

func runToolCallTaskByInvoke(ctx context.Context, task *toolCallTask, opts ...tool.Option) {
	if task.executed {
		return
	}
	ctx = callbacks.ReuseHandlers(ctx, &callbacks.RunInfo{
		Name:      task.name,
		Type:      task.meta.componentImplType,
		Component: task.meta.component,
	})

	ctx = setToolCallInfo(ctx, &toolCallInfo{toolCallID: task.callID})
	output, err := task.endpoint(ctx, &ToolInput{
		Name:        task.name,
		Arguments:   task.arg,
		CallID:      task.callID,
		CallOptions: opts,
	})
	if err != nil {
		task.err = err
	} else {
		task.output = output.Result
		task.executed = true
	}
}

func runToolCallTaskByStream(ctx context.Context, task *toolCallTask, opts ...tool.Option) {
	ctx = callbacks.ReuseHandlers(ctx, &callbacks.RunInfo{
		Name:      task.name,
		Type:      task.meta.componentImplType,
		Component: task.meta.component,
	})

	ctx = setToolCallInfo(ctx, &toolCallInfo{toolCallID: task.callID})
	output, err := task.streamEndpoint(ctx, &ToolInput{
		Name:        task.name,
		Arguments:   task.arg,
		CallID:      task.callID,
		CallOptions: opts,
	})
	if err != nil {
		task.err = err
	} else {
		task.sOutput = output.Result
		task.executed = true
	}
}

func sequentialRunToolCall(ctx context.Context,
	run func(ctx2 context.Context, callTask *toolCallTask, opts ...tool.Option),
	tasks []toolCallTask, opts ...tool.Option) {

	for i := range tasks {
		if tasks[i].executed {
			continue
		}
		run(ctx, &tasks[i], opts...)
	}
}

func parallelRunToolCall(ctx context.Context,
	run func(ctx2 context.Context, callTask *toolCallTask, opts ...tool.Option),
	tasks []toolCallTask, opts ...tool.Option) {

	if len(tasks) == 1 {
		run(ctx, &tasks[0], opts...)
		return
	}

	var wg sync.WaitGroup
	for i := 1; i < len(tasks); i++ {
		if tasks[i].executed {
			continue
		}
		wg.Add(1)
		go func(ctx_ context.Context, t *toolCallTask, opts ...tool.Option) {
			defer wg.Done()
			defer func() {
				panicErr := recover()
				if panicErr != nil {
					t.err = safe.NewPanicErr(panicErr, debug.Stack())
				}
			}()
			run(ctx_, t, opts...)
		}(ctx, &tasks[i], opts...)
	}

	if !tasks[0].executed {
		run(ctx, &tasks[0], opts...)
	}

	wg.Wait()
}

// Invoke 调用工具并收集可调用工具的结果。
// 输入消息中有多个工具调用时并行执行
func (tn *ToolsNode) Invoke(ctx context.Context, input *schema.Message,
	opts ...ToolsNodeOption) ([]*schema.Message, error) {

	opt := getToolsNodeOptions(opts...)
	tuple := tn.tuple
	if opt.ToolList != nil {
		var err error
		tuple, err = convTools(ctx, opt.ToolList, tn.toolCallMiddlewares, tn.streamToolCallMiddlewares)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool list from call option: %w", err)
		}
	}

	tasks, err := tn.genToolCallTasks(ctx, tuple, input, opt.executedTools, false)
	if err != nil {
		return nil, err
	}

	if tn.executeSequentially {
		sequentialRunToolCall(ctx, runToolCallTaskByInvoke, tasks, opt.ToolOptions...)
	} else {
		parallelRunToolCall(ctx, runToolCallTaskByInvoke, tasks, opt.ToolOptions...)
	}

	n := len(tasks)
	output := make([]*schema.Message, n)

	rerunExtra := &ToolsInterruptAndRerunExtra{
		ToolCalls:     input.ToolCalls,
		ExecutedTools: make(map[string]string),
		RerunExtraMap: make(map[string]any),
	}
	rerun := false
	for i := 0; i < n; i++ {
		if tasks[i].err != nil {
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
		if !rerun {
			output[i] = schema.ToolMessage(tasks[i].output, tasks[i].callID, schema.WithToolName(tasks[i].name))
		}
	}
	if rerun {
		return nil, NewInterruptAndRerunErr(rerunExtra)
	}

	return output, nil
}

// Stream 调用工具并收集流式读取器的结果。
// 输入消息中有多个工具调用时并行执行
func (tn *ToolsNode) Stream(ctx context.Context, input *schema.Message,
	opts ...ToolsNodeOption) (*schema.StreamReader[[]*schema.Message], error) {

	opt := getToolsNodeOptions(opts...)
	tuple := tn.tuple
	if opt.ToolList != nil {
		var err error
		tuple, err = convTools(ctx, opt.ToolList, tn.toolCallMiddlewares, tn.streamToolCallMiddlewares)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool list from call option: %w", err)
		}
	}

	tasks, err := tn.genToolCallTasks(ctx, tuple, input, opt.executedTools, true)
	if err != nil {
		return nil, err
	}

	if tn.executeSequentially {
		sequentialRunToolCall(ctx, runToolCallTaskByStream, tasks, opt.ToolOptions...)
	} else {
		parallelRunToolCall(ctx, runToolCallTaskByStream, tasks, opt.ToolOptions...)
	}

	n := len(tasks)

	rerun := false
	rerunExtra := &ToolsInterruptAndRerunExtra{
		ToolCalls:     input.ToolCalls,
		RerunExtraMap: make(map[string]any),
		ExecutedTools: make(map[string]string),
	}

	// 检查重试
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

	if rerun {
		// 合并并保存工具输出
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

	// 常规返回
	sOutput := make([]*schema.StreamReader[[]*schema.Message], n)
	for i := 0; i < n; i++ {
		index := i
		callID := tasks[i].callID
		callName := tasks[i].name
		cvt := func(s string) ([]*schema.Message, error) {
			ret := make([]*schema.Message, n)
			ret[index] = schema.ToolMessage(s, callID, schema.WithToolName(callName))

			return ret, nil
		}

		sOutput[i] = schema.StreamReaderWithConvert(tasks[i].sOutput, cvt)
	}
	return schema.MergeStreamReaders(sOutput), nil
}

// GetType 获取节点类型
func (tn *ToolsNode) GetType() string {
	return ""
}

// getToolsNodeOptions 获取工具节点选项
func getToolsNodeOptions(opts ...ToolsNodeOption) *toolsNodeOptions {
	o := &toolsNodeOptions{
		ToolOptions: make([]tool.Option, 0),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// toolCallInfoKey 工具调用信息上下文键
type toolCallInfoKey struct{}

// toolCallInfo 工具调用信息
type toolCallInfo struct {
	toolCallID string // 工具调用ID
}

// setToolCallInfo 设置工具调用信息到上下文
func setToolCallInfo(ctx context.Context, toolCallInfo *toolCallInfo) context.Context {
	return context.WithValue(ctx, toolCallInfoKey{}, toolCallInfo)
}

// GetToolCallID 从上下文中获取当前工具调用 ID
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
