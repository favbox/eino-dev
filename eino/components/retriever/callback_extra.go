package retriever

import (
	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/schema"
)

// CallbackInput 定义了检索器回调的输入参数。
//
// 在检索器的 OnStart 回调中使用，包含查询条件和检索参数。
type CallbackInput struct {
	// Query 是检索查询字符串。
	//
	// 来源：
	//   - 用户输入的搜索问题
	//   - 聊天上下文中的关键信息
	//   - 文本匹配或语义检索的查询
	//
	// 处理方式：
	//   - 向量化后进行相似度匹配
	//   - 或基于关键词进行全文检索
	Query string

	// TopK 是返回文档的数量上限。
	//
	// 作用：
	//   - 控制返回结果的数量
	//   - 平衡相关性和性能
	//
	// 建议值：
	//   - 简单查询：3-5
	//   - 复杂查询：5-10
	//   - 研究场景：10-20
	TopK int

	// Filter 是检索时的过滤条件。
	//
	// 格式：通常是 SQL 风格的查询条件
	// 示例：
	//   - "category = 'tech'"
	//   - "score > 0.8"
	//   - "tags @> '[\"important\"]'"
	//
	// 用途：
	//   - 按元数据筛选文档
	//   - 业务规则过滤
	//   - 权限控制
	Filter string

	// ScoreThreshold 是相似度分数阈值。
	//
	// 范围：0.0 - 1.0
	//
	// 示例：
	//   - 0.5：只返回高相似度文档
	//   - 0.8：严格过滤低质量结果
	//   - nil：使用默认值
	//
	// 作用：
	//   - 过滤低相关性文档
	//   - 保障检索质量
	ScoreThreshold *float64

	// Extra 是额外的检索参数。
	//
	// 常见用途：
	//   - 检索模式（向量/混合）
	//   - 索引标识（index_name）
	//   - 用户上下文（user_id）
	//
	// 示例键值：
	//   - search_type：检索类型
	//   - index_id：索引ID
	//   - namespace：命名空间
	Extra map[string]any
}

// CallbackOutput 定义了检索器回调的输出结果。
//
// 在检索器的 OnEnd 回调中使用，包含检索到的文档和相关信息。
type CallbackOutput struct {
	// Docs 是检索到的文档列表。
	//
	// 特点：
	//   - 按相似度分数降序排列
	//   - 数量不超过 TopK
	//   - 分数高于 ScoreThreshold
	//
	// 包含信息：
	//   - 文档内容（文本）
	//   - 元数据（标题、作者、URL等）
	//   - 相似度分数（score）
	//   - 文档ID（可唯一标识）
	//
	// 后续处理：
	//   - 直接返回给用户
	//   - 传递给模型生成回答
	//   - 用于引用和溯源
	Docs []*schema.Document

	// Extra 是检索结果的统计信息。
	//
	// 常见指标：
	//   - retrieve_time：检索耗时
	//   - doc_count：返回文档数
	//   - avg_score：平均相似度
	//   - max_score：最高相似度
	//
	// 性能数据：
	//   - 索引命中率
	//   - 缓存命中率
	//   - 查询复杂度
	Extra map[string]any
}

// ConvCallbackInput 将通用回调输入转换为检索器特定的回调输入。
//
// 转换逻辑：
//   - *CallbackInput：直接返回（组件内触发）
//   - string：包装为 CallbackInput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 图形编排中的信息检索节点
//   - 用户查询到检索器的直接传递
//
// 示例：
//
//	// 组件内
//	input := &CallbackInput{Query: "什么是AI"}
//	converted := ConvCallbackInput(input)
//
//	// 图形节点
//	converted := ConvCallbackInput("机器学习基础")
//	// 返回新的 CallbackInput，Query 字段被设置
func ConvCallbackInput(src callbacks.CallbackInput) *CallbackInput {
	switch t := src.(type) {
	case *CallbackInput:
		return t
	case string:
		return &CallbackInput{
			Query: t,
		}
	default:
		return nil
	}
}

// ConvCallbackOutput 将通用回调输出转换为检索器特定的回调输出。
//
// 转换逻辑：
//   - *CallbackOutput：直接返回（组件内触发）
//   - []*schema.Document：包装为 CallbackOutput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 检索结果传递给下游组件
//   - 文档列表到模型的传递
//   - RAG 架构中的信息流
//
// 设计特点：
//   - 文档列表是核心数据结构
//   - 保持检索结果的完整性
//   - 支持灵活的组件组合
//
// 示例：
//
//	// 组件内
//	output := &CallbackOutput{Docs: docs}
//	converted := ConvCallbackOutput(output)
//
//	// 图形节点
//	converted := ConvCallbackOutput(docs)
//	// 返回新的 CallbackOutput，Docs 字段被设置
func ConvCallbackOutput(src callbacks.CallbackOutput) *CallbackOutput {
	switch t := src.(type) {
	case *CallbackOutput:
		return t
	case []*schema.Document:
		return &CallbackOutput{
			Docs: t,
		}
	default:
		return nil
	}
}
