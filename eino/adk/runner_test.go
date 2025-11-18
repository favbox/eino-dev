/*
 * runner_test.go - Runner 运行器功能测试
 *
 * 测试内容：
 *   - NewRunner: 测试运行器的创建
 *   - Run: 测试完整的智能体运行流程（流式和非流式）
 *   - Query: 测试简化查询接口（流式和非流式）
 *
 * 测试策略：
 *   - 使用 mockRunnerAgent 模拟智能体行为
 *   - 验证参数传递的正确性（messages, enableStreaming）
 *   - 验证事件流的正确输出
 *   - 验证迭代器的正确关闭
 *
 * 核心验证点：
 *   - Runner 正确包装 Agent 并传递参数
 *   - 流式模式标志正确传递到 Agent
 *   - Query 方法正确将字符串包装为 UserMessage
 *   - 事件流从 Agent 正确转发到调用方
 *
 * Mock 设计：
 *   mockRunnerAgent 实现 Agent 接口，记录调用参数并返回预定义的事件流，
 *   用于验证 Runner 的参数传递和事件转发逻辑
 */

package adk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/schema"
)

// mockRunnerAgent 是用于测试 Runner 的 Agent 接口简单实现。
// 记录调用参数并返回预定义的事件流，用于验证 Runner 的行为
type mockRunnerAgent struct {
	name        string
	description string
	responses   []*AgentEvent // 预定义的响应事件列表

	// 记录调用信息，用于验证参数传递
	callCount       int         // Run 方法被调用的次数
	lastInput       *AgentInput // 最后一次调用时的输入参数
	enableStreaming bool        // 最后一次调用时的流式模式标志
}

func (a *mockRunnerAgent) Name(_ context.Context) string {
	return a.name
}

func (a *mockRunnerAgent) Description(_ context.Context) string {
	return a.description
}

func (a *mockRunnerAgent) Run(_ context.Context, input *AgentInput, _ ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	// 记录调用详情，用于测试验证
	a.callCount++
	a.lastInput = input
	a.enableStreaming = input.EnableStreaming

	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()

	go func() {
		defer generator.Close()

		// 逐个发送预定义的响应事件
		for _, event := range a.responses {
			generator.Send(event)

			// 如果事件包含 Exit 动作，停止发送后续事件
			if event.Action != nil && event.Action.Exit {
				break
			}
		}
	}()

	return iterator
}

func newMockRunnerAgent(name, description string, responses []*AgentEvent) *mockRunnerAgent {
	return &mockRunnerAgent{
		name:        name,
		description: description,
		responses:   responses,
	}
}

// TestNewRunner 测试 Runner 的创建。
// 验证 NewRunner 能够成功创建运行器实例
func TestNewRunner(t *testing.T) {
	ctx := context.Background()
	config := RunnerConfig{}

	runner := NewRunner(ctx, config)

	// 验证返回非 nil 的运行器实例
	assert.NotNil(t, runner)
}

// TestRunner_Run 测试 Runner 的非流式运行。
// 验证 Run 方法正确调用 Agent，传递参数并返回事件流。
//
// 测试流程：
//  1. 创建带预定义响应的 mock agent
//  2. 创建 Runner（非流式模式）
//  3. 调用 Run 方法并传入消息
//  4. 验证 Agent.Run 被正确调用，参数正确传递
//  5. 验证事件流返回预期的响应
//  6. 验证迭代器正确关闭
func TestRunner_Run(t *testing.T) {
	ctx := context.Background()

	// 创建带预定义响应的 mock agent
	mockAgent_ := newMockRunnerAgent("TestAgent", "Test agent for Runner", []*AgentEvent{
		{
			AgentName: "TestAgent",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Response from test agent", nil),
					Role:        schema.Assistant,
				},
			}},
	})

	// 创建 Runner（默认非流式模式）
	runner := NewRunner(ctx, RunnerConfig{Agent: mockAgent_})

	// 创建测试消息
	msgs := []Message{
		schema.UserMessage("Hello, agent!"),
	}

	// 测试 Run 方法（非流式）
	iterator := runner.Run(ctx, msgs)

	// 验证 Agent 的 Run 方法被正确调用，参数传递正确
	assert.Equal(t, 1, mockAgent_.callCount)             // 调用次数：1 次
	assert.Equal(t, msgs, mockAgent_.lastInput.Messages) // 消息参数正确
	assert.False(t, mockAgent_.enableStreaming)          // 非流式模式

	// 验证可以从迭代器获取预期的响应
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.Equal(t, "TestAgent", event.AgentName)
	assert.NotNil(t, event.Output)
	assert.NotNil(t, event.Output.MessageOutput)
	assert.NotNil(t, event.Output.MessageOutput.Message)
	assert.Equal(t, "Response from test agent", event.Output.MessageOutput.Message.Content)

	// 验证迭代器已关闭（没有更多事件）
	_, ok = iterator.Next()
	assert.False(t, ok)
}

// TestRunner_Run_WithStreaming 测试 Runner 的流式运行。
// 验证 Run 方法在流式模式下正确工作，流式标志正确传递到 Agent。
//
// 测试流程：
//  1. 创建带流式响应的 mock agent
//  2. 创建 Runner（启用流式模式）
//  3. 调用 Run 方法并传入消息
//  4. 验证 enableStreaming 标志正确传递
//  5. 验证事件流返回流式响应
//  6. 验证迭代器正确关闭
func TestRunner_Run_WithStreaming(t *testing.T) {
	ctx := context.Background()

	// 创建带流式响应的 mock agent
	mockAgent_ := newMockRunnerAgent("TestAgent", "Test agent for Runner", []*AgentEvent{
		{
			AgentName: "TestAgent",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming:   true,
					Message:       nil,
					MessageStream: schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("Streaming response", nil)}),
					Role:          schema.Assistant,
				},
			}},
	})

	// 创建 Runner（启用流式模式）
	runner := NewRunner(ctx, RunnerConfig{EnableStreaming: true, Agent: mockAgent_})

	// 创建测试消息
	msgs := []Message{
		schema.UserMessage("Hello, agent!"),
	}

	// 测试 Run 方法（流式模式）
	iterator := runner.Run(ctx, msgs)

	// 验证 Agent 的 Run 方法被正确调用，参数传递正确
	assert.Equal(t, 1, mockAgent_.callCount)             // 调用次数：1 次
	assert.Equal(t, msgs, mockAgent_.lastInput.Messages) // 消息参数正确
	assert.True(t, mockAgent_.enableStreaming)           // 流式模式已启用

	// 验证可以从迭代器获取预期的流式响应
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.Equal(t, "TestAgent", event.AgentName)
	assert.NotNil(t, event.Output)
	assert.NotNil(t, event.Output.MessageOutput)
	assert.True(t, event.Output.MessageOutput.IsStreaming) // 验证是流式输出

	// 验证迭代器已关闭（没有更多事件）
	_, ok = iterator.Next()
	assert.False(t, ok)
}

// TestRunner_Query 测试 Runner 的简化查询接口（非流式）。
// 验证 Query 方法正确将字符串包装为 UserMessage 并调用 Run。
//
// 测试流程：
//  1. 创建带预定义响应的 mock agent
//  2. 创建 Runner（非流式模式）
//  3. 调用 Query 方法传入查询字符串
//  4. 验证字符串被正确包装为 UserMessage
//  5. 验证事件流返回预期响应
//  6. 验证迭代器正确关闭
//
// Query 是 Run 的便捷封装：
//
//	Query(ctx, "hello") 等价于 Run(ctx, []Message{UserMessage("hello")})
func TestRunner_Query(t *testing.T) {
	ctx := context.Background()

	// 创建带预定义响应的 mock agent
	mockAgent_ := newMockRunnerAgent("TestAgent", "Test agent for Runner", []*AgentEvent{
		{
			AgentName: "TestAgent",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Response to query", nil),
					Role:        schema.Assistant,
				},
			}},
	})

	// 创建 Runner（默认非流式模式）
	runner := NewRunner(ctx, RunnerConfig{Agent: mockAgent_})

	// 测试 Query 方法（便捷接口）
	iterator := runner.Query(ctx, "Test query")

	// 验证 Agent 的 Run 方法被正确调用
	assert.Equal(t, 1, mockAgent_.callCount)                                // 调用次数：1 次
	assert.Equal(t, 1, len(mockAgent_.lastInput.Messages))                  // 消息数量：1 条
	assert.Equal(t, "Test query", mockAgent_.lastInput.Messages[0].Content) // 查询字符串被包装为 UserMessage
	assert.False(t, mockAgent_.enableStreaming)                             // 非流式模式

	// 验证可以从迭代器获取预期的响应
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.Equal(t, "TestAgent", event.AgentName)
	assert.NotNil(t, event.Output)
	assert.NotNil(t, event.Output.MessageOutput)
	assert.NotNil(t, event.Output.MessageOutput.Message)
	assert.Equal(t, "Response to query", event.Output.MessageOutput.Message.Content)

	// 验证迭代器已关闭（没有更多事件）
	_, ok = iterator.Next()
	assert.False(t, ok)
}

// TestRunner_Query_WithStreaming 测试 Runner 的简化查询接口（流式）。
// 验证 Query 方法在流式模式下正确工作。
//
// 测试流程：
//  1. 创建带流式响应的 mock agent
//  2. 创建 Runner（启用流式模式）
//  3. 调用 Query 方法传入查询字符串
//  4. 验证字符串被正确包装为 UserMessage
//  5. 验证流式标志正确传递
//  6. 验证事件流返回流式响应
//  7. 验证迭代器正确关闭
func TestRunner_Query_WithStreaming(t *testing.T) {
	ctx := context.Background()

	// 创建带流式响应的 mock agent
	mockAgent_ := newMockRunnerAgent("TestAgent", "Test agent for Runner", []*AgentEvent{
		{
			AgentName: "TestAgent",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming:   true,
					Message:       nil,
					MessageStream: schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("Streaming query response", nil)}),
					Role:          schema.Assistant,
				},
			}},
	})

	// 创建 Runner（启用流式模式）
	runner := NewRunner(ctx, RunnerConfig{EnableStreaming: true, Agent: mockAgent_})

	// 测试 Query 方法（流式模式）
	iterator := runner.Query(ctx, "Test query")

	// 验证 Agent 的 Run 方法被正确调用
	assert.Equal(t, 1, mockAgent_.callCount)                                // 调用次数：1 次
	assert.Equal(t, 1, len(mockAgent_.lastInput.Messages))                  // 消息数量：1 条
	assert.Equal(t, "Test query", mockAgent_.lastInput.Messages[0].Content) // 查询字符串被包装为 UserMessage
	assert.True(t, mockAgent_.enableStreaming)                              // 流式模式已启用

	// 验证可以从迭代器获取预期的流式响应
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.Equal(t, "TestAgent", event.AgentName)
	assert.NotNil(t, event.Output)
	assert.NotNil(t, event.Output.MessageOutput)
	assert.True(t, event.Output.MessageOutput.IsStreaming) // 验证是流式输出

	// 验证迭代器已关闭（没有更多事件）
	_, ok = iterator.Next()
	assert.False(t, ok)
}
