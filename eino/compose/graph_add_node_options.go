package compose

import (
	"reflect"

	"github.com/favbox/eino/internal/generic"
)

// graphAddNodeOpts 节点添加选项结构体 - 封装所有节点配置参数
// 设计意图：统一管理节点的基本选项、处理器选项和状态需求
// 关键字段：
//   - nodeOptions: 节点基本选项（名称、键映射、编译选项）
//   - processor: 处理器选项（前后置处理器、状态类型）
//   - needState: 是否需要状态支持（触发状态生成检查）
type graphAddNodeOpts struct {
	// 节点基本选项：名称、键映射、子图编译配置
	nodeOptions *nodeOptions
	// 处理器选项：状态前后处理器和类型验证
	processor *processorOpts

	// 需要状态：标记节点是否使用状态功能
	// 用于在添加节点时检查图是否启用了状态
	needState bool
}

// GraphAddNodeOpt 节点添加选项的函数式选项类型 - 支持链式调用和可选配置
// 设计意图：采用函数式选项模式，提供灵活的配置方式，支持任意组合
// 使用示例：
//
//	graph.AddNode("node_name", node,
//		compose.WithInputKey("input_key"),
//		compose.WithOutputKey("output_key"),
//		compose.WithNodeName("显示名称"))
type GraphAddNodeOpt func(o *graphAddNodeOpts)

// nodeOptions 节点基本选项结构体 - 管理节点的标识和映射配置
// 设计意图：封装节点的显示名称、链式键、输入输出键映射和子图编译选项
// 关键字段：
//   - nodeName: 节点显示名称（用于调试和日志，不唯一）
//   - nodeKey: 链式节点键（仅Chain/StateChain使用）
//   - inputKey/outputKey: 输入输出键映射（支持结构化数据）
//   - graphCompileOption: 子图编译选项（AnyGraph类型节点专用）
type nodeOptions struct {
	// 节点显示名称：用于日志和调试输出，不影响节点键
	nodeName string

	// 链式节点键：用于在Chain中标识节点（仅Chain/StateChain支持）
	nodeKey string

	// 输入输出键：支持从结构体中提取特定字段
	// inputKey: 从上游节点的 map 输出中提取指定字段作为输入
	// outputKey: 将当前节点的输出包装为 map[key]value 格式
	inputKey  string
	outputKey string

	// 子图编译选项：当节点本身是 AnyGraph 子图时使用的编译配置
	// 例如：设置子图名称、检查点等
	graphCompileOption []GraphCompileOption
}

// WithNodeName 设置节点的显示名称 - 用于日志和调试输出
// 设计意图：为节点提供人类可读的标识，便于在复杂图中追踪节点执行
// 使用场景：调试、日志输出、监控展示
// 注意：名称不唯一，多个节点可以同名
func WithNodeName(n string) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.nodeName = n
	}
}

// WithNodeKey 设置链式节点的键 - 仅用于 Chain/StateChain
// 设计意图：在链式结构中提供节点标识，支持链式API调用
// 适用范围：仅 Chain 和 StateChain，其他图类型不支持
// 使用场景：Chain.AppendXXX() 等链式操作
func WithNodeKey(key string) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.nodeKey = key
	}
}

// WithInputKey 设置节点的输入键 - 从上游节点的 map 输出中提取字段
// 设计意图：支持结构化数据流，从上游节点的 map 输出中提取指定字段作为输入
// 使用示例：
//
//	上游输出：map[string]any{"user_name": "张三", "age": 25}
//	当前节点配置：WithInputKey("user_name")
//	当前节点输入："张三"（字符串类型）
func WithInputKey(k string) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.inputKey = k
	}
}

// WithOutputKey 设置节点的输出键 - 将输出包装为 map 格式
// 设计意图：支持结构化数据输出，将节点输出包装为 map[key]value 格式
// 使用示例：
//
//	当前节点输出：任何类型的数据
//	当前节点配置：WithOutputKey("result")
//	下游节点输入：map[string]any{"result": 原始输出}
func WithOutputKey(k string) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.outputKey = k
	}
}

// WithGraphCompileOptions 设置子图编译选项 - 专门用于 AnyGraph 类型节点
// 设计意图：为嵌套的 AnyGraph 子图提供独立的编译配置
// 使用场景：设置子图名称、检查点、监控等配置
// 示例：graph.AddNode("sub_graph", subGraph, compose.WithGraphCompileOptions(compose.WithGraphName("my_sub_graph")))
func WithGraphCompileOptions(opts ...GraphCompileOption) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.graphCompileOption = opts
	}
}

// WithStatePreHandler 设置状态前置处理器 - 在节点执行前处理输入和状态
// 设计意图：在节点执行前修改输入或存储输入信息到状态，线程安全
// 参数：
//   - I: 节点输入类型（如 ChatModel、Lambda、Retriever 等）
//   - S: WithGenLocalState 中定义的状态类型
//
// 注意：需要图使用 WithGenLocalState 创建
func WithStatePreHandler[I, S any](pre StatePreHandler[I, S]) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.processor.statePreHandler = convertPreHandler(pre)
		o.processor.preStateType = generic.TypeOf[S]()
		o.needState = true
	}
}

// WithStatePostHandler 设置状态后置处理器 - 在节点执行后处理输出和状态
// 设计意图：在节点执行后修改输出或存储输出信息到状态，线程安全
// 参数：
//   - O: 节点输出类型（如 ChatModel、Lambda、Retriever 等）
//   - S: WithGenLocalState 中定义的状态类型
//
// 注意：需要图使用 WithGenLocalState 创建
func WithStatePostHandler[O, S any](post StatePostHandler[O, S]) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.processor.statePostHandler = convertPostHandler(post)
		o.processor.postStateType = generic.TypeOf[S]()
		o.needState = true
	}
}

// WithStreamStatePreHandler 设置流式状态前置处理器 - 保持流的实时性
// 设计意图：处理流式输入的同时操作状态，保持输入流的实时性
// 使用场景：上游节点输出是实际流，且希望经过状态处理后仍是流
// 线程安全：StreamStatePreHandler 本身是线程安全的
// 注意：自己 goroutine 内修改状态不是线程安全的
// 参数：
//   - I: 节点输入类型
//   - S: 状态类型
func WithStreamStatePreHandler[I, S any](pre StreamStatePreHandler[I, S]) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.processor.statePreHandler = streamConvertPreHandler(pre)
		o.processor.preStateType = generic.TypeOf[S]()
		o.needState = true
	}
}

// WithStreamStatePostHandler 设置流式状态后置处理器 - 保持流的实时性
// 设计意图：处理流式输出的同时操作状态，保持输出流的实时性
// 使用场景：当前节点输出是实际流，且希望经过状态处理后仍是流
// 线程安全：StreamStatePostHandler 本身是线程安全的
// 注意：自己 goroutine 内修改状态不是线程安全的
// 参数：
//   - O: 节点输出类型
//   - S: 状态类型
func WithStreamStatePostHandler[O, S any](post StreamStatePostHandler[O, S]) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.processor.statePostHandler = streamConvertPostHandler(post)
		o.processor.postStateType = generic.TypeOf[S]()
		o.needState = true
	}
}

// processorOpts 处理器选项结构体 - 管理状态前后置处理器和类型验证
// 设计意图：封装节点执行前后的状态处理器配置和类型验证信息
// 关键字段：
//   - statePreHandler/statePostHandler: 状态前后处理器（可执行对象）
//   - preStateType/postStateType: 状态类型（用于编译时类型验证）
//
// 线程安全：处理器本身是线程安全的，但用户代码中的状态修改需要自行保证线程安全
type processorOpts struct {
	// 状态前置处理器：在节点执行前修改输入或操作状态
	statePreHandler *composableRunnable
	// 前置状态类型：用于编译时类型验证，确保状态类型匹配
	preStateType reflect.Type

	// 状态后置处理器：在节点执行后修改输出或操作状态
	statePostHandler *composableRunnable
	// 后置状态类型：用于编译时类型验证，确保状态类型匹配
	postStateType reflect.Type
}

// getGraphAddNodeOpts 创建默认选项对象并应用所有选项 - 聚合所有配置
// 设计意图：初始化默认的选项对象，然后依次应用所有传入的函数式选项
// 执行流程：
//  1. 创建包含默认值的选项对象
//  2. 遍历所有选项函数，依次应用到选项对象
//  3. 返回配置完整的选项对象
//
// 使用场景：所有需要解析 GraphAddNodeOpt 的地方都会调用此函数
func getGraphAddNodeOpts(opts ...GraphAddNodeOpt) *graphAddNodeOpts {
	// 初始化默认选项：空名称、空键、无处理器
	opt := &graphAddNodeOpts{
		nodeOptions: &nodeOptions{
			nodeName: "", // 默认空名称
			nodeKey:  "", // 默认空键
		},
		processor: &processorOpts{
			statePreHandler:  nil, // 默认无前置处理器
			statePostHandler: nil, // 默认无后置处理器
		},
	}

	// 应用所有选项：函数式选项模式的核心逻辑
	for _, fn := range opts {
		fn(opt) // 每个选项函数修改 opt 对象
	}

	return opt
}
