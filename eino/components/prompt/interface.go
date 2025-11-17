package prompt

import (
	"context"

	"github.com/favbox/eino/schema"
)

var _ ChatTemplate = &DefaultChatTemplate{}

// ChatTemplate 提示词模板接口，用于格式化模板生成消息列表。
type ChatTemplate interface {
	// Format 使用给定的变量格式化模板，生成消息列表。
	Format(ctx context.Context, vs map[string]any, opts ...Option) ([]*schema.Message, error)
}
