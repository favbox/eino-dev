package compose

// graphCompileOptions 图编译选项结构体 - 封装图编译的所有配置参数
// 设计意图：统一管理图编译时的所有配置，包括执行控制、调试信息、状态管理和合并策略
// 关键字段：
//   - maxRunSteps: 最大执行步数（防止无限循环）
//   - graphName: 图名称（用于调试和日志）
//   - nodeTriggerMode: 节点触发模式（AnyPredecessor/AllPredecessor）
//   - callbacks: 编译回调函数
//   - checkPointStore: 检查点存储（断点恢复）
//   - interruptBeforeNodes/interruptAfterNodes: 中断控制
//   - eagerDisabled: 是否禁用急切执行
//   - mergeConfigs: 扇入合并配置
type graphCompileOptions struct {
	// 最大执行步数：防止 Pregel 模式中的无限循环
	maxRunSteps int
	// 图名称：用于调试、日志和监控输出
	graphName string
	// 节点触发模式：默认 AnyPredecessor（Pregel模式）
	nodeTriggerMode NodeTriggerMode

	// 编译回调：图编译完成时执行的回调函数
	callbacks []GraphCompileCallback

	// 原始选项：保存用户传入的所有编译选项（用于调试和重构）
	origOpts []GraphCompileOption

	// 检查点存储：用于断点恢复和状态持久化
	checkPointStore CheckPointStore
	// 序列化器：用于检查点的序列化和反序列化
	serializer Serializer
	// 中断控制：在指定节点前/后中断图执行
	interruptBeforeNodes []string
	interruptAfterNodes  []string

	// 禁用急切执行：强制使用分步执行而非急切执行
	eagerDisabled bool

	// 扇入合并配置：管理多输入源的合并策略
	mergeConfigs map[string]FanInMergeConfig
}

// newGraphCompileOptions 创建图编译选项对象 - 应用所有函数式选项
// 设计意图：初始化默认配置，然后依次应用所有传入的函数式选项
// 执行流程：
//  1. 创建默认的选项对象（所有字段为默认值）
//  2. 遍历所有选项函数，依次应用到选项对象
//  3. 保存原始选项列表（用于调试）
//  4. 返回配置完整的选项对象
func newGraphCompileOptions(opts ...GraphCompileOption) *graphCompileOptions {
	option := &graphCompileOptions{}

	// 应用所有选项：函数式选项模式的核心逻辑
	for _, o := range opts {
		o(option)
	}

	// 保存原始选项：用于调试、监控和选项回放
	option.origOpts = opts

	return option
}

// GraphCompileOption 图编译选项的函数式选项类型 - 支持链式调用和可选配置
// 设计意图：采用函数式选项模式，提供灵活的图编译配置方式，支持任意组合
// 使用示例：
//
//	graph.Compile(ctx,
//		compose.WithGraphName("my_graph"),
//		compose.WithMaxRunSteps(100),
//		compose.WithFanInMergeConfig(configs))
type GraphCompileOption func(*graphCompileOptions)

// WithMaxRunSteps 设置图的最大执行步数 - 防止无限循环的安全机制
// 设计意图：在 Pregel 模式中防止无限循环，为图执行提供硬性限制
// 使用场景：包含循环结构的动态图，防止异常情况下的无限执行
// 注意：仅对 Pregel 模式有效，DAG 模式不支持
// 行为：当执行步数超过限制时，图执行将以错误终止
func WithMaxRunSteps(maxSteps int) GraphCompileOption {
	return func(o *graphCompileOptions) {
		o.maxRunSteps = maxSteps
	}
}

// WithGraphName 设置图的名称 - 用于调试和日志输出
// 设计意图：为图提供人类可读的标识，便于在复杂系统中追踪和监控
// 使用场景：
//   - 调试：区分不同的图实例
//   - 日志：在执行日志中标识图
//   - 监控：监控系统中的图执行情况
//
// 默认行为：如果未设置，将使用自动生成的默认名称
func WithGraphName(graphName string) GraphCompileOption {
	return func(o *graphCompileOptions) {
		o.graphName = graphName
	}
}

// WithEagerExecution 启用图急切执行模式 - [已废弃]
// 设计意图：使节点就绪后立即执行，无需等待超级步完成
// 废弃原因：当节点触发模式设置为 AllPredecessor 时，急切执行会自动启用
// 注意：此选项已废弃，无需手动调用，可安全移除而不改变行为
// 参考：https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/orchestration_design_principles/#runtime-engine
func WithEagerExecution() GraphCompileOption {
	return func(o *graphCompileOptions) {
		// 空实现：急切执行现在由触发模式自动控制
		return
	}
}

// WithEagerExecutionDisabled 禁用图急切执行模式 - 强制分步执行
// 设计意图：强制节点等待超级步完成，而非就绪后立即执行
// 默认行为：Workflow 和 AllPredecessor 模式的图默认启用急切执行
// 使用场景：需要精确控制执行顺序或调试复杂图时
// 参考：https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/orchestration_design_principles/#runtime-engine
func WithEagerExecutionDisabled() GraphCompileOption {
	return func(o *graphCompileOptions) {
		o.eagerDisabled = true
	}
}

// WithNodeTriggerMode 设置图中节点的触发模式 - 控制节点执行时机
// 设计意图：定义节点在图执行过程中的触发时机和依赖关系
// 默认模式：AnyPredecessor（任意前置节点完成即可触发）
// 替代模式：AllPredecessor（所有前置节点完成后才触发）
// 参考：https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/orchestration_design_principles/#runtime-engine
// 使用场景：
//   - AnyPredecessor：动态图，循环结构，灵活触发
//   - AllPredecessor：静态图，DAG模式，严格依赖
func WithNodeTriggerMode(triggerMode NodeTriggerMode) GraphCompileOption {
	return func(o *graphCompileOptions) {
		o.nodeTriggerMode = triggerMode
	}
}

// WithGraphCompileCallbacks 设置图编译回调函数 - 在编译完成后执行
// 设计意图：在图编译完成后执行自定义逻辑，支持监控、调试和扩展
// 使用场景：
//   - 编译监控：记录编译时间和状态
//   - 调试信息：输出图的拓扑结构
//   - 自定义处理：动态修改编译结果
//
// 注意：回调在图编译的 finalization 阶段执行
func WithGraphCompileCallbacks(cbs ...GraphCompileCallback) GraphCompileOption {
	return func(o *graphCompileOptions) {
		o.callbacks = append(o.callbacks, cbs...)
	}
}

// FanInMergeConfig 扇入合并操作配置结构体 - 管理多输入源的合并策略
// 设计意图：定义如何将多个输入源合并为单个输入，支持命名流合并和流完成跟踪
// 关键字段：
//   - StreamMergeWithSourceEOF: 是否在每个流结束时发出 SourceEOF 错误
//
// 使用场景：
//   - 多输入节点：当节点有多个前置节点时
//   - 流式合并：在流式处理中跟踪各个输入流的完成情况
//   - 命名流合并：在 named stream merge 中追踪单个输入流的完成
type FanInMergeConfig struct {
	// 流合并时发出 SourceEOF：控制是否在每个流结束时发出错误信号
	// 行为：在产生最终合并输出前，为每个结束的流发出 SourceEOF 错误
	// 用途：在命名流合并中跟踪各个输入流的完成情况
	StreamMergeWithSourceEOF bool
}

// WithFanInMergeConfig 设置图的扇入合并配置 - 为多输入源节点配置合并策略
// 设计意图：为接收多个输入源的图节点配置合并行为，支持不同场景下的合并策略
// 适用场景：多前置节点的合并节点（如聚合器、路由器等）
// 配置方式：按节点键名配置不同的合并策略
// 使用示例：
//
//	configs := map[string]FanInMergeConfig{
//	    "aggregator_node": {
//	        StreamMergeWithSourceEOF: true,
//	    },
//	}
//	compose.WithFanInMergeConfig(configs)
func WithFanInMergeConfig(confs map[string]FanInMergeConfig) GraphCompileOption {
	return func(o *graphCompileOptions) {
		o.mergeConfigs = confs
	}
}

// InitGraphCompileCallbacks 初始化全局图编译回调函数 - 仅对顶级图生效
// 设计意图：设置全局编译回调，这些回调会自动添加到顶级图的编译选项中
// 适用范围：仅顶级图（子图不会自动继承全局回调）
// 使用场景：为整个应用的所有图设置统一的编译监控和日志
// 注意：这是一个全局设置，影响整个应用的图编译行为
func InitGraphCompileCallbacks(cbs []GraphCompileCallback) {
	globalGraphCompileCallbacks = cbs
}

// 全局图编译回调变量 - 存储全局注册的编译回调函数
// 用途：在创建图编译选项时自动包含这些全局回调
var globalGraphCompileCallbacks []GraphCompileCallback
