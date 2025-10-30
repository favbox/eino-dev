package prompt

import (
	"context"

	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/components"
	"github.com/favbox/eino/schema"
)

// DefaultChatTemplate 是默认的聊天模板实现。
//
// 它实现了 ChatTemplate 接口，提供了基本的模板格式化功能。
type DefaultChatTemplate struct {
	// templates 是聊天模板的消息模板列表。
	// 每个模板可以包含多个消息，用于定义对话的结构和内容。
	templates []schema.MessagesTemplate

	// formatType 是聊天模板的格式类型。
	// 用于指定消息的格式化方式（如 FString、Jinja2、GoTemplate）
	formatType schema.FormatType
}

// FromMessages 从给定的消息模板和格式类型创建一个新的默认聊天模板。
//
// 这是一个便捷的构造方法，用于快速创建聊天模板实例。
//
// 示例：
//
//	// 创建包含变量替换的模板
//	template := prompt.FromMessages(
//		schema.FString,
//		&schema.Message{Content: "你好，{name}！"},
//		&schema.Message{Content: "最近怎么样？"},
//	)
//
//	// 在链式或图形编排中使用
//	chain := compose.NewChain[map[string]any, []*schema.Message]()
//	chain.AppendChatTemplate(template)
//
// 参数：
//   - formatType: 消息格式类型，指定模板的格式化方式
//   - templates：可变参数的消息模板列表
//
// 返回：
//   - *DefaultChatTemplate：创建的聊天模板实例
func FromMessages(formatType schema.FormatType, templates ...schema.MessagesTemplate) *DefaultChatTemplate {
	return &DefaultChatTemplate{
		templates:  templates,
		formatType: formatType,
	}
}

// Format 使用给定的上下文和变量格式化聊天模板。
//
// 该方法会：
//  1. 确保回调运行信息正确设置
//  2. 触发 OnStart 回调，记录格式化开始
//  3. 遍历所有模板并格式化
//  4. 如果发生错误，触发 OnError 回调
//  5. 成功后触发 OnEnd 回调，记录格式化结果
//
// 参数：
//   - ctx: 上下文信息
//   - vs: 变量映射表，用于替换模板中的占位符
//   - opts: 可选参数（当前未使用）
//
// 返回：
//   - result: 格式化后的消息列表
//   - err: 格式化过程中的错误（如果有）
func (t *DefaultChatTemplate) Format(ctx context.Context,
	vs map[string]any, opts ...Option) (result []*schema.Message, err error) {
	// 设置回调运行信息，标识当前为提示词模板组件
	callbacks.EnsureRunInfo(ctx, t.GetType(), components.ComponentOfPrompt)

	// 触发 OnStart 回调，记录格式化开始和输入变量
	callbacks.OnStart(ctx, &CallbackInput{
		Variables: vs,
		Templates: t.templates,
	})

	// 延迟处理：如果发生错误，触发 OnError 回调
	defer func() {
		if err != nil {
			_ = callbacks.OnError(ctx, err)
		}
	}()

	// 初始化结果列表，预分配容量以提高性能
	result = make([]*schema.Message, 0, len(t.templates))

	// 	遍历所有模板并格式化
	for _, template := range t.templates {
		// 	使用指定的格式化类型格式化模板
		msgs, err := template.Format(ctx, vs, t.formatType)
		if err != nil {
			return nil, err
		}

		// 将格式化后的消息追加到结果中
		result = append(result, msgs...)
	}

	// 触发 OnEnd 回调，记录格式化成功和结果
	_ = callbacks.OnEnd(ctx, &CallbackOutput{
		Result:    result,
		Templates: t.templates,
	})

	return result, nil
}

// GetType 返回聊天模板的类型（“Default”）。
//
// 用于组件的识别和调试。
func (t *DefaultChatTemplate) GetType() string {
	return "Default"
}

// IsCallbacksEnabled 检查聊天模板是否启用了回调机制。
//
// 返回 true，表示该组件使用自定义的回调控制逻辑。
func (t *DefaultChatTemplate) IsCallbacksEnabled() bool {
	return true
}
