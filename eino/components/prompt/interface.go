package prompt

import (
	"context"

	"github.com/favbox/eino/schema"
)

// ChatTemplate 接口定义了提示词模板的格式化能力。
//
// 用于将模板与变量结合，生成符合模型输入格式的消息列表。
type ChatTemplate interface {
	// Format 使用给定的上下文和变量格式化模板。
	//
	// 该方法将模板中的占位符替换为实际变量值，
	// 生成标准格式的消息列表供聊天模型使用。
	//
	// 参数：
	//   - ctx: 上下文信息，用于取消、超时和传递请求相关数据
	// 	- vs：变量映射表，包含模板中使用的变量名和值
	// 	- opts：可选的配置参数，用于定制格式化行为
	//
	// 返回：
	//   - []*schema.Message: 格式化后的消息列表，可以直接传递给聊天模型
	//   - error: 格式化过程中的错误（如果有）
	Format(ctx context.Context, vs map[string]any, opts ...Option) ([]*schema.Message, error)
}
