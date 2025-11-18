/*
 * chain.go - 链式组件编排，提供流畅的构建器模式
 *
 * 核心组件：
 *   - Chain[I, O]: 泛型链，支持顺序、并行和分支节点
 *   - ChainBranch: 条件分支，根据条件选择执行路径
 *   - Parallel: 并行执行，同时执行多个节点
 *
 * 设计特点：
 *   - 构建器模式: 链式调用 AppendXX 方法添加节点
 *   - 类型安全: 使用泛型确保输入输出类型匹配
 *   - 灵活编排: 支持顺序、并行、分支三种编排方式
 *   - 延迟编译: 构建完成后调用 Compile 生成可运行对象
 *
 * 使用流程：
 *   1. 创建链：NewChain[I, O]()
 *   2. 添加节点：AppendChatModel().AppendToolsNode()
 *   3. 添加并行/分支：AppendParallel() / AppendBranch()
 *   4. 编译：Compile(ctx)
 *   5. 执行：Invoke/Stream/Collect/Transform
 *
 * 在图中使用：
 *   - 链实现了 AnyGraph 接口，可作为图的节点
 *   - 可以在另一个链中通过 AppendGraph 添加
 */

package compose

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/favbox/eino/components/document"
	"github.com/favbox/eino/components/embedding"
	"github.com/favbox/eino/components/indexer"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/components/retriever"
	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/internal/gmap"
	"github.com/favbox/eino/internal/gslice"
)

// NewChain 创建指定输入输出类型的链。
// 支持通过 WithGenLocalState 选项配置节点间共享状态。
func NewChain[I, O any](opts ...NewGraphOption) *Chain[I, O] {
	ch := &Chain[I, O]{
		gg: NewGraph[I, O](opts...),
	}

	ch.gg.cmp = ComponentOfChain

	return ch
}

// Chain 是组件链，支持顺序、并行和分支节点。
// 采用构建器模式设计，使用前需要先 Compile。
// 提供链式调用接口，可以像这样使用：chain.AppendXX(...).AppendXX(...)
//
// 基本用法：
//  1. 创建指定输入输出类型的链：chain := NewChain[inputType, outputType]()
//  2. 添加组件到链：
//     2.1 添加组件：chain.AppendChatTemplate(...).AppendChatModel(...).AppendToolsNode(...)
//     2.2 添加并行或分支：chain.AppendParallel()、chain.AppendBranch()
//  3. 编译：r, err := c.Compile()
//  4. 运行：
//     4.1 单输入单输出：r.Invoke(ctx, input)
//     4.2 单输入流式输出：r.Stream(ctx, input)
//     4.3 流式输入单输出：r.Collect(ctx, inputReader)
//     4.4 流式输入流式输出：r.Transform(ctx, inputReader)
//
// 在图或其他链中使用：
//
//	chain1 := NewChain[inputType, outputType]()
//	graph := NewGraph[]()
//	graph.AddGraph("key", chain1) // chain 是 AnyGraph 实现
//
//	// 或在另一个链中：
//	chain2 := NewChain[inputType, outputType]()
//	chain2.AppendGraph(chain1)
type Chain[I, O any] struct {
	err error // 构建过程中的第一个错误

	gg *Graph[I, O] // 底层图实现

	nodeIdx int // 节点索引，用于生成节点键

	preNodeKeys []string // 前驱节点键列表

	hasEnd bool // 是否已添加终点边
}

// ErrChainCompiled 表示尝试修改已编译的链时返回的错误
var ErrChainCompiled = errors.New("chain has been compiled, cannot be modified")

// compile 编译链为可组合运行对象。
// 实现 AnyGraph 接口。
func (c *Chain[I, O]) compile(ctx context.Context, option *graphCompileOptions) (*composableRunnable, error) {
	if err := c.addEndIfNeeded(); err != nil {
		return nil, err
	}

	return c.gg.compile(ctx, option)
}

// addEndIfNeeded 添加链的终点边。
// 编译时只运行一次。
func (c *Chain[I, O]) addEndIfNeeded() error {
	if c.hasEnd {
		return nil
	}

	if c.err != nil {
		return c.err
	}

	if len(c.preNodeKeys) == 0 {
		return fmt.Errorf("pre node keys not set, number of nodes in chain= %d", len(c.gg.nodes))
	}

	for _, nodeKey := range c.preNodeKeys {
		err := c.gg.AddEdge(nodeKey, END)
		if err != nil {
			return err
		}
	}

	c.hasEnd = true

	return nil
}

func (c *Chain[I, O]) getGenericHelper() *genericHelper {
	return newGenericHelper[I, O]()
}

// inputType 返回链的输入类型。
// 实现 AnyGraph 接口。
func (c *Chain[I, O]) inputType() reflect.Type {
	return generic.TypeOf[I]()
}

// outputType 返回链的输出类型。
// 实现 AnyGraph 接口。
func (c *Chain[I, O]) outputType() reflect.Type {
	return generic.TypeOf[O]()
}

// component 返回链的组件类型。
// 实现 AnyGraph 接口。
func (c *Chain[I, O]) component() component {
	return c.gg.component()
}

// Compile 将链编译为可运行对象。
// 可运行对象可以直接使用。
//
// 示例：
//
//	chain := NewChain[string, string]()
//	r, err := chain.Compile()
//	if err != nil {}
//
//	r.Invoke(ctx, input)           // 单输入 => 单输出
//	r.Stream(ctx, input)           // 单输入 => 流式输出
//	r.Collect(ctx, inputReader)    // 流式输入 => 单输出
//	r.Transform(ctx, inputReader)  // 流式输入 => 流式输出
func (c *Chain[I, O]) Compile(ctx context.Context, opts ...GraphCompileOption) (Runnable[I, O], error) {
	if err := c.addEndIfNeeded(); err != nil {
		return nil, err
	}

	return c.gg.Compile(ctx, opts...)
}

// AppendChatModel 向链中添加 ChatModel 节点。
//
// 示例：
//
//	model, err := openai.NewChatModel(ctx, config)
//	if err != nil {...}
//	chain.AppendChatModel(model)
func (c *Chain[I, O]) AppendChatModel(node model.BaseChatModel, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toChatModelNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendChatTemplate 向链中添加 ChatTemplate 节点。
//
// 示例：
//
//	chatTemplate, err := prompt.FromMessages(schema.FString, &schema.Message{
//		Role:    schema.System,
//		Content: "You are acting as a {role}.",
//	})
//
//	chain.AppendChatTemplate(chatTemplate)
func (c *Chain[I, O]) AppendChatTemplate(node prompt.ChatTemplate, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toChatTemplateNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendToolsNode 向链中添加 ToolsNode 节点。
//
// 示例：
//
//	toolsNode, err := tools.NewToolNode(ctx, &tools.ToolsNodeConfig{
//		Tools: []tools.Tool{...},
//	})
//
//	chain.AppendToolsNode(toolsNode)
func (c *Chain[I, O]) AppendToolsNode(node *ToolsNode, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toToolsNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendDocumentTransformer 向链中添加 document.Transformer 节点。
//
// 示例：
//
//	markdownSplitter, err := markdown.NewHeaderSplitter(ctx, &markdown.HeaderSplitterConfig{})
//
//	chain.AppendDocumentTransformer(markdownSplitter)
func (c *Chain[I, O]) AppendDocumentTransformer(node document.Transformer, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toDocumentTransformerNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendLambda 向链中添加 Lambda 节点。
// Lambda 节点可用于实现自定义逻辑。
//
// 示例：
//
//	lambdaNode := compose.InvokableLambda(func(ctx context.Context, docs []*schema.Document) (string, error) {...})
//	chain.AppendLambda(lambdaNode)
//
// 注意：
// 创建 Lambda 节点需要使用 compose.AnyLambda、compose.InvokableLambda、compose.StreamableLambda 或 compose.TransformableLambda。
// 如果需要真正的流式输出，需要使用 compose.StreamableLambda 或 compose.TransformableLambda。
func (c *Chain[I, O]) AppendLambda(node *Lambda, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toLambdaNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendEmbedding 向链中添加 Embedding 节点。
//
// 示例：
//
//	embedder, err := openai.NewEmbedder(ctx, config)
//	if err != nil {...}
//	chain.AppendEmbedding(embedder)
func (c *Chain[I, O]) AppendEmbedding(node embedding.Embedder, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toEmbeddingNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendRetriever 向链中添加 Retriever 节点。
//
// 示例：
//
//	retriever, err := vectorstore.NewRetriever(ctx, config)
//	if err != nil {...}
//	chain.AppendRetriever(retriever)
//
// 或使用 fornax knowledge 作为检索器：
//
//	config := fornaxknowledge.Config{...}
//	retriever, err := fornaxknowledge.NewKnowledgeRetriever(ctx, config)
//	if err != nil {...}
//	chain.AppendRetriever(retriever)
func (c *Chain[I, O]) AppendRetriever(node retriever.Retriever, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toRetrieverNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendLoader 向链中添加 document.Loader 节点。
//
// 示例：
//
//	loader, err := file.NewFileLoader(ctx, &file.FileLoaderConfig{})
//	if err != nil {...}
//	chain.AppendLoader(loader)
func (c *Chain[I, O]) AppendLoader(node document.Loader, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toLoaderNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendIndexer 向链中添加 Indexer 节点。
// Indexer 节点可以存储文档。
//
// 示例：
//
//	vectorStoreImpl, err := vikingdb.NewVectorStorer(ctx, vikingdbConfig)
//	if err != nil {...}
//
//	config := vectorstore.IndexerConfig{VectorStore: vectorStoreImpl}
//	indexer, err := vectorstore.NewIndexer(ctx, config)
//	if err != nil {...}
//
//	chain.AppendIndexer(indexer)
func (c *Chain[I, O]) AppendIndexer(node indexer.Indexer, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toIndexerNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendBranch 向链中添加条件分支。
// ChainBranch 中的每个分支都可以是 AnyGraph。
// 所有分支应该通向 END，或汇聚到链中的另一个节点。
//
// 示例：
//
//	cb := compose.NewChainBranch(conditionFunc)
//	cb.AddChatTemplate("chat_template_key_01", chatTemplate)
//	cb.AddChatTemplate("chat_template_key_02", chatTemplate2)
//	chain.AppendBranch(cb)
func (c *Chain[I, O]) AppendBranch(b *ChainBranch) *Chain[I, O] {
	if b == nil {
		c.reportError(fmt.Errorf("append branch invalid, branch is nil"))
		return c
	}

	if b.err != nil {
		c.reportError(fmt.Errorf("append branch error: %w", b.err))
		return c
	}

	if len(b.key2BranchNode) == 0 {
		c.reportError(fmt.Errorf("append branch invalid, nodeList is empty"))
		return c
	}

	if len(b.key2BranchNode) == 1 {
		c.reportError(fmt.Errorf("append branch invalid, nodeList length = 1"))
		return c
	}

	var startNode string
	if len(c.preNodeKeys) == 0 { // branch appended directly to START
		startNode = START
	} else if len(c.preNodeKeys) == 1 {
		startNode = c.preNodeKeys[0]
	} else {
		c.reportError(fmt.Errorf("append branch invalid, multiple previous nodes: %v ", c.preNodeKeys))
		return c
	}

	prefix := c.nextNodeKey()
	key2NodeKey := make(map[string]string, len(b.key2BranchNode))

	for key := range b.key2BranchNode {
		node := b.key2BranchNode[key]

		var nodeKey string

		if node.Second != nil && node.Second.nodeOptions != nil && node.Second.nodeOptions.nodeKey != "" {
			nodeKey = node.Second.nodeOptions.nodeKey
		} else {
			nodeKey = fmt.Sprintf("%s_branch_%s", prefix, key)
		}

		if err := c.gg.addNode(nodeKey, node.First, node.Second); err != nil {
			c.reportError(fmt.Errorf("add branch node[%s] to chain failed: %w", nodeKey, err))
			return c
		}

		key2NodeKey[key] = nodeKey
	}

	gBranch := *b.internalBranch

	invokeCon := func(ctx context.Context, in any) (endNode []string, err error) {
		ends, err := b.internalBranch.invoke(ctx, in)
		if err != nil {
			return nil, err
		}

		nodeKeyEnds := make([]string, 0, len(ends))
		for _, end := range ends {
			if nodeKey, ok := key2NodeKey[end]; !ok {
				return nil, fmt.Errorf("branch invocation returns unintended end node: %s", end)
			} else {
				nodeKeyEnds = append(nodeKeyEnds, nodeKey)
			}
		}

		return nodeKeyEnds, nil
	}
	gBranch.invoke = invokeCon

	collectCon := func(ctx context.Context, sr streamReader) ([]string, error) {
		ends, err := b.internalBranch.collect(ctx, sr)
		if err != nil {
			return nil, err
		}

		nodeKeyEnds := make([]string, 0, len(ends))
		for _, end := range ends {
			if nodeKey, ok := key2NodeKey[end]; !ok {
				return nil, fmt.Errorf("branch invocation returns unintended end node: %s", end)
			} else {
				nodeKeyEnds = append(nodeKeyEnds, nodeKey)
			}
		}

		return nodeKeyEnds, nil
	}
	gBranch.collect = collectCon

	gBranch.endNodes = gslice.ToMap(gmap.Values(key2NodeKey), func(k string) (string, bool) {
		return k, true
	})

	if err := c.gg.AddBranch(startNode, &gBranch); err != nil {
		c.reportError(fmt.Errorf("chain append branch failed: %w", err))
		return c
	}

	c.preNodeKeys = gmap.Values(key2NodeKey)

	return c
}

// AppendParallel 向链中添加并行结构（多个并发节点）。
//
// 示例：
//
//	parallel := compose.NewParallel()
//	parallel.AddChatModel("openai", model1) // => "openai": *schema.Message{}
//	parallel.AddChatModel("maas", model2)   // => "maas": *schema.Message{}
//
//	chain.AppendParallel(parallel) // => 多个并发节点被添加到链中
//
// 链中的下一个节点应该是 END，或接受 map[string]any 的节点，其中键为上面指定的 "openai"、"maas"。
func (c *Chain[I, O]) AppendParallel(p *Parallel) *Chain[I, O] {
	if p == nil {
		c.reportError(fmt.Errorf("append parallel invalid, parallel is nil"))
		return c
	}

	if p.err != nil {
		c.reportError(fmt.Errorf("append parallel invalid, parallel error: %w", p.err))
		return c
	}

	if len(p.nodes) <= 1 {
		c.reportError(fmt.Errorf("append parallel invalid, not enough nodes, count = %d", len(p.nodes)))
		return c
	}

	var startNode string
	if len(c.preNodeKeys) == 0 { // parallel appended directly to START
		startNode = START
	} else if len(c.preNodeKeys) == 1 {
		startNode = c.preNodeKeys[0]
	} else {
		c.reportError(fmt.Errorf("append parallel invalid, multiple previous nodes: %v ", c.preNodeKeys))
		return c
	}

	prefix := c.nextNodeKey()
	var nodeKeys []string

	for i := range p.nodes {
		node := p.nodes[i]

		var nodeKey string
		if node.Second != nil && node.Second.nodeOptions != nil && node.Second.nodeOptions.nodeKey != "" {
			nodeKey = node.Second.nodeOptions.nodeKey
		} else {
			nodeKey = fmt.Sprintf("%s_parallel_%d", prefix, i)
		}

		if err := c.gg.addNode(nodeKey, node.First, node.Second); err != nil {
			c.reportError(fmt.Errorf("add parallel node to chain failed, key=%s, err: %w", nodeKey, err))
			return c
		}

		if err := c.gg.AddEdge(startNode, nodeKey); err != nil {
			c.reportError(fmt.Errorf("add parallel edge failed, from=%s, to=%s, err: %w", startNode, nodeKey, err))
			return c
		}

		nodeKeys = append(nodeKeys, nodeKey)
	}

	c.preNodeKeys = nodeKeys

	return c
}

// AppendGraph 向链中添加 AnyGraph 节点。
// AnyGraph 可以是链或图。
//
// 示例：
//
//	graph := compose.NewGraph[string, string]()
//	chain.AppendGraph(graph)
func (c *Chain[I, O]) AppendGraph(node AnyGraph, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toAnyGraphNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendPassthrough 向链中添加 Passthrough 节点。
// 可用于连接多个 ChainBranch 或 Parallel。
//
// 示例：
//
//	chain.AppendPassthrough()
func (c *Chain[I, O]) AppendPassthrough(opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toPassthroughNode(opts...)
	c.addNode(gNode, options)
	return c
}

// nextNodeKey 获取链的下一个节点键。
// 链键格式：
//   - node_idx：如 node_0 表示链的第一个节点（索引从 0 开始）
//   - node_idx_parallel_idx：如 node_0_parallel_1 表示第一个节点的并行节点中的第二个
//   - node_idx_branch_key：如 node_1_branch_customkey 表示第二个节点的分支节点中的自定义键
func (c *Chain[I, O]) nextNodeKey() string {
	idx := c.nodeIdx
	c.nodeIdx++
	return fmt.Sprintf("node_%d", idx)
}

// reportError 保存链中的第一个错误
func (c *Chain[I, O]) reportError(err error) {
	if c.err == nil {
		c.err = err
	}
}

// addNode 向链中添加节点，自动连接前驱节点
func (c *Chain[I, O]) addNode(node *graphNode, options *graphAddNodeOpts) {
	if c.err != nil {
		return
	}

	if c.gg.compiled {
		c.reportError(ErrChainCompiled)
		return
	}

	if node == nil {
		c.reportError(fmt.Errorf("chain add node invalid, node is nil"))
		return
	}

	nodeKey := options.nodeOptions.nodeKey
	defaultNodeKey := c.nextNodeKey()
	if nodeKey == "" {
		nodeKey = defaultNodeKey
	}

	err := c.gg.addNode(nodeKey, node, options)
	if err != nil {
		c.reportError(err)
		return
	}

	if len(c.preNodeKeys) == 0 {
		c.preNodeKeys = append(c.preNodeKeys, START)
	}

	for _, preNodeKey := range c.preNodeKeys {
		e := c.gg.AddEdge(preNodeKey, nodeKey)
		if e != nil {
			c.reportError(e)
			return
		}
	}

	c.preNodeKeys = []string{nodeKey}
}
