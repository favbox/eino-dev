package compose

import (
	"context"
	"fmt"
	"reflect"

	"github.com/favbox/eino/callbacks"
	icb "github.com/favbox/eino/internal/callbacks"
	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// on 是回调处理器的高阶函数类型。
//
// 用途：将回调逻辑封装为可重用的处理器。
// 这是 Eino 框架"装饰器模式"的体现 - 为组件添加横切关注点。
//
// 类型参数：
//   - T：回调处理的数据类型（可能是输入、输出或错误）
//
// 设计意义：
//   - 统一回调接口：所有回调处理器都遵循相同的函数签名
//   - 类型安全：通过泛型确保回调的类型正确
//   - 可组合：可以嵌套多个 on 处理器
type on[T any] func(context.Context, T) (context.Context, T)

// onStart 是开始回调处理器。
//
// 用途：在组件开始执行时触发回调。
// 这是五种回调时机中的第一个。
//
// 处理逻辑：
//  1. 调用 icb.On 注册回调处理器
//  2. 设置回调时机为 TimingOnStart
//  3. 标记为前置回调（在执行前触发）
//
// 应用场景：
//   - 记录组件开始时间
//   - 初始化日志上下文
//   - 发送开始事件
//   - 验证输入参数
//
// 注意事项：
//   - 这是前置回调，在实际执行前触发
//   - 回调失败不会阻止组件执行
func onStart[T any](ctx context.Context, input T) (context.Context, T) {
	return icb.On(ctx, input, icb.OnStartHandle[T], callbacks.TimingOnStart, true)
}

// onEnd 是结束回调处理器。
//
// 用途：在组件正常结束时触发回调。
// 这是五种回调时机中的第二个。
//
// 处理逻辑：
//  1. 调用 icb.On 注册回调处理器
//  2. 设置回调时机为 TimingOnEnd
//  3. 标记为后置回调（在执行后触发）
//
// 应用场景：
//   - 记录组件执行时间
//   - 发送完成事件
//   - 验证输出结果
//   - 清理资源
//
// 注意事项：
//   - 这是后置回调，在执行成功后触发
//   - 如果组件执行出错，会触发 onError 而不是 onEnd
func onEnd[T any](ctx context.Context, output T) (context.Context, T) {
	return icb.On(ctx, output, icb.OnEndHandle[T], callbacks.TimingOnEnd, false)
}

// onStartWithStreamInput 是流式输入开始回调处理器。
//
// 用途：在组件开始处理流式输入时触发回调。
// 这是第三种回调时机，专门用于流式场景。
//
// 处理逻辑：
//  1. 调用 icb.On 注册回调处理器
//  2. 设置回调时机为 TimingOnStartWithStreamInput
//  3. 标记为前置回调
//
// 应用场景：
//   - 流式处理的初始化
//   - 记录流开始时间
//   - 设置流处理上下文
//
// 特点：
//   - 专门处理流式输入
//   - 接收 *schema.StreamReader[T] 类型的参数
//   - 在流处理开始前触发
func onStartWithStreamInput[T any](ctx context.Context, input *schema.StreamReader[T]) (
	context.Context, *schema.StreamReader[T]) {

	return icb.On(ctx, input, icb.OnStartWithStreamInputHandle[T], callbacks.TimingOnStartWithStreamInput, true)
}

// genericOnStartWithStreamInputHandle 是通用流式输入开始回调处理器。
//
// 用途：处理流式输入开始的通用回调逻辑。
// 这是对具体类型的 onStartWithStreamInput 的泛化实现。
//
// 处理流程：
//  1. 反转处理器列表：确保回调按正确顺序执行
//  2. 复制输入流：创建流的副本用于回调处理
//  3. 循环处理：对每个回调处理器应用流式处理
//  4. 类型解包：将 streamReader 解包为具体类型进行回调
//
// 设计特点：
//   - 通用性：适用于任意类型的流
//   - 类型安全：通过 unpackStreamReader 确保类型正确
//   - 流式处理：支持流式回调处理
//
// 注意事项：
//   - 这是内部函数，供其他回调处理器使用
//   - 不建议直接调用
func genericOnStartWithStreamInputHandle(ctx context.Context, input streamReader,
	runInfo *icb.RunInfo, handlers []icb.Handler) (context.Context, streamReader) {

	handlers = generic.Reverse(handlers)

	cpy := input.copy

	handle := func(ctx context.Context, handler icb.Handler, in streamReader) context.Context {
		in_, ok := unpackStreamReader[icb.CallbackInput](in)
		if !ok {
			panic("impossible")
		}

		return handler.OnStartWithStreamInput(ctx, runInfo, in_)
	}

	return icb.OnWithStreamHandle(ctx, input, handlers, cpy, handle)
}

// genericOnStartWithStreamInput 是通用流式输入开始回调。
//
// 用途：为流式输入提供通用回调入口。
// 这是 genericOnStartWithStreamInputHandle 的包装器。
//
// 应用场景：
//   - Graph 执行时处理流式输入
//   - 自动适配不同类型的流
func genericOnStartWithStreamInput(ctx context.Context, input streamReader) (context.Context, streamReader) {
	return icb.On(ctx, input, genericOnStartWithStreamInputHandle, callbacks.TimingOnStartWithStreamInput, true)
}

// onEndWithStreamOutput 是流式输出结束回调处理器。
//
// 用途：在组件结束流式输出时触发回调。
// 这是第四种回调时机。
//
// 处理逻辑：
//  1. 调用 icb.On 注册回调处理器
//  2. 设置回调时机为 TimingOnEndWithStreamOutput
//  3. 标记为后置回调
//
// 应用场景：
//   - 流式处理的结束清理
//   - 记录流完成时间
//   - 发送流结束事件
//
// 特点：
//   - 专门处理流式输出
//   - 在流处理完成后触发
func onEndWithStreamOutput[T any](ctx context.Context, output *schema.StreamReader[T]) (
	context.Context, *schema.StreamReader[T]) {

	return icb.On(ctx, output, icb.OnEndWithStreamOutputHandle[T], callbacks.TimingOnEndWithStreamOutput, false)
}

// genericOnEndWithStreamOutputHandle 是通用流式输出结束回调处理器。
//
// 用途：处理流式输出结束的通用回调逻辑。
// 与 genericOnStartWithStreamInputHandle 类似，但处理输出结束。
//
// 处理流程：
//  1. 复制输出流
//  2. 循环处理每个回调处理器
//  3. 类型解包和回调执行
//  4. 返回处理后的流
func genericOnEndWithStreamOutputHandle(ctx context.Context, output streamReader,
	runInfo *icb.RunInfo, handlers []icb.Handler) (context.Context, streamReader) {

	cpy := output.copy

	handle := func(ctx context.Context, handler icb.Handler, out streamReader) context.Context {
		out_, ok := unpackStreamReader[icb.CallbackOutput](out)
		if !ok {
			panic("impossible")
		}

		return handler.OnEndWithStreamOutput(ctx, runInfo, out_)
	}

	return icb.OnWithStreamHandle(ctx, output, handlers, cpy, handle)
}

// genericOnEndWithStreamOutput 是通用流式输出结束回调。
//
// 用途：为流式输出提供通用回调入口。
func genericOnEndWithStreamOutput(ctx context.Context, output streamReader) (context.Context, streamReader) {
	return icb.On(ctx, output, genericOnEndWithStreamOutputHandle, callbacks.TimingOnEndWithStreamOutput, false)
}

// onError 是错误回调处理器。
//
// 用途：在组件执行出错时触发回调。
// 这是第五种回调时机，专门处理错误情况。
//
// 处理逻辑：
//  1. 调用 icb.On 注册回调处理器
//  2. 设置回调时机为 TimingOnError
//  3. 标记为后置回调
//
// 应用场景：
//   - 错误日志记录
//   - 发送错误事件
//   - 错误监控和告警
//   - 错误恢复处理
//
// 特点：
//   - 错误发生时必触发
//   - 不会与 onEnd 同时触发
//   - 可以阻止错误向上传播（通过 ctx）
func onError(ctx context.Context, err error) (context.Context, error) {
	return icb.On(ctx, err, icb.OnErrorHandle, callbacks.TimingOnError, false)
}

// runWithCallbacks 是带回调的运行器装饰器。
//
// 用途：为任意函数添加回调能力。
// 这是 Eino 框架"装饰器模式"的经典实现 - 在不修改原函数的情况下添加横切关注点。
//
// 类型参数：
//   - I：输入类型
//   - O：输出类型
//   - TOption：选项类型
//
// 参数：
//   - r：原始函数（需要添加回调的函数）
//   - onStart：开始回调处理器
//   - onEnd：结束回调处理器
//   - onError：错误回调处理器
//
// 返回：
//   - 包装后的函数（自动添加回调）
//
// 执行流程：
//  1. 执行 onStart 回调（前置）
//  2. 调用原始函数 r
//     - 如果出错：执行 onError 回调，返回错误
//     - 如果成功：执行 onEnd 回调，返回结果
//  3. 返回处理结果
//
// 设计优势：
//   - 非侵入性：不需要修改原始函数
//   - 可组合：可以嵌套多个装饰器
//   - 职责分离：回调逻辑与业务逻辑分离
//   - 错误安全：确保回调在适当时机执行
//
// 示例：
//
//	func baseFunc(ctx context.Context, input string) (string, error) {
//	    return "result", nil
//	}
//
//	wrappedFunc := runWithCallbacks(
//	    baseFunc,
//	    onStart,
//	    onEnd,
//	    onError,
//	)
//
//	result, err := wrappedFunc(ctx, "input")  // 自动添加回调
func runWithCallbacks[I, O, TOption any](r func(context.Context, I, ...TOption) (O, error),
	onStart on[I], onEnd on[O], onError on[error]) func(context.Context, I, ...TOption) (O, error) {

	return func(ctx context.Context, input I, opts ...TOption) (output O, err error) {
		ctx, input = onStart(ctx, input)

		output, err = r(ctx, input, opts...)
		if err != nil {
			ctx, err = onError(ctx, err)
			return output, err
		}

		ctx, output = onEnd(ctx, output)

		return output, nil
	}
}

// invokeWithCallbacks 是 Invoke 模式的回调装饰器。
//
// 用途：为 Invoke 模式添加回调能力。
// 这是 runWithCallbacks 的特化版本，专门用于 Invoke 模式。
//
// Invoke 模式特点：
//   - 同步调用
//   - 一次性输入，一次性输出
//   - 非流式处理
//
// 回调配置：
//   - onStart：普通开始回调
//   - onEnd：普通结束回调
//   - onError：错误回调
//
// 应用场景：
//   - 同步 ChatModel 调用
//   - 同步 Tool 调用
//   - 任何非流式的组件调用
func invokeWithCallbacks[I, O, TOption any](i Invoke[I, O, TOption]) Invoke[I, O, TOption] {
	return runWithCallbacks(i, onStart[I], onEnd[O], onError)
}

// onGraphStart 是 Graph 开始的回调选择器。
//
// 用途：根据输入类型选择合适的开始回调处理器。
// 这是"适配器模式"的体现 - 统一不同类型回调的接口。
//
// 参数：
//   - ctx：上下文
//   - input：输入数据（可能是任意类型）
//   - isStream：是否为流式输入
//
// 返回：
//   - 包装后的上下文和输入
//
// 选择逻辑：
//   - 如果是流式：使用 genericOnStartWithStreamInput
//   - 如果是非流式：使用普通的 onStart
//
// 应用场景：
//   - Graph 节点执行开始时
//   - 自动适配流式/非流式输入
//   - 统一的回调入口
func onGraphStart(ctx context.Context, input any, isStream bool) (context.Context, any) {
	if isStream {
		return genericOnStartWithStreamInput(ctx, input.(streamReader))
	}
	return onStart(ctx, input)
}

// onGraphEnd 是 Graph 结束的回调选择器。
//
// 用途：根据输出类型选择合适的结束回调处理器。
// 与 onGraphStart 类似，但处理输出结束。
//
// 选择逻辑：
//   - 如果是流式：使用 genericOnEndWithStreamOutput
//   - 如果是非流式：使用普通的 onEnd
func onGraphEnd(ctx context.Context, output any, isStream bool) (context.Context, any) {
	if isStream {
		return genericOnEndWithStreamOutput(ctx, output.(streamReader))
	}
	return onEnd(ctx, output)
}

// onGraphError 是 Graph 错误的回调处理器。
//
// 用途：处理 Graph 执行过程中的错误。
// 注意：错误回调没有流式/非流式之分，统一使用 onError。
func onGraphError(ctx context.Context, err error) (context.Context, error) {
	return onError(ctx, err)
}

// streamWithCallbacks 是 Stream 模式的回调装饰器。
//
// 用途：为 Stream 模式添加回调能力。
// Stream 模式特点：
//   - 流式输入，流式输出
//   - 实时响应
//
// 回调配置：
//   - onStart：普通开始回调
//   - onEndWithStreamOutput：流式输出结束回调
//   - onError：错误回调
//
// 应用场景：
//   - 流式 ChatModel 调用
//   - 实时工具调用
//   - 任何需要流式处理的组件
func streamWithCallbacks[I, O, TOption any](s Stream[I, O, TOption]) Stream[I, O, TOption] {
	return runWithCallbacks(s, onStart[I], onEndWithStreamOutput[O], onError)
}

// collectWithCallbacks 是 Collect 模式的回调装饰器。
//
// 用途：为 Collect 模式添加回调能力。
// Collect 模式特点：
//   - 流式输入，聚合输出
//   - 收集所有流数据后输出单个结果
//
// 回调配置：
//   - onStartWithStreamInput：流式输入开始回调
//   - onEnd：普通结束回调
//   - onError：错误回调
//
// 应用场景：
//   - 收集流式数据并聚合
//   - 流式检索器收集结果
//   - 多路流数据合并
func collectWithCallbacks[I, O, TOption any](c Collect[I, O, TOption]) Collect[I, O, TOption] {
	return runWithCallbacks(c, onStartWithStreamInput[I], onEnd[O], onError)
}

// transformWithCallbacks 是 Transform 模式的回调装饰器。
//
// 用途：为 Transform 模式添加回调能力。
// Transform 模式特点：
//   - 流式输入，流式输出
//   - 流式数据转换
//
// 回调配置：
//   - onStartWithStreamInput：流式输入开始回调
//   - onEndWithStreamOutput：流式输出结束回调
//   - onError：错误回调
//
// 应用场景：
//   - 流式数据转换
//   - 流式过滤和映射
//   - 流式处理链
func transformWithCallbacks[I, O, TOption any](t Transform[I, O, TOption]) Transform[I, O, TOption] {
	return runWithCallbacks(t, onStartWithStreamInput[I], onEndWithStreamOutput[O], onError)
}

// initGraphCallbacks 初始化 Graph 级别的回调处理器。
//
// 用途：为整个 Graph 初始化回调处理器。
// 这些是全局回调，对 Graph 中的所有节点生效。
//
// 参数：
//   - ctx：上下文
//   - info：节点信息（可能为 nil）
//   - meta：执行器元数据（可能为 nil）
//   - opts：选项列表
//
// 返回：
//   - 包装了回调处理器的上下文
//
// 处理逻辑：
//  1. 创建 RunInfo 结构，包含组件信息和节点名称
//  2. 提取全局回调（paths 为空的回调）
//  3. 如果没有全局回调：复用现有处理器
//  4. 如果有全局回调：追加到上下文
//
// 应用场景：
//   - Graph 初始化时设置全局回调
//   - 为所有节点添加统一的监控
//   - 全局错误处理和日志记录
//
// 注意事项：
//   - 只处理全局回调（paths 为空）
//   - 节点特定回调在 initNodeCallbacks 中处理
func initGraphCallbacks(ctx context.Context, info *nodeInfo, meta *executorMeta, opts ...Option) context.Context {
	ri := &callbacks.RunInfo{}
	if meta != nil {
		ri.Component = meta.component
		ri.Type = meta.componentImplType
	}

	if info != nil {
		ri.Name = info.name
	}

	var cbs []callbacks.Handler
	for i := range opts {
		if len(opts[i].handler) != 0 && len(opts[i].paths) == 0 {
			cbs = append(cbs, opts[i].handler...)
		}
	}

	if len(cbs) == 0 {
		return icb.ReuseHandlers(ctx, ri)
	}

	return icb.AppendHandlers(ctx, ri, cbs...)
}

// initNodeCallbacks 初始化节点级别的回调处理器。
//
// 用途：为特定节点初始化回调处理器。
// 这些是节点特定回调，只对指定节点生效。
//
// 参数：
//   - ctx：上下文
//   - key：节点键名
//   - info：节点信息（可能为 nil）
//   - meta：执行器元数据（可能为 nil）
//   - opts：选项列表
//
// 返回：
//   - 包装了回调处理器的上下文
//
// 处理逻辑：
//  1. 创建 RunInfo 结构
//  2. 提取指定节点的回调（paths 包含该节点）
//  3. 匹配逻辑：检查路径是否指向当前节点
//  4. 追加匹配的回调处理器
//
// 匹配规则：
//   - 路径长度必须为 1
//   - 路径的第一个节点必须等于 key
//   - 只匹配直接指向该节点的回调
//
// 应用场景：
//   - 为特定节点添加专属回调
//   - 节点级别的监控和日志
//   - 条件性回调触发
func initNodeCallbacks(ctx context.Context, key string, info *nodeInfo, meta *executorMeta, opts ...Option) context.Context {
	ri := &callbacks.RunInfo{}
	if meta != nil {
		ri.Component = meta.component
		ri.Type = meta.componentImplType
	}

	if info != nil {
		ri.Name = info.name
	}

	var cbs []callbacks.Handler
	for i := range opts {
		if len(opts[i].handler) != 0 {
			if len(opts[i].paths) != 0 {
				for _, k := range opts[i].paths {
					if len(k.path) == 1 && k.path[0] == key {
						cbs = append(cbs, opts[i].handler...)
						break
					}
				}
			}
		}
	}

	if len(cbs) == 0 {
		return icb.ReuseHandlers(ctx, ri)
	}

	return icb.AppendHandlers(ctx, ri, cbs...)
}

// streamChunkConvertForCBOutput 是流式输出块的回调转换器。
//
// 用途：将流式输出块转换为回调输出格式。
// 这是一个类型适配器，确保回调系统能正确处理输出。
//
// 类型参数：
//   - O：输出类型
//
// 参数：
//   - o：输出值
//
// 返回：
//   - 回调输出格式的值
//   - 转换错误（总是返回 nil）
//
// 设计意义：
//   - 统一回调输入输出格式
//   - 类型安全转换
//   - 简化回调处理逻辑
//
// 应用场景：
//   - 流式输出的回调处理
//   - 类型转换和验证
//   - 回调链中的格式统一
func streamChunkConvertForCBOutput[O any](o O) (callbacks.CallbackOutput, error) {
	return o, nil
}

// streamChunkConvertForCBInput 是流式输入块的回调转换器。
//
// 用途：将流式输入块转换为回调输入格式。
// 与 streamChunkConvertForCBOutput 类似，但处理输入。
//
// 类型参数：
//   - I：输入类型
//
// 应用场景：
//   - 流式输入的回调处理
//   - 输入验证和转换
//   - 统一回调格式
func streamChunkConvertForCBInput[I any](i I) (callbacks.CallbackInput, error) {
	return i, nil
}

// toAnyList 是类型列表转换为 any 列表的工具函数。
//
// 用途：将泛型列表转换为 []any 类型。
// 这在需要统一处理不同类型数据时非常有用。
//
// 类型参数：
//   - T：源列表的元素类型
//
// 参数：
//   - in：输入列表
//
// 返回：
//   - 转换后的 []any 列表
//
// 转换过程：
//   - 创建等长的 []any 切片
//   - 逐个元素赋值（隐式类型转换）
//   - 返回转换后的切片
//
// 应用场景：
//   - 反射操作前的类型统一
//   - 日志记录（需要 any 类型）
//   - 泛型转非泛型的桥接
//
// 注意事项：
//   - 这是一个浅拷贝，只复制引用
//   - 适用于基本类型和指针类型
func toAnyList[T any](in []T) []any {
	ret := make([]any, len(in))
	for i := range in {
		ret[i] = in[i]
	}
	return ret
}

// assignableType 是类型可赋值性的枚举类型。
//
// 用途：表示类型之间的可赋值关系。
// 用于编译时的类型检查和推断。
//
// 取值：
//   - assignableTypeMustNot：绝对不可赋值
//   - assignableTypeMust：绝对可赋值
//   - assignableTypeMay：可能可赋值（需要运行时检查）
type assignableType uint8

const (
	// assignableTypeMustNot 表示类型绝对不可赋值。
	// 例如：不同类型的非接口类型之间
	assignableTypeMustNot assignableType = iota

	// assignableTypeMust 表示类型绝对可赋值。
	// 例如：相同类型、实现接口等
	assignableTypeMust

	// assignableTypeMay 表示类型可能可赋值。
	// 例如：接口类型，需要运行时检查具体类型是否实现了接口
	assignableTypeMay
)

// checkAssignable 检查输入类型相对于参数类型的可赋值性。
//
// 用途：在组件连接时进行类型兼容性检查。
// 这是 Eino 框架"类型安全"机制的重要一环。
//
// 参数：
//   - input：输入类型（被赋值的类型）
//   - arg：参数类型（要赋值的类型）
//
// 返回：
//   - assignableType：可赋值性枚举值
//
// 检查逻辑：
//  1. 任意一个为 nil：返回 MustNot
//  2. 两个类型相同：返回 Must
//  3. 参数是接口且输入实现了接口：返回 Must
//  4. 输入是接口且参数实现了输入接口：返回 May
//  5. 其他情况：返回 MustNot
//
// 应用场景：
//   - 节点连接时的类型检查
//   - 自动补全机制的类型推断
//   - 编译时的类型安全保证
//
// 设计意义：
//   - 三值逻辑：不仅有可/不可，还有可能
//   - 区分编译时和运行时检查
//   - 支持接口的多态性
func checkAssignable(input, arg reflect.Type) assignableType {
	if arg == nil || input == nil {
		return assignableTypeMustNot
	}

	if arg == input {
		return assignableTypeMust
	}

	if arg.Kind() == reflect.Interface && input.Implements(arg) {
		return assignableTypeMust
	}
	if input.Kind() == reflect.Interface {
		if arg.Implements(input) {
			return assignableTypeMay
		}
		return assignableTypeMustNot
	}

	return assignableTypeMustNot
}

// extractOption 提取并分发选项到指定节点。
//
// 用途：将全局选项和路径选项分发到对应的节点。
// 这是 Eino 框架"选项路由"机制的实现。
//
// 参数：
//   - nodes：节点映射表
//   - opts：选项列表
//
// 返回：
//   - optMap：节点到选项列表的映射
//   - error：提取过程中的错误
//
// 分发逻辑：
//
//  1. 全局选项（paths 为空）：
//     - 过滤选项按类型分发到各个节点
//     - 子图节点：传递整个 Option
//     - 组件节点：传递选项的具体值
//
//  2. 路径选项（paths 不为空）：
//     - 验证路径有效性
//     - 检查路径指向的节点是否存在
//     - 根据路径长度处理：
//     * 长度为 1：直接指向节点
//     * 长度 > 1：指向子图的子节点
//
//  3. 选项验证：
//     - 检查空路径
//     - 检查未知节点
//     - 检查类型匹配
//
// 应用场景：
//   - Chain/Graph/Workflow 的选项分发
//   - 回调处理器的路由
//   - 组件选项的精准传递
//
// 注意事项：
//   - 这是内部函数，通常由框架自动调用
//   - 选项的分发是按引用传递，不做深拷贝
func extractOption(nodes map[string]*chanCall, opts ...Option) (map[string][]any, error) {
	optMap := map[string][]any{}
	for _, opt := range opts {
		if len(opt.paths) == 0 {
			// 全局选项：分发给所有节点
			if len(opt.options) == 0 {
				continue // 跳过无选项的全局回调
			}
			for name, c := range nodes {
				if c.action.optionType == nil {
					// 子图节点：传递整个 Option 对象
					optMap[name] = append(optMap[name], opt)
				} else if reflect.TypeOf(opt.options[0]) == c.action.optionType {
					// 组件节点：传递选项的具体值
					optMap[name] = append(optMap[name], opt.options...)
				}
			}
		}
		// 处理路径选项
		for _, path := range opt.paths {
			if len(path.path) == 0 {
				return nil, fmt.Errorf("call option has designated an empty path")
			}

			var curNode *chanCall
			var ok bool
			if curNode, ok = nodes[path.path[0]]; !ok {
				return nil, fmt.Errorf("option has designated an unknown node: %s", path)
			}
			curNodeKey := path.path[0]

			if len(path.path) == 1 {
				// 路径长度为 1：直接指向节点
				if len(opt.options) == 0 {
					continue // 跳过无选项的节点回调
				}
				if curNode.action.optionType == nil {
					// 子图：创建新选项，移除路径信息
					nOpt := opt.deepCopy()
					nOpt.paths = []*NodePath{}
					optMap[curNodeKey] = append(optMap[curNodeKey], nOpt)
				} else {
					// 组件：直接传递选项值
					if curNode.action.optionType != reflect.TypeOf(opt.options[0]) {
						return nil, fmt.Errorf("option type[%s] is different from which the designated node[%s] expects[%s]",
							reflect.TypeOf(opt.options[0]).String(), path, curNode.action.optionType.String())
					}
					optMap[curNodeKey] = append(optMap[curNodeKey], opt.options...)
				}
			} else {
				// 路径长度 > 1：指向子图的子节点
				if curNode.action.optionType != nil {
					return nil, fmt.Errorf("cannot designate sub path of a component, path:%s", path)
				}
				// 传递给子图的子节点
				nOpt := opt.deepCopy()
				nOpt.paths = []*NodePath{NewNodePath(path.path[1:]...)}
				optMap[curNodeKey] = append(optMap[curNodeKey], nOpt)
			}
		}
	}

	return optMap, nil
}

// mapToList 是 map 转换为 list 的工具函数。
//
// 用途：将 map 的值转换为切片。
// 这是为了方便按顺序处理 map 中的值。
//
// 参数：
//   - m：输入 map
//
// 返回：
//   - 包含所有 map 值的切片
//
// 转换过程：
//  1. 预分配切片容量（避免动态扩容）
//  2. 遍历 map 的所有值
//  3. 将每个值追加到切片
//
// 应用场景：
//   - 并行处理 map 值
//   - 序列化 map 值
//   - 统一接口（需要 slice 而不是 map）
//
// 注意事项：
//   - 值的顺序是不确定的（Go map 的特性）
//   - 这是一个浅拷贝，只复制值的引用
//   - 如果需要确定性顺序，应该使用排序
func mapToList(m map[string]any) []any {
	ret := make([]any, 0, len(m))
	for _, v := range m {
		ret = append(ret, v)
	}
	return ret
}
