/*
 * runner.go - 智能体运行器，提供统一的智能体运行接口
 *
 * 核心组件：
 *   - Runner: 智能体运行器，封装智能体执行逻辑和生命周期管理
 *   - RunnerConfig: 运行器配置，包含智能体、流式模式和检查点存储
 *
 * 设计特点：
 *   - 统一运行接口: 提供 Run、Query、Resume 三种运行模式
 *   - 流式支持: 可配置流式或非流式执行模式
 *   - 检查点集成: 集成检查点存储，支持中断和恢复
 *   - 会话管理: 自动管理运行上下文和会话值
 *   - 异步事件流: 通过 AsyncIterator 异步返回智能体事件
 *   - 错误恢复: 内置 panic 恢复机制，确保错误安全传递
 *
 * 运行流程：
 *   1. 创建运行上下文和会话
 *   2. 将智能体转换为 flowAgent
 *   3. 执行智能体并获取事件迭代器
 *   4. 如果配置了检查点存储，监听中断事件并保存检查点
 *   5. 异步转发事件到调用方
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
// 支持流式和非流式执行模式，集成检查点存储以支持中断恢复。
type Runner struct {
	a               Agent                   // 要运行的智能体
	enableStreaming bool                    // 是否启用流式模式
	store           compose.CheckPointStore // 检查点存储，用于中断恢复
}

// RunnerConfig 配置 Runner 的行为参数
type RunnerConfig struct {
	Agent           Agent // 要运行的智能体
	EnableStreaming bool  // 是否启用流式模式

	CheckPointStore compose.CheckPointStore // 检查点存储，可选
}

// NewRunner 创建并返回一个新的智能体运行器
func NewRunner(_ context.Context, conf RunnerConfig) *Runner {
	return &Runner{
		enableStreaming: conf.EnableStreaming,
		a:               conf.Agent,
		store:           conf.CheckPointStore,
	}
}

// Run 运行智能体并返回事件迭代器。
// 创建新的运行上下文，设置会话值，执行智能体并异步返回事件流。
// 如果配置了检查点存储，会在中断时自动保存检查点。
func (r *Runner) Run(ctx context.Context, messages []Message,
	opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	o := getCommonOptions(nil, opts...)

	fa := toFlowAgent(ctx, r.a)

	input := &AgentInput{
		Messages:        messages,
		EnableStreaming: r.enableStreaming,
	}

	ctx = ctxWithNewRunCtx(ctx)

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
func (r *Runner) Query(ctx context.Context,
	query string, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {

	return r.Run(ctx, []Message{schema.UserMessage(query)}, opts...)
}

// Resume 从指定的检查点恢复智能体执行。
// 从检查点存储中加载运行上下文和中断信息，继续执行被中断的智能体。
// 如果检查点不存在或加载失败，返回错误。
func (r *Runner) Resume(ctx context.Context, checkPointID string, opts ...AgentRunOption) (*AsyncIterator[*AgentEvent], error) {
	if r.store == nil {
		return nil, fmt.Errorf("failed to resume: store is nil")
	}

	runCtx, info, existed, err := getCheckPoint(ctx, r.store, checkPointID)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint: %w", err)
	}
	if !existed {
		return nil, fmt.Errorf("checkpoint[%s] is not existed", checkPointID)
	}

	ctx = setRunCtx(ctx, runCtx)

	o := getCommonOptions(nil, opts...)
	AddSessionValues(ctx, o.sessionValues)

	aIter := toFlowAgent(ctx, r.a).Resume(ctx, info, opts...)
	if r.store == nil {
		// 没有检查点存储，直接返回智能体事件迭代器
		return aIter, nil
	}

	// 有检查点存储，创建新的迭代器对用于处理后续中断
	niter, gen := NewAsyncIteratorPair[*AgentEvent]()

	go r.handleIter(ctx, aIter, gen, &checkPointID)
	return niter, nil
}

// handleIter 处理智能体事件流，转发事件并在中断时保存检查点。
// 在 goroutine 中运行，负责事件转发、中断检测和检查点保存。
// 包含 panic 恢复机制，确保错误安全传递到事件流。
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
			break
		}

		// 检测是否为中断事件
		if event.Action != nil && event.Action.Interrupted != nil {
			interruptedInfo = event.Action.Interrupted
		} else {
			interruptedInfo = nil
		}

		// 转发事件到调用方
		gen.Send(event)
	}

	// 如果发生中断且配置了检查点 ID，保存检查点
	if interruptedInfo != nil && checkPointID != nil {
		err := saveCheckPoint(ctx, r.store, *checkPointID, getInterruptRunCtx(ctx), interruptedInfo)
		if err != nil {
			gen.Send(&AgentEvent{Err: fmt.Errorf("failed to save checkpoint: %w", err)})
		}
	}
}
