/*
 * runner.go - 智能体运行器，提供统一的智能体运行接口和生命周期管理
 *
 * 核心组件：
 *   - Runner: 智能体运行器，封装智能体执行逻辑和生命周期管理
 *   - RunnerConfig: 运行器配置，包含智能体、流式模式和检查点存储
 *
 * 设计意图：
 *   Runner 是智能体执行的最外层封装，位于架构的最顶层。
 *   在 Agent/flowAgent 的基础上增加了检查点管理、中断恢复和会话管理能力，
 *   为用户提供开箱即用的完整智能体运行方案。
 *
 * 架构层次：
 *   用户代码 → Runner → flowAgent → ChatModelAgent → ReAct Graph
 *
 * 设计特点：
 *   - 统一运行接口: 提供 Run、Query、Resume 三种运行模式
 *   - 流式支持: 可配置流式或非流式执行模式
 *   - 检查点集成: 自动监听中断事件并保存检查点，支持无缝恢复
 *   - 会话管理: 自动创建运行上下文和管理会话值
 *   - 异步事件流: 通过 AsyncIterator 异步返回智能体事件
 *   - 错误恢复: 内置 panic 恢复机制，确保错误安全传递
 *
 * 典型使用场景：
 *   - 长时间运行的智能体任务（需要中断恢复）
 *   - 多智能体协作系统（需要会话状态管理）
 *   - 生产环境部署（需要可靠的错误处理和检查点）
 *
 * 运行流程：
 *   1. 创建运行上下文和会话
 *   2. 将 Agent 转换为 flowAgent（增加多智能体能力）
 *   3. 执行智能体并获取事件迭代器
 *   4. 如果配置了检查点存储，启动 handleIter 监听中断事件
 *   5. 异步转发事件到调用方，在中断时自动保存检查点
 */

package adk

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/internal/safe"
	"github.com/favbox/eino/schema"
)

// Runner 是智能体运行器，提供统一的智能体运行和生命周期管理接口。
// 在 Agent 基础上增加检查点保存、中断恢复和会话管理能力，
// 是面向用户的最高层封装，简化智能体的部署和使用。
//
// Runner 自动将 Agent 转换为 flowAgent 以支持多智能体编排，
// 并通过 handleIter 拦截事件流，在检测到中断时自动保存检查点。
type Runner struct {
	a               Agent                   // 要运行的智能体（可以是 ChatModelAgent 或其他 Agent 实现）
	enableStreaming bool                    // 是否启用流式模式
	store           compose.CheckPointStore // 检查点存储，用于中断恢复（可选）
}

// RunnerConfig 配置 Runner 的行为参数
type RunnerConfig struct {
	Agent           Agent // 要运行的智能体
	EnableStreaming bool  // 是否启用流式模式

	CheckPointStore compose.CheckPointStore // 检查点存储，可选，配置后支持中断恢复
}

// NewRunner 创建新的智能体运行器。
// 根据配置初始化运行器，如果提供了 compose.CheckPointStore，将支持中断恢复功能
func NewRunner(_ context.Context, conf RunnerConfig) *Runner {
	return &Runner{
		enableStreaming: conf.EnableStreaming,
		a:               conf.Agent,
		store:           conf.CheckPointStore,
	}
}

// Run 运行智能体并返回事件迭代器。
// 这是 Runner 的核心方法，执行完整的智能体运行流程：
//  1. 将 Agent 转换为 flowAgent（增加多智能体能力）
//  2. 创建新的运行上下文并初始化会话值
//  3. 执行智能体获取事件迭代器
//  4. 如果配置了检查点存储，通过 handleIter 监听中断并自动保存检查点
//
// 参数 messages 为智能体的输入消息列表，opts 为运行选项（如会话值、检查点 ID 等）。
// 返回的 AsyncIterator 异步产生智能体事件，调用方通过 Next() 方法获取事件。
func (r *Runner) Run(ctx context.Context, messages []Message,
	opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	o := getCommonOptions(nil, opts...)

	// 将 Agent 转换为 flowAgent，增加多智能体编排能力
	fa := toFlowAgent(ctx, r.a)

	input := &AgentInput{
		Messages:        messages,
		EnableStreaming: r.enableStreaming,
	}

	// 创建新的运行上下文，用于追踪智能体执行路径
	ctx = ctxWithNewRunCtx(ctx)

	// 将会话值添加到上下文，供智能体访问
	AddSessionValues(ctx, o.sessionValues)

	iter := fa.Run(ctx, input, opts...)
	if r.store == nil {
		// 没有检查点存储，直接返回智能体事件迭代器
		return iter
	}

	// 有检查点存储，创建新的迭代器对用于处理中断和保存检查点
	niter, gen := NewAsyncIteratorPair[*AgentEvent]()

	go r.handleIter(ctx, iter, gen, o.checkPointID)
	return niter
}

// getInterruptRunCtx 从上下文中获取中断时的运行上下文。
// 运行上下文包含智能体执行路径、会话值等状态信息，用于恢复执行。
// 假设不存在并发情况，因此只返回第一个运行上下文。
func getInterruptRunCtx(ctx context.Context) *runContext {
	cs := getInterruptRunCtxs(ctx)
	if len(cs) == 0 {
		return nil
	}
	return cs[0] // 假设不存在并发，因此只有一个运行上下文在 ctx 中
}

// Query 提供简化的查询接口，将字符串查询包装为用户消息并运行智能体。
// 这是 Run 方法的便捷封装，适用于简单的文本查询场景。
//
// 示例：
//
//	iter := runner.Query(ctx, "今天天气如何？")
//	for {
//	    event, ok := iter.Next()
//	    if !ok { break }
//	    // 处理事件
//	}
func (r *Runner) Query(ctx context.Context, query string, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	return r.Run(ctx, []Message{schema.UserMessage(query)}, opts...)
}

// Resume 从指定的检查点恢复智能体执行。
// 从检查点存储中加载运行上下文和中断信息，继续执行被中断的智能体。
//
// 恢复流程：
//  1. 从 CheckPointStore 加载运行上下文和中断信息
//  2. 恢复会话值到上下文中
//  3. 调用 flowAgent.Resume() 继续执行
//  4. 如果有检查点存储，继续通过 handleIter 监听后续中断
//
// 如果检查点不存在或加载失败，返回错误。
// 要求 Runner 创建时必须配置 CheckPointStore，否则返回错误。
func (r *Runner) Resume(ctx context.Context, checkPointID string, opts ...AgentRunOption) (*AsyncIterator[*AgentEvent], error) {
	if r.store == nil {
		return nil, fmt.Errorf("failed to resume: store is nil")
	}

	// 从检查点存储加载运行上下文和中断信息
	runCtx, info, existed, err := getCheckPoint(ctx, r.store, checkPointID)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint: %w", err)
	}
	if !existed {
		return nil, fmt.Errorf("checkpoint[%s] is not existed", checkPointID)
	}

	// 恢复运行上下文到当前上下文
	ctx = setRunCtx(ctx, runCtx)

	o := getCommonOptions(nil, opts...)
	// 添加会话值（可能包含恢复时需要的额外信息）
	AddSessionValues(ctx, o.sessionValues)

	// 调用 flowAgent.Resume() 从中断点继续执行
	aIter := toFlowAgent(ctx, r.a).Resume(ctx, info, opts...)
	if r.store == nil {
		// 没有检查点存储，直接返回智能体事件迭代器
		// 理论上不应该走到这里（前面已检查 store 是否为 nil）
		return aIter, nil
	}

	// 继续通过 handleIter 监听后续可能的中断
	niter, gen := NewAsyncIteratorPair[*AgentEvent]()

	go r.handleIter(ctx, aIter, gen, &checkPointID)
	return niter, nil
}

// handleIter 处理智能体事件流，转发事件并在中断时保存检查点。
// 这是 Runner 的核心拦截逻辑，在 goroutine 中异步运行。
//
// 职责：
//  1. 遍历智能体事件流，逐个转发事件到调用方
//  2. 检测中断事件（event.Action.Interrupted != nil）
//  3. 在事件流结束时，如果发生了中断且有 checkPointID，保存检查点
//  4. 提供 panic 恢复机制，确保错误安全传递到事件流
//
// 参数：
//   - aIter: 智能体原始事件迭代器
//   - gen: 新的事件生成器，用于转发事件给调用方
//   - checkPointID: 检查点 ID，用于保存检查点（可为 nil）
func (r *Runner) handleIter(ctx context.Context, aIter *AsyncIterator[*AgentEvent], gen *AsyncGenerator[*AgentEvent], checkPointID *string) {
	defer func() {
		panicErr := recover()
		if panicErr != nil {
			e := safe.NewPanicErr(panicErr, debug.Stack())
			gen.Send(&AgentEvent{Err: e})
		}

		gen.Close()
	}()
	var interruptedInfo *InterruptInfo
	// 遍历智能体事件流，检测中断事件
	for {
		event, ok := aIter.Next()
		if !ok {
			// 事件流结束
			break
		}

		// 检测是否为中断事件（包含 Interrupted 动作）
		if event.Action != nil && event.Action.Interrupted != nil {
			interruptedInfo = event.Action.Interrupted
		} else {
			// 非中断事件，清空之前的中断信息（只保留最后一个中断）
			interruptedInfo = nil
		}

		// 转发事件到调用方（无论是否中断都要转发）
		gen.Send(event)
	}

	// 事件流结束后，如果最后发生了中断且配置了检查点 ID，保存检查点
	if interruptedInfo != nil && checkPointID != nil {
		// 保存运行上下文和中断信息到检查点存储
		err := saveCheckPoint(ctx, r.store, *checkPointID, getInterruptRunCtx(ctx), interruptedInfo)
		if err != nil {
			// 保存失败，发送错误事件
			gen.Send(&AgentEvent{Err: fmt.Errorf("failed to save checkpoint: %w", err)})
		}
	}
}
