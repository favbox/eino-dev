package parser

// Options 定义了文档解析器的配置选项。
type Options struct {
	// URI 是文档源的统一资源标识符。
	//
	// 用于在 ExtParser 中选择合适的解析器。
	// 解析器根据 URI 的文件扩展名或其他特征来确定解析策略。
	URI string

	// ExtraMeta 是要合并到每个文档中的额外元数据。
	//
	// 这些元数据将作为文档的附加属性，
	// 常用于标记文档来源、分类或其他业务信息。
	ExtraMeta map[string]any
}

// Option 定义了用于 Parser 组件的调用选项。
//
// 它是组件接口签名的一部分，用于统一不同解析器实现的选项类型。
// 每个解析器实现可以在自己的包中定义自己的选项结构体和选项函数，
// 然后使用 WrapImplSpecificOptFn 将实现特定的选项函数包装为该类型，
// 再传递给 Transform。
type Option struct {
	// apply 函数用于应用选项到 Options 结构体。
	apply func(opts *Options)

	// implSpecificOptFn 存储实现特定的选项函数。
	implSpecificOptFn any
}

// WithURI 指定文档的 URI。
//
// 该 URI 将用于在 ExtParser 中选择合适的解析器。
// 根据文件扩展名或 URI 模式自动选择解析方式。
//
// 参数：
//   - uri: 文档的 URI 字符串
//
// 返回：
//   - Option: 可传递给解析器方法的选项
//
// 示例：
//
//	parser.Transform(docs, parser.WithURI("file:///path/to/document.pdf"))
func WithURI(uri string) Option {
	return Option{
		apply: func(opts *Options) {
			opts.URI = uri
		},
	}
}

// WithExtraMeta 指定文档的额外元数据。
//
// 用于为解析后的文档添加自定义元数据信息。
//
// 参数：
//   - meta: 元数据键值对映射
//
// 返回：
//   - Option: 可传递给解析器方法的选项
//
// 示例：
//
//	parser.Transform(docs,
//		parser.WithExtraMeta(map[string]any{
//			"source": "upload",
//			"category": "tech",
//		}))
func WithExtraMeta(meta map[string]any) Option {
	return Option{
		apply: func(opts *Options) {
			opts.ExtraMeta = meta
		},
	}
}

// GetCommonOptions 从选项列表中提取解析器的通用选项。
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
		opt := opts[i]
		if opt.apply != nil {
			opt.apply(base)
		}
	}

	return base
}

// WrapImplSpecificOptFn 将实现特定的选项函数包装为 Option 类型。
// 类型参数：
//   - T: 实现特定的选项结构体类型
//
// 解析器实现需要使用此函数将其自己的选项函数转换为统一的 Option 类型。
// For example, if the Parser impl defines its own options struct:
//
//	type customOptions struct {
//	    conf string
//	}
//
// Then the impl needs to provide an option function as such:
//
//	func WithConf(conf string) Option {
//	    return WrapImplSpecificOptFn(func(o *customOptions) {
//			o.conf = conf
//		}
//	}
//
// .
func WrapImplSpecificOptFn[T any](optFn func(*T)) Option {
	return Option{
		implSpecificOptFn: optFn,
	}
}

// GetImplSpecificOptions 为解析器作者提供从统一 Option 类型中提取自定义选项的能力。
//
// 类型参数：
//   - T: 实现特定的选项结构体类型
//
// 该函数应在解析器实现的 Transform 函数内部使用。
// 建议在第一个参数中提供一个基础 T，解析器作者可以在其中提供实现特定选项的默认值。
//
// 参数：
//   - base: 可选的基础选项，包含默认值
//   - opts: 要解析的 Option 列表
//
// 返回：
//   - *T: 提取了所有自定义选项的结构体实例
//
// 示例：
//
//	type MyOption struct {
//		Field1 string
//	}
//	opts := parser.GetImplSpecificOptions(&MyOption{Field1: "default"}, opts...)
func GetImplSpecificOptions[T any](base *T, opts ...Option) *T {
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
