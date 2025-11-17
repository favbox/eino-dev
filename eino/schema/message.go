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

func init() {
	internal.RegisterStreamChunkConcatFunc(ConcatMessages)
	internal.RegisterStreamChunkConcatFunc(ConcatMessageArray)
}

// ConcatMessageArray 合并消息数组的数组。
// 要求所有子数组长度相同，按位置合并消息。
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

// FormatType 消息模板的格式化类型。
type FormatType uint8

const (
	// FString Python 风格的字符串格式化 (PEP-3101)。
	// 由 pyfmt 库实现。
	FString FormatType = 0
	// GoTemplate Go 标准库的 text/template 格式化。
	GoTemplate FormatType = 1
	// Jinja2 Jinja2 模板格式化。
	// 由 gonja 库实现。
	Jinja2 FormatType = 2
)

// RoleType 消息角色类型。
type RoleType string

const (
	// Assistant 助手角色，表示消息由聊天模型返回。
	Assistant RoleType = "assistant"
	// User 用户角色，表示消息来自用户输入。
	User RoleType = "user"
	// System 系统角色，表示消息为系统消息。
	System RoleType = "system"
	// Tool 工具角色，表示消息为工具调用输出。
	Tool RoleType = "tool"
)

// FunctionCall 消息中的函数调用信息。
// 用于 Assistant 消息中的工具调用。
type FunctionCall struct {
	// Name 函数名称，用于标识具体的函数。
	Name string `json:"name,omitempty"`
	// Arguments 函数参数，JSON 格式字符串。
	Arguments string `json:"arguments,omitempty"`
}

// ToolCall 消息中的工具调用信息。
// 用于 Assistant 消息中需要调用工具的场景。
type ToolCall struct {
	// Index 工具调用索引，用于多工具调用场景。
	// 流式模式下用于标识分块以便合并。
	Index *int `json:"index,omitempty"`
	// ID 工具调用的唯一标识符。
	ID string `json:"id"`
	// Type 工具调用类型。
	// 默认值："function"。
	Type string `json:"type"`
	// Function 要调用的函数信息。
	Function FunctionCall `json:"function"`

	// Extra 额外信息存储。
	Extra map[string]any `json:"extra,omitempty"`
}

// ImageURLDetail 图像 URL 的质量级别。
type ImageURLDetail string

const (
	// ImageURLDetailHigh 高质量图像。
	ImageURLDetailHigh ImageURLDetail = "high"
	// ImageURLDetailLow 低质量图像。
	ImageURLDetailLow ImageURLDetail = "low"
	// ImageURLDetailAuto 自动选择质量。
	ImageURLDetailAuto ImageURLDetail = "auto"
)

// MessagePartCommon 多模态类型的通用抽象组件。
// 用于输入和输出的统一表示。
type MessagePartCommon struct {
	// URL 资源 URL，可以是传统 URL 或符合 RFC-2397 的 data URL。
	// 具体使用方式请参考模型实现的文档。
	URL *string `json:"url,omitempty"`

	// Base64Data Base64 编码的二进制数据。
	Base64Data *string `json:"base64data,omitempty"`

	// MIMEType MIME 类型，例如 "image/png"、"audio/wav"。
	MIMEType string `json:"mime_type,omitempty"`

	// Extra 额外信息存储。
	Extra map[string]any `json:"extra,omitempty"`
}

// MessageInputImage 消息中的图像输入部分。
// 可选择使用 URL 或 Base64Data。
type MessageInputImage struct {
	MessagePartCommon

	// Detail 图像质量级别。
	Detail ImageURLDetail `json:"detail,omitempty"`
}

// MessageInputAudio 消息中的音频输入部分。
// 可选择使用 URL 或 Base64Data。
type MessageInputAudio struct {
	MessagePartCommon
}

// MessageInputVideo 消息中的视频输入部分。
// 可选择使用 URL 或 Base64Data。
type MessageInputVideo struct {
	MessagePartCommon
}

// MessageInputFile 消息中的文件输入部分。
// 可选择使用 URL 或 Base64Data。
type MessageInputFile struct {
	MessagePartCommon
}

// MessageInputPart 消息的输入部分。
// 支持文本和多种多模态内容类型。
type MessageInputPart struct {
	// Type 内容类型。
	Type ChatMessagePartType `json:"type"`

	// Text 文本内容，当 Type 为 "text" 时使用。
	Text string `json:"text,omitempty"`

	// Image 图像输入，当 Type 为 "image_url" 时使用。
	Image *MessageInputImage `json:"image,omitempty"`

	// Audio 音频输入，当 Type 为 "audio_url" 时使用。
	Audio *MessageInputAudio `json:"audio,omitempty"`

	// Video 视频输入，当 Type 为 "video_url" 时使用。
	Video *MessageInputVideo `json:"video,omitempty"`

	// File 文件输入，当 Type 为 "file_url" 时使用。
	File *MessageInputFile `json:"file,omitempty"`
}

// MessageOutputImage 消息中的图像输出部分。
type MessageOutputImage struct {
	MessagePartCommon
}

// MessageOutputAudio 消息中的音频输出部分。
type MessageOutputAudio struct {
	MessagePartCommon
}

// MessageOutputVideo 消息中的视频输出部分。
type MessageOutputVideo struct {
	MessagePartCommon
}

// MessageOutputPart 助手生成消息的输出部分。
// 可包含文本或多媒体内容（图像、音频、视频）。
type MessageOutputPart struct {
	// Type 内容类型，例如 "text"、"image_url"、"audio_url"、"video_url"。
	Type ChatMessagePartType `json:"type"`

	// Text 文本内容，当 Type 为 "text" 时使用。
	Text string `json:"text,omitempty"`

	// Image 图像输出，当 Type 为 ChatMessagePartTypeImageURL 时使用。
	Image *MessageOutputImage `json:"image,omitempty"`

	// Audio 音频输出，当 Type 为 ChatMessagePartTypeAudioURL 时使用。
	Audio *MessageOutputAudio `json:"audio,omitempty"`

	// Video 视频输出，当 Type 为 ChatMessagePartTypeVideoURL 时使用。
	Video *MessageOutputVideo `json:"video,omitempty"`
}

// ChatMessageImageURL 聊天消息中的图像部分（已废弃）。
// 已废弃：此结构已废弃，因为 MultiContent 字段已废弃。
// 可选择使用 URL 或 URI。支持 RFC-2397 定义的内联图像数据。
type ChatMessageImageURL struct {
	// URL 资源 URL，可以是传统 URL 或符合 RFC-2397 的 data URL。
	// 具体使用方式请参考模型实现的文档。
	URL string `json:"url,omitempty"`

	// URI 资源 URI（已废弃）。
	URI string `json:"uri,omitempty"`
	// Detail 图像质量级别。
	Detail ImageURLDetail `json:"detail,omitempty"`

	// MIMEType MIME 类型，例如 "image/png"。
	MIMEType string `json:"mime_type,omitempty"`
	// Extra 额外信息存储。
	Extra map[string]any `json:"extra,omitempty"`
}

// ChatMessagePartType 聊天消息部分的内容类型。
type ChatMessagePartType string

const (
	// ChatMessagePartTypeText 文本类型。
	ChatMessagePartTypeText ChatMessagePartType = "text"
	// ChatMessagePartTypeImageURL 图像 URL 类型。
	ChatMessagePartTypeImageURL ChatMessagePartType = "image_url"
	// ChatMessagePartTypeAudioURL 音频 URL 类型。
	ChatMessagePartTypeAudioURL ChatMessagePartType = "audio_url"
	// ChatMessagePartTypeVideoURL 视频 URL 类型。
	ChatMessagePartTypeVideoURL ChatMessagePartType = "video_url"
	// ChatMessagePartTypeFileURL 文件 URL 类型。
	ChatMessagePartTypeFileURL ChatMessagePartType = "file_url"
)

// ChatMessageAudioURL 聊天消息中的音频部分（已废弃）。
// 已废弃：此结构已废弃，因为 MultiContent 字段已废弃。
// 可选择使用 URL 或 URI。支持 RFC-2397 定义的内联音频数据。
type ChatMessageAudioURL struct {
	// URL 资源 URL，可以是传统 URL 或符合 RFC-2397 的 data URL。
	// 具体使用方式请参考模型实现的文档。
	URL string `json:"url,omitempty"`
	// URI 资源 URI（已废弃）。
	URI string `json:"uri,omitempty"`

	// MIMEType MIME 类型，例如 "audio/wav"、"audio/ogg"。
	MIMEType string `json:"mime_type,omitempty"`
	// Extra 额外信息存储。
	Extra map[string]any `json:"extra,omitempty"`
}

// ChatMessageVideoURL 聊天消息中的视频部分（已废弃）。
// 已废弃：此结构已废弃，因为 MultiContent 字段已废弃。
// 可选择使用 URL 或 URI。支持 RFC-2397 定义的内联视频数据。
type ChatMessageVideoURL struct {
	// URL 资源 URL，可以是传统 URL 或符合 RFC-2397 的 data URL。
	// 具体使用方式请参考模型实现的文档。
	URL string `json:"url,omitempty"`
	// URI 资源 URI（已废弃）。
	URI string `json:"uri,omitempty"`

	// MIMEType MIME 类型，例如 "video/mp4"。
	MIMEType string `json:"mime_type,omitempty"`
	// Extra 额外信息存储。
	Extra map[string]any `json:"extra,omitempty"`
}

// ChatMessageFileURL 聊天消息中的文件部分（已废弃）。
// 已废弃：此结构已废弃，因为 MultiContent 字段已废弃。
// 可选择使用 URL 或 URI。
type ChatMessageFileURL struct {
	// URL 资源 URL。
	URL string `json:"url,omitempty"`
	// URI 资源 URI（已废弃）。
	URI string `json:"uri,omitempty"`

	// MIMEType MIME 类型，例如 "application/pdf"、"text/plain"。
	MIMEType string `json:"mime_type,omitempty"`
	// Name 文件名称。
	Name string `json:"name,omitempty"`

	// Extra 额外信息存储。
	Extra map[string]any `json:"extra,omitempty"`
}

// ChatMessagePart 聊天消息中的内容部分（已废弃）。
// 已废弃：此结构已废弃，因为 MultiContent 字段已废弃。
type ChatMessagePart struct {
	// Type 内容类型，例如 "text"、"image_url"、"audio_url"、"video_url"、"file_url"。
	Type ChatMessagePartType `json:"type,omitempty"`

	// Text 文本内容，当 Type 为 "text" 时使用。
	Text string `json:"text,omitempty"`

	// ImageURL 图像 URL，当 Type 为 "image_url" 时使用。
	ImageURL *ChatMessageImageURL `json:"image_url,omitempty"`
	// AudioURL 音频 URL，当 Type 为 "audio_url" 时使用。
	AudioURL *ChatMessageAudioURL `json:"audio_url,omitempty"`
	// VideoURL 视频 URL，当 Type 为 "video_url" 时使用。
	VideoURL *ChatMessageVideoURL `json:"video_url,omitempty"`
	// FileURL 文件 URL，当 Type 为 "file_url" 时使用。
	FileURL *ChatMessageFileURL `json:"file_url,omitempty"`
}

// LogProbs 包含日志概率信息的顶层结构。
type LogProbs struct {
	// Content 消息内容 token 及其日志概率信息列表。
	Content []LogProb `json:"content"`
}

// LogProb token 的概率信息。
type LogProb struct {
	// Token token 的文本表示，是语言模型分词过程识别的连续字符序列。
	// 例如：单词、词的一部分或标点符号。
	Token string `json:"token"`
	// LogProb token 的对数概率。
	// 若在前 20 个最可能的 token 之内，则为实际值；否则为 -9999.0 表示极不可能。
	LogProb float64 `json:"logprob"`
	// Bytes token 的 UTF-8 字节表示。
	// 用于多 token 表示单个字符时的字节组合场景。可能为空。
	Bytes []int64 `json:"bytes,omitempty"`
	// TopLogProbs 当前位置最可能的 token 及其对数概率列表。
	// 在极少数情况下，返回数量可能少于请求的 top_logprobs 数量。
	TopLogProbs []TopLogProb `json:"top_logprobs"`
}

// TopLogProb 高概率 token 的信息。
type TopLogProb struct {
	// Token token 的文本表示。
	Token string `json:"token"`
	// LogProb token 的对数概率。
	// 若在前 20 个最可能的 token 之内，则为实际值；否则为 -9999.0。
	LogProb float64 `json:"logprob"`
	// Bytes token 的 UTF-8 字节表示。
	Bytes []int64 `json:"bytes,omitempty"`
}

// ResponseMeta 聊天响应的元信息。
type ResponseMeta struct {
	// FinishReason 响应结束原因。
	// 通常为 "stop"、"length"、"tool_calls"、"content_filter"、"null" 等，具体由模型实现定义。
	FinishReason string `json:"finish_reason,omitempty"`
	// Usage token 使用情况。
	// 是否存在取决于聊天模型实现是否返回。
	Usage *TokenUsage `json:"usage,omitempty"`
	// LogProbs 日志概率信息。
	LogProbs *LogProbs `json:"logprobs,omitempty"`
}

// Message 消息结构体，表示模型输入和输出的数据结构。
// 来源可以是用户输入或模型返回，支持纯文本和多模态内容。
//
// 用户纯文本输入，使用 Content 字段：
//
//	&schema.Message{
//		Role:    schema.User,
//		Content: "法国的首都是什么？",
//	}
//
// 用户多模态输入，使用 UserInputMultiContent 字段。
// 可以组合文本与其他媒体（如图像）：
//
//	&schema.Message{
//		Role: schema.User,
//		UserInputMultiContent: []schema.MessageInputPart{
//			{Type: schema.ChatMessagePartTypeText, Text: "这张图片里有什么？"},
//			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{
//				MessagePartCommon: schema.MessagePartCommon{
//					URL: toPtr("https://example.com/cat.jpg"),
//				},
//				Detail: schema.ImageURLDetailHigh,
//			}},
//		},
//	}
//
// 模型返回多模态内容时，使用 AssistantGenMultiContent 字段：
//
//	&schema.Message{
//		Role: schema.Assistant,
//		AssistantGenMultiContent: []schema.MessageOutputPart{
//			{Type: schema.ChatMessagePartTypeText, Text: "这是生成的图像："},
//			{Type: schema.ChatMessagePartTypeImage, Image: &schema.MessageOutputImage{
//				MessagePartCommon: schema.MessagePartCommon{
//					Base64Data: toPtr("base64_image_binary"),
//					MIMEType:   "image/png",
//				},
//			}},
//		},
//	}
type Message struct {
	// Role 消息角色。
	Role RoleType `json:"role"`

	// Content 文本内容。
	// 用于用户文本输入和模型文本输出。
	Content string `json:"content"`

	// MultiContent 多内容部分（已废弃）。
	// 已废弃：用户多模态输入使用 UserInputMultiContent，模型多模态输出使用 AssistantGenMultiContent。
	MultiContent []ChatMessagePart `json:"multi_content,omitempty"`

	// UserInputMultiContent 用户提供的多模态内容。
	UserInputMultiContent []MessageInputPart `json:"user_input_multi_content,omitempty"`

	// AssistantGenMultiContent 模型生成的多模态输出。
	AssistantGenMultiContent []MessageOutputPart `json:"assistant_output_multi_content,omitempty"`

	// Name 消息名称。
	Name string `json:"name,omitempty"`

	// ToolCalls 工具调用列表（仅用于 Assistant 消息）。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// ToolCallID 工具调用 ID（仅用于 Tool 消息）。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolName 工具名称（仅用于 Tool 消息）。
	ToolName string `json:"tool_name,omitempty"`

	// ResponseMeta 响应元信息。
	ResponseMeta *ResponseMeta `json:"response_meta,omitempty"`

	// ReasoningContent 模型的思考过程。
	// 当模型返回推理内容时包含此字段。
	ReasoningContent string `json:"reasoning_content,omitempty"`

	// Extra 模型实现的自定义信息。
	Extra map[string]any `json:"extra,omitempty"`
}

// TokenUsage 聊天模型请求的 token 使用情况。
type TokenUsage struct {
	// PromptTokens 提示 token 数量。
	// 包含本次请求的所有输入 token。
	PromptTokens int `json:"prompt_tokens"`
	// PromptTokenDetails 提示 token 的详细分解。
	PromptTokenDetails PromptTokenDetails `json:"prompt_token_details"`
	// CompletionTokens 补全 token 数量。
	CompletionTokens int `json:"completion_tokens"`
	// TotalTokens 总 token 数量。
	TotalTokens int `json:"total_tokens"`
}

// PromptTokenDetails 提示 token 的详细信息。
type PromptTokenDetails struct {
	// CachedTokens 提示中的缓存 token 数量。
	CachedTokens int `json:"cached_tokens"`
}

var _ MessagesTemplate = &Message{}
var _ MessagesTemplate = MessagesPlaceholder("", false)

// MessagesTemplate 消息模板接口。
// 用于将模板渲染为消息列表。
//
// 使用示例：
//
//	chatTemplate := prompt.FromMessages(
//		schema.SystemMessage("you are eino helper"),
//		schema.MessagesPlaceholder("history", false), // 使用 params 中的 "history" 值
//	)
//	msgs, err := chatTemplate.Format(ctx, params)
type MessagesTemplate interface {
	Format(ctx context.Context, vs map[string]any, formatType FormatType) ([]*Message, error)
}

// messagesPlaceholder 消息占位符实现。
type messagesPlaceholder struct {
	key      string // 参数键名。
	optional bool   // 是否可选。
}

// MessagesPlaceholder 创建消息占位符。
// 将占位符渲染为参数中的消息列表。
//
// 使用示例：
//
//	placeholder := MessagesPlaceholder("history", false)
//	params := map[string]any{
//		"history": []*schema.Message{{Role: "user", Content: "what is eino?"}, {Role: "assistant", Content: "eino is a great framework to build llm apps"}},
//		"query": "how to use eino?",
//	}
//	chatTemplate := prompt.FromMessages(
//		schema.SystemMessage("you are eino helper"),
//		schema.MessagesPlaceholder("history", false), // 使用 params 中的 "history" 值
//	)
//	msgs, err := chatTemplate.Format(ctx, params)
func MessagesPlaceholder(key string, optional bool) MessagesTemplate {
	return &messagesPlaceholder{
		key:      key,
		optional: optional,
	}
}

// Format 返回指定键对应的消息列表。
// 因为这是占位符，所以直接返回参数中的消息。
//
// 使用示例：
//
//	placeholder := MessagesPlaceholder("history", false)
//	params := map[string]any{
//		"history": []*schema.Message{{Role: "user", Content: "what is eino?"}, {Role: "assistant", Content: "eino is a great framework to build llm apps"}},
//		"query": "how to use eino?",
//	}
//	msgs, err := placeholder.Format(ctx, params) // 返回 params 中 "history" 的值
func (p *messagesPlaceholder) Format(_ context.Context, vs map[string]any, _ FormatType) ([]*Message, error) {
	v, ok := vs[p.key]
	if !ok {
		if p.optional {
			return []*Message{}, nil
		}

		return nil, fmt.Errorf("message placeholder format: %s not found", p.key)
	}

	msgs, ok := v.([]*Message)
	if !ok {
		return nil, fmt.Errorf("only messages can be used to format message placeholder, key: %v, actual type: %v", p.key, reflect.TypeOf(v))
	}

	return msgs, nil
}

// formatContent 根据格式化类型格式化内容字符串。
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
		return "", fmt.Errorf("unknown format type: %v", formatType)
	}
}

// Format 根据指定格式类型渲染消息并返回。
//
// 使用示例：
//
//	msg := schema.UserMessage("hello world, {name}")
//	msgs, err := msg.Format(ctx, map[string]any{"name": "eino"}, schema.FString) // 使用 pyfmt 渲染消息内容
//	// msgs[0].Content 将是 "hello world, eino"
func (m *Message) Format(_ context.Context, vs map[string]any, formatType FormatType) ([]*Message, error) {
	c, err := formatContent(m.Content, vs, formatType)
	if err != nil {
		return nil, err
	}
	copied := *m
	copied.Content = c

	if len(m.MultiContent) > 0 {
		copied.MultiContent, err = formatMultiContent(m.MultiContent, vs, formatType)
		if err != nil {
			return nil, err
		}
	}

	if len(m.UserInputMultiContent) > 0 {
		copied.UserInputMultiContent, err = formatUserInputMultiContent(m.UserInputMultiContent, vs, formatType)
		if err != nil {
			return nil, err
		}
	}

	return []*Message{&copied}, nil
}

// formatMultiContent 格式化多内容部分（已废弃的 MultiContent 字段）。
func formatMultiContent(multiContent []ChatMessagePart, vs map[string]any, formatType FormatType) ([]ChatMessagePart, error) {
	copiedMC := make([]ChatMessagePart, len(multiContent))
	copy(copiedMC, multiContent)

	for i, mc := range copiedMC {
		switch mc.Type {
		case ChatMessagePartTypeText:
			nmc, err := formatContent(mc.Text, vs, formatType)
			if err != nil {
				return nil, err
			}
			copiedMC[i].Text = nmc
		case ChatMessagePartTypeImageURL:
			if mc.ImageURL == nil {
				continue
			}
			url, err := formatContent(mc.ImageURL.URL, vs, formatType)
			if err != nil {
				return nil, err
			}
			copiedMC[i].ImageURL.URL = url
		case ChatMessagePartTypeAudioURL:
			if mc.AudioURL == nil {
				continue
			}
			url, err := formatContent(mc.AudioURL.URL, vs, formatType)
			if err != nil {
				return nil, err
			}
			copiedMC[i].AudioURL.URL = url
		case ChatMessagePartTypeVideoURL:
			if mc.VideoURL == nil {
				continue
			}
			url, err := formatContent(mc.VideoURL.URL, vs, formatType)
			if err != nil {
				return nil, err
			}
			copiedMC[i].VideoURL.URL = url
		case ChatMessagePartTypeFileURL:
			if mc.FileURL == nil {
				continue
			}
			url, err := formatContent(mc.FileURL.URL, vs, formatType)
			if err != nil {
				return nil, err
			}
			copiedMC[i].FileURL.URL = url
		}
	}

	return copiedMC, nil
}

// formatUserInputMultiContent 格式化用户输入的多模态内容。
func formatUserInputMultiContent(userInputMultiContent []MessageInputPart, vs map[string]any, formatType FormatType) ([]MessageInputPart, error) {
	copiedUIMC := make([]MessageInputPart, len(userInputMultiContent))
	copy(copiedUIMC, userInputMultiContent)

	for i, uimc := range copiedUIMC {
		switch uimc.Type {
		case ChatMessagePartTypeText:
			text, err := formatContent(uimc.Text, vs, formatType)
			if err != nil {
				return nil, err
			}
			copiedUIMC[i].Text = text
		case ChatMessagePartTypeImageURL:
			if uimc.Image == nil {
				continue
			}
			if uimc.Image.URL != nil && *uimc.Image.URL != "" {
				url, err := formatContent(*uimc.Image.URL, vs, formatType)
				if err != nil {
					return nil, err
				}
				copiedUIMC[i].Image.URL = &url
			}
			if uimc.Image.Base64Data != nil && *uimc.Image.Base64Data != "" {
				base64data, err := formatContent(*uimc.Image.Base64Data, vs, formatType)
				if err != nil {
					return nil, err
				}
				copiedUIMC[i].Image.Base64Data = &base64data
			}
		case ChatMessagePartTypeAudioURL:
			if uimc.Audio == nil {
				continue
			}
			if uimc.Audio.URL != nil && *uimc.Audio.URL != "" {
				url, err := formatContent(*uimc.Audio.URL, vs, formatType)
				if err != nil {
					return nil, err
				}
				copiedUIMC[i].Audio.URL = &url
			}
			if uimc.Audio.Base64Data != nil && *uimc.Audio.Base64Data != "" {
				base64data, err := formatContent(*uimc.Audio.Base64Data, vs, formatType)
				if err != nil {
					return nil, err
				}
				copiedUIMC[i].Audio.Base64Data = &base64data
			}
		case ChatMessagePartTypeVideoURL:
			if uimc.Video == nil {
				continue
			}
			if uimc.Video.URL != nil && *uimc.Video.URL != "" {
				url, err := formatContent(*uimc.Video.URL, vs, formatType)
				if err != nil {
					return nil, err
				}
				copiedUIMC[i].Video.URL = &url
			}
			if uimc.Video.Base64Data != nil && *uimc.Video.Base64Data != "" {
				base64data, err := formatContent(*uimc.Video.Base64Data, vs, formatType)
				if err != nil {
					return nil, err
				}
				copiedUIMC[i].Video.Base64Data = &base64data
			}
		case ChatMessagePartTypeFileURL:
			if uimc.File == nil {
				continue
			}
			if uimc.File.URL != nil && *uimc.File.URL != "" {
				url, err := formatContent(*uimc.File.URL, vs, formatType)
				if err != nil {
					return nil, err
				}
				copiedUIMC[i].File.URL = &url
			}
			if uimc.File.Base64Data != nil && *uimc.File.Base64Data != "" {
				base64data, err := formatContent(*uimc.File.Base64Data, vs, formatType)
				if err != nil {
					return nil, err
				}
				copiedUIMC[i].File.Base64Data = &base64data
			}
		}
	}

	return copiedUIMC, nil
}

// String 返回消息的字符串表示。
//
// 使用示例：
//
//	msg := schema.UserMessage("hello world")
//	fmt.Println(msg.String()) // 输出: user: hello world
//
//	msg := schema.Message{
//		Role:    schema.Tool,
//		Content: "{...}",
//		ToolCallID: "callxxxx"
//	}
//	fmt.Println(msg.String())
//	// 输出:
//	//   tool: {...}
//	//   call_id: callxxxx
func (m *Message) String() string {
	sb := &strings.Builder{}
	sb.WriteString(fmt.Sprintf("%s: %s", m.Role, m.Content))
	if len(m.ReasoningContent) > 0 {
		sb.WriteString("\nreasoning content:\n")
		sb.WriteString(m.ReasoningContent)
	}
	if len(m.ToolCalls) > 0 {
		sb.WriteString("\ntool_calls:\n")
		for _, tc := range m.ToolCalls {
			if tc.Index != nil {
				sb.WriteString(fmt.Sprintf("index[%d]:", *tc.Index))
			}
			sb.WriteString(fmt.Sprintf("%+v\n", tc))
		}
	}
	if m.ToolCallID != "" {
		sb.WriteString(fmt.Sprintf("\ntool_call_id: %s", m.ToolCallID))
	}
	if m.ToolName != "" {
		sb.WriteString(fmt.Sprintf("\ntool_call_name: %s", m.ToolName))
	}
	if m.ResponseMeta != nil {
		sb.WriteString(fmt.Sprintf("\nfinish_reason: %s", m.ResponseMeta.FinishReason))
		if m.ResponseMeta.Usage != nil {
			sb.WriteString(fmt.Sprintf("\nusage: %v", m.ResponseMeta.Usage))
		}
	}

	return sb.String()
}

// SystemMessage 创建系统消息。
func SystemMessage(content string) *Message {
	return &Message{
		Role:    System,
		Content: content,
	}
}

// AssistantMessage 创建助手消息。
func AssistantMessage(content string, toolCalls []ToolCall) *Message {
	return &Message{
		Role:      Assistant,
		Content:   content,
		ToolCalls: toolCalls,
	}
}

// UserMessage 创建用户消息。
func UserMessage(content string) *Message {
	return &Message{
		Role:    User,
		Content: content,
	}

}

// toolMessageOptions 工具消息的选项配置。
type toolMessageOptions struct {
	toolName string // 工具名称。
}

// ToolMessageOption 工具消息选项函数类型。
type ToolMessageOption func(*toolMessageOptions)

// WithToolName 设置工具调用名称。
func WithToolName(name string) ToolMessageOption {
	return func(o *toolMessageOptions) {
		o.toolName = name
	}
}

// ToolMessage 创建工具消息。
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

// concatToolCalls 合并工具调用分块。
// 根据 Index 字段合并相同工具调用的多个分块。
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
		toolID, toolType, toolName := "", "", "" // these field will output atomically in any chunk

		for _, n := range v {
			chunk := chunks[n]
			if chunk.ID != "" {
				if toolID == "" {
					toolID = chunk.ID
				} else if toolID != chunk.ID {
					return nil, fmt.Errorf("cannot concat ToolCalls with different tool id: '%s' '%s'", toolID, chunk.ID)
				}

			}

			if chunk.Type != "" {
				if toolType == "" {
					toolType = chunk.Type
				} else if toolType != chunk.Type {
					return nil, fmt.Errorf("cannot concat ToolCalls with different tool type: '%s' '%s'", toolType, chunk.Type)
				}
			}

			if chunk.Function.Name != "" {
				if toolName == "" {
					toolName = chunk.Function.Name
				} else if toolName != chunk.Function.Name {
					return nil, fmt.Errorf("cannot concat ToolCalls with different tool name: '%s' '%s'", toolName, chunk.Function.Name)
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

// isBase64AudioPart 检查是否为 Base64 编码的音频部分。
func isBase64AudioPart(part MessageOutputPart) bool {
	return part.Type == ChatMessagePartTypeAudioURL &&
		part.Audio != nil &&
		part.Audio.Base64Data != nil &&
		part.Audio.URL == nil
}

// concatAssistantMultiContent 合并助手多内容部分。
// 连续的文本部分会被合并，连续的 Base64 音频部分会被合并。
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
			// --- Text Merging ---
			// Find end of contiguous text block
			end := start + 1
			for end < len(parts) && parts[end].Type == ChatMessagePartTypeText {
				end++
			}

			// If only one part, just append it
			if end == start+1 {
				merged = append(merged, currentPart)
			} else {
				// Multiple parts to merge
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
			// --- Audio Merging ---
			// Find end of contiguous audio block
			end := start + 1
			for end < len(parts) && isBase64AudioPart(parts[end]) {
				end++
			}

			// If only one part, just append it
			if end == start+1 {
				merged = append(merged, currentPart)
			} else {
				// Multiple parts to merge
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
						return nil, fmt.Errorf("failed to concat audio extra: %w", err)
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
			// --- Non-mergeable part ---
			merged = append(merged, currentPart)
			i++
		}
	}

	return merged, nil
}

// concatExtra 合并额外信息映射。
func concatExtra(extraList []map[string]any) (map[string]any, error) {
	if len(extraList) == 1 {
		return generic.CopyMap(extraList[0]), nil
	}

	return internal.ConcatItems(extraList)
}

// ConcatMessages 合并具有相同角色和名称的消息。
// 合并相同索引的工具调用，若消息角色或名称不同则返回错误。
// 适用于流式消息的合并场景。
//
// 使用示例：
//
//	msgs := []*Message{}
//	for {
//		msg, err := stream.Recv()
//		if errors.Is(err, io.EOF) {
//			break
//		}
//		if err != nil {...}
//		msgs = append(msgs, msg)
//	}
//	concatedMsg, err := ConcatMessages(msgs) // concatedMsg.Content 将是所有消息的完整内容
func ConcatMessages(msgs []*Message) (*Message, error) {
	var (
		contents                      []string
		contentLen                    int
		reasoningContents             []string
		reasoningContentLen           int
		toolCalls                     []ToolCall
		multiContentParts             []ChatMessagePart
		assistantGenMultiContentParts []MessageOutputPart
		ret                           = Message{}
		extraList                     = make([]map[string]any, 0, len(msgs))
	)

	for idx, msg := range msgs {
		if msg == nil {
			return nil, fmt.Errorf("unexpected nil chunk in message stream, index: %d", idx)
		}

		if msg.Role != "" {
			if ret.Role == "" {
				ret.Role = msg.Role
			} else if ret.Role != msg.Role {
				return nil, fmt.Errorf("cannot concat messages with "+
					"different roles: '%s' '%s'", ret.Role, msg.Role)
			}
		}

		if msg.Name != "" {
			if ret.Name == "" {
				ret.Name = msg.Name
			} else if ret.Name != msg.Name {
				return nil, fmt.Errorf("cannot concat messages with"+
					" different names: '%s' '%s'", ret.Name, msg.Name)
			}
		}

		if msg.ToolCallID != "" {
			if ret.ToolCallID == "" {
				ret.ToolCallID = msg.ToolCallID
			} else if ret.ToolCallID != msg.ToolCallID {
				return nil, fmt.Errorf("cannot concat messages with"+
					" different toolCallIDs: '%s' '%s'", ret.ToolCallID, msg.ToolCallID)
			}
		}
		if msg.ToolName != "" {
			if ret.ToolName == "" {
				ret.ToolName = msg.ToolName
			} else if ret.ToolName != msg.ToolName {
				return nil, fmt.Errorf("cannot concat messages with"+
					" different toolNames: '%s' '%s'", ret.ToolCallID, msg.ToolCallID)
			}
		}

		if msg.Content != "" {
			contents = append(contents, msg.Content)
			contentLen += len(msg.Content)
		}
		if msg.ReasoningContent != "" {
			reasoningContents = append(reasoningContents, msg.ReasoningContent)
			reasoningContentLen += len(msg.ReasoningContent)
		}

		if len(msg.ToolCalls) > 0 {
			toolCalls = append(toolCalls, msg.ToolCalls...)
		}

		if len(msg.Extra) > 0 {
			extraList = append(extraList, msg.Extra)
		}

		// The 'MultiContent' field is deprecated but is kept for backward compatibility.
		if len(msg.MultiContent) > 0 {
			multiContentParts = append(multiContentParts, msg.MultiContent...)
		}

		if len(msg.AssistantGenMultiContent) > 0 {
			assistantGenMultiContentParts = append(assistantGenMultiContentParts, msg.AssistantGenMultiContent...)
		}

		if msg.ResponseMeta != nil && ret.ResponseMeta == nil {
			ret.ResponseMeta = &ResponseMeta{}
		}

		if msg.ResponseMeta != nil && ret.ResponseMeta != nil {
			// keep the last FinishReason with a valid value.
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
	if len(reasoningContents) > 0 {
		var sb strings.Builder
		sb.Grow(reasoningContentLen)
		for _, rc := range reasoningContents {
			_, err := sb.WriteString(rc)
			if err != nil {
				return nil, err
			}
		}

		ret.ReasoningContent = sb.String()
	}

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
			return nil, fmt.Errorf("failed to concat message's extra: %w", err)
		}

		if len(extra) > 0 {
			ret.Extra = extra
		}
	}

	if len(multiContentParts) > 0 {
		ret.MultiContent = multiContentParts
	}

	if len(assistantGenMultiContentParts) > 0 {
		merged, err := concatAssistantMultiContent(assistantGenMultiContentParts)
		if err != nil {
			return nil, fmt.Errorf("failed to concat message's assistant multi content: %w", err)
		}
		ret.AssistantGenMultiContent = merged
	}

	return &ret, nil
}

// ConcatMessageStream 从流式读取器合并消息。
// 读取所有消息并合并为单个消息。
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

// jinjaEnvOnce 确保 jinja 环境只初始化一次。
var jinjaEnvOnce sync.Once

// jinjaEnv 自定义的 jinja 环境实例。
var jinjaEnv *gonja.Environment

// envInitErr jinja 环境初始化错误。
var envInitErr error

const (
	// jinjaInclude 禁用的 include 关键字。
	jinjaInclude = "include"
	// jinjaExtends 禁用的 extends 关键字。
	jinjaExtends = "extends"
	// jinjaImport 禁用的 import 关键字。
	jinjaImport = "import"
	// jinjaFrom 禁用的 from 关键字。
	jinjaFrom = "from"
)

// getJinjaEnv 获取自定义的 jinja 环境。
// 禁用了 include、extends、import、from 等不安全的关键字。
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
