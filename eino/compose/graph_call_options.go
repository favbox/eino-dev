package compose

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/components/document"
	"github.com/favbox/eino/components/embedding"
	"github.com/favbox/eino/components/indexer"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/components/retriever"
)

// ========== 图中断机制定义 ==========

// graphCancelChanKey 是 Context 中用于存储图取消通道的键。
// 设计意图：通过 Context value 在调用链中传递中断控制信息，实现分布式的图执行控制。
// 机制：使用结构体作为键可以避免键冲突，确保类型安全。
type graphCancelChanKey struct{}

// graphCancelChanVal 是存储图取消通道的值结构体。
// 设计意图：封装图中断所需的所有信息，包括取消通道和超时配置。
// 字段：
//   - ch: 传递超时信息的通道，nil 表示无超时，非 nil 表示超时时间
type graphCancelChanVal struct {
	ch chan *time.Duration // 超时时间通道
}

// graphInterruptOptions 是图中断选项的配置结构体。
// 设计意图：封装图中断执行时的可配置参数，支持优雅中断和强制中断。
type graphInterruptOptions struct {
	timeout *time.Duration // 最大等待时间，nil 表示等待当前任务完成
}

// GraphInterruptOption 是图中断选项的函数类型。
// 设计意图：采用函数式选项模式，提供灵活的中断配置方式。
type GraphInterruptOption func(o *graphInterruptOptions)

// WithGraphInterruptTimeout 设置图中断前的最大等待时间。
// 设计意图：控制图中断的时机，避免无限等待或过于急促的中断。
// 参数：
//   - timeout: 最大等待时间，超过此时间将强制中断图执行
//
// 行为：
//   - 图会等待当前正在执行的任务完成
//   - 如果超时，强制中断图执行
//   - 未完成的任务在图恢复时会重新执行
//
// 适用场景：
//   - 避免图执行时间过长
//   - 实现超时控制机制
//   - 在用户取消操作时提供优雅退出
func WithGraphInterruptTimeout(timeout time.Duration) GraphInterruptOption {
	return func(o *graphInterruptOptions) {
		o.timeout = &timeout
	}
}

// WithGraphInterrupt 创建支持图中断的 Context。
// 设计意图：提供可控的图执行中断机制，支持优雅和强制两种中断模式。
// 参数：
//   - parent: 父 Context，用于继承取消和超时设置
//
// 返回值：
//   - ctx: 包含图中断支持的子 Context
//   - interrupt: 中断触发函数，可传入中断选项
//
// 机制：
//   - 默认行为：等待当前任务完成后中断
//   - 可选超时：超过指定时间强制中断
//   - 支持异步触发：通过 channel 传递中断信号
//
// 使用示例：
//
//	ctx, interrupt := compose.WithGraphInterrupt(parentCtx)
//	go func() {
//		time.Sleep(5 * time.Second)
//		interrupt() // 优雅中断
//	}()
//	result := graph.Invoke(ctx, input)
func WithGraphInterrupt(parent context.Context) (ctx context.Context, interrupt func(opts ...GraphInterruptOption)) {
	// 创建通道用于传递超时信息
	ch := make(chan *time.Duration, 1)
	// 将取消通道信息存储在 Context 中
	ctx = context.WithValue(parent, graphCancelChanKey{}, &graphCancelChanVal{
		ch: ch,
	})
	// 返回中断触发函数
	return ctx, func(opts ...GraphInterruptOption) {
		o := &graphInterruptOptions{}
		// 解析中断选项
		for _, opt := range opts {
			opt(o)
		}
		// 发送超时信息到通道
		ch <- o.timeout
		close(ch)
	}
}

// getGraphCancel 从 Context 中获取图取消通道信息。
// 设计意图：提供安全的 Context 值提取机制，支持可选的图中断功能。
// 参数：
//   - ctx: 包含图取消信息的 Context
//
// 返回值：
//   - *graphCancelChanVal: 图取消通道信息，nil 表示未启用图中断
//
// 机制：
//   - 类型安全的类型断言
//   - 优雅处理不存在的情况
//   - 支持可选的图中断功能
func getGraphCancel(ctx context.Context) *graphCancelChanVal {
	val, ok := ctx.Value(graphCancelChanKey{}).(*graphCancelChanVal)
	if !ok {
		return nil
	}
	return val
}

// ========== 核心选项结构体定义 ==========

// Option 是图调用的函数式选项类型。
// 设计意图：封装图执行时的所有可配置参数，提供灵活的配置机制，支持节点级和全局级配置。
// 特点：
//   - 函数式选项模式，链式调用和组合
//   - 支持节点路径指定，精确定位配置目标
//   - 支持多种组件类型的选项统一封装
//   - 支持运行时控制和状态管理
//
// 字段说明：
//   - options: 各种组件的配置选项列表，any 类型支持多种组件
//   - handler: 回调处理器列表，用于监控和调试
//   - paths: 节点路径列表，指定选项应用的目标节点
//   - maxRunSteps: 图执行的最大步数限制
//   - checkPointID: 检查点标识，用于断点恢复
//   - writeToCheckPointID: 写入检查点标识，用于持久化状态
//   - forceNewRun: 是否强制重新执行，忽略缓存
//   - stateModifier: 状态修改器，用于动态调整图状态
type Option struct {
	options []any               // 组件选项列表，支持任意类型的组件配置
	handler []callbacks.Handler // 回调处理器列表，用于全局回调设置

	paths []*NodePath // 节点路径列表，精确定位配置目标

	maxRunSteps         int           // 图执行的最大步数，防止无限执行
	checkPointID        *string       // 检查点ID，支持断点恢复
	writeToCheckPointID *string       // 写入检查点ID，支持状态持久化
	forceNewRun         bool          // 强制重新执行，忽略缓存结果
	stateModifier       StateModifier // 状态修改器，支持动态状态调整
}

// deepCopy 创建 Option 的深拷贝副本。
// 设计意图：确保选项配置在传递过程中的不可变性，避免意外的修改影响原始配置。
// 机制：
//   - 值类型拷贝：避免指针共享导致的状态污染
//   - 深度复制：对指针类型进行逐一复制
//   - 隔离性：确保每个调用者拥有独立的配置副本
//
// 注意：只复制必要字段，不包括不可变或引用透明度高的字段
func (o Option) deepCopy() Option {
	nOptions := make([]any, len(o.options))
	copy(nOptions, o.options)
	nHandler := make([]callbacks.Handler, len(o.handler))
	copy(nHandler, o.handler)
	nPaths := make([]*NodePath, len(o.paths))
	for i, path := range o.paths {
		nPath := *path
		nPaths[i] = &nPath
	}
	return Option{
		options:     nOptions,
		handler:     nHandler,
		paths:       nPaths,
		maxRunSteps: o.maxRunSteps,
		// 注意：checkPointID 等字段未复制，因为它们通常在特定调用中设置
	}
}

// DesignateNode 指定选项应用到的节点键。
// 设计意图：提供便捷的方式将选项精确应用到特定节点，避免全图生效。
// 参数：
//   - nodeKey: 一个或多个节点键，用于标识目标节点
//
// 行为：
//   - 创建节点路径列表
//   - 调用 DesignateNodeWithPath 进行处理
//   - 支持链式调用
//
// 注意：只在顶层图生效，不支持嵌套图的直接指定
// 使用示例：
//
//	embeddingOption := compose.WithEmbeddingOption(embedding.WithModel("text-embedding-3-small"))
//	runnable.Invoke(ctx, "input", embeddingOption.DesignateNode("embedding_node_key"))
func (o Option) DesignateNode(nodeKey ...string) Option {
	nKeys := make([]*NodePath, len(nodeKey))
	for i, k := range nodeKey {
		nKeys[i] = NewNodePath(k)
	}
	return o.DesignateNodeWithPath(nKeys...)
}

// DesignateNodeWithPath 指定选项应用到的节点路径。
// 设计意图：提供精确的节点路径指定机制，支持嵌套图中特定节点的配置。
// 参数：
//   - path: 一个或多个节点路径，支持嵌套路径指定
//
// 机制：
//   - NodePath 支持多级路径，如 "sub_graph_node_key", "node_key_within_sub_graph"
//   - 允许多个路径同时指定，选项将应用到所有匹配节点
//   - 支持链式调用，可与其他选项组合
//
// 使用示例：
//
//	nodePath := NewNodePath("sub_graph_node_key", "node_key_within_sub_graph")
//	DesignateNodeWithPath(nodePath)
func (o Option) DesignateNodeWithPath(path ...*NodePath) Option {
	o.paths = append(o.paths, path...)
	return o
}

// ========== 组件选项函数定义 ==========

// WithEmbeddingOption 是嵌入组件的函数式选项类型。
// 设计意图：为嵌入组件提供统一的选项配置接口，支持在图调用时动态配置组件参数。
// 参数：
//   - opts: 嵌入组件的配置选项列表
//
// 返回：图调用选项，可以与其他选项组合使用
// 使用示例：
//
//	embeddingOption := compose.WithEmbeddingOption(embedding.WithModel("text-embedding-3-small"))
//	runnable.Invoke(ctx, "input", embeddingOption)
func WithEmbeddingOption(opts ...embedding.Option) Option {
	return withComponentOption(opts...)
}

// WithRetrieverOption 是检索器组件的函数式选项类型。
// 设计意图：为检索器组件提供统一的选项配置接口。
// 使用场景：在图执行时动态配置检索器参数，如索引名称、检索策略等。
func WithRetrieverOption(opts ...retriever.Option) Option {
	return withComponentOption(opts...)
}

// WithLoaderOption 是文档加载器组件的函数式选项类型。
// 设计意图：为文档加载器提供统一的选项配置接口。
// 使用场景：动态配置文档加载参数，如集合名称、数据源等。
func WithLoaderOption(opts ...document.LoaderOption) Option {
	return withComponentOption(opts...)
}

// WithDocumentTransformerOption 是文档转换器组件的函数式选项类型。
// 设计意图：为文档转换器提供统一的选项配置接口。
func WithDocumentTransformerOption(opts ...document.TransformerOption) Option {
	return withComponentOption(opts...)
}

// WithIndexerOption 是索引器组件的函数式选项类型。
// 设计意图：为索引器提供统一的选项配置接口。
// 使用场景：动态配置索引器参数，如子索引、索引策略等。
func WithIndexerOption(opts ...indexer.Option) Option {
	return withComponentOption(opts...)
}

// WithChatModelOption 是聊天模型组件的函数式选项类型。
// 设计意图：为聊天模型提供统一的选项配置接口，是最常用的组件选项之一。
// 使用场景：动态配置模型参数，如温度、最大令牌数、系统提示等。
// 使用示例：
//
//	chatModelOption := compose.WithChatModelOption(model.WithTemperature(0.7))
//	runnable.Invoke(ctx, "input", chatModelOption)
func WithChatModelOption(opts ...model.Option) Option {
	return withComponentOption(opts...)
}

// WithChatTemplateOption 是聊天模板组件的函数式选项类型。
// 设计意图：为聊天模板提供统一的选项配置接口。
func WithChatTemplateOption(opts ...prompt.Option) Option {
	return withComponentOption(opts...)
}

// WithToolsNodeOption 是工具节点组件的函数式选项类型。
// 设计意图：为工具节点提供统一的选项配置接口。
func WithToolsNodeOption(opts ...ToolsNodeOption) Option {
	return withComponentOption(opts...)
}

// WithLambdaOption 是 Lambda 组件的函数式选项类型。
// 设计意图：为 Lambda 组件提供灵活的选项配置方式。
// 特点：
//   - 使用 any 类型支持任意选项参数
//   - 提供最大的灵活性，但也需要用户自行确保类型安全
func WithLambdaOption(opts ...any) Option {
	return Option{
		options: opts,
		paths:   make([]*NodePath, 0),
	}
}

// ========== 全局配置选项定义 ==========

// WithCallbacks 为所有组件设置回调处理器。
// 设计意图：提供全局的回调配置机制，一次性为图中的所有组件设置统一的回调处理。
// 特点：
//   - 作用域：影响图中所有组件的回调执行
//   - 优先级：局部回调优先于全局回调
//   - 累积性：可以多次调用，累积多个回调处理器
//
// 使用示例：
//
//	runnable.Invoke(ctx, "input", compose.WithCallbacks(&myCallbacks{}))
func WithCallbacks(cbs ...callbacks.Handler) Option {
	return Option{
		handler: cbs,
	}
}

// WithRuntimeMaxSteps 设置图运行时的最大步数。
// 设计意图：防止图执行进入无限循环，提供执行安全控制。
// 机制：
//   - 限制图执行的最大迭代次数
//   - 超过限制时抛出错误，终止执行
//   - 用于保护系统免受异常图结构的影响
//
// 使用场景：
//   - 避免无限循环执行
//   - 控制图执行时间
//   - 提供执行超时保护
//
// 使用示例：
//
//	runnable.Invoke(ctx, "input", compose.WithRuntimeMaxSteps(20))
func WithRuntimeMaxSteps(maxSteps int) Option {
	return Option{
		maxRunSteps: maxSteps,
	}
}

// ========== 内部工具函数定义 ==========

// withComponentOption 通用组件选项处理函数。
// 设计意图：为所有类型化的组件选项提供统一的处理逻辑，减少重复代码。
// 参数：
//   - opts: 任意类型的组件选项列表
//
// 返回：封装后的图调用选项
// 机制：
//   - 泛型函数，支持任意类型的组件选项
//   - 将类型化选项转换为 any 类型统一存储
//   - 支持空路径，表示全局生效
func withComponentOption[TOption any](opts ...TOption) Option {
	o := make([]any, 0, len(opts))
	for i := range opts {
		o = append(o, opts[i])
	}
	return Option{
		options: o,
		paths:   make([]*NodePath, 0),
	}
}

// convertOption 将 any 类型的选项列表转换为指定类型的选项列表。
// 设计意图：提供类型安全的选项转换机制，确保运行时类型匹配。
// 参数：
//   - opts: any 类型的选项列表
//
// 返回：
//   - []TOption: 转换后的类型化选项列表
//   - error: 类型转换错误
//
// 机制：
//   - 类型断言验证类型安全
//   - 详细的错误信息包含期望类型和实际类型
//   - 支持空列表的快速处理
func convertOption[TOption any](opts ...any) ([]TOption, error) {
	if len(opts) == 0 {
		return nil, nil
	}
	ret := make([]TOption, 0, len(opts))
	for i := range opts {
		o, ok := opts[i].(TOption)
		if !ok {
			return nil, fmt.Errorf("unexpected component option type, expected:%s, actual:%s", reflect.TypeOf((*TOption)(nil)).Elem().String(), reflect.TypeOf(opts[i]).String())
		}
		ret = append(ret, o)
	}
	return ret, nil
}
