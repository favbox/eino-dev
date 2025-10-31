package document

import "github.com/favbox/eino/components/document/parser"

// LoaderOptions 定义了文档加载器的配置选项。
type LoaderOptions struct {
	// ParserOptions 是传递给解析器的选项列表。
	//
	// 用于配置文档解析器的行为，如编码格式、解析深度等。
	ParserOptions []parser.Option
}

// LoaderOption 定义了用于 Loader 组件的调用选项。
//
// 它是组件接口签名的一部分，用于统一不同加载器实现的选项类型。
// 每个加载器实现可以在自己的包中定义自己的选项结构体和选项函数，
// 然后使用 WrapLoaderImplSpecificOptFn 将实现特定的选项函数包装为该类型，
// 再传递给 Load。
type LoaderOption struct {
	// apply 函数用于应用选项到 LoaderOptions 结构体。
	apply func(opts *LoaderOptions)

	// implSpecificOptFn 存储实现特定的选项函数。
	implSpecificOptFn any
}

// WrapLoaderImplSpecificOptFn 将实现特定的选项函数包装为 LoaderOption 类型。
//
// 类型参数：
//   - T: 实现特定的选项结构体类型
//
// 加载器实现需要使用此函数将其自己的选项函数转换为统一的 LoaderOption 类型。
//
// 示例：
//
//	// 定义自定义选项结构体
//	type customOptions struct {
//	    conf string
//	}
//
//	// 提供选项函数
//	func WithConf(conf string) LoaderOption {
//	    return WrapLoaderImplSpecificOptFn(func(o *customOptions) {
//			o.conf = conf
//		})
//	}
func WrapLoaderImplSpecificOptFn[T any](optFn func(*T)) LoaderOption {
	return LoaderOption{
		implSpecificOptFn: optFn,
	}
}

// GetLoaderImplSpecificOptions 为加载器作者提供从统一 LoaderOption 类型中提取自定义选项的能力。
//
// 类型参数：
//   - T: 实现特定的选项结构体类型
//
// 该函数应在加载器实现的 Load 函数内部使用。
// 建议在第一个参数中提供一个基础 T，加载器作者可以在其中提供实现特定选项的默认值。
//
// 参数：
//   - base: 可选的基础选项，包含默认值
//   - opts: 要解析的 LoaderOption 列表
//
// 返回：
//   - *T: 提取了所有自定义选项的结构体实例
//
// 示例：
//
//	type MyOption struct {
//		Field1 string
//	}
//	opts := loader.GetLoaderImplSpecificOptions(&MyOption{Field1: "default"}, opts...)
func GetLoaderImplSpecificOptions[T any](base *T, opts ...LoaderOption) *T {
	if base == nil {
		base = new(T)
	}

	for i := range opts {
		opt := opts[i]
		if opt.implSpecificOptFn != nil {
			s, ok := opt.implSpecificOptFn.(func(*T))
			if ok {
				s(base)
			}
		}
	}

	return base
}

// GetLoaderCommonOptions 从选项列表中提取加载器的通用选项。
//
// 可以选择性地提供一个基础 LoaderOptions 作为默认值。
//
// 参数：
//   - base: 可选的基础 LoaderOptions，包含默认值
//   - opts: 要解析的 LoaderOption 列表
//
// 返回：
//   - *LoaderOptions: 合并了所有选项配置的 LoaderOptions 实例
func GetLoaderCommonOptions(base *LoaderOptions, opts ...LoaderOption) *LoaderOptions {
	if base == nil {
		base = &LoaderOptions{}
	}

	for i := range opts {
		opt := opts[i]
		if opt.apply != nil {
			opt.apply(base)
		}
	}

	return base
}

// WithParserOptions 设置传递给解析器的选项列表。
//
// 用于在加载器级别配置底层解析器的行为。
//
// 参数：
//   - opts: 解析器选项列表
//
// 返回：
//   - LoaderOption: 可传递给 Load 方法的选项
//
// 示例：
//
//	loader := document.NewLoader(...)
//	docs, err := loader.Load(ctx, src,
//		document.WithParserOptions(parser.WithEncoding("utf-8")))
func WithParserOptions(opts ...parser.Option) LoaderOption {
	return LoaderOption{
		apply: func(o *LoaderOptions) {
			o.ParserOptions = opts
		},
	}
}

// TransformerOption 定义了用于 Transformer 组件的调用选项。
//
// 它是组件接口签名的一部分，用于统一不同转换器实现的选项类型。
// 每个转换器实现可以在自己的包中定义自己的选项结构体和选项函数，
// 然后使用 WrapTransformerImplSpecificOptFn 将实现特定的选项函数包装为该类型，
// 再传递给 Transform。
type TransformerOption struct {
	// implSpecificOptFn 存储实现特定的选项函数。
	implSpecificOptFn any
}

// WrapTransformerImplSpecificOptFn 将实现特定的选项函数包装为 TransformerOption 类型。
//
// 类型参数：
//   - T: 实现特定的选项结构体类型
//
// 转换器实现需要使用此函数将其自己的选项函数转换为统一的 TransformerOption 类型。
//
// 示例：
//
//	// 定义自定义选项结构体
//	type customOptions struct {
//	    conf string
//	}
//
//	// 提供选项函数
//	func WithConf(conf string) TransformerOption {
//	    return WrapTransformerImplSpecificOptFn(func(o *customOptions) {
//			o.conf = conf
//		})
//	}
func WrapTransformerImplSpecificOptFn[T any](optFn func(*T)) TransformerOption {
	return TransformerOption{
		implSpecificOptFn: optFn,
	}
}

// GetTransformerImplSpecificOptions 为转换器作者提供从统一 TransformerOption 类型中提取自定义选项的能力。
//
// 类型参数：
//   - T: 实现特定的选项结构体类型
//
// 该函数应在转换器实现的 Transform 函数内部使用。
// 建议在第一个参数中提供一个基础 T，转换器作者可以在其中提供实现特定选项的默认值。
//
// 参数：
//   - base: 可选的基础选项，包含默认值
//   - opts: 要解析的 TransformerOption 列表
//
// 返回：
//   - *T: 提取了所有自定义选项的结构体实例
//
// 示例：
//
//	type MyOption struct {
//		Field1 string
//	}
//	opts := transformer.GetTransformerImplSpecificOptions(&MyOption{Field1: "default"}, opts...)
func GetTransformerImplSpecificOptions[T any](base *T, opts ...TransformerOption) *T {
	if base == nil {
		base = new(T)
	}

	for i := range opts {
		opt := opts[i]
		if opt.implSpecificOptFn != nil {
			s, ok := opt.implSpecificOptFn.(func(*T))
			if ok {
				s(base)
			}
		}
	}

	return base
}
