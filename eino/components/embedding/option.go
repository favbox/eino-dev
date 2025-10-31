package embedding

// Options 定义了嵌入模型的配置选项。
type Options struct {
	// Model 指定嵌入模型的名称。
	//
	// 例如："text-embedding-ada-002"、"text-embedding-3-small" 等。
	// 不同提供商有不同的模型标识符。
	Model *string
}

// Option 定义了用于 Embedder 组件的函数选项类型。
//
// 使用函数式选项（Functional Options）模式，支持灵活配置。
type Option struct {
	// apply 函数用于应用通用选项到 Options 结构体。
	apply func(opts *Options)

	// implSpecificOptFn 存储实现特定的选项函数。
	implSpecificOptFn any
}

// WithModel 设置嵌入模型的名称。
//
// 用于指定具体使用的嵌入模型实例。
//
// 示例：
//
//	embeddings, err := embedder.EmbedStrings(ctx, texts,
//		embedding.WithModel("text-embedding-3-large"))
//
// 参数：
//   - model: 模型名称字符串
//
// 返回：
//   - Option: 可传递给 EmbedStrings 方法的选项
func WithModel(model string) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Model = &model
		},
	}
}

// GetCommonOptions 从选项列表中提取嵌入模型的通用选项。
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
//	defaultModelName := "default_model"
//	embeddingOption := &embedding.Options{
//		Model: &defaultModelName,
//	}
//	embeddingOption := embedding.GetCommonOptions(embeddingOption, opts...)
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
// 用于支持特定模型提供商的扩展配置选项。
// 这允许在不修改核心接口的情况下添加新功能。
//
// 类型参数：
//   - T: 特定实现的选项结构体类型
//
// 参数：
//   - optFn: 应用于选项结构体的函数
//
// 返回：
//   - Option: 可传递给 EmbedStrings 方法的选项
func WrapImplSpecificOptFn[T any](optFn func(*T)) Option {
	return Option{
		implSpecificOptFn: optFn,
	}
}

// GetImplSpecificOptions 从选项列表中提取实现特定的选项。
//
// 可以选择性地提供一个基础选项作为默认值。
// 主要用于处理特定模型提供商的扩展配置。
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
//	defaultValue := &MyOption{Field1: "default"}
//	opts := embedding.GetImplSpecificOptions(defaultValue, opts...)
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
