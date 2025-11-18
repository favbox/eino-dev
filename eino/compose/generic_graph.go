/*
 * generic_graph.go - 泛型图实现，提供类型安全的图构建和编译
 *
 * 核心组件：
 *   - Graph[I, O]: 泛型图，指定输入和输出类型的有向图
 *   - NewGraphOption: 图构建选项，支持状态管理配置
 *   - WithGenLocalState: 状态生成器，为节点间共享状态
 *
 * 设计特点：
 *   - 类型安全: 使用泛型确保图的输入输出类型正确
 *   - 状态共享: 支持节点间共享状态，通过前置/后置处理器访问
 *   - 灵活编排: 支持组件、Lambda、Chain、Parallel 等多种节点类型
 *   - 编译时检查: 编译阶段验证图的完整性和类型兼容性
 *
 * 使用流程：
 *   1. 使用 NewGraph[I, O] 创建泛型图，可选配置状态生成器
 *   2. 使用 AddNode 添加节点，配置前置/后置处理器
 *   3. 使用 AddEdge 添加边，定义数据流向
 *   4. 使用 Compile 编译为可运行对象
 *   5. 调用 Invoke/Stream/Collect/Transform 执行图
 */

package compose

import (
	"context"
	"reflect"

	"github.com/favbox/eino/internal/generic"
)

// ====== 图构建选项 ======

// newGraphOptions 配置图构建选项
type newGraphOptions struct {
	withState func(ctx context.Context) any // 状态生成函数
	stateType reflect.Type                  // 状态类型
}

// NewGraphOption 是图构建选项函数类型
type NewGraphOption func(ngo *newGraphOptions)

// WithGenLocalState 配置图的本地状态生成器。
// 状态在节点间共享，可通过前置/后置处理器访问和修改。
func WithGenLocalState[S any](gls GenLocalState[S]) NewGraphOption {
	return func(ngo *newGraphOptions) {
		ngo.withState = func(ctx context.Context) any {
			return gls(ctx)
		}
		ngo.stateType = generic.TypeOf[S]()
	}
}

// ====== 图创建与构建 ======

// NewGraph 创建泛型有向图，可以组合组件、Lambda、Chain、Parallel 等。
// 同时提供灵活的多粒度切面治理能力。
// I 是图编译产物的输入类型，O 是图编译产物的输出类型。
//
// 要在节点间共享状态，使用 WithGenLocalState 选项：
//
//	type testState struct {
//		UserInfo *UserInfo
//		KVs      map[string]any
//	}
//
//	genStateFunc := func(ctx context.Context) *testState {
//		return &testState{}
//	}
//
//	graph := compose.NewGraph[string, string](WithGenLocalState(genStateFunc))
//
//	// 可以使用 WithStatePreHandler 和 WithStatePostHandler 操作状态
//	graph.AddNode("node1", someNode, compose.WithStatePreHandler(func(ctx context.Context, in string, state *testState) (string, error) {
//		// 操作状态
//		return in, nil
//	}), compose.WithStatePostHandler(func(ctx context.Context, out string, state *testState) (string, error) {
//		// 操作状态
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

// Graph 是泛型图，用于组合各种组件。
// I 是图编译产物的输入类型，O 是图编译产物的输出类型。
// 提供类型安全的图构建和编译能力。
type Graph[I, O any] struct {
	*graph
}

// ====== 图边管理 ======

// AddEdge 向图中添加边，边表示从 startNode 到 endNode 的数据流。
// 前一个节点的输出类型必须与下一个节点的输入类型匹配。
// 注意：添加边之前，startNode 和 endNode 必须已添加到图中。
//
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

// Compile 将原始图编译为可运行的形式。
// 执行类型检查、拓扑排序和运行时优化。
//
// 示例：
//
//	graph := compose.NewGraph[string, string]()
//
//	runnable, err := graph.Compile(ctx, compose.WithGraphName("my_graph"))
//	if err != nil {...}
//
//	runnable.Invoke(ctx, "input")      // 调用
//	runnable.Stream(ctx, "input")      // 流式
//	runnable.Collect(ctx, inputReader) // 收集
//	runnable.Transform(ctx, inputReader) // 转换
func (g *Graph[I, O]) Compile(ctx context.Context, opts ...GraphCompileOption) (Runnable[I, O], error) {
	return compileAnyGraph[I, O](ctx, g, opts...)
}

// compileAnyGraph 编译任意图为泛型可运行对象。
// 处理全局回调、编译选项和上下文包装。
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
