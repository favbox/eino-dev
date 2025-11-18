/*
 * workflow_test.go - workflowAgent 工作流智能体功能测试
 *
 * 测试内容：
 *   - Sequential: 测试顺序执行模式，验证子智能体按顺序依次执行
 *   - Parallel: 测试并行执行模式，验证子智能体并发执行
 *   - Loop: 测试循环执行模式，验证子智能体循环执行和迭代次数限制
 *   - BreakLoop: 测试循环中断机制，验证 BreakLoopAction 提前终止循环
 *   - Exit: 测试退出动作，验证 Exit 动作提前终止工作流
 *   - Panic Recovery: 测试 panic 恢复机制
 *   - Resume: 测试工作流的中断恢复功能
 *
 * 测试策略：
 *   - 使用 mockAgent 模拟子智能体行为
 *   - 验证事件流的顺序和内容
 *   - 验证各种控制动作（Exit, BreakLoop, Interrupted）
 *   - 验证错误处理和 panic 恢复
 *
 * 核心验证点：
 *   - Sequential: 事件按子智能体顺序产生
 *   - Parallel: 所有子智能体的事件都产生（顺序不确定）
 *   - Loop: 事件数量等于迭代次数 × 子智能体数量
 *   - BreakLoop: 循环提前终止，Done 标志被设置
 *   - Exit: 后续子智能体不再执行
 *
 * Mock 设计：
 *   mockAgent 实现 Agent 接口，返回预定义的事件流，
 *   用于验证 workflowAgent 的执行逻辑和事件转发
 */

package adk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/schema"
)

// mockAgent 是用于测试 workflowAgent 的 Agent 接口简单实现。
// 返回预定义的事件流，用于验证工作流的执行逻辑
type mockAgent struct {
	name        string
	description string
	responses   []*AgentEvent // 预定义的相应事件列表
}

func (a *mockAgent) Name(ctx context.Context) string {
	return a.name
}

func (a *mockAgent) Description(ctx context.Context) string {
	return a.description
}

func (a *mockAgent) Run(ctx context.Context, input *AgentInput, options ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()

	go func() {
		defer generator.Close()

		for _, event := range a.responses {
			generator.Send(event)

			// 如果事件有退出操作，则停止发送事件
			if event.Action != nil && event.Action.Exit {
				break
			}
		}
	}()

	return iterator
}

func newMockAgent(name string, description string, responses []*AgentEvent) *mockAgent {
	return &mockAgent{
		name:        name,
		description: description,
		responses:   responses,
	}
}

// TestSequentialAgent 测试顺序工作流智能体。
// 验证子智能体按照配置顺序依次执行，事件按顺序产生。
//
// 测试流程：
//  1. 创建两个 mock agent (agent1, agent2)
//  2. 创建 SequentialAgent 包含这两个子智能体
//  3. 运行工作流并收集事件
//  4. 验证事件顺序：先收到 agent1 的事件，再收到 agent2 的事件
//  5. 验证事件内容正确
func TestSequentialAgent(t *testing.T) {
	ctx := context.Background()

	// 创建带预定义响应的 mock agent
	agent1 := newMockAgent("Agent1", "First agent", []*AgentEvent{
		{
			AgentName: "Agent1",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Response from Agent1", nil),
					Role:        schema.Assistant,
				},
			},
		},
	})

	agent2 := newMockAgent("Agent2", "Second agent", []*AgentEvent{
		{
			AgentName: "Agent2",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Response from Agent2", nil),
					Role:        schema.Assistant,
				},
			}},
	})

	// 创建顺序工作流智能体
	config := &SequentialAgentConfig{
		Name:        "SequentialTestAgent",
		Description: "Test sequential agent",
		SubAgents:   []Agent{agent1, agent2},
	}

	sequentialAgent, err := NewSequentialAgent(ctx, config)
	assert.NoError(t, err)
	assert.NotNil(t, sequentialAgent)

	assert.Equal(t, "Test sequential agent", sequentialAgent.Description(ctx))

	// 运行顺序工作流智能体
	input := &AgentInput{
		Messages: []Message{
			schema.UserMessage("Test input"),
		},
	}

	iterator := sequentialAgent.Run(ctx, input)
	assert.NotNil(t, iterator)

	// 第一个事件应该来自 agent1（顺序执行的第一个子智能体）
	event1, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event1)
	assert.Nil(t, event1.Err)
	assert.NotNil(t, event1.Output)
	assert.NotNil(t, event1.Output.MessageOutput)

	// 验证 agent1 的消息内容
	msg1 := event1.Output.MessageOutput.Message
	assert.NotNil(t, msg1)
	assert.Equal(t, "Response from Agent1", msg1.Content)

	// 第二个事件应该来自 agent2（顺序执行的第二个子智能体）
	event2, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event2)
	assert.Nil(t, event2.Err)
	assert.NotNil(t, event2.Output)
	assert.NotNil(t, event2.Output.MessageOutput)

	// 验证 agent2 的消息内容
	msg2 := event2.Output.MessageOutput.Message
	assert.NotNil(t, msg2)
	assert.Equal(t, "Response from Agent2", msg2.Content)

	// 验证没有更多事件（两个子智能体都已执行完成）
	_, ok = iterator.Next()
	assert.False(t, ok)
}

// TestSequentialAgentWithExit 测试顺序工作流智能体的退出机制。
// 验证当某个子智能体发出 Exit 动作时，工作流提前终止，后续子智能体不再执行。
//
// 测试流程：
//  1. 创建两个 mock agent，agent1 返回 Exit 动作
//  2. 创建 SequentialAgent 包含这两个子智能体
//  3. 运行工作流并收集事件
//  4. 验证只收到 agent1 的事件（带 Exit 动作）
//  5. 验证 agent2 没有执行（提前终止）
func TestSequentialAgentWithExit(t *testing.T) {
	ctx := context.Background()

	// 创建带 Exit 动作的 mock agent
	agent1 := newMockAgent("Agent1", "First agent", []*AgentEvent{
		{
			AgentName: "Agent1",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Response from Agent1", nil),
					Role:        schema.Assistant,
				},
			},
			Action: &AgentAction{
				Exit: true,
			},
		},
	})

	// agent2 不应该被执行，因为 agent1 已经发出 Exit 动作
	agent2 := newMockAgent("Agent2", "Second agent", []*AgentEvent{
		{
			AgentName: "Agent2",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Response from Agent2", nil),
					Role:        schema.Assistant,
				},
			},
		},
	})

	// 创建顺序工作流智能体
	config := &SequentialAgentConfig{
		Name:        "SequentialTestAgent",
		Description: "Test sequential agent",
		SubAgents:   []Agent{agent1, agent2},
	}

	sequentialAgent, err := NewSequentialAgent(ctx, config)
	assert.NoError(t, err)
	assert.NotNil(t, sequentialAgent)

	// 运行顺序工作流智能体
	input := &AgentInput{
		Messages: []Message{
			schema.UserMessage("Test input"),
		},
	}

	iterator := sequentialAgent.Run(ctx, input)
	assert.NotNil(t, iterator)

	// 第一个事件来自 agent1，包含 Exit 动作
	event1, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event1)
	assert.Nil(t, event1.Err)
	assert.NotNil(t, event1.Output)
	assert.NotNil(t, event1.Output.MessageOutput)
	assert.NotNil(t, event1.Action)
	assert.True(t, event1.Action.Exit) // 验证 Exit 动作存在

	// 验证没有更多事件（Exit 动作导致提前终止，agent2 未执行）
	_, ok = iterator.Next()
	assert.False(t, ok)
}

// TestParallelAgent 测试并行工作流智能体。
// 验证所有子智能体并发执行，所有事件都能收到（但顺序不确定）。
//
// 测试流程：
//  1. 创建两个 mock agent (agent1, agent2)
//  2. 创建 ParallelAgent 包含这两个子智能体
//  3. 运行工作流并收集所有事件
//  4. 验证收到 2 个事件（来自两个子智能体）
//  5. 验证每个事件的内容正确（顺序可能不同）
func TestParallelAgent(t *testing.T) {
	ctx := context.Background()

	// 创建带预定义响应的 mock agent
	agent1 := newMockAgent("Agent1", "First agent", []*AgentEvent{
		{
			AgentName: "Agent1",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Response from Agent1", nil),
					Role:        schema.Assistant,
				},
			},
		},
	})

	agent2 := newMockAgent("Agent2", "Second agent", []*AgentEvent{
		{
			AgentName: "Agent2",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Response from Agent2", nil),
					Role:        schema.Assistant,
				},
			},
		},
	})

	// 创建并行工作流智能体
	config := &ParallelAgentConfig{
		Name:        "ParallelTestAgent",
		Description: "Test parallel agent",
		SubAgents:   []Agent{agent1, agent2},
	}

	parallelAgent, err := NewParallelAgent(ctx, config)
	assert.NoError(t, err)
	assert.NotNil(t, parallelAgent)

	// 运行并行工作流智能体
	input := AgentInput{
		Messages: []Message{
			schema.UserMessage("Test input"),
		},
	}

	iterator := parallelAgent.Run(ctx, &input)
	assert.NotNil(t, iterator)

	// 收集所有事件（并行执行，事件顺序不确定）
	var events []*AgentEvent
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		events = append(events, event)
	}

	// 应该收到 2 个事件，每个子智能体各一个
	assert.Equal(t, 2, len(events))

	// 验证事件内容（由于并行执行，顺序不确定）
	for _, event := range events {
		assert.Nil(t, event.Err)
		assert.NotNil(t, event.Output)
		assert.NotNil(t, event.Output.MessageOutput)

		msg := event.Output.MessageOutput.Message
		assert.NotNil(t, msg)
		assert.NoError(t, err)

		// 根据事件的来源智能体验证消息内容
		if event.AgentName == "Agent1" {
			assert.Equal(t, "Response from Agent1", msg.Content)
		} else if event.AgentName == "Agent2" {
			assert.Equal(t, "Response from Agent2", msg.Content)
		} else {
			t.Fatalf("Unexpected source agent name: %s", event.AgentName)
		}
	}
}

// TestLoopAgent 测试循环工作流智能体。
// 验证子智能体按照指定的迭代次数循环执行。
//
// 测试流程：
//  1. 创建一个 mock agent
//  2. 创建 LoopAgent，设置 MaxIterations = 3
//  3. 运行工作流并收集所有事件
//  4. 验证收到 3 个事件（对应 3 次迭代）
//  5. 验证每个事件的内容一致
func TestLoopAgent(t *testing.T) {
	ctx := context.Background()

	// 创建将被多次调用的 mock agent
	agent := newMockAgent("LoopAgent", "Loop agent", []*AgentEvent{
		{
			AgentName: "LoopAgent",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Loop iteration", nil),
					Role:        schema.Assistant,
				},
			},
		},
	})

	// 创建循环工作流智能体，最大迭代次数设为 3
	config := &LoopAgentConfig{
		Name:        "LoopTestAgent",
		Description: "Test loop agent",
		SubAgents:   []Agent{agent},

		MaxIterations: 3,
	}

	loopAgent, err := NewLoopAgent(ctx, config)
	assert.NoError(t, err)
	assert.NotNil(t, loopAgent)

	// 运行循环工作流智能体
	input := &AgentInput{
		Messages: []Message{
			schema.UserMessage("Test input"),
		},
	}

	iterator := loopAgent.Run(ctx, input)
	assert.NotNil(t, iterator)

	// 收集所有事件
	var events []*AgentEvent
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		events = append(events, event)
	}

	// 应该收到 3 个事件（每次迭代一个）
	assert.Equal(t, 3, len(events))

	// 验证所有事件的内容
	for _, event := range events {
		assert.Nil(t, event.Err)
		assert.NotNil(t, event.Output)
		assert.NotNil(t, event.Output.MessageOutput)

		msg := event.Output.MessageOutput.Message
		assert.NotNil(t, msg)
		assert.Equal(t, "Loop iteration", msg.Content)
	}
}

// TestLoopAgentWithBreakLoop 测试循环工作流智能体的循环中断机制。
// 验证 BreakLoopAction 能够提前终止循环，即使未达到 MaxIterations。
//
// 测试流程：
//  1. 创建一个 mock agent，返回 BreakLoopAction
//  2. 创建 LoopAgent，MaxIterations = 3
//  3. 运行工作流并收集事件
//  4. 验证只收到 1 个事件（第一次迭代后就中断）
//  5. 验证 BreakLoopAction 的 Done 标志被设置为 true
//  6. 验证 CurrentIterations = 0（在第 0 次迭代时中断）
func TestLoopAgentWithBreakLoop(t *testing.T) {
	ctx := context.Background()

	// 创建会在第一次迭代后中断循环的 mock agent
	agent := newMockAgent("LoopAgent", "Loop agent", []*AgentEvent{
		{
			AgentName: "LoopAgent",
			Output: &AgentOutput{
				MessageOutput: &MessageVariant{
					IsStreaming: false,
					Message:     schema.AssistantMessage("Loop iteration with break loop", nil),
					Role:        schema.Assistant,
				},
			},
			Action: NewBreakLoopAction("LoopAgent"),
		},
	})

	// 创建循环工作流智能体，MaxIterations = 3（但会因 BreakLoop 提前终止）
	config := &LoopAgentConfig{
		Name:          "LoopTestAgent",
		Description:   "Test loop agent",
		SubAgents:     []Agent{agent},
		MaxIterations: 3,
	}

	loopAgent, err := NewLoopAgent(ctx, config)
	assert.NoError(t, err)
	assert.NotNil(t, loopAgent)

	// 运行循环工作流智能体
	input := &AgentInput{
		Messages: []Message{
			schema.UserMessage("Test input"),
		},
	}

	iterator := loopAgent.Run(ctx, input)
	assert.NotNil(t, iterator)

	// 收集所有事件
	var events []*AgentEvent
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		events = append(events, event)
	}

	// 应该只有 1 个事件（BreakLoopAction 导致提前终止）
	assert.Equal(t, 1, len(events))

	// 验证事件内容
	event := events[0]
	assert.Nil(t, event.Err)
	assert.NotNil(t, event.Output)
	assert.NotNil(t, event.Output.MessageOutput)
	assert.NotNil(t, event.Action)
	assert.NotNil(t, event.Action.BreakLoop)                     // 验证包含 BreakLoop 动作
	assert.True(t, event.Action.BreakLoop.Done)                  // 验证 Done 标志为 true（已被处理）
	assert.Equal(t, "LoopAgent", event.Action.BreakLoop.From)    // 验证动作来源
	assert.Equal(t, 0, event.Action.BreakLoop.CurrentIterations) // 验证在第 0 次迭代时中断

	msg := event.Output.MessageOutput.Message
	assert.NotNil(t, msg)
	assert.Equal(t, "Loop iteration with break loop", msg.Content)
}

// TestWorkflowAgentPanicRecovery 测试工作流智能体的 panic 恢复机制。
// 验证当子智能体发生 panic 时，工作流能够捕获并转换为错误事件。
func TestWorkflowAgentPanicRecovery(t *testing.T) {
	ctx := context.Background()

	// Create a panic agent that panics in Run method
	panicAgent := &panicMockAgent{
		mockAgent: mockAgent{
			name:        "PanicAgent",
			description: "Agent that panics",
			responses:   []*AgentEvent{},
		},
	}

	// Create a sequential agent with the panic agent
	config := &SequentialAgentConfig{
		Name:        "PanicTestAgent",
		Description: "Test agent with panic",
		SubAgents:   []Agent{panicAgent},
	}

	sequentialAgent, err := NewSequentialAgent(ctx, config)
	assert.NoError(t, err)

	// Run the agent and expect panic recovery
	input := &AgentInput{
		Messages: []Message{
			schema.UserMessage("Test input"),
		},
	}

	iterator := sequentialAgent.Run(ctx, input)
	assert.NotNil(t, iterator)

	// Should receive an error event due to panic recovery
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event)
	assert.NotNil(t, event.Err)
	assert.Contains(t, event.Err.Error(), "panic")

	// No more events
	_, ok = iterator.Next()
	assert.False(t, ok)
}

// 添加这些新的模拟智能体类型，使其能够正确地引发恐慌
type panicMockAgent struct {
	mockAgent
}

func (a *panicMockAgent) Run(ctx context.Context, input *AgentInput, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	panic("test panic in agent")
}

type panicResumableMockAgent struct {
	mockAgent
}

func (a *panicResumableMockAgent) Resume(ctx context.Context, info *ResumeInfo, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	panic("test panic in resume")
}

// TestWorkflowAgentUnsupportedMode tests unsupported workflow mode error (lines 65-71)
func TestWorkflowAgentUnsupportedMode(t *testing.T) {
	ctx := context.Background()

	// Create a workflow agent with unsupported mode
	agent := &workflowAgent{
		name:        "UnsupportedModeAgent",
		description: "Agent with unsupported mode",
		subAgents:   []*flowAgent{},
		mode:        workflowAgentMode(999), // Invalid mode
	}

	// Run the agent and expect error
	input := &AgentInput{
		Messages: []Message{
			schema.UserMessage("Test input"),
		},
	}

	iterator := agent.Run(ctx, input)
	assert.NotNil(t, iterator)

	// Should receive an error event due to unsupported mode
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event)
	assert.NotNil(t, event.Err)
	assert.Contains(t, event.Err.Error(), "unsupported workflow agent mode")

	// No more events
	_, ok = iterator.Next()
	assert.False(t, ok)
}

// TestWorkflowAgentResumePanicRecovery tests panic recovery in Resume method (lines 108-115)
func TestWorkflowAgentResumePanicRecovery(t *testing.T) {
	ctx := context.Background()

	// Create a mock resumable agent that panics on Resume
	panicAgent := &mockResumableAgent{
		mockAgent: mockAgent{
			name:        "PanicResumeAgent",
			description: "Agent that panics on resume",
			responses:   []*AgentEvent{},
		},
	}

	// Create a sequential agent with the panic agent
	config := &SequentialAgentConfig{
		Name:        "ResumeTestAgent",
		Description: "Test agent for resume panic",
		SubAgents:   []Agent{panicAgent},
	}

	sequentialAgent, err := NewSequentialAgent(ctx, config)
	assert.NoError(t, err)

	// Initialize context with run context - this is the key fix
	ctx = ctxWithNewRunCtx(ctx)

	// Create valid resume info
	resumeInfo := &ResumeInfo{
		EnableStreaming: false,
		InterruptInfo: &InterruptInfo{
			Data: &WorkflowInterruptInfo{
				OrigInput: &AgentInput{
					Messages: []Message{schema.UserMessage("test")},
				},
				SequentialInterruptIndex: 0,
				SequentialInterruptInfo: &InterruptInfo{
					Data: "some interrupt data",
				},
				LoopIterations: 0,
			},
		},
	}

	// Call Resume and expect panic recovery
	iterator := sequentialAgent.(ResumableAgent).Resume(ctx, resumeInfo)
	assert.NotNil(t, iterator)

	// Should receive an error event due to panic recovery
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event)
	assert.NotNil(t, event.Err)
	assert.Contains(t, event.Err.Error(), "panic")

	// No more events
	_, ok = iterator.Next()
	assert.False(t, ok)
}

// mockResumableAgent extends mockAgent to implement ResumableAgent interface
type mockResumableAgent struct {
	mockAgent
}

func (a *mockResumableAgent) Resume(ctx context.Context, info *ResumeInfo, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	panic("test panic in resume")
}

// TestWorkflowAgentResumeInvalidDataType tests invalid data type in Resume method
func TestWorkflowAgentResumeInvalidDataType(t *testing.T) {
	ctx := context.Background()

	// Create a workflow agent
	agent := &workflowAgent{
		name:        "InvalidDataTestAgent",
		description: "Agent for invalid data test",
		subAgents:   []*flowAgent{},
		mode:        workflowAgentModeSequential,
	}

	// Create resume info with invalid data type
	resumeInfo := &ResumeInfo{
		EnableStreaming: false,
		InterruptInfo: &InterruptInfo{
			Data: "invalid data type", // Should be *WorkflowInterruptInfo
		},
	}

	// Call Resume and expect type assertion error
	iterator := agent.Resume(ctx, resumeInfo)
	assert.NotNil(t, iterator)

	// Should receive an error event due to type assertion failure
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event)
	assert.NotNil(t, event.Err)
	assert.Contains(t, event.Err.Error(), "type of InterruptInfo.Data is expected to")
	assert.Contains(t, event.Err.Error(), "actual: string")

	// No more events
	_, ok = iterator.Next()
	assert.False(t, ok)
}
