/*
 * utils.go - ADK 工具函数和辅助类型
 *
 * 核心组件：
 *   - AsyncIterator/AsyncGenerator: 异步迭代器和生成器，实现事件流的生产消费模型
 *   - copyAgentEvent: 事件复制函数，确保事件流的独占性
 *   - getMessageFromWrappedEvent: 从包装的事件中提取消息，支持流式消息合并
 *
 * 设计特点：
 *   - 无界通道：使用 UnboundedChan 避免阻塞
 *   - 流式复制：支持 MessageStream 的安全复制和消费
 *   - 自动关闭：通过 SetAutomaticClose 确保资源释放
 *   - 并发安全：通过互斥锁保护流式消息的合并
 *
 * 与其他文件关系：
 *   - 为所有 ADK 组件提供基础的异步迭代能力
 *   - 为 flow.go 提供事件复制和消息提取功能
 *   - 为 interface.go 中的 Agent.Run 提供返回值类型
 */

package adk

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/favbox/eino/internal"
	"github.com/favbox/eino/schema"
)

// AsyncIterator 是异步迭代器，用于从事件流中读取数据。
// 基于无界通道实现，不会阻塞生产者。
type AsyncIterator[T any] struct {
	ch *internal.UnboundedChan[T]
}

// Next 从迭代器中获取下一个值。
// 返回值和是否有更多数据的标识，当通道关闭且无数据时返回 false。
func (ai *AsyncIterator[T]) Next() (T, bool) {
	return ai.ch.Receive()
}

// AsyncGenerator 是异步生成器，用于向事件流中发送数据。
// 与 AsyncIterator 配对使用，通过共享的无界通道通信。
type AsyncGenerator[T any] struct {
	ch *internal.UnboundedChan[T]
}

// Send 向生成器发送一个值。
// 不会阻塞，值会被缓存到无界通道中。
func (ag *AsyncGenerator[T]) Send(v T) {
	ag.ch.Send(v)
}

// Close 关闭生成器。
// 关闭后不能再发送数据，迭代器会在消费完所有数据后返回 false。
func (ag *AsyncGenerator[T]) Close() {
	ag.ch.Close()
}

// NewAsyncIteratorPair 创建配对的异步迭代器和生成器。
// 返回的迭代器和生成器共享同一个无界通道，用于实现生产者-消费者模式。
func NewAsyncIteratorPair[T any]() (*AsyncIterator[T], *AsyncGenerator[T]) {
	ch := internal.NewUnboundedChan[T]()
	return &AsyncIterator[T]{ch}, &AsyncGenerator[T]{ch}
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	res := make(map[K]V, len(m))
	for k, v := range m {
		res[k] = v
	}
	return res
}

func concatInstructions(instructions ...string) string {
	var sb strings.Builder
	sb.WriteString(instructions[0])
	for i := 1; i < len(instructions); i++ {
		sb.WriteString("\n\n")
		sb.WriteString(instructions[i])
	}

	return sb.String()
}

// GenTransferMessages 生成转移到指定智能体的消息对。
// 返回 assistant 消息（包含工具调用）和 tool 消息（包含转移结果）。
// 用于在多智能体系统中实现智能体间的转移。
func GenTransferMessages(_ context.Context, destAgentName string) (Message, Message) {
	toolCallID := uuid.NewString()
	tooCall := schema.ToolCall{ID: toolCallID, Function: schema.FunctionCall{Name: TransferToAgentToolName, Arguments: destAgentName}}
	assistantMessage := schema.AssistantMessage("", []schema.ToolCall{tooCall})
	toolMessage := schema.ToolMessage(transferToAgentToolOutput(destAgentName), toolCallID, schema.WithToolName(TransferToAgentToolName))
	return assistantMessage, toolMessage
}

// setAutomaticClose 为事件的消息流设置自动关闭。
// 确保即使流未被完全消费，也能在适当时机自动释放资源。
func setAutomaticClose(e *AgentEvent) {
	if e.Output == nil || e.Output.MessageOutput == nil || !e.Output.MessageOutput.IsStreaming {
		return
	}

	e.Output.MessageOutput.MessageStream.SetAutomaticClose()
}

// getMessageFromWrappedEvent 从包装的事件中提取消息。
// 流式消息会被合并为单条消息并缓存，避免重复读取。
// 使用互斥锁保护并发访问，确保消息只被合并一次。
func getMessageFromWrappedEvent(e *agentEventWrapper) (Message, error) {
	if e.AgentEvent.Output == nil || e.AgentEvent.Output.MessageOutput == nil {
		return nil, nil
	}

	if !e.AgentEvent.Output.MessageOutput.IsStreaming {
		return e.AgentEvent.Output.MessageOutput.Message, nil
	}

	if e.concatenatedMessage != nil {
		return e.concatenatedMessage, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.concatenatedMessage != nil {
		return e.concatenatedMessage, nil
	}

	var (
		msgs []Message
		s    = e.AgentEvent.Output.MessageOutput.MessageStream
	)

	defer s.Close()
	for {
		msg, err := s.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		msgs = append(msgs, msg)
	}

	if len(msgs) == 0 {
		return nil, errors.New("no messages in MessageVariant.MessageStream")
	}

	if len(msgs) == 1 {
		e.concatenatedMessage = msgs[0]
	} else {
		var err error
		e.concatenatedMessage, err = schema.ConcatMessages(msgs)
		if err != nil {
			return nil, err
		}
	}

	return e.concatenatedMessage, nil
}

// copyAgentEvent 复制 AgentEvent，确保事件流的独占性。
// 流式消息的 MessageStream 会被复制为两个独立的流，RunPath 会被深拷贝。
// 复制后的事件可以安全地修改字段、扩展 RunPath 和接收 MessageStream。
// 注意：Message 本身和 MessageStream 的数据块不会被复制，不应修改它们。
// CustomizedOutput 和 CustomizedAction 不会被复制。
func copyAgentEvent(ae *AgentEvent) *AgentEvent {
	rp := make([]RunStep, len(ae.RunPath))
	copy(rp, ae.RunPath)

	copied := &AgentEvent{
		AgentName: ae.AgentName,
		RunPath:   rp,
		Action:    ae.Action,
		Err:       ae.Err,
	}

	if ae.Output == nil {
		return copied
	}

	copied.Output = &AgentOutput{
		CustomizedOutput: ae.Output.CustomizedOutput,
	}

	mv := ae.Output.MessageOutput
	if mv == nil {
		return copied
	}

	copied.Output.MessageOutput = &MessageVariant{
		IsStreaming: mv.IsStreaming,
		Role:        mv.Role,
		ToolName:    mv.ToolName,
	}
	if mv.IsStreaming {
		sts := ae.Output.MessageOutput.MessageStream.Copy(2)
		mv.MessageStream = sts[0]
		copied.Output.MessageOutput.MessageStream = sts[1]
	} else {
		copied.Output.MessageOutput.Message = mv.Message
	}

	return copied
}

// GetMessage 从事件中提取完整消息。
// 流式消息会被合并为单条消息，同时复制事件以保持原事件的流可用。
// 返回提取的消息、更新后的事件和可能的错误。
func GetMessage(e *AgentEvent) (Message, *AgentEvent, error) {
	if e.Output == nil || e.Output.MessageOutput == nil {
		return nil, e, nil
	}

	msgOutput := e.Output.MessageOutput
	if msgOutput.IsStreaming {
		ss := msgOutput.MessageStream.Copy(2)
		e.Output.MessageOutput.MessageStream = ss[0]

		msg, err := schema.ConcatMessageStream(ss[1])

		return msg, e, err
	}

	return msgOutput.Message, e, nil
}

// genErrorIter 创建只包含一个错误事件的迭代器。
// 用于在遇到错误时快速返回错误迭代器。
func genErrorIter(err error) *AsyncIterator[*AgentEvent] {
	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()
	generator.Send(&AgentEvent{Err: err})
	generator.Close()
	return iterator
}
