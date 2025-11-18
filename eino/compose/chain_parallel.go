/*
 * chain_parallel.go - Chain 并行执行实现，支持多个节点同时运行
 *
 * 核心组件：
 *   - Parallel: 并行执行容器，管理多个同时运行的节点
 *   - outputKey: 每个并行节点的输出标识，用于结果映射
 *
 * 设计特点：
 *   - 所有节点接收相同的输入并行执行
 *   - 每个节点必须指定唯一的 outputKey
 *   - 通过 outputKey 收集和映射各节点的执行结果
 *
 * 使用场景：
 *   - 多模型并行推理（如同时调用多个 LLM）
 *   - 并行数据处理（如同时执行多个转换操作）
 *   - 扇出模式（Fan-out）的执行流程
 */

package compose

import (
	"fmt"

	"github.com/favbox/eino/components/document"
	"github.com/favbox/eino/components/embedding"
	"github.com/favbox/eino/components/indexer"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/components/retriever"
)

// NewParallel 创建新的 Parallel 实例。
// 用于在 Chain 中并行运行多个节点
func NewParallel() *Parallel {
	return &Parallel{
		outputKeys: make(map[string]bool),
	}
}

// Parallel 并行运行多个节点。
//
// 使用 NewParallel() 创建新的 Parallel 实例。
//
// 示例：
//
//	parallel := NewParallel()
//	parallel.AddChatModel("output_key01", chat01)
//	parallel.AddChatModel("output_key02", chat02)
//
//	chain := NewChain[any,any]()
//	chain.AppendParallel(parallel)
type Parallel struct {
	nodes      []nodeOptionsPair
	outputKeys map[string]bool
	err        error
}

// AddChatModel 向 Parallel 添加 ChatModel 节点。
//
// 示例：
//
//	chatModel01, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
//		Model: "gpt-4o",
//	})
//
//	chatModel02, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
//		Model: "gpt-4o",
//	})
//
//	p.AddChatModel("output_key01", chatModel01)
//	p.AddChatModel("output_key02", chatModel02)
func (p *Parallel) AddChatModel(outputKey string, node model.BaseChatModel, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toChatModelNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddChatTemplate 向 Parallel 添加 ChatTemplate 节点。
//
// 示例：
//
//	chatTemplate01, err := prompt.FromMessages(schema.FString, &schema.Message{
//		Role:    schema.System,
//		Content: "You are acting as a {role}.",
//	})
//
//	p.AddChatTemplate("output_key01", chatTemplate01)
func (p *Parallel) AddChatTemplate(outputKey string, node prompt.ChatTemplate, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toChatTemplateNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddToolsNode 向 Parallel 添加 ToolsNode。
//
// 示例：
//
//	toolsNode, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{
//		Tools: []tool.BaseTool{...},
//	})
//
//	p.AddToolsNode("output_key01", toolsNode)
func (p *Parallel) AddToolsNode(outputKey string, node *ToolsNode, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toToolsNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddLambda 向 Parallel 添加 Lambda 节点。
//
// 示例：
//
//	lambdaFunc := func(ctx context.Context, input *schema.Message) ([]*schema.Message, error) {
//		return []*schema.Message{input}, nil
//	}
//
//	p.AddLambda("output_key01", compose.InvokeLambda(lambdaFunc))
func (p *Parallel) AddLambda(outputKey string, node *Lambda, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toLambdaNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddEmbedding 向 Parallel 添加 Embedding 节点。
//
// 示例：
//
//	embeddingNode, err := openai.NewEmbedder(ctx, &openai.EmbeddingConfig{
//		Model: "text-embedding-3-small",
//	})
//
//	p.AddEmbedding("output_key01", embeddingNode)
func (p *Parallel) AddEmbedding(outputKey string, node embedding.Embedder, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toEmbeddingNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddRetriever 向 Parallel 添加 Retriever 节点。
//
// 示例：
//
//	retriever, err := vikingdb.NewRetriever(ctx, &vikingdb.RetrieverConfig{})
//
//	p.AddRetriever("output_key01", retriever)
func (p *Parallel) AddRetriever(outputKey string, node retriever.Retriever, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toRetrieverNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddLoader 向 Parallel 添加 Loader 节点。
//
// 示例：
//
//	loader, err := file.NewLoader(ctx, &file.LoaderConfig{})
//
//	p.AddLoader("output_key01", loader)
func (p *Parallel) AddLoader(outputKey string, node document.Loader, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toLoaderNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddIndexer 向 Parallel 添加 Indexer 节点。
//
// 示例：
//
//	indexer, err := volc_vikingdb.NewIndexer(ctx, &volc_vikingdb.IndexerConfig{
//		Collection: "my_collection",
//	})
//
//	p.AddIndexer("output_key01", indexer)
func (p *Parallel) AddIndexer(outputKey string, node indexer.Indexer, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toIndexerNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddDocumentTransformer 向 Parallel 添加 Document Transformer 节点。
//
// 示例：
//
//	markdownSplitter, err := markdown.NewHeaderSplitter(ctx, &markdown.HeaderSplitterConfig{})
//
//	p.AddDocumentTransformer("output_key01", markdownSplitter)
func (p *Parallel) AddDocumentTransformer(outputKey string, node document.Transformer, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toDocumentTransformerNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddGraph 向 Parallel 添加 Graph 节点。
// 当需要将 Graph 或 Chain 作为并行节点使用时很有用。
//
// 示例：
//
//	graph, err := compose.NewChain[any,any]()
//
//	p.AddGraph("output_key01", graph)
func (p *Parallel) AddGraph(outputKey string, node AnyGraph, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toAnyGraphNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddPassthrough 向 Parallel 添加 Passthrough 节点。
//
// 示例：
//
//	p.AddPassthrough("output_key01")
func (p *Parallel) AddPassthrough(outputKey string, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toPassthroughNode(append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

func (p *Parallel) addNode(outputKey string, node *graphNode, options *graphAddNodeOpts) *Parallel {
	if p.err != nil {
		return p
	}

	if node == nil {
		p.err = fmt.Errorf("chain parallel add node invalid, node is nil")
		return p
	}

	if p.outputKeys == nil {
		p.outputKeys = make(map[string]bool)
	}

	if _, ok := p.outputKeys[outputKey]; ok {
		p.err = fmt.Errorf("parallel add node err, duplicate output key= %s", outputKey)
		return p
	}

	if node.nodeInfo == nil {
		p.err = fmt.Errorf("chain parallel add node invalid, nodeInfo is nil")
		return p
	}

	node.nodeInfo.outputKey = outputKey
	p.nodes = append(p.nodes, nodeOptionsPair{node, options})
	p.outputKeys[outputKey] = true
	return p
}
