package callbacks

import (
	"context"

	"github.com/favbox/eino/schema"
)

// HandlerBuilder 回调处理器构建器结构体。
//
// 提供链式构建回调处理器的能力，支持通过函数式编程的方式定义五种回调时机对应的处理逻辑，
// 自动实现 Handler 接口和 TimingChecker 接口，简化回调处理器的创建过程。
type HandlerBuilder struct {
	// onStartFn 组件开始执行时的处理函数
	onStartFn func(ctx context.Context, info *RunInfo, input CallbackInput) context.Context

	// onEndFn 组件正常结束时的处理函数
	onEndFn func(ctx context.Context, info *RunInfo, output CallbackOutput) context.Context

	// onErrorFn 组件执行出错时的处理函数
	onErrorFn func(ctx context.Context, info *RunInfo, err error) context.Context

	// onStartWithStreamInputFn 流式输入开始时的处理函数
	onStartWithStreamInputFn func(ctx context.Context, info *RunInfo, input *schema.StreamReader[CallbackInput]) context.Context

	// onEndWithStreamOutputFn 流式输出开始时的处理函数
	onEndWithStreamOutputFn func(ctx context.Context, info *RunInfo, output *schema.StreamReader[CallbackOutput]) context.Context
}

// handlerImpl 回调处理器实现结构体。
//
// 嵌入 HandlerBuilder 并实现 Handler 接口和 TimingChecker 接口，
// 作为 HandlerBuilder 构建结果的内部实现，将构建器的函数字段转换为接口方法。
type handlerImpl struct {
	HandlerBuilder
}

// OnStart 实现 Handler 接口的组件开始回调方法。
func (hb *handlerImpl) OnStart(ctx context.Context, info *RunInfo, input CallbackInput) context.Context {
	return hb.onStartFn(ctx, info, input)
}

// OnEnd 实现 Handler 接口的组件结束回调方法。
func (hb *handlerImpl) OnEnd(ctx context.Context, info *RunInfo, output CallbackOutput) context.Context {
	return hb.onEndFn(ctx, info, output)
}

// OnError 实现 Handler 接口的组件错误回调方法。
func (hb *handlerImpl) OnError(ctx context.Context, info *RunInfo, err error) context.Context {
	return hb.onErrorFn(ctx, info, err)
}

// OnStartWithStreamInput 实现 Handler 接口的流式输入开始回调方法。
func (hb *handlerImpl) OnStartWithStreamInput(ctx context.Context, info *RunInfo,
	input *schema.StreamReader[CallbackInput]) context.Context {

	return hb.onStartWithStreamInputFn(ctx, info, input)
}

// OnEndWithStreamOutput 实现 Handler 接口的流式输出开始回调方法。
func (hb *handlerImpl) OnEndWithStreamOutput(ctx context.Context, info *RunInfo,
	output *schema.StreamReader[CallbackOutput]) context.Context {

	return hb.onEndWithStreamOutputFn(ctx, info, output)
}

// Needed 实现 TimingChecker 接口的时机检查方法。
//
// 根据指定的回调时机判断是否需要执行该处理器，通过检查对应函数字段是否为空来确定。
func (hb *handlerImpl) Needed(_ context.Context, _ *RunInfo, timing CallbackTiming) bool {
	switch timing {
	case TimingOnStart:
		return hb.onStartFn != nil
	case TimingOnEnd:
		return hb.onEndFn != nil
	case TimingOnError:
		return hb.onErrorFn != nil
	case TimingOnStartWithStreamInput:
		return hb.onStartWithStreamInputFn != nil
	case TimingOnEndWithStreamOutput:
		return hb.onEndWithStreamOutputFn != nil
	default:
		return false
	}
}

// NewHandlerBuilder 创建并返回新的 HandlerBuilder 实例。
//
// HandlerBuilder 用于构建具有自定义回调函数的 Handler 实例，
// 支持链式调用和函数式编程风格，简化回调处理器的创建过程。
func NewHandlerBuilder() *HandlerBuilder {
	return &HandlerBuilder{}
}

// OnStartFn 设置组件开始执行时的回调函数。
//
// 支持链式调用，返回构建器本身以便继续配置其他回调函数。
func (hb *HandlerBuilder) OnStartFn(
	fn func(ctx context.Context, info *RunInfo, input CallbackInput) context.Context) *HandlerBuilder {

	hb.onStartFn = fn
	return hb
}

// OnEndFn 设置组件正常结束时的回调函数。
//
// 支持链式调用，返回构建器本身以便继续配置其他回调函数。
func (hb *HandlerBuilder) OnEndFn(
	fn func(ctx context.Context, info *RunInfo, output CallbackOutput) context.Context) *HandlerBuilder {

	hb.onEndFn = fn
	return hb
}

// OnErrorFn 设置组件执行出错时的回调函数。
//
// 支持链式调用，返回构建器本身以便继续配置其他回调函数。
func (hb *HandlerBuilder) OnErrorFn(
	fn func(ctx context.Context, info *RunInfo, err error) context.Context) *HandlerBuilder {

	hb.onErrorFn = fn
	return hb
}

// OnStartWithStreamInputFn 设置流式输入开始时的回调函数。
//
// 支持链式调用，返回构建器本身以便继续配置其他回调函数。
func (hb *HandlerBuilder) OnStartWithStreamInputFn(
	fn func(ctx context.Context, info *RunInfo, input *schema.StreamReader[CallbackInput]) context.Context) *HandlerBuilder {

	hb.onStartWithStreamInputFn = fn
	return hb
}

// OnEndWithStreamOutputFn 设置流式输出开始时的回调函数。
//
// 支持链式调用，返回构建器本身以便继续配置其他回调函数。
func (hb *HandlerBuilder) OnEndWithStreamOutputFn(
	fn func(ctx context.Context, info *RunInfo, output *schema.StreamReader[CallbackOutput]) context.Context) *HandlerBuilder {

	hb.onEndWithStreamOutputFn = fn
	return hb
}

// Build 构建并返回具有所设置函数的 Handler 实例。
//
// 将构建器配置的所有函数封装成一个完整的 Handler 实现，
// 自动实现 Handler 接口和 TimingChecker 接口。
func (hb *HandlerBuilder) Build() Handler {
	return &handlerImpl{*hb}
}
