package document

import (
	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/schema"
)

// LoaderCallbackInput 定义了文档加载器回调的输入参数。
//
// 在加载器的 OnStart 回调中使用，包含文档源信息和扩展数据。
//
// 设计理念：
// 文档加载器负责将外部文档源转换为统一的 Document 结构。
// CallbackInput 捕获加载所需的核心信息：数据源和元数据。
type LoaderCallbackInput struct {
	// Source 是文档的来源定义。
	//
	// 可以是：
	//   - 文件路径：本地或远程文件
	//   - URL：网络资源地址
	//   - 数据流：内存中的数据
	//
	// 常见来源：
	//   - 文件：/path/to/doc.pdf
	//   - S3：s3://bucket/key
	//   - HTTP：https://example.com/doc
	//   - 向量数据库：连接字符串
	Source Source

	// Extra 是额外的加载元数据。
	//
	// 用于传递：
	//   - 加载选项（编码格式、超时等）
	//   - 认证信息（API Key、凭证等）
	//   - 业务标识（用户ID、租户ID等）
	//
	// 常见键值：
	//   - encoding：文本编码（如 utf-8）
	//   - timeout：加载超时时间
	//   - auth_type：认证方式
	//   - metadata：自定义元数据
	Extra map[string]any
}

// LoaderCallbackOutput 定义了文档加载器回调的输出结果。
//
// 在加载器的 OnEnd 回调中使用，包含加载的文档和相关信息。
type LoaderCallbackOutput struct {
	// Source 是文档的来源（与输入保持一致）。
	//
	// 用途：
	//   - 记录实际使用的源地址
	//   - 支持审计和追踪
	//   - 便于调试和问题排查
	Source Source

	// Docs 是加载的文档列表。
	//
	// 包含：
	//   - 文档内容（原始文本或结构化数据）
	//   - 元数据（标题、作者、创建时间等）
	//   - 文档ID（唯一标识符）
	//
	// 使用场景：
	//   - 直接传递给下游处理组件
	//   - 存储到向量数据库
	//   - 用于索引和检索
	Docs []*schema.Document

	// Extra 是额外的输出信息。
	//
	// 常见指标：
	//   - load_time：加载耗时（毫秒）
	//   - doc_count：文档数量
	//   - total_size：文档总大小（字节）
	//   - error_count：错误数量
	//
	// 性能数据：
	//   - 网络请求次数
	//   - 缓存命中率
	//   - 解析耗时统计
	Extra map[string]any
}

// ConvLoaderCallbackInput 将通用回调输入转换为加载器特定的回调输入。
//
// 转换逻辑：
//   - *LoaderCallbackInput：直接返回（组件内触发）
//   - Source：包装为 LoaderCallbackInput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 图形编排中的文档加载节点
//   - 组件内部的直接调用
//
// 示例：
//
//	// 组件内
//	input := &LoaderCallbackInput{Source: src}
//	converted := ConvLoaderCallbackInput(input)
//	// converted == input
//
//	// 图形节点
//	converted := ConvLoaderCallbackInput("s3://bucket/doc")
//	// 返回新的 LoaderCallbackInput，Source 字段被设置
func ConvLoaderCallbackInput(src callbacks.CallbackInput) *LoaderCallbackInput {
	switch t := src.(type) {
	case *LoaderCallbackInput:
		return t
	case Source:
		return &LoaderCallbackInput{
			Source: t,
		}
	default:
		return nil
	}
}

// ConvLoaderCallbackOutput 将通用回调输出转换为加载器特定的回调输出。
//
// 转换逻辑：
//   - *LoaderCallbackOutput：直接返回（组件内触发）
//   - []*schema.Document：包装为 LoaderCallbackOutput（图节点注入）
//   - 其他类型：返回 nil
//
// 使用场景：
//   - 图形编排中的文档处理流水线
//   - 加载器输出到下游组件的传递
//
// 设计特点：
//   - 直接返回保持数据完整性
//   - 轻量级包装支持灵活集成
//   - 零拷贝性能优化
//
// 示例：
//
//	// 组件内
//	output := &LoaderCallbackOutput{Docs: docs}
//	converted := ConvLoaderCallbackOutput(output)
//	// converted == output
//
//	// 图形节点
//	output := []*schema.Document{...}
//	converted := ConvLoaderCallbackOutput(output)
//	// 返回新的 LoaderCallbackOutput，Docs 字段被设置
func ConvLoaderCallbackOutput(src callbacks.CallbackOutput) *LoaderCallbackOutput {
	switch t := src.(type) {
	case *LoaderCallbackOutput:
		return t
	case []*schema.Document:
		return &LoaderCallbackOutput{
			Docs: t,
		}
	default:
		return nil
	}
}
