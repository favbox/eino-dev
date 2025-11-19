package planexecute

import (
	"context"

	"github.com/favbox/eino/adk"
)

// outputSessionKVsAgent 是一个包装智能体，用于在运行结束时输出会话键值对。
// 它会转发原始智能体的所有事件，并在最后添加一个包含所有会话值的事件。
type outputSessionKVsAgent struct {
	adk.Agent
}

// Run 执行包装的智能体，并在结束时输出会话键值对。
func (o *outputSessionKVsAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {

	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	iterator_ := o.Agent.Run(ctx, input, options...)
	go func() {
		defer generator.Close()
		// 转发原始智能体的所有事件
		for {
			event, ok := iterator_.Next()
			if !ok {
				break
			}
			generator.Send(event)
		}

		// 获取所有会话键值对
		kvs := adk.GetSessionValues(ctx)

		// 发送包含会话键值对的事件
		event := &adk.AgentEvent{
			Output: &adk.AgentOutput{CustomizedOutput: kvs},
		}
		generator.Send(event)
	}()

	return iterator
}

// agentOutputSessionKVs 将给定的智能体包装为一个会输出会话键值对的智能体。
// 包装后的智能体会在运行结束时自动发送一个包含所有会话值的事件。
// 这在测试中特别有用，可以验证会话状态的变化。
func agentOutputSessionKVs(ctx context.Context, agent adk.Agent) (adk.Agent, error) {
	return &outputSessionKVsAgent{Agent: agent}, nil
}
