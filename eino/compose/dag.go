package compose

import "fmt"

/*
 * dag.go - DAG 调度器实现
 *
 * 核心组件：
 *   - dependencyState: 依赖状态机（Waiting/Ready/Skipped）
 *   - dagChannel: 节点数据通道（值存储+依赖追踪）
 *   - Report 模式：reportValues/Dependencies/Skip
 *
 * 设计特点：
 *   - 状态机驱动执行流：依赖状态决定节点是否可执行
 *   - 上报机制解耦依赖：节点通过 Report 方法上报状态变化
 *   - 通道封装数据流：集中管理依赖状态、数据值和配置
 *
 * 与其他文件关系：
 *   - 为 graph_run.go 提供通道实现
 *   - 支持 DAG 模式下的节点调度和依赖管理
 */

// ====== 通道构建器 ======

// 创建 DAG 通道实例
func dagChannelBuilder(controlDependencies []string, dataDependencies []string, zeroValue func() any, emptyStream func() streamReader) channel {
	deps := make(map[string]dependencyState, len(controlDependencies))
	for _, dep := range controlDependencies {
		deps[dep] = dependencyStateWaiting
	}
	indirect := make(map[string]bool, len(dataDependencies))
	for _, dep := range dataDependencies {
		indirect[dep] = false
	}

	return &dagChannel{
		Values:              make(map[string]any),
		ControlPredecessors: deps,
		DataPredecessors:    indirect,
		zeroValue:           zeroValue,
		emptyStream:         emptyStream,
	}
}

// ====== 依赖状态机 ======

// dependencyState 依赖状态枚举 - 标记 DAG 通道中控制依赖的当前状态
// Waiting: 依赖节点未执行；Ready: 依赖完成可执行；Skipped: 条件分支跳过
type dependencyState uint8

const (
	// dependencyStateWaiting 等待状态 - 依赖节点尚未执行
	dependencyStateWaiting dependencyState = iota

	// dependencyStateReady 就绪状态 - 依赖节点执行完成且未被跳过
	dependencyStateReady

	// dependencyStateSkipped 跳过状态 - 依赖节点在条件分支中跳过
	dependencyStateSkipped
)

// ====== 通道数据结构 ======

// dagChannel DAG 通道 - 封装节点的数据流和依赖管理
// 工厂函数、依赖状态追踪、值存储与合并配置
type dagChannel struct {
	// 零值工厂 - 生成节点输入的零值
	zeroValue func() any
	// 空流工厂 - 生成节点输入的空流
	emptyStream func() streamReader

	// 控制依赖状态 - 追踪各控制前置节点的执行状态
	ControlPredecessors map[string]dependencyState
	// 依赖值存储 - 缓存来自前置节点的数据值
	Values map[string]any
	// 数据依赖就绪标记 - 标记各数据前置节点是否就绪
	DataPredecessors map[string]bool // 所有依赖跳过则间接依赖失效
	// 通道跳过标记 - 当前节点是否被条件分支跳过
	Skipped bool

	// 扇入合并配置 - 多输入场景下的数据合并策略
	mergeConfig FanInMergeConfig
}

// ====== 通道方法 ======

// 设置扇入合并策略
func (ch *dagChannel) setMergeConfig(cfg FanInMergeConfig) {
	ch.mergeConfig.StreamMergeWithSourceEOF = cfg.StreamMergeWithSourceEOF
}

// load 加载通道状态
func (ch *dagChannel) load(c channel) error {
	dc, ok := c.(*dagChannel)
	if !ok {
		return fmt.Errorf("load dag channel fail, got %T, want *dagChannel", c)
	}
	ch.ControlPredecessors = dc.ControlPredecessors
	ch.DataPredecessors = dc.DataPredecessors
	ch.Skipped = dc.Skipped
	ch.Values = dc.Values
	return nil
}

// ====== Report 上报机制 ======

// reportValues 报告上游节点的值
func (ch *dagChannel) reportValues(ins map[string]any) error {
	if ch.Skipped {
		return nil
	}

	for k, v := range ins {
		if _, ok := ch.DataPredecessors[k]; !ok {
			continue
		}
		ch.DataPredecessors[k] = true
		ch.Values[k] = v
	}
	return nil
}

// reportDependencies 报告依赖完成
func (ch *dagChannel) reportDependencies(dependencies []string) {
	if ch.Skipped {
		return
	}

	for _, dep := range dependencies {
		if _, ok := ch.ControlPredecessors[dep]; ok {
			ch.ControlPredecessors[dep] = dependencyStateReady
		}
	}
	return
}

// reportSkip 报告依赖跳过
func (ch *dagChannel) reportSkip(keys []string) bool {
	for _, k := range keys {
		if _, ok := ch.ControlPredecessors[k]; ok {
			ch.ControlPredecessors[k] = dependencyStateSkipped
		}
		if _, ok := ch.DataPredecessors[k]; ok {
			ch.DataPredecessors[k] = true
		}
	}

	allSkipped := true
	for _, state := range ch.ControlPredecessors {
		if state != dependencyStateSkipped {
			allSkipped = false
			break
		}
	}
	ch.Skipped = allSkipped

	return allSkipped
}

// ====== 数据读取 ======

// get 获取节点输入数据
func (ch *dagChannel) get(isStream bool, name string, edgeHandler *edgeHandlerManager) (
	any, bool, error) {
	if ch.Skipped {
		return nil, false, nil
	}

	if len(ch.ControlPredecessors)+len(ch.DataPredecessors) == 0 {
		return nil, false, nil
	}

	for _, state := range ch.ControlPredecessors {
		if state == dependencyStateWaiting {
			return nil, false, nil
		}
	}
	for _, ready := range ch.DataPredecessors {
		if !ready {
			return nil, false, nil
		}
	}

	defer func() {
		ch.Values = make(map[string]any)
		for k := range ch.ControlPredecessors {
			ch.ControlPredecessors[k] = dependencyStateWaiting
		}
		for k := range ch.DataPredecessors {
			ch.DataPredecessors[k] = false
		}
	}()

	valueList := make([]any, len(ch.Values))
	names := make([]string, len(ch.Values))
	i := 0
	for k, value := range ch.Values {
		resolvedV, err := edgeHandler.handle(k, name, value, isStream)
		if err != nil {
			return nil, false, err
		}
		valueList[i] = resolvedV
		names[i] = k
		i++
	}

	if len(valueList) == 0 {
		if isStream {
			return ch.emptyStream(), true, nil
		}
		return ch.zeroValue(), true, nil
	}
	if len(valueList) == 1 {
		return valueList[0], true, nil
	}

	mergeOpts := &mergeOptions{
		streamMergeWithSourceEOF: ch.mergeConfig.StreamMergeWithSourceEOF,
		names:                    names,
	}
	v, err := mergeValues(valueList, mergeOpts)
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

// convertValues 转换通道值
func (ch *dagChannel) convertValues(fn func(map[string]any) error) error {
	return fn(ch.Values)
}
