package callbacks

import (
	"context"

	"github.com/favbox/eino/components"
	"github.com/favbox/eino/internal/callbacks"
	"github.com/favbox/eino/schema"
)

// 为组件开发者提供快速注入回调输入/输出方面的功能。
//
// 使用示例:
//
//	func (t *testChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (resp *schema.Message, err error) {
//		defer func() {
//			if err != nil {
//				callbacks.OnEnd(ctx, err)
//			}
//		}()
//
//		ctx = callbacks.OnStart(ctx, &model.CallbackInput{
//			Messages: input,
//			Tools:    nil,
//			Extra:    nil,
//		})
//
//		// 执行业务逻辑
//
//		ctx = callbacks.OnEnd(ctx, &model.CallbackOutput{
//			Message: resp,
//			Extra:   nil,
//		})
//
//		return resp, nil
//	}

// OnStart 调用特定上下文的 OnStart 逻辑，确保在进程开始时所有注册的处理器按添加顺序的逆序执行。
func OnStart[T any](ctx context.Context, input T) context.Context {
	ctx, _ = callbacks.On(ctx, input, callbacks.OnStartHandle[T], TimingOnStart, true)

	return ctx
}

// OnEnd 调用特定上下文的 OnEnd 逻辑，在进程结束时进行适当的清理和最终化处理。
// 处理器按添加顺序的正序执行。
func OnEnd[T any](ctx context.Context, output T) context.Context {
	ctx, _ = callbacks.On(ctx, output, callbacks.OnEndHandle[T], TimingOnEnd, false)

	return ctx
}

// OnStartWithStreamInput 调用特定上下文的 OnStartWithStreamInput 逻辑，确保每个输入流在处理器中被正确关闭。
// 处理器按添加顺序的逆序执行。
func OnStartWithStreamInput[T any](ctx context.Context, input *schema.StreamReader[T]) (
	nextCtx context.Context, newStreamReader *schema.StreamReader[T]) {

	return callbacks.On(ctx, input, callbacks.OnStartWithStreamInputHandle[T], TimingOnStartWithStreamInput, true)
}

// OnEndWithStreamOutput 调用特定上下文的 OnEndWithStreamOutput 逻辑，确保每个输出流在处理器中被正确关闭。
// 处理器按添加顺序的正序执行。
func OnEndWithStreamOutput[T any](ctx context.Context, output *schema.StreamReader[T]) (
	nextCtx context.Context, newStreamReader *schema.StreamReader[T]) {

	return callbacks.On(ctx, output, callbacks.OnEndWithStreamOutputHandle[T], TimingOnEndWithStreamOutput, false)
}

// OnError 调用特定上下文的 OnError 逻辑，注意流中的错误不会在此处表示。
// 处理器按添加顺序的正序执行。
func OnError(ctx context.Context, err error) context.Context {
	ctx, _ = callbacks.On(ctx, err, callbacks.OnErrorHandle, TimingOnError, false)

	return ctx
}

// EnsureRunInfo 确保上下文中的 RunInfo 与给定类型和组件匹配。
// 如果当前回调管理器不匹配或不存在，它会创建一个新的管理器同时保留现有处理器。
// 如果上下文中之前没有全局回调处理器，将会初始化全局回调处理器。
func EnsureRunInfo(ctx context.Context, typ string, comp components.Component) context.Context {
	return callbacks.EnsureRunInfo(ctx, typ, comp)
}

// ReuseHandlers 使用提供的 RunInfo 初始化新上下文，同时复用已存在的相同处理器。
// 如果上下文中之前没有全局回调处理器，将会初始化全局回调处理器。
func ReuseHandlers(ctx context.Context, info *RunInfo) context.Context {
	return callbacks.ReuseHandlers(ctx, info)
}

// InitCallbacks 使用提供的 RunInfo 和处理器初始化新上下文。
// 任何先前为此上下文设置的 RunInfo 和处理器都将被覆盖。
func InitCallbacks(ctx context.Context, info *RunInfo, handlers ...Handler) context.Context {
	return callbacks.InitCallbacks(ctx, info, handlers...)
}
