package tool

import (
	"context"

	"github.com/favbox/eino/schema"
)

// BaseTool 接口定义了工具的基本信息获取能力。
//
// 用于聊天模型的意图识别和工具节点执行前的信息准备。
type BaseTool interface {
	// Info 返回工具的元信息。
	//
	// 工具信息包含工具名称、描述、参数模式等，用于：
	// 	- 聊天模型理解工具能力和用途
	// 	- 工具注册和发现
	// 	- 工具调用的参数验证
	//
	// 参数：
	// 	- ctx：上下文信息，用于取消、超时和传递请求相关数据
	//
	// 返回：
	// 	- *schema.ToolInfo：工具的完整信息，包括名称、描述、参数定义等
	// 	- error：获取工具信息过程中的错误（如果有）
	Info(ctx context.Context) (*schema.ToolInfo, error)
}

// InvokableTool 接口定义了可调用的工具。
//
// 适用于聊天模型的意图识别和工具节点的同步执行。
type InvokableTool interface {
	BaseTool

	// InvokableRun 使用 JSON 格式的参数调用工具函数。
	//
	// 该方法执行工具的核心逻辑，并返回字符串格式的结果。
	// 适用于需要同步等待工具执行完成的场景。
	//
	// 参数：
	// 	- ctx：上下文信息，用于取消、超时和传递请求相关数据
	// 	- argumentsInJSON：JSON 格式的工具参数，结构由工具定义决定
	// 	- opts：可选的配置参数，用于定制工具执行行为
	//
	// 返回：
	// 	- string：工具执行的结果，格式由工具实现决定
	// 	- error：工具执行过程中的错误（如果有）
	InvokableRun(ctx context.Context, argumentsInJSON string, opts ...Option) (string, error)
}

// StreamableTool 接口定义了可流式执行的工具。
type StreamableTool interface {
	BaseTool

	// StreamableRun 使用 JSON 格式的参数流式执行工具。
	//
	// 该方法返回流式读取器，允许客户端逐步接收工具的输出，
	// 适用于长时间运行或需要实时反馈的工具。
	//
	// 参数：
	//   - ctx: 上下文信息，用于取消、超时和传递请求相关数据
	//   - argumentsInJSON: JSON 格式的工具参数，结构由工具定义决定
	//   - opts: 可选的配置参数，用于定制工具执行行为
	//
	// 返回：
	//   - *schema.StreamReader[string]: 结果流式读取器，以字符串形式返回数据
	//   - error: 工具执行过程中的错误（如果有）
	StreamableRun(ctx context.Context, argumentsInJSON string, opts ...Option) (*schema.StreamReader[string], error)
}
