package callbacks

import "github.com/favbox/eino/internal/callbacks"

// RunInfo 回调运行时信息类型别名。
//
// 指向内部包中的 RunInfo 结构体，包含组件执行过程中的上下文信息，
// 提供对外部用户友好的类型别名访问方式。
type RunInfo = callbacks.RunInfo

// CallbackInput 回调输入类型别名。
//
// 指向内部包中的 CallbackInput 类型，表示组件传递给回调处理器的输入数据，
// 具体的输入类型由组件定义，需要通过类型断言或转换函数获取正确的类型。
//
// 例如，components/model/interface.go 中的 CallbackInput 定义为：
//
//	type CallbackInput struct {
//		Messages []*schema.Message
//		Config   *Config
//		Extra map[string]any
//	}
//
// 并提供了 model.ConvCallbackInput() 函数来转换 CallbackInput 为 *model.CallbackInput
// 在回调处理器中，可以使用以下代码获取输入：
//
//	modelCallbackInput := model.ConvCallbackInput(in)
//	if modelCallbackInput == nil {
//		// 不是模型回调输入，直接忽略
//		return
//	}
type CallbackInput = callbacks.CallbackInput

// CallbackOutput 回调输出类型别名。
//
// 指向内部包中的 CallbackOutput 类型，表示组件执行完成后传递给回调处理器的输出数据，
// 与 CallbackInput 配套使用，完成回调处理器的数据流闭环。
type CallbackOutput = callbacks.CallbackOutput

// Handler 回调处理器接口类型别名。
//
// 指向内部包中的 Handler 接口，定义了组件执行生命周期中的五个关键回调时机，
// 为回调处理器的实现提供统一的接口规范。
type Handler = callbacks.Handler

// AppendGlobalHandlers 追加全局回调处理器。
//
// 将给定的处理器追加到全局回调处理器列表中，这是添加全局回调处理器的首选方式，
// 因为它会保留现有的处理器。全局回调处理器将在所有节点中执行，在 CallOption 中
// 用户特定的处理器之前执行。注意：此函数不是线程安全的，只能在进程初始化期间调用。
func AppendGlobalHandlers(handlers ...Handler) {
	callbacks.GlobalHandlers = append(callbacks.GlobalHandlers, handlers...)
}

// CallbackTiming 回调时机枚举类型。
//
// 枚举了所有回调方面的执行时机，用于标识组件执行生命周期中的不同回调触发点。
type CallbackTiming = callbacks.CallbackTiming

const (
	// TimingOnStart 组件开始执行时机
	TimingOnStart CallbackTiming = iota
	// TimingOnEnd 组件结束执行时机
	TimingOnEnd
	// TimingOnError 组件错误执行时机
	TimingOnError
	// TimingOnStartWithStreamInput 流式输入开始时机
	TimingOnStartWithStreamInput
	// TimingOnEndWithStreamOutput 流式输出开始时机
	TimingOnEndWithStreamOutput
)

// TimingChecker 回调时机检查器接口。
//
// 检查处理器是否需要在给定的回调时机执行，推荐回调处理器实现此接口，但不是强制性的。
// 如果回调处理器是通过 callbacks.HandlerHelper 或 handlerBuilder 创建的，则此接口会自动实现。
// Eino 的回调机制将尝试使用此接口来确定是否需要为给定时机执行任何处理器。
// 同样，不需要该时机的回调处理器将被跳过。
type TimingChecker = callbacks.TimingChecker
