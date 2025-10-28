package schema

import (
	"context"
	"fmt"
	"strings"

	"github.com/bytedance/sonic"
)

// MessageParser - 消息解析器接口，将消息解析为指定类型对象。
type MessageParser[T any] interface {
	Parse(ctx context.Context, m *Message) (T, error)
}

// MessageParseFrom - 消息解析数据源类型枚举，默认从消息内容解析。
type MessageParseFrom string

const (
	MessageParseFromContent  MessageParseFrom = "content"   // 从消息内容解析
	MessageParseFromToolCall MessageParseFrom = "tool_call" // 从工具调用参数解析
)

// MessageJSONParseConfig - JSON消息解析配置，指定解析来源和字段路径。
type MessageJSONParseConfig struct {
	// ParseFrom - 解析数据来源，默认从消息内容解析。
	ParseFrom MessageParseFrom `json:"parse_from,omitempty"`

	// ParseKeyPath - JSON字段路径，支持嵌套字段提取，如 "field.sub_field"。
	ParseKeyPath string `json:"parse_key_path,omitempty"`
}

// NewMessageJSONParser 创建一个新的 MessageJSONParser。
func NewMessageJSONParser[T any](config *MessageJSONParseConfig) MessageParser[T] {
	if config == nil {
		config = &MessageJSONParseConfig{}
	}

	if config.ParseFrom == "" {
		config.ParseFrom = MessageParseFromContent
	}

	return &MessageJSONParser[T]{
		ParseFrom:    config.ParseFrom,
		ParseKeyPath: config.ParseKeyPath,
	}
}

// MessageJSONParser - JSON消息解析器，使用JSON反序列化将消息解析为指定类型对象。
type MessageJSONParser[T any] struct {
	ParseFrom    MessageParseFrom // 解析数据来源
	ParseKeyPath string           // JSON字段路径
}

// Parse - 根据配置将消息解析为指定类型对象，支持从内容或工具调用解析。
func (p *MessageJSONParser[T]) Parse(ctx context.Context, m *Message) (parsed T, err error) {
	// 根据解析来源选择不同的解析策略
	if p.ParseFrom == MessageParseFromContent {
		// 从消息内容解析数据
		return p.parse(m.Content)
	} else if p.ParseFrom == MessageParseFromToolCall {
		// 检查消息中是否包含工具调用
		if len(m.ToolCalls) == 0 {
			return parsed, fmt.Errorf("消息中未找到工具调用信息")
		}

		// 从第一个工具调用的参数中解析数据
		return p.parse(m.ToolCalls[0].Function.Arguments)
	}

	// 错误处理：不支持的解析来源类型
	return parsed, fmt.Errorf("无效的解析来源类型: %s", p.ParseFrom)
}

// extractData - 根据配置的JSON路径从数据中提取目标字段，支持嵌套字段和数组元素。
func (p *MessageJSONParser[T]) extractData(data string) (string, error) {
	// 情况1：未配置路径，直接返回原始数据
	if p.ParseKeyPath == "" {
		return data, nil
	}

	// 第一步：将路径字符串分割为键数组
	keys := strings.Split(p.ParseKeyPath, ".")

	// 第二步：转换为 sonic 库所需的 interface{} 类型
	interfaceKeys := make([]interface{}, len(keys))
	for i, key := range keys {
		interfaceKeys[i] = key
	}

	// 第三步：使用 sonic 库根据路径获取JSON节点
	node, err := sonic.GetFromString(data, interfaceKeys...)
	if err != nil {
		return "", fmt.Errorf("JSON路径提取失败: %w", err)
	}

	// 第四步：将提取的节点重新序列化为JSON字符串
	bytes, err := node.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("JSON节点序列化失败: %w", err)
	}

	return string(bytes), nil
}

// parse - 解析字符串数据为指定类型对象，支持JSON路径提取和反序列化。
func (p *MessageJSONParser[T]) parse(data string) (parsed T, err error) {
	// 第一步：根据路径提取目标数据
	parsedData, err := p.extractData(data)
	if err != nil {
		return parsed, err
	}

	// 第二步：使用 sonic 库进行 JSON 反序列化
	if err := sonic.UnmarshalString(parsedData, &parsed); err != nil {
		return parsed, fmt.Errorf("JSON反序列化失败: %w", err)
	}

	return parsed, nil
}
