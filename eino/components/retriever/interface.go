package retriever

import (
	"context"

	"github.com/favbox/eino/schema"
)

// Retriever 接口定义了检索器的核心能力。
//
// 用于从数据源中检索相关文档，是 RAG（检索增强生成）系统的核心组件之一。
type Retriever interface {
	// Retrieve 根据查询条件从数据源检索相关文档。
	//
	// 该方法接收一个查询字符串，并在预构建的索引或数据库中搜索相关文档，
	// 返回按相关性排序的文档列表。
	//
	// 参数：
	//   - ctx: 上下文信息，用于取消、超时和传递请求相关数据
	//   - query: 查询字符串，用于在数据源中搜索相关内容
	//   - opts: 可选的配置参数，用于定制检索行为（如返回文档数量、相似度阈值等）
	//
	// 返回：
	//   - []*schema.Document: 检索到的相关文档列表，按相关性排序
	//   - error: 检索过程中的错误（如果有）
	//
	// 使用示例：
	//
	// 	// 直接使用检索器
	// 	retriever, err := redis.NewRetriever(ctx, &redis.RetrieverConfig{})
	// 	if err == nil {...}
	// 	docs, err := retriever.Retrieve(ctx, "query")
	// 	docs, err := retriever.Retrieve(ctx, "query", retriever.WithTopK(3))
	//
	// 	// 在图编排中使用
	// 	graph := compose.NewGraph[inputType, outputType](compose.RunTpeDAG)
	// 	graph.AddRetrieverNode("retriever_node_key", retriever)
	Retrieve(ctx context.Context, query string, opts ...Option) (*schema.StreamReader[string], error)
}
