package compose

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/favbox/eino/internal"
)

// chanCall 通道调用结构体 - 封装节点执行和连接信息
// 设计意图：统一管理节点的执行器、连接关系和处理器装饰器
// 关键字段：
//   - action: 节点的实际执行逻辑（可执行对象）
//   - writeTo: 数据边连接的后续节点
//   - writeToBranches: 分支连接的后续节点
//   - controls: 控制边连接的后续节点
//   - preProcessor/postProcessor: 节点前后装饰器
type chanCall struct {
	// 节点执行器：封装节点的执行逻辑
	action *composableRunnable
	// 数据边连接：节点输出数据传输到的后续节点
	writeTo []string
	// 分支连接：通过条件分支连接的后续节点
	writeToBranches []*GraphBranch

	// 控制边连接：分支必须控制的节点（用于执行顺序）
	controls []string

	// 节点前后置处理器：装饰节点的执行逻辑
	preProcessor, postProcessor *composableRunnable
}

// chanBuilder 通道构建器函数类型 - 根据依赖关系创建通道
// 设计意图：支持 Pregel 和 DAG 两种不同的通道构建策略
// 参数：
//   - dependencies: 直接依赖节点列表
//   - indirectDependencies: 间接依赖节点列表
//   - zeroValue: 零值工厂函数（用于初始化）
//   - emptyStream: 空流工厂函数（用于初始化）
//
// 返回：构建好的通道对象
type chanBuilder func(dependencies []string, indirectDependencies []string, zeroValue func() any, emptyStream func() streamReader) channel

// runner 图运行器结构体 - 封装图的执行引擎和运行时状态
// 设计意图：统一管理图执行的所有要素，包括节点映射、依赖关系、处理器和执行配置
// 关键组成部分：
//   - 节点管理：chanSubscribeTo（节点执行器）、successors（后续节点）
//   - 依赖关系：dataPredecessors（数据前置）、controlPredecessors（控制前置）
//   - 执行模式：eager（急切执行）、dag（DAG模式）、chanBuilder（通道构建策略）
//   - 类型系统：inputType、outputType、genericHelper（类型辅助）
//   - 处理器：edgeHandlerManager、preNodeHandlerManager、preBranchHandlerManager
//   - 执行控制：checkPointer（检查点）、interruptBefore/AfterNodes（中断控制）
//   - 合并配置：mergeConfigs（扇入合并策略）
type runner struct {
	// 节点执行器映射：节点键到通道调用的映射
	chanSubscribeTo map[string]*chanCall

	// 后续节点映射：节点的后续节点列表
	successors map[string][]string
	// 数据前置依赖：节点到其数据前置节点的映射
	dataPredecessors map[string][]string
	// 控制前置依赖：节点到其控制前置节点的映射
	controlPredecessors map[string][]string

	// 输入通道：图的入口通道
	inputChannels *chanCall

	// 通道构建器：基于依赖关系创建通道（可为nil）
	// Pregel和DAG模式使用不同的构建策略
	chanBuilder chanBuilder
	// 急切执行模式：是否启用急切执行
	eager bool
	// DAG模式标记：是否为有向无环图
	dag bool

	// 运行上下文生成器：为执行生成上下文（支持状态注入）
	runCtx func(ctx context.Context) context.Context

	// 编译选项：图的编译时配置
	options graphCompileOptions

	// 输入输出类型：图的类型信息
	inputType  reflect.Type
	outputType reflect.Type

	// 子图处理：通过 toComposableRunnable 生效
	// 输入流过滤器：处理输入流
	inputStreamFilter streamMapFilter
	// 输入转换器：处理输入转换
	inputConverter handlerPair
	// 输入字段映射转换器：处理字段映射
	inputFieldMappingConverter handlerPair
	// 输入输出流转换对：流式处理的转换器
	inputConvertStreamPair, outputConvertStreamPair streamConvertPair

	// 通用辅助系统：类型安全和转换
	*genericHelper

	// 运行时检查：编译时无法检查的验证
	// 运行时检查边：需要运行时验证的边
	runtimeCheckEdges map[string]map[string]bool
	// 运行时检查分支：需要运行时验证的分支
	runtimeCheckBranches map[string][]bool

	// 处理器管理器：
	// 边处理器管理器：处理边上的数据转换
	edgeHandlerManager *edgeHandlerManager
	// 节点前置处理器管理器：处理节点前置逻辑
	preNodeHandlerManager *preNodeHandlerManager
	// 分支前置处理器管理器：处理分支前置逻辑
	preBranchHandlerManager *preBranchHandlerManager

	// 检查点和中断控制：
	// 检查点：支持断点恢复和状态持久化
	checkPointer *checkPointer
	// 中断节点：在指定节点前/后中断图执行
	interruptBeforeNodes []string
	interruptAfterNodes  []string

	// 扇入合并配置：多输入节点的合并策略
	mergeConfigs map[string]FanInMergeConfig
}

// invoke 图同步执行入口 - 同步模式调用图执行
// 设计意图：为图提供同步执行接口，阻塞直到执行完成
// 参数：
//   - ctx: 上下文对象（支持取消和超时）
//   - input: 图的输入数据
//   - opts: 运行时选项（回调、中断控制等）
//
// 返回：
//   - any: 图执行结果
//   - error: 执行过程中的错误
func (r *runner) invoke(ctx context.Context, input any, opts ...Option) (any, error) {
	return r.run(ctx, false, input, opts...)
}

// transform 图流式执行入口 - 流模式调用图执行
// 设计意图：为图提供流式执行接口，支持实时流式数据处理
// 参数：
//   - ctx: 上下文对象
//   - input: 流式输入数据
//   - opts: 运行时选项
//
// 返回：
//   - streamReader: 流式输出数据
//   - error: 执行错误
func (r *runner) transform(ctx context.Context, input streamReader, opts ...Option) (streamReader, error) {
	s, err := r.run(ctx, true, input, opts...)
	if err != nil {
		return nil, err
	}

	return s.(streamReader), nil
}

// runnableCallWrapper 可执行对象调用包装器函数类型 - 统一同步和流式调用
// 设计意图：根据执行模式选择合适的调用策略（同步或流式）
// 参数：
//   - ctx: 上下文对象
//   - r: 可执行对象
//   - input: 输入数据
//   - opts: 可选参数
//
// 返回：
//   - any: 执行结果
//   - error: 执行错误
type runnableCallWrapper func(context.Context, *composableRunnable, any, ...any) (any, error)

// runnableInvoke 可执行对象同步调用函数 - 同步模式执行
func runnableInvoke(ctx context.Context, r *composableRunnable, input any, opts ...any) (any, error) {
	return r.i(ctx, input, opts...)
}

// runnableTransform 可执行对象流式调用函数 - 流模式执行
func runnableTransform(ctx context.Context, r *composableRunnable, input any, opts ...any) (any, error) {
	return r.t(ctx, input.(streamReader), opts...)
}

// run 图执行核心方法 - 统一同步和流式执行入口
// 设计意图：封装图的完整执行生命周期，支持检查点恢复、中断控制和任务调度
// 执行流程：
//  1. 延迟初始化：等待状态生成器完成，确保 onGraphStart 可访问状态
//  2. 执行管理器：初始化通道管理器和任务管理器
//  3. 检查点恢复：从上下文或存储加载执行状态
//  4. 主执行循环：任务提交→等待完成→计算后续任务
//  5. 中断处理：支持优雅和强制中断，恢复和重试机制
//  6. 生命周期回调：在 defer 中统一处理开始/错误/结束回调
func (r *runner) run(ctx context.Context, isStream bool, input any, opts ...Option) (result any, err error) {
	haveOnStart := false // 延迟触发 onGraphStart，等待状态初始化完成，确保状态可在回调中访问
	defer func() {
		if !haveOnStart {
			ctx, input = onGraphStart(ctx, input, isStream)
		}
		if err != nil {
			ctx, err = onGraphError(ctx, err)
		} else {
			ctx, result = onGraphEnd(ctx, result, isStream)
		}
	}()

	// 执行模式选择：根据是否流式执行选择对应的调用包装器
	var runWrapper runnableCallWrapper
	runWrapper = runnableInvoke
	if isStream {
		runWrapper = runnableTransform
	}

	// 初始化执行管理器：创建通道管理器和任务管理器
	cm := r.initChannelManager(isStream)
	tm := r.initTaskManager(runWrapper, getGraphCancel(ctx), opts...)
	maxSteps := r.options.maxRunSteps

	// 最大步数验证：DAG 模式不支持限制步数，Pregel 模式可选
	if r.dag {
		for i := range opts {
			if opts[i].maxRunSteps > 0 {
				return nil, newGraphRunError(fmt.Errorf("cannot set max run steps in dag"))
			}
		}
	} else {
		// 更新最大步数：运行时选项可覆盖编译时配置
		for i := range opts {
			if opts[i].maxRunSteps > 0 {
				maxSteps = opts[i].maxRunSteps
			}
		}
		if maxSteps < 1 {
			return nil, newGraphRunError(errors.New("max run steps limit must be at least 1"))
		}
	}

	// 提取节点选项：为每个节点分配对应的运行时选项
	optMap, extractErr := extractOption(r.chanSubscribeTo, opts...)
	if extractErr != nil {
		return nil, newGraphRunError(fmt.Errorf("graph extract option fail: %w", extractErr))
	}

	// 提取检查点信息：获取检查点 ID、状态修改器和执行控制参数
	checkPointID, writeToCheckPointID, stateModifier, forceNewRun := getCheckPointInfo(opts...)
	if checkPointID != nil && r.checkPointer.store == nil {
		return nil, newGraphRunError(fmt.Errorf("receive checkpoint id but have not set checkpoint store"))
	}

	// 提取子图信息：判断当前是否在子图执行，获取节点路径
	path, isSubGraph := getNodeKey(ctx)

	// 检查点恢复或图初始化：根据执行上下文决定恢复或从头开始
	initialized := false
	var nextTasks []*task
	if cp := getCheckPointFromCtx(ctx); cp != nil {
		// 子图检查点：从上下文恢复执行状态
		initialized = true
		ctx, nextTasks, err = r.restoreFromCheckPoint(ctx, *path, getStateModifier(ctx), cp, isStream, cm, optMap)
		ctx, input = onGraphStart(ctx, input, isStream)
		haveOnStart = true
	} else if checkPointID != nil && !forceNewRun {
		// 外部检查点：从存储加载执行状态
		cp, err = getCheckPointFromStore(ctx, *checkPointID, r.checkPointer)
		if err != nil {
			return nil, newGraphRunError(fmt.Errorf("load checkpoint from store fail: %w", err))
		}
		if cp != nil {
			// 加载检查点并恢复执行
			initialized = true

			ctx = setStateModifier(ctx, stateModifier)
			ctx = setCheckPointToCtx(ctx, cp)

			ctx, nextTasks, err = r.restoreFromCheckPoint(ctx, *NewNodePath(), stateModifier, cp, isStream, cm, optMap)
			ctx, input = onGraphStart(ctx, input, isStream)
			haveOnStart = true
		}
	}
	if !initialized {
		// 全新执行：未从检查点恢复，初始化图执行
		if r.runCtx != nil {
			ctx = r.runCtx(ctx)
		}

		ctx, input = onGraphStart(ctx, input, isStream)
		haveOnStart = true

		var isEnd bool
		nextTasks, result, isEnd, err = r.calculateNextTasks(ctx, []*task{{
			nodeKey: START,
			call:    r.inputChannels,
			output:  input,
		}}, isStream, cm, optMap)
		if err != nil {
			return nil, newGraphRunError(fmt.Errorf("calculate next tasks fail: %w", err))
		}
		if isEnd {
			return result, nil
		}
		if len(nextTasks) == 0 {
			return nil, newGraphRunError(fmt.Errorf("no tasks to execute after graph start"))
		}

		// 检查起始中断：监控在指定节点前的中断信号
		if keys := getHitKey(nextTasks, r.interruptBeforeNodes); len(keys) > 0 {
			tempInfo := newInterruptTempInfo()
			tempInfo.interruptBeforeNodes = append(tempInfo.interruptBeforeNodes, keys...)
			return nil, r.handleInterrupt(ctx,
				tempInfo,
				nextTasks,
				cm.channels,
				isStream,
				isSubGraph,
				writeToCheckPointID,
			)
		}
	}

	// 任务跟踪：记录最后完成的任务，用于无任务错误报告
	var lastCompletedTask []*task

	// 主执行循环：Pregel 风格的消息传递和迭代计算
	for step := 0; ; step++ {
		// 上下文取消检查：监听执行取消信号
		select {
		case <-ctx.Done():
			_, _ = tm.waitAll()
			return nil, newGraphRunError(fmt.Errorf("context has been canceled: %w", ctx.Err()))
		default:
		}
		// 步数限制：Pregel 模式防止无限循环
		if !r.dag && step >= maxSteps {
			return nil, newGraphRunError(ErrExceedMaxSteps)
		}

		// 执行步骤：
		//  1. 提交后续任务到任务池
		//  2. 等待任务完成
		//  3. 计算下一轮任务

		// 步骤1：提交任务
		err = tm.submit(nextTasks)
		if err != nil {
			return nil, newGraphRunError(fmt.Errorf("failed to submit tasks: %w", err))
		}

		var totalCanceledTasks []*task

		// 步骤2：等待任务完成
		completedTasks, canceled, canceledTasks := tm.wait()
		totalCanceledTasks = append(totalCanceledTasks, canceledTasks...)
		tempInfo := newInterruptTempInfo()
		// 处理取消任务：区分重试节点和中断后节点
		if canceled {
			if len(canceledTasks) > 0 {
				// 取消任务作为重试节点
				for _, t := range canceledTasks {
					tempInfo.interruptRerunNodes = append(tempInfo.interruptRerunNodes, t.nodeKey)
				}
			} else {
				// 无取消任务，作为中断后节点处理
				for _, t := range completedTasks {
					tempInfo.interruptAfterNodes = append(tempInfo.interruptAfterNodes, t.nodeKey)
				}
			}
		}

		// 解析中断完成的任务：处理子图中断和重试错误
		err = r.resolveInterruptCompletedTasks(tempInfo, completedTasks)
		if err != nil {
			return nil, err // 错误已包装
		}

		// 复杂中断处理：包含子图中断或重试节点
		if len(tempInfo.subGraphInterrupts)+len(tempInfo.interruptRerunNodes) > 0 {
			var newCompletedTasks []*task
			newCompletedTasks, canceledTasks = tm.waitAll()
			totalCanceledTasks = append(totalCanceledTasks, canceledTasks...)
			for _, ct := range canceledTasks {
				// 处理超时任务为重试
				tempInfo.interruptRerunNodes = append(tempInfo.interruptRerunNodes, ct.nodeKey)
			}

			err = r.resolveInterruptCompletedTasks(tempInfo, newCompletedTasks)
			if err != nil {
				return nil, err // 错误已包装
			}

			// 子图中断处理：
			// - 保存其他完成任务到通道
			// - 将中断子图保存为带 SkipPreHandler 的后续任务
			// - 报告当前图中断信息
			return nil, r.handleInterruptWithSubGraphAndRerunNodes(
				ctx,
				tempInfo,
				append(append(completedTasks, newCompletedTasks...), totalCanceledTasks...), // 取消任务按重试处理
				writeToCheckPointID,
				isSubGraph,
				cm,
				isStream,
			)
		}

		// 任务完成检查：无任务可执行时报告错误
		if len(completedTasks) == 0 {
			return nil, newGraphRunError(fmt.Errorf("no tasks to execute, last completed nodes: %v", printTask(lastCompletedTask)))
		}
		lastCompletedTask = completedTasks

		// 步骤3：计算下一轮任务
		var isEnd bool
		nextTasks, result, isEnd, err = r.calculateNextTasks(ctx, completedTasks, isStream, cm, optMap)
		if err != nil {
			return nil, newGraphRunError(fmt.Errorf("failed to calculate next tasks: %w", err))
		}
		if isEnd {
			return result, nil
		}

		// 检查后续中断：监控后续任务中的中断节点
		tempInfo.interruptBeforeNodes = getHitKey(nextTasks, r.interruptBeforeNodes)

		// 简单中断处理：处理节点前/后中断
		if len(tempInfo.interruptBeforeNodes) > 0 || len(tempInfo.interruptAfterNodes) > 0 {
			var newCompletedTasks []*task
			newCompletedTasks, canceledTasks = tm.waitAll()
			totalCanceledTasks = append(totalCanceledTasks, canceledTasks...)
			for _, ct := range canceledTasks {
				tempInfo.interruptRerunNodes = append(tempInfo.interruptRerunNodes, ct.nodeKey)
			}

			err = r.resolveInterruptCompletedTasks(tempInfo, newCompletedTasks)
			if err != nil {
				return nil, err // 错误已包装
			}

			// 复杂中断：包含子图中断或重试节点
			if len(tempInfo.subGraphInterrupts)+len(tempInfo.interruptRerunNodes) > 0 {
				return nil, r.handleInterruptWithSubGraphAndRerunNodes(
					ctx,
					tempInfo,
					append(append(completedTasks, newCompletedTasks...), totalCanceledTasks...),
					writeToCheckPointID,
					isSubGraph,
					cm,
					isStream,
				)
			}

			// 重新计算中断后的任务
			var newNextTasks []*task
			newNextTasks, result, isEnd, err = r.calculateNextTasks(ctx, newCompletedTasks, isStream, cm, optMap)
			if err != nil {
				return nil, newGraphRunError(fmt.Errorf("failed to calculate next tasks: %w", err))
			}

			if isEnd {
				return result, nil
			}

			// 合并中断前后节点列表
			tempInfo.interruptBeforeNodes = append(tempInfo.interruptBeforeNodes, getHitKey(newNextTasks, r.interruptBeforeNodes)...)

			// 简单中断：仅处理节点前/后中断
			return nil, r.handleInterrupt(ctx, tempInfo, append(nextTasks, newNextTasks...), cm.channels, isStream, isSubGraph, writeToCheckPointID)
		}
	}
}

func (r *runner) restoreFromCheckPoint(
	ctx context.Context,
	path NodePath,
	sm StateModifier,
	cp *checkpoint,
	isStream bool,
	cm *channelManager,
	optMap map[string][]any,
) (context.Context, []*task, error) {
	err := r.checkPointer.restoreCheckPoint(cp, isStream)
	if err != nil {
		return ctx, nil, newGraphRunError(fmt.Errorf("restore checkpoint fail: %w", err))
	}

	err = cm.loadChannels(cp.Channels)
	if err != nil {
		return ctx, nil, newGraphRunError(err)
	}
	if sm != nil && cp.State != nil {
		err = sm(ctx, path, cp.State)
		if err != nil {
			return ctx, nil, newGraphRunError(fmt.Errorf("state modifier fail: %w", err))
		}
	}
	if cp.State != nil {
		ctx = context.WithValue(ctx, stateKey{}, &internalState{state: cp.State})
	}

	nextTasks, err := r.restoreTasks(ctx, cp.Inputs, cp.SkipPreHandler, cp.ToolsNodeExecutedTools, cp.RerunNodes, isStream, optMap) // should restore after set state to context
	if err != nil {
		return ctx, nil, newGraphRunError(fmt.Errorf("restore tasks fail: %w", err))
	}
	return ctx, nextTasks, nil
}

func newInterruptTempInfo() *interruptTempInfo {
	return &interruptTempInfo{
		subGraphInterrupts:     map[string]*subGraphInterruptError{},
		interruptRerunExtra:    map[string]any{},
		interruptExecutedTools: make(map[string]map[string]string),
	}
}

type interruptTempInfo struct {
	subGraphInterrupts     map[string]*subGraphInterruptError
	interruptRerunNodes    []string
	interruptBeforeNodes   []string
	interruptAfterNodes    []string
	interruptRerunExtra    map[string]any
	interruptExecutedTools map[string]map[string]string
}

func (r *runner) resolveInterruptCompletedTasks(tempInfo *interruptTempInfo, completedTasks []*task) (err error) {
	for _, completedTask := range completedTasks {
		if completedTask.err != nil {
			if info := isSubGraphInterrupt(completedTask.err); info != nil {
				tempInfo.subGraphInterrupts[completedTask.nodeKey] = info
				continue
			}
			extra, ok := IsInterruptRerunError(completedTask.err)
			if ok {
				tempInfo.interruptRerunNodes = append(tempInfo.interruptRerunNodes, completedTask.nodeKey)
				if extra != nil {
					tempInfo.interruptRerunExtra[completedTask.nodeKey] = extra

					// save tool node info
					if completedTask.call.action.meta.component == ComponentOfToolsNode {
						if e, ok := extra.(*ToolsInterruptAndRerunExtra); ok {
							tempInfo.interruptExecutedTools[completedTask.nodeKey] = e.ExecutedTools
						}
					}
				}
				continue
			}
			return wrapGraphNodeError(completedTask.nodeKey, completedTask.err)
		}

		for _, key := range r.interruptAfterNodes {
			if key == completedTask.nodeKey {
				tempInfo.interruptAfterNodes = append(tempInfo.interruptAfterNodes, key)
				break
			}
		}
	}
	return nil
}

func getHitKey(tasks []*task, keys []string) []string {
	var ret []string
	for _, t := range tasks {
		for _, key := range keys {
			if key == t.nodeKey {
				ret = append(ret, t.nodeKey)
			}
		}
	}
	return ret
}

func (r *runner) handleInterrupt(
	ctx context.Context,
	tempInfo *interruptTempInfo,
	nextTasks []*task,
	channels map[string]channel,
	isStream bool,
	isSubGraph bool,
	checkPointID *string,
) error {
	cp := &checkpoint{
		Channels:       channels,
		Inputs:         make(map[string]any),
		SkipPreHandler: map[string]bool{},
	}
	if r.runCtx != nil {
		// 当前图具有启用状态
		if state, ok := ctx.Value(stateKey{}).(*internalState); ok {
			cp.State = state.state
		}
	}
	intInfo := &InterruptInfo{
		State:           cp.State,
		AfterNodes:      tempInfo.interruptAfterNodes,
		BeforeNodes:     tempInfo.interruptBeforeNodes,
		RerunNodes:      tempInfo.interruptRerunNodes,
		RerunNodesExtra: tempInfo.interruptRerunExtra,
		SubGraphs:       make(map[string]*InterruptInfo),
	}
	for _, t := range nextTasks {
		cp.Inputs[t.nodeKey] = t.input
	}
	err := r.checkPointer.convertCheckPoint(cp, isStream)
	if err != nil {
		return fmt.Errorf("failed to convert checkpoint: %w", err)
	}
	if isSubGraph {
		return &subGraphInterruptError{
			Info:       intInfo,
			CheckPoint: cp,
		}
	} else if checkPointID != nil {
		err := r.checkPointer.set(ctx, *checkPointID, cp)
		if err != nil {
			return fmt.Errorf("failed to set checkpoint: %w, checkPointID: %s", err, *checkPointID)
		}
	}
	return &interruptError{Info: intInfo}
}

func (r *runner) handleInterruptWithSubGraphAndRerunNodes(
	ctx context.Context,
	tempInfo *interruptTempInfo,
	completeTasks []*task,
	checkPointID *string,
	isSubGraph bool,
	cm *channelManager,
	isStream bool,
) error {
	var rerunTasks, subgraphTasks, otherTasks []*task
	skipPreHandler := map[string]bool{}
	for _, t := range completeTasks {
		if _, ok := tempInfo.subGraphInterrupts[t.nodeKey]; ok {
			subgraphTasks = append(subgraphTasks, t)
			skipPreHandler[t.nodeKey] = true // subgraph won't run pre-handler again, but rerun nodes will
			continue
		}
		rerun := false
		for _, key := range tempInfo.interruptRerunNodes {
			if key == t.nodeKey {
				rerunTasks = append(rerunTasks, t)
				rerun = true
				break
			}
		}
		if !rerun {
			otherTasks = append(otherTasks, t)
		}
	}

	// forward completed tasks
	toValue, controls, err := r.resolveCompletedTasks(ctx, otherTasks, isStream, cm)
	if err != nil {
		return fmt.Errorf("failed to resolve completed tasks in interrupt: %w", err)
	}
	err = cm.updateValues(ctx, toValue)
	if err != nil {
		return fmt.Errorf("failed to update values in interrupt: %w", err)
	}
	err = cm.updateDependencies(ctx, controls)
	if err != nil {
		return fmt.Errorf("failed to update dependencies in interrupt: %w", err)
	}

	cp := &checkpoint{
		Channels:               cm.channels,
		Inputs:                 make(map[string]any),
		SkipPreHandler:         skipPreHandler,
		ToolsNodeExecutedTools: tempInfo.interruptExecutedTools,
		SubGraphs:              make(map[string]*checkpoint),
	}
	if r.runCtx != nil {
		// 当前图具有启用状态
		if state, ok := ctx.Value(stateKey{}).(*internalState); ok {
			cp.State = state.state
		}
	}

	intInfo := &InterruptInfo{
		State:           cp.State,
		BeforeNodes:     tempInfo.interruptBeforeNodes,
		AfterNodes:      tempInfo.interruptAfterNodes,
		RerunNodes:      tempInfo.interruptRerunNodes,
		RerunNodesExtra: tempInfo.interruptRerunExtra,
		SubGraphs:       make(map[string]*InterruptInfo),
	}
	for _, t := range subgraphTasks {
		cp.RerunNodes = append(cp.RerunNodes, t.nodeKey)
		cp.SubGraphs[t.nodeKey] = tempInfo.subGraphInterrupts[t.nodeKey].CheckPoint
		intInfo.SubGraphs[t.nodeKey] = tempInfo.subGraphInterrupts[t.nodeKey].Info
	}
	for _, t := range rerunTasks {
		cp.RerunNodes = append(cp.RerunNodes, t.nodeKey)
	}
	err = r.checkPointer.convertCheckPoint(cp, isStream)
	if err != nil {
		return fmt.Errorf("failed to convert checkpoint: %w", err)
	}
	if isSubGraph {
		return &subGraphInterruptError{
			Info:       intInfo,
			CheckPoint: cp,
		}
	} else if checkPointID != nil {
		err = r.checkPointer.set(ctx, *checkPointID, cp)
		if err != nil {
			return fmt.Errorf("failed to set checkpoint: %w, checkPointID: %s", err, *checkPointID)
		}
	}
	return &interruptError{Info: intInfo}
}

// calculateNextTasks 计算后续任务 - 执行循环的核心调度逻辑
// 设计意图：根据已完成的任务，计算并准备下一轮需要执行的任务
// 算法流程：
//  1. 解析完成结果：提取任务输出数据和依赖关系
//  2. 更新通道状态：通知下游节点数据就绪
//  3. 检查终止条件：如果到达 END 节点，提前返回
//  4. 创建新任务：为就绪节点生成执行任务
//
// 返回：(后续任务列表, 结果值, 是否结束, 错误)
func (r *runner) calculateNextTasks(ctx context.Context, completedTasks []*task, isStream bool, cm *channelManager, optMap map[string][]any) ([]*task, any, bool, error) {
	writeChannelValues, controls, err := r.resolveCompletedTasks(ctx, completedTasks, isStream, cm)
	if err != nil {
		return nil, nil, false, err
	}
	nodeMap, err := cm.updateAndGet(ctx, writeChannelValues, controls)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to update and get channels: %w", err)
	}
	var nextTasks []*task
	if len(nodeMap) > 0 {
		// 检查是否到达 END 节点（终止条件）
		if v, ok := nodeMap[END]; ok {
			return nil, v, true, nil
		}

		// 创建下一批待执行任务
		nextTasks, err = r.createTasks(ctx, nodeMap, optMap)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to create tasks: %w", err)
		}
	}
	return nextTasks, nil, false, nil
}

func (r *runner) createTasks(ctx context.Context, nodeMap map[string]any, optMap map[string][]any) ([]*task, error) {
	var nextTasks []*task
	for nodeKey, nodeInput := range nodeMap {
		call, ok := r.chanSubscribeTo[nodeKey]
		if !ok {
			return nil, fmt.Errorf("node[%s] has not been registered", nodeKey)
		}

		if call.action.nodeInfo != nil && call.action.nodeInfo.compileOption != nil {
			ctx = forwardCheckPoint(ctx, nodeKey)
		}

		nextTasks = append(nextTasks, &task{
			ctx:     setNodeKey(ctx, nodeKey),
			nodeKey: nodeKey,
			call:    call,
			input:   nodeInput,
			option:  optMap[nodeKey],
		})
	}
	return nextTasks, nil
}

func getCheckPointInfo(opts ...Option) (checkPointID *string, writeToCheckPointID *string, stateModifier StateModifier, forceNewRun bool) {
	for _, opt := range opts {
		if opt.checkPointID != nil {
			checkPointID = opt.checkPointID
		}
		if opt.writeToCheckPointID != nil {
			writeToCheckPointID = opt.writeToCheckPointID
		}
		if opt.stateModifier != nil {
			stateModifier = opt.stateModifier
		}
		forceNewRun = opt.forceNewRun
	}
	if writeToCheckPointID == nil {
		writeToCheckPointID = checkPointID
	}
	return
}

func (r *runner) restoreTasks(
	ctx context.Context,
	inputs map[string]any,
	skipPreHandler map[string]bool,
	toolNodeExecutedTools map[string]map[string]string,
	rerunNodes []string,
	isStream bool,
	optMap map[string][]any) ([]*task, error) {
	ret := make([]*task, 0, len(inputs))
	for _, key := range rerunNodes {
		call, ok := r.chanSubscribeTo[key]
		if !ok {
			return nil, fmt.Errorf("channel[%s] from checkpoint is not registered", key)
		}
		if isStream {
			inputs[key] = call.action.inputEmptyStream()
		} else {
			inputs[key] = call.action.inputZeroValue()
		}
	}
	for key, input := range inputs {
		call, ok := r.chanSubscribeTo[key]
		if !ok {
			return nil, fmt.Errorf("channel[%s] from checkpoint is not registered", key)
		}

		if call.action.nodeInfo != nil && call.action.nodeInfo.compileOption != nil {
			// sub graph
			ctx = forwardCheckPoint(ctx, key)
		}

		newTask := &task{
			ctx:            setNodeKey(ctx, key),
			nodeKey:        key,
			call:           call,
			input:          input,
			option:         nil,
			skipPreHandler: skipPreHandler[key],
		}
		if opt, ok := optMap[key]; ok {
			newTask.option = opt
		}
		if executedTools, ok := toolNodeExecutedTools[key]; ok {
			newTask.option = append(newTask.option, withExecutedTools(executedTools))
		}

		ret = append(ret, newTask)
	}
	return ret, nil
}

// resolveCompletedTasks 解析已完成任务 - 提取输出数据和更新依赖关系
// 设计意图：将已完成任务的结果传播给下游节点，并更新执行依赖图
// 算法流程：
//  1. 控制依赖更新：为每个控制边添加依赖关系
//  2. 分支计算：执行条件分支，确定后续节点路径
//  3. 数据复制：多后继节点时复制数据，确保每个节点有独立输入
//  4. 通道更新：准备写入下游节点的数据映射
//
// 返回：(节点输入数据映射, 新依赖关系映射, 错误)
func (r *runner) resolveCompletedTasks(ctx context.Context, completedTasks []*task, isStream bool, cm *channelManager) (map[string]map[string]any, map[string][]string, error) {
	writeChannelValues := make(map[string]map[string]any)
	newDependencies := make(map[string][]string)
	for _, t := range completedTasks {
		for _, key := range t.call.controls {
			newDependencies[key] = append(newDependencies[key], t.nodeKey)
		}

		// 更新通道数据和新任务计算
		vs := copyItem(t.output, len(t.call.writeTo)+len(t.call.writeToBranches)*2)
		nextNodeKeys, err := r.calculateBranch(ctx, t.nodeKey, t.call,
			vs[len(t.call.writeTo)+len(t.call.writeToBranches):], isStream, cm)
		if err != nil {
			return nil, nil, fmt.Errorf("calculate next step fail, node: %s, error: %w", t.nodeKey, err)
		}

		for _, key := range nextNodeKeys {
			newDependencies[key] = append(newDependencies[key], t.nodeKey)
		}
		nextNodeKeys = append(nextNodeKeys, t.call.writeTo...)

		// 分支生成多个后继时，需相应复制输入数据
		if len(nextNodeKeys) > 0 {
			toCopyNum := len(nextNodeKeys) - len(t.call.writeTo) - len(t.call.writeToBranches)
			nVs := copyItem(vs[len(t.call.writeTo)+len(t.call.writeToBranches)-1], toCopyNum+1)
			vs = append(vs[:len(t.call.writeTo)+len(t.call.writeToBranches)-1], nVs...)

			for i, next := range nextNodeKeys {
				if _, ok := writeChannelValues[next]; !ok {
					writeChannelValues[next] = make(map[string]any)
				}
				writeChannelValues[next][t.nodeKey] = vs[i]
			}
		}
	}
	return writeChannelValues, newDependencies, nil
}

// calculateBranch 计算分支路径 - 条件分支的路径选择算法
// 设计意图：执行条件分支逻辑，确定哪些后续节点被选中执行
// 算法流程：
//  1. 分支预处理：应用分支前置处理器（类型转换等）
//  2. 条件执行：执行每个分支的条件判断函数（同步或流式）
//  3. 路径收集：收集被选中的后续节点路径
//  4. 跳过节点：检测被所有分支跳过的节点
//  5. 去重处理：避免被部分分支选中但被其他分支跳过的节点被错误跳过
//
// 返回：选中的后续节点键列表
func (r *runner) calculateBranch(ctx context.Context, curNodeKey string, startChan *chanCall, input []any, isStream bool, cm *channelManager) ([]string, error) {
	if len(input) < len(startChan.writeToBranches) {
		// 不可达：输入长度不足
		return nil, errors.New("calculate next input length is shorter than branches")
	}

	ret := make([]string, 0, len(startChan.writeToBranches))

	skippedNodes := make(map[string]struct{})
	for i, branch := range startChan.writeToBranches {
		// 检查分支输入类型
		var err error
		input[i], err = r.preBranchHandlerManager.handle(curNodeKey, i, input[i], isStream)
		if err != nil {
			return nil, fmt.Errorf("branch[%s]-[%d] pre handler fail: %w", curNodeKey, branch.idx, err)
		}

		// 处理分支输出
		var ws []string
		if isStream {
			ws, err = branch.collect(ctx, input[i].(streamReader))
			if err != nil {
				return nil, fmt.Errorf("branch collect run error: %w", err)
			}
		} else {
			ws, err = branch.invoke(ctx, input[i])
			if err != nil {
				return nil, fmt.Errorf("branch invoke run error: %w", err)
			}
		}

		// 标记被跳过的节点
		for node := range branch.endNodes {
			skipped := true
			for _, w := range ws {
				if node == w {
					skipped = false
					break
				}
			}
			if skipped {
				skippedNodes[node] = struct{}{}
			}
		}

		ret = append(ret, ws...)
	}

	// 多分支场景优化：
	// 当节点有多个分支时，可能出现某些后续节点被部分分支选中而被其他分支跳过的情况
	// 此时该节点不应该被跳过
	var skippedNodeList []string
	for _, selected := range ret {
		if _, ok := skippedNodes[selected]; ok {
			delete(skippedNodes, selected)
		}
	}
	for skipped := range skippedNodes {
		skippedNodeList = append(skippedNodeList, skipped)
	}

	err := cm.reportBranch(curNodeKey, skippedNodeList)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (r *runner) initTaskManager(runWrapper runnableCallWrapper, cancelVal *graphCancelChanVal, opts ...Option) *taskManager {
	tm := &taskManager{
		runWrapper:   runWrapper,
		opts:         opts,
		needAll:      !r.eager,
		done:         internal.NewUnboundedChan[*task](),
		runningTasks: make(map[string]*task),
	}
	if cancelVal != nil {
		tm.cancelCh = cancelVal.ch
	}
	return tm
}

// initChannelManager 初始化通道管理器 - 构建节点间通信通道
// 设计意图：为每个节点创建独立的通信通道，支持 Pregel/DAG 两种构建策略
// 构建流程：
//  1. 策略选择：根据图类型选择通道构建器（默认 Pregel）
//  2. 节点通道：为每个节点创建通信通道，注入零值和空流工厂
//  3. END通道：为终止节点创建特殊通道
//  4. 依赖转换：将依赖列表转换为集合结构以提升查找性能
//  5. 合并配置：为多输入节点应用扇入合并策略
func (r *runner) initChannelManager(isStream bool) *channelManager {
	builder := r.chanBuilder
	if builder == nil {
		builder = pregelChannelBuilder
	}

	chs := make(map[string]channel)
	for ch := range r.chanSubscribeTo {
		chs[ch] = builder(r.controlPredecessors[ch], r.dataPredecessors[ch], r.chanSubscribeTo[ch].action.inputZeroValue, r.chanSubscribeTo[ch].action.inputEmptyStream)
	}

	chs[END] = builder(r.controlPredecessors[END], r.dataPredecessors[END], r.outputZeroValue, r.outputEmptyStream)

	dataPredecessors := make(map[string]map[string]struct{})
	for k, vs := range r.dataPredecessors {
		dataPredecessors[k] = make(map[string]struct{})
		for _, v := range vs {
			dataPredecessors[k][v] = struct{}{}
		}
	}
	controlPredecessors := make(map[string]map[string]struct{})
	for k, vs := range r.controlPredecessors {
		controlPredecessors[k] = make(map[string]struct{})
		for _, v := range vs {
			controlPredecessors[k][v] = struct{}{}
		}
	}

	for k, v := range chs {
		if cfg, ok := r.mergeConfigs[k]; ok {
			v.setMergeConfig(cfg)
		}
	}

	return &channelManager{
		isStream:            isStream,
		channels:            chs,
		successors:          r.successors,
		dataPredecessors:    dataPredecessors,
		controlPredecessors: controlPredecessors,

		edgeHandlerManager:    r.edgeHandlerManager,
		preNodeHandlerManager: r.preNodeHandlerManager,
	}
}

// toComposableRunnable 转换为可执行对象 - 实现接口适配
// 设计意图：将 runner 适配为 composableRunnable，实现统一的可执行接口
// 适配内容：
//   - i 字段：同步执行接口，转换参数并调用 runner.invoke
//   - t 字段：流式执行接口，转换参数并调用 runner.transform
//   - 类型系统：继承输入输出类型和通用辅助系统
//   - 选项透传：optionType 为 nil，支持所有选项透传
func (r *runner) toComposableRunnable() *composableRunnable {
	cr := &composableRunnable{
		i: func(ctx context.Context, input any, opts ...any) (output any, err error) {
			tos, err := convertOption[Option](opts...)
			if err != nil {
				return nil, err
			}
			return r.invoke(ctx, input, tos...)
		},
		t: func(ctx context.Context, input streamReader, opts ...any) (output streamReader, err error) {
			tos, err := convertOption[Option](opts...)
			if err != nil {
				return nil, err
			}
			return r.transform(ctx, input, tos...)
		},

		inputType:     r.inputType,
		outputType:    r.outputType,
		genericHelper: r.genericHelper,
		optionType:    nil, // 选项类型为 nil，图将透传所有选项
	}

	return cr
}

// copyItem 将一个 item 复制 n 次，返回包含 n 个元素的切片
//
// 特殊处理逻辑：
//  1. 当 n < 2 时，直接返回包含该 item 的单元素切片，避免不必要的复制
//  2. 当 item 实现 streamReader 接口时，调用其专门的 copy 方法进行深拷贝，
//     确保流状态的正确复制
//  3. 对于普通对象，直接将同一个引用填充到切片的每个位置
//
// 这种设计既保证了流对象的正确复制，又避免了对普通对象的过度复制开销
func copyItem(item any, n int) []any {
	if n < 2 {
		return []any{item}
	}

	ret := make([]any, n)
	if s, ok := item.(streamReader); ok {
		ss := s.copy(n)
		for i := range ret {
			ret[i] = ss[i]
		}

		return ret
	}

	for i := range ret {
		ret[i] = item
	}

	return ret
}

// printTask 任务切片格式化函数 - 将任务列表转换为可读字符串
// 设计意图：为调试和日志输出提供标准化的任务列表字符串表示
// 格式化规则：
//  1. 空列表返回 "[]"，表示无任务
//  2. 非空列表格式为 "[node1, node2, ..., nodeN]"
//  3. 使用 strings.Builder 优化字符串拼接性能
//  4. 提取每个任务的 nodeKey 作为标识符
//
// 应用场景：
//   - 调试时打印任务执行进度
//   - 错误信息中报告当前任务状态
//   - 日志记录任务流程追踪
func printTask(ts []*task) string {
	if len(ts) == 0 {
		return "[]"
	}
	sb := strings.Builder{}
	sb.WriteString("[")
	for i := 0; i < len(ts)-1; i++ {
		sb.WriteString(ts[i].nodeKey)
		sb.WriteString(", ")
	}
	sb.WriteString(ts[len(ts)-1].nodeKey)
	sb.WriteString("]")
	return sb.String()
}
