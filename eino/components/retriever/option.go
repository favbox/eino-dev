package retriever

import "github.com/favbox/eino/components/embedding"

// Options 定义了检索器的配置选项。
type Options struct {
	// Index 指定检索器使用的索引名称。
	//
	// 不同的检索器实现可能有不同的索引格式和含义。
	// 例如：数据库中的表名、向量数据库的集合名等。
	Index *string

	// SubIndex 指定检索器的子索引名称。
	//
	// 用于在主索引下进一步细分数据。
	// 不同的检索器实现可能有不同的子索引格式。
	SubIndex *string

	// TopK 指定要检索的文档数量上限。
	//
	// 表示返回最相关的 K 个文档。
	// 例如：TopK=5 表示返回最相关的前 5 个文档。
	TopK *int

	// ScoreThreshold 设置相似度分数阈值。
	//
	// 只有分数超过该阈值的文档才会被返回。
	// 例如：0.5 表示只返回相似度大于 0.5 的文档。
	ScoreThreshold *float64

	// Embedding 是用于将查询转换为嵌入向量的嵌入器。
	//
	// 检索器使用此嵌入器将输入查询转换为向量，
	// 然后在向量空间中搜索相似的文档向量。
	Embedding embedding.Embedder

	// DSLInfo 是检索器的 DSL（领域特定语言）信息。
	//
	// 仅用于 Viking 检索器实现。
	// 用于描述复杂的检索查询逻辑和条件。
	DSLInfo map[string]interface{}
}

// WithIndex 设置检索器的索引名称。
//
// 参数：
//   - index: 索引名称字符串
//
// 返回：
//   - Option: 可传递给 Retrieve 方法的选项
//
// 示例：
//
//	docs, err := retriever.Retrieve(ctx, "query",
//		retriever.WithIndex("my_index"))
func WithIndex(index string) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Index = &index
		},
	}
}

// WithSubIndex 设置检索器的子索引名称。
//
// 参数：
//   - subIndex: 子索引名称字符串
//
// 返回：
//   - Option: 可传递给 Retrieve 方法的选项
//
// 示例：
//
//	docs, err := retriever.Retrieve(ctx, "query",
//		retriever.WithSubIndex("sub_index"))
func WithSubIndex(subIndex string) Option {
	return Option{
		apply: func(opts *Options) {
			opts.SubIndex = &subIndex
		},
	}
}

// WithTopK 设置检索返回的文档数量上限。
//
// 参数：
//   - topK: 要返回的文档数量
//
// 返回：
//   - Option: 可传递给 Retrieve 方法的选项
//
// 示例：
//
//	// 只返回最相关的前 10 个文档
//	docs, err := retriever.Retrieve(ctx, "query",
//		retriever.WithTopK(10))
func WithTopK(topK int) Option {
	return Option{
		apply: func(opts *Options) {
			opts.TopK = &topK
		},
	}
}

// WithScoreThreshold 设置相似度分数阈值。
//
// 参数：
//   - threshold: 相似度分数阈值（0.0-1.0）
//
// 返回：
//   - Option: 可传递给 Retrieve 方法的选项
//
// 示例：
//
//	// 只返回高相似度的文档
//	docs, err := retriever.Retrieve(ctx, "query",
//		retriever.WithScoreThreshold(0.8))
func WithScoreThreshold(threshold float64) Option {
	return Option{
		apply: func(opts *Options) {
			opts.ScoreThreshold = &threshold
		},
	}
}

// WithEmbedding 设置检索器使用的嵌入器。
//
// 用于将查询文本转换为向量，以便进行向量相似度搜索。
//
// 参数：
//   - emb: 嵌入器实例
//
// 返回：
//   - Option: 可传递给 Retrieve 方法的选项
//
// 示例：
//
//	emb := embedding.NewEmbedder(...)
//	docs, err := retriever.Retrieve(ctx, "query",
//		retriever.WithEmbedding(emb))
func WithEmbedding(emb embedding.Embedder) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Embedding = emb
		},
	}
}

// WithDSLInfo 设置检索器的 DSL 信息。
//
// 仅用于 Viking 检索器实现。
// 用于传递复杂的查询条件和过滤逻辑。
//
// 参数：
//   - dsl: DSL 配置映射
//
// 返回：
//   - Option: 可传递给 Retrieve 方法的选项
//
// 示例：
//
//	dsl := map[string]any{"filter": "category == 'tech'"}
//	docs, err := retriever.Retrieve(ctx, "query",
//		retriever.WithDSLInfo(dsl))
func WithDSLInfo(dsl map[string]any) Option {
	return Option{
		apply: func(opts *Options) {
			opts.DSLInfo = dsl
		},
	}
}

// Option 定义了用于 Retriever 组件的函数选项类型。
//
// 使用函数式选项（Functional Options）模式。
type Option struct {
	// apply 函数用于应用通用选项到 Options 结构体。
	apply func(opts *Options)

	// implSpecificOptFn 存储实现特定的选项函数。
	implSpecificOptFn any
}

// GetCommonOptions 从选项列表中提取检索器的通用选项。
//
// 可以选择性地提供一个基础 Options 作为默认值。
//
// 参数：
//   - base: 可选的基础 Options，包含默认值
//   - opts: 要解析的 Option 列表
//
// 返回：
//   - *Options: 合并了所有选项配置的 Options 实例
func GetCommonOptions(base *Options, opts ...Option) *Options {
	if base == nil {
		base = &Options{}
	}

	for i := range opts {
		if opts[i].apply != nil {
			opts[i].apply(base)
		}
	}

	return base
}

// WrapImplSpecificOptFn 包装实现特定的选项函数。
//
// 用于支持特定检索器实现的扩展配置选项。
//
// 类型参数：
//   - T: 特定实现的选项结构体类型
//
// 参数：
//   - optFn: 应用于选项结构体的函数
//
// 返回：
//   - Option: 可传递给 Retrieve 方法的选项
func WrapImplSpecificOptFn[T any](optFn func(*T)) Option {
	return Option{
		implSpecificOptFn: optFn,
	}
}

// GetImplSpecificOptions 从选项列表中提取实现特定的选项。
//
// 可以选择性地提供一个基础选项作为默认值。
// 主要用于处理特定检索器实现的扩展配置。
//
// 类型参数：
//   - T: 目标选项结构体类型
//
// 参数：
//   - base: 可选的基础选项，包含默认值
//   - opts: 要解析的 Option 列表
//
// 返回：
//   - *T: 应用了所有特定选项的目标结构体实例
//
// 示例：
//
//	type MyOption struct {
//		Field1 string
//	}
//	opts := retriever.GetImplSpecificOptions(&MyOption{Field1: "default"}, opts...)
func GetImplSpecificOptions[T any](base *T, opts ...Option) *T {
	if base == nil {
		base = new(T)
	}

	for i := range opts {
		opt := opts[i]
		if opt.implSpecificOptFn != nil {
			optFn, ok := opt.implSpecificOptFn.(func(*T))
			if ok {
				optFn(base)
			}
		}
	}

	return base
}
