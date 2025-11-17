package compose

import (
	"reflect"

	"github.com/favbox/eino/internal/generic"
)

type graphAddNodeOpts struct {
	nodeOptions *nodeOptions
	processor   *processorOpts
	needState   bool
}

// GraphAddNodeOpt 节点添加选项的函数式选项类型。
//
// 使用示例：
//
//	graph.AddLambdaNode("my_node", lambda,
//		compose.WithInputKey("input_key"),
//		compose.WithOutputKey("output_key"),
//		compose.WithNodeName("显示名称"))
type GraphAddNodeOpt func(o *graphAddNodeOpts)

type nodeOptions struct {
	nodeName           string
	nodeKey            string
	inputKey           string
	outputKey          string
	graphCompileOption []GraphCompileOption
}

// WithNodeName 设置节点的显示名称，用于日志和调试输出。
// 注意：名称不唯一，多个节点可以同名。
func WithNodeName(n string) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.nodeName = n
	}
}

// WithNodeKey 设置链式节点的键。
// 仅用于 Chain 和 StateChain，其他图类型不支持。
func WithNodeKey(key string) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.nodeKey = key
	}
}

// WithInputKey 设置节点的输入键，从上游节点的 map 输出中提取字段。
//
// 使用示例：
//
//	// 上游输出：map[string]any{"user_name": "张三", "age": 25}
//	// 配置：WithInputKey("user_name")
//	// 当前节点输入："张三"
func WithInputKey(k string) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.inputKey = k
	}
}

// WithOutputKey 设置节点的输出键，将输出包装为 map 格式。
//
// 使用示例：
//
//	// 当前节点输出："result_value"
//	// 配置：WithOutputKey("result")
//	// 下游节点输入：map[string]any{"result": "result_value"}
func WithOutputKey(k string) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.outputKey = k
	}
}

// WithGraphCompileOptions 设置子图编译选项，专门用于 AnyGraph 类型节点。
//
// 使用示例：
//
//	graph.AddGraphNode("sub_graph", subGraph,
//		compose.WithGraphCompileOptions(
//			compose.WithGraphName("my_sub_graph")))
func WithGraphCompileOptions(opts ...GraphCompileOption) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.nodeOptions.graphCompileOption = opts
	}
}

// WithStatePreHandler 设置状态前置处理器，在节点执行前处理输入和状态。
// 处理器本身是线程安全的。
// 注意：需要图使用 WithGenLocalState 创建。
func WithStatePreHandler[I, S any](pre StatePreHandler[I, S]) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.processor.statePreHandler = convertPreHandler(pre)
		o.processor.preStateType = generic.TypeOf[S]()
		o.needState = true
	}
}

// WithStatePostHandler 设置状态后置处理器，在节点执行后处理输出和状态。
// 处理器本身是线程安全的。
// 注意：需要图使用 WithGenLocalState 创建。
func WithStatePostHandler[O, S any](post StatePostHandler[O, S]) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.processor.statePostHandler = convertPostHandler(post)
		o.processor.postStateType = generic.TypeOf[S]()
		o.needState = true
	}
}

// WithStreamStatePreHandler 设置流式状态前置处理器，保持流的实时性。
// 适用场景：上游节点输出是实际流，且希望经过状态处理后仍是流。
// 注意：StreamStatePreHandler 本身是线程安全的，但在自己的 goroutine 内修改状态不是线程安全的。
func WithStreamStatePreHandler[I, S any](pre StreamStatePreHandler[I, S]) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.processor.statePreHandler = streamConvertPreHandler(pre)
		o.processor.preStateType = generic.TypeOf[S]()
		o.needState = true
	}
}

// WithStreamStatePostHandler 设置流式状态后置处理器，保持流的实时性。
// 适用场景：当前节点输出是实际流，且希望经过状态处理后仍是流。
// 注意：StreamStatePostHandler 本身是线程安全的，但在自己的 goroutine 内修改状态不是线程安全的。
func WithStreamStatePostHandler[O, S any](post StreamStatePostHandler[O, S]) GraphAddNodeOpt {
	return func(o *graphAddNodeOpts) {
		o.processor.statePostHandler = streamConvertPostHandler(post)
		o.processor.postStateType = generic.TypeOf[S]()
		o.needState = true
	}
}

type processorOpts struct {
	statePreHandler  *composableRunnable
	preStateType     reflect.Type
	statePostHandler *composableRunnable
	postStateType    reflect.Type
}

func getGraphAddNodeOpts(opts ...GraphAddNodeOpt) *graphAddNodeOpts {
	opt := &graphAddNodeOpts{
		nodeOptions: &nodeOptions{},
		processor:   &processorOpts{},
	}

	for _, fn := range opts {
		fn(opt)
	}

	return opt
}
