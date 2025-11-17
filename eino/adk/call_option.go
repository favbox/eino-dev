/*
 * call_option.go - 智能体调用选项系统
 *
 * 核心组件：
 *   - AgentRunOption: 智能体运行选项，支持类型安全的扩展机制
 *   - options: 通用选项结构，包含会话值、检查点 ID 等
 *   - 选项过滤: 支持指定特定智能体可见的选项
 *
 * 设计特点：
 *   - 类型安全：通过泛型实现类型安全的选项注入
 *   - 可扩展：implSpecificOptFn 支持任意类型的选项结构
 *   - 可指定：DesignateAgent 支持选项仅对特定智能体生效
 *   - 向下传递：选项在多智能体调用链中自动传递
 *
 * 使用模式：
 *   1. 定义自定义选项类型（如 chatModelOptions）
 *   2. 使用 WrapImplSpecificOptFn 包装选项函数
 *   3. 通过 GetImplSpecificOptions 提取特定类型选项
 *   4. 可选：使用 DesignateAgent 限定选项作用范围
 *
 * 与其他文件关系：
 *   - 为 runner.go 提供 Runner.Run() 的选项传递机制
 *   - 为 chatmodel.go 提供 ChatModelAgent 的选项扩展能力
 *   - 为 agent_tool.go 提供工具调用时的选项传递
 */

package adk

// ====== 通用选项 ======

// options 是 ADK 内部使用的通用选项结构
type options struct {
	// sessionValues 会话变量，用于在智能体间共享数据
	sessionValues map[string]any
	// checkPointID 检查点 ID，用于中断恢复
	checkPointID *string
	// skipTransferMessages 是否跳过转移消息，用于历史重写优化
	skipTransferMessages bool
}

// ====== 智能体运行选项 ======

// AgentRunOption 是智能体运行时的调用选项。
// 使用泛型函数实现类型安全的选项注入，通过 implSpecificOptFn 存储任意类型的选项函数。
// 支持通过 DesignateAgent 指定选项仅对特定智能体生效。
type AgentRunOption struct {
	// implSpecificOptFn 实现特定的选项函数，类型为 func(*T)
	// T 可以是 options、chatModelOptions、toolOptions 等任意选项类型
	implSpecificOptFn any

	// agentNames 指定哪些智能体可以看到此选项，
	// 为空时表示所有智能体都可见
	agentNames []string
}

// DesignateAgent 指定选项仅对特定智能体生效。
// 在多智能体系统中，某些选项只需要特定智能体处理时使用，避免选项污染。
func (o AgentRunOption) DesignateAgent(name ...string) AgentRunOption {
	o.agentNames = append(o.agentNames, name...)
	return o
}

// getCommonOptions 从 AgentRunOption 列表中提取 options 类型的选项。
func getCommonOptions(base *options, opts ...AgentRunOption) *options {
	if base == nil {
		base = &options{}
	}

	return GetImplSpecificOptions[options](base, opts...)
}

// ====== 预置选项构造函数 ======

// WithSessionValues 设置会话变量，用于在多智能体间共享全局状态和业务上下文
func WithSessionValues(v map[string]any) AgentRunOption {
	return WrapImplSpecificOptFn(func(o *options) {
		o.sessionValues = v
	})
}

// WithSkipTransferMessages 跳过转移消息的历史重写，减少历史消息冗余
func WithSkipTransferMessages() AgentRunOption {
	return WrapImplSpecificOptFn(func(t *options) {
		t.skipTransferMessages = true
	})
}

// ====== 泛型选项工具 ======

// WrapImplSpecificOptFn 将类型特定的选项函数包装为 AgentRunOption。
// 该函数会被存储，稍后由 GetImplSpecificOptions 通过类型断言提取并执行。
func WrapImplSpecificOptFn[T any](optFn func(*T)) AgentRunOption {
	return AgentRunOption{
		implSpecificOptFn: optFn,
	}
}

// GetImplSpecificOptions 从 AgentRunOption 列表中提取类型 T 的选项并应用到 base。
// 如果 base 为 nil，会创建类型 T 的零值。对于每个选项，尝试将 implSpecificOptFn
// 断言为 func(*T) 类型，断言成功则执行该函数修改 base。
func GetImplSpecificOptions[T any](base *T, opts ...AgentRunOption) *T {
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

// filterOptions 过滤出指定智能体可见的选项。
// agentNames 为空的选项对所有智能体可见。
func filterOptions(agentName string, opts []AgentRunOption) []AgentRunOption {
	if len(opts) == 0 {
		return nil
	}
	var filteredOpts []AgentRunOption
	for i := range opts {
		opt := opts[i]
		if len(opt.agentNames) == 0 {
			filteredOpts = append(filteredOpts, opt)
			continue
		}
		for j := range opt.agentNames {
			if opt.agentNames[j] == agentName {
				filteredOpts = append(filteredOpts, opt)
				break
			}
		}
	}
	return filteredOpts
}
