package model

import "github.com/favbox/eino/schema"

// Options 模型组件的通用配置选项。
type Options struct {
	// Temperature 控制输出随机性。
	// 值越高（接近 2.0）输出越随机，值越低（接近 0.0）输出越确定。
	// 建议范围：0.0-2.0。
	Temperature *float32

	// MaxTokens 限制生成的最大令牌数。
	// 0 表示使用模型默认值。
	MaxTokens *int

	// Model 指定要使用的模型名称。
	Model *string

	// TopP 控制输出多样性（核心采样）。
	// 建议范围：0.0-1.0。
	// 注意：建议只设置 Temperature 或 TopP 其中一个。
	TopP *float32

	// Stop 定义停止词列表。
	// 当模型生成到这些词汇时会提前停止。
	Stop []string

	// Tools 列出模型可调用的工具列表。
	// 仅适用于支持工具调用的模型。
	Tools []*schema.ToolInfo

	// ToolChoice 控制模型如何选择工具。
	ToolChoice *schema.ToolChoice
}

// Option 用于配置 ChatModel 组件的函数选项类型。
type Option struct {
	apply             func(opts *Options)
	implSpecificOptFn any
}

// WithTemperature 设置模型的温度参数。
//
// 使用示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithTemperature(0.2))
func WithTemperature(temperature float32) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Temperature = &temperature
		},
	}
}

// WithMaxTokens 设置生成的最大令牌数。
//
// 使用示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithMaxTokens(100))
func WithMaxTokens(maxTokens int) Option {
	return Option{
		apply: func(opts *Options) {
			opts.MaxTokens = &maxTokens
		},
	}
}

// WithModel 设置模型名称。
//
// 使用示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithModel("gpt-4"))
func WithModel(name string) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Model = &name
		},
	}
}

// WithTopP 设置核心采样参数。
// 注意：建议只设置 Temperature 或 TopP 其中一个。
//
// 使用示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithTopP(0.9))
func WithTopP(topP float32) Option {
	return Option{
		apply: func(opts *Options) {
			opts.TopP = &topP
		},
	}
}

// WithStop 设置停止词列表。
//
// 使用示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithStop([]string{"\n", "END"}))
func WithStop(stop []string) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Stop = stop
		},
	}
}

// WithTools 设置模型可用的工具列表。
// 仅适用于支持工具调用的模型。
//
// 使用示例：
//
//	tools := []*schema.ToolInfo{...}
//	resp, err := model.Generate(ctx, messages,
//		model.WithTools(tools))
func WithTools(tools []*schema.ToolInfo) Option {
	if tools == nil {
		tools = []*schema.ToolInfo{}
	}
	return Option{
		apply: func(opts *Options) {
			opts.Tools = tools
		},
	}
}

// WithToolChoice 设置工具选择策略。
//
// 使用示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithToolChoice(toolChoice))
func WithToolChoice(toolChoice schema.ToolChoice) Option {
	return Option{
		apply: func(opts *Options) {
			opts.ToolChoice = &toolChoice
		},
	}
}

// WrapImplSpecificOptFn 包装实现特定的选项函数。
// 用于支持特定模型提供商的扩展配置选项。
//
// 使用示例：
//
//	type AzureOption struct {
//		APIVersion string
//	}
//
//	azureOpt := func(opt *AzureOption) {
//		opt.APIVersion = "2023-05-15"
//	}
//
//	opt := model.WrapImplSpecificOptFn(azureOpt)
func WrapImplSpecificOptFn[T any](optFn func(*T)) Option {
	return Option{
		implSpecificOptFn: optFn,
	}
}

// GetCommonOptions 从选项列表中提取通用选项。
//
// 使用示例：
//
//	opts := model.GetCommonOptions(nil,
//		model.WithTemperature(0.7),
//		model.WithMaxTokens(100))
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

// GetImplSpecificOptions 从选项列表中提取实现特定的选项。
//
// 使用示例：
//
//	type MyOption struct {
//		Field1 string
//		Field2 int
//	}
//
//	opts := model.GetImplSpecificOptions[MyOption](&MyOption{
//		Field1: "default",
//	}, opts...)
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
