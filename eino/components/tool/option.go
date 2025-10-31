package tool

// Option 定义了用于 InvokableTool 或 StreamableTool 组件的调用选项。
//
// 它是组件接口签名的一部分，用于统一不同工具实现的选项类型。
// 每个工具实现可以在自己的包中定义自己的选项结构体和选项函数，
// 然后使用 WrapImplSpecificOptFn 将实现特定的选项函数包装为该类型，
// 再传递给 InvokableRun 或 StreamableRun。
type Option struct {
	// implSpecificOptFn 存储实现特定的选项函数。
	implSpecificOptFn any
}

// WrapImplSpecificOptFn 将实现特定的选项函数包装为 Option 类型。
//
// 类型参数：
//   - T: 实现特定的选项结构体类型
//
// 工具实现需要使用此函数将其自己的选项函数转换为统一的 Option 类型。
//
// 示例：
//
//	// 定义自定义选项结构体
//	type customOptions struct {
//	    conf string
//	}
//
//	// 提供选项函数
//	func WithConf(conf string) Option {
//	    return WrapImplSpecificOptFn(func(o *customOptions) {
//			o.conf = conf
//		}
//	}
//
// 使用示例：
//
//	result, err := tool.InvokableRun(ctx, args, WithConf("value"))
func WrapImplSpecificOptFn[T any](optFn func(*T)) Option {
	return Option{
		implSpecificOptFn: optFn,
	}
}

// GetImplSpecificOptions 为工具作者提供从统一 Option 类型中提取自定义选项的能力。
//
// 类型参数：
//   - T: 实现特定的选项结构体类型
//
// 该函数应在工具实现的 InvokableRun 或 StreamableRun 函数内部使用。
// 建议在第一个参数中提供一个基础 T，工具作者可以在其中提供实现特定选项的默认值。
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
//	type customOptions struct {
//	    conf string
//	}
//	defaultOptions := &customOptions{}
//
//	customOptions := tool.GetImplSpecificOptions(defaultOptions, opts...)
//
//	// 在工具的 InvokableRun 或 StreamableRun 中使用
//	func (t *MyTool) InvokableRun(ctx context.Context, args string, opts ...Option) (string, error) {
//	    customOpts := tool.GetImplSpecificOptions(&customOptions{}, opts...)
//	    // 使用 customOpts.conf 进行工具逻辑
//	    ...
//	}
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
