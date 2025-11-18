package compose

import (
	"context"
	"fmt"

	"github.com/favbox/eino/components/document"
	"github.com/favbox/eino/components/embedding"
	"github.com/favbox/eino/components/indexer"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/components/retriever"
	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

type nodeOptionsPair generic.Pair[*graphNode, *graphAddNodeOpts]

// ChainBranch 表示链式操作中的条件分支。
// 支持根据条件动态路由执行路径。
// ChainBranch 中的所有分支必须结束链或汇聚到链中的另一个节点
type ChainBranch struct {
	internalBranch *GraphBranch
	key2BranchNode map[string]nodeOptionsPair
	err            error
}

// NewChainMultiBranch 基于多分支条件创建新的 ChainBranch 实例。
// 接受泛型类型 T 和 GraphMultiBranchCondition 函数。
// 条件函数可返回多个目标节点的 key，允许执行路径同时进入多个分支。
//
// 示例：
//
//	condition := func(ctx context.Context, in string) (endNodes map[string]bool, err error) {
//		// 根据输入决定进入哪些分支
//		if strings.Contains(in, "both") {
//			return map[string]bool{"branch_a": true, "branch_b": true}, nil
//		}
//		return map[string]bool{"branch_a": true}, nil
//	}
//
//	cb := NewChainMultiBranch[string](condition)
//	cb.AddPassthrough("branch_a")
//	cb.AddPassthrough("branch_b")
func NewChainMultiBranch[T any](cond GraphMultiBranchCondition[T]) *ChainBranch {
	invokeCond := func(ctx context.Context, in T, opts ...any) (endNodes []string, err error) {
		ends, err := cond(ctx, in)
		if err != nil {
			return nil, err
		}
		endNodes = make([]string, 0, len(ends))
		for end := range ends {
			endNodes = append(endNodes, end)
		}
		return endNodes, nil
	}

	return &ChainBranch{
		key2BranchNode: make(map[string]nodeOptionsPair),
		internalBranch: newGraphBranch(newRunnablePacker(invokeCond, nil, nil, nil, false), nil),
	}
}

// NewStreamChainMultiBranch 基于流式多分支条件创建新的 ChainBranch 实例。
// 接受泛型类型 T 和 StreamGraphMultiBranchCondition 函数。
// 条件函数接收流式输入，可返回多个目标节点的 key，允许执行路径同时进入多个分支。
//
// 示例：
//
//	condition := func(ctx context.Context, in *schema.StreamReader[string]) (endNodes map[string]bool, err error) {
//		// 读取流的第一个块做决策
//		firstChunk, ok := in.Recv()
//		if !ok {
//			return nil, errors.New("empty stream")
//		}
//		// 根据内容决定进入哪些分支
//		branches := make(map[string]bool)
//		if strings.Contains(firstChunk, "model") {
//			branches["model_branch"] = true
//		}
//		if strings.Contains(firstChunk, "tool") {
//			branches["tool_branch"] = true
//		}
//		return branches, nil
//	}
//
//	cb := NewStreamChainMultiBranch[string](condition)
//	cb.AddChatModel("model_branch", chatModel)
//	cb.AddToolsNode("tool_branch", toolsNode)
func NewStreamChainMultiBranch[T any](cond StreamGraphMultiBranchCondition[T]) *ChainBranch {
	collectCon := func(ctx context.Context, in *schema.StreamReader[T], opts ...any) (endNodes []string, err error) {
		ends, err := cond(ctx, in)
		if err != nil {
			return nil, err
		}
		endNodes = make([]string, 0, len(ends))
		for end := range ends {
			endNodes = append(endNodes, end)
		}
		return endNodes, nil
	}

	return &ChainBranch{
		key2BranchNode: make(map[string]nodeOptionsPair),
		internalBranch: newGraphBranch(newRunnablePacker(nil, nil, collectCon, nil, false), nil),
	}
}

// NewChainBranch 基于给定条件创建新的 ChainBranch 实例。
// 接受泛型类型 T 和该类型的 GraphBranchCondition 函数。
// 返回的 ChainBranch 包含空的 key2BranchNode 映射和包装后的条件函数，
// 用于处理类型断言和错误检查。
//
// 示例：
//
//	condition := func(ctx context.Context, in string, opts ...any) (endNode string, err error) {
//		// 确定下一个节点的逻辑
//		return "some_next_node_key", nil
//	}
//
//	cb := NewChainBranch[string](condition)
//	cb.AddPassthrough("next_node_key_01", xxx) // 分支中的节点，代表分支的一条路径
//	cb.AddPassthrough("next_node_key_02", xxx) // 分支中的节点
func NewChainBranch[T any](cond GraphBranchCondition[T]) *ChainBranch {
	return NewChainMultiBranch(func(ctx context.Context, in T) (endNode map[string]bool, err error) {
		ret, err := cond(ctx, in)
		if err != nil {
			return nil, err
		}
		return map[string]bool{ret: true}, nil
	})
}

// NewStreamChainBranch 基于给定流式条件创建新的 ChainBranch 实例。
// 接受泛型类型 T 和该类型的 StreamGraphBranchCondition 函数。
// 返回的 ChainBranch 包含空的 key2BranchNode 映射和包装后的条件函数，
// 用于处理类型断言和错误检查。
//
// 示例：
//
//	condition := func(ctx context.Context, in *schema.StreamReader[string], opts ...any) (endNode string, err error) {
//		// 确定下一个节点的逻辑，可以读取流并做出决策
//		// 为节省时间，通常读取流的第一个块，然后决定走哪条路径
//		return "some_next_node_key", nil
//	}
//
//	cb := NewStreamChainBranch[string](condition)
func NewStreamChainBranch[T any](cond StreamGraphBranchCondition[T]) *ChainBranch {
	return NewStreamChainMultiBranch(func(ctx context.Context, in *schema.StreamReader[T]) (endNodes map[string]bool, err error) {
		ret, err := cond(ctx, in)
		if err != nil {
			return nil, err
		}
		return map[string]bool{ret: true}, nil
	})
}

// AddChatModel 向分支添加 ChatModel 节点。
//
// 示例：
//
//	chatModel01, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
//		Model: "gpt-4o",
//	})
//	chatModel02, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
//		Model: "gpt-4o-mini",
//	})
//	cb.AddChatModel("chat_model_key_01", chatModel01)
//	cb.AddChatModel("chat_model_key_02", chatModel02)
func (cb *ChainBranch) AddChatModel(key string, node model.BaseChatModel, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toChatModelNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddChatTemplate 向分支添加 ChatTemplate 节点。
//
// 示例：
//
//	chatTemplate, err := prompt.FromMessages(schema.FString, &schema.Message{
//		Role:    schema.System,
//		Content: "You are acting as a {role}.",
//	})
//
//	cb.AddChatTemplate("chat_template_key_01", chatTemplate)
//
//	chatTemplate2, err := prompt.FromMessages(schema.FString, &schema.Message{
//		Role:    schema.System,
//		Content: "You are acting as a {role}, you are not allowed to chat in other topics.",
//	})
//
//	cb.AddChatTemplate("chat_template_key_02", chatTemplate2)
func (cb *ChainBranch) AddChatTemplate(key string, node prompt.ChatTemplate, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toChatTemplateNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddToolsNode 向分支添加 ToolsNode。
//
// 示例：
//
//	toolsNode, err := tools.NewToolNode(ctx, &tools.ToolsNodeConfig{
//		Tools: []tools.Tool{...},
//	})
//
//	cb.AddToolsNode("tools_node_key", toolsNode)
func (cb *ChainBranch) AddToolsNode(key string, node *ToolsNode, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toToolsNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddLambda 向分支添加 Lambda 节点。
//
// 示例：
//
//	lambdaFunc := func(ctx context.Context, in string, opts ...any) (out string, err error) {
//		// 处理输入的逻辑
//		return "processed_output", nil
//	}
//
//	cb.AddLambda("lambda_node_key", compose.InvokeLambda(lambdaFunc))
func (cb *ChainBranch) AddLambda(key string, node *Lambda, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toLambdaNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddEmbedding 向分支添加 Embedding 节点。
//
// 示例：
//
//	embeddingNode, err := openai.NewEmbedder(ctx, &openai.EmbeddingConfig{
//		Model: "text-embedding-3-small",
//	})
//
//	cb.AddEmbedding("embedding_node_key", embeddingNode)
func (cb *ChainBranch) AddEmbedding(key string, node embedding.Embedder, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toEmbeddingNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddRetriever 向分支添加 Retriever 节点。
//
// 示例：
//
//	retriever, err := volc_vikingdb.NewRetriever(ctx, &volc_vikingdb.RetrieverConfig{
//		Collection: "my_collection",
//	})
//
//	cb.AddRetriever("retriever_node_key", retriever)
func (cb *ChainBranch) AddRetriever(key string, node retriever.Retriever, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toRetrieverNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddLoader 向分支添加 Loader 节点。
//
// 示例：
//
//	pdfParser, err := pdf.NewPDFParser()
//	loader, err := file.NewFileLoader(ctx, &file.FileLoaderConfig{
//		Parser: pdfParser,
//	})
//
//	cb.AddLoader("loader_node_key", loader)
func (cb *ChainBranch) AddLoader(key string, node document.Loader, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toLoaderNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddIndexer 向分支添加 Indexer 节点。
//
// 示例：
//
//	indexer, err := volc_vikingdb.NewIndexer(ctx, &volc_vikingdb.IndexerConfig{
//		Collection: "my_collection",
//	})
//
//	cb.AddIndexer("indexer_node_key", indexer)
func (cb *ChainBranch) AddIndexer(key string, node indexer.Indexer, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toIndexerNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddDocumentTransformer 向分支添加 document.Transformer 节点。
//
// 示例：
//
//	markdownSplitter, err := markdown.NewHeaderSplitter(ctx, &markdown.HeaderSplitterConfig{})
//
//	cb.AddDocumentTransformer("document_transformer_node_key", markdownSplitter)
func (cb *ChainBranch) AddDocumentTransformer(key string, node document.Transformer, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toDocumentTransformerNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddGraph 向分支添加通用 Graph 节点。
//
// 示例：
//
//	graph, err := compose.NewGraph[string, string]()
//
//	cb.AddGraph("graph_node_key", graph)
func (cb *ChainBranch) AddGraph(key string, node AnyGraph, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toAnyGraphNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddPassthrough 向分支添加 Passthrough 节点。
//
// 示例：
//
//	cb.AddPassthrough("passthrough_node_key")
func (cb *ChainBranch) AddPassthrough(key string, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toPassthroughNode(opts...)
	return cb.addNode(key, gNode, options)
}

func (cb *ChainBranch) addNode(key string, node *graphNode, options *graphAddNodeOpts) *ChainBranch {
	if cb.err != nil {
		return cb
	}

	if cb.key2BranchNode == nil {
		cb.key2BranchNode = make(map[string]nodeOptionsPair)
	}

	_, ok := cb.key2BranchNode[key]
	if ok {
		cb.err = fmt.Errorf("chain branch add node, duplicate branch node key= %s", key)
		return cb
	}

	cb.key2BranchNode[key] = nodeOptionsPair{node, options}

	return cb
}
