/*
 * graph_manager.go - 图管理器，负责图的运行时调度和任务管理
 *
 * 核心组件：
 *   - channelManager: 通道管理器，管理节点间的数据流和控制流
 *   - taskManager: 任务管理器，负责任务的提交、执行和等待
 *   - edgeHandlerManager: 边处理器管理器，处理边上的数据转换
 *
 * 设计特点：
 *   - 并发调度: 支持多任务并发执行，自动管理任务依赖
 *   - 数据流控制: 区分数据依赖和控制依赖，精确控制执行顺序
 *   - 中断支持: 支持用户中断和超时控制
 *   - 流式处理: 统一处理流式和非流式数据传递
 *   - 边处理器: 支持在边上添加数据转换处理器
 *
 * 执行流程：
 *   1. channelManager 管理节点间的数据和控制依赖
 *   2. 当节点就绪时，创建任务提交到 taskManager
 *   3. taskManager 并发执行任务或同步执行单个任务
 *   4. 任务完成后更新 channel，触发后续节点执行
 *   5. 支持用户中断和超时控制机制
 */

package compose

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/favbox/eino/internal"
	"github.com/favbox/eino/internal/safe"
)

// channel 是节点通道接口，管理节点的输入数据和执行依赖
type channel interface {
	reportValues(map[string]any) error                        // 报告输入值
	reportDependencies([]string)                              // 报告控制依赖
	reportSkip([]string) bool                                 // 报告跳过状态
	get(bool, string, *edgeHandlerManager) (any, bool, error) // 获取就绪的输入数据
	convertValues(fn func(map[string]any) error) error        // 转换输入值
	load(channel) error                                       // 从另一个 channel 加载状态

	setMergeConfig(FanInMergeConfig) // 设置扇入合并配置
}

// edgeHandlerManager 管理边上的数据处理器。
// 在数据从一个节点流向另一个节点时，可以对数据进行转换处理。
type edgeHandlerManager struct {
	h map[string]map[string][]handlerPair // 边处理器映射：from -> to -> handlers
}

// handle 处理从 from 节点到 to 节点的边上的数据转换
func (e *edgeHandlerManager) handle(from, to string, value any, isStream bool) (any, error) {
	if _, ok := e.h[from]; !ok {
		return value, nil
	}
	if _, ok := e.h[from][to]; !ok {
		return value, nil
	}
	if isStream {
		for _, v := range e.h[from][to] {
			value = v.transform(value.(streamReader))
		}
	} else {
		for _, v := range e.h[from][to] {
			var err error
			value, err = v.invoke(value)
			if err != nil {
				return nil, err
			}
		}
	}
	return value, nil
}

// preNodeHandlerManager 管理节点前置处理器。
// 在节点执行前对输入数据进行预处理。
type preNodeHandlerManager struct {
	h map[string][]handlerPair // 节点前置处理器映射：nodeKey -> handlers
}

// handle 处理节点执行前的数据预处理
func (p *preNodeHandlerManager) handle(nodeKey string, value any, isStream bool) (any, error) {
	if _, ok := p.h[nodeKey]; !ok {
		return value, nil
	}
	if isStream {
		for _, v := range p.h[nodeKey] {
			value = v.transform(value.(streamReader))
		}
	} else {
		for _, v := range p.h[nodeKey] {
			var err error
			value, err = v.invoke(value)
			if err != nil {
				return nil, err
			}
		}
	}
	return value, nil
}

// preBranchHandlerManager 管理分支前置处理器。
// 在分支条件判断前对数据进行预处理。
type preBranchHandlerManager struct {
	h map[string][][]handlerPair // 分支前置处理器映射：nodeKey -> branchIdx -> handlers
}

// handle 处理分支条件判断前的数据预处理
func (p *preBranchHandlerManager) handle(nodeKey string, idx int, value any, isStream bool) (any, error) {
	if _, ok := p.h[nodeKey]; !ok {
		return value, nil
	}
	if isStream {
		for _, v := range p.h[nodeKey][idx] {
			value = v.transform(value.(streamReader))
		}
	} else {
		for _, v := range p.h[nodeKey][idx] {
			var err error
			value, err = v.invoke(value)
			if err != nil {
				return nil, err
			}
		}
	}
	return value, nil
}

// channelManager 管理图中所有节点的通道，协调数据流和控制流。
// 维护节点间的依赖关系，决定节点的执行顺序。
type channelManager struct {
	isStream bool               // 是否流式模式
	channels map[string]channel // 节点通道映射

	successors          map[string][]string            // 后继节点映射
	dataPredecessors    map[string]map[string]struct{} // 数据前驱节点映射
	controlPredecessors map[string]map[string]struct{} // 控制前驱节点映射

	edgeHandlerManager    *edgeHandlerManager    // 边处理器管理器
	preNodeHandlerManager *preNodeHandlerManager // 节点前置处理器管理器
}

// loadChannels 从另一组通道加载状态，用于恢复执行
func (c *channelManager) loadChannels(channels map[string]channel) error {
	for key, ch := range c.channels {
		if nCh, ok := channels[key]; ok {
			if err := ch.load(nCh); err != nil {
				return fmt.Errorf("load channel[%s] fail: %w", key, err)
			}
		}
	}
	return nil
}

// updateValues 更新节点通道的输入值
func (c *channelManager) updateValues(_ context.Context, values map[string] /*to*/ map[string] /*from*/ any) error {
	for target, fromMap := range values {
		toChannel, ok := c.channels[target]
		if !ok {
			return fmt.Errorf("target channel doesn't existed: %s", target)
		}
		dps, ok := c.dataPredecessors[target]
		if !ok {
			dps = map[string]struct{}{}
		}
		nFromMap := make(map[string]any, len(fromMap))
		for from, value := range fromMap {
			if _, ok = dps[from]; ok {
				nFromMap[from] = fromMap[from]
			} else {
				if sr, okk := value.(streamReader); okk {
					sr.close()
				}
			}
		}

		err := toChannel.reportValues(nFromMap)
		if err != nil {
			return fmt.Errorf("update target channel[%s] fail: %w", target, err)
		}
	}
	return nil
}

// updateDependencies 更新节点通道的控制依赖
func (c *channelManager) updateDependencies(_ context.Context, dependenciesMap map[string][]string) error {
	for target, dependencies := range dependenciesMap {
		toChannel, ok := c.channels[target]
		if !ok {
			return fmt.Errorf("target channel doesn't existed: %s", target)
		}
		cps, ok := c.controlPredecessors[target]
		if !ok {
			cps = map[string]struct{}{}
		}
		var deps []string
		for _, from := range dependencies {
			if _, ok = cps[from]; ok {
				deps = append(deps, from)
			}
		}

		toChannel.reportDependencies(deps)
	}
	return nil
}

// getFromReadyChannels 获取所有就绪节点的输入数据 - 识别可以执行的后续节点
// 返回：
//   - map[string]any: 节点键到输入数据的映射（仅包含就绪节点）
//   - error: 获取过程中的错误
//
// 执行逻辑：
//  1. 遍历所有通道，检查是否就绪
//  2. 应用边处理器对数据进行处理
//  3. 应用节点前置处理器进行预处理
//  4. 返回所有就绪节点的输入数据映射
//
// 关键机制：只有同时满足数据和依赖条件的节点才会被返回
func (c *channelManager) getFromReadyChannels(_ context.Context) (map[string]any, error) {
	result := make(map[string]any)
	for target, ch := range c.channels {
		v, ready, err := ch.get(c.isStream, target, c.edgeHandlerManager)
		if err != nil {
			return nil, fmt.Errorf("get value from ready channel[%s] fail: %w", target, err)
		}
		if ready {
			v, err = c.preNodeHandlerManager.handle(target, v, c.isStream)
			if err != nil {
				return nil, err
			}
			result[target] = v
		}
	}
	return result, nil
}

// updateAndGet 更新通道状态并获取就绪节点的输入数据。
//
// 参数：
//   - values: 节点数据更新映射
//   - dependencies: 依赖关系更新映射
//
// 返回：
//   - 就绪节点及其输入数据的映射
//   - 可能的错误
//
// 设计意图：将通道更新的三个步骤组合为一个原子操作
// 执行步骤：
//  1. 更新通道数据值
//  2. 更新通道依赖关系
//  3. 获取所有就绪节点
//
// 优势：减少外部调用，确保数据一致性和依赖关系的同步更新
func (c *channelManager) updateAndGet(ctx context.Context, values map[string]map[string]any, dependencies map[string][]string) (map[string]any, error) {
	err := c.updateValues(ctx, values)
	if err != nil {
		return nil, fmt.Errorf("update channel fail: %w", err)
	}
	err = c.updateDependencies(ctx, dependencies)
	if err != nil {
		return nil, fmt.Errorf("update channel fail: %w", err)
	}
	return c.getFromReadyChannels(ctx)
}

// reportBranch 报告分支跳过的节点，并递归传播跳过状态。
// 返回：处理过程中的错误
// 处理逻辑：
//  1. 遍历直接被跳过的节点，标记这些节点为跳过状态
//  2. 递归传播跳过效应：如果一个节点被跳过，其所有后继也可能被跳过
//  3. 构建完整的跳过节点列表，用于后续的依赖更新
//
// 算法特点：使用广度优先搜索（BFS）遍历，跳过节点的影响会沿图结构传播
// 关键点：END 节点不会被跳过，因为它不参与实际执行
func (c *channelManager) reportBranch(from string, skippedNodes []string) error {
	var nKeys []string
	for _, node := range skippedNodes {
		skipped := c.channels[node].reportSkip([]string{from})
		if skipped {
			nKeys = append(nKeys, node)
		}
	}

	for i := 0; i < len(nKeys); i++ {
		key := nKeys[i]

		if key == END {
			continue
		}
		if _, ok := c.successors[key]; !ok {
			return fmt.Errorf("unknown node: %s", key)
		}
		for _, successor := range c.successors[key] {
			skipped := c.channels[successor].reportSkip([]string{key})
			if skipped {
				nKeys = append(nKeys, successor)
			}
			// todo: detect if end node has been skipped?
		}
	}
	return nil
}

// task 表示单个节点的执行任务
type task struct {
	ctx            context.Context // 执行上下文
	nodeKey        string          // 节点键名
	call           *chanCall       // 节点调用信息
	input          any             // 输入数据
	output         any             // 输出数据
	option         []any           // 调用选项
	err            error           // 执行错误
	skipPreHandler bool            // 是否跳过前置处理器
}

// taskManager 管理任务的提交、执行和等待。
// 支持并发执行多个任务，支持用户中断和超时控制。
type taskManager struct {
	runWrapper runnableCallWrapper // 可运行对象调用包装器
	opts       []Option            // 图选项
	needAll    bool                // 是否需要等待所有任务完成，true=DAG，false=Pregel

	num          uint32                         // 运行中的任务数量
	done         *internal.UnboundedChan[*task] // 完成任务通道
	runningTasks map[string]*task               // 运行中的任务映射

	cancelCh chan *time.Duration // 中断信号通道
	canceled bool                // 是否已中断
	deadline *time.Time          // 中断超时截止时间
}

// execute 执行单个任务，捕获 panic 并发送完成信号
func (t *taskManager) execute(currentTask *task) {
	defer func() {
		panicInfo := recover()
		if panicInfo != nil {
			currentTask.output = nil
			currentTask.err = safe.NewPanicErr(panicInfo, debug.Stack())
		}

		t.done.Send(currentTask)
	}()

	ctx := initNodeCallbacks(currentTask.ctx, currentTask.nodeKey, currentTask.call.action.nodeInfo, currentTask.call.action.meta, t.opts...)
	currentTask.output, currentTask.err = t.runWrapper(ctx, currentTask.call.action, currentTask.input, currentTask.option...)
}

// submit 提交任务到任务池。
// 根据任务数量和执行模式决定是同步执行还是异步执行。
// 如果只有一个任务或需要等待所有任务，且没有中断通道，则同步执行第一个任务。
func (t *taskManager) submit(tasks []*task) error {
	if len(tasks) == 0 {
		return nil
	}

	// 同步执行优化：若无任务池且满足条件，则同步执行单个任务
	// 条件：1. 任务池为空 2. 单任务或 needAll 模式 3. 不可中断
	for i := 0; i < len(tasks); i++ {
		currentTask := tasks[i]
		err := runPreHandler(currentTask, t.runWrapper)
		if err != nil {
			// 前置处理器错误，视为任务本身失败
			currentTask.err = err
			tasks = append(tasks[:i], tasks[i+1:]...)
			i--
			t.num++
			t.done.Send(currentTask)
		}

		t.runningTasks[currentTask.nodeKey] = currentTask
	}
	if len(tasks) == 0 {
		// 所有任务的前置处理器都失败
		return nil
	}

	var syncTask *task
	if t.num == 0 && (len(tasks) == 1 || t.needAll) && t.cancelCh == nil /*如果图可以被用户中断，不应该同步运行任务*/ {
		syncTask = tasks[0]
		tasks = tasks[1:]
	}
	for _, currentTask := range tasks {
		t.num += 1
		go t.execute(currentTask)
	}
	if syncTask != nil {
		t.num += 1
		t.execute(syncTask)
	}
	return nil
}

// wait 等待任务完成。
// 根据执行模式返回完成的任务、中断标志和被中断的任务。
func (t *taskManager) wait() (tasks []*task, canceled bool, canceledTasks []*task) {
	if t.needAll {
		tasks, canceledTasks = t.waitAll()
		return tasks, t.canceled, canceledTasks
	}

	ta, success, canceled := t.waitOne()
	if canceled {
		// 已中断且超时，返回被中断的任务
		for _, rta := range t.runningTasks {
			canceledTasks = append(canceledTasks, rta)
		}
		t.runningTasks = make(map[string]*task)
		t.num = 0
		return nil, true, canceledTasks
	}
	if t.canceled {
		// 已中断但未超时，等待所有任务
		tasks, canceledTasks = t.waitAll()
		return append(tasks, ta), true, canceledTasks
	}
	if !success {
		return []*task{}, t.canceled, nil
	}

	return []*task{ta}, t.canceled, nil
}

// waitOne 等待单个任务完成
func (t *taskManager) waitOne() (ta *task, success bool, canceled bool) {
	if t.num == 0 {
		return nil, false, false
	}

	if t.cancelCh == nil {
		ta, _ = t.done.Receive()
	} else {
		ta, _, canceled = t.receive(t.done.Receive)
	}

	t.num--

	if canceled {
		return nil, false, true
	}

	delete(t.runningTasks, ta.nodeKey)
	if ta.err != nil {
		// 业务错误，跳过后置处理器
		return ta, true, false
	}
	runPostHandler(ta, t.runWrapper)
	return ta, true, false
}

// waitAll 等待所有任务完成
func (t *taskManager) waitAll() (successTasks []*task, canceledTasks []*task) {
	result := make([]*task, 0, t.num)
	for {
		ta, success, canceled := t.waitOne()
		if canceled {
			for _, rt := range t.runningTasks {
				canceledTasks = append(canceledTasks, rt)
			}
			t.runningTasks = make(map[string]*task)
			t.num = 0
			return result, canceledTasks
		}
		if !success {
			return result, nil
		}
		result = append(result, ta)
	}
}

// receive 从通道接收任务，处理中断和超时
func (t *taskManager) receive(recv func() (*task, bool)) (ta *task, closed bool, canceled bool) {
	if t.deadline != nil {
		// 已中断，在指定时间内接收
		return receiveWithDeadline(recv, *t.deadline)
	}
	if t.canceled {
		// 已中断但无超时
		ta, closed = recv()
		return ta, closed, false
	}
	if t.cancelCh != nil {
		// 尚未中断，监听中断信号同时接收
		ta, closed, canceled, t.canceled, t.deadline = receiveWithListening(recv, t.cancelCh)
		return ta, closed, canceled
	}
	// 不会中断
	ta, closed = recv()
	return ta, closed, false
}

// receiveWithDeadline 在截止时间前接收任务
func receiveWithDeadline(recv func() (*task, bool), deadline time.Time) (ta *task, closed bool, canceled bool) {
	now := time.Now()
	if deadline.Before(now) {
		return nil, false, true
	}

	timeout := deadline.Sub(now)

	resultCh := make(chan struct{}, 1)

	go func() {
		ta, closed = recv()
		resultCh <- struct{}{}
	}()

	timeoutCh := time.After(timeout)

	select {
	case <-resultCh:
		return ta, closed, false
	case <-timeoutCh:
		return nil, false, true
	}
}

// receiveWithListening 接收任务的同时监听中断信号
func receiveWithListening(recv func() (*task, bool), cancel chan *time.Duration) (*task, bool, bool, bool, *time.Time) {
	type pair struct {
		ta     *task
		closed bool
	}
	resultCh := make(chan pair, 1)
	var timeoutCh <-chan time.Time

	var deadline *time.Time
	canceled := false
	go func() {
		ta, closed := recv()
		resultCh <- pair{ta, closed}
	}()

	select {
	case p := <-resultCh:
		return p.ta, p.closed, false, false, nil
	case timeout, ok := <-cancel:
		if !ok {
			// 不可达
			break
		}
		canceled = true
		if timeout == nil {
			// 无超时的中断
			break
		}
		timeoutCh = time.After(*timeout)
		dt := time.Now().Add(*timeout)
		deadline = &dt
	}

	if timeoutCh != nil {
		select {
		case p := <-resultCh:
			return p.ta, p.closed, false, canceled, deadline
		case <-timeoutCh:
			return nil, false, true, canceled, deadline
		}
	}
	p := <-resultCh
	return p.ta, p.closed, false, canceled, nil
}

// runPreHandler 运行任务的前置处理器
func runPreHandler(ta *task, runWrapper runnableCallWrapper) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = safe.NewPanicErr(fmt.Errorf("panic in pre handler: %v", e), debug.Stack())
		}
	}()
	if ta.call.preProcessor != nil && !ta.skipPreHandler {
		nInput, err := runWrapper(ta.ctx, ta.call.preProcessor, ta.input, ta.option...)
		if err != nil {
			return fmt.Errorf("run node[%s] pre processor fail: %w", ta.nodeKey, err)
		}
		ta.input = nInput
	}
	return nil
}

// runPostHandler 运行任务的后置处理器
func runPostHandler(ta *task, runWrapper runnableCallWrapper) {
	defer func() {
		if e := recover(); e != nil {
			ta.err = safe.NewPanicErr(fmt.Errorf("panic in post handler: %v", e), debug.Stack())
		}
	}()
	if ta.call.postProcessor != nil {
		nOutput, err := runWrapper(ta.ctx, ta.call.postProcessor, ta.output, ta.option...)
		if err != nil {
			ta.err = fmt.Errorf("run node[%s] post processor fail: %w", ta.nodeKey, err)
		}
		ta.output = nOutput
	}
}
