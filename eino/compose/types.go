package compose

import "github.com/favbox/eino/components"

// component 组件类型别名，标识图节点的原始可执行对象类型。
type component = components.Component

const (
	// ComponentOfUnknown 未知或未分类的组件类型。
	ComponentOfUnknown component = "Unknown"

	// ComponentOfGraph 复合图组件，包含嵌套子图。
	ComponentOfGraph component = "Graph"

	// ComponentOfWorkflow 工作流组件，基于字段映射的声明式编排。
	ComponentOfWorkflow component = "Workflow"

	// ComponentOfChain 链式组件，简单的线性链式编排。
	ComponentOfChain component = "Chain"

	// ComponentOfPassthrough 透传组件，输入数据直接传递给输出。
	ComponentOfPassthrough component = "Passthrough"

	// ComponentOfToolsNode 工具节点组件，封装一组可调用的工具函数。
	ComponentOfToolsNode component = "ToolsNode"

	// ComponentOfLambda Lambda 函数组件，用户自定义的匿名函数或闭包。
	ComponentOfLambda component = "Lambda"
)

// NodeTriggerMode 定义图节点的触发模式。
type NodeTriggerMode string

const (
	// AnyPredecessor 任意前置触发模式。
	// 节点在任意一个前置节点完成后即被触发。
	// 支持循环结构，适用于 Pregel 算法和流式处理。
	//
	// 参考文档：
	//   https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/orchestration_design_principles/#runtime-engine
	AnyPredecessor NodeTriggerMode = "any_predecessor"

	// AllPredecessor 全前置触发模式。
	// 节点在所有前置节点都完成后才被触发。
	// 要求图结构为有向无环图 (DAG)，不支持循环结构。
	AllPredecessor NodeTriggerMode = "all_predecessor"
)
