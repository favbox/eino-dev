package callbacks

import (
	"context"

	"github.com/favbox/eino/components"
	"github.com/favbox/eino/schema"
)

// RunInfo 回调运行信息结构体，用于在回调处理器中传递组件执行时的上下文信息。
type RunInfo struct {
	// Name 用于显示的图节点名称，并非唯一标识
	// 通过 compose.WithNodeName() 设置传递
	Name string
	// Type 组件的具体类型标识，描述组件的实现类型
	Type string
	// Component 组件在 Eino 框架中的分类类型
	// 如 ChatModel、Retriever、Tool 等预定义组件类型
	Component components.Component
}

// CallbackInput 回调输入类型。
//
// 作为组件输入到回调处理器的统一类型抽象。
type CallbackInput any

// CallbackOutput 回调输出类型。
//
// 作为组件输出到回调处理器的统一类型抽象。
type CallbackOutput any

// Handler 回调处理器接口。
//
// 定义了组件执行生命周期中的五个关键回调时机。
type Handler interface {
	// OnStart 组件开始执行时触发。
	//
	// 在组件实际执行业务逻辑之前调用，可用于初始化、日志记录等预处理操作
	OnStart(ctx context.Context, info *RunInfo, input CallbackInput) context.Context

	// OnEnd 组件正常执行结束时触发。
	//
	// 在组件成功完成执行后调用，可用于结果处理、指标收集等后处理操作
	OnEnd(ctx context.Context, info *RunInfo, output CallbackOutput) context.Context

	// OnError 组件执行出错时触发。
	//
	// 在组件执行过程中发生错误时调用，可用于错误记录、异常处理等容错操作
	OnError(ctx context.Context, info *RunInfo, err error) context.Context

	// OnStartWithStreamInput 流式输入开始时触发。
	//
	// 在组件开始接收流式输入时调用，专门用于流式组件的输入阶段处理
	OnStartWithStreamInput(ctx context.Context, info *RunInfo,
		input *schema.StreamReader[CallbackInput]) context.Context

	// OnEndWithStreamOutput 流式输出开始时触发。
	//
	// 在组件开始输出流式数据时调用，专门用于流式组件的输出阶段处理
	OnEndWithStreamOutput(ctx context.Context, info *RunInfo,
		output *schema.StreamReader[CallbackOutput]) context.Context
}

// CallbackTiming 回调时机类型。
//
// 用于标识组件执行过程中的不同回调时机点
type CallbackTiming uint8

// TimingChecker 回调时机检查器接口。
//
// 用于动态判断是否需要在特定时机执行回调逻辑
type TimingChecker interface {
	// Needed 判断在指定时机是否需要执行回调。
	//
	// 根据运行信息和回调时机动态决定是否触发回调处理逻辑，
	// 返回 true 表示需要执行回调，false 表示跳过回调
	Needed(ctx context.Context, info *RunInfo, timing CallbackTiming) bool
}
