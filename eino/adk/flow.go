/*
 * flow.go - 多智能体编排和流转控制
 *
 * 核心组件：
 *   - flowAgent: 智能体装饰器，提供子智能体管理和转移控制
 *   - HistoryRewriter: 历史消息重写器，将其他智能体的消息转换为当前智能体可理解的格式
 *   - AgentOption: 智能体配置选项，支持禁止向父智能体转移、自定义历史重写等
 *
 * 设计特点：
 *   - 装饰器模式：flowAgent 包装原始 Agent，增强多智能体能力
 *   - 层次结构：支持父子智能体关系，通过 parentAgent 和 subAgents 建立
 *   - 转移控制：通过 TransferToAgent 动作实现智能体间的流转
 *   - 历史重写：自动将其他智能体的消息改写为用户消息，保持上下文连贯
 *
 * 与其他文件关系：
 *   - 为 runner.go 提供多智能体的编排能力
 *   - 使用 runctx.go 管理执行路径和会话状态
 *   - 为 chatmodel.go 和 workflow.go 提供子智能体管理
 */

package adk

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/internal/safe"
	"github.com/favbox/eino/schema"
)

// HistoryEntry 表示历史消息记录项。
// 用于在历史重写时区分用户输入和智能体输出。
type HistoryEntry struct {
	IsUserInput bool
	AgentName   string
	Message     Message
}

// HistoryRewriter 是历史消息重写函数。
// 将历史记录项转换为当前智能体可理解的消息列表，通常会将其他智能体的消息改写为用户消息。
type HistoryRewriter func(ctx context.Context, entries []*HistoryEntry) ([]Message, error)

// flowAgent 是智能体的装饰器，提供多智能体编排能力。
// 包装原始 Agent 接口，增加子智能体管理、转移控制和历史重写功能。
// 支持任意层级的智能体嵌套，通过 parentAgent 和 subAgents 建立层次关系。
type flowAgent struct {
	Agent

	subAgents   []*flowAgent
	parentAgent *flowAgent

	disallowTransferToParent bool
	historyRewriter          HistoryRewriter

	checkPointStore compose.CheckPointStore
}

func (a *flowAgent) deepCopy() *flowAgent {
	ret := &flowAgent{
		Agent:                    a.Agent,
		subAgents:                make([]*flowAgent, 0, len(a.subAgents)),
		parentAgent:              a.parentAgent,
		disallowTransferToParent: a.disallowTransferToParent,
		historyRewriter:          a.historyRewriter,
		checkPointStore:          a.checkPointStore,
	}

	for _, sa := range a.subAgents {
		ret.subAgents = append(ret.subAgents, sa.deepCopy())
	}
	return ret
}

func (a *flowAgent) getAgent(ctx context.Context, name string) *flowAgent {
	for _, subAgent := range a.subAgents {
		if subAgent.Name(ctx) == name {
			return subAgent
		}
	}

	if a.parentAgent != nil && a.parentAgent.Name(ctx) == name {
		return a.parentAgent
	}

	return nil
}

// SetSubAgents 为智能体设置子智能体。
// 建立父子关系，子智能体可以通过 TransferToAgent 动作转移控制权到父智能体或兄弟智能体。
func SetSubAgents(ctx context.Context, agent Agent, subAgents []Agent) (Agent, error) {
	return setSubAgents(ctx, agent, subAgents)
}

// AgentOption 是智能体配置选项函数。
type AgentOption func(options *flowAgent)

// WithDisallowTransferToParent 禁止子智能体向父智能体转移。
// 用于 WorkflowAgent 等场景，确保子智能体不会跳出工作流的控制。
func WithDisallowTransferToParent() AgentOption {
	return func(fa *flowAgent) {
		fa.disallowTransferToParent = true
	}
}

// WithHistoryRewriter 设置自定义的历史消息重写器。
// 如果不设置，会使用默认的重写器将其他智能体的消息改写为用户消息。
func WithHistoryRewriter(h HistoryRewriter) AgentOption {
	return func(fa *flowAgent) {
		fa.historyRewriter = h
	}
}

func toFlowAgent(ctx context.Context, agent Agent, opts ...AgentOption) *flowAgent {
	var fa *flowAgent
	var ok bool
	if fa, ok = agent.(*flowAgent); !ok {
		fa = &flowAgent{Agent: agent}
	} else {
		fa = fa.deepCopy()
	}
	for _, opt := range opts {
		opt(fa)
	}

	if fa.historyRewriter == nil {
		fa.historyRewriter = buildDefaultHistoryRewriter(agent.Name(ctx))
	}

	return fa
}

// AgentWithOptions 为智能体应用配置选项。
// 将普通 Agent 转换为 flowAgent 并应用选项。
func AgentWithOptions(ctx context.Context, agent Agent, opts ...AgentOption) Agent {
	return toFlowAgent(ctx, agent, opts...)
}

func setSubAgents(ctx context.Context, agent Agent, subAgents []Agent) (*flowAgent, error) {
	fa := toFlowAgent(ctx, agent)

	if len(fa.subAgents) > 0 {
		return nil, errors.New("agent's sub-agents has already been set")
	}

	if onAgent, ok_ := fa.Agent.(OnSubAgents); ok_ {
		err := onAgent.OnSetSubAgents(ctx, subAgents)
		if err != nil {
			return nil, err
		}
	}

	for _, s := range subAgents {
		fsa := toFlowAgent(ctx, s)

		if fsa.parentAgent != nil {
			return nil, errors.New("agent has already been set as a sub-agent of another agent")
		}

		fsa.parentAgent = fa
		if onAgent, ok__ := fsa.Agent.(OnSubAgents); ok__ {
			err := onAgent.OnSetAsSubAgent(ctx, agent)
			if err != nil {
				return nil, err
			}

			if fsa.disallowTransferToParent {
				err = onAgent.OnDisallowTransferToParent(ctx)
				if err != nil {
					return nil, err
				}
			}
		}

		fa.subAgents = append(fa.subAgents, fsa)
	}

	return fa, nil
}

// belongToRunPath 判断事件的运行路径是否属于当前运行路径。
// 用于过滤历史事件，只保留当前执行路径上的事件。
func belongToRunPath(eventRunPath []RunStep, runPath []RunStep) bool {
	if len(runPath) < len(eventRunPath) {
		return false
	}

	for i, step := range eventRunPath {
		if !runPath[i].Equals(step) {
			return false
		}
	}

	return true
}

// rewriteMessage 将智能体消息重写为用户消息。
// 添加上下文前缀说明消息来源，便于当前智能体理解其他智能体的输出。
func rewriteMessage(msg Message, agentName string) Message {
	var sb strings.Builder
	sb.WriteString("For context:")
	if msg.Role == schema.Assistant {
		if msg.Content != "" {
			sb.WriteString(fmt.Sprintf(" [%s] said: %s.", agentName, msg.Content))
		}
		if len(msg.ToolCalls) > 0 {
			for i := range msg.ToolCalls {
				f := msg.ToolCalls[i].Function
				sb.WriteString(fmt.Sprintf(" [%s] called tool: `%s` with arguments: %s.",
					agentName, f.Name, f.Arguments))
			}
		}
	} else if msg.Role == schema.Tool && msg.Content != "" {
		sb.WriteString(fmt.Sprintf(" [%s] `%s` tool returned result: %s.",
			agentName, msg.ToolName, msg.Content))
	}

	return schema.UserMessage(sb.String())
}

func genMsg(entry *HistoryEntry, agentName string) (Message, error) {
	msg := entry.Message
	if entry.AgentName != agentName {
		msg = rewriteMessage(msg, entry.AgentName)
	}

	return msg, nil
}

func (ai *AgentInput) deepCopy() *AgentInput {
	copied := &AgentInput{
		Messages:        make([]Message, len(ai.Messages)),
		EnableStreaming: ai.EnableStreaming,
	}

	copy(copied.Messages, ai.Messages)

	return copied
}

func (a *flowAgent) genAgentInput(ctx context.Context, runCtx *runContext, skipTransferMessages bool) (*AgentInput, error) {
	input := runCtx.RootInput.deepCopy()
	runPath := runCtx.RunPath

	events := runCtx.Session.getEvents()
	historyEntries := make([]*HistoryEntry, 0)

	for _, m := range input.Messages {
		historyEntries = append(historyEntries, &HistoryEntry{
			IsUserInput: true,
			Message:     m,
		})
	}

	for _, event := range events {
		if !belongToRunPath(event.RunPath, runPath) {
			continue
		}

		if skipTransferMessages && event.Action != nil && event.Action.TransferToAgent != nil {
			// 如果 skipTransferMessages 为 true 且事件包含转移动作，该事件的消息不会添加到历史记录
			if event.Output != nil &&
				event.Output.MessageOutput != nil &&
				event.Output.MessageOutput.Role == schema.Tool &&
				len(historyEntries) > 0 {
				// 如果跳过的消息角色是 Tool，移除前一个历史记录项，因为它也是转移消息（来自
				// 和 GenTransferMessages）
				historyEntries = historyEntries[:len(historyEntries)-1]
			}
			continue
		}

		msg, err := getMessageFromWrappedEvent(event)
		if err != nil {
			return nil, err
		}

		if msg == nil {
			continue
		}

		historyEntries = append(historyEntries, &HistoryEntry{
			AgentName: event.AgentName,
			Message:   msg,
		})
	}

	messages, err := a.historyRewriter(ctx, historyEntries)
	if err != nil {
		return nil, err
	}
	input.Messages = messages

	return input, nil
}

// buildDefaultHistoryRewriter 构建默认的历史消息重写器。
// 将其他智能体的消息改写为用户消息，保持当前智能体的输入格式统一。
func buildDefaultHistoryRewriter(agentName string) HistoryRewriter {
	return func(ctx context.Context, entries []*HistoryEntry) ([]Message, error) {
		messages := make([]Message, 0, len(entries))
		var err error
		for _, entry := range entries {
			msg := entry.Message
			if !entry.IsUserInput {
				msg, err = genMsg(entry, agentName)
				if err != nil {
					return nil, fmt.Errorf("gen agent input failed: %w", err)
				}
			}

			if msg != nil {
				messages = append(messages, msg)
			}
		}

		return messages, nil
	}
}

// Run 运行智能体，返回事件流。
// 初始化运行上下文，生成智能体输入，执行智能体并处理转移逻辑。
func (a *flowAgent) Run(ctx context.Context, input *AgentInput, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	agentName := a.Name(ctx)

	ctx, runCtx := initRunCtx(ctx, agentName, input)

	o := getCommonOptions(nil, opts...)

	input, err := a.genAgentInput(ctx, runCtx, o.skipTransferMessages)
	if err != nil {
		return genErrorIter(err)
	}

	if wf, ok := a.Agent.(*workflowAgent); ok {
		return wf.Run(ctx, input, opts...)
	}

	aIter := a.Agent.Run(ctx, input, filterOptions(agentName, opts)...)

	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()

	go a.run(ctx, runCtx, aIter, generator, opts...)

	return iterator
}

// Resume 从中断点恢复智能体执行。
// 根据 runContext 中的路径找到中断的智能体，并从中断点继续执行。
func (a *flowAgent) Resume(ctx context.Context, info *ResumeInfo, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	runCtx := getRunCtx(ctx)
	if runCtx == nil {
		return genErrorIter(fmt.Errorf("failed to resume agent: run context is empty"))
	}

	agentName := a.Name(ctx)
	targetName := agentName
	if len(runCtx.RunPath) > 0 {
		targetName = runCtx.RunPath[len(runCtx.RunPath)-1].agentName
	}

	if agentName != targetName {
		// 跳转到目标智能体
		targetAgent := recursiveGetAgent(ctx, a, targetName)
		if targetAgent == nil {
			return genErrorIter(fmt.Errorf("failed to resume agent: cannot find agent: %s", agentName))
		}
		return targetAgent.Resume(ctx, info, opts...)
	}

	if wf, ok := a.Agent.(*workflowAgent); ok {
		return wf.Resume(ctx, info, opts...)
	}

	// 恢复当前智能体
	ra, ok := a.Agent.(ResumableAgent)
	if !ok {
		return genErrorIter(fmt.Errorf("failed to resume agent: target agent[%s] isn't resumable", agentName))
	}
	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()
	aIter := ra.Resume(ctx, info, opts...)

	go a.run(ctx, runCtx, aIter, generator, opts...)

	return iterator
}

// DeterministicTransferConfig 是确定性转移配置。
// 限制智能体只能转移到指定的目标智能体列表。
type DeterministicTransferConfig struct {
	Agent        Agent
	ToAgentNames []string
}

func (a *flowAgent) run(
	ctx context.Context,
	runCtx *runContext,
	aIter *AsyncIterator[*AgentEvent],
	generator *AsyncGenerator[*AgentEvent],
	opts ...AgentRunOption) {
	defer func() {
		panicErr := recover()
		if panicErr != nil {
			e := safe.NewPanicErr(panicErr, debug.Stack())
			generator.Send(&AgentEvent{Err: e})
		}

		generator.Close()
	}()

	var lastAction *AgentAction
	for {
		event, ok := aIter.Next()
		if !ok {
			break
		}

		event.AgentName = a.Name(ctx)
		event.RunPath = runCtx.RunPath
		if event.Action == nil || event.Action.Interrupted == nil {
			// 复制事件，确保复制后的事件流对任何潜在消费者都是独占的
			// 在添加到 session 前复制，因为一旦添加到 session，流可能随时被 genAgentInput 消费
			// 中断动作不添加到 session，因为其中的所有信息要么呈现给最终用户，要么通过其他方式提供给智能体
			copied := copyAgentEvent(event)
			setAutomaticClose(copied)
			setAutomaticClose(event)
			runCtx.Session.addEvent(copied)
		}
		lastAction = event.Action
		generator.Send(event)
	}

	var destName string
	if lastAction != nil {
		if lastAction.Interrupted != nil {
			appendInterruptRunCtx(ctx, runCtx)
			return
		}
		if lastAction.Exit {
			return
		}

		if lastAction.TransferToAgent != nil {
			destName = lastAction.TransferToAgent.DestAgentName
		}
	}

	// 处理转移到其他智能体
	if destName != "" {
		agentToRun := a.getAgent(ctx, destName)
		if agentToRun == nil {
			e := errors.New(fmt.Sprintf(
				"transfer failed: agent '%s' not found when transferring from '%s'",
				destName, a.Name(ctx)))
			generator.Send(&AgentEvent{Err: e})
			return
		}

		subAIter := agentToRun.Run(ctx, nil /* 子智能体从 runCtx 获取输入 */, opts...)
		for {
			subEvent, ok_ := subAIter.Next()
			if !ok_ {
				break
			}

			setAutomaticClose(subEvent)
			generator.Send(subEvent)
		}
	}
}

// recursiveGetAgent 递归查找指定名称的智能体。
// 在当前智能体及其所有子智能体中查找。
func recursiveGetAgent(ctx context.Context, agent *flowAgent, agentName string) *flowAgent {
	if agent == nil {
		return nil
	}
	if agent.Name(ctx) == agentName {
		return agent
	}
	a := agent.getAgent(ctx, agentName)
	if a != nil {
		return a
	}
	for _, sa := range agent.subAgents {
		a = recursiveGetAgent(ctx, sa, agentName)
		if a != nil {
			return a
		}
	}
	return nil
}
