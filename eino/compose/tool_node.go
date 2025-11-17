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

// toolsNodeOptions 工具节点运行时选项。
// 用于在运行时动态配置工具执行行为。
type toolsNodeOptions struct {
	ToolOptions   []tool.Option
	ToolList      []tool.BaseTool
	executedTools map[string]string // 已执行工具的结果缓存(CallID -> Result),用于中断后恢复执行。
}

// ToolsNodeOption 工具节点选项函数类型。
type ToolsNodeOption func(o *toolsNodeOptions)

// WithToolOption 添加工具选项。
func WithToolOption(opts ...tool.Option) ToolsNodeOption {
	return func(o *toolsNodeOptions) {
		o.ToolOptions = append(o.ToolOptions, opts...)
	}
}

// WithToolList 设置工具列表。
func WithToolList(tool ...tool.BaseTool) ToolsNodeOption {
	return func(o *toolsNodeOptions) {
		o.ToolList = tool
	}
}

// withExecutedTools 设置已执行工具映射。
func withExecutedTools(executedTools map[string]string) ToolsNodeOption {
	return func(o *toolsNodeOptions) {
		o.executedTools = executedTools
	}
}

// ToolsNode 工具执行节点。
// 支持同步和流式两种执行模式，支持顺序和并行执行。
type ToolsNode struct {
	tuple                     *toolsTuple
	unknownToolHandler        func(ctx context.Context, name, input string) (string, error)
	executeSequentially       bool
	toolArgumentsHandler      func(ctx context.Context, name, input string) (string, error)
	toolCallMiddlewares       []InvokableToolMiddleware
	streamToolCallMiddlewares []StreamableToolMiddleware
}

// ToolInput represents the input parameters for a tool call execution.
type ToolInput struct {
	// Name is the name of the tool to be executed.
	Name string
	// Arguments contains the arguments for the tool call.
	Arguments string
	// CallID is the unique identifier for this tool call.
	CallID string
	// CallOptions contains tool options for the execution.
	CallOptions []tool.Option
}

// ToolOutput represents the result of a non-streaming tool call execution.
type ToolOutput struct {
	// Result contains the string output from the tool execution.
	Result string
}

// StreamToolOutput represents the result of a streaming tool call execution.
type StreamToolOutput struct {
	// Result is a stream reader that provides access to the tool's streaming output.
	Result *schema.StreamReader[string]
}

type InvokableToolEndpoint func(ctx context.Context, input *ToolInput) (*ToolOutput, error)

type StreamableToolEndpoint func(ctx context.Context, input *ToolInput) (*StreamToolOutput, error)

// InvokableToolMiddleware is a function that wraps InvokableToolEndpoint to add custom processing logic.
// It can be used to intercept, modify, or enhance tool call execution for non-streaming tools.
type InvokableToolMiddleware func(InvokableToolEndpoint) InvokableToolEndpoint

// StreamableToolMiddleware is a function that wraps StreamableToolEndpoint to add custom processing logic.
// It can be used to intercept, modify, or enhance tool call execution for streaming tools.
type StreamableToolMiddleware func(StreamableToolEndpoint) StreamableToolEndpoint

type ToolMiddleware struct {
	// Invokable contains middleware function for non-streaming tool calls.
	// Note: This middleware only applies to tools that implement the InvokableTool interface.
	Invokable InvokableToolMiddleware

	// Streamable contains middleware function for streaming tool calls.
	// Note: This middleware only applies to tools that implement the StreamableTool interface.
	Streamable StreamableToolMiddleware
}

// ToolsNodeConfig 工具节点配置。
type ToolsNodeConfig struct {
	// Tools 工具列表。
	Tools []tool.BaseTool

	// UnknownToolsHandler 未知工具处理器。
	// 可选，未设置时调用不存在的工具将返回错误。
	UnknownToolsHandler func(ctx context.Context, name, input string) (string, error)

	// ExecuteSequentially 是否顺序执行工具调用。
	// 默认值：false（并行执行）。
	ExecuteSequentially bool

	// ToolArgumentsHandler 工具参数预处理器。
	// 可选，在工具执行前处理和转换参数。
	ToolArgumentsHandler func(ctx context.Context, name, arguments string) (string, error)

	// ToolCallMiddlewares configures middleware for tool calls.
	// Each element can contain Invokable and/or Streamable middleware.
	// Invokable middleware only applies to tools implementing InvokableTool interface.
	// Streamable middleware only applies to tools implementing StreamableTool interface.
	ToolCallMiddlewares []ToolMiddleware
}

// NewToolNode 创建工具节点实例。
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

// ToolsInterruptAndRerunExtra 工具节点中断重运行扩展信息。
type ToolsInterruptAndRerunExtra struct {
	// ToolCalls 原始的工具调用列表。
	ToolCalls []schema.ToolCall
	// ExecutedTools 已执行工具的映射(CallID -> 执行结果)。
	ExecutedTools map[string]string
	// RerunTools 需要重新运行的工具调用 ID 列表。
	RerunTools []string
	// RerunExtraMap 重新运行的额外信息映射(CallID -> Extra)。
	RerunExtraMap map[string]any
}

func init() {
	schema.RegisterName[*ToolsInterruptAndRerunExtra]("_eino_compose_tools_interrupt_and_rerun_extra") // TODO: check if this is really needed when refactoring adk resume
}

// toolsTuple 工具元数据封装。
type toolsTuple struct {
	indexes map[string]int  // 工具名称到数组索引的映射。
	meta    []*executorMeta // 工具执行器元数据数组(与 indexes 对应)。
	// rps     []*runnablePacker[string, string, tool.Option] // 工具可运行包装器数组(与 indexes 对应)。
	endpoints       []InvokableToolEndpoint
	streamEndpoints []StreamableToolEndpoint
}

// convTools 转换工具列表为内部元数据结构。
func convTools(ctx context.Context, tools []tool.BaseTool, ms []InvokableToolMiddleware, sms []StreamableToolMiddleware) (*toolsTuple, error) {
	ret := &toolsTuple{
		indexes: make(map[string]int),
		meta:    make([]*executorMeta, len(tools)),
		// rps:     make([]*runnablePacker[string, string, tool.Option], len(tools)),
		endpoints:       make([]InvokableToolEndpoint, len(tools)),
		streamEndpoints: make([]StreamableToolEndpoint, len(tools)),
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

			// invokable  func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error)
			// streamable func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error)

			invokable  InvokableToolEndpoint
			streamable StreamableToolEndpoint

			ok   bool
			meta *executorMeta
		)

		meta = parseExecutorInfoFromComponent(components.ComponentOfTool, bt)

		// 检查流式工具接口
		if st, ok = bt.(tool.StreamableTool); ok {
			streamable = wrapStreamToolCall(st, sms, !meta.isComponentCallbackEnabled)
		}

		// 检查同步工具接口
		if it, ok = bt.(tool.InvokableTool); ok {
			invokable = wrapToolCall(it, ms, !meta.isComponentCallbackEnabled)
		}

		// 工具必须实现至少一种执行接口
		if st == nil && it == nil {
			return nil, fmt.Errorf("tool %s is not invokable or streamable", toolName)
		}

		// 提取执行器元数据（用于回调和组件信息）
		// if st != nil {
		// 	meta = parseExecutorInfoFromComponent(components.ComponentOfTool, st)
		// } else {
		// 	meta = parseExecutorInfoFromComponent(components.ComponentOfTool, it)
		// }

		if streamable == nil {
			streamable = invokableToStreamable(invokable)
		}
		if invokable == nil {
			invokable = streamableToInvokable(streamable)
		}

		// 构建工具索引和执行器包装
		ret.indexes[toolName] = idx
		ret.meta[idx] = meta
		// ret.rps[idx] = newRunnablePacker(invokable, streamable, nil, nil, !meta.isComponentCallbackEnabled)
		ret.endpoints[idx] = invokable
		ret.streamEndpoints[idx] = streamable
	}
	return ret, nil
}

// ====== 工具调用任务定义 ======

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

type invokableToolWithCallback struct {
	it tool.InvokableTool
}

func (i *invokableToolWithCallback) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return i.it.Info(ctx)
}

func (i *invokableToolWithCallback) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	return invokeWithCallbacks(i.it.InvokableRun)(ctx, argumentsInJSON, opts...)
}

type streamableToolWithCallback struct {
	st tool.StreamableTool
}

func (s *streamableToolWithCallback) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return s.st.Info(ctx)
}

func (s *streamableToolWithCallback) StreamableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
	return streamWithCallbacks(s.st.StreamableRun)(ctx, argumentsInJSON, opts...)
}

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

func invokableToStreamable(e InvokableToolEndpoint) StreamableToolEndpoint {
	return func(ctx context.Context, input *ToolInput) (*StreamToolOutput, error) {
		o, err := e(ctx, input)
		if err != nil {
			return nil, err
		}
		return &StreamToolOutput{Result: schema.StreamReaderFromArray([]string{o.Result})}, nil
	}
}

// toolCallTask 工具调用任务 - 封装单个工具调用的输入输出
type toolCallTask struct {
	// 输入参数
	r      *runnablePacker[string, string, tool.Option]
	meta   *executorMeta
	name   string
	arg    string
	callID string // 工具调用唯一标识符。

	// 输出结果
	executed bool                         // 标记是否已执行(用于跳过重复调用)。
	output   string                       // 同步执行结果。
	sOutput  *schema.StreamReader[string] // 流式执行结果。
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
			toolCallTasks[i].r = tuple.rps[index]
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

// newUnknownToolTask 创建未知工具任务。
func newUnknownToolTask(name, arg, callID string, unknownToolHandler func(ctx context.Context, name, input string) (string, error)) toolCallTask {
	return toolCallTask{
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

// runToolCallTaskByInvoke 同步执行工具调用任务。
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
	task.output, task.err = task.r.Invoke(ctx, task.arg, opts...)
	if task.err == nil {
		task.executed = true
	}
}

// runToolCallTaskByStream 流式执行工具调用任务。
func runToolCallTaskByStream(ctx context.Context, task *toolCallTask, opts ...tool.Option) {
	ctx = callbacks.ReuseHandlers(ctx, &callbacks.RunInfo{
		Name:      task.name,
		Type:      task.meta.componentImplType,
		Component: task.meta.component,
	})

	ctx = setToolCallInfo(ctx, &toolCallInfo{toolCallID: task.callID})
	task.sOutput, task.err = task.r.Stream(ctx, task.arg, opts...)
	if task.err == nil {
		task.executed = true
	}
}

// sequentialRunToolCall 顺序执行工具调用。
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

// parallelRunToolCall 并行执行工具调用。
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

// Invoke 同步执行工具调用并返回结果。
func (tn *ToolsNode) Invoke(ctx context.Context, input *schema.Message,
	opts ...ToolsNodeOption) ([]*schema.Message, error) {

	opt := getToolsNodeOptions(opts...)
	tuple := tn.tuple

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

	opt := getToolsNodeOptions(opts...)
	tuple := tn.tuple

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
	return schema.MergeStreamReaders(sOutput), nil
}

// GetType 返回节点类型。
func (tn *ToolsNode) GetType() string {
	return ""
}

// getToolsNodeOptions 解析工具节点选项。
func getToolsNodeOptions(opts ...ToolsNodeOption) *toolsNodeOptions {
	o := &toolsNodeOptions{
		ToolOptions: make([]tool.Option, 0),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// toolCallInfoKey 工具调用信息的上下文键类型。
type toolCallInfoKey struct{}

// toolCallInfo 工具调用上下文信息。
type toolCallInfo struct {
	toolCallID string // 当前工具调用的唯一标识符。
}

// setToolCallInfo 将工具调用信息注入上下文。
func setToolCallInfo(ctx context.Context, toolCallInfo *toolCallInfo) context.Context {
	return context.WithValue(ctx, toolCallInfoKey{}, toolCallInfo)
}

// GetToolCallID 获取当前工具调用 ID。
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
