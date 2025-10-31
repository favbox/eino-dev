package indexer

import "github.com/favbox/eino/components/embedding"

// Options 定义了索引器的配置选项。
type Options struct {
	// SubIndexes 指定要索引的子索引列表。
	//
	// 用于在多个子索引中存储文档。
	// 不同的索引器实现可能有不同的子索引格式和含义。
	SubIndexes []string

	// Embedding 是用于将文档转换为嵌入向量的嵌入组件。
	//
	// 索引器使用此嵌入器将文档内容转换为向量，
	// 然后将向量存储到向量数据库中。
	Embedding embedding.Embedder
}

// WithSubIndexes 设置索引器的子索引列表。
//
// 参数：
//   - subIndexes: 子索引名称列表
//
// 返回：
//   - Option: 可传递给 Store 方法的选项
//
// 示例：
//
//	ids, err := indexer.Store(ctx, docs,
//		indexer.WithSubIndexes([]string{"sub_index_1", "sub_index_2"}))
func WithSubIndexes(subIndexes []string) Option {
	return Option{
		apply: func(opts *Options) {
			opts.SubIndexes = subIndexes
		},
	}
}

// WithEmbedding 设置索引器使用的嵌入器。
//
// 用于将文档内容转换为向量，以便进行向量索引存储。
//
// 参数：
//   - emb: 嵌入器实例
//
// 返回：
//   - Option: 可传递给 Store 方法的选项
//
// 示例：
//
//	emb := embedding.NewEmbedder(...)
//	ids, err := indexer.Store(ctx, docs,
//		indexer.WithEmbedding(emb))
func WithEmbedding(emb embedding.Embedder) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Embedding = emb
		},
	}
}

// Option 定义了用于 Indexer 组件的函数选项类型。
//
// 使用函数式选项（Functional Options）模式。
type Option struct {
	// apply 函数用于应用通用选项到 Options 结构体。
	apply func(opts *Options)

	// implSpecificOptFn 存储实现特定的选项函数。
	implSpecificOptFn any
}

// GetCommonOptions 从选项列表中提取索引器的通用选项。
//
// 可以选择性地提供一个基础 Options 作为默认值。
//
// 参数：
//   - base: 可选的基础 Options，包含默认值
//   - opts: 要解析的 Option 列表
//
// 返回：
//   - *Options: 合并了所有选项配置的 Options 实例
//
// 示例：
//
//	indexerOption := &indexer.Options{
//		SubIndexes: []string{"default_sub_index"},
//	}
//	indexerOption := indexer.GetCommonOptions(indexerOption, opts...)
func GetCommonOptions(base *Options, opts ...Option) *Options {
	if base == nil {
		base = &Options{}
	}

	for i := range opts {
		opt := opts[i]
		if opt.apply != nil {
			opt.apply(base)
		}
	}

	return base
}

// WrapImplSpecificOptFn 包装实现特定的选项函数。
//
// 用于支持特定索引器实现的扩展配置选项。
//
// 类型参数：
//   - T: 特定实现的选项结构体类型
//
// 参数：
//   - optFn: 应用于选项结构体的函数
//
// 返回：
//   - Option: 可传递给 Store 方法的选项
func WrapImplSpecificOptFn[T any](optFn func(*T)) Option {
	return Option{
		implSpecificOptFn: optFn,
	}
}

// GetImplSpecificOptions 从选项列表中提取实现特定的选项。
//
// 可以选择性地提供一个基础选项作为默认值。
// 主要用于处理特定索引器实现的扩展配置。
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
//	opts := indexer.GetImplSpecificOptions(&MyOption{Field1: "default"}, opts...)
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
