package callbacks

import (
	"context"

	"github.com/favbox/eino/components"
	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// InitCallbacks 初始化回调系统并创建上下文。
//
// 在组件执行开始时调用，用于设置回调处理器链并返回包含管理器的上下文
func InitCallbacks(ctx context.Context, info *RunInfo, handlers ...Handler) context.Context {
	// 尝试创建回调管理器实例
	mgr, ok := newManager(info, handlers...)
	if ok {
		// 管理器创建成功，将其存储到上下文中
		return ctxWithManager(ctx, mgr)
	}

	// 没有处理器需要管理，存储空管理器到上下文中
	return ctxWithManager(ctx, nil)
}

// ReuseHandlers 复用现有回调处理器并更新运行信息。
//
// 在保持现有回调处理器链不变的情况下，仅更新运行信息并返回新的上下文
func ReuseHandlers(ctx context.Context, info *RunInfo) context.Context {
	// 尝试从上下文中获取现有的回调管理器
	cbm, ok := managerFromCtx(ctx)
	if !ok {
		// 没有现有管理器，创建新的管理器
		return InitCallbacks(ctx, info)
	}

	// 复用现有处理器，仅更新运行信息
	return ctxWithManager(ctx, cbm.withRunInfo(info))
}

// EnsureRunInfo 确保上下文中包含有效的运行信息。
//
// 检查并确保上下文中的回调管理器包含完整的运行信息，必要时进行初始化或补充
func EnsureRunInfo(ctx context.Context, typ string, comp components.Component) context.Context {
	// 尝试从上下文中获取回调管理器
	cbm, ok := managerFromCtx(ctx)
	if !ok {
		// 上下文中没有管理器，创建新的管理器并初始化运行信息
		return InitCallbacks(ctx, &RunInfo{
			Type:      typ,
			Component: comp,
		})
	}

	// 检查管理器中的运行信息是否为空
	if cbm.runInfo == nil {
		// 运行信息为空，复用现有处理器并补充运行信息
		return ReuseHandlers(ctx, &RunInfo{
			Type:      typ,
			Component: comp,
		})
	}

	// 上下文和运行信息都已存在，直接返回原上下文
	return ctx
}

// AppendHandlers 追加回调处理器到现有处理器链中。
//
// 在保持现有回调处理器的基础上，追加新的处理器并创建包含完整处理器链的新上下文。
func AppendHandlers(ctx context.Context, info *RunInfo, handlers ...Handler) context.Context {
	// 尝试从上下文中获取现有的回调管理器
	cbm, ok := managerFromCtx(ctx)
	if !ok {
		// 没有现有管理器，直接使用新处理器初始化
		return InitCallbacks(ctx, info, handlers...)
	}

	// 合并现有处理器和新处理器
	nh := make([]Handler, len(cbm.handlers)+len(handlers))
	copy(nh[:len(cbm.handlers)], cbm.handlers)
	copy(nh[len(cbm.handlers):], handlers)

	// 使用合并后的处理器链创建新的管理器
	return InitCallbacks(ctx, info, nh...)
}

// Handle 回调处理函数类型，泛型类型 T 表示输入输出数据的类型。
//
// 定义了执行回调处理的标准函数签名，支持类型安全的输入输出处理。
type Handle[T any] func(context.Context, T, *RunInfo, []Handler) (context.Context, T)

// On 执行指定时机的回调处理。
//
// 从上下文中获取回调管理器，根据时机过滤适用的处理器，并执行指定的回调处理函数。
func On[T any](ctx context.Context, inOut T, handle Handle[T], timing CallbackTiming, start bool) (context.Context, T) {
	// 尝试从上下文中获取回调管理器
	mgr, ok := managerFromCtx(ctx)
	if !ok {
		// 没有管理器，直接返回原始输入输出
		return ctx, inOut
	}

	// 创建管理器副本以避免修改原始管理器
	nMgr := *mgr

	// 处理运行信息的获取和管理
	var info *RunInfo
	if start {
		// 开始状态：从管理器获取运行信息并清除管理器中的信息
		info = nMgr.runInfo
		nMgr.runInfo = nil
		ctx = context.WithValue(ctx, CtxRunInfoKey{}, info)
	} else {
		// 非开始状态：从管理器或上下文中获取运行信息
		if nMgr.runInfo != nil {
			info = nMgr.runInfo
		} else {
			info, _ = ctx.Value(CtxRunInfoKey{}).(*RunInfo)
		}
	}

	// 过滤适用于当前时机的处理器
	hs := make([]Handler, 0, len(nMgr.handlers)+len(nMgr.globalHandlers))
	for _, handler := range append(nMgr.handlers, nMgr.globalHandlers...) {
		// 检查处理器是否需要在当前时机执行
		timingChecker, ok_ := handler.(TimingChecker)
		if !ok_ || timingChecker.Needed(ctx, info, timing) {
			hs = append(hs, handler)
		}
	}

	// 执行回调处理函数
	var out T
	ctx, out = handle(ctx, inOut, info, hs)

	// 返回包含更新后管理器的上下文和处理结果
	return ctxWithManager(ctx, &nMgr), out
}

// OnStartHandle 执行组件开始时的回调处理。
//
// 按照从后到前的顺序遍历处理器链，执行每个处理器的OnStart方法，
// 确保后注册的处理器先执行，实现类似栈的调用顺序。
func OnStartHandle[T any](ctx context.Context, input T, runInfo *RunInfo, handlers []Handler) (context.Context, T) {
	// 按照从后到前的顺序执行处理器
	for i := len(handlers) - 1; i >= 0; i-- {
		// 调用每个处理器的OnStart方法
		ctx = handlers[i].OnStart(ctx, runInfo, input)
	}

	// 返回处理后的上下文和原始输入
	return ctx, input
}

// OnEndHandle 执行组件结束时的回调处理。
//
// 按照从前往后的顺序遍历处理器链，执行每个处理器的OnEnd方法，
// 确保先注册的处理器先执行，实现类似队列的调用顺序，与OnStartHandle形成对称。
func OnEndHandle[T any](ctx context.Context, output T, runInfo *RunInfo, handlers []Handler) (context.Context, T) {
	// 按照从前往后的顺序执行处理器
	for _, handler := range handlers {
		// 调用每个处理器的OnEnd方法
		ctx = handler.OnEnd(ctx, runInfo, output)
	}

	// 返回处理后的上下文和原始输出
	return ctx, output
}

// OnWithStreamHandle 执行流式数据的自定义回调处理。
//
// 支持通过自定义的处理函数对流式数据进行个性化处理，适用于需要特殊流处理逻辑的场景，
// 与 OnStartHandle 和 OnEndHandle 的固定模式不同，提供了更大的灵活性和扩展性。
func OnWithStreamHandle[S any](
	ctx context.Context,
	inOut S,
	handlers []Handler,
	cpy func(int) []S,
	handle func(context.Context, Handler, S) context.Context) (context.Context, S) {
	// 检查是否有处理器需要执行
	if len(handlers) == 0 {
		return ctx, inOut
	}

	// 创建处理器数量的副本数组，最后一个位置存储最终结果
	inOuts := cpy(len(handlers) + 1)

	// 遍历每个处理器，执行自定义的处理逻辑
	for i, handler := range handlers {
		ctx = handle(ctx, handler, inOuts[i])
	}

	// 返回最终处理后的上下文和结果
	return ctx, inOuts[len(inOuts)-1]
}

// OnStartWithStreamInputHandle 执行流式输入开始时的回调处理。
//
// 专门用于处理组件开始接收流式输入时的回调，通过逆序处理器并转换流数据格式，
// 确保流式输入能够正确地触发相应处理器的OnStartWithStreamInput方法。
func OnStartWithStreamInputHandle[T any](ctx context.Context, input *schema.StreamReader[T],
	runInfo *RunInfo, handlers []Handler) (context.Context, *schema.StreamReader[T]) {
	// 逆序排列处理器，保持与OnStartHandle的一致性
	handlers = generic.Reverse(handlers)

	// 使用流数据的复制函数
	cpy := input.Copy

	// 定义自定义处理函数：转换流数据格式并调用处理器的流式输入方法
	handle := func(ctx context.Context, handler Handler, in *schema.StreamReader[T]) context.Context {
		// 将流数据转换为CallbackInput格式
		in_ := schema.StreamReaderWithConvert(in, func(i T) (CallbackInput, error) {
			return i, nil
		})
		// 调用处理器的 OnStartWithStreamInput 方法
		return handler.OnStartWithStreamInput(ctx, runInfo, in_)
	}

	// 使用通用的流式处理函数执行回调
	return OnWithStreamHandle(ctx, input, handlers, cpy, handle)
}

// OnEndWithStreamOutputHandle 执行流式输出开始时的回调处理。
//
// 专门用于处理组件开始输出流式数据时的回调，通过转换流数据格式并调用相应的处理器方法，
// 确保流式输出能够正确地触发处理器的 OnEndWithStreamOutput 方法，与 OnStartWithStreamInputHandle 形成对称。
func OnEndWithStreamOutputHandle[T any](ctx context.Context, output *schema.StreamReader[T],
	runInfo *RunInfo, handlers []Handler) (context.Context, *schema.StreamReader[T]) {
	// 使用流数据的复制函数
	cpy := output.Copy

	// 定义自定义处理函数：转换流数据格式并调用处理器的流式输出方法
	handle := func(ctx context.Context, handler Handler, out *schema.StreamReader[T]) context.Context {
		// 将流数据转换为CallbackOutput格式
		out_ := schema.StreamReaderWithConvert(out, func(i T) (CallbackOutput, error) {
			return i, nil
		})
		// 调用处理器的OnEndWithStreamOutput方法
		return handler.OnEndWithStreamOutput(ctx, runInfo, out_)
	}

	// 使用通用的流式处理函数执行回调
	return OnWithStreamHandle(ctx, output, handlers, cpy, handle)
}

// OnErrorHandle 执行组件错误时的回调处理。
//
// 按照从前往后的顺序遍历处理器链，执行每个处理器的OnError方法，
// 确保错误信息能够被所有处理器捕获和处理，提供统一的错误处理机制。
func OnErrorHandle(ctx context.Context, err error,
	runInfo *RunInfo, handlers []Handler) (context.Context, error) {
	// 按照从前往后的顺序执行处理器
	for _, handler := range handlers {
		// 调用每个处理器的OnError方法处理错误
		ctx = handler.OnError(ctx, runInfo, err)
	}

	// 返回处理后的上下文和原始错误
	return ctx, err
}
