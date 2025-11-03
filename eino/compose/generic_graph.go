package compose

import (
	"context"
	"reflect"

	"github.com/favbox/eino/internal/generic"
)

/*
 * generic_graph.go - 通用图实现与构建器
 *
 * 核心组件：
 *   - Graph 结构体：泛型图实现，支持组件、Lambda、Chain 等组合
 *   - NewGraph 工厂函数：创建图的构造函数，支持状态生成选项
 *   - newGraphOptions：图构建选项，封装状态生成器
 *   - NewGraphOption：函数式选项模式，支持状态管理
 *   - compileAnyGraph：统一图编译接口
 *
 * 设计特点：
 *   - 泛型设计：通过泛型 I/O 类型确保编译期类型安全
 *   - 灵活组合：支持组件、Lambda、并行等多种图构建模式
 *   - 状态共享：可选的 WithGenLocalState 支持节点间状态共享
 *   - 切面治理：提供前置/后置处理器等横切能力
 *   - 执行模式：支持 Invoke/Stream/Collect/Transform 四种执行范式
 *
 * 与其他文件关系：
 *   - 继承 graph.go 的基础图实现
 *   - 为 types_composable.go 提供 Graph[I,O] 类型实现
 *   - 与 component_to_graph_node.go 协作构建图节点
 *   - 为 graph_run.go 提供编译后的可执行对象
 *
 * 使用场景：
 *   - 复杂业务流程编排：多节点依赖的有向图执行
 *   - 状态管理场景：节点间共享和传递状态信息
 *   - 切面编程：通过处理器注入横切逻辑
 *   - 多范式执行：支持同步、流式、收集、转换模式
 */

// ====== 图构建选项 ======

// newGraphOptions 图构建选项结构体 - 封装图创建时的配置参数
type newGraphOptions struct {
	withState func(ctx context.Context) any // 状态生成器函数
	stateType reflect.Type                  // 状态类型（反射）
}

// NewGraphOption 图构建选项函数类型 - 函数式选项模式
type NewGraphOption func(ngo *newGraphOptions)

// WithGenLocalState 设置状态生成器选项 - 支持节点间状态共享
// S: 状态类型参数
func WithGenLocalState[S any](gls GenLocalState[S]) NewGraphOption {
	return func(ngo *newGraphOptions) {
		ngo.withState = func(ctx context.Context) any {
			return gls(ctx)
		}
		ngo.stateType = generic.TypeOf[S]()
	}
}

// ====== 图创建与构建 ======

// NewGraph 创建有向图 - 支持组件、Lambda、Chain、并行等的灵活组合。
//
// 同时提供灵活且多粒度的切面治理能力
//
// I: 图编译产物的输入类型； O: 图编译产物的输出类型
//
// 要在节点间共享状态，请使用 WithGenLocalState 选项：
//
//	type testState struct {
//		UserInfo *UserInfo
//		KVs     map[string]any
//	}
//
//	genStateFunc := func(ctx context.Context) *testState {
//		return &testState{}
//	}
//
//	graph := compose.NewGraph[string, string](WithGenLocalState(genStateFunc))
//
//	// 可以使用 WithStatePreHandler 和 WithStatePostHandler 处理状态
//	graph.AddNode("node1", someNode, compose.WithPreHandler(func(ctx context.Context, in string, state *testState) (string, error) {
//		// 处理状态
//		return in, nil
//	}), compose.WithPostHandler(func(ctx context.Context, out string, state *testState) (string, error) {
//		// 处理状态
//		return out, nil
//	}))
func NewGraph[I, O any](opts ...NewGraphOption) *Graph[I, O] {
	options := &newGraphOptions{}
	for _, opt := range opts {
		opt(options)
	}

	g := &Graph[I, O]{
		newGraphFromGeneric[I, O](
			ComponentOfGraph,
			options.withState,
			options.stateType,
			opts,
		),
	}

	return g
}

// ====== Graph 结构体定义 ======

// Graph 通用图结构体 - 用于组合组件的有向图
// I: 图编译产物的输入类型
// O: 图编译产物的输出类型
type Graph[I, O any] struct {
	*graph
}

// ====== 图边管理 ======

// AddEdge 添加图边 - 表示从 startNode 到 endNode 的数据流
// 前驱节点的输出类型必须与后继节点的输入类型匹配
// 注意：添加边之前，startNode 和 endNode 必须已添加到图中
// 示例：
//
//	graph.AddNode("start_node_key", compose.NewPassthroughNode())
//	graph.AddNode("end_node_key", compose.NewPassthroughNode())
//
//	err := graph.AddEdge("start_node_key", "end_node_key")
func (g *Graph[I, O]) AddEdge(startNode, endNode string) (err error) {
	return g.graph.addEdgeWithMappings(startNode, endNode, false, false)
}

// ====== 图编译与执行 ======

// Compile 将原始图编译为可执行形式
// 示例：
//
//	graph, err := compose.NewGraph[string, string]()
//	if err != nil {...}
//
//	runnable, err := graph.Compile(ctx, compose.WithGraphName("my_graph"))
//	if err != nil {...}
//
//	runnable.Invoke(ctx, "input") // 同步调用
//	runnable.Stream(ctx, "input") // 流式调用
//	runnable.Collect(ctx, inputReader) // 收集模式
//	runnable.Transform(ctx, inputReader) // 转换模式
func (g *Graph[I, O]) Compile(ctx context.Context, opts ...GraphCompileOption) (Runnable[I, O], error) {
	return compileAnyGraph[I, O](ctx, g, opts...)
}

// compileAnyGraph 编译任意图实现为可执行对象
func compileAnyGraph[I, O any](ctx context.Context, g AnyGraph, opts ...GraphCompileOption) (Runnable[I, O], error) {
	if len(globalGraphCompileCallbacks) > 0 {
		opts = append([]GraphCompileOption{WithGraphCompileCallbacks(globalGraphCompileCallbacks...)}, opts...)
	}
	option := newGraphCompileOptions(opts...)

	cr, err := g.compile(ctx, option)
	if err != nil {
		return nil, err
	}

	cr.meta = &executorMeta{
		component:                  g.component(),
		isComponentCallbackEnabled: true,
		componentImplType:          "",
	}

	cr.nodeInfo = &nodeInfo{
		name: option.graphName,
	}

	ctxWrapper := func(ctx context.Context, opts ...Option) context.Context {
		return initGraphCallbacks(clearNodeKey(ctx), cr.nodeInfo, cr.meta, opts...)
	}

	rp, err := toGenericRunnable[I, O](cr, ctxWrapper)
	if err != nil {
		return nil, err
	}

	return rp, nil
}
