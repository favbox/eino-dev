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

/*
 * chain.go - 链式编排模式实现
 *
 * 核心组件：
 *   - Chain 结构体：链式编排核心类型，支持组件、Lambda、并行、分支等
 *   - NewChain 工厂函数：创建链式编排实例
 *   - AppendXXX 方法族：链式构建器，支持15+种节点类型
 *   - ChainBranch：条件分支结构，支持多路径选择
 *   - Parallel：并行结构，支持多节点并发执行
 *
 * 设计特点：
 *   - 链式模式：基于建造者模式的流畅 API 设计
 *   - 类型安全：通过泛型确保编译期类型检查
 *   - 多范式支持：顺序、分支、并行三种执行模式
 *   - 错误容忍：编译前收集所有错误，避免中途失败
 *   - 可组合性：链可以作为节点嵌入图或其他链中
 *
 * 与其他文件关系：
 *   - 继承 generic_graph.go 的 Graph 实现
 *   - 为 types_composable.go 提供 Chain[I,O] 类型实现
 *   - 与 component_to_graph_node.go 协作转换组件为节点
 *   - 与 branch.go 和 pregel.go 的分支/并行结构协同
 *
 * 使用场景：
 *   - 简单流程：线性数据处理的流水线
 *   - 条件分支：根据条件选择不同执行路径
 *   - 并发执行：多节点同时处理提高性能
 *   - 可重用链：将复杂流程封装为可复用组件
 *
 * 常用模式：
 *   1. 创建链：chain := NewChain[inputType, outputType]()
 *   2. 添加组件：chain.AppendChatModel(...).AppendToolsNode(...)
 *   3. 添加结构：AppendBranch() / AppendParallel()
 *   4. 编译执行：r, err := chain.Compile()
 *   5. 调用执行：r.Invoke(ctx, input) / r.Stream(ctx, input)
 */

// ====== Chain 创建与初始化 ======

// NewChain 创建链式编排实例 - 支持输入输出类型的链式构建
func NewChain[I, O any](opts ...NewGraphOption) *Chain[I, O] {
	ch := &Chain[I, O]{
		gg: NewGraph[I, O](opts...),
	}

	ch.gg.cmp = ComponentOfChain

	return ch
}

// ====== Chain 结构体定义 ======

// Chain 链式编排结构体 - 组件的有序链式组合
// 链节点可以是并行/分支/顺序组件
// Chain 采用建造者模式设计（使用前需调用 Compile()）
// 提供"链式风格"接口：`chain.AppendXX(...).AppendXX(...)`
//
// 常规使用：
//  1. 创建链：指定输入输出类型 `chain := NewChain[inputType, outputType]()`
//  2. 添加组件到链：
//     2.1 添加组件：`chain.AppendChatTemplate(...).AppendChatModel(...).AppendToolsNode(...)`
//     2.2 如需并行或分支：`chain.AppendParallel()`, `chain.AppendBranch()`
//  3. 编译：`r, err := c.Compile()`
//  4. 运行：
//     4.1 `单输入&单输出` 使用 `r.Invoke(ctx, input)`
//     4.2 `单输入&多输出块` 使用 `r.Stream(ctx, input)`
//     4.3 `多输入块&单输出` 使用 `r.Collect(ctx, inputReader)`
//     4.4 `多输入块&多输出块` 使用 `r.Transform(ctx, inputReader)`
//
// 在图或其他链中使用：
// chain1 := NewChain[inputType, outputType]()
// graph := NewGraph[](runTypePregel)
// graph.AddGraph("key", chain1) // chain 是 AnyGraph 实现
//
// // 或在另一个链中：
// chain2 := NewChain[inputType, outputType]()
// chain2.AppendGraph(chain1)
type Chain[I, O any] struct {
	err error // 链级错误状态

	gg *Graph[I, O] // 内部图实现

	nodeIdx int // 节点索引计数器

	preNodeKeys []string // 前置节点键列表

	hasEnd bool // 是否已添加 END 节点
}

// ====== 错误定义 ======

// ErrChainCompiled 链已编译错误 - 尝试修改已编译的链时返回
var ErrChainCompiled = errors.New("chain has been compiled, cannot be modified")

// ====== AnyGraph 接口实现 ======

// compile 编译链为可执行对象 - 实现 AnyGraph 接口
func (c *Chain[I, O]) compile(ctx context.Context, option *graphCompileOptions) (*composableRunnable, error) {
	if err := c.addEndIfNeeded(); err != nil {
		return nil, err
	}

	return c.gg.compile(ctx, option)
}

// addEndIfNeeded 添加链/图的 END 边 - 确保所有路径正确结束
// 仅在编译时运行一次
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

// getGenericHelper 获取泛型辅助器 - 实现 AnyGraph 接口
func (c *Chain[I, O]) getGenericHelper() *genericHelper {
	return newGenericHelper[I, O]()
}

// inputType 获取链的输入类型 - 实现 AnyGraph 接口
func (c *Chain[I, O]) inputType() reflect.Type {
	return generic.TypeOf[I]()
}

// outputType 获取链的输出类型 - 实现 AnyGraph 接口
func (c *Chain[I, O]) outputType() reflect.Type {
	return generic.TypeOf[O]()
}

// component 获取链的组件类型 - 实现 AnyGraph 接口
func (c *Chain[I, O]) component() component {
	return c.gg.component()
}

// ====== Chain 编译 ======

// Compile 编译链为可执行对象 - Runnable 可直接使用
// 示例：
//
//		chain := NewChain[string, string]()
//		r, err := chain.Compile()
//		if err != nil {}
//
//	 	r.Invoke(ctx, input) // ping => pong
//		r.Stream(ctx, input) // ping => stream out
//		r.Collect(ctx, inputReader) // stream in => pong
//		r.Transform(ctx, inputReader) // stream in => stream out
func (c *Chain[I, O]) Compile(ctx context.Context, opts ...GraphCompileOption) (Runnable[I, O], error) {
	if err := c.addEndIfNeeded(); err != nil {
		return nil, err
	}

	return c.gg.Compile(ctx, opts...)
}

// ====== Append 方法族（组件类型）======

// AppendChatModel 添加聊天模型节点到链
// e.g.
//
//	model, err := openai.NewChatModel(ctx, config)
//	if err != nil {...}
//	chain.AppendChatModel(model)
func (c *Chain[I, O]) AppendChatModel(node model.BaseChatModel, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toChatModelNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendChatTemplate 添加聊天模板节点到链
// eg.
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

// AppendToolsNode 添加工具节点到链
// e.g.
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

// ====== Append 方法族（结构类型）======

// AppendDocumentTransformer 添加文档转换器节点到链
// e.g.
//
//	markdownSplitter, err := markdown.NewHeaderSplitter(ctx, &markdown.HeaderSplitterConfig{})
//
//	chain.AppendDocumentTransformer(markdownSplitter)
func (c *Chain[I, O]) AppendDocumentTransformer(node document.Transformer, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toDocumentTransformerNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendLambda 添加 Lambda 节点到链 - 实现自定义逻辑
// Lambda 是可用于实现自定义逻辑的节点
// 示例：
//
//	lambdaNode := compose.InvokableLambda(func(ctx context.Context, docs []*schema.Document) (string, error) {...})
//	chain.AppendLambda(lambdaNode)
//
// 注意：
// 创建 Lambda 节点需要使用 `compose.AnyLambda` 或 `compose.InvokableLambda` 或 `compose.StreamableLambda` 或 `compose.TransformableLambda`
// 如果希望节点有真正的流式输出，需要使用 `compose.StreamableLambda` 或 `compose.TransformableLambda`
func (c *Chain[I, O]) AppendLambda(node *Lambda, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toLambdaNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendEmbedding 添加嵌入模型节点到链
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

// AppendRetriever 添加检索器节点到链
// 示例：
//
//		retriever, err := vectorstore.NewRetriever(ctx, config)
//		if err != nil {...}
//		chain.AppendRetriever(retriever)
//
//	 或使用 fornax knowledge 作为检索器：
//
//		config := fornaxknowledge.Config{...}
//		retriever, err := fornaxknowledge.NewKnowledgeRetriever(ctx, config)
//		if err != nil {...}
//		chain.AppendRetriever(retriever)
func (c *Chain[I, O]) AppendRetriever(node retriever.Retriever, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toRetrieverNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendLoader 添加文档加载器节点到链
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

// AppendIndexer 添加索引器节点到链 - 用于存储文档
// Indexer 是可以存储文档的节点
// 示例：
//
//	vectorStoreImpl, err := vikingdb.NewVectorStorer(ctx, vikingdbConfig) // in components/vectorstore/vikingdb/vectorstore.go
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

// AppendBranch 添加条件分支到链 - 支持多路径选择
// ChainBranch 中的每个分支都可以是 AnyGraph
// 所有分支应指向 END 或汇聚到链中的另一个节点
// 示例：
//
//	cb := compose.NewChainBranch(conditionFunc)
//	cb.AddChatTemplate("chat_template_key_01", chatTemplate)
//	cb.AddChatTemplate("chat_template_key_02", chatTemplate2)
//	chain.AppendBranch(cb)
func (c *Chain[I, O]) AppendBranch(b *ChainBranch) *Chain[I, O] {
	// 参数验证：检查分支结构是否存在
	if b == nil {
		c.reportError(fmt.Errorf("append branch invalid, branch is nil"))
		return c
	}

	// 检查分支构建过程中的错误
	if b.err != nil {
		c.reportError(fmt.Errorf("append branch error: %w", b.err))
		return c
	}

	// 分支数量验证：至少需要2个分支才能形成有效分支
	if len(b.key2BranchNode) == 0 {
		c.reportError(fmt.Errorf("append branch invalid, nodeList is empty"))
		return c
	}

	if len(b.key2BranchNode) == 1 {
		c.reportError(fmt.Errorf("append branch invalid, nodeList length = 1"))
		return c
	}

	// 确定分支的起始节点
	var startNode string
	if len(c.preNodeKeys) == 0 {
		// 分支直接附加到 START 节点
		startNode = START
	} else if len(c.preNodeKeys) == 1 {
		startNode = c.preNodeKeys[0] // 从单个前置节点开始
	} else {
		// 不支持从多个前置节点开始的分支（避免复杂的多源合并）
		c.reportError(fmt.Errorf("append branch invalid, multiple previous nodes: %v ", c.preNodeKeys))
		return c
	}

	// 生成节点键前缀，用于创建唯一的分支节点键
	prefix := c.nextNodeKey()
	// 创建分支键到实际节点键的映射表
	key2NodeKey := make(map[string]string, len(b.key2BranchNode))

	// 为每个分支创建节点
	for key := range b.key2BranchNode {
		node := b.key2BranchNode[key]

		var nodeKey string

		// 使用用户指定的节点键或生成默认键
		if node.Second != nil && node.Second.nodeOptions != nil && node.Second.nodeOptions.nodeKey != "" {
			nodeKey = node.Second.nodeOptions.nodeKey
		} else {
			nodeKey = fmt.Sprintf("%s_branch_%s", prefix, key)
		}

		// 将分支节点添加到图中
		if err := c.gg.addNode(nodeKey, node.First, node.Second); err != nil {
			c.reportError(fmt.Errorf("add branch node[%s] to chain failed: %w", nodeKey, err))
			return c
		}

		// 记录分支键到节点键的映射
		key2NodeKey[key] = nodeKey
	}

	// 复制内部分支结构
	gBranch := *b.internalBranch

	// 将调用转换为实际节点键
	invokeCon := func(ctx context.Context, in any) (endNode []string, err error) {
		// 执行原始分支逻辑，获取分支键
		ends, err := b.internalBranch.invoke(ctx, in)
		if err != nil {
			return nil, err
		}

		// 将分支键映射为实际节点键
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

	// 处理收集模式的分支逻辑
	collectCon := func(ctx context.Context, sr streamReader) ([]string, error) {
		ends, err := b.internalBranch.collect(ctx, sr)
		if err != nil {
			return nil, err
		}

		// 将流式分支键映射为实际节点键
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

	// 设置分支的结束节点集合（用于流式执行的控制）
	gBranch.endNodes = gslice.ToMap(gmap.Values(key2NodeKey), func(k string) (string, bool) {
		return k, true
	})

	// 将分支添加到图中
	if err := c.gg.AddBranch(startNode, &gBranch); err != nil {
		c.reportError(fmt.Errorf("chain append branch failed: %w", err))
		return c
	}

	// 更新前置节点列表为所有分支节点（用于后续节点的连接）
	c.preNodeKeys = gmap.Values(key2NodeKey)

	return c
}

// AppendParallel 添加并行结构到链 - 多节点并发执行
// 示例：
//
//	parallel := compose.NewParallel()
//	parallel.AddChatModel("openai", model1) // => "openai": *schema.Message{}
//	parallel.AddChatModel("maas", model2) // => "maas": *schema.Message{}
//
//	chain.AppendParallel(parallel) // => multiple concurrent nodes are added to the Chain
//
// 链中的下一个节点应是 END，或接受 map[string]any 的节点，键为上述指定的 'openai'、'maas' 等
func (c *Chain[I, O]) AppendParallel(p *Parallel) *Chain[I, O] {
	// 参数验证：检查并行结构是否存在
	if p == nil {
		c.reportError(fmt.Errorf("append parallel invalid, parallel is nil"))
		return c
	}

	// 检查并行结构构建过程中的错误
	if p.err != nil {
		c.reportError(fmt.Errorf("append parallel invalid, parallel error: %w", p.err))
		return c
	}

	// 并行节点数量验证：至少需要2个节点才能形成有效并行
	if len(p.nodes) <= 1 {
		c.reportError(fmt.Errorf("append parallel invalid, not enough nodes, count = %d", len(p.nodes)))
		return c
	}

	// 确定并行的起始节点
	var startNode string
	if len(c.preNodeKeys) == 0 {
		// 并行直接附加到 START 节点
		startNode = START
	} else if len(c.preNodeKeys) == 1 {
		startNode = c.preNodeKeys[0] // 从单个前置节点开始
	} else {
		// 不支持从多个前置节点开始的并行（避免复杂的多源合并）
		c.reportError(fmt.Errorf("append parallel invalid, multiple previous nodes: %v ", c.preNodeKeys))
		return c
	}

	// 生成节点键前缀，用于创建唯一的并行节点键
	prefix := c.nextNodeKey()
	var nodeKeys []string

	// 为每个并行节点创建节点并添加到图中
	for i := range p.nodes {
		node := p.nodes[i]

		var nodeKey string
		// 使用用户指定的节点键或生成默认键
		if node.Second != nil && node.Second.nodeOptions != nil && node.Second.nodeOptions.nodeKey != "" {
			nodeKey = node.Second.nodeOptions.nodeKey
		} else {
			// 格式：prefix_parallel_idx
			nodeKey = fmt.Sprintf("%s_parallel_%d", prefix, i)
		}

		// 将并行节点添加到图中
		if err := c.gg.addNode(nodeKey, node.First, node.Second); err != nil {
			c.reportError(fmt.Errorf("add parallel node to chain failed, key=%s, err: %w", nodeKey, err))
			return c
		}

		// 添加从起始节点到当前并行节点的边
		if err := c.gg.AddEdge(startNode, nodeKey); err != nil {
			c.reportError(fmt.Errorf("add parallel edge failed, from=%s, to=%s, err: %w", startNode, nodeKey, err))
			return c
		}

		// 记录并行节点的键（用于后续节点的连接）
		nodeKeys = append(nodeKeys, nodeKey)
	}

	// 更新前置节点列表为所有并行节点（用于后续节点的连接）
	c.preNodeKeys = nodeKeys

	return c
}

// AppendGraph 添加 AnyGraph 节点到链 - 支持链或图
// AnyGraph 可以是链或图
// 示例：
//
//	graph := compose.NewGraph[string, string]()
//	chain.AppendGraph(graph)
func (c *Chain[I, O]) AppendGraph(node AnyGraph, opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toAnyGraphNode(node, opts...)
	c.addNode(gNode, options)
	return c
}

// AppendPassthrough 添加直通节点到链 - 连接多个分支或并行结构
// 可用于连接多个 ChainBranch 或 Parallel
// 示例：
//
//	chain.AppendPassthrough()
func (c *Chain[I, O]) AppendPassthrough(opts ...GraphAddNodeOpt) *Chain[I, O] {
	gNode, options := toPassthroughNode(opts...)
	c.addNode(gNode, options)
	return c
}

// ====== 内部辅助方法 ======

// nextNodeKey 获取下一个节点键 - 自动生成节点索引键
// 链键格式：
// - 普通节点：node_idx（如 node_0 表示链的第一个节点，索引从0开始）
// - 并行节点：node_idx_parallel_idx（如 node_0_parallel_1 表示第一个节点的并行结构中的第二个节点）
// - 分支节点：node_idx_branch_key（如 node_1_branch_customkey 表示第二个节点的分叉结构，customkey为分支键）
func (c *Chain[I, O]) nextNodeKey() string {
	idx := c.nodeIdx
	c.nodeIdx++
	return fmt.Sprintf("node_%d", idx)
}

// reportError 报告错误 - 保存链的第一个错误
func (c *Chain[I, O]) reportError(err error) {
	if c.err == nil {
		c.err = err
	}
}

// addNode 添加节点到链 - 将节点和选项添加到链中
func (c *Chain[I, O]) addNode(node *graphNode, options *graphAddNodeOpts) {
	// 检查链是否已有错误（错误容忍机制：遇到第一个错误后停止处理）
	if c.err != nil {
		return
	}

	// 检查链是否已编译（编译后禁止修改）
	if c.gg.compiled {
		c.reportError(ErrChainCompiled)
		return
	}

	// 验证节点有效性
	if node == nil {
		c.reportError(fmt.Errorf("chain add node invalid, node is nil"))
		return
	}

	// 确定节点键：优先使用用户指定的键，否则生成默认键
	nodeKey := options.nodeOptions.nodeKey
	defaultNodeKey := c.nextNodeKey()
	if nodeKey == "" {
		nodeKey = defaultNodeKey
	}

	// 将节点添加到内部图中
	err := c.gg.addNode(nodeKey, node, options)
	if err != nil {
		c.reportError(err)
		return
	}

	// 初始化前置节点列表：如果为空，添加 START 作为第一个前置节点
	if len(c.preNodeKeys) == 0 {
		c.preNodeKeys = append(c.preNodeKeys, START)
	}

	// 添加从前置节点到当前节点的边（建立节点间的连接关系）
	for _, preNodeKey := range c.preNodeKeys {
		e := c.gg.AddEdge(preNodeKey, nodeKey)
		if e != nil {
			c.reportError(e)
			return
		}
	}

	// 更新前置节点列表为当前节点（用于连接后续节点）
	c.preNodeKeys = []string{nodeKey}
}
