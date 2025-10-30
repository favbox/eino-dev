package schema

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"text/template"

	"github.com/nikolalohinski/gonja"
	"github.com/nikolalohinski/gonja/config"
	"github.com/nikolalohinski/gonja/nodes"
	"github.com/nikolalohinski/gonja/parser"
	"github.com/slongfield/pyfmt"

	"github.com/favbox/eino/internal"
	"github.com/favbox/eino/internal/generic"
)

// === Layer 1: 基础定义层 (被依赖的基础) ===
// 1.1 核心枚举和常量

// RoleType 表示消息角色类型。
type RoleType string

const (
	Assistant RoleType = "assistant" // 助手角色，ChatModel 返回的消息
	User      RoleType = "user"      // 用户角色，用户发送的消息
	System    RoleType = "system"    // 系统角色，系统消息
	Tool      RoleType = "tool"      // 工具角色，工具调用输出
)

// FormatType 消息模板格式类型
type FormatType uint8

const (
	// FString Python pyfmt 格式，支持 PEP-3101 格式化
	FString FormatType = 0
	// Jinja2 Python Jinja2 模板引擎格式
	Jinja2 FormatType = 2
	// GoTemplate Go 标准库 text/template 格式
	GoTemplate FormatType = 1
)

// ImageURLDetail 表示图片 URL 的质量级别。
type ImageURLDetail string

const (
	ImageURLDetailHigh ImageURLDetail = "high" // 高质量
	ImageURLDetailLow  ImageURLDetail = "low"  // 低质量
	ImageURLDetailAuto ImageURLDetail = "auto" // 自动质量
)

// ChatMessagePartType 表示聊天消息部分的类型，用于多模态消息处理。
type ChatMessagePartType string

const (
	ChatMessagePartTypeText     ChatMessagePartType = "text"      // 文本类型
	ChatMessagePartTypeImageURL ChatMessagePartType = "image_url" // 图片URL类型
	ChatMessagePartTypeAudioURL ChatMessagePartType = "audio_url" // 音频URL类型
	ChatMessagePartTypeVideoURL ChatMessagePartType = "video_url" // 视频URL类型
	ChatMessagePartTypeFileURL  ChatMessagePartType = "file_url"  // 文件URL类型
)

// 1.2 核心接口定义

// MessagesTemplate 消息模板接口，将模板渲染为消息列表
//
// 示例: 使用占位符引用历史消息
//
//	chatTemplate := prompt.FromMessages(
//	    Schema.SystemMessage("你是一个AI助手"),
//	    Schema.MessagesPlaceholder("history", false), // 使用参数中的"history"值
//	)
//	msgs, err := chatTemplate.Format(ctx, params)
type MessagesTemplate interface {
	Format(ctx context.Context, vs map[string]any, formatType FormatType) ([]*Message, error)
}

// === Layer 2: 核心类型定义层 ===

// 2.1 核心消息结构体 (用户最关心的类型)

// Message 表示聊天消息。
type Message struct {
	Role RoleType `json:"role"`

	// Content 用于用户文本输入和模型文本输出。
	Content string `json:"content"`

	// UserInputMultiContent 传递用户提供的多模态内容给模型。
	UserInputMultiContent []MessageInputPart `json:"user_input_multi_content,omitempty"`

	// AssistantGenMultiContent 用于接收模型的多模态输出。
	AssistantGenMultiContent []MessageOutputPart `json:"assistant_output_multi_content,omitempty"`

	Name string `json:"name,omitempty"`

	// 仅用于 AssistantMessage
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// 仅用于 ToolMessage
	ToolCallID string `json:"tool_call_id,omitempty"`
	// 仅用于 ToolMessage
	ToolName string `json:"tool_name,omitempty"`

	ResponseMeta *ResponseMeta `json:"response_meta,omitempty"`

	// ReasoningContent 模型的思考过程。
	// 模型返回推理内容：包含该字段；其他情况：省略该字段
	ReasoningContent string `json:"reasoning_content,omitempty"`

	// Extra 模型实现的定制信息
	Extra map[string]any `json:"extra,omitempty"`
}

// 2.2 工具调用相关结构体

// ToolCall 表示消息中的工具调用。 用于助手消息中需要执行工具调用的场景。
type ToolCall struct {
	// Index 多个工具调用时的标识符
	// 流式模式下用于标识和合并工具调用块
	Index *int `json:"index,omitempty"`

	// ID 工具调用的唯一标识
	ID string `json:"id"`

	// Type 工具调用类型，默认为"function"
	Type string `json:"type"`

	// Function 要执行的函数调用
	Function FunctionCall `json:"function"`

	// Extra 工具调用的额外信息
	Extra map[string]any `json:"extra,omitempty"`
}

// FunctionCall 表示消息中的函数调用。用于助手消息中。
type FunctionCall struct {
	//  Name 要调用的函数名称
	Name string `json:"name,omitempty"`

	// Arguments 调用函数的参数，JSON 格式
	Arguments string `json:"arguments,omitempty"`
}

// 2.3 响应元数据结构体

// ResponseMeta 收集聊天响应的元信息。
type ResponseMeta struct {
	// FinishReason 聊天响应结束的原因
	// 通常为"stop"、"length"、"tool_calls"、"content_filter"、"null"，由聊天模型实现定义
	FinishReason string `json:"finish_reason,omitempty"`

	// Usage 聊天响应的 Token 使用量，取决于聊天模型实现是否返回
	Usage *TokenUsage `json:"usage,omitempty"`

	// LogProbs 对数概率信息
	LogProbs *LogProbs `json:"log_probs,omitempty"`
}

// 2.4 Token使用相关结构体

// TokenUsage 表示聊天模型请求的 Token 使用量。
type TokenUsage struct {
	// PromptTokens 提示词 Token 数量，包含该请求的所有输入 Token
	PromptTokens int `json:"prompt_tokens"`

	// PromptTokenDetails 提示词 Token 的详细分解
	PromptTokenDetails PromptTokenDetails `json:"prompt_token_details"`

	// Completion 完成 Token 数量
	CompletionTokens int `json:"completion_tokens"`

	// TotalTokens Token 总数量
	TotalTokens int `json:"total_tokens"`
}

// PromptTokenDetails 表示提示词Token的详细信息。
type PromptTokenDetails struct {
	// CachedTokens 提示词中的缓存Token数量
	CachedTokens int `json:"cached_tokens"`
}

// 2.5 辅助配置结构体

// toolMessageOptions - 工具消息的可选配置项。
type toolMessageOptions struct {
	toolName string // 工具名称
}

// 2.6 函数选项类型定义

// ToolMessageOption - 工具消息配置选项函数类型。
type ToolMessageOption func(*toolMessageOptions)

// 2.7 消息部分结构体

// MessagePartCommon 表示多模态类型的输入/输出的通用消息组件。
type MessagePartCommon struct {
	// URL 传统 URL 或RFC-2379格式的特殊 URL
	URL *string `json:"url,omitempty"`

	// Base64Data Base64编码的二进制数据
	Base64Data *string `json:"base64_data,omitempty"`

	// MIMEType MIME 类型，如"image/png"、"audio/wav"等
	MIMEType string `json:"mime_type,omitempty"`

	// Extra 存储额外信息
	Extra map[string]any `json:"extra,omitempty"`
}

// MessageInputImage 表示消息中的图片输入部分。
// URL 和 Base64Data 选择其一使用。
type MessageInputImage struct {
	MessagePartCommon

	// Detail 图片质量级别
	Detail ImageURLDetail `json:"detail,omitempty"`
}

// MessageInputAudio 表示消息中的音频输入部分。
// URL 和 Base64Data 选择其一使用。
type MessageInputAudio struct {
	MessagePartCommon
}

// MessageInputVideo 表示消息中的视频输入部分。
// URL 和 Base64Data 选择其一使用。
type MessageInputVideo struct {
	MessagePartCommon
}

// MessageInputFile 表示消息中的文件输入部分。
// URL 和 Base64Data 选择其一使用。
type MessageInputFile struct {
	MessagePartCommon
}

// MessageInputPart 表示消息的输入部分。
type MessageInputPart struct {
	Type ChatMessagePartType `json:"type"`

	Text string `json:"text,omitempty"`

	// Image 图片输入，Type 为 "image_url" 时使用
	Image *MessageInputImage `json:"image,omitempty"`

	// Audio 音频输入，Type 为 "audio_url" 时使用
	Audio *MessageInputAudio `json:"audio,omitempty"`

	// Video 视频输入，Type 为 "video_url" 时使用
	Video *MessageInputVideo `json:"video,omitempty"`

	// File 视频输入，Type 为 "file_url" 时使用
	File *MessageInputFile `json:"file,omitempty"`
}

// MessageOutputImage 表示消息中的图片输出部分。
type MessageOutputImage struct {
	MessagePartCommon
}

// MessageOutputAudio 表示消息中的音频输出部分。
type MessageOutputAudio struct {
	MessagePartCommon
}

// MessageOutputVideo 表示消息中的视频输出部分。
type MessageOutputVideo struct {
	MessagePartCommon
}

// MessageOutputPart 表示助手生成消息的部分。
// 可包含文本或多媒体内容（图片、音频、视频）。
type MessageOutputPart struct {
	// Type 部分类型，如"text"、"image_url"、"audio_url"、"video_url"
	Type ChatMessagePartType `json:"type"`

	// Text 部分文本，Type为"text"时使用
	Text string `json:"text,omitempty"`

	// Image 图片输出，Type为ChatMessagePartTypeImageURL时使用
	Image *MessageOutputImage `json:"image,omitempty"`

	// Audio 音频输出，Type为ChatMessagePartTypeAudioURL时使用
	Audio *MessageOutputAudio `json:"audio,omitempty"`

	// Video 视频输出，Type为ChatMessagePartTypeVideoURL时使用
	Video *MessageOutputVideo `json:"video,omitempty"`
}

// 2.7 日志相关结构体

// LogProbs 包含对数概率信息的顶级结构。
type LogProbs struct {
	// Content 带有对数概率信息的消息内容 Token 列表
	Content []LogProb `json:"content"`
}

// LogProb 表示 Token 的概率信息。
type LogProb struct {
	// Token 文本，表示语言模型分词过程中的连续字符序列
	Token string `json:"token"`

	// LogProb Token 的对数概率
	// 前 20个最可能 Token：显示实际概率值；其他 Token：-9999.0
	LogProb float64 `json:"logprob"`

	// Bytes Token 的 UTF-8字节表示，整数列表
	// 多 Token 表示一字符：需要合并字节；无字节表示：null 省略
	Bytes []int64 `json:"bytes,omitempty"`

	// TopLogProbs 该 Token 位置最可能的 Token 列表
	// 正常情况：返回请求的数量；少数情况：返回数量少于请求
	TopLogProbs []TopLogProb `json:"top_logprobs"`
}

// TopLogProb 表示最可能Token 的概率信息。
type TopLogProb struct {
	// Token 文本，表示语言模型分词过程中的连续字符序列
	Token string `json:"token"`

	// LogProb Token 的对数概率
	// 前 20个最可能 Token：显示实际概率值；其他 Token：-9999.0
	LogProb float64 `json:"logprob"`

	// Bytes Token 的 UTF-8字节表示，整数列表
	// 多 Token 表示一字符：需要合并字节；无字节表示：null 省略
	Bytes []int64 `json:"bytes,omitempty"`
}

// 2.8 模板相关结构体

type messagesPlaceholder struct {
	key      string
	optional bool
}

// === Layer 3: 公开API层 (用户最常用的功能) ===

// 3.1 消息构造函数

// SystemMessage 创建系统角色的消息
func SystemMessage(content string) *Message {
	return &Message{
		Role:    System,
		Content: content,
	}
}

// AssistantMessage 创建助手角色的消息，支持工具调用
func AssistantMessage(content string, toolCalls []ToolCall) *Message {
	return &Message{
		Role:      Assistant,
		Content:   content,
		ToolCalls: toolCalls,
	}
}

// UserMessage 创建用户角色的消息
func UserMessage(content string) *Message {
	return &Message{
		Role:    User,
		Content: content,
	}
}

// ToolMessage - 创建工具角色消息，支持可选配置。
func ToolMessage(content string, toolCallID string, opts ...ToolMessageOption) *Message {
	o := &toolMessageOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return &Message{
		Role:       Tool,
		Content:    content,
		ToolCallID: toolCallID,
		ToolName:   o.toolName,
	}
}

// WithToolName - 设置工具消息的工具名称选项。
func WithToolName(name string) ToolMessageOption {
	return func(o *toolMessageOptions) {
		o.toolName = name
	}
}

// 3.2 消息合并函数

// ConcatMessages 合并相同角色和名称的消息。
// 合并相同索引的工具调用，不同角色或名称的消息会返回错误。
// 用于合并流式消息。
//
// 示例:
//
//	msgs := []*Message{}
//	for {
//		msg, err := stream.Recv()
//		if errors.Is(err, io.EOF) {
//			break
//	    }
//	    if err != nil {...}
//	    msgs = append(msgs, msg)
//	}
//
// merged, err := ConcatMessages(msgs) // merged.Content 将包含所有消息的完整内容
func ConcatMessages(msgs []*Message) (*Message, error) {
	var (
		contents                      []string
		contentLen                    int
		reasoningContents             []string
		reasoningContentLen           int
		toolCalls                     []ToolCall
		assistantGenMultiContentParts []MessageOutputPart
		ret                           = Message{}
		extraList                     = make([]map[string]any, 0, len(msgs))
	)

	for idx, msg := range msgs {
		if msg == nil {
			return nil, fmt.Errorf("消息流中出现了意外的nil块，索引：%d", idx)
		}

		// 角色校验
		if msg.Role != "" {
			if ret.Role == "" {
				ret.Role = msg.Role
			} else if ret.Role != msg.Role {
				return nil, fmt.Errorf("无法连接不同角色的消息：%s %s", ret.Role, msg.Role)
			}
		}

		// 名称校验
		if msg.Name != "" {
			if ret.Name == "" {
				ret.Name = msg.Name
			} else if ret.Name != msg.Name {
				return nil, fmt.Errorf("无法连接不同名称的消息：%s %s", ret.Name, msg.Name)
			}
		}

		// 工具调用ID校验
		if msg.ToolCallID != "" {
			if ret.ToolCallID == "" {
				ret.ToolCallID = msg.ToolCallID
			} else if ret.ToolCallID != msg.ToolCallID {
				return nil, fmt.Errorf("无法连接不同工具调用ID的消息：%s %s", ret.ToolCallID, msg.ToolCallID)
			}
		}
		// 工具调用名称校验
		if msg.ToolName != "" {
			if ret.ToolName == "" {
				ret.ToolName = msg.ToolName
			} else if ret.ToolName != msg.ToolName {
				return nil, fmt.Errorf("无法连接不同工具调用名称的消息：%s %s", ret.ToolName, msg.ToolName)
			}
		}

		// 收集内容
		if msg.Content != "" {
			contents = append(contents, msg.Content)
			contentLen += len(msg.Content)
		}

		// 收集推理内容
		if msg.ReasoningContent != "" {
			reasoningContents = append(reasoningContents, msg.ReasoningContent)
			reasoningContentLen += len(msg.ReasoningContent)
		}

		// 收集工具调用
		if len(msg.ToolCalls) > 0 {
			toolCalls = append(toolCalls, msg.ToolCalls...)
		}

		// 收集额外信息
		if len(msg.Extra) > 0 {
			extraList = append(extraList, msg.Extra)
		}

		if len(msg.AssistantGenMultiContent) > 0 {
			assistantGenMultiContentParts = append(assistantGenMultiContentParts, msg.AssistantGenMultiContent...)
		}

		if msg.ResponseMeta != nil && ret.ResponseMeta == nil {
			ret.ResponseMeta = &ResponseMeta{}
		}
		if msg.ResponseMeta != nil && ret.ResponseMeta != nil {
			// 保留最后一个有效的 FinishReason 值
			if msg.ResponseMeta.FinishReason != "" {
				ret.ResponseMeta.FinishReason = msg.ResponseMeta.FinishReason
			}

			if msg.ResponseMeta.Usage != nil {
				if ret.ResponseMeta.Usage == nil {
					ret.ResponseMeta.Usage = &TokenUsage{}
				}

				if msg.ResponseMeta.Usage.PromptTokens > ret.ResponseMeta.Usage.PromptTokens {
					ret.ResponseMeta.Usage.PromptTokens = msg.ResponseMeta.Usage.PromptTokens
				}
				if msg.ResponseMeta.Usage.CompletionTokens > ret.ResponseMeta.Usage.CompletionTokens {
					ret.ResponseMeta.Usage.CompletionTokens = msg.ResponseMeta.Usage.CompletionTokens
				}

				if msg.ResponseMeta.Usage.TotalTokens > ret.ResponseMeta.Usage.TotalTokens {
					ret.ResponseMeta.Usage.TotalTokens = msg.ResponseMeta.Usage.TotalTokens
				}

				if msg.ResponseMeta.Usage.PromptTokenDetails.CachedTokens > ret.ResponseMeta.Usage.PromptTokenDetails.CachedTokens {
					ret.ResponseMeta.Usage.PromptTokenDetails.CachedTokens = msg.ResponseMeta.Usage.PromptTokenDetails.CachedTokens
				}
			}

			if msg.ResponseMeta.LogProbs != nil {
				if ret.ResponseMeta.LogProbs == nil {
					ret.ResponseMeta.LogProbs = &LogProbs{}
				}

				ret.ResponseMeta.LogProbs.Content = append(ret.ResponseMeta.LogProbs.Content, msg.ResponseMeta.LogProbs.Content...)
			}

		}
	}

	// 合并文本内容
	if len(contents) > 0 {
		var sb strings.Builder
		sb.Grow(contentLen)
		for _, content := range contents {
			_, err := sb.WriteString(content)
			if err != nil {
				return nil, err
			}
		}

		ret.Content = sb.String()
	}

	// 合并推理内容
	if len(reasoningContents) > 0 {
		var sb strings.Builder
		sb.Grow(reasoningContentLen)
		for _, content := range reasoningContents {
			_, err := sb.WriteString(content)
			if err != nil {
				return nil, err
			}
		}

		ret.ReasoningContent = sb.String()
	}

	// 合并工具调用
	if len(toolCalls) > 0 {
		merged, err := concatToolCalls(toolCalls)
		if err != nil {
			return nil, err
		}

		ret.ToolCalls = merged
	}

	if len(extraList) > 0 {
		extra, err := concatExtra(extraList)
		if err != nil {
			return nil, fmt.Errorf("连接消息的额外部分失败: %w", err)
		}

		if len(extra) > 0 {
			ret.Extra = extra
		}
	}

	if len(assistantGenMultiContentParts) > 0 {
		merged, err := concatAssistantMultiContent(assistantGenMultiContentParts)
		if err != nil {
			return nil, fmt.Errorf("合并助手生成的多个内容失败: %w", err)
		}
		ret.AssistantGenMultiContent = merged
	}

	return &ret, nil
}

// ConcatMessageArray - 按位置合并多个消息数组，支持空消息和长度校验。
func ConcatMessageArray(mas [][]*Message) ([]*Message, error) {
	arrayLen := len(mas[0])

	ret := make([]*Message, arrayLen)
	slicesToConcat := make([][]*Message, arrayLen)

	for _, ma := range mas {
		if len(ma) != arrayLen {
			return nil, fmt.Errorf("unexpected array length. "+
				"Got %d, expected %d", len(ma), arrayLen)
		}

		for i := 0; i < arrayLen; i++ {
			m := ma[i]
			if m != nil {
				slicesToConcat[i] = append(slicesToConcat[i], m)
			}
		}
	}

	for i, slice := range slicesToConcat {
		if len(slice) == 0 {
			ret[i] = nil
		} else if len(slice) == 1 {
			ret[i] = slice[0]
		} else {
			cm, err := ConcatMessages(slice)
			if err != nil {
				return nil, err
			}

			ret[i] = cm
		}
	}

	return ret, nil
}

// ConcatMessageStream 读取并合并消息流中的所有消息。
// 自动关闭流，返回合并后的完整消息。
func ConcatMessageStream(s *StreamReader[*Message]) (*Message, error) {
	defer s.Close()

	var msgs []*Message
	for {
		msg, err := s.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		msgs = append(msgs, msg)
	}

	return ConcatMessages(msgs)
}

// 3.3 模板相关函数

// MessagesPlaceholder - 创建消息占位符模板，用于渲染时替换参数中的消息列表。
//
// 示例:
//
//	// 创建包含历史消息占位符的模板
//	chatTemplate := prompt.FromMessages(
//	        schema.SystemMessage("你是一个有用的助手"),
//	        schema.MessagesPlaceholder("history", false), // 渲染时使用 params["history"]
//	        schema.UserMessage("{query}"), // 用户查询消息
//	)
//
//	// 准备渲染参数
//	params := map[string]any{
//	        "history": []*schema.Message{
//	                {Role: "user", Content: "什么是 eino？"},
//	                {Role: "assistant", Content: "eino 是一个很好的 LLM 应用开发框架"},
//	        },
//	        "query": "如何使用 eino？",
//	}
//
//	// 渲染模板，history 占位符会被替换为参数中的消息列表
//	msgs, err := chatTemplate.Format(ctx, params)
func MessagesPlaceholder(key string, optional bool) MessagesTemplate {
	return &messagesPlaceholder{
		key:      key,
		optional: optional,
	}
}

// === Layer 4: 类型方法层 ===

// 4.1 Message 方法

// Format - 按指定格式渲染消息内容，支持文本和多模态内容的模板替换。
//
// 示例:
//
//	msg := schema.UserMessage("hello world, {name}")
//	msgs, err := msg.Format(ctx, map[string]any{"name": "eino"}, schema.FString)
//	// msgs[0].Content == "hello world, eino"
func (m *Message) Format(_ context.Context, vs map[string]any, formatType FormatType) ([]*Message, error) {
	c, err := formatContent(m.Content, vs, formatType)
	if err != nil {
		return nil, err
	}
	copied := *m
	copied.Content = c

	return []*Message{&copied}, nil
}

// String - 返回消息的字符串表示，包含角色、内容和工具调用等信息。
//
// 示例:
//
//	msg := schema.UserMessage("hello world")
//	fmt.Println(msg.String()) // 输出: `user: hello world`
//
//	工具消息示例:
//	msg := schema.ToolMessage("result", "call123")
//	fmt.Println(msg.String())
//	// 输出:
//	// tool: result
//	// tool_call_id: call123
func (m *Message) String() string {
	sb := &strings.Builder{}
	sb.WriteString(fmt.Sprintf("%s: %s", m.Role, m.Content))
	if len(m.ReasoningContent) > 0 {
		sb.WriteString("\n推理内容:\n")
		sb.WriteString(m.ReasoningContent)
	}
	if len(m.ToolCalls) > 0 {
		sb.WriteString("\n工具调用:\n")
		for _, tc := range m.ToolCalls {
			if tc.Index != nil {
				sb.WriteString(fmt.Sprintf("索引[%d]:", *tc.Index))
			}
			sb.WriteString(fmt.Sprintf("%+v\n", tc))
		}
	}
	if m.ToolCallID != "" {
		sb.WriteString(fmt.Sprintf("\n工具调用ID: %s", m.ToolCallID))
	}
	if m.ToolName != "" {
		sb.WriteString(fmt.Sprintf("\n工具调用名称: %s", m.ToolName))
	}
	if m.ResponseMeta != nil {
		sb.WriteString(fmt.Sprintf("\n完成原因: %s", m.ResponseMeta.FinishReason))
		if m.ResponseMeta.Usage != nil {
			sb.WriteString(fmt.Sprintf("\n用量: %v", m.ResponseMeta.Usage))
		}
	}

	return sb.String()
}

// 4.2 其他类型方法

// Format - 返回参数中指定键名的消息列表，实现占位符的渲染逻辑。
//
// 示例:
//
//	placeholder := MessagesPlaceholder("history", false)
//	params := map[string]any{
//	        "history": []*schema.Message{
//	                {Role: "user", Content: "什么是 eino？"},
//	                {Role: "assistant", Content: "eino 是一个很好的 LLM 应用开发框架"},
//	        },
//	        "query": "如何使用 eino？",
//	}
//	msgs, err := placeholder.Format(ctx, params) // 返回 params["history"] 的消息列表
func (p *messagesPlaceholder) Format(ctx context.Context, vs map[string]any, formatType FormatType) ([]*Message, error) {
	v, ok := vs[p.key]
	if !ok {
		if p.optional {
			return []*Message{}, nil
		}

		return nil, fmt.Errorf("消息占位符格式化：未找到键 '%s'", p.key)
	}

	msgs, ok := v.([]*Message)
	if !ok {
		return nil, fmt.Errorf("消息占位符只能使用消息列表格式化，键: '%v'，实际类型: %v", p.key, reflect.TypeOf(v))
	}

	return msgs, nil
}

// === Layer 5: 内部工具函数层 ===

// 5.1 内部实现

func concatToolCalls(chunks []ToolCall) ([]ToolCall, error) {
	var merged []ToolCall
	m := make(map[int][]int)
	for i := range chunks {
		index := chunks[i].Index
		if index == nil {
			merged = append(merged, chunks[i])
		} else {
			m[*index] = append(m[*index], i)
		}
	}

	var args strings.Builder
	for k, v := range m {
		index := k
		toolCall := ToolCall{Index: &index}
		if len(v) > 0 {
			toolCall = chunks[v[0]]
		}

		args.Reset()
		toolID, toolType, toolName := "", "", "" // 这些字段将在任何块中原子性地输出

		for _, n := range v {
			chunk := chunks[n]
			if chunk.ID != "" {
				if toolID == "" {
					toolID = chunk.ID
				} else if toolID != chunk.ID {
					return nil, fmt.Errorf("无法连接不同工具调用ID的工具调用：'%s' '%s'", toolID, chunk.ID)
				}

			}

			if chunk.Type != "" {
				if toolType == "" {
					toolType = chunk.Type
				} else if toolType != chunk.Type {
					return nil, fmt.Errorf("无法连接不同工具类型的工具调用：'%s' '%s'", toolType, chunk.Type)
				}
			}

			if chunk.Function.Name != "" {
				if toolName == "" {
					toolName = chunk.Function.Name
				} else if toolName != chunk.Function.Name {
					return nil, fmt.Errorf("无法连接不同工具名称的工具调用：'%s' '%s'", toolName, chunk.Function.Name)
				}
			}

			if chunk.Function.Arguments != "" {
				_, err := args.WriteString(chunk.Function.Arguments)
				if err != nil {
					return nil, err
				}
			}
		}

		toolCall.ID = toolID
		toolCall.Type = toolType
		toolCall.Function.Name = toolName
		toolCall.Function.Arguments = args.String()

		merged = append(merged, toolCall)
	}

	if len(merged) > 1 {
		sort.SliceStable(merged, func(i, j int) bool {
			iVal, jVal := merged[i].Index, merged[j].Index
			if iVal == nil && jVal == nil {
				return false
			} else if iVal == nil && jVal != nil {
				return true
			} else if iVal != nil && jVal == nil {
				return false
			}

			return *iVal < *jVal
		})
	}

	return merged, nil
}

func isBase64AudioPart(part MessageOutputPart) bool {
	return part.Type == ChatMessagePartTypeAudioURL &&
		part.Audio != nil &&
		part.Audio.Base64Data != nil &&
		part.Audio.URL == nil
}

// 合并助手生成的多个内容部分。
func concatAssistantMultiContent(parts []MessageOutputPart) ([]MessageOutputPart, error) {
	if len(parts) == 0 {
		return parts, nil
	}

	merged := make([]MessageOutputPart, 0, len(parts))
	i := 0
	for i < len(parts) {
		currentPart := parts[i]
		start := i

		if currentPart.Type == ChatMessagePartTypeText {
			// --- 文本合并 ---
			// 查找连续文本块的结束位置
			end := start + 1
			for end < len(parts) && parts[end].Type == ChatMessagePartTypeText {
				end++
			}

			// 只有一个部分，直接追加
			if end == start+1 {
				merged = append(merged, currentPart)
			} else {
				// 合并多个部分
				var sb strings.Builder
				for k := start; k < end; k++ {
					sb.WriteString(parts[k].Text)
				}
				mergedPart := MessageOutputPart{
					Type: ChatMessagePartTypeText,
					Text: sb.String(),
				}
				merged = append(merged, mergedPart)
			}
			i = end
		} else if isBase64AudioPart(currentPart) {
			// --- 音频合并 ---
			// 查找连续音频块的结束位置
			end := start + 1
			for end < len(parts) && isBase64AudioPart(parts[end]) {
				end++
			}

			// 只有一个部分，直接追加
			if end == start+1 {
				merged = append(merged, currentPart)
			} else {
				// 合并多个部分
				var b64Builder strings.Builder
				var mimeType string
				extraList := make([]map[string]any, 0, end-start)

				for k := start; k < end; k++ {
					audioPart := parts[k].Audio
					if audioPart.Base64Data != nil {
						b64Builder.WriteString(*audioPart.Base64Data)
					}
					if mimeType == "" {
						mimeType = audioPart.MIMEType
					}
					if len(audioPart.Extra) > 0 {
						extraList = append(extraList, audioPart.Extra)
					}
				}

				var mergedExtra map[string]any
				var err error
				if len(extraList) > 0 {
					mergedExtra, err = concatExtra(extraList)
					if err != nil {
						return nil, fmt.Errorf("合并音频额外信息失败: %w", err)
					}
				}

				mergedB64 := b64Builder.String()
				mergedPart := MessageOutputPart{
					Type: ChatMessagePartTypeAudioURL,
					Audio: &MessageOutputAudio{
						MessagePartCommon: MessagePartCommon{
							Base64Data: &mergedB64,
							MIMEType:   mimeType,
							Extra:      mergedExtra,
						},
					},
				}
				merged = append(merged, mergedPart)
			}
			i = end
		} else {
			// --- 其他类型：不可合并部分，直接追加 ---
			merged = append(merged, currentPart)
			i++
		}
	}

	return merged, nil
}

func concatExtra(extraList []map[string]any) (map[string]any, error) {
	if len(extraList) == 1 {
		return generic.CopyMap(extraList[0]), nil
	}

	return internal.ConcatItems(extraList)
}

// formatContent - 根据指定格式类型渲染内容模板，支持 FString、GoTemplate 和 Jinja2。
func formatContent(content string, vs map[string]any, formatType FormatType) (string, error) {
	switch formatType {
	case FString:
		return pyfmt.Fmt(content, vs)
	case GoTemplate:
		parsedTmpl, err := template.New("template").
			Option("missingkey=error").
			Parse(content)
		if err != nil {
			return "", err
		}
		sb := new(strings.Builder)
		err = parsedTmpl.Execute(sb, vs)
		if err != nil {
			return "", err
		}
		return sb.String(), nil
	case Jinja2:
		env, err := getJinjaEnv()
		if err != nil {
			return "", err
		}
		tpl, err := env.FromString(content)
		if err != nil {
			return "", err
		}
		out, err := tpl.Execute(vs)
		if err != nil {
			return "", err
		}
		return out, nil
	default:
		return "", fmt.Errorf("未知的格式类型: %v", formatType)
	}
}

// 5.2 模板工具函数

const (
	jinjaInclude = "include" // Jinja 包含语法
	jinjaExtends = "extends" // Jinja 继承语法
	jinjaImport  = "import"  // Jinja 导入语法
	jinjaFrom    = "from"    // Jinja 变量引用语法
)

var jinjaEnvOnce sync.Once      // 确保只初始化一次
var jinjaEnv *gonja.Environment // 全局环境实例
var envInitErr error            // 初始化错误状态

func getJinjaEnv() (*gonja.Environment, error) {
	jinjaEnvOnce.Do(func() {
		jinjaEnv = gonja.NewEnvironment(config.DefaultConfig, gonja.DefaultLoader)
		formatInitError := "init jinja env fail: %w"
		var err error
		if jinjaEnv.Statements.Exists(jinjaInclude) {
			err = jinjaEnv.Statements.Replace(jinjaInclude, func(parser *parser.Parser, args *parser.Parser) (nodes.Statement, error) {
				return nil, fmt.Errorf("keyword[include] has been disabled")
			})
			if err != nil {
				envInitErr = fmt.Errorf(formatInitError, err)
				return
			}
		}
		if jinjaEnv.Statements.Exists(jinjaExtends) {
			err = jinjaEnv.Statements.Replace(jinjaExtends, func(parser *parser.Parser, args *parser.Parser) (nodes.Statement, error) {
				return nil, fmt.Errorf("keyword[extends] has been disabled")
			})
			if err != nil {
				envInitErr = fmt.Errorf(formatInitError, err)
				return
			}
		}
		if jinjaEnv.Statements.Exists(jinjaFrom) {
			err = jinjaEnv.Statements.Replace(jinjaFrom, func(parser *parser.Parser, args *parser.Parser) (nodes.Statement, error) {
				return nil, fmt.Errorf("keyword[from] has been disabled")
			})
			if err != nil {
				envInitErr = fmt.Errorf(formatInitError, err)
				return
			}
		}
		if jinjaEnv.Statements.Exists(jinjaImport) {
			err = jinjaEnv.Statements.Replace(jinjaImport, func(parser *parser.Parser, args *parser.Parser) (nodes.Statement, error) {
				return nil, fmt.Errorf("keyword[import] has been disabled")
			})
			if err != nil {
				envInitErr = fmt.Errorf(formatInitError, err)
				return
			}
		}
	})
	return jinjaEnv, envInitErr
}

// 5.3 类型断言（验证实现关系）
var _ MessagesTemplate = &Message{}                     // 验证 Message 实现接口
var _ MessagesTemplate = MessagesPlaceholder("", false) // 验证占位符实现接口

// 5.4 包初始化

func init() {
	internal.RegisterStreamChunkConcatFunc(ConcatMessages)
	internal.RegisterStreamChunkConcatFunc(ConcatMessageArray)
}
