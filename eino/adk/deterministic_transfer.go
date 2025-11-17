/*
 * deterministic_transfer.go - 确定性智能体转移实现
 *
 * 核心组件：
 *   - agentWithDeterministicTransferTo: 包装智能体，自动添加确定性转移动作
 *   - resumableAgentWithDeterministicTransferTo: 支持恢复的确定性转移智能体
 *
 * 设计特点：
 *   - 装饰器模式: 包装现有智能体，增加确定性转移能力
 *   - 自动转移: 智能体完成后自动生成转移消息和动作
 *   - 恢复支持: 支持可恢复智能体的中断和恢复
 *   - 中断感知: 检测中断事件，中断时不生成转移动作
 *
 * 工作流程：
 *   1. 运行被包装的智能体
 *   2. 转发所有事件到调用方
 *   3. 检测是否发生中断
 *   4. 如果未中断，自动生成转移消息和转移动作
 *   5. 按配置的目标智能体列表依次生成转移事件
 */

package adk

import (
	"context"
	"runtime/debug"

	"github.com/favbox/eino/internal/safe"
	"github.com/favbox/eino/schema"
)

// AgentWithDeterministicTransferTo 创建确定性转移智能体，包装现有智能体并自动添加转移能力。
// 智能体完成后会自动生成转移消息和动作，将控制权转移到配置的目标智能体。
// 如果智能体支持恢复（实现 ResumableAgent），则返回支持恢复的包装器。
func AgentWithDeterministicTransferTo(_ context.Context, config *DeterministicTransferConfig) Agent {
	if ra, ok := config.Agent.(ResumableAgent); ok {
		return &resumableAgentWithDeterministicTransferTo{
			agent:        ra,
			toAgentNames: config.ToAgentNames,
		}
	}
	return &agentWithDeterministicTransferTo{
		agent:        config.Agent,
		toAgentNames: config.ToAgentNames,
	}
}

// agentWithDeterministicTransferTo 包装智能体，自动添加确定性转移动作。
// 智能体完成后会按配置的目标智能体列表依次生成转移事件。
type agentWithDeterministicTransferTo struct {
	agent        Agent    // 被包装的智能体
	toAgentNames []string // 目标智能体名称列表
}

// Description 返回被包装智能体的描述
func (a *agentWithDeterministicTransferTo) Description(ctx context.Context) string {
	return a.agent.Description(ctx)
}

// Name 返回被包装智能体的名称
func (a *agentWithDeterministicTransferTo) Name(ctx context.Context) string {
	return a.agent.Name(ctx)
}

// Run 运行被包装的智能体，并在完成后自动添加转移动作。
// 转发所有智能体事件，智能体完成且未中断时，生成转移消息和转移动作。
func (a *agentWithDeterministicTransferTo) Run(ctx context.Context,
	input *AgentInput, options ...AgentRunOption) *AsyncIterator[*AgentEvent] {

	// 如果是 flowAgent，清除运行上下文避免冲突
	if _, ok := a.agent.(*flowAgent); ok {
		ctx = ClearRunCtx(ctx)
	}

	aIter := a.agent.Run(ctx, input, options...)

	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()
	go appendTransferAction(ctx, aIter, generator, a.toAgentNames)

	return iterator
}

// resumableAgentWithDeterministicTransferTo 包装可恢复智能体，支持中断恢复和确定性转移。
// 在恢复执行时也会自动添加转移动作。
type resumableAgentWithDeterministicTransferTo struct {
	agent        ResumableAgent // 被包装的可恢复智能体
	toAgentNames []string       // 目标智能体名称列表
}

// Description 返回被包装智能体的描述
func (a *resumableAgentWithDeterministicTransferTo) Description(ctx context.Context) string {
	return a.agent.Description(ctx)
}

// Name 返回被包装智能体的名称
func (a *resumableAgentWithDeterministicTransferTo) Name(ctx context.Context) string {
	return a.agent.Name(ctx)
}

// Run 运行被包装的智能体，并在完成后自动添加转移动作。
// 转发所有智能体事件，智能体完成且未中断时，生成转移消息和转移动作。
func (a *resumableAgentWithDeterministicTransferTo) Run(ctx context.Context,
	input *AgentInput, options ...AgentRunOption) *AsyncIterator[*AgentEvent] {

	// 如果是 flowAgent，清除运行上下文避免冲突
	if _, ok := a.agent.(*flowAgent); ok {
		ctx = ClearRunCtx(ctx)
	}

	aIter := a.agent.Run(ctx, input, options...)

	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()
	go appendTransferAction(ctx, aIter, generator, a.toAgentNames)

	return iterator
}

// Resume 从中断点恢复智能体执行，并在完成后自动添加转移动作。
// 恢复执行后的行为与正常运行相同，完成时会生成转移动作。
func (a *resumableAgentWithDeterministicTransferTo) Resume(ctx context.Context, info *ResumeInfo, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	aIter := a.agent.Resume(ctx, info, opts...)

	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()
	go appendTransferAction(ctx, aIter, generator, a.toAgentNames)

	return iterator
}

// appendTransferAction 处理智能体事件流并在完成后添加转移动作。
// 转发所有事件，检测中断状态，未中断时按目标智能体列表生成转移事件。
func appendTransferAction(ctx context.Context, aIter *AsyncIterator[*AgentEvent], generator *AsyncGenerator[*AgentEvent], toAgentNames []string) {
	defer func() {
		panicErr := recover()
		if panicErr != nil {
			e := safe.NewPanicErr(panicErr, debug.Stack())
			generator.Send(&AgentEvent{Err: e})
		}

		generator.Close()
	}()

	interrupted := false

	// 遍历智能体事件流，转发所有事件并检测中断状态
	for {
		event, ok := aIter.Next()
		if !ok {
			break
		}

		generator.Send(event)

		// 检测是否发生中断
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = true
		} else {
			interrupted = false
		}
	}

	// 如果发生中断，不生成转移动作，直接返回
	if interrupted {
		return
	}

	// 按目标智能体列表依次生成转移消息和转移动作
	for _, toAgentName := range toAgentNames {
		aMsg, tMsg := GenTransferMessages(ctx, toAgentName)
		aEvent := EventFromMessage(aMsg, nil, schema.Assistant, "")
		generator.Send(aEvent)
		tEvent := EventFromMessage(tMsg, nil, schema.Tool, tMsg.ToolName)
		tEvent.Action = &AgentAction{
			TransferToAgent: &TransferToAgentAction{
				DestAgentName: toAgentName,
			},
		}
		generator.Send(tEvent)
	}
}
