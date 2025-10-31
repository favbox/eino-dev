package document

import (
	"context"

	"github.com/favbox/eino/schema"
)

// Source 结构体定义了文档的来源信息。
//
// 用于指定要加在的文档的 URI，可以是本地文件路径或远程 URL。
//
// 示例：
//   - https://www.abc.com/docx/xxx
//   - https://www.example.com/xxx.pdf
//
// 注意：请确保 URI 可以被服务访问。
type Source struct {
	// URI 是文档的统一资源标识符。
	// 可以是本地文件路径或远程 URL。
	URI string
}

// Loader 接口定义了文档加载器的核心能力 uri->doc。
//
// 用于从各种来源（本地文件、远程 URL、数据库等）加载文档。
type Loader interface {
	// Load 从指定来源加在文档。
	//
	// 该方法根据 Source 中的 URI 加载对应的文档。
	// 并将其转换为标准格式的 schema.Document 对象。
	//
	// 参数：
	//   - ctx: 上下文信息，用于取消、超时和传递请求相关数据
	//   - src: 文档来源信息，包含要加载的文档的 URI
	//   - opts: 可选的加载配置参数，用于定制加载行为（如解析选项、编码格式等）
	// 返回：
	//   - []*schema.Document: 加载得到的文档列表
	//   - error: 加载过程中的错误（如果有）
	//
	// 支持的文档类型：
	//   - PDF、Word、Excel 等 Office 文档
	//   - 纯文本文件
	//   - Markdown 文件
	//   - 网页内容
	//   - 数据库记录等
	Load(ctx context.Context, src Source, opts ...LoaderOptions) ([]*schema.Document, error)
}

// Transformer 接口定义了文档转换器的核心能力。
//
// 用于对已加载的文档进行各种转换处理，如分割、过滤、清洗等。
type Transformer interface {
	// Transform 对文档列表进行转换处理。
	//
	// 该方法接收文档列表，执行各种转换操作。
	// 返回处理后的文档列表。
	//
	// 参数：
	//   - ctx: 上下文信息，用于取消、超时和传递请求相关数据
	//   - src: 要转换的源文档列表
	//   - opts: 可选的转换配置参数，用于指定转换类型和参数（如分割策略、过滤条件等）
	//
	// 返回：
	//   - []*schema.Document: 转换后的文档列表
	//   - error: 转换过程中的错误（如果有）
	//
	// 常见转换操作：
	// 	- 文档分割：将长文档分割为较小的片段
	// 	- 内容过滤：移除无关的内容或低质量文本
	// 	- 格式转换：统一文档格式和结构
	// 	- 元数据提取：从文档中提取关键信息
	// 	- 内容清晰：去除特殊字符、HTML 标签等
	Transform(ctx context.Context, docs []*schema.Document, opts ...TransformerOption) ([]*schema.Document, error)
}
