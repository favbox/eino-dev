/*
 * graph_call_options.go - 图调用选项系统
 *
 * 核心组件：
 *   - Option: 图调用选项，支持组件选项、回调、路径指定等
 *   - GraphInterruptOption: 图中断选项，支持超时设置
 *   - WithGraphInterrupt: 创建支持中断的上下文
 *
 * 设计特点：
 *   - 统一选项接口: 为所有组件提供统一的选项传递机制
 *   - 路径指定: 支持通过节点路径精确指定选项生效范围
 *   - 中断控制: 支持图执行的中断和超时控制
 *   - 类型安全: 使用泛型确保选项类型转换的安全性
 *   - 深拷贝: 支持选项的深拷贝，避免并发问题
 *
 * 选项传递机制：
 *   1. 创建组件选项（如 WithChatModelOption）
 *   2. 可选：使用 DesignateNode 指定生效节点
 *   3. 传递给 Invoke/Stream 方法
 *   4. 框架自动分发到对应组件
 */

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

// graphCancelChanKey 用于在上下文中存储图中断通道的键
type graphCancelChanKey struct{}

// graphCancelChanVal 包装图中断通道
type graphCancelChanVal struct {
	ch chan *time.Duration // 中断信号通道，接收超时时长
}

// graphInterruptOptions 配置图中断行为
type graphInterruptOptions struct {
	timeout *time.Duration // 中断超时时长
}

// GraphInterruptOption 是配置图中断行为的选项函数类型
type GraphInterruptOption func(o *graphInterruptOptions)

// WithGraphInterruptTimeout 指定图中断前的最大等待时间。
// 超过最大等待时间后，图会强制中断。任何未完成的任务会在图恢复时重新运行。
func WithGraphInterruptTimeout(timeout time.Duration) GraphInterruptOption {
	return func(o *graphInterruptOptions) {
		o.timeout = &timeout
	}
}

// WithGraphInterrupt 创建支持图中断的上下文。
// 当返回的上下文用于调用图或工作流时，调用 interrupt 函数会触发中断。
// 图默认会等待当前任务完成后再中断。
func WithGraphInterrupt(parent context.Context) (ctx context.Context, interrupt func(opts ...GraphInterruptOption)) {
	ch := make(chan *time.Duration, 1)
	ctx = context.WithValue(parent, graphCancelChanKey{}, &graphCancelChanVal{
		ch: ch,
	})
	return ctx, func(opts ...GraphInterruptOption) {
		o := &graphInterruptOptions{}
		for _, opt := range opts {
			opt(o)
		}
		ch <- o.timeout
		close(ch)
	}
}

func getGraphCancel(ctx context.Context) *graphCancelChanVal {
	val, ok := ctx.Value(graphCancelChanKey{}).(*graphCancelChanVal)
	if !ok {
		return nil
	}
	return val
}

// Option 是调用图时的函数式选项类型。
// 封装组件选项、回调处理器、路径指定和运行时配置。
type Option struct {
	options []any               // 组件选项列表
	handler []callbacks.Handler // 回调处理器列表

	paths []*NodePath // 选项生效的节点路径列表

	maxRunSteps         int           // 最大运行步数
	checkPointID        *string       // 检查点 ID
	writeToCheckPointID *string       // 写入检查点 ID
	forceNewRun         bool          // 强制新运行
	stateModifier       StateModifier // 状态修改器
}

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
	}
}

// DesignateNode 设置选项将应用到的节点键。
// 注意：仅在顶层图中生效。
//
// 示例：
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

// DesignateNodeWithPath 设置选项将应用到的节点路径。
// 可以通过 NodePath 指定子图中的节点，使选项仅在该节点生效。
//
// 示例：
//
//	nodePath := NewNodePath("sub_graph_node_key", "node_key_within_sub_graph")
//	DesignateNodeWithPath(nodePath)
func (o Option) DesignateNodeWithPath(path ...*NodePath) Option {
	o.paths = append(o.paths, path...)
	return o
}

// WithEmbeddingOption 创建嵌入模型组件的选项。
//
// 示例：
//
//	embeddingOption := compose.WithEmbeddingOption(embedding.WithModel("text-embedding-3-small"))
//	runnable.Invoke(ctx, "input", embeddingOption)
func WithEmbeddingOption(opts ...embedding.Option) Option {
	return withComponentOption(opts...)
}

// WithRetrieverOption 创建检索器组件的选项。
//
// 示例：
//
//	retrieverOption := compose.WithRetrieverOption(retriever.WithIndex("my_index"))
//	runnable.Invoke(ctx, "input", retrieverOption)
func WithRetrieverOption(opts ...retriever.Option) Option {
	return withComponentOption(opts...)
}

// WithLoaderOption 创建文档加载器组件的选项。
//
// 示例：
//
//	loaderOption := compose.WithLoaderOption(document.WithCollection("my_collection"))
//	runnable.Invoke(ctx, "input", loaderOption)
func WithLoaderOption(opts ...document.LoaderOption) Option {
	return withComponentOption(opts...)
}

// WithDocumentTransformerOption 创建文档转换器组件的选项
func WithDocumentTransformerOption(opts ...document.TransformerOption) Option {
	return withComponentOption(opts...)
}

// WithIndexerOption 创建索引器组件的选项。
//
// 示例：
//
//	indexerOption := compose.WithIndexerOption(indexer.WithSubIndexes([]string{"my_sub_index"}))
//	runnable.Invoke(ctx, "input", indexerOption)
func WithIndexerOption(opts ...indexer.Option) Option {
	return withComponentOption(opts...)
}

// WithChatModelOption 创建聊天模型组件的选项。
//
// 示例：
//
//	chatModelOption := compose.WithChatModelOption(model.WithTemperature(0.7))
//	runnable.Invoke(ctx, "input", chatModelOption)
func WithChatModelOption(opts ...model.Option) Option {
	return withComponentOption(opts...)
}

// WithChatTemplateOption 创建聊天模板组件的选项
func WithChatTemplateOption(opts ...prompt.Option) Option {
	return withComponentOption(opts...)
}

// WithToolsNodeOption 创建工具节点组件的选项
func WithToolsNodeOption(opts ...ToolsNodeOption) Option {
	return withComponentOption(opts...)
}

// WithLambdaOption 创建 Lambda 组件的选项
func WithLambdaOption(opts ...any) Option {
	return Option{
		options: opts,
		paths:   make([]*NodePath, 0),
	}
}

// WithCallbacks 为所有组件设置回调处理器。
//
// 示例：
//
//	runnable.Invoke(ctx, "input", compose.WithCallbacks(&myCallbacks{}))
func WithCallbacks(cbs ...callbacks.Handler) Option {
	return Option{
		handler: cbs,
	}
}

// WithRuntimeMaxSteps 设置图运行时的最大步数。
//
// 示例：
//
//	runnable.Invoke(ctx, "input", compose.WithRuntimeMaxSteps(20))
func WithRuntimeMaxSteps(maxSteps int) Option {
	return Option{
		maxRunSteps: maxSteps,
	}
}

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
