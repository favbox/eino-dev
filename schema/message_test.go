package schema

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"eino/internal/generic"
)

func TestMessageTemplate(t *testing.T) {
	pyFmtMessage := UserMessage("输入：{question}")
	jinja2Message := UserMessage("输入：{{question}}")
	goTemplateMessage := UserMessage("输入：{{.question}}")
	ctx := context.Background()
	question := "今天天气怎么样"
	expected := []*Message{UserMessage("输入：" + question)}

	ms, err := pyFmtMessage.Format(ctx, map[string]any{"question": question}, FString)
	assert.Nil(t, err)
	assert.True(t, reflect.DeepEqual(expected, ms))
	ms, err = jinja2Message.Format(ctx, map[string]any{"question": question}, Jinja2)
	assert.Nil(t, err)
	assert.True(t, reflect.DeepEqual(expected, ms))
	ms, err = goTemplateMessage.Format(ctx, map[string]any{"question": question}, GoTemplate)
	assert.Nil(t, err)
	assert.True(t, reflect.DeepEqual(expected, ms))

	mp := MessagesPlaceholder("chat_history", false)
	m1 := UserMessage("你好吗？")
	m2 := AssistantMessage("我很好。你呢？", nil)
	ms, err = mp.Format(ctx, map[string]any{"chat_history": []*Message{m1, m2}}, FString)
	assert.Nil(t, err)

	assert.Len(t, ms, 2)
	assert.Equal(t, ms[0], m1)
	assert.Equal(t, ms[1], m2)
}

func TestConcatMessage(t *testing.T) {
	t.Run("验证工具调用字段合并时的追加机制", func(t *testing.T) {
		// 验证消息合并时工具调用字段级别的追加合并机制，
		// 确保多个消息中的相同工具调用能够正确合并为
		// 包含所有非空字段的完整工具调用信息。
		expected := &Message{
			Role:    "assistant",
			Content: "",
			ToolCalls: []ToolCall{
				{
					Index: generic.PtrOf(0),
					ID:    "i_am_a_tool_call_id",
					Type:  "function",
					Function: FunctionCall{
						Name:      "i_am_a_tool_name",
						Arguments: "{}",
					},
				},
			},
		}
		givenMsgList := []*Message{
			{
				Role:    "",
				Content: "",
				ToolCalls: []ToolCall{
					{
						Index: generic.PtrOf(0),
						ID:    "",
						Type:  "",
						Function: FunctionCall{
							Name: "",
						},
					},
				},
			},
			{

				Role:    "assistant",
				Content: "",
				ToolCalls: []ToolCall{
					{
						Index: generic.PtrOf(0),
						ID:    "i_am_a_tool_call_id",
						Type:  "function",
						Function: FunctionCall{
							Name: "i_am_a_tool_name",
						},
					},
				},
			},
			{

				Role:    "",
				Content: "",
				ToolCalls: []ToolCall{
					{
						Index: generic.PtrOf(0),
						ID:    "i_am_a_tool_call_id",
						Type:  "function",
						Function: FunctionCall{
							Name:      "i_am_a_tool_name",
							Arguments: "{}",
						},
					},
				},
			},
		}

		msg, err := ConcatMessages(givenMsgList)
		assert.NoError(t, err)
		assert.EqualValues(t, expected, msg)
	})

	t.Run("验证消息流中存在 nil 消息时的错误检测机制", func(t *testing.T) {
		givenMsgList := []*Message{
			nil,
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []ToolCall{
					{
						Index: generic.PtrOf(0),
						ID:    "i_am_a_too_call_id",
						Type:  "function",
						Function: FunctionCall{
							Name: "i_am_a_tool_name",
						},
					},
				},
			},
		}

		_, err := ConcatMessages(givenMsgList)
		assert.ErrorContains(t, err, "消息流中出现了意外的nil块")
		fmt.Println(err)
	})
}
