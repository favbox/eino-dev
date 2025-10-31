package document

import (
	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/schema"
)

// TransformerCallbackInput 定义了文档转换器回调的输入参数。
//
// 在转换器的 OnStart 回调中使用，包含待转换的文档和扩展数据。
//
// 设计理念：
// 转换器负责将输入文档转换为特定格式（如分块、向量化、过滤等）。
// CallbackInput 捕获转换所需的核心输入：文档列表和转换选项。
type TransformerCallbackInput struct {
	// Input 是待转换的文档列表。
	//
	// 来源：
	//   - 加载器输出的文档
	//   - 上游转换器的处理结果
	//   - 外部传入的文档集合
	//
	// 常见转换操作：
	//   - 文档分块（按长度、语义等）
	//   - 文本清洗（去除格式、特殊字符）
	//   - 过滤筛选（基于元数据或内容）
	//   - 格式转换（HTML → 纯文本）
	Input []*schema.Document

	// Extra 是转换相关的选项和元数据。
	//
	// 常见用途：
	//   - 转换参数（分块大小、重叠长度等）
	//   - 过滤规则（包含/排除条件）
	//   - 输出格式（纯文本/结构化）
	//
	// 示例键值：
	//   - chunk_size：分块大小（字符数）
	//   - overlap：分块重叠长度
	//   - filter_meta：过滤元数据条件
	//   - output_format：输出格式类型
	Extra map[string]any
}

// TransformerCallbackOutput 定义了文档转换器回调的输出结果。
//
// 在转换器的 OnEnd 回调中使用，包含转换后的文档和统计信息。
type TransformerCallbackOutput struct {
	// Output 是转换后的文档列表。
	//
	// 特点：
	//   - 数量可能与输入不同（如分块操作）
	//   - 内容可能发生变换（如文本清洗）
	//   - 元数据可能被更新或添加
	//
	// 典型场景：
	//   - 分块后的小文档集合
	//   - 清洗后的纯文本内容
	//   - 过滤后符合条件的文档
	//   - 格式转换后的文档
	//
	// 后续处理：
	//   - 传递给索引器建立索引
	//   - 直接用于向量化和检索
	//   - 存储到向量数据库
	Output []*schema.Document

	// Extra 是转换结果的统计信息。
	//
	// 关键指标：
	//   - transform_time：转换耗时
	//   - input_count：输入文档数
	//   - output_count：输出文档数
	//   - dropped_count：丢弃的文档数
	//
	// 质量指标：
	//   - avg_chunk_size：平均分块大小
	//   - total_tokens：文档总 token 数
	//   - coverage：转换覆盖率（百分比）
	Extra map[string]any
}

// ConvTransformerCallbackInput 将通用回调输入转换为转换器特定的回调输入。
//
// 转换逻辑：
//   - *TransformerCallbackInput：直接返回（组件内触发）
//   - []*schema.Document：包装为 TransformerCallbackInput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 图形编排中的文档转换流水线
//   - 加载器 → 转换器 → 索引器的数据流
//
// 示例：
//
//	// 组件内
//	input := &TransformerCallbackInput{Docs: docs}
//	converted := ConvTransformerCallbackInput(input)
//
//	// 图形节点
//	converted := ConvTransformerCallbackInput(docs)
//	// 返回新的 TransformerCallbackInput，Input 字段被设置
func ConvTransformerCallbackInput(src callbacks.CallbackInput) *TransformerCallbackInput {
	switch t := src.(type) {
	case *TransformerCallbackInput:
		return t
	case []*schema.Document:
		return &TransformerCallbackInput{
			Input: t,
		}
	default:
		return nil
	}
}

// ConvTransformerCallbackOutput 将通用回调输出转换为转换器特定的回调输出。
//
// 转换逻辑：
//   - *TransformerCallbackOutput：直接返回（组件内触发）
//   - []*schema.Document：包装为 TransformerCallbackOutput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 文档转换流水线中的下游处理
//   - 转换结果传递给索引器或向量数据库
//
// 设计特点：
//   - 文档列表是核心数据结构
//   - 转换过程保持引用完整性
//   - 支持批量文档处理
//
// 示例：
//
//	// 组件内
//	output := &TransformerCallbackOutput{Docs: docs}
//	converted := ConvTransformerCallbackOutput(output)
//
//	// 图形节点
//	converted := ConvTransformerCallbackOutput(docs)
//	// 返回新的 TransformerCallbackOutput，Output 字段被设置
func ConvTransformerCallbackOutput(src callbacks.CallbackOutput) *TransformerCallbackOutput {
	switch t := src.(type) {
	case *TransformerCallbackOutput:
		return t
	case []*schema.Document:
		return &TransformerCallbackOutput{
			Output: t,
		}
	default:
		return nil
	}
}
