package schema

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestStructForParse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	XX   struct {
		YY int `json:"yy"`
	} `json:"xx"`
}

func TestMessageJSONParser(t *testing.T) {
	ctx := context.Background()

	t.Run("从消息内容中解析", func(t *testing.T) {
		parser := NewMessageJSONParser[TestStructForParse](&MessageJSONParseConfig{
			ParseFrom: MessageParseFromContent,
		})

		parsed, err := parser.Parse(ctx, &Message{
			Content: `{"id": 1, "name": "test", "xx": {"yy": 2}}`,
		})
		assert.Nil(t, err)
		assert.Equal(t, 1, parsed.ID)
	})

	t.Run("从工具调用中解析", func(t *testing.T) {
		t.Run("只有一个工具调用，默认使用第一个工具调用", func(t *testing.T) {
			parser := NewMessageJSONParser[TestStructForParse](&MessageJSONParseConfig{
				ParseFrom: MessageParseFromToolCall,
			})

			parsed, err := parser.Parse(ctx, &Message{
				ToolCalls: []ToolCall{
					{Function: FunctionCall{Arguments: `{"id": 1, "name": "test", "xx": {"yy": 2}}`}},
				},
			})
			assert.Nil(t, err)
			assert.Equal(t, 1, parsed.ID)
		})

		t.Run("验证JSON路径解析时单层字段的提取能力", func(t *testing.T) {
			// 测试场景：从 "xx" 路径提取 {"yy": 2}
			// 验证目标：单层路径字段的正确提取
			// 核心机制：基础路径解析能力

			type TestStructForParse2 struct {
				YY int `json:"yy"`
			}

			parser := NewMessageJSONParser[TestStructForParse2](&MessageJSONParseConfig{
				ParseFrom:    MessageParseFromToolCall,
				ParseKeyPath: "xx",
			})

			parsed, err := parser.Parse(ctx, &Message{
				ToolCalls: []ToolCall{
					{Function: FunctionCall{Arguments: `{"id": 1, "name": "test", "xx": {"yy": 2}}`}},
				},
			})
			assert.Nil(t, err)
			assert.Equal(t, 2, parsed.YY)
		})

		t.Run("验证嵌套JSON路径解析时深层字段的提取能力", func(t *testing.T) {
			// 测试场景：从 "xx.yy" 路径提取 {"zz": 3}
			// 验证目标：多层嵌套路径的递归解析
			// 核心机制：深层路径的递归提取能力

			type TestStructForParse3 struct {
				ZZ int `json:"zz"`
			}

			parser := NewMessageJSONParser[TestStructForParse3](&MessageJSONParseConfig{
				ParseFrom:    MessageParseFromToolCall,
				ParseKeyPath: "xx.yy",
			})

			parsed, err := parser.Parse(ctx, &Message{
				ToolCalls: []ToolCall{
					{Function: FunctionCall{Arguments: `{"id": 1, "name": "test", "xx": {"yy": {"zz": 3}}}`}},
				},
			})
			assert.Nil(t, err)
			assert.Equal(t, 3, parsed.ZZ)
		})

		t.Run("验证指针类型字段解析时的类型转换和内存分配机制", func(t *testing.T) {
			// 测试场景：解析指向指针类型的结构体
			// 验证目标：指针类型的正确解析和内存安全
			// 核心机制：复杂泛型类型的类型安全处理

			type TestStructForParse4 struct {
				ZZ *int `json:"zz"`
			}

			parser := NewMessageJSONParser[**TestStructForParse4](&MessageJSONParseConfig{
				ParseFrom: MessageParseFromToolCall,
			})

			parsed, err := parser.Parse(ctx, &Message{
				ToolCalls: []ToolCall{{Function: FunctionCall{Arguments: `{"zz": 3}`}}},
			})
			assert.Nil(t, err)
			assert.Equal(t, 3, *((**parsed).ZZ))
		})
	})

	t.Run("验证JSON数组解析时切片类型转换和数据完整性保证", func(t *testing.T) {
		t.Run("验证单个工具调用中有效JSON数组解析时的切片转换能力", func(t *testing.T) {
			// 测试场景：单个ToolCall包含完整数组 `[{"id": 1}, {"id": 2}]`
			// 验证目标：JSON数组正确转换为Go切片
			// 核心机制：数组类型转换和元素解析

			parser := NewMessageJSONParser[[]map[string]any](&MessageJSONParseConfig{
				ParseFrom: MessageParseFromToolCall,
			})

			parsed, err := parser.Parse(ctx, &Message{
				ToolCalls: []ToolCall{{Function: FunctionCall{Arguments: `[{"id": 1}, {"id": 2}]`}}},
			})
			assert.Nil(t, err)
			assert.Equal(t, 2, len(parsed))
		})

		t.Run("验证多工具调用场景下数组解析错误时的异常处理机制", func(t *testing.T) {
			// 测试场景：多个ToolCall分别包含对象，不是数组
			// 验证目标：错误情况下的异常处理
			// 核心机制：类型不匹配的错误检测和报告

			parser := NewMessageJSONParser[[]map[string]any](&MessageJSONParseConfig{
				ParseFrom: MessageParseFromToolCall,
			})

			_, err := parser.Parse(ctx, &Message{
				ToolCalls: []ToolCall{
					{Function: FunctionCall{Arguments: `{"id": 1}`}},
					{Function: FunctionCall{Arguments: `{"id": 2}`}},
				},
			})
			assert.NotNil(t, err)
		})
	})

	t.Run("验证解析器配置为空时的参数验证和初始化容错机制", func(t *testing.T) {
		parser := NewMessageJSONParser[TestStructForParse](nil)
		_, err := parser.Parse(ctx, &Message{
			Content: "",
		})
		assert.NotNil(t, err)
	})

	t.Run("invalid parse key path", func(t *testing.T) {
		parser := NewMessageJSONParser[TestStructForParse](&MessageJSONParseConfig{
			ParseKeyPath: "...invalid",
		})
		_, err := parser.Parse(ctx, &Message{})
		assert.NotNil(t, err)
	})

	t.Run("invalid parse from", func(t *testing.T) {
		parser := NewMessageJSONParser[TestStructForParse](&MessageJSONParseConfig{
			ParseFrom: "invalid",
		})
		_, err := parser.Parse(ctx, &Message{})
		assert.NotNil(t, err)
	})

	t.Run("invalid parse from type", func(t *testing.T) {
		parser := NewMessageJSONParser[int](&MessageJSONParseConfig{
			ParseFrom: MessageParseFrom("invalid"),
		})
		_, err := parser.Parse(ctx, &Message{})
		assert.NotNil(t, err)
	})
}
