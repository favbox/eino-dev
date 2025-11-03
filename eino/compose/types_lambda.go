package compose

import (
	"context"
	"fmt"

	"github.com/favbox/eino/schema"
)

// ========== 四种执行模式的函数类型定义 ==========

// Invoke 是可调用 Lambda 函数的类型定义。
// 设计意图：提供最基础的同步执行模式，函数接收输入参数，经过处理后返回输出结果和错误。
// 适用场景：简单的数据转换、同步计算、模型调用等非流式处理场景。
// 特点：
//   - 同步执行，调用方阻塞等待结果
//   - 一次性返回完整结果
//   - 支持自定义选项参数 TOption
//   - 是其他三种模式的基础抽象
type Invoke[I, O, TOption any] func(ctx context.Context, input I, opts ...TOption) (output O, err error)

// Stream 是可流式输出 Lambda 函数的类型定义。
// 设计意图：支持流式输出场景，函数处理输入后，返回一个流读取器，逐步产生输出数据。
// 适用场景：LLM 响应流、实时数据处理、长文本生成等需要渐进式输出的场景。
// 特点：
//   - 输入为普通类型，输出为流
//   - 支持实时流式响应，用户可以边生成边消费
//   - 适用于 Server-Sent Events、WebSocket 等场景
//   - 典型用法：ChatModel 的流式回复
type Stream[I, O, TOption any] func(ctx context.Context,
	input I, opts ...TOption) (output *schema.StreamReader[O], err error)

// Collect 是可收集流式输入 Lambda 函数的类型定义。
// 设计意图：处理上游节点的流式输出，将流数据收集聚合后返回单个结果。
// 适用场景：流式数据聚合、多个流结果合并、批处理等需要收集流数据再处理的场景。
// 特点：
//   - 输入为流类型，输出为普通类型
//   - 将分散的流数据聚合成完整结果
//   - 支持流式输入的背压控制
//   - 典型用法：收集多个工具调用结果进行汇总
type Collect[I, O, TOption any] func(ctx context.Context,
	input *schema.StreamReader[I], opts ...TOption) (output O, err error)

// Transform 是可转换流式数据的 Lambda 函数类型定义。
// 设计意图：实现流到流的转换，既消费流数据又产生流数据，构成流处理管道。
// 适用场景：数据流处理、消息转换、流式计算等需要边消费边产生的场景。
// 特点：
//   - 输入为流，输出也为流
//   - 流式转换，保持数据流的连续性
//   - 支持流式数据处理和转换
//   - 典型用法：文本分词、数据格式化、流式过滤
type Transform[I, O, TOption any] func(ctx context.Context,
	input *schema.StreamReader[I], opts ...TOption) (output *schema.StreamReader[O], err error)

// ========== 无选项版本的函数类型定义 ==========

// InvokeWOOpt 是无选项版本的 Invoke 类型。
// 设计意图：为简单场景提供便捷的函数类型，无需定义选项参数。
// 适用场景：简单的转换函数、测试代码、快速原型开发等不需要配置选项的场景。
// 注意：通过 unreachableOption 类型确保用户不会意外传递选项参数。
type InvokeWOOpt[I, O any] func(ctx context.Context, input I) (output O, err error)

// StreamWOOpt 是无选项版本的 Stream 类型。
// 设计意图：为不需要配置的流式输出场景提供简化类型。
// 适用场景：简单流式处理、测试环境、快速迭代开发。
type StreamWOOpt[I, O any] func(ctx context.Context,
	input I) (output *schema.StreamReader[O], err error)

// CollectWOOpt 是无选项版本的 Collect 类型。
// 设计意图：为不需要配置的流式收集场景提供简化类型。
// 适用场景：简单的流数据聚合、基本的流处理操作。
type CollectWOOpt[I, O any] func(ctx context.Context,
	input *schema.StreamReader[I]) (output O, err error)

// TransformWOOpts 是无选项版本的 Transform 类型。
// 设计意图：为不需要配置的流式转换场景提供简化类型。
// 适用场景：简单的流到流转换、数据格式化等场景。
type TransformWOOpts[I, O any] func(ctx context.Context,
	input *schema.StreamReader[I]) (output *schema.StreamReader[O], err error)

// ========== Lambda 节点结构体定义 ==========

// Lambda 是包装用户提供的 Lambda 函数的节点结构体。
// 设计意图：提供一个统一的接口，将任意 Lambda 函数转换为可组合的节点，支持四种执行模式。
// 适用场景：
//   - 在 Graph 或 Chain 中作为自定义处理节点
//   - 封装业务逻辑为可复用的组件
//   - 实现快速原型开发和调试
//
// 特点：
//   - 支持四种执行模式的任意组合（Invoke、Stream、Collect、Transform）
//   - 内部封装了执行器和元数据，提供统一的执行接口
//   - 支持回调机制和组件类型标识
//   - 可以通过工厂函数创建不同类型的 Lambda 节点
//
// 使用示例：
//
//	lambda := compose.InvokableLambda(func(ctx context.Context, input string) (output string, err error) {
//		return input, nil
//	})
type Lambda struct {
	executor *composableRunnable // 内部执行器，封装了实际的 Lambda 函数和执行逻辑
}

// ========== Lambda 配置选项定义 ==========

// lambdaOpts 是创建 Lambda 时的配置选项结构体。
// 设计意图：封装 Lambda 的配置参数，支持回调机制和组件类型标识。
// 字段说明：
//   - enableComponentCallback: 是否启用组件级回调。当为 true 时，表示 Lambda 函数本身会处理回调，
//     框架将不会重复执行对应的图节点回调，用于避免重复回调和性能优化。
//   - componentImplType: 组件实现类型标识。用于标识 Lambda 的具体类型，便于调试、监控和日志记录。
//     如果为空，框架会尝试从实例的类名或函数名推断，但不能保证准确性。
type lambdaOpts struct {
	enableComponentCallback bool   // 是否启用组件级回调机制
	componentImplType       string // 组件实现类型标识
}

// LambdaOpt 是创建 Lambda 时的选项函数类型。
// 设计意图：采用函数式选项模式，提供灵活的配置方式，支持默认值和可选配置。
// 适用场景：为 Lambda 创建提供可扩展的配置机制，用户可以根据需要选择性地启用某些特性。
type LambdaOpt func(o *lambdaOpts)

// WithLambdaCallbackEnable 启用 Lambda 函数的回调机制。
// 设计意图：允许用户控制的回调执行。当设置为 true 时，表示 Lambda 函数本身会处理回调逻辑，
// 框架将跳过对应的图节点回调，避免重复执行回调逻辑。
// 使用场景：
//   - Lambda 函数内部已经实现了完整的回调逻辑
//   - 需要自定义回调执行时机或逻辑
//   - 性能优化，避免重复回调开销
//
// 参数：y - 是否启用组件级回调，true 表示启用，false 表示使用框架默认回调
func WithLambdaCallbackEnable(y bool) LambdaOpt {
	return func(o *lambdaOpts) {
		o.enableComponentCallback = y
	}
}

// WithLambdaType 设置 Lambda 函数的类型标识。
// 设计意图：为 Lambda 函数提供明确的类型标识，便于调试、监控、日志记录和组件管理。
// 使用场景：
//   - 在监控系统中区分不同类型的 Lambda 组件
//   - 在日志中快速识别组件类型
//   - 单元测试和集成测试中的组件分类
//   - 动态路由和策略选择
//
// 参数：t - 自定义的类型标识字符串，建议使用有意义的名称，如 "DataProcessor"、"MessageValidator" 等
func WithLambdaType(t string) LambdaOpt {
	return func(o *lambdaOpts) {
		o.componentImplType = t
	}
}

// unreachableOption 是无选项版本的 Lambda 函数中使用的占位符类型。
// 设计意图：通过类型系统确保用户不会意外地为无选项函数传递额外的选项参数。
// 机制：这是一个空的结构体类型，在无选项函数签名中作为最后一个参数，
//
//	编译器会阻止用户传递此类型的参数，从而保证函数签名的简洁性。
type unreachableOption struct{}

// ========== Lambda 工厂函数定义 ==========

// InvokableLambdaWithOption 创建带有 Invoke 函数和自定义选项的 Lambda 节点。
// 设计意图：为支持自定义选项的 Invoke 函数提供工厂方法，允许用户在创建时配置回调和类型。
// 使用场景：
//   - 需要为 Invoke 函数传递自定义选项参数 TOption
//   - 需要在创建时配置回调机制或类型标识
//   - 复杂业务逻辑的配置化处理
//
// 参数：
//   - i: Invoke 类型的函数，接受输入 I 和选项，返回输出 O 和错误
//   - opts: 可选的 LambdaOpt 配置函数列表
//
// 返回：配置完成的 Lambda 节点，可直接用于 Graph 或 Chain 中
func InvokableLambdaWithOption[I, O, TOption any](i Invoke[I, O, TOption], opts ...LambdaOpt) *Lambda {
	return anyLambda(i, nil, nil, nil, opts...)
}

// InvokableLambda 创建带有 Invoke 函数（无选项版本）的 Lambda 节点。
// 设计意图：为简单的 Invoke 函数提供便捷的工厂方法，无需定义选项参数类型。
// 使用场景：
//   - 简单的数据转换或处理逻辑
//   - 快速原型开发和测试
//   - 不需要自定义配置的基础 Lambda
//
// 参数：
//   - i: InvokeWOOpt 类型的函数，只接受输入 I，返回输出 O 和错误
//   - opts: 可选的 LambdaOpt 配置函数列表
//
// 返回：包装完成的 Lambda 节点，内部会自动处理无选项函数的适配
func InvokableLambda[I, O any](i InvokeWOOpt[I, O], opts ...LambdaOpt) *Lambda {
	// 使用 unreachableOption 适配器将无选项函数转换为标准 Invoke 函数
	f := func(ctx context.Context, input I, opts_ ...unreachableOption) (output O, err error) {
		return i(ctx, input)
	}

	return anyLambda(f, nil, nil, nil, opts...)
}

// StreamableLambdaWithOption 创建带有 Stream 函数和自定义选项的 Lambda 节点。
// 设计意图：为支持自定义选项的 Stream 函数提供工厂方法，支持流式输出场景。
// 使用场景：
//   - 需要流式输出的业务逻辑
//   - LLM 响应流处理
//   - 实时数据处理管道
//
// 参数：
//   - s: Stream 类型的函数，支持流式输出
//   - opts: 可选的配置选项
func StreamableLambdaWithOption[I, O, TOption any](s Stream[I, O, TOption], opts ...LambdaOpt) *Lambda {
	return anyLambda(nil, s, nil, nil, opts...)
}

// StreamableLambda 创建带有 Stream 函数（无选项版本）的 Lambda 节点。
// 设计意图：为简单的流式输出函数提供便捷创建方式。
// 使用场景：快速创建流式数据处理节点，无需复杂配置
func StreamableLambda[I, O any](s StreamWOOpt[I, O], opts ...LambdaOpt) *Lambda {
	f := func(ctx context.Context, input I, opts_ ...unreachableOption) (
		output *schema.StreamReader[O], err error) {

		return s(ctx, input)
	}

	return anyLambda(nil, f, nil, nil, opts...)
}

// CollectableLambdaWithOption 创建带有 Collect 函数和自定义选项的 Lambda 节点。
// 设计意图：为支持自定义选项的 Collect 函数提供工厂方法，处理流式输入聚合。
// 使用场景：
//   - 流式数据聚合和处理
//   - 多个流结果的合并
//   - 批处理场景
func CollectableLambdaWithOption[I, O, TOption any](c Collect[I, O, TOption], opts ...LambdaOpt) *Lambda {
	return anyLambda(nil, nil, c, nil, opts...)
}

// CollectableLambda 创建带有 Collect 函数（无选项版本）的 Lambda 节点。
// 设计意图：为简单的流式收集函数提供便捷创建方式。
func CollectableLambda[I, O any](c CollectWOOpt[I, O], opts ...LambdaOpt) *Lambda {
	f := func(ctx context.Context, input *schema.StreamReader[I],
		opts_ ...unreachableOption) (output O, err error) {

		return c(ctx, input)
	}

	return anyLambda(nil, nil, f, nil, opts...)
}

// TransformableLambdaWithOption 创建带有 Transform 函数和自定义选项的 Lambda 节点。
// 设计意图：为支持自定义选项的 Transform 函数提供工厂方法，实现流到流的转换。
// 使用场景：
//   - 流式数据转换和处理
//   - 数据格式化
//   - 流式计算管道
func TransformableLambdaWithOption[I, O, TOption any](t Transform[I, O, TOption], opts ...LambdaOpt) *Lambda {
	return anyLambda(nil, nil, nil, t, opts...)
}

// TransformableLambda 创建带有 Transform 函数（无选项版本）的 Lambda 节点。
// 设计意图：为简单的流式转换函数提供便捷创建方式。
func TransformableLambda[I, O any](t TransformWOOpts[I, O], opts ...LambdaOpt) *Lambda {

	f := func(ctx context.Context, input *schema.StreamReader[I],
		opts_ ...unreachableOption) (output *schema.StreamReader[O], err error) {

		return t(ctx, input)
	}

	return anyLambda(nil, nil, nil, f, opts...)
}

// ========== 通用的 Lambda 创建和工具函数 ==========

// AnyLambda 创建支持任意组合的 Lambda 节点。
// 设计意图：提供最大的灵活性，允许用户同时实现多种执行模式，根据调用上下文自动选择合适的模式。
// 使用场景：
//   - 需要同时支持同步和流式处理的组件
//   - 复用同一份逻辑处理不同类型的输入
//   - 需要根据执行上下文动态选择执行模式
//
// 重要说明：
//   - 至少需要实现四种模式中的一种，其余可以为 nil
//   - 框架会自动完成其他三种模式的实现（基于已实现的模式）
//   - 提供了构建多模式 Lambda 的最大灵活性
//
// 示例：
//
//	invokeFunc := func(ctx context.Context, input string, opts ...myOption) (output string, err error) {
//		// 同步处理逻辑
//	}
//	streamFunc := func(ctx context.Context, input string, opts ...myOption) (output *schema.StreamReader[string], err error) {
//		// 流式处理逻辑
//	}
//
//	lambda := compose.AnyLambda(invokeFunc, streamFunc, nil, nil)
func AnyLambda[I, O, TOption any](i Invoke[I, O, TOption], s Stream[I, O, TOption],
	c Collect[I, O, TOption], t Transform[I, O, TOption], opts ...LambdaOpt) (*Lambda, error) {

	// 验证至少实现了一种执行模式
	if i == nil && s == nil && c == nil && t == nil {
		return nil, fmt.Errorf("needs to have at least one of four lambda types: invoke/stream/collect/transform, got none")
	}

	return anyLambda(i, s, c, t, opts...), nil
}

// anyLambda 是 AnyLambda 的内部实现，负责将四种执行模式的函数包装为 Lambda 节点。
// 设计意图：统一处理不同执行模式的函数，提供通用的创建逻辑。
// 参数：
//   - i, s, c, t: 四种执行模式的函数指针
//   - opts: 配置选项
//
// 处理流程：
//  1. 解析配置选项
//  2. 创建执行器并设置回调配置
//  3. 初始化元数据（组件类型、回调启用状态等）
//  4. 返回包装完成的 Lambda 节点
func anyLambda[I, O, TOption any](i Invoke[I, O, TOption], s Stream[I, O, TOption],
	c Collect[I, O, TOption], t Transform[I, O, TOption], opts ...LambdaOpt) *Lambda {

	opt := getLambdaOpt(opts...)

	// 创建执行器，enableCallback 参数取反是因为内部逻辑是 "禁用框架回调"
	executor := runnableLambda(i, s, c, t,
		!opt.enableComponentCallback,
	)
	executor.meta = &executorMeta{
		component:                  ComponentOfLambda,
		isComponentCallbackEnabled: opt.enableComponentCallback,
		componentImplType:          opt.componentImplType,
	}

	return &Lambda{
		executor: executor,
	}
}

// getLambdaOpt 解析并合并多个 LambdaOpt 配置选项。
// 设计意图：提供默认配置，同时支持用户自定义配置的覆盖。
// 默认配置：
//   - enableComponentCallback: false（禁用组件级回调）
//   - componentImplType: ""（空字符串，使用框架推断）
//
// 处理流程：
//  1. 初始化默认配置
//  2. 依次应用用户提供的配置函数
//  3. 返回合并后的配置对象
func getLambdaOpt(opts ...LambdaOpt) *lambdaOpts {
	opt := &lambdaOpts{
		enableComponentCallback: false,
		componentImplType:       "",
	}

	// 应用所有配置函数
	for _, optFn := range opts {
		optFn(opt)
	}
	return opt
}

// ToList 创建将单个输入转换为列表的 Lambda 节点。
// 设计意图：提供常用的数据类型转换功能，解决编排过程中的类型不匹配问题。
// 适用场景：
//   - ChatModel 返回单个消息，但下游需要消息数组
//   - 需要将标量值转换为数组进行批量处理
//   - 类型适配和转换
//
// 实现细节：
//   - 同时实现了 Invoke 和 Transform 两种模式
//   - Invoke 模式：直接包装单个值为数组
//   - Transform 模式：使用流转换器逐个转换流中的元素
//
// 示例：
//
//	lambda := compose.ToList[*schema.Message]()
//	chain := compose.NewChain[[]*schema.Message, []*schema.Message]()
//
//	chain.AddChatModel(chatModel) // chatModel returns *schema.Message, but we need []*schema.Message
//	chain.AddLambda(lambda) // convert *schema.Message to []*schema.Message
func ToList[I any](opts ...LambdaOpt) *Lambda {
	// Invoke 模式：单值转列表
	i := func(ctx context.Context, input I, opts_ ...unreachableOption) (output []I, err error) {
		return []I{input}, nil
	}

	// Transform 模式：流式转换
	f := func(ctx context.Context, inputS *schema.StreamReader[I], opts_ ...unreachableOption) (outputS *schema.StreamReader[[]I], err error) {
		return schema.StreamReaderWithConvert(inputS, func(i I) ([]I, error) {
			return []I{i}, nil
		}), nil
	}

	return anyLambda(i, nil, nil, f, opts...)
}

// MessageParser 创建将消息解析为结构化对象的 Lambda 节点。
// 设计意图：专门用于 ChatModel 输出后的消息解析，提供标准化的消息处理流程。
// 适用场景：
//   - ChatModel 输出后需要解析为特定结构
//   - JSON、结构化数据的提取和转换
//   - LLM 输出后处理和验证
//
// 典型工作流程：
//  1. ChatModel 生成包含结构化内容的消息
//  2. MessageParser Lambda 解析消息内容
//  3. 下游节点接收结构化数据继续处理
//
// 示例：
//
//	parser := schema.NewMessageJSONParser[MyStruct](&schema.MessageJSONParseConfig{
//		ParseFrom: schema.MessageParseFromContent,
//	})
//	parserLambda := MessageParser(parser)
//
//	chain := NewChain[*schema.Message, MyStruct]()
//	chain.AppendChatModel(chatModel)
//	chain.AppendLambda(parserLambda)
//
//	r, err := chain.Compile(context.Background())
//
//	// parsed is a MyStruct object
//	parsed, err := r.Invoke(context.Background(), &schema.Message{
//		Role:    schema.MessageRoleUser,
//		Content: "return a json string for my struct",
//	})
func MessageParser[T any](p schema.MessageParser[T], opts ...LambdaOpt) *Lambda {
	i := func(ctx context.Context, input *schema.Message, opts_ ...unreachableOption) (output T, err error) {
		return p.Parse(ctx, input)
	}

	// 自动设置类型标识为 "MessageParse"，便于识别和调试
	opts = append([]LambdaOpt{WithLambdaType("MessageParse")}, opts...)

	return anyLambda(i, nil, nil, nil, opts...)
}
