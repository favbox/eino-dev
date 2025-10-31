package embedding

import "github.com/favbox/eino/callbacks"

// TokenUsage 记录嵌入模型的令牌使用统计。
//
// 用于跟踪嵌入生成的成本和性能指标。
type TokenUsage struct {
	// PromptTokens 是输入文本的令牌数。
	//
	// 计算方式：
	//   - 按文本长度和模型分词规则计算
	//   - 包括所有待嵌入的文本
	//
	// 应用场景：
	//   - 计算嵌入生成成本
	//   - 监控 API 调用配额
	//   - 优化批量处理大小
	PromptTokens int

	// CompletionTokens 是嵌入输出的令牌数。
	//
	// 通常为 0（嵌入模型不生成文本），
	// 保留字段用于统一接口。
	//
	// 用途：
	//   - 与其他组件接口保持一致
	//   - 未来可能的扩展功能
	CompletionTokens int

	// TotalTokens 是总令牌数。
	//
	// 计算：TotalTokens = PromptTokens + CompletionTokens
	//
	// 使用场景：
	//   - 统一的成本计算接口
	//   - 配额管理
	//   - 性能监控
	TotalTokens int
}

// Config 定义了嵌入模型的配置信息。
type Config struct {
	// Model 是嵌入模型的名称。
	//
	// 示例：
	//   - text-embedding-ada-002 (OpenAI)
	//   - sentence-transformers/all-MiniLM-L6-v2
	//   - bge-large-zh (BAAI)
	//
	// 常见类型：
	//   - 通用模型：适用于多种文本
	//   - 中文模型：针对中文优化
	//   - 领域模型：针对特定领域训练
	Model string

	// EncodingFormat 是向量编码格式。
	//
	// 常见值：
	//   - float：32位浮点数（默认）
	//   - base64：Base64编码的二进制
	//
	// 选择建议：
	//   - float：通用场景，性能好
	//   - base64：需要字符串传输时使用
	//
	// 性能考虑：
	//   - float 向量运算效率高
	//   - base64 占用空间大 33%
	EncodingFormat string
}

// ComponentExtra 是嵌入组件的额外信息容器。
//
// 用于封装配置和统计信息，提供统一的访问接口。
type ComponentExtra struct {
	// Config 是嵌入模型配置。
	//
	// 包含：
	//   - 模型名称
	//   - 编码格式
	//
	// 用途：
	//   - 记录实际使用的配置
	//   - 支持配置审计
	//   - 便于问题排查
	Config *Config

	// TokenUsage 是令牌使用统计。
	//
	// 包含：
	//   - 输入令牌数
	//   - 输出令牌数（通常为 0）
	//   - 总令牌数
	//
	// 用途：
	//   - 成本核算
	//   - 性能监控
	//   - 配额管理
	TokenUsage *TokenUsage
}

// CallbackInput 定义了嵌入回调的输入参数。
//
// 在嵌入组件的 OnStart 回调中使用，包含待嵌入文本和配置信息。
type CallbackInput struct {
	// Texts 是待嵌入的文本列表。
	//
	// 输入格式：
	//   - 字符串数组
	//   - 每项为独立的文本片段
	//   - 可批量处理多个文本
	//
	// 常见来源：
	//   - 文档分块后的文本
	//   - 用户查询内容
	//   - 知识库条目
	//
	// 限制：
	//   - 单个文本长度受限
	//   - 批量大小受限
	//   - 总 token 数受限
	Texts []string

	// Config 是嵌入模型配置。
	//
	// 包含：
	//   - 模型名称
	//   - 编码格式
	//
	// 作用：
	//   - 选择嵌入模型
	//   - 确定输出格式
	//   - 控制向量维度
	Config *Config

	// Extra 是额外的回调信息。
	//
	// 常见用途：
	//   - 批处理ID（batch_id）
	//   - 用户标识（user_id）
	//   - 请求来源（source）
	//
	// 性能指标：
	//   - 文本总数（text_count）
	//   - 平均长度（avg_length）
	//   - 总字符数（total_chars）
	Extra map[string]any
}

// CallbackOutput 定义了嵌入回调的输出结果。
//
// 在嵌入组件的 OnEnd 回调中使用，包含生成的向量和统计信息。
type CallbackOutput struct {
	// Embeddings 是生成的向量列表。
	//
	// 格式：
	//   - 二维数组：[][](维度)
	//   - 与输入文本一一对应
	//   - 向量元素为浮点数
	//
	// 维度：
	//   - 由模型决定（如 512、768、1536）
	//   - 所有向量维度相同
	//
	// 用途：
	//   - 存储到向量数据库
	//   - 计算相似度
	//   - 检索和推荐
	Embeddings [][]float64

	// Config 是实际使用的嵌入配置。
	//
	// 可能与输入配置不同（如应用默认值）。
	//
	// 用途：
	//   - 记录实际生效的配置
	//   - 配置审计
	//   - 问题排查
	Config *Config

	// TokenUsage 是令牌使用统计。
	//
	// 用途：
	//   - 计算嵌入成本
	//   - 性能监控
	//   - 配额管理
	TokenUsage *TokenUsage

	// Extra 是额外的输出信息。
	//
	// 常见指标：
	//   - embedding_time：嵌入耗时
	//   - input_count：输入文本数
	//   - output_count：输出向量数
	//   - vector_dim：向量维度
	//
	// 性能数据：
	//   - 吞吐量（向量/秒）
	//   - 平均延迟
	//   - 错误数量
	Extra map[string]any
}

// ConvCallbackInput 将通用回调输入转换为嵌入特定的回调输入。
//
// 转换逻辑：
//   - *CallbackInput：直接返回（组件内触发）
//   - []string：包装为 CallbackInput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 图形编排中的文本向量化流水线
//   - 文档 → 嵌入 → 存储的数据流
//
// 示例：
//
//	// 组件内
//	input := &CallbackInput{Texts: texts}
//	converted := ConvCallbackInput(input)
//
//	// 图形节点
//	converted := ConvCallbackInput([]string{"text1", "text2"})
//	// 返回新的 CallbackInput，Texts 字段被设置
func ConvCallbackInput(src callbacks.CallbackInput) *CallbackInput {
	switch t := src.(type) {
	case *CallbackInput:
		return t
	case []string:
		return &CallbackInput{
			Texts: t,
		}
	default:
		return nil
	}
}

// ConvCallbackOutput 将通用回调输出转换为嵌入特定的回调输出。
//
// 转换逻辑：
//   - *CallbackOutput：直接返回（组件内触发）
//   - [][]float64：包装为 CallbackOutput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 嵌入结果存储到向量数据库
//   - 相似度计算和检索
//   - 向量流水线中的下游处理
//
// 设计特点：
//   - 直接返回保持数据完整性
//   - 轻量级包装支持灵活集成
//   - 向量是核心数据结构
//
// 示例：
//
//	// 组件内
//	output := &CallbackOutput{Embeddings: vectors}
//	converted := ConvCallbackOutput(output)
//
//	// 图形节点
//	converted := ConvCallbackOutput(vectors)
//	// 返回新的 CallbackOutput，Embeddings 字段被设置
func ConvCallbackOutput(src callbacks.CallbackOutput) *CallbackOutput {
	switch t := src.(type) {
	case *CallbackOutput:
		return t
	case [][]float64:
		return &CallbackOutput{
			Embeddings: t,
		}
	default:
		return nil
	}
}
