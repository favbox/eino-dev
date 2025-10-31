package parser

import (
	"context"
	"errors"
	"io"
	"path/filepath"

	"github.com/favbox/eino/schema"
)

// ExtParserConfig 定义了扩展解析器的配置。
type ExtParserConfig struct {
	// Parsers 是扩展名到解析器的映射。
	//
	// 格式：扩展名 → 解析器实例
	// 示例：
	//   map[string]Parser{
	//       ".pdf": &PDFParser{},
	//       ".md": &MarkdownParser{},
	//   }
	//
	// 使用场景：
	//   - PDF文件解析
	//   - Markdown文档解析
	//   - Word文档解析
	Parsers map[string]Parser

	// FallbackParser 是默认解析器。
	//
	// 当找不到匹配的解析器时使用。
	// 如果未设置，默认为 TextParser。
	//
	// 用途：
	//   - 处理未知格式的文件
	//   - 提供通用的文本解析
	FallbackParser Parser
}

// ExtParser 是扩展名解析器。
//
// 根据文件扩展名自动选择合适的解析器。
// 支持注册自定义解析器。
//
// 使用说明：
//  1. 通过文件扩展名匹配解析器
//  2. 必须使用 WithURI 传入文件URI
//  3. URI需要通过 filepath.Ext 解析扩展名
//
// 示例：
//
//	pdf, _ := os.Open("./testdata/test.pdf")
//	docs, err := ExtParser.Parse(ctx, pdf, parser.WithURI("./testdata/test.pdf"))
type ExtParser struct {
	// parsers 是扩展名到解析器的映射。
	parsers map[string]Parser

	// fallbackParser 是默认解析器。
	fallbackParser Parser
}

// NewExtParser 创建新的扩展解析器实例。
//
// 参数：
//   - conf：解析器配置，可选
//     如果为 nil 或字段为空，使用默认值
//
// 返回：
//   - *ExtParser：解析器实例
//   - error：创建错误
//
// 默认行为：
//   - FallbackParser：默认为 TextParser
//   - Parsers：初始化为空映射
func NewExtParser(ctx context.Context, conf *ExtParserConfig) (*ExtParser, error) {
	if conf == nil {
		conf = &ExtParserConfig{}
	}

	p := &ExtParser{
		parsers:        conf.Parsers,
		fallbackParser: conf.FallbackParser,
	}

	if p.fallbackParser == nil {
		p.fallbackParser = TextParser{}
	}

	if p.parsers == nil {
		p.parsers = make(map[string]Parser)
	}

	return p, nil
}

// Parse 解析文档并返回文档列表。
//
// 处理流程：
//  1. 获取解析选项
//  2. 从 URI 提取文件扩展名
//  3. 查找匹配的解析器
//  4. 如果未找到，使用默认解析器
//  5. 调用解析器进行解析
//  6. 添加额外的元数据到文档
//
// 返回：
//   - []*schema.Document：解析后的文档列表
//   - error：解析错误
//
// 错误情况：
//   - 未找到解析器且无默认解析器
//   - 下游解析器返回错误
func (p *ExtParser) Parse(ctx context.Context, reader io.Reader, opts ...Option) ([]*schema.Document, error) {
	opt := GetCommonOptions(&Options{}, opts...)

	ext := filepath.Ext(opt.URI)

	parser, ok := p.parsers[ext]

	if !ok {
		parser = p.fallbackParser
	}

	if parser == nil {
		return nil, errors.New("no parser found for extension " + ext)
	}

	docs, err := parser.Parse(ctx, reader, opts...)
	if err != nil {
		return nil, err
	}

	for _, doc := range docs {
		if doc == nil {
			continue
		}

		if doc.MetaData == nil {
			doc.MetaData = make(map[string]any)
		}

		for k, v := range opt.ExtraMeta {
			doc.MetaData[k] = v
		}
	}

	return docs, nil
}

// GetParsers 返回已注册解析器的副本。
//
// 返回值：
//   - map[string]Parser：扩展名到解析器的映射副本
//
// 特点：
//   - 返回副本，修改不会影响原始解析器
//   - 线程安全
//   - 用于查看或调试注册的解析器
func (p *ExtParser) GetParsers() map[string]Parser {
	res := make(map[string]Parser, len(p.parsers))
	for k, v := range p.parsers {
		res[k] = v
	}

	return res
}
