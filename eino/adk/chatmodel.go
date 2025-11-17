/*
 * chatmodel.go - ChatModelAgent 实现，基于 ReAct 模式的智能体
 *
 * 核心组件：
 *   - ChatModelAgent: 基于 ChatModel 的智能体实现，支持工具调用和智能体转移
 *   - AgentMiddleware: 中间件机制，支持在关键节点扩展智能体行为
 *   - ToolsConfig: 工具配置，支持直接返回工具和工具调用中间件
 *
 * 设计特点：
 *   - ReAct 模式: 支持推理-行动循环，通过工具调用实现复杂任务
 *   - 智能体转移: 支持子智能体和父智能体间的任务转移
 *   - 中断恢复: 支持中断智能体执行并从检查点恢复
 *   - 冻结机制: 首次运行后冻结配置，确保线程安全
 *   - 流式输出: 支持流式和非流式两种执行模式
 */

package adk

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bytedance/sonic"

	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/components/tool"
	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/internal/safe"
	"github.com/favbox/eino/schema"
	ub "github.com/favbox/eino/utils/callbacks"
)

// chatModelAgentRunOptions 聚合 ChatModelAgent 运行时的各类选项。
// 通过 GetImplSpecificOptions 从 AgentRunOption 列表中提取。
type chatModelAgentRunOptions struct {
	chatModelOptions []model.Option                             // ChatModel 调用选项
	toolOptions      []tool.Option                              // 工具调用选项
	agentToolOptions map[ /*tool name*/ string][]AgentRunOption // 智能体工具的运行选项

	historyModifier func(context.Context, []Message) []Message // 恢复时修改历史消息的函数
}

// WithChatModelOptions 设置 ChatModel 的调用选项
func WithChatModelOptions(opts []model.Option) AgentRunOption {
	return WrapImplSpecificOptFn(func(t *chatModelAgentRunOptions) {
		t.chatModelOptions = opts
	})
}

// WithToolOptions 设置工具调用选项
func WithToolOptions(opts []tool.Option) AgentRunOption {
	return WrapImplSpecificOptFn(func(t *chatModelAgentRunOptions) {
		t.toolOptions = opts
	})
}

// WithAgentToolRunOptions 为智能体工具设置运行选项，key 为工具名称
func WithAgentToolRunOptions(opts map[string] /*tool name*/ []AgentRunOption) AgentRunOption {
	return WrapImplSpecificOptFn(func(t *chatModelAgentRunOptions) {
		t.agentToolOptions = opts
	})
}

// WithHistoryModifier 设置历史消息修改函数，用于恢复时修改历史记录
func WithHistoryModifier(f func(context.Context, []Message) []Message) AgentRunOption {
	return WrapImplSpecificOptFn(func(t *chatModelAgentRunOptions) {
		t.historyModifier = f
	})
}

// ToolsConfig 配置智能体的工具行为。
// 继承 compose.ToolsNodeConfig 的基础配置，并增加直接返回工具的支持。
type ToolsConfig struct {
	compose.ToolsNodeConfig

	// ReturnDirectly 指定哪些工具被调用后会立即返回结果。
	// 当多个直接返回工具同时被调用时，只有第一个会触发返回。
	// map 的 key 是工具名称，value 为 true 表示该工具会触发立即返回。
	ReturnDirectly map[string]bool
}

// GenModelInput 将智能体指令和输入转换为模型可接受的消息格式。
// 默认实现会将指令作为系统消息，并支持 f-string 占位符替换。
type GenModelInput func(ctx context.Context, instruction string, input *AgentInput) ([]Message, error)

func defaultGenModelInput(ctx context.Context, instruction string, input *AgentInput) ([]Message, error) {
	msgs := make([]Message, 0, len(input.Messages)+1)

	if instruction != "" {
		sp := schema.SystemMessage(instruction)

		vs := GetSessionValues(ctx)
		if len(vs) > 0 {
			ct := prompt.FromMessages(schema.FString, sp)
			ms, err := ct.Format(ctx, vs)
			if err != nil {
				return nil, err
			}

			sp = ms[0]
		}

		msgs = append(msgs, sp)
	}

	msgs = append(msgs, input.Messages...)

	return msgs, nil
}

// ChatModelAgentState 表示智能体在对话过程中的状态
type ChatModelAgentState struct {
	// Messages 包含当前对话会话中的所有消息
	Messages []Message
}

// AgentMiddleware 提供在智能体执行的各个阶段自定义行为的钩子。
// 支持添加额外指令、工具、以及在 ChatModel 调用前后修改状态。
type AgentMiddleware struct {
	// AdditionalInstruction 添加到智能体系统指令的补充文本。
	// 会在每次 ChatModel 调用前与基础指令拼接。
	AdditionalInstruction string

	// AdditionalTools 添加到智能体工具集的补充工具。
	// 这些工具会与智能体配置的工具合并使用。
	AdditionalTools []tool.BaseTool

	// BeforeChatModel 在每次 ChatModel 调用前执行，可以修改智能体状态
	BeforeChatModel func(context.Context, *ChatModelAgentState) error

	// AfterChatModel 在每次 ChatModel 调用后执行，可以修改智能体状态
	AfterChatModel func(context.Context, *ChatModelAgentState) error

	// WrapToolCall 用自定义中间件逻辑包装工具调用。
	// 中间件可以包含 Invokable 和/或 Streamable 函数。
	WrapToolCall compose.ToolMiddleware
}

// ChatModelAgentConfig 配置 ChatModelAgent 的行为和能力。
// 包含智能体的基本信息、模型配置、工具配置和扩展机制。
type ChatModelAgentConfig struct {
	// Name 是智能体的名称，建议在所有智能体中保持唯一
	Name string

	// Description 描述智能体的能力，帮助其他智能体判断是否转移任务
	Description string

	// Instruction 用作智能体的系统提示词。
	// 可选，为空时不使用系统提示词。
	// 在默认的 GenModelInput 中支持 f-string 占位符，例如：
	// "You are a helpful assistant. The current time is {Time}. The current user is {User}."
	// 这些占位符会被替换为会话值 "Time" 和 "User"。
	Instruction string

	// Model 是智能体使用的支持工具调用的聊天模型
	Model model.ToolCallingChatModel

	// ToolsConfig 配置智能体可用的工具和工具调用行为
	ToolsConfig ToolsConfig

	// GenModelInput 将指令和输入消息转换为模型的输入格式。
	// 可选，默认使用 defaultGenModelInput 合并指令和消息。
	GenModelInput GenModelInput

	// Exit 定义用于终止智能体流程的工具。
	// 可选，为 nil 时不会生成退出动作。
	// 可以直接使用提供的 ExitTool 实现。
	Exit tool.BaseTool

	// OutputKey 指定将智能体响应存储到会话的键名。
	// 可选，设置后会通过 AddSessionValue(ctx, outputKey, msg.Content) 存储输出。
	OutputKey string

	// MaxIterations 定义 ChatModel 生成循环的上限次数。
	// 超过此限制时智能体会以错误终止。
	// 可选，默认为 20。
	MaxIterations int

	// Middlewares 配置智能体中间件以扩展功能
	Middlewares []AgentMiddleware
}

// ChatModelAgent 实现基于 ChatModel 的智能体，支持 ReAct 模式的推理-行动循环。
// 首次运行后会冻结配置以确保线程安全，支持工具调用、智能体转移和中断恢复。
type ChatModelAgent struct {
	name        string
	description string
	instruction string

	model       model.ToolCallingChatModel
	toolsConfig ToolsConfig

	genModelInput GenModelInput

	outputKey     string
	maxIterations int

	subAgents   []Agent
	parentAgent Agent

	disallowTransferToParent bool

	exit tool.BaseTool

	beforeChatModels, afterChatModels []func(context.Context, *ChatModelAgentState) error

	once   sync.Once // 确保 run 函数只构建一次
	run    runFunc   // 智能体的运行函数
	frozen uint32    // 冻结标志，首次运行后设置为 1
}

type runFunc func(ctx context.Context, input *AgentInput, generator *AsyncGenerator[*AgentEvent], store *mockStore, opts ...compose.Option)

// NewChatModelAgent 创建并返回一个新的 ChatModelAgent。
// 会校验必填配置项，合并中间件的指令和工具，返回配置完整的智能体实例。
func NewChatModelAgent(_ context.Context, config *ChatModelAgentConfig) (*ChatModelAgent, error) {
	if config.Name == "" {
		return nil, errors.New("agent 'Name' is required")
	}
	if config.Description == "" {
		return nil, errors.New("agent 'Description' is required")
	}
	if config.Model == nil {
		return nil, errors.New("agent 'Model' is required")
	}

	genInput := defaultGenModelInput
	if config.GenModelInput != nil {
		genInput = config.GenModelInput
	}

	beforeChatModels := make([]func(context.Context, *ChatModelAgentState) error, 0)
	afterChatModels := make([]func(context.Context, *ChatModelAgentState) error, 0)
	sb := &strings.Builder{}
	sb.WriteString(config.Instruction)
	tc := config.ToolsConfig
	for _, m := range config.Middlewares {
		sb.WriteString("\n")
		sb.WriteString(m.AdditionalInstruction)
		tc.Tools = append(tc.Tools, m.AdditionalTools...)

		if m.WrapToolCall.Invokable != nil || m.WrapToolCall.Streamable != nil {
			tc.ToolCallMiddlewares = append(tc.ToolCallMiddlewares, m.WrapToolCall)
		}
		if m.BeforeChatModel != nil {
			beforeChatModels = append(beforeChatModels, m.BeforeChatModel)
		}
		if m.AfterChatModel != nil {
			afterChatModels = append(afterChatModels, m.AfterChatModel)
		}
	}

	return &ChatModelAgent{
		name:             config.Name,
		description:      config.Description,
		instruction:      sb.String(),
		model:            config.Model,
		toolsConfig:      tc,
		genModelInput:    genInput,
		exit:             config.Exit,
		outputKey:        config.OutputKey,
		maxIterations:    config.MaxIterations,
		beforeChatModels: beforeChatModels,
		afterChatModels:  afterChatModels,
	}, nil
}

const (
	// TransferToAgentToolName 是智能体转移工具的名称
	TransferToAgentToolName = "transfer_to_agent"
	// TransferToAgentToolDesc 是智能体转移工具的描述
	TransferToAgentToolDesc = "Transfer the question to another agent."
)

var (
	toolInfoTransferToAgent = &schema.ToolInfo{
		Name: TransferToAgentToolName,
		Desc: TransferToAgentToolDesc,

		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"agent_name": {
				Desc:     "the name of the agent to transfer to",
				Required: true,
				Type:     schema.String,
			},
		}),
	}

	// ToolInfoExit 是退出工具的信息定义
	ToolInfoExit = &schema.ToolInfo{
		Name: "exit",
		Desc: "Exit the agent process and return the final result.",

		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"final_result": {
				Desc:     "the final result to return",
				Required: true,
				Type:     schema.String,
			},
		}),
	}
)

// ExitTool 实现智能体退出工具，调用后会生成退出动作并返回最终结果
type ExitTool struct{}

// Info 返回退出工具的信息定义
func (et ExitTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return ToolInfoExit, nil
}

// InvokableRun 执行退出工具，解析最终结果并发送退出动作
func (et ExitTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	type exitParams struct {
		FinalResult string `json:"final_result"`
	}

	params := &exitParams{}
	err := sonic.UnmarshalString(argumentsInJSON, params)
	if err != nil {
		return "", err
	}

	err = SendToolGenAction(ctx, "exit", NewExitAction())
	if err != nil {
		return "", err
	}

	return params.FinalResult, nil
}

type transferToAgent struct{}

func (tta transferToAgent) Info(_ context.Context) (*schema.ToolInfo, error) {
	return toolInfoTransferToAgent, nil
}

func transferToAgentToolOutput(destName string) string {
	return fmt.Sprintf("successfully transferred to agent [%s]", destName)
}

func (tta transferToAgent) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	type transferParams struct {
		AgentName string `json:"agent_name"`
	}

	params := &transferParams{}
	err := sonic.UnmarshalString(argumentsInJSON, params)
	if err != nil {
		return "", err
	}

	err = SendToolGenAction(ctx, TransferToAgentToolName, NewTransferToAgentAction(params.AgentName))
	if err != nil {
		return "", err
	}

	return transferToAgentToolOutput(params.AgentName), nil
}

// Name 返回智能体的名称
func (a *ChatModelAgent) Name(_ context.Context) string {
	return a.name
}

// Description 返回智能体的能力描述
func (a *ChatModelAgent) Description(_ context.Context) string {
	return a.description
}

// OnSetSubAgents 设置子智能体列表。
// 智能体冻结后或已设置子智能体时会返回错误。
func (a *ChatModelAgent) OnSetSubAgents(_ context.Context, subAgents []Agent) error {
	if atomic.LoadUint32(&a.frozen) == 1 {
		return errors.New("agent has been frozen after run")
	}

	if len(a.subAgents) > 0 {
		return errors.New("agent's sub-agents has already been set")
	}

	a.subAgents = subAgents
	return nil
}

// OnSetAsSubAgent 将当前智能体设置为另一个智能体的子智能体。
// 智能体冻结后或已设置父智能体时会返回错误。
func (a *ChatModelAgent) OnSetAsSubAgent(_ context.Context, parent Agent) error {
	if atomic.LoadUint32(&a.frozen) == 1 {
		return errors.New("agent has been frozen after run")
	}

	if a.parentAgent != nil {
		return errors.New("agent has already been set as a sub-agent of another agent")
	}

	a.parentAgent = parent
	return nil
}

// OnDisallowTransferToParent 禁止智能体转移任务给父智能体。
// 智能体冻结后会返回错误。
func (a *ChatModelAgent) OnDisallowTransferToParent(_ context.Context) error {
	if atomic.LoadUint32(&a.frozen) == 1 {
		return errors.New("agent has been frozen after run")
	}

	a.disallowTransferToParent = true

	return nil
}

type cbHandler struct {
	*AsyncGenerator[*AgentEvent]
	agentName string

	enableStreaming         bool
	store                   *mockStore
	returnDirectlyToolEvent atomic.Value
}

func (h *cbHandler) onChatModelEnd(ctx context.Context,
	_ *callbacks.RunInfo, output *model.CallbackOutput) context.Context {

	event := EventFromMessage(output.Message, nil, schema.Assistant, "")
	h.Send(event)
	return ctx
}

func (h *cbHandler) onChatModelEndWithStreamOutput(ctx context.Context,
	_ *callbacks.RunInfo, output *schema.StreamReader[*model.CallbackOutput]) context.Context {

	cvt := func(in *model.CallbackOutput) (Message, error) {
		return in.Message, nil
	}
	out := schema.StreamReaderWithConvert(output, cvt)
	event := EventFromMessage(nil, out, schema.Assistant, "")
	h.Send(event)

	return ctx
}

func (h *cbHandler) onToolEnd(ctx context.Context,
	runInfo *callbacks.RunInfo, output *tool.CallbackOutput) context.Context {

	toolCallID := compose.GetToolCallID(ctx)
	msg := schema.ToolMessage(output.Response, toolCallID, schema.WithToolName(runInfo.Name))
	event := EventFromMessage(msg, nil, schema.Tool, runInfo.Name)

	action := popToolGenAction(ctx, runInfo.Name)
	event.Action = action

	returnDirectlyID, hasReturnDirectly := getReturnDirectlyToolCallID(ctx)
	if hasReturnDirectly && returnDirectlyID == toolCallID {
		// return-directly tool event will be sent on the end of tools node to ensure this event must be the last tool event.
		h.returnDirectlyToolEvent.Store(event)
	} else {
		h.Send(event)
	}
	return ctx
}

func (h *cbHandler) onToolEndWithStreamOutput(ctx context.Context,
	runInfo *callbacks.RunInfo, output *schema.StreamReader[*tool.CallbackOutput]) context.Context {

	toolCallID := compose.GetToolCallID(ctx)
	cvt := func(in *tool.CallbackOutput) (Message, error) {
		return schema.ToolMessage(in.Response, toolCallID), nil
	}
	out := schema.StreamReaderWithConvert(output, cvt)
	event := EventFromMessage(nil, out, schema.Tool, runInfo.Name)

	returnDirectlyID, hasReturnDirectly := getReturnDirectlyToolCallID(ctx)
	if hasReturnDirectly && returnDirectlyID == toolCallID {
		// return-directly tool event will be sent on the end of tools node to ensure this event must be the last tool event.
		h.returnDirectlyToolEvent.Store(event)
	} else {
		h.Send(event)
	}

	return ctx
}

func (h *cbHandler) sendReturnDirectlyToolEvent() {
	if e, ok := h.returnDirectlyToolEvent.Load().(*AgentEvent); ok && e != nil {
		h.Send(e)
	}
}

func (h *cbHandler) onToolsNodeEnd(ctx context.Context, _ *callbacks.RunInfo, _ []*schema.Message) context.Context {
	h.sendReturnDirectlyToolEvent()
	return ctx
}

func (h *cbHandler) onToolsNodeEndWithStreamOutput(ctx context.Context, _ *callbacks.RunInfo, _ *schema.StreamReader[[]*schema.Message]) context.Context {
	h.sendReturnDirectlyToolEvent()
	return ctx
}

// ChatModelAgentInterruptInfo 包装中断信息和检查点数据，用于序列化和恢复。
// 保存时将临时中断信息替换为完整的中断信息。
type ChatModelAgentInterruptInfo struct {
	Info *compose.InterruptInfo // 中断信息
	Data []byte                 // 检查点数据
}

func init() {
	schema.RegisterName[*ChatModelAgentInterruptInfo]("_eino_adk_chat_model_agent_interrupt_info")
}

func (h *cbHandler) onGraphError(ctx context.Context,
	_ *callbacks.RunInfo, err error) context.Context {

	info, ok := compose.ExtractInterruptInfo(err)
	if !ok {
		h.Send(&AgentEvent{Err: err})
		return ctx
	}

	data, existed, err := h.store.Get(ctx, mockCheckPointID)
	if err != nil {
		h.Send(&AgentEvent{AgentName: h.agentName, Err: fmt.Errorf("failed to get interrupt info: %w", err)})
		return ctx
	}
	if !existed {
		h.Send(&AgentEvent{AgentName: h.agentName, Err: fmt.Errorf("interrupt has happened, but cannot find interrupt info")})
		return ctx
	}
	h.Send(&AgentEvent{AgentName: h.agentName, Action: &AgentAction{
		Interrupted: &InterruptInfo{
			Data: &ChatModelAgentInterruptInfo{Data: data, Info: info},
		},
	}})

	return ctx
}

func genReactCallbacks(agentName string,
	generator *AsyncGenerator[*AgentEvent],
	enableStreaming bool,
	store *mockStore) compose.Option {

	h := &cbHandler{AsyncGenerator: generator, agentName: agentName, store: store, enableStreaming: enableStreaming}

	cmHandler := &ub.ModelCallbackHandler{
		OnEnd:                 h.onChatModelEnd,
		OnEndWithStreamOutput: h.onChatModelEndWithStreamOutput,
	}
	toolHandler := &ub.ToolCallbackHandler{
		OnEnd:                 h.onToolEnd,
		OnEndWithStreamOutput: h.onToolEndWithStreamOutput,
	}
	toolsNodeHandler := &ub.ToolsNodeCallbackHandlers{
		OnEnd:                 h.onToolsNodeEnd,
		OnEndWithStreamOutput: h.onToolsNodeEndWithStreamOutput,
	}
	graphHandler := callbacks.NewHandlerBuilder().OnErrorFn(h.onGraphError).Build()

	cb := ub.NewHandlerHelper().ChatModel(cmHandler).Tool(toolHandler).ToolsNode(toolsNodeHandler).Chain(graphHandler).Handler()

	return compose.WithCallbacks(cb)
}

func setOutputToSession(ctx context.Context, msg Message, msgStream MessageStream, outputKey string) error {
	if msg != nil {
		AddSessionValue(ctx, outputKey, msg.Content)
		return nil
	}

	concatenated, err := schema.ConcatMessageStream(msgStream)
	if err != nil {
		return err
	}

	AddSessionValue(ctx, outputKey, concatenated.Content)
	return nil
}

func errFunc(err error) runFunc {
	return func(ctx context.Context, input *AgentInput, generator *AsyncGenerator[*AgentEvent], store *mockStore, _ ...compose.Option) {
		generator.Send(&AgentEvent{Err: err})
	}
}

// buildRunFunc 构建智能体的运行函数。
// 使用 sync.Once 确保只构建一次，构建完成后冻结智能体配置。
// 根据是否配置工具决定使用简单的 ChatModel 调用还是完整的 ReAct 循环。
func (a *ChatModelAgent) buildRunFunc(ctx context.Context) runFunc {
	a.once.Do(func() {
		instruction := a.instruction
		toolsNodeConf := a.toolsConfig.ToolsNodeConfig
		returnDirectly := copyMap(a.toolsConfig.ReturnDirectly)

		transferToAgents := a.subAgents
		if a.parentAgent != nil && !a.disallowTransferToParent {
			transferToAgents = append(transferToAgents, a.parentAgent)
		}

		if len(transferToAgents) > 0 {
			transferInstruction := genTransferToAgentInstruction(ctx, transferToAgents)
			instruction = concatInstructions(instruction, transferInstruction)

			toolsNodeConf.Tools = append(toolsNodeConf.Tools, &transferToAgent{})
			returnDirectly[TransferToAgentToolName] = true
		}

		if a.exit != nil {
			toolsNodeConf.Tools = append(toolsNodeConf.Tools, a.exit)
			exitInfo, err := a.exit.Info(ctx)
			if err != nil {
				a.run = errFunc(err)
				return
			}
			returnDirectly[exitInfo.Name] = true
		}

		if len(toolsNodeConf.Tools) == 0 {
			a.run = func(ctx context.Context, input *AgentInput, generator *AsyncGenerator[*AgentEvent], store *mockStore, opts ...compose.Option) {
				r, err := compose.NewChain[*AgentInput, Message]().
					AppendLambda(compose.InvokableLambda(func(ctx context.Context, input *AgentInput) ([]Message, error) {
						return a.genModelInput(ctx, instruction, input)
					})).
					AppendChatModel(a.model).
					Compile(ctx, compose.WithGraphName(a.name))
				if err != nil {
					generator.Send(&AgentEvent{Err: err})
					return
				}

				var msg Message
				var msgStream MessageStream
				if input.EnableStreaming {
					msgStream, err = r.Stream(ctx, input) // todo: chat model option
				} else {
					msg, err = r.Invoke(ctx, input)
				}

				var event *AgentEvent
				if err == nil {
					if a.outputKey != "" {
						if msgStream != nil {
							// copy the stream first because when setting output to session, the stream will be consumed
							ss := msgStream.Copy(2)
							event = EventFromMessage(msg, ss[1], schema.Assistant, "")
							msgStream = ss[0]
						} else {
							event = EventFromMessage(msg, nil, schema.Assistant, "")
						}
						// send event asap, because setting output to session will block until stream fully consumed
						generator.Send(event)
						err = setOutputToSession(ctx, msg, msgStream, a.outputKey)
						if err != nil {
							generator.Send(&AgentEvent{Err: err})
						}
					} else {
						event = EventFromMessage(msg, msgStream, schema.Assistant, "")
						generator.Send(event)
					}
				} else {
					event = &AgentEvent{Err: err}
					generator.Send(event)
				}

				generator.Close()
			}

			return
		}

		// react
		conf := &reactConfig{
			model:               a.model,
			toolsConfig:         &toolsNodeConf,
			toolsReturnDirectly: returnDirectly,
			agentName:           a.name,
			maxIterations:       a.maxIterations,
			beforeChatModel:     a.beforeChatModels,
			afterChatModel:      a.afterChatModels,
		}

		g, err := newReact(ctx, conf)
		if err != nil {
			a.run = errFunc(err)
			return
		}

		a.run = func(ctx context.Context, input *AgentInput, generator *AsyncGenerator[*AgentEvent], store *mockStore, opts ...compose.Option) {
			var compileOptions []compose.GraphCompileOption
			compileOptions = append(compileOptions,
				compose.WithGraphName(a.name),
				compose.WithCheckPointStore(store),
				compose.WithSerializer(&gobSerializer{}),
				// ensure the graph won't exceed max steps due to max iterations
				compose.WithMaxRunSteps(math.MaxInt))

			runnable, err_ := compose.NewChain[*AgentInput, Message]().
				AppendLambda(
					compose.InvokableLambda(func(ctx context.Context, input *AgentInput) ([]Message, error) {
						return a.genModelInput(ctx, instruction, input)
					}),
				).
				AppendGraph(g, compose.WithNodeName("ReAct")).
				Compile(ctx, compileOptions...)
			if err_ != nil {
				generator.Send(&AgentEvent{Err: err_})
				return
			}

			callOpt := genReactCallbacks(a.name, generator, input.EnableStreaming, store)

			var msg Message
			var msgStream MessageStream
			if input.EnableStreaming {
				msgStream, err_ = runnable.Stream(ctx, input, append(opts, callOpt)...)
			} else {
				msg, err_ = runnable.Invoke(ctx, input, append(opts, callOpt)...)
			}

			if err_ == nil {
				if a.outputKey != "" {
					err_ = setOutputToSession(ctx, msg, msgStream, a.outputKey)
					if err_ != nil {
						generator.Send(&AgentEvent{Err: err_})
					}
				} else if msgStream != nil {
					msgStream.Close()
				}
			}

			generator.Close()
		}
	})

	atomic.StoreUint32(&a.frozen, 1)

	return a.run
}

// Run 运行智能体并返回事件迭代器。
// 支持流式和非流式执行模式，通过 AsyncIterator 异步返回智能体事件。
// 首次调用会触发运行函数的构建并冻结智能体配置。
func (a *ChatModelAgent) Run(ctx context.Context, input *AgentInput, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	run := a.buildRunFunc(ctx)

	co := getComposeOptions(opts)
	co = append(co, compose.WithCheckPointID(mockCheckPointID))

	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()
	go func() {
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				e := safe.NewPanicErr(panicErr, debug.Stack())
				generator.Send(&AgentEvent{Err: e})
			}

			generator.Close()
		}()

		run(ctx, input, generator, newEmptyStore(), co...)
	}()

	return iterator
}

// Resume 从中断点恢复智能体执行并返回事件迭代器。
// 使用保存的检查点数据恢复智能体状态，继续执行被中断的任务。
func (a *ChatModelAgent) Resume(ctx context.Context, info *ResumeInfo, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	run := a.buildRunFunc(ctx)

	co := getComposeOptions(opts)
	co = append(co, compose.WithCheckPointID(mockCheckPointID))

	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()
	go func() {
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				e := safe.NewPanicErr(panicErr, debug.Stack())
				generator.Send(&AgentEvent{Err: e})
			}

			generator.Close()
		}()

		run(ctx, &AgentInput{EnableStreaming: info.EnableStreaming}, generator, newResumeStore(info.Data.(*ChatModelAgentInterruptInfo).Data), co...)
	}()

	return iterator
}

func getComposeOptions(opts []AgentRunOption) []compose.Option {
	o := GetImplSpecificOptions[chatModelAgentRunOptions](nil, opts...)
	var co []compose.Option
	if len(o.chatModelOptions) > 0 {
		co = append(co, compose.WithChatModelOption(o.chatModelOptions...))
	}
	var to []tool.Option
	if len(o.toolOptions) > 0 {
		to = append(to, o.toolOptions...)
	}
	for toolName, atos := range o.agentToolOptions {
		to = append(to, withAgentToolOptions(toolName, atos))
	}
	if len(to) > 0 {
		co = append(co, compose.WithToolsNodeOption(compose.WithToolOption(to...)))
	}
	if o.historyModifier != nil {
		co = append(co, compose.WithStateModifier(func(ctx context.Context, path compose.NodePath, state any) error {
			s, ok := state.(*State)
			if !ok {
				return fmt.Errorf("unexpected state type: %T, expected: %T", state, &State{})
			}
			s.Messages = o.historyModifier(ctx, s.Messages)
			return nil
		}))
	}
	return co
}

type gobSerializer struct{}

func (g *gobSerializer) Marshal(v any) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := gob.NewEncoder(buf).Encode(v)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (g *gobSerializer) Unmarshal(data []byte, v any) error {
	buf := bytes.NewBuffer(data)
	return gob.NewDecoder(buf).Decode(v)
}
