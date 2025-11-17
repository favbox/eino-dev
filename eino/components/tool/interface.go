package tool

import (
	"context"

	"github.com/favbox/eino/schema"
)

// BaseTool 工具基础接口。
// 提供工具的元信息，用于工具注册和 ChatModel 理解工具能力。
type BaseTool interface {
	// Info 返回工具的元信息。
	Info(ctx context.Context) (*schema.ToolInfo, error)
}

// InvokableTool 可同步调用的工具接口。
type InvokableTool interface {
	BaseTool

	// InvokableRun 使用 JSON 格式的参数调用工具并返回结果。
	InvokableRun(ctx context.Context, argumentsInJSON string, opts ...Option) (string, error)
}

// StreamableTool 可流式执行的工具接口。
type StreamableTool interface {
	BaseTool

	// StreamableRun 使用 JSON 格式的参数流式执行工具并返回结果流。
	StreamableRun(ctx context.Context, argumentsInJSON string, opts ...Option) (*schema.StreamReader[string], error)
}
