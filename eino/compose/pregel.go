package compose

import "fmt"

/*
 * pregel.go - Pregel 模式通道实现
 *
 * 核心组件：
 *   - pregelChannel: 轻量级通道（仅值存储）
 *   - pregelChannelBuilder: 通道构建器
 *
 * 设计特点：
 *   - 简化通道模型：无需依赖状态管理，仅存储数据值
 *   - 无控制依赖：节点执行无状态约束，支持简单数据流
 *   - 高效合并：直接合并多输入，无复杂依赖检查
 *
 * 与其他文件关系：
 *   - 与 dag.go 并列：两种不同的通道实现策略
 *   - 为 graph_run.go 提供 Pregel 模式通道
 *   - 适用于简单的链式或并行执行场景
 */

// ====== 通道构建器 ======

// 创建 Pregel 通道实例
func pregelChannelBuilder(_ []string, _ []string, _ func() any, _ func() streamReader) channel {
	return &pregelChannel{Values: make(map[string]any)}
}

// ====== 通道数据结构 ======

// pregelChannel Pregel 通道 - 简化数据存储结构
type pregelChannel struct {
	// 简单值存储
	Values map[string]any

	// 扇入合并配置
	mergeConfig FanInMergeConfig
}

// ====== 通道方法 ======

// 设置扇入合并策略
func (ch *pregelChannel) setMergeConfig(cfg FanInMergeConfig) {
	ch.mergeConfig.StreamMergeWithSourceEOF = cfg.StreamMergeWithSourceEOF
}

// load 加载通道状态
func (ch *pregelChannel) load(c channel) error {
	dc, ok := c.(*pregelChannel)
	if !ok {
		return fmt.Errorf("load pregel channel fail, got %T, want *pregelChannel", c)
	}
	ch.Values = dc.Values
	return nil
}

// convertValues 转换通道值
func (ch *pregelChannel) convertValues(fn func(map[string]any) error) error {
	return fn(ch.Values)
}

// ====== Report 上报机制 ======

// reportValues 报告上游节点的值
func (ch *pregelChannel) reportValues(ins map[string]any) error {
	for k, v := range ins {
		ch.Values[k] = v
	}
	return nil
}

// ====== 数据读取 ======

// get 获取节点输入数据
func (ch *pregelChannel) get(isStream bool, name string, edgeHandler *edgeHandlerManager) (
	any, bool, error) {
	if len(ch.Values) == 0 {
		return nil, false, nil
	}
	defer func() { ch.Values = map[string]any{} }()
	values := make([]any, len(ch.Values))
	names := make([]string, len(ch.Values))
	i := 0
	for k, v := range ch.Values {
		resolvedV, err := edgeHandler.handle(k, name, v, isStream)
		if err != nil {
			return nil, false, err
		}
		values[i] = resolvedV
		names[i] = k
		i++
	}

	if len(values) == 1 {
		return values[0], true, nil
	}

	// merge
	mergeOpts := &mergeOptions{
		streamMergeWithSourceEOF: ch.mergeConfig.StreamMergeWithSourceEOF,
		names:                    names,
	}
	v, err := mergeValues(values, mergeOpts)
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

// reportSkip 报告依赖跳过 - Pregel 模式不支持跳过
func (ch *pregelChannel) reportSkip(_ []string) bool {
	return false
}

// reportDependencies 报告依赖完成 - Pregel 模式无依赖
func (ch *pregelChannel) reportDependencies(_ []string) {
	return
}
