package parser

import (
	"context"
	"io"

	"github.com/favbox/eino/schema"
)

// TextParser 是文本解析器。
//
// 将输入的文本内容解析为单个文档，
// 适用于纯文本文件的解析。
//
// 示例：
//
// docs, err := TextParser.Parse(ctx, strings.NewReader("你好，世界"))
// fmt.Println(docs[0].Content) // "你好，世界"

// MetaKeySource 是文档来源的元数据键。
//
// 用于记录文档的来源 URI，便于后续追踪和引用。
const MetaKeySource = "_source"

type TextParser struct{}

// Parse 解析文本内容并返回文档。
//
// 处理流程：
//  1. 读取所有输入内容
//  2. 获取解析选项
//  3. 构建文档元数据（来源URI、额外元数据）
//  4. 创建文档结构
//
// 返回：
//   - 单个文档的列表
func (p TextParser) Parse(ctx context.Context, reader io.Reader, opts ...Option) ([]*schema.Document, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	opt := GetCommonOptions(&Options{}, opts...)

	meta := make(map[string]any)
	meta[MetaKeySource] = opt.URI

	for k, v := range opt.ExtraMeta {
		meta[k] = v
	}

	doc := &schema.Document{
		Content:  string(data),
		MetaData: meta,
	}

	return []*schema.Document{doc}, nil
}
