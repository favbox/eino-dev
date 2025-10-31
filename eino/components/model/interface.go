package model

import (
	"context"

	"github.com/favbox/eino/schema"
)

// BaseChatModel 定义了聊天模型的基础接口。
//
// 它提供生成完整输出和流式输出的方法。
// 该接口作为所有聊天模型实现的基础。
//
//go:generate  mockgen -destination ../../internal/mock/components/model/ChatModel_mock.go --package model -source interface.go
type BaseChatModel interface {
	// Generate 在给定上下文中生成完整的聊天响应。
	//
	// 参数：
	//   - ctx: 上下文信息，用于取消、超时和传递请求相关数据
	//   - input: 输入消息列表，包含对话历史和当前用户输入
	//   - opts: 可选的配置参数，用于定制模型行为
	//
	// 返回：
	//   - *schema.Message: 完整的模型响应消息
	//   - error: 生成过程中的错误（如果有）
	Generate(ctx context.Context, input []*schema.Message, opts ...Option) (*schema.Message, error)

	// Stream 在给定上下文中生成流式聊天响应。
	//
	// 该方法返回流式读取器，允许客户端逐步接收模型输出，
	// 适用于需要实时显示生成内容的场景。
	//
	// 参数：
	//   - ctx: 上下文信息，用于取消、超时和传递请求相关数据
	//   - input: 输入消息列表，包含对话历史和当前用户输入
	//   - opts: 可选的配置参数，用于定制模型行为
	//
	// 返回：
	//   - *schema.StreamReader[*schema.Message]: 消息流式读取器
	//   - error: 流式生成过程中的错误（如果有）
	Stream(ctx context.Context, input []*schema.Message, opts ...Option) (*schema.StreamReader[*schema.Message], error)
}

// ToolCallingChatModel 扩展了 BaseChatModel，添加了工具调用能力。
//
// 它提供了 WithTools 方法，返回绑定指定工具的新实例，
// 避免了状态修改和并发问题。
type ToolCallingChatModel interface {
	BaseChatModel

	// WithTools 返回一个新的 ToolCallingChatModel 实例，该实例已绑定指定的工具。
	//
	// 该方法不会修改当前实例，使其在并发使用中更安全。
	// 每次调用都会创建一个新的实例，确保工具配置的隔离性。
	//
	// 参数：
	// 	- tools：要绑定的工具信息列表
	//
	// 返回：
	//
	// 	- ToolCallingChatModel：绑定工具后的新实例
	// 	- error：工具绑定过程中的错误（如果有）
	//
	// 优势：
	//
	// 	- 线程安全：无共享状态，避免并发问题
	// 	- 不可变设计：每次调用返回新实例，保持原始实例不变
	// 	- 易于测试：可以轻松创建具有不同工具配置的不同实例
	WithTools(tools []*schema.ToolInfo) (ToolCallingChatModel, error)
}
