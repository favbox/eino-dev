/*
 * workflow.go - 工作流智能体实现，提供三种执行模式
 *
 * 核心组件：
 *   - workflowAgent: 工作流智能体，支持顺序、循环和并行执行模式
 *   - WorkflowInterruptInfo: 工作流中断信息，保存各模式的中断状态
 *   - BreakLoopAction: 循环中断动作，用于提前终止循环执行
 *
 * 三种执行模式：
 *   - Sequential (顺序): 子智能体按顺序依次执行，前一个完成后执行下一个
 *   - Loop (循环): 子智能体按顺序循环执行，支持最大迭代次数限制
 *   - Parallel (并行): 子智能体并发执行，等待所有子智能体完成
 *
 * 设计特点：
 *   - 模式切换: 根据配置自动选择执行模式
 *   - 中断恢复: 支持各模式的中断和恢复，保存执行进度
 *   - 循环控制: 支持 BreakLoopAction 提前终止循环
 *   - 并发安全: 并行模式使用 goroutine 和 WaitGroup 确保并发安全
 *   - 运行路径: 自动构建和维护完整的运行路径信息
 *
 * 中断处理：
 *   - 顺序模式: 保存中断位置索引和子智能体中断信息
 *   - 循环模式: 保存当前迭代次数和中断位置
 *   - 并行模式: 保存所有未完成子智能体的中断信息映射
 */

package adk

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"

	"github.com/favbox/eino/internal/safe"
)

// workflowAgentMode 定义工作流智能体的执行模式
type workflowAgentMode int

const (
	workflowAgentModeUnknown    workflowAgentMode = iota // 未知模式
	workflowAgentModeSequential                          // 顺序执行模式
	workflowAgentModeLoop                                // 循环执行模式
	workflowAgentModeParallel                            // 并行执行模式
)

// workflowAgent 实现工作流智能体，支持顺序、循环和并行三种执行模式。
// 根据模式配置，协调多个子智能体的执行顺序和方式。
type workflowAgent struct {
	name          string            // 智能体名称
	description   string            // 智能体描述
	subAgents     []*flowAgent      // 子智能体列表
	mode          workflowAgentMode // 执行模式
	maxIterations int               // 循环模式的最大迭代次数，0 表示无限循环
}

// Name 返回工作流智能体的名称
func (a *workflowAgent) Name(_ context.Context) string {
	return a.name
}

// Description 返回工作流智能体的描述
func (a *workflowAgent) Description(_ context.Context) string {
	return a.description
}

// Run 运行工作流智能体并返回事件迭代器。
// 根据执行模式选择对应的执行策略，支持顺序、循环和并行三种模式。
func (a *workflowAgent) Run(ctx context.Context, input *AgentInput, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()

	go func() {

		var err error
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				e := safe.NewPanicErr(panicErr, debug.Stack())
				generator.Send(&AgentEvent{Err: e})
			} else if err != nil {
				generator.Send(&AgentEvent{Err: err})
			}

			generator.Close()
		}()

		// 根据执行模式选择不同的工作流执行策略
		switch a.mode {
		case workflowAgentModeSequential:
			a.runSequential(ctx, input, generator, nil, 0, opts...)
		case workflowAgentModeLoop:
			a.runLoop(ctx, input, generator, nil, opts...)
		case workflowAgentModeParallel:
			a.runParallel(ctx, input, generator, nil, opts...)
		default:
			err = fmt.Errorf("unsupported workflow agent mode: %d", a.mode)
		}
	}()

	return iterator
}

// Resume 从中断点恢复工作流智能体的执行。
// 根据保存的中断信息和执行模式，从上次中断的位置继续执行。
func (a *workflowAgent) Resume(ctx context.Context, info *ResumeInfo, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
	wi, ok := info.Data.(*WorkflowInterruptInfo)
	if !ok {
		// 不可达，中断信息类型错误
		iterator, generator := NewAsyncIteratorPair[*AgentEvent]()
		generator.Send(&AgentEvent{Err: fmt.Errorf("type of InterruptInfo.Data is expected to %s, actual: %T", reflect.TypeOf((*WorkflowInterruptInfo)(nil)).String(), info.Data)})
		generator.Close()

		return iterator
	}

	iterator, generator := NewAsyncIteratorPair[*AgentEvent]()

	go func() {

		var err error
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				e := safe.NewPanicErr(panicErr, debug.Stack())
				generator.Send(&AgentEvent{Err: e})
			} else if err != nil {
				generator.Send(&AgentEvent{Err: err})
			}

			generator.Close()
		}()

		// 根据执行模式选择不同的工作流恢复策略
		switch a.mode {
		case workflowAgentModeSequential:
			a.runSequential(ctx, wi.OrigInput, generator, wi, 0, opts...)
		case workflowAgentModeLoop:
			a.runLoop(ctx, wi.OrigInput, generator, wi, opts...)
		case workflowAgentModeParallel:
			a.runParallel(ctx, wi.OrigInput, generator, wi, opts...)
		default:
			err = fmt.Errorf("unsupported workflow agent mode: %d", a.mode)
		}
	}()
	return iterator
}

// WorkflowInterruptInfo 保存工作流智能体的中断信息。
// 根据执行模式的不同，保存相应的中断状态和恢复所需的信息。
type WorkflowInterruptInfo struct {
	OrigInput *AgentInput // 原始输入

	SequentialInterruptIndex int            // 顺序模式：中断位置的子智能体索引
	SequentialInterruptInfo  *InterruptInfo // 顺序模式：子智能体的中断信息

	LoopIterations int // 循环模式：当前迭代次数

	ParallelInterruptInfo map[int] /*index*/ *InterruptInfo // 并行模式：各子智能体的中断信息映射
}

// runSequential 执行顺序工作流，子智能体按顺序依次执行。
// 支持从中断点恢复，返回是否退出和是否中断两个标志。
// iterations 参数由循环智能体传入，用于构建运行路径。
func (a *workflowAgent) runSequential(ctx context.Context, input *AgentInput,
	generator *AsyncGenerator[*AgentEvent], intInfo *WorkflowInterruptInfo, iterations int /*passed by loop agent*/, opts ...AgentRunOption) (exit, interrupted bool) {
	var runPath []RunStep // 每次循环重新构建运行路径
	if iterations > 0 {
		runPath = make([]RunStep, 0, (iterations+1)*len(a.subAgents))
		for iter := 0; iter < iterations; iter++ {
			for j := 0; j < len(a.subAgents); j++ {
				runPath = append(runPath, RunStep{
					agentName: a.subAgents[j].Name(ctx),
				})
			}
		}
	}

	i := 0
	if intInfo != nil { // 恢复之前的运行路径
		i = intInfo.SequentialInterruptIndex

		for j := 0; j < i; j++ {
			runPath = append(runPath, RunStep{
				agentName: a.subAgents[j].Name(ctx),
			})
		}
	}

	runCtx := getRunCtx(ctx)
	nRunCtx := runCtx.deepCopy()
	nRunCtx.RunPath = append(nRunCtx.RunPath, runPath...)
	nCtx := setRunCtx(ctx, nRunCtx)

	for ; i < len(a.subAgents); i++ {
		subAgent := a.subAgents[i]

		var subIterator *AsyncIterator[*AgentEvent]
		if intInfo != nil && i == intInfo.SequentialInterruptIndex {
			nCtx, nRunCtx = initRunCtx(nCtx, subAgent.Name(nCtx), nRunCtx.RootInput)
			enableStreaming := false
			if runCtx.RootInput != nil {
				enableStreaming = runCtx.RootInput.EnableStreaming
			}
			subIterator = subAgent.Resume(nCtx, &ResumeInfo{
				EnableStreaming: enableStreaming,
				InterruptInfo:   intInfo.SequentialInterruptInfo,
			}, opts...)
		} else {
			subIterator = subAgent.Run(nCtx, input, opts...)
			nCtx, _ = initRunCtx(nCtx, subAgent.Name(nCtx), input)
		}

		var lastActionEvent *AgentEvent
		for {
			event, ok := subIterator.Next()
			if !ok {
				break
			}

			if event.Err != nil {
				// 如果报告错误则退出
				generator.Send(event)
				return true, false
			}

			if lastActionEvent != nil {
				generator.Send(lastActionEvent)
				lastActionEvent = nil
			}

			if event.Action != nil {
				lastActionEvent = event
				continue
			}
			generator.Send(event)
		}

		if lastActionEvent != nil {
			if lastActionEvent.Action.Interrupted != nil {
				newEvent := wrapWorkflowInterrupt(lastActionEvent, input, i, iterations)

				// 重置运行上下文，
				// 因为控制权应该转移到工作流智能体，而不是被中断的智能体
				replaceInterruptRunCtx(nCtx, runCtx)

				// 转发事件
				generator.Send(newEvent)
				return true, true
			}

			if lastActionEvent.Action.Exit {
				// 转发事件
				generator.Send(lastActionEvent)
				return true, false
			}

			if a.doBreakLoopIfNeeded(lastActionEvent.Action, iterations) {
				lastActionEvent.Action.BreakLoop.CurrentIterations = iterations
				generator.Send(lastActionEvent)
				return true, false
			}

			generator.Send(lastActionEvent)
		}
	}

	return false, false
}

// wrapWorkflowInterrupt 包装工作流中断事件，将子智能体的中断信息封装为工作流的中断信息。
// 保存原始输入、中断位置和迭代次数，用于后续恢复。
func wrapWorkflowInterrupt(e *AgentEvent, origInput *AgentInput, seqIdx int, iterations int) *AgentEvent {
	newEvent := &AgentEvent{
		AgentName: e.AgentName,
		RunPath:   e.RunPath,
		Output:    e.Output,
		Action: &AgentAction{
			Exit:             e.Action.Exit,
			Interrupted:      &InterruptInfo{Data: e.Action.Interrupted.Data},
			TransferToAgent:  e.Action.TransferToAgent,
			CustomizedAction: e.Action.CustomizedAction,
		},
		Err: e.Err,
	}
	newEvent.Action.Interrupted.Data = &WorkflowInterruptInfo{
		OrigInput:                origInput,
		SequentialInterruptIndex: seqIdx,
		SequentialInterruptInfo:  e.Action.Interrupted,
		LoopIterations:           iterations,
	}
	return newEvent
}

// BreakLoopAction 是用于提前终止循环工作流智能体执行的程序化动作。
// 当循环工作流智能体从子智能体接收到此动作时，会停止当前迭代，不再继续下一次迭代。
// 会将 BreakLoopAction 标记为 Done，向上层循环智能体表明此动作已处理，不应继续传递。
// 此动作仅供程序化使用，不适用于 LLM 生成。
type BreakLoopAction struct {
	// From 记录发起循环中断动作的智能体名称
	From string
	// Done 是状态标志，框架用于标记动作已被处理
	Done bool
	// CurrentIterations 由框架填充，记录循环在哪次迭代时被中断
	CurrentIterations int
}

// NewBreakLoopAction 创建新的 BreakLoopAction，发出终止当前循环的请求
func NewBreakLoopAction(agentName string) *AgentAction {
	return &AgentAction{BreakLoop: &BreakLoopAction{
		From: agentName,
	}}
}

// doBreakLoopIfNeeded 检查是否需要中断循环，如果是循环模式且收到未处理的 BreakLoopAction，则中断循环
func (a *workflowAgent) doBreakLoopIfNeeded(aa *AgentAction, iterations int) bool {
	if a.mode != workflowAgentModeLoop {
		return false
	}

	if aa != nil && aa.BreakLoop != nil && !aa.BreakLoop.Done {
		aa.BreakLoop.Done = true
		aa.BreakLoop.CurrentIterations = iterations
		return true
	}
	return false
}

// runLoop 执行循环工作流，子智能体按顺序循环执行。
// 支持最大迭代次数限制（0 表示无限循环），支持从中断点恢复。
func (a *workflowAgent) runLoop(ctx context.Context, input *AgentInput,
	generator *AsyncGenerator[*AgentEvent], intInfo *WorkflowInterruptInfo, opts ...AgentRunOption) {

	if len(a.subAgents) == 0 {
		return
	}
	var iterations int
	if intInfo != nil {
		iterations = intInfo.LoopIterations
	}
	for iterations < a.maxIterations || a.maxIterations == 0 {
		exit, interrupted := a.runSequential(ctx, input, generator, intInfo, iterations, opts...)
		if interrupted {
			return
		}
		if exit {
			return
		}
		intInfo = nil // 中断信息只生效一次
		iterations++
	}
}

// runParallel 执行并行工作流，所有子智能体并发执行。
// 使用 goroutine 和 WaitGroup 确保并发安全，收集所有中断信息。
func (a *workflowAgent) runParallel(ctx context.Context, input *AgentInput,
	generator *AsyncGenerator[*AgentEvent], intInfo *WorkflowInterruptInfo, opts ...AgentRunOption) {

	if len(a.subAgents) == 0 {
		return
	}

	runners := getRunners(a.subAgents, input, intInfo, opts...)
	var wg sync.WaitGroup
	interruptMap := make(map[int]*InterruptInfo)
	var mu sync.Mutex
	if len(runners) > 1 {
		for i := 1; i < len(runners); i++ {
			wg.Add(1)
			go func(idx int, runner func(ctx context.Context) *AsyncIterator[*AgentEvent]) {
				defer func() {
					panicErr := recover()
					if panicErr != nil {
						e := safe.NewPanicErr(panicErr, debug.Stack())
						generator.Send(&AgentEvent{Err: e})
					}
					wg.Done()
				}()

				iterator := runner(ctx)
				for {
					event, ok := iterator.Next()
					if !ok {
						break
					}
					if event.Action != nil && event.Action.Interrupted != nil {
						mu.Lock()
						interruptMap[idx] = event.Action.Interrupted
						mu.Unlock()
						break
					}
					// 转发事件
					generator.Send(event)
				}
			}(i, runners[i])
		}
	}

	runner := runners[0]
	iterator := runner(ctx)
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			mu.Lock()
			interruptMap[0] = event.Action.Interrupted
			mu.Unlock()
			break
		}
		// 转发事件
		generator.Send(event)
	}

	if len(a.subAgents) > 1 {
		wg.Wait()
	}

	if len(interruptMap) > 0 {
		replaceInterruptRunCtx(ctx, getRunCtx(ctx))
		generator.Send(&AgentEvent{
			AgentName: a.Name(ctx),
			RunPath:   getRunCtx(ctx).RunPath,
			Action: &AgentAction{
				Interrupted: &InterruptInfo{
					Data: &WorkflowInterruptInfo{
						OrigInput:             input,
						ParallelInterruptInfo: interruptMap,
					},
				},
			},
		})
	}
}

// getRunners 为并行执行准备子智能体的运行函数列表。
// 如果是恢复模式，只为有中断信息的子智能体创建恢复运行函数。
func getRunners(subAgents []*flowAgent, input *AgentInput, intInfo *WorkflowInterruptInfo, opts ...AgentRunOption) []func(ctx context.Context) *AsyncIterator[*AgentEvent] {
	ret := make([]func(ctx context.Context) *AsyncIterator[*AgentEvent], 0, len(subAgents))
	if intInfo == nil {
		// 初始运行
		for _, subAgent := range subAgents {
			sa := subAgent
			ret = append(ret, func(ctx context.Context) *AsyncIterator[*AgentEvent] {
				return sa.Run(ctx, input, opts...)
			})
		}
		return ret
	}
	// 恢复运行
	for i, subAgent := range subAgents {
		sa := subAgent
		info, ok := intInfo.ParallelInterruptInfo[i]
		if !ok {
			// 已执行完成，跳过
			continue
		}
		ret = append(ret, func(ctx context.Context) *AsyncIterator[*AgentEvent] {
			nCtx, runCtx := initRunCtx(ctx, sa.Name(ctx), input)
			enableStreaming := false
			if runCtx.RootInput != nil {
				enableStreaming = runCtx.RootInput.EnableStreaming
			}
			return sa.Resume(nCtx, &ResumeInfo{
				EnableStreaming: enableStreaming,
				InterruptInfo:   info,
			}, opts...)
		})
	}
	return ret
}

// SequentialAgentConfig 配置顺序工作流智能体
type SequentialAgentConfig struct {
	Name        string  // 智能体名称
	Description string  // 智能体描述
	SubAgents   []Agent // 子智能体列表，按顺序执行
}

// ParallelAgentConfig 配置并行工作流智能体
type ParallelAgentConfig struct {
	Name        string  // 智能体名称
	Description string  // 智能体描述
	SubAgents   []Agent // 子智能体列表，并发执行
}

// LoopAgentConfig 配置循环工作流智能体
type LoopAgentConfig struct {
	Name        string  // 智能体名称
	Description string  // 智能体描述
	SubAgents   []Agent // 子智能体列表，循环执行

	MaxIterations int // 最大迭代次数，0 表示无限循环
}

func newWorkflowAgent(ctx context.Context, name, desc string,
	subAgents []Agent, mode workflowAgentMode, maxIterations int) (*flowAgent, error) {

	wa := &workflowAgent{
		name:        name,
		description: desc,
		mode:        mode,

		maxIterations: maxIterations,
	}

	fas := make([]Agent, len(subAgents))
	for i, subAgent := range subAgents {
		fas[i] = toFlowAgent(ctx, subAgent, WithDisallowTransferToParent())
	}

	fa, err := setSubAgents(ctx, wa, fas)
	if err != nil {
		return nil, err
	}

	wa.subAgents = fa.subAgents

	return fa, nil
}

// NewSequentialAgent 创建顺序工作流智能体，子智能体按配置顺序依次执行
func NewSequentialAgent(ctx context.Context, config *SequentialAgentConfig) (Agent, error) {
	return newWorkflowAgent(ctx, config.Name, config.Description, config.SubAgents, workflowAgentModeSequential, 0)
}

// NewParallelAgent 创建并行工作流智能体，所有子智能体并发执行
func NewParallelAgent(ctx context.Context, config *ParallelAgentConfig) (Agent, error) {
	return newWorkflowAgent(ctx, config.Name, config.Description, config.SubAgents, workflowAgentModeParallel, 0)
}

// NewLoopAgent 创建循环工作流智能体，子智能体按顺序循环执行，支持最大迭代次数限制
func NewLoopAgent(ctx context.Context, config *LoopAgentConfig) (Agent, error) {
	return newWorkflowAgent(ctx, config.Name, config.Description, config.SubAgents, workflowAgentModeLoop, config.MaxIterations)
}
