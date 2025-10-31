package model

import "github.com/favbox/eino/schema"

// Options 定义了模型组件的通用配置选项。
//
// 这些选项控制模型的行为和输出特征。
type Options struct {
	// Temperature 控制模型输出的随机性。
	//
	// 值越高（接近 2.0），输出越随机和创造性；
	// 值越低（接近 0.0），输出越聚焦和确定性。
	// 建议范围：0.0-2.0
	Temperature *float32

	// MaxTokens 限制模型生成的最大令牌数。
	//
	// 当达到最大令牌数时，模型将停止生成，
	// 通常返回的 finish reason 为 "length"。
	// 0 标识使用模型默认值。
	MaxTokens *int

	// 指定要使用的模型名称。
	//
	// 例如："gpt-3.5-turbo"、"gpt-4" 等。
	// 不同提供商有不同的模型标识符。
	Model *string

	// TopP 控制模型输出的多样性（核心采样）。
	//
	// 与 Temperature 一起使用，但 TopP 更关注【词汇选择的多样性】。
	// 建议范围：0.0-1.0
	// 注意：建议只设置 Temperature 或 TopP 其中一个，避免冲突。
	TopP *float32

	// Stop 定义模型的停止词列表。
	//
	// 当模型生成到这些词汇或短语时，会提前停止生成。
	// 常用语控制生成边界或格式。
	Stop []string

	// Tools 列出模型可能调用的工具列表。
	//
	// 仅适用于支持工具调用的模型。
	// 每个 ToolInfo 包含工具的名称、描述和参数模式。
	Tools []*schema.ToolInfo

	// ToolChoice 控制模型如何选择要调用的工具。
	//
	// 可以设置为：
	// 	- 强制使用特定工具
	// 	- 允许模型自主选择
	// 	- 禁止工具调用
	ToolChoice *schema.ToolChoice
}

// Option 定义了用于配置 ChatModel 组件的函数选项类型。
//
// 这是一个函数式选项（Functional Options）模式，
// 允许灵活地传递多个配置参数。
type Option struct {
	// apply 函数用于应用通用选项到 Options 结构体。
	apply func(opts *Options)

	// implSpecificOpt 存储实现特定的选项函数。
	// 用于支持特定模型提供商的扩展选项。
	implSpecificOptFn any
}

// WithTemperature 设置模型的温度参数。
//
// 控制输出随机性：值越高越随机，值越低越确定性。
//
// 示例：
//
//	// 设置低随机性，用于精准任务
//	model.Generate(ctx, messages,
//		model.WithTemperature(0.0))
//
// 参数：
//   - temperature：温度值，范围 0.0-2.0
//
// 返回：
//   - Option：可传递给 Generate 或 Stream 方法的选项
func WithTemperature(temperature float32) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Temperature = &temperature
		}}
}

// WithMaxTokens 设置模型生成的最大令牌数。
//
// 示例：
//
//	// 限制输出长度为 100 令牌
//	resp, err := model.Generate(ctx, messages,
//		model.WithMaxTokens(100))
//
// 参数：
//   - maxTokens: 最大令牌数，0 表示使用模型默认值
//
// 返回：
//   - Option: 可传递给 Generate 或 Stream 方法的选项
func WithMaxTokens(maxTokens int) Option {
	return Option{
		apply: func(opts *Options) {
			opts.MaxTokens = &maxTokens
		},
	}
}

// WithModel 设置模型名称。
//
// 用于指定具体使用的模型实例。
//
// 示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithModel("gpt-4"))
//
// 参数：
//   - name: 模型名称字符串
//
// 返回：
//   - Option: 可传递给 Generate 或 Stream 方法的选项
func WithModel(name string) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Model = &name
		},
	}
}

// WithTopP 设置模型的核心采样参数。
//
// 控制输出多样性：值越高词汇选择越多样，值越低越聚焦。
//
// 示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithTopP(0.9))
//
// 注意：
//   - 与 Temperature 配合使用，但建议只设置其中一个
//   - 范围：0.0-1.0
//
// 参数：
//   - topP: 核心采样值
//
// 返回：
//   - Option: 可传递给 Generate 或 Stream 方法的选项
func WithTopP(topP float32) Option {
	return Option{
		apply: func(opts *Options) {
			opts.TopP = &topP
		},
	}
}

// WithStop 设置模型的停止词。
//
// 当模型生成到这些词汇时会提前停止。
//
// 示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithStop([]string{"\n", "END"}))
//
// 参数：
//   - stop: 停止词列表
//
// 返回：
//   - Option: 可传递给 Generate 或 Stream 方法的选项
func WithStop(stop []string) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Stop = stop
		},
	}
}

// WithTools 设置模型可用的工具列表。
//
// 用于工具调用功能，仅适用于支持工具调用的模型。
//
// 示例：
//
//	tools := []*schema.ToolInfo{...}
//	resp, err := model.Generate(ctx, messages,
//		model.WithTools(tools))
//
// 参数：
//   - tools: 工具信息列表，nil 或空列表表示无工具
//
// 返回：
//   - Option: 可传递给 Generate 或 Stream 方法的选项
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
// 控制模型如何选择和使用工具。
//
// 示例：
//
//	resp, err := model.Generate(ctx, messages,
//		model.WithToolChoice(toolChoice))
//
// 参数：
//   - toolChoice: 工具选择策略
//
// 返回：
//   - Option: 可传递给 Generate 或 Stream 方法的选项
func WithToolChoice(toolChoice schema.ToolChoice) Option {
	return Option{
		apply: func(opts *Options) {
			opts.ToolChoice = &toolChoice
		},
	}
}

// WrapImplSpecificOptFn 包装实现的特定选项函数。
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
//   - Option: 可传递给 Generate 或 Stream 方法的选项
//
// 示例：
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

// GetImplSpecificOptions 从选项列表中提取实现的特定选项。
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

// GetCommonOptions 从选项列表中提取模型的通用选项。
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
