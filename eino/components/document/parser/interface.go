package parser

import (
	"context"
	"io"

	"github.com/favbox/eino/schema"
)

// Parser 定义了文档解析器的接口。
//
// 解析器负责将输入的文档内容转换为统一的 Document 结构。
type Parser interface {
	// Parse 解析输入的文档流。
	//
	// 参数：
	//   - ctx：上下文，用于控制超时和取消
	//   - reader：文档输入流
	//   - opts：解析选项（URI、额外元数据等）
	//
	// 返回：
	//   - []*schema.Document：解析后的文档列表
	//   - error：解析错误
	//
	// 实现说明：
	//   - 支持多种文档格式（TXT、PDF、MD等）
	//   - 提取文档内容和元数据
	//   - 保持文档的原始结构信息
	Parse(ctx context.Context, reader io.Reader, opts ...Option) ([]*schema.Document, error)
}
