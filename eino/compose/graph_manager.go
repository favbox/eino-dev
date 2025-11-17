package compose

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/favbox/eino/internal"
	"github.com/favbox/eino/internal/safe"
)

// channel 通道接口 - 定义节点间通信通道的标准契约
// 设计意图：抽象不同类型的通道实现（Pregel/DAG），提供统一的通信抽象
// 关键方法：
//   - reportValues: 报告数据值，更新通道内容
//   - reportDependencies: 报告依赖关系，管理前置节点
//   - reportSkip: 报告被跳过的节点，处理分支跳过逻辑
//   - get: 获取就绪节点的数据，支持边处理器
//   - convertValues: 批量转换通道值
//   - load: 加载检查点通道数据
//   - setMergeConfig: 设置扇入合并策略
type channel interface {
	reportValues(map[string]any) error
	reportDependencies([]string)
	reportSkip([]string) bool
	get(bool, string, *edgeHandlerManager) (any, bool, error)
	convertValues(fn func(map[string]any) error) error
	load(channel) error

	setMergeConfig(FanInMergeConfig)
}

// edgeHandlerManager 边处理器管理器 - 管理节点间边上的数据转换处理器
// 设计意图：集中管理边上的类型转换、字段映射等处理器，支持流式和非流式处理
// 数据结构：三层嵌套映射 - fromNode -> toNode -> handlerPair[]
// 关键能力：
//   - 批量转换：按顺序应用多个处理器
//   - 模式适配：自动适配流式/非流式模式
//   - 错误传播：处理器链中任何错误都会中断处理
type edgeHandlerManager struct {
	h map[string]map[string][]handlerPair
}

// handle 边处理器执行方法 - 应用边上所有处理器对数据进行转换
// 参数：
//   - from: 源节点键
//   - to: 目标节点键
//   - value: 待处理的数据
//   - isStream: 是否流式模式
//
// 返回：转换后的数据和可能的错误
// 处理逻辑：
//  1. 检查是否存在 from->to 的处理器
//  2. 根据执行模式选择处理器类型（流式 transform / 非流式 invoke）
//  3. 顺序执行处理器链，任何错误都会中断处理
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

// preNodeHandlerManager 节点前置处理器管理器 - 管理节点执行前的数据预处理
// 设计意图：在节点正式执行前对输入数据进行转换，支持字段映射、类型转换等
// 数据结构：nodeKey -> handlerPair[] 映射
// 应用场景：
//   - 字段映射：从结构体提取特定字段
//   - 输入转换：将外部数据格式转换为节点期望格式
//   - 验证检查：对输入数据进行预处理验证
type preNodeHandlerManager struct {
	h map[string][]handlerPair
}

// handle 节点前置处理器执行方法 - 应用节点的所有前置处理器
// 参数：
//   - nodeKey: 节点键
//   - value: 待处理的输入数据
//   - isStream: 是否流式模式
//
// 返回：预处理后的数据和可能的错误
// 执行流程：顺序应用节点的所有前置处理器，类似于边处理器
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

// preBranchHandlerManager 分支前置处理器管理器 - 管理分支条件执行前的数据处理
// 设计意图：处理分支条件的数据预处理，支持多分支场景下的独立处理器链
// 数据结构：nodeKey -> []handlerPair[] 映射（第二层是分支索引，第三层是处理器链）
// 特殊处理：支持多分支索引，每个分支可以有自己的处理器链
type preBranchHandlerManager struct {
	h map[string][][]handlerPair
}

// handle 分支前置处理器执行方法 - 应用指定分支的处理器链
// 参数：
//   - nodeKey: 节点键
//   - idx: 分支索引
//   - value: 待处理的输入数据
//   - isStream: 是否流式模式
//
// 返回：预处理后的数据和可能的错误
// 处理逻辑：
//  1. 根据 nodeKey 和 idx 定位处理器链
//  2. 顺序执行该分支的所有前置处理器
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

// channelManager 通道管理器 - 管理图中所有节点的通信通道和依赖关系
// 设计意图：统一管理节点间的数据流和控制流，是图执行时的核心调度组件
// 关键职责：
//   - 通道管理：创建、维护和更新节点间通信通道
//   - 依赖跟踪：管理数据依赖和控制依赖关系
//   - 就绪检测：判断哪些节点已准备好执行
//   - 处理器协调：协调边处理器和节点前置处理器
//
// 核心字段：
//   - isStream: 是否流式执行模式
//   - channels: 所有节点的通道映射
//   - successors: 节点的后续节点映射
//   - dataPredecessors/controlPredecessors: 数据和控制前置依赖
//   - 处理器管理器：边、节点、分支处理器
type channelManager struct {
	isStream bool
	channels map[string]channel

	successors          map[string][]string
	dataPredecessors    map[string]map[string]struct{}
	controlPredecessors map[string]map[string]struct{}

	edgeHandlerManager    *edgeHandlerManager
	preNodeHandlerManager *preNodeHandlerManager
}

// loadChannels 从检查点加载通道数据 - 恢复图执行状态
// 参数：
//   - channels: 检查点中的通道映射
//
// 返回：加载过程中的错误
// 使用场景：从检查点恢复图执行时，加载所有通道的持久化状态
// 处理逻辑：遍历当前管理的通道，从检查点中加载对应通道的数据
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

// updateValues 更新通道数据值 - 将完成的节点输出传递给后继节点
// 参数：
//   - values: 目标节点 -> (源节点 -> 数据值) 的映射
//
// 返回：更新过程中的错误
// 执行流程：
//  1. 遍历所有需要更新的目标节点
//  2. 验证目标通道存在
//  3. 检查数据前置依赖，过滤无效的数据源
//  4. 关闭无关的流式数据，防止资源泄漏
//  5. 向目标通道报告有效的数据值
//
// 关键处理：只接受有数据依赖关系的数据源，无关数据会被丢弃并关闭流
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

// updateDependencies 更新通道依赖关系 - 管理节点执行顺序的控制依赖
// 参数：
//   - dependenciesMap: 目标节点 -> 依赖源节点列表 的映射
//
// 返回：更新过程中的错误
// 执行逻辑：
//  1. 遍历所有需要更新依赖的目标节点
//  2. 验证目标通道存在
//  3. 检查控制前置依赖，过滤无效的依赖源
//  4. 向目标通道报告有效的依赖列表
//
// 用途：确保节点按照正确的顺序执行，控制依赖不影响数据传递
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

// getFromReadyChannels 获取就绪通道的数据 - 识别可以执行的后续节点
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

// updateAndGet 更新通道并获取就绪节点 - 一站式通道更新和就绪检测
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

// reportBranch 报告分支跳过情况 - 处理分支中被跳过的节点及其级联影响
// 参数：
//   - from: 分支起始节点
//   - skippedNodes: 直接被跳过的节点列表
//
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

// task 任务结构体 - 封装节点执行的所有上下文和状态
// 设计意图：统一管理任务执行的所有信息，包括输入输出、选项、错误等
// 关键字段：
//   - ctx: 执行上下文，包含节点路径、状态等
//   - nodeKey: 节点键标识
//   - call: 节点的通道调用信息（执行器、连接关系、处理器）
//   - input/output: 任务的输入和输出数据
//   - option: 运行时选项，支持节点级配置
//   - err: 任务执行错误
//   - skipPreHandler: 跳过前置处理器标记（用于检查点恢复）
type task struct {
	ctx            context.Context
	nodeKey        string
	call           *chanCall
	input          any
	output         any
	option         []any
	err            error
	skipPreHandler bool
}

// taskManager 任务管理器 - 负责任务的提交、执行和等待，是图执行的调度核心。
// 设计意图：提供灵活的任务调度机制，支持同步/异步执行、优雅/强制取消、超时控制。
// 执行模式：
//   - needAll=true: 等待所有任务完成（DAG模式）
//   - needAll=false: 任意任务完成即可（Pregel模式）
//
// 核心字段：
//   - runWrapper: 可执行对象调用包装器（同步/流式）
//   - opts: 全局运行时选项
//   - needAll: 执行模式标记
//   - num: 当前运行的任务数量
//   - done: 任务完成通道（无界通道，避免阻塞）
//   - runningTasks: 运行中的任务映射（用于取消）
//   - cancelCh: 取消信号通道
//   - canceled: 取消状态标记
//   - deadline: 截止时间（用于超时控制）
type taskManager struct {
	runWrapper runnableCallWrapper
	opts       []Option
	needAll    bool

	num          uint32
	done         *internal.UnboundedChan[*task]
	runningTasks map[string]*task

	cancelCh chan *time.Duration
	canceled bool
	deadline *time.Time
}

// execute 任务执行方法 - 在 goroutine 中执行单个任务
// 参数：
//   - currentTask: 要执行的任务
//
// 执行流程：
//  1. 延迟恢复：捕获执行过程中的 panic，转换为安全错误
//  2. 回调初始化：初始化节点的回调处理器（OnStart, OnEnd, OnError等）
//  3. 任务执行：调用 runWrapper 执行实际任务（同步/流式）
//  4. 结果传递：通过 done 通道传递执行结果
//
// panic 处理：将 panic 转换为结构化错误，包含堆栈信息便于调试
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

// submit 提交任务池 - 负责任务的提交和调度策略选择
// 参数：
//   - tasks: 待提交的任务列表
//
// 返回：提交过程中的错误
// 调度策略：
//  1. 前置处理器验证：执行每个任务的前置处理器，失败则立即标记为失败
//  2. 同步执行优化：单任务或 needAll 模式下同步执行，减少 goroutine 开销
//  3. 异步并行执行：多任务场景下并发执行，提高吞吐量
//  4. 取消保护：可中断模式下禁止同步执行，确保响应中断信号
//
// 性能优化点：
//   - 单任务同步执行避免 goroutine 开销
//   - 无界通道保证任务提交不阻塞
//   - 前置处理器失败快速返回，减少资源占用
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
	if t.num == 0 && (len(tasks) == 1 || t.needAll) && t.cancelCh == nil /*if graph can be interrupted by user, shouldn't sync run task*/ {
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

// wait 等待任务完成 - 根据执行模式选择等待策略
// 返回：
//   - tasks: 完成的任务列表
//   - canceled: 是否发生取消
//   - canceledTasks: 被取消的任务列表
//
// 执行逻辑：
//  1. needAll 模式：等待所有任务完成（使用 waitAll）
//  2. 单任务模式：等待任意一个任务完成（使用 waitOne）
//  3. 取消处理：
//     - 超时取消：返回所有运行中的任务为取消任务
//     - 非超时取消：等待所有任务完成后再返回
//  4. 失败处理：前置处理器失败的任务不参与等待
func (t *taskManager) wait() (tasks []*task, canceled bool, canceledTasks []*task) {
	if t.needAll {
		tasks, canceledTasks = t.waitAll()
		return tasks, t.canceled, canceledTasks
	}

	ta, success, canceled := t.waitOne()
	if canceled {
		// 已取消且超时，返回取消的任务
		for _, rta := range t.runningTasks {
			canceledTasks = append(canceledTasks, rta)
		}
		t.runningTasks = make(map[string]*task)
		t.num = 0
		return nil, true, canceledTasks
	}
	if t.canceled {
		// 已取消但未超时，等待所有任务
		tasks, canceledTasks = t.waitAll()
		return append(tasks, ta), true, canceledTasks
	}
	if !success {
		return []*task{}, t.canceled, nil
	}

	return []*task{ta}, t.canceled, nil
}

// waitOne 等待任意一个任务完成 - 单任务模式的核心等待逻辑
// 返回：
//   - ta: 完成的任务
//   - success: 是否成功完成（包含业务错误）
//   - canceled: 是否被取消
//
// 执行流程：
//  1. 检查任务数量，若为 0 则直接返回
//  2. 接收完成的任务：
//     - 不可取消：直接接收（阻塞）
//     - 可取消：使用 receive 支持中断和超时
//  3. 更新计数器，减少运行中的任务数量
//  4. 处理取消：返回取消状态
//  5. 运行后置处理器：仅对无错误的任务执行
//
// 设计要点：区分业务错误和系统错误，业务错误不跳过后续处理
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

// waitAll 等待所有任务完成 - 任务池模式的等待策略
// 返回：
//   - successTasks: 成功完成的任务列表
//   - canceledTasks: 被取消的任务列表
//
// 执行逻辑：
//  1. 循环等待直到所有任务完成
//  2. 调用 waitOne 获取单个任务结果
//  3. 取消处理：一旦发生取消，立即收集所有运行中任务并返回
//  4. 失败处理：遇到失败（通常是前置处理器失败）立即返回已收集结果
//  5. 成功累计：将成功完成的任务添加到结果列表
//
// 特点：保证所有任务都有结果（完成/失败/取消），不遗漏任何任务
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

// receive 带取消和超时的接收方法 - 统一处理多种接收场景
// 参数：
//   - recv: 接收函数，从 done 通道获取任务
//
// 返回：
//   - ta: 接收到的任务
//   - closed: 通道是否已关闭
//   - canceled: 是否被取消
//
// 处理策略：
//  1. 有截止时间：使用超时接收，超过截止时间则取消
//  2. 已取消（无超时）：直接接收，不等待
//  3. 可取消：监听取消信号，支持优雅超时取消
//  4. 不可取消：直接接收，阻塞直到有结果
//
// 设计亮点：通过状态组合（deadline/canceled）灵活适配不同场景
func (t *taskManager) receive(recv func() (*task, bool)) (ta *task, closed bool, canceled bool) {
	if t.deadline != nil {
		// 已取消，在指定时间内接收
		return receiveWithDeadline(recv, *t.deadline)
	}
	if t.canceled {
		// 取消但无超时
		ta, closed = recv()
		return ta, closed, false
	}
	if t.cancelCh != nil {
		// 未取消，监听接收
		ta, closed, canceled, t.canceled, t.deadline = receiveWithListening(recv, t.cancelCh)
		return ta, closed, canceled
	}
	// 不可取消
	ta, closed = recv()
	return ta, closed, false
}

// receiveWithDeadline 带截止时间的接收 - 限时等待任务完成
// 参数：
//   - recv: 接收函数
//   - deadline: 截止时间
//
// 返回：
//   - ta: 接收到的任务（可能为 nil）
//   - closed: 通道是否关闭
//   - canceled: 是否超时取消
//
// 实现机制：
//  1. 启动接收 goroutine 和超时计时器
//  2. 使用 select 竞争：接收完成 vs 超时
//  3. 先到先得：哪个先发生就返回哪个结果
//  4. 超时处理：返回取消状态，ta 为 nil
//
// 适用场景：需要精确控制任务等待时间的场景
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

// receiveWithListening 监听取消信号的接收 - 支持优雅取消和超时控制
// 参数：
//   - recv: 接收函数
//   - cancel: 取消信号通道
//
// 返回：
//   - ta: 接收到的任务
//   - closed: 通道是否关闭
//   - canceled: 是否被取消
//   - isCanceled: 取消标记（无超时）
//   - deadline: 截止时间（有超时）
//
// 执行流程：
//  1. 并发接收任务和监听取消信号
//  2. 任务先完成：直接返回任务结果
//  3. 取消信号到达：
//     - nil 超时：标记取消，返回（不等待任务）
//     - 有效超时：设置截止时间，继续等待
//  4. 超时发生：返回取消状态
//
// 关键状态：canceled（取消标记）、deadline（超时时间）
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
			// 取消但无超时
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

// runPreHandler 运行节点前置处理器 - 在节点执行前对输入进行预处理
// 参数：
//   - ta: 目标任务
//   - runWrapper: 可执行对象调用包装器
//
// 返回：处理过程中的错误
// 执行逻辑：
//  1. panic 恢复：将前置处理器中的 panic 转换为结构化错误
//  2. 检查跳过标记：skipPreHandler 为 true 时跳过处理
//  3. 执行前置处理器：使用 runWrapper 调用前置处理器
//  4. 更新输入：将处理后的输入写回任务
//
// 5. 错误传播：任何错误都会中断任务执行
// 使用场景：
//   - 字段映射：从结构体提取特定字段
//   - 数据验证：验证输入数据的合法性
//   - 类型转换：将外部数据转换为节点期望类型
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

// runPostHandler 运行节点后置处理器 - 在节点执行后对输出进行后处理
// 参数：
//   - ta: 目标任务
//   - runWrapper: 可执行对象调用包装器
//
// 执行逻辑：
//  1. panic 恢复：捕获后置处理器中的 panic，写入任务错误
//  2. 执行后置处理器：使用 runWrapper 调用后置处理器
//  3. 更新输出：将处理后的输出写回任务
//  4. 错误处理：后置处理器错误会覆盖任务原有输出，但不中断主流程
//
// 设计要点：
//   - 后置处理器错误不中断图执行，只记录在任务中
//   - 输出仍会被更新，允许下游节点处理部分结果
//
// 使用场景：
//   - 结果清理：清理敏感数据或临时资源
//   - 指标统计：记录执行时间、内存使用等指标
//   - 结果转换：将输出格式转换为下游节点期望格式
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
