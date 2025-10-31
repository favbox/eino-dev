package prompt

// Option 定义了用于 ChatTemplate 组件的调用选项。
//
// 用于统一不同模板实现的选项类型，支持模板的扩展配置。
type Option struct {
	// implSpecificOptFn 存储实现特定的选项函数。
	implSpecificOptFn any
}

// WrapImplSpecificOptFn 包装实现特定的选项函数。
//
// 类型参数：
//   - T: 实现特定的选项结构体类型
//
// 用于将实现特定的选项函数转换为统一的 Option 类型。
//
// 示例：
//
//	// 定义自定义选项结构体
//	type CustomTemplateOption struct {
//	    Culture string
//	}
//
//	// 提供选项函数
//	func WithCulture(culture string) Option {
//	    return WrapImplSpecificOptFn(func(o *CustomTemplateOption) {
//			o.Culture = culture
//		})
//	}
//
//	// 使用示例
//	template := prompt.FromMessages(...)
//	formatted := template.Format(ctx, vars, WithCulture("zh-CN"))
func WrapImplSpecificOptFn[T any](optFn func(*T)) Option {
	return Option{
		implSpecificOptFn: optFn,
	}
}

// GetImplSpecificOptions 从选项列表中提取实现特定的选项。
//
// 可以选择性地提供一个基础选项作为默认值。
// 主要用于模板实现的内部使用，在 Format 方法中解析自定义选项。
//
// 类型参数：
//   - T: 目标选项结构体类型
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
//	func (t *CustomChatTemplate) Format(ctx context.Context, vs map[string]any, opts ...Option) ([]*schema.Message, error) {
//	    customOpts := GetImplSpecificOptions(&CustomTemplateOption{
//	        Culture: "en-US",
//	    }, opts...)
//
//	    // 使用 customOpts.Culture 进行模板格式化
//	    ...
//	}
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
