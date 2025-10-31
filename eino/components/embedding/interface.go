package embedding

import (
	"context"
)

// Embedder 接口定义了嵌入模型的核心能力。
//
// 用于将文本转换为向量标识，为后续的相似度计算、检索和匹配提供基础。
type Embedder interface {
	// EmbedStrings 将文本列表转换为对应的向量表示。
	//
	// 这是嵌入模型的核心功能，将自然语言文本映射到高维向量空间，
	// 使得语义相似的文本在向量空间中距离较近。
	//
	// 参数：
	//   - ctx: 上下文信息，用于取消、超时和传递请求相关数据
	//   - texts: 要转换的文本列表，每个文本都会被转换为向量
	//   - opts: 可选的配置参数，用于定制嵌入模型的参数和行为
	//
	// 返回：
	//   - [][]float64: 文本列表对应的向量列表，每个向量是一个 float64 数组
	//   - error: 嵌入过程中的错误（如果有）
	//
	// 使用场景：
	// 	- 文档检索和相似度匹配
	// 	- 文本聚类和分类
	// 	- 推荐系统和语义搜索
	EmbedStrings(ctx context.Context, texts []string, opts ...Option) ([][]float64, error)
}
