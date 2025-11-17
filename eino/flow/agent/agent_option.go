package agent

import "github.com/favbox/eino/compose"

// AgentOption 智能体选项。
// 采用双层架构，分别管理底层 compose 选项和智能体特定选项。
type AgentOption struct {
	implSpecificOptFn any
	composeOptions    []compose.Option
}

// GetComposeOptions 提取底层 compose 选项。
func GetComposeOptions(opts ...AgentOption) []compose.Option {
	var result []compose.Option
	for _, opt := range opts {
		result = append(result, opt.composeOptions...)
	}
	return result
}

// WithComposeOptions 创建包含底层 compose 选项的智能体选项。
//
// 使用示例：
//
//	opt := WithComposeOptions(
//		compose.WithTools(myTools...),
//		compose.WithCallbacks(myCallback),
//	)
//	agent.Generate(ctx, messages, opt)
func WithComposeOptions(opts ...compose.Option) AgentOption {
	return AgentOption{
		composeOptions: opts,
	}
}

// WrapImplSpecificOptFn 创建包含智能体特定选项的智能体选项。
// 通过泛型实现类型安全的选项配置。
//
// 使用示例：
//
//	type MyAgentConfig struct {
//		MaxIterations int
//		Temperature   float64
//	}
//
//	opt := WrapImplSpecificOptFn(func(c *MyAgentConfig) {
//		c.MaxIterations = 10
//		c.Temperature = 0.7
//	})
//	agent := NewMyAgent(ctx, config, opt)
func WrapImplSpecificOptFn[T any](optFn func(*T)) AgentOption {
	return AgentOption{
		implSpecificOptFn: optFn,
	}
}

// GetImplSpecificOptions 提取并应用智能体特定选项到配置对象。
// 如果 base 为 nil，自动创建新实例。
//
// 使用示例：
//
//	opts := []AgentOption{
//		WrapImplSpecificOptFn(func(c *MyAgentConfig) {
//			c.Name = "Alice"
//		}),
//		WrapImplSpecificOptFn(func(c *MyAgentConfig) {
//			c.Age = 30
//		}),
//	}
//	config := GetImplSpecificOptions[MyAgentConfig](nil, opts...)
func GetImplSpecificOptions[T any](base *T, opts ...AgentOption) *T {
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
