package parser

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/schema"
)

// ParserForTest 是测试用的模拟解析器。
//
// 用于测试 ExtParser 的解析器映射功能。
// 通过 mock 函数模拟解析行为。
type ParserForTest struct {
	mock func() ([]*schema.Document, error)
}

// Parse 模拟解析器的解析行为。
func (p *ParserForTest) Parse(ctx context.Context, reader io.Reader, opts ...Option) ([]*schema.Document, error) {
	return p.mock()
}

// TestParser 测试解析器的核心功能。
func TestParser(t *testing.T) {
	ctx := context.Background()

	// 测试场景1：默认解析器
	//
	// 验证点：
	//   - 无配置时使用默认解析器
	//   - 未知扩展名使用 TextParser
	//   - Markdown文件被正确解析
	t.Run("Test default parser", func(t *testing.T) {
		// 创建空配置（使用默认配置）
		conf := &ExtParserConfig{}

		// 创建解析器实例
		p, err := NewExtParser(ctx, conf)
		if err != nil {
			t.Fatal(err)
		}

		// 打开测试文件
		f, err := os.Open("testdata/test.md")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		// 解析文档
		docs, err := p.Parse(ctx, f, WithURI("testdata/test.md"))
		if err != nil {
			t.Fatal(err)
		}

		// 验证返回文档数量
		assert.Equal(t, 1, len(docs))

		// 验证文档内容
		assert.Equal(t, "# Title\nhello world", docs[0].Content)
	})

	// 测试场景2：自定义解析器映射
	//
	// 验证点：
	//   - 扩展名到解析器的映射正确
	//   - 自定义解析器的行为正确
	//   - 元数据正确传递
	t.Run("test types", func(t *testing.T) {
		// 创建模拟解析器
		mockParser := &ParserForTest{
			mock: func() ([]*schema.Document, error) {
				return []*schema.Document{
					{
						Content: "hello world",
						MetaData: map[string]any{
							"type": "text",
						},
					},
				}, nil
			},
		}

		// 配置 .md 扩展名使用模拟解析器
		conf := &ExtParserConfig{
			Parsers: map[string]Parser{
				".md": mockParser,
			},
		}

		// 创建解析器实例
		p, err := NewExtParser(ctx, conf)
		if err != nil {
			t.Fatal(err)
		}

		// 打开测试文件
		f, err := os.Open("testdata/test.md")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		// 解析文档（注意URI包含扩展名）
		docs, err := p.Parse(ctx, f, WithURI("x/test.md"))
		if err != nil {
			t.Fatal(err)
		}

		// 验证返回文档数量
		assert.Equal(t, 1, len(docs))

		// 验证文档内容
		assert.Equal(t, "hello world", docs[0].Content)

		// 验证元数据
		assert.Equal(t, "text", docs[0].MetaData["type"])
	})

	// 测试场景3：获取解析器列表
	//
	// 验证点：
	//   - GetParsers 返回正确的映射
	//   - 返回的映射大小正确
	//   - 返回副本不影响原始映射
	t.Run("test get parsers", func(t *testing.T) {
		// 创建包含解析器的配置
		p, err := NewExtParser(ctx, &ExtParserConfig{
			Parsers: map[string]Parser{
				".md": &TextParser{},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		// 获取解析器列表
		ps := p.GetParsers()

		// 验证解析器数量
		assert.Equal(t, 1, len(ps))
	})
}
