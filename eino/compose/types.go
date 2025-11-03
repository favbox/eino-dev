package compose

import "github.com/favbox/eino/components"

// component 是组件类型的别名。
//
// 表示图中节点的原始可执行对象类型，
// 用于标识节点对应的组件类型。
type component = components.Component

// 内置的组件类型常量。
//
// 这些常量代表图中节点对应的最原始的可执行对象类型，
// 用于标识节点是用户提供的哪种组件实现。
//
// 组件类型层次：
//   - Unknown：未知或未分类的组件
//   - Graph：复合图组件，包含嵌套子图
//   - Workflow：工作流组件，基于字段映射的编排
//   - Chain：链式组件，简单线性编排
//   - Passthrough：透传组件，输入直接输出
//   - ToolsNode：工具节点组件
//   - Lambda：函数组件，自定义逻辑
const (
	// ComponentOfUnknown 表示未知或未分类的组件类型。
	// 当无法确定具体组件类型时使用此值。
	ComponentOfUnknown component = "unknown"

	// ComponentOfGraph 表示复合图组件。
	// 包含嵌套子图的复杂节点，可进一步编译为完整的图结构。
	ComponentOfGraph component = "Graph"

	// ComponentOfWorkflow 表示工作流组件。
	// 基于字段映射的声明式编排，无循环结构。
	ComponentOfWorkflow component = "Workflow"

	// ComponentOfChain 表示链式组件。
	// 简单的线性链式编排，内部使用 Graph 实现。
	ComponentOfChain component = "Chain"

	// ComponentOfPassthrough 表示透传组件。
	// 特殊节点，输入数据直接传递给输出，不做任何处理。
	// 用于数据流的直接传输或延迟处理。
	ComponentOfPassthrough component = "Passthrough"

	// ComponentOfToolsNode 表示工具节点组件。
	// 封装一组可调用的工具函数，支持工具调用模式。
	ComponentOfToolsNode component = "ToolsNode"

	// ComponentOfLambda 表示 Lambda 函数组件。
	// 用户自定义的匿名函数或闭包，实现特定业务逻辑。
	ComponentOfLambda component = "LambdaNode"
)

// NodeTriggerMode 定义了图节点的触发模式类型。
//
// 节点触发模式决定了节点何时开始执行，
// 是基于前置节点完成情况的条件判断。
//
// 两种触发模式：
//   - AnyPredecessor：任何前置完成即可触发
//   - AllPredecessor：所有前置完成才触发
type NodeTriggerMode string

const (
	// AnyPredecessor 任意前置触发模式。
	//
	// 节点在以下情况会被触发：
	//   上一个完成的超级步骤中包含了该节点的任意一个前置节点。
	//
	// 应用场景：
	//   - Pregel 算法场景，需要支持循环迭代
	//   - 图中存在前向边或循环边
	//   - 需要快速响应的流式处理
	//
	// 特点：
	//   - 支持图中存在循环结构
	//   - 触发条件较宽松，可能导致频繁触发
	//   - 适合大规模并行处理
	//
	// 参考文档：
	//   https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/orchestration_design_principles/#runtime-engine
	AnyPredecessor NodeTriggerMode = "any_predecessor"

	// AllPredecessor 全前置触发模式。
	//
	// 节点只有在所有前置节点都完成执行后才会被触发。
	//
	// 应用场景：
	//   - DAG 有向无环图场景
	//   - 需要严格依赖关系的处理流程
	//   - 数据处理流水线，确保数据完整性
	//
	// 特点：
	//   - 不支持图中存在循环结构
	//   - 触发条件严格，保证执行顺序
	//   - 适合结构化的业务流程
	//
	// 说明：
	//   此模式下，图结构必须是有向无环图 (DAG)，
	//   如果图中存在循环，会导致编译错误。
	AllPredecessor NodeTriggerMode = "all_predecessor"
)
