package schema

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/internal/generic"
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
	})

	t.Run("验证响应元数据合并时后来者居上的覆盖机制", func(t *testing.T) {
		expectedMsg := &Message{
			Role: "assistant",
			ResponseMeta: &ResponseMeta{
				FinishReason: "stop",
				Usage: &TokenUsage{
					CompletionTokens: 15,
					PromptTokens:     30,
					PromptTokenDetails: PromptTokenDetails{
						CachedTokens: 15,
					},
					TotalTokens: 45,
				},
			},
		}

		givenMsgList := []*Message{
			{
				Role: "assistant",
			},
			{
				Role: "assistant",
				ResponseMeta: &ResponseMeta{
					FinishReason: "",
					Usage: &TokenUsage{
						CompletionTokens: 10,
						PromptTokens:     20,
						PromptTokenDetails: PromptTokenDetails{
							CachedTokens: 10,
						},
						TotalTokens: 30,
					},
				},
			},
			{
				Role: "assistant",
				ResponseMeta: &ResponseMeta{
					FinishReason: "stop",
				},
			},
			{
				Role: "assistant",
				ResponseMeta: &ResponseMeta{
					Usage: &TokenUsage{
						CompletionTokens: 15,
						PromptTokens:     30,
						PromptTokenDetails: PromptTokenDetails{
							CachedTokens: 15,
						},
						TotalTokens: 45,
					},
				},
			},
		}

		msg, err := ConcatMessages(givenMsgList)
		assert.Nil(t, err)
		assert.Equal(t, expectedMsg, msg)

		givenMsgList = append(givenMsgList, &Message{
			Role: "assistant",
			ResponseMeta: &ResponseMeta{
				FinishReason: "tool_calls",
			},
		})
		msg, err = ConcatMessages(givenMsgList)
		assert.NoError(t, err)
		expectedMsg.ResponseMeta.FinishReason = "tool_calls"
		assert.Equal(t, expectedMsg, msg)
	})

	t.Run("验证不同角色消息合并时的角色一致性检查机制", func(t *testing.T) {
		/* 测试验证的核心总结：
		- 验证 ConcatMessages 函数的角色一致性检查机制，
		- 确保系统在合并消息时能够检测并阻止不同角色消息的错误合并，
		- 保护消息流的语义一致性和业务逻辑正确性，
		- 同时提供清晰的错误信息帮助开发者快速定位问题。
		*/

		msgs := []*Message{
			{Role: User},
			{Role: Assistant},
		}

		msg, err := ConcatMessages(msgs)
		if assert.Error(t, err) {
			assert.ErrorContains(t, err, "无法连接不同角色的消息")
			assert.Nil(t, msg)
		}
	})

	t.Run("验证相同角色消息合并时名称一致性检查机制", func(t *testing.T) {
		// 验证 ConcatMessages 函数的消息名称一致性检查机制，
		// 确保系统在合并相同角色消息时能够进一步验证消息名称的一致性，
		// 实现更精确的身份匹配控制，保护消息的完整身份标识和业务逻辑的精确性。
		msgs := []*Message{
			{Role: Assistant, Name: "n", Content: "1"},
			{Role: Assistant, Name: "a", Content: "2"},
		}

		msg, err := ConcatMessages(msgs)
		if assert.Error(t, err) {
			assert.ErrorContains(t, err, "无法连接不同名称的消息")
			assert.Nil(t, msg)
		}
	})

	t.Run("验证工具消息合并时工具调用ID一致性检查机制", func(t *testing.T) {
		msgs := []*Message{
			{
				Role:       "",
				Content:    "",
				ToolCallID: "123",
				ToolCalls: []ToolCall{
					{
						Index: generic.PtrOf(0),
						ID:    "abc",
						Type:  "",
						Function: FunctionCall{
							Name: "",
						},
					},
				},
			},
			{
				Role:       "assistant",
				Content:    "",
				ToolCallID: "321",
				ToolCalls: []ToolCall{
					{
						Index: generic.PtrOf(0),
						ID:    "abc",
						Type:  "function",
						Function: FunctionCall{
							Name: "i_am_a_tool_name",
						},
					},
				},
			},
		}

		msg, err := ConcatMessages(msgs)
		if assert.Error(t, err) {
			assert.ErrorContains(t, err, "无法连接不同工具调用ID的消息")
			assert.Nil(t, msg)
		}
	})

	t.Run("验证响应元数据部分字段为空时的合并填充机制", func(t *testing.T) {
		// 测试验证的核心总结：
		// 验证 ConcatMessages 函数在处理 ResponseMeta 字段时的渐进式合并机制，
		// 确保当某些字段为 nil 或空值时，
		// 系统能够正确地跳过这些字段并使用后续消息中的非空字段进行填充，
		// 最终生成包含所有有效信息的完整响应元数据。
		exp := &Message{
			Role: "assistant",
			ResponseMeta: &ResponseMeta{
				FinishReason: "stop",
				Usage: &TokenUsage{
					CompletionTokens: 15,
					PromptTokens:     30,
					TotalTokens:      45,
				},
			},
		}

		msgs := []*Message{
			{
				Role: "assistant",
				ResponseMeta: &ResponseMeta{
					FinishReason: "",
					Usage:        nil,
				},
			},
			{
				Role: "assistant",
				ResponseMeta: &ResponseMeta{
					FinishReason: "stop",
				},
			},
			{
				Role: "assistant",
				ResponseMeta: &ResponseMeta{
					Usage: &TokenUsage{
						CompletionTokens: 15,
						PromptTokens:     30,
						TotalTokens:      45,
					},
				},
			},
		}

		msg, err := ConcatMessages(msgs)
		assert.NoError(t, err)
		assert.Equal(t, exp, msg)
	})

	t.Run("验证消息合并在高并发场景下的线程安全性和结果一致性", func(t *testing.T) {
		// 测试验证的核心总结：
		// 验证 ConcatMessages 函数在高并发场景下的线程安全性和结果一致性保证，
		// 确保多个 goroutine 同时执行消息合并操作时不会产生竞态条件，
		// 所有并发调用都能返回完全相同且正确的结果，
		// 为函数在生产环境中的多线程使用提供可靠性保证。
		content := "i_am_a_good_concat_message"
		exp := &Message{Role: Assistant, Content: content}
		var msgs []*Message
		for i := 0; i < len(content); i++ {
			msgs = append(msgs, &Message{Role: Assistant, Content: content[i : i+1]})
		}

		wg := sync.WaitGroup{}
		size := 100
		wg.Add(size)
		for i := 0; i < size; i++ {
			go func() {
				defer wg.Done()
				msg, err := ConcatMessages(msgs)
				assert.NoError(t, err)
				assert.Equal(t, exp, msg)
			}()
		}

		wg.Wait()
	})

	t.Run("验证对数概率内容追加合并时的顺序保持机制", func(t *testing.T) {
		// 测试验证的核心总结：
		// 验证 ConcatMessages 函数在处理 LogProbs.Content 字段时的追加合并机制，
		// 确保多个消息的 token 级别对数概率信息能够按照正确的顺序进行追加合并，
		// 最终生成包含完整 token 序列和概率信息的响应元数据，
		// 为模型调试和分析提供准确的数据支持。
		msgs := []*Message{
			{
				Role:    Assistant,
				Content: "🚀",
				ResponseMeta: &ResponseMeta{
					LogProbs: &LogProbs{
						Content: []LogProb{
							{
								Token:   "\\xf0\\x9f\\x9a",
								LogProb: -0.0000073458323,
								Bytes:   []int64{240, 159, 154},
							},
							{
								Token:   "\\x80",
								LogProb: 0,
								Bytes:   []int64{128},
							},
						},
					},
				},
			},
			{
				Role:    "",
				Content: "❤️",
				ResponseMeta: &ResponseMeta{
					LogProbs: &LogProbs{
						Content: []LogProb{
							{
								Token:   "❤️",
								LogProb: -0.0011431955,
								Bytes:   []int64{226, 157, 164, 239, 184, 143},
							},
						},
					},
				},
			},
			{
				Role: "",
				ResponseMeta: &ResponseMeta{
					FinishReason: "stop",
					Usage: &TokenUsage{
						PromptTokens:     7,
						CompletionTokens: 3,
						TotalTokens:      10,
					},
				},
			},
		}

		msg, err := ConcatMessages(msgs)
		assert.NoError(t, err)
		assert.Equal(t, 3, len(msg.ResponseMeta.LogProbs.Content))
		assert.Equal(t, msgs[0].ResponseMeta.LogProbs.Content[0], msg.ResponseMeta.LogProbs.Content[0])
		assert.Equal(t, msgs[0].ResponseMeta.LogProbs.Content[1], msg.ResponseMeta.LogProbs.Content[1])
		assert.Equal(t, msgs[1].ResponseMeta.LogProbs.Content[0], msg.ResponseMeta.LogProbs.Content[2])
	})

	t.Run("验证消息合并时输入参数不变性保护机制", func(t *testing.T) {
		// 测试验证的核心总结：
		// 验证 ConcatMessages 函数的输入参数不变性保护机制，
		// 确保函数在执行合并操作时不会意外修改或污染传入的参数切片，
		// 保护调用方数据的完整性，体现函数设计的纯函数特性和 API 的可靠性保证。
		// 这个测试很可能是对之前某个关于输入参数被意外修改的 bug 的回归测试。
		msgs := []*Message{
			{
				Role:    Assistant,
				Content: "🚀",
				// ResponseMeta: &ResponseMeta{},
			},
			{
				Role:         "",
				Content:      "❤️",
				ResponseMeta: &ResponseMeta{},
			},
			{
				Role: "",
				ResponseMeta: &ResponseMeta{
					FinishReason: "stop",
					Usage: &TokenUsage{
						PromptTokens:     7,
						CompletionTokens: 3,
						TotalTokens:      10,
					},
				},
			},
		}

		msg, err := ConcatMessages(msgs)
		assert.NoError(t, err)
		assert.Equal(t, msgs[2].ResponseMeta, msg.ResponseMeta)
		assert.Nil(t, msgs[0].ResponseMeta)
	})

	t.Run("验证多模态内容按类型智能合并时的内容聚合机制", func(t *testing.T) {
		// 测试验证的核心总结：
		// 验证 ConcatMessages 函数在处理 AssistantGenMultiContent 字段时的类型感知合并机制，
		// 确保系统能够根据内容类型采用不同的合并策略：
		// 文本内容进行拼接合并，
		// 音频数据进行 base64 拼接并保留属性，
		// 图片内容保持独立，
		// 最终生成符合多模态内容特性的智能合并结果。
		base64Audio1 := "dGVzdF9hdWRpb18x"
		base64Audio2 := "dGVzdF9hdWRpb18y"
		imageURL1 := "https://example.com/image1.png"
		imageURL2 := "https://example.com/image2.png"

		msgs := []*Message{
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeText, Text: "Hello, "},
				},
			},
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeText, Text: "world!"},
				},
			},
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeAudioURL, Audio: &MessageOutputAudio{MessagePartCommon: MessagePartCommon{Base64Data: &base64Audio1}}},
				},
			},
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeAudioURL, Audio: &MessageOutputAudio{MessagePartCommon: MessagePartCommon{Base64Data: &base64Audio2, MIMEType: "audio/wav"}}},
				},
			},
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeImageURL, Image: &MessageOutputImage{MessagePartCommon: MessagePartCommon{URL: &imageURL1}}},
				},
			},
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeImageURL, Image: &MessageOutputImage{MessagePartCommon: MessagePartCommon{URL: &imageURL2}}},
				},
			},
		}

		mergedMsg, err := ConcatMessages(msgs)
		assert.NoError(t, err)

		mergedBase64Audio := base64Audio1 + base64Audio2
		expectedContent := []MessageOutputPart{
			{Type: ChatMessagePartTypeText, Text: "Hello, world!"},
			{Type: ChatMessagePartTypeAudioURL, Audio: &MessageOutputAudio{MessagePartCommon: MessagePartCommon{Base64Data: &mergedBase64Audio, MIMEType: "audio/wav"}}},
			{Type: ChatMessagePartTypeImageURL, Image: &MessageOutputImage{MessagePartCommon: MessagePartCommon{URL: &imageURL1}}},
			{Type: ChatMessagePartTypeImageURL, Image: &MessageOutputImage{MessagePartCommon: MessagePartCommon{URL: &imageURL2}}},
		}

		assert.Equal(t, expectedContent, mergedMsg.AssistantGenMultiContent)
	})

	t.Run("验证多模态内容合并时额外信息的字段级合并机制", func(t *testing.T) {
		// 测试验证的核心总结：
		// 验证 ConcatMessages 函数在处理多模态内容时对 Extra 字段的合并机制，
		// 确保系统能够正确地将多个消息中的扩展信息进行字段级合并，
		// 将分散的 Extra map 聚合为一个包含所有键值对的完整扩展信息，
		// 为多模态内容提供丰富的自定义元数据支持。
		base64Audio1 := "dGVzdF9hdWRpb18x"
		base64Audio2 := "dGVzdF9hdWRpb18y"

		msgs := []*Message{
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeAudioURL, Audio: &MessageOutputAudio{MessagePartCommon: MessagePartCommon{Base64Data: &base64Audio1, Extra: map[string]any{"key1": "val1"}}}},
				},
			},
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeAudioURL, Audio: &MessageOutputAudio{MessagePartCommon: MessagePartCommon{Base64Data: &base64Audio2, Extra: map[string]any{"key2": "val2"}}}},
				},
			},
		}

		mergedMsg, err := ConcatMessages(msgs)
		assert.NoError(t, err)

		mergedBase64Audio := base64Audio1 + base64Audio2
		expectedContent := []MessageOutputPart{
			{Type: ChatMessagePartTypeAudioURL, Audio: &MessageOutputAudio{MessagePartCommon: MessagePartCommon{Base64Data: &mergedBase64Audio, Extra: map[string]any{"key1": "val1", "key2": "val2"}}}},
		}

		assert.Equal(t, expectedContent, mergedMsg.AssistantGenMultiContent)
	})

	t.Run("验证部分多模态内容有额外信息时的选择性合并机制", func(t *testing.T) {
		// 测试验证的核心总结：
		//  验证 ConcatMessages 函数在处理多模态内容时对部分空 Extra 字段的选择性保留机制，
		//  确保当只有部分内容包含扩展信息时，系统能够智能地选择并保留有效的 Extra 数据，
		//  避免有价值的扩展信息因为其他内容缺少相应字段而丢失，体现了合并策略的灵活性和健壮性。
		base64Audio1 := "dGVzdF9hdWRpb18x"
		base64Audio2 := "dGVzdF9hdWRpb18y"

		msgs := []*Message{
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeAudioURL, Audio: &MessageOutputAudio{MessagePartCommon: MessagePartCommon{Base64Data: &base64Audio1, Extra: map[string]any{"key1": "val1"}}}},
				},
			},
			{
				Role: Assistant,
				AssistantGenMultiContent: []MessageOutputPart{
					{Type: ChatMessagePartTypeAudioURL, Audio: &MessageOutputAudio{MessagePartCommon: MessagePartCommon{Base64Data: &base64Audio2}}},
				},
			},
		}

		mergedMsg, err := ConcatMessages(msgs)
		assert.NoError(t, err)

		mergedBase64Audio := base64Audio1 + base64Audio2
		expectedContent := []MessageOutputPart{
			{Type: ChatMessagePartTypeAudioURL, Audio: &MessageOutputAudio{MessagePartCommon: MessagePartCommon{Base64Data: &mergedBase64Audio, Extra: map[string]any{"key1": "val1"}}}},
		}

		assert.Equal(t, expectedContent, mergedMsg.AssistantGenMultiContent)
	})
}

func TestConcatToolCalls(t *testing.T) {
	t.Run("验证工具调用字段合并时非空字段的原子性保留机制", func(t *testing.T) {
		// 测试验证的核心总结：
		//  验证 concatToolCalls 函数的字段原子性合并机制，确保在处理工具调用信息时，
		//  已经设置的非空字段具有原子性保护，不会被后续分块中的空值覆盖，
		//  同时支持通过多个分块逐步构建完整的工具调用信息，
		//  体现了流式场景下工具调用数据的完整性保证和原子性字段保护策略。
		givenToolCalls := []ToolCall{
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "function",
				Function: FunctionCall{
					Name: "tool_name",
				},
			},
			{
				Index: generic.PtrOf(0),
				Function: FunctionCall{
					Arguments: "call me please",
				},
			},
		}

		expectedToolCall := ToolCall{
			Index: generic.PtrOf(0),
			ID:    "tool_call_id",
			Type:  "function",
			Function: FunctionCall{
				Name:      "tool_name",
				Arguments: "call me please",
			},
		}

		tc, err := concatToolCalls(givenToolCalls)
		assert.NoError(t, err)
		assert.Len(t, tc, 1)
		assert.EqualValues(t, expectedToolCall, tc[0])
	})

	t.Run("验证所有分块包含相同非空字段时的一致性保证机制", func(t *testing.T) {
		// 测试验证的核心总结：
		//  验证 concatToolCalls 函数在处理包含重复非空字段的多个分块时的一致性保证机制，
		//  确保当不同分块包含相同的原子字段信息时，系统能够正确处理冗余数据并保持字段值的一致性，
		//  同时仍然支持新字段（如 Arguments）的增量添加，体现了流式数据处理中对冗余信息的容错能力和数据一致性保证。
		givenToolCalls := []ToolCall{
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "function",
				Function: FunctionCall{
					Name: "tool_name",
				},
			},
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "function",
				Function: FunctionCall{
					Name:      "tool_name",
					Arguments: "call me please",
				},
			},
		}

		expectedToolCall := ToolCall{
			Index: generic.PtrOf(0),
			ID:    "tool_call_id",
			Type:  "function",
			Function: FunctionCall{
				Name:      "tool_name",
				Arguments: "call me please",
			},
		}

		tc, err := concatToolCalls(givenToolCalls)
		assert.NoError(t, err)
		assert.Len(t, tc, 1)
		assert.EqualValues(t, expectedToolCall, tc[0])
	})

	t.Run("验证非连续分块中原子字段的跨分片聚合机制", func(t *testing.T) {
		// 测试验证的核心总结：
		//  验证 concatToolCalls 函数在处理分散在多个非连续分块中的原子字段时的跨分片聚合机制，
		//  确保系统能够从复杂的数据流中收集和聚合分散的有效信息，即使相同字段在不同分片中非连续出现也能保持一致性，
		//  并最终构建出包含所有有效字段信息的完整工具调用，
		//  体现了流式数据处理中对复杂信息分布模式的强大处理能力。
		givenToolCalls := []ToolCall{
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "",
				Function: FunctionCall{
					Name: "",
				},
			},
			{
				Index: generic.PtrOf(0),
				ID:    "",
				Type:  "function",
				Function: FunctionCall{
					Name:      "",
					Arguments: "call me please",
				},
			},
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "",
				Function: FunctionCall{
					Name:      "",
					Arguments: "",
				},
			},
		}

		expectedToolCall := ToolCall{
			Index: generic.PtrOf(0),
			ID:    "tool_call_id",
			Type:  "function",
			Function: FunctionCall{
				Name:      "",
				Arguments: "call me please",
			},
		}

		tc, err := concatToolCalls(givenToolCalls)
		assert.NoError(t, err)
		assert.Len(t, tc, 1)
		assert.EqualValues(t, expectedToolCall, tc[0])
	})

	t.Run("验证工具调用ID冲突时的错误检测和处理机制", func(t *testing.T) {
		// 测试验证的核心总结：
		//  验证 concatToolCalls 函数的工具调用ID一致性检查机制，
		//  确保当相同Index的ToolCall具有不同ID时，系统能够检测到身份冲突并阻止合并操作，
		//  保护工具调用的身份唯一性和数据完整性，防止不同工具调用的信息被错误混合，
		//  体现系统对工具调用身份严格验证的可靠性保证。
		givenToolCalls := []ToolCall{
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "function",
				Function: FunctionCall{
					Name: "tool_name",
				},
			},
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id_1",
				Type:  "function",
				Function: FunctionCall{
					Name:      "tool_name",
					Arguments: "call me please",
				},
			},
		}

		_, err := concatToolCalls(givenToolCalls)
		assert.ErrorContains(t, err, "无法连接不同工具调用ID的工具调用")
	})

	t.Run("验证工具调用类型冲突时的错误检测和处理机制", func(t *testing.T) {
		// 测试验证的核心总结：
		//  验证 concatToolCalls 函数的工具调用类型一致性检查机制，
		//  确保当相同ID的ToolCall具有不同Type时，系统能够检测到类型冲突并阻止合并操作，
		//  保护工具调用的语义一致性和类型安全性，防止不同类型工具调用的信息被错误混合，
		//  体现系统对工具调用语义严格验证的可靠性保证。
		givenToolCalls := []ToolCall{
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "function",
				Function: FunctionCall{
					Name: "tool_name",
				},
			},
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "function_1",
				Function: FunctionCall{
					Name:      "tool_name",
					Arguments: "call me please",
				},
			},
		}

		_, err := concatToolCalls(givenToolCalls)
		assert.ErrorContains(t, err, "无法连接不同工具类型的工具调用")
	})

	t.Run("验证工具函数名称冲突时的错误检测和处理机制", func(t *testing.T) {
		// 测试验证的核心总结：
		//  验证 concatToolCalls 函数的工具函数名称一致性检查机制，
		//  确保当相同ID和Type的ToolCall具有不同Function.Name时，系统能够检测到函数名称冲突并阻止合并操作，
		//  保护工具调用的执行精确性和语义一致性，防止不同函数的调用信息被错误混合，
		//  体现系统对工具调用函数级别严格验证的可靠性保证。
		givenToolCalls := []ToolCall{
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "function",
				Function: FunctionCall{
					Name: "tool_name",
				},
			},
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "function",
				Function: FunctionCall{
					Name:      "tool_name_1",
					Arguments: "call me please",
				},
			},
		}

		_, err := concatToolCalls(givenToolCalls)
		assert.ErrorContains(t, err, "无法连接不同工具名称的工具调用")
	})

	t.Run("验证多个工具调用并行处理时的分组聚合和排序机制", func(t *testing.T) {
		// 测试验证的核心总结：
		//  验证 concatToolCalls 函数在处理多个工具调用时的分组聚合和排序机制，
		//  确保系统能够正确地将相同Index的ToolCall进行分组聚合，保持不同Index组之间的独立性，
		//  对nil Index的ToolCall保持独立不合并，并最终按照Index=nil优先、数值Index递增的规则进行排序输出，
		//  体现了系统对复杂多工具调用场景的完整处理能力和结果一致性保证。
		givenToolCalls := []ToolCall{
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "",
				Function: FunctionCall{
					Name: "",
				},
			},
			{
				Index: generic.PtrOf(0),
				ID:    "",
				Type:  "function",
				Function: FunctionCall{
					Name:      "",
					Arguments: "call me please",
				},
			},
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "",
				Function: FunctionCall{
					Name:      "",
					Arguments: "",
				},
			},
			{
				Index: generic.PtrOf(1),
				ID:    "tool_call_id",
				Type:  "",
				Function: FunctionCall{
					Name: "",
				},
			},
			{
				Index: generic.PtrOf(1),
				ID:    "",
				Type:  "function",
				Function: FunctionCall{
					Name:      "",
					Arguments: "call me please",
				},
			},
			{
				Index: generic.PtrOf(1),
				ID:    "tool_call_id",
				Type:  "",
				Function: FunctionCall{
					Name:      "",
					Arguments: "",
				},
			},
			{
				Index: nil,
				ID:    "22",
				Type:  "",
				Function: FunctionCall{
					Name: "",
				},
			},
			{
				Index: nil,
				ID:    "44",
				Type:  "",
				Function: FunctionCall{
					Name: "",
				},
			},
		}

		expectedToolCall := []ToolCall{
			{
				Index: nil,
				ID:    "22",
				Type:  "",
				Function: FunctionCall{
					Name: "",
				},
			},
			{
				Index: nil,
				ID:    "44",
				Type:  "",
				Function: FunctionCall{
					Name: "",
				},
			},
			{
				Index: generic.PtrOf(0),
				ID:    "tool_call_id",
				Type:  "function",
				Function: FunctionCall{
					Name:      "",
					Arguments: "call me please",
				},
			},
			{
				Index: generic.PtrOf(1),
				ID:    "tool_call_id",
				Type:  "function",
				Function: FunctionCall{
					Name:      "",
					Arguments: "call me please",
				},
			},
		}

		tc, err := concatToolCalls(givenToolCalls)
		assert.NoError(t, err)
		assert.EqualValues(t, expectedToolCall, tc)
	})
}
