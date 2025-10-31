package indexer

import (
	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/schema"
)

// CallbackInput 定义了索引器回调的输入参数。
//
// 在索引器的 OnStart 回调中使用，包含待索引的文档和扩展数据。
type CallbackInput struct {
	// Docs 是待索引的文档列表。
	//
	// 来源：
	//   - 转换器输出的文档
	//   - 预处理后的知识库条目
	//   - 用户上传的文档
	//
	// 包含信息：
	//   - 文档内容（文本或结构化数据）
	//   - 向量（如果已生成）
	//   - 元数据（标题、标签等）
	//
	// 处理流程：
	//   - 验证文档格式
	//   - 生成或获取向量
	//   - 存储到索引系统
	Docs []*schema.Document

	// Extra 是索引操作的额外参数。
	//
	// 常见用途：
	//   - 索引库名称（index_name）
	//   - 批量标识（batch_id）
	//   - 用户上下文（user_id）
	//
	// 示例键值：
	//   - namespace：命名空间
	//   - overwrite：是否覆盖已存在
	//   - async：是否异步索引
	Extra map[string]any
}

// CallbackOutput 定义了索引器回调的输出结果。
//
// 在索引器的 OnEnd 回调中使用，包含索引结果的ID和统计信息。
type CallbackOutput struct {
	// IDs 是索引成功的文档ID列表。
	//
	// 特点：
	//   - 与输入文档一一对应
	//   - 唯一标识索引系统中的文档
	//   - 用于后续检索和删除操作
	//
	// 格式：
	//   - 字符串数组
	//   - 每个ID唯一标识一个文档
	//   - 可包含索引库前缀
	//
	// 用途：
	//   - 存储到向量数据库
	//   - 记录索引状态
	//   - 后续检索和管理
	IDs []string

	// Extra 是索引结果的统计信息。
	//
	// 常见指标：
	//   - index_time：索引耗时
	//   - doc_count：索引文档数
	//   - success_count：成功数量
	//   - failed_count：失败数量
	//
	// 性能数据：
	//   - 索引吞吐量（文档/秒）
	//   - 索引成功率（百分比）
	//   - 平均索引延迟
	Extra map[string]any
}

// ConvCallbackInput 将通用回调输入转换为索引器特定的回调输入。
//
// 转换逻辑：
//   - *CallbackInput：直接返回（组件内触发）
//   - []*schema.Document：包装为 CallbackInput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 文档处理流水线中的索引节点
//   - 转换器 → 索引器的数据流
//
// 示例：
//
//	// 组件内
//	input := &CallbackInput{Docs: docs}
//	converted := ConvCallbackInput(input)
//
//	// 图形节点
//	converted := ConvCallbackInput(docs)
//	// 返回新的 CallbackInput，Docs 字段被设置
func ConvCallbackInput(src callbacks.CallbackInput) *CallbackInput {
	switch t := src.(type) {
	case *CallbackInput:
		return t
	case []*schema.Document:
		return &CallbackInput{
			Docs: t,
		}
	default:
		return nil
	}
}

// ConvCallbackOutput 将通用回调输出转换为索引器特定的回调输出。
//
// 转换逻辑：
//   - *CallbackOutput：直接返回（组件内触发）
//   - []string：包装为 CallbackOutput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 索引结果存储到数据库
//   - 索引ID的追踪和管理
//   - 流水线中的下游处理
//
// 设计特点：
//   - ID 列表是核心输出
//   - 简单直接的映射关系
//   - 支持批量索引操作
//
// 示例：
//
//	// 组件内
//	output := &CallbackOutput{IDs: ids}
//	converted := ConvCallbackOutput(output)
//
//	// 图形节点
//	converted := ConvCallbackOutput(ids)
//	// 返回新的 CallbackOutput，IDs 字段被设置
func ConvCallbackOutput(src callbacks.CallbackOutput) *CallbackOutput {
	switch t := src.(type) {
	case *CallbackOutput:
		return t
	case []string:
		return &CallbackOutput{
			IDs: t,
		}
	default:
		return nil
	}
}
