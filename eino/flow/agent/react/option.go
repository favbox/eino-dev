package react

import (
	"context"

	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/tool"
	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/flow/agent"
	"github.com/favbox/eino/internal"
	"github.com/favbox/eino/schema"
	ub "github.com/favbox/eino/utils/callbacks"
)

// WithToolOptions 为智能体配置工具选项
func WithToolOptions(opts ...tool.Option) agent.AgentOption {
	return agent.WithComposeOptions(compose.WithToolsNodeOption(compose.WithToolOption(opts...)))
}

// WithChatModelOptions 为智能体配置聊天模型选项
func WithChatModelOptions(opts ...model.Option) agent.AgentOption {
	return agent.WithComposeOptions(compose.WithChatModelOption(opts...))
}

// WithToolList 配置工具节点的工具列表
// 已废弃：推荐使用 WithTools，它会同时配置模型和工具节点
func WithToolList(tools ...tool.BaseTool) agent.AgentOption {
	return agent.WithComposeOptions(compose.WithToolsNodeOption(compose.WithToolList(tools...)))
}

// WithTools 为 ReAct 智能体配置工具列表
// 同时配置聊天模型的工具信息和工具节点的实际实现
// 返回必须同时应用的 2 个选项
func WithTools(ctx context.Context, tools ...tool.BaseTool) ([]agent.AgentOption, error) {
	toolInfos := make([]*schema.ToolInfo, 0, len(tools))
	for _, tl := range tools {
		info, err := tl.Info(ctx)
		if err != nil {
			return nil, err
		}

		toolInfos = append(toolInfos, info)
	}

	opts := make([]agent.AgentOption, 2)
	opts[0] = agent.WithComposeOptions(compose.WithChatModelOption(model.WithTools(toolInfos)))
	opts[1] = agent.WithComposeOptions(compose.WithToolsNodeOption(compose.WithToolList(tools...)))
	return opts, nil
}

// Iterator 提供对通道消息的安全迭代访问
type Iterator[T any] struct {
	ch *internal.UnboundedChan[item[T]]
}

// Next 获取下一个消息项
func (iter *Iterator[T]) Next() (T, bool, error) {
	ch := iter.ch
	if ch == nil {
		var zero T
		return zero, false, nil
	}

	i, ok := ch.Receive()
	if !ok {
		var zero T
		return zero, false, nil
	}

	return i.v, true, i.err
}

// MessageFuture 提供对智能体执行过程中生成消息的异步访问
type MessageFuture interface {
	// GetMessages 获取非流式消息迭代器（用于 Generate 方法）
	GetMessages() *Iterator[*schema.Message]

	// GetMessageStreams 获取流式消息迭代器（用于 Stream 方法）
	GetMessageStreams() *Iterator[*schema.StreamReader[*schema.Message]]
}

// WithMessageFuture 配置智能体执行过程中的消息收集
// 返回配置选项和用于异步访问收集消息的接口
func WithMessageFuture() (agent.AgentOption, MessageFuture) {
	h := &cbHandler{started: make(chan struct{})}

	cmHandler := &ub.ModelCallbackHandler{
		OnEnd:                 h.onChatModelEnd,
		OnEndWithStreamOutput: h.onChatModelEndWithStreamOutput,
	}
	toolHandler := &ub.ToolCallbackHandler{
		OnEnd:                 h.onToolEnd,
		OnEndWithStreamOutput: h.onToolEndWithStreamOutput,
	}
	graphHandler := callbacks.NewHandlerBuilder().
		OnStartFn(h.onGraphStart).
		OnStartWithStreamInputFn(h.onGraphStartWithStreamInput).
		OnEndFn(h.onGraphEnd).
		OnEndWithStreamOutputFn(h.onGraphEndWithStreamOutput).
		OnErrorFn(h.onGraphError).Build()
	cb := ub.NewHandlerHelper().ChatModel(cmHandler).Tool(toolHandler).Graph(graphHandler).Handler()

	option := agent.WithComposeOptions(compose.WithCallbacks(cb))

	return option, h
}

type item[T any] struct {
	v   T
	err error
}

// cbHandler 通过回调机制收集和分发智能体执行过程中的消息
type cbHandler struct {
	msgs  *internal.UnboundedChan[item[*schema.Message]]
	sMsgs *internal.UnboundedChan[item[*schema.StreamReader[*schema.Message]]]

	started chan struct{}
}

// GetMessages 获取非流式消息迭代器
func (h *cbHandler) GetMessages() *Iterator[*schema.Message] {
	<-h.started

	return &Iterator[*schema.Message]{ch: h.msgs}
}

// GetMessageStreams 获取流式消息迭代器
func (h *cbHandler) GetMessageStreams() *Iterator[*schema.StreamReader[*schema.Message]] {
	<-h.started

	return &Iterator[*schema.StreamReader[*schema.Message]]{ch: h.sMsgs}
}

func (h *cbHandler) onChatModelEnd(ctx context.Context,
	_ *callbacks.RunInfo, input *model.CallbackOutput) context.Context {

	h.sendMessage(input.Message)

	return ctx
}

func (h *cbHandler) onChatModelEndWithStreamOutput(ctx context.Context,
	_ *callbacks.RunInfo, input *schema.StreamReader[*model.CallbackOutput]) context.Context {

	c := func(output *model.CallbackOutput) (*schema.Message, error) {
		return output.Message, nil
	}
	s := schema.StreamReaderWithConvert(input, c)

	h.sendMessageStream(s)

	return ctx
}

func (h *cbHandler) onToolEnd(ctx context.Context,
	info *callbacks.RunInfo, input *tool.CallbackOutput) context.Context {

	toolCallID := compose.GetToolCallID(ctx)
	toolName := ""
	if info != nil {
		toolName = info.Name
	}
	msg := schema.ToolMessage(input.Response, toolCallID, schema.WithToolName(toolName))

	h.sendMessage(msg)

	return ctx
}

func (h *cbHandler) onToolEndWithStreamOutput(ctx context.Context,
	info *callbacks.RunInfo, input *schema.StreamReader[*tool.CallbackOutput]) context.Context {

	toolCallID := compose.GetToolCallID(ctx)
	toolName := ""
	if info != nil {
		toolName = info.Name
	}
	c := func(output *tool.CallbackOutput) (*schema.Message, error) {
		return schema.ToolMessage(output.Response, toolCallID, schema.WithToolName(toolName)), nil
	}
	s := schema.StreamReaderWithConvert(input, c)

	h.sendMessageStream(s)

	return ctx
}

func (h *cbHandler) onGraphError(ctx context.Context,
	_ *callbacks.RunInfo, err error) context.Context {

	if h.msgs != nil {
		h.msgs.Send(item[*schema.Message]{err: err})
	} else {
		h.sMsgs.Send(item[*schema.StreamReader[*schema.Message]]{err: err})
	}

	return ctx
}

func (h *cbHandler) onGraphEnd(ctx context.Context,
	_ *callbacks.RunInfo, _ callbacks.CallbackOutput) context.Context {

	h.msgs.Close()

	return ctx
}

func (h *cbHandler) onGraphEndWithStreamOutput(ctx context.Context,
	_ *callbacks.RunInfo, _ *schema.StreamReader[callbacks.CallbackOutput]) context.Context {

	h.sMsgs.Close()

	return ctx
}

func (h *cbHandler) onGraphStart(ctx context.Context,
	_ *callbacks.RunInfo, _ callbacks.CallbackInput) context.Context {

	h.msgs = internal.NewUnboundedChan[item[*schema.Message]]()

	close(h.started)

	return ctx
}

func (h *cbHandler) onGraphStartWithStreamInput(ctx context.Context, _ *callbacks.RunInfo,
	_ *schema.StreamReader[callbacks.CallbackInput]) context.Context {

	h.sMsgs = internal.NewUnboundedChan[item[*schema.StreamReader[*schema.Message]]]()

	close(h.started)

	return ctx
}

// sendMessage 根据当前执行模式发送消息到对应通道
func (h *cbHandler) sendMessage(msg *schema.Message) {
	if h.msgs != nil {
		h.msgs.Send(item[*schema.Message]{v: msg})
	} else {
		sMsg := schema.StreamReaderFromArray([]*schema.Message{msg})
		h.sMsgs.Send(item[*schema.StreamReader[*schema.Message]]{v: sMsg})
	}
}

// sendMessageStream 根据当前执行模式发送流式消息
func (h *cbHandler) sendMessageStream(sMsg *schema.StreamReader[*schema.Message]) {
	if h.sMsgs != nil {
		h.sMsgs.Send(item[*schema.StreamReader[*schema.Message]]{v: sMsg})
	} else {
		msg, err := schema.ConcatMessageStream(sMsg)

		if err != nil {
			h.msgs.Send(item[*schema.Message]{err: err})
		} else {
			h.msgs.Send(item[*schema.Message]{v: msg})
		}
	}
}
