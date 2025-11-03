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

/*
 * chain_parallel.go - 链式并行模式实现
 *
 * 核心组件：
 *   - Parallel: 并行执行容器，支持多个节点同时运行
 *   - NewParallel: 并行实例创建工厂函数
 *   - AddXXX 方法族: 为并行添加节点，支持10+种组件类型
 *   - addNode: 内部节点注册方法
 *
 * 设计特点：
 *   - 并行执行：多个节点同时运行，提高处理效率
 *   - 输出键管理：通过 outputKey 区分并行节点的输出
 *   - 错误容忍：累积构建错误，支持优雅降级
 *   - 建造者模式：链式 API 设计，支持流畅调用
 *   - 类型安全：通过泛型确保编译期类型检查
 *
 * 与其他文件关系：
 *   - 为 chain.go 提供并行构建能力
 *   - 与 component_to_graph_node.go 协作转换组件
 *   - 使用 nodeOptionsPair 统一管理节点和选项
 *
 * 使用场景：
 *   - 并行检索：从多个数据源同时检索信息
 *   - 并行生成：多个模型同时生成候选结果
 *   - 性能优化：将串行流程转换为并行执行
 *   - 多角度分析：同时从不同维度处理数据
 *   - 冗余计算：对关键结果进行多路径验证
 *
 * 并行执行优势：
 *   - 吞吐量提升：多个任务同时执行，充分利用系统资源
 *   - 延迟降低：串行变并行，总体执行时间显著减少
 *   - 资源利用率：提高 CPU 和 I/O 的并发度
 *   - 可扩展性：支持动态增减并行节点数量
 *
 * 设计要点：
 *   - 输出键唯一性：每个并行节点必须使用唯一的 outputKey
 *   - 节点完整性：确保节点和节点信息都不为空
 *   - 错误收集：构建过程中收集所有错误，避免部分失败
 *
 * 典型示例：
 *   创建并行检索，从多个数据源同时获取信息：
 *
 *   parallel := compose.NewParallel()
 *   parallel.AddRetriever("vikingdb_result", vikingRetriever)
 *   parallel.AddRetriever("qdrant_result", qdrantRetriever)
 *   parallel.AddRetriever("es_result", esRetriever)
 *
 *   chain := compose.NewChain[string, any]()
 *   chain.AppendParallel(parallel)
 */

// ====== 并行容器工厂函数 ======

// NewParallel 创建并行执行实例 - 用于在链中并行运行多个节点
// 初始化空的并行容器，准备添加并行节点
// 返回值：配置好的 Parallel 实例，可继续添加节点
func NewParallel() *Parallel {
	return &Parallel{
		outputKeys: make(map[string]bool),
	}
}

// ====== 并行容器结构体 ======

// Parallel 并行执行容器 - 封装多个同时运行的节点
// 支持将多个节点添加到同一个并行组中，这些节点会同时执行
// 所有并行节点的输出通过 outputKey 区分，最终合并为 map[string]any
//
// 示例：并行检索多个数据源
//
//	parallel := NewParallel()
//	parallel.AddRetriever("viking_result", vikingRetriever)
//	parallel.AddRetriever("qdrant_result", qdrantRetriever)
//	parallel.AddRetriever("es_result", esRetriever)
//
//	chain := NewChain[string, map[string]any]()
//	chain.AppendParallel(parallel)
type Parallel struct {
	// 节点列表：存储所有并行节点的元数据
	// 使用 slice 保持添加顺序，执行时按顺序启动
	nodes []nodeOptionsPair
	// 输出键集合：记录所有已使用的 outputKey，确保唯一性
	// 用于快速检查重复键，避免覆盖
	outputKeys map[string]bool
	// 构建错误：累积构建过程中的错误，支持优雅降级
	// 错误会在后续添加节点时被忽略
	err error
}

// ====== 组件节点添加方法族 ======

// AddChatModel 添加聊天模型节点 - 在并行中添加大语言模型处理路径
// 支持多个模型并行处理，接收相同输入，生成不同模型的响应
// 典型场景：多模型投票、模型性能对比、成本优化选择
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

// AddChatTemplate 添加聊天模板节点 - 在并行中添加提示词模板处理路径
// 支持多个模板并行处理，根据模板生成不同风格的提示词
// 典型场景：多模板风格对比、提示词优化测试
//
// 示例：并行处理不同风格的提示词模板
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

// AddToolsNode 添加工具节点 - 在并行中添加工具调用处理路径
// 支持多个工具集并行执行，同时处理不同的工具调用
// 典型场景：多工具并行检索、工具性能对比
//
// 示例：并行使用不同的工具集进行数据处理
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

// AddLambda 添加 Lambda 节点 - 在并行中添加自定义逻辑处理路径
// 支持多个自定义函数并行执行，同时处理不同的业务逻辑
// 典型场景：多策略并行计算、数据并行处理
//
// 示例：并行执行不同的数据转换逻辑
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

// AddEmbedding 添加嵌入模型节点 - 在并行中添加文本向量化处理路径
// 支持多个嵌入模型并行处理，生成不同维度的向量表示
// 典型场景：多嵌入模型对比、向量质量评估
//
// 示例：并行生成不同模型的文本嵌入
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

// AddRetriever 添加检索器节点 - 在并行中添加信息检索处理路径
// 支持多个数据源并行检索，同时从不同数据库获取信息
// 典型场景：多数据源融合检索、检索质量对比、冗余检索验证
//
// 示例：并行从多个数据源检索信息
//
// retriever, err := vikingdb.NewRetriever(ctx, &vikingdb.RetrieverConfig{})
//
//	p.AddRetriever("output_key01", retriever)
func (p *Parallel) AddRetriever(outputKey string, node retriever.Retriever, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toRetrieverNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddLoader 添加加载器节点 - 在并行中添加文档加载处理路径
// 支持多个数据源并行加载，同时处理不同的文档格式
// 典型场景：多文件并行解析、文档格式对比
//
// 示例：并行加载不同格式的文档
//
//	loader, err := file.NewLoader(ctx, &file.LoaderConfig{})
//
//	p.AddLoader("output_key01", loader)
func (p *Parallel) AddLoader(outputKey string, node document.Loader, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toLoaderNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddIndexer 添加索引器节点 - 在并行中添加文档索引处理路径
// 支持多个索引库并行写入，同时更新不同的索引系统
// 典型场景：索引库同步、索引性能对比
//
// 示例：并行向多个索引库写入数据
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

// AddDocumentTransformer 添加文档转换器节点 - 在并行中添加文档转换处理路径
// 支持多种转换策略并行执行，同时处理不同格式转换
// 典型场景：多转换策略对比、格式优化选择
//
// 示例：并行使用不同策略转换文档格式
//
//	markdownSplitter, err := markdown.NewHeaderSplitter(ctx, &markdown.HeaderSplitterConfig{})
//
//	p.AddDocumentTransformer("output_key01", markdownSplitter)
func (p *Parallel) AddDocumentTransformer(outputKey string, node document.Transformer, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toDocumentTransformerNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddGraph 添加子图节点 - 在并行中添加复杂图结构处理路径
// 支持嵌套图的并行执行，适合复杂业务逻辑的并行处理
// 典型场景：多流程并行执行、子系统并行集成
//
// 示例：并行执行不同的子流程
//
//	graph, err := compose.NewChain[any,any]()
//
//	p.AddGraph("output_key01", graph)
func (p *Parallel) AddGraph(outputKey string, node AnyGraph, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toAnyGraphNode(node, append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// AddPassthrough 添加直通节点 - 在并行中添加数据透传处理路径
// 输入数据直接传递到输出，不做任何处理
// 典型场景：并行中的数据路由、结果合并辅助节点
//
// 示例：在并行结构中添加透传节点用于数据路由
//
//	p.AddPassthrough("output_key01")
func (p *Parallel) AddPassthrough(outputKey string, opts ...GraphAddNodeOpt) *Parallel {
	gNode, options := toPassthroughNode(append(opts, WithOutputKey(outputKey))...)
	return p.addNode(outputKey, gNode, options)
}

// ====== 内部辅助方法 ======

// addNode 添加节点到并行 - 并行节点的统一注册方法
// 执行多层验证：节点有效性、键唯一性、节点信息完整性
// 错误处理：累积构建错误，支持优雅降级
func (p *Parallel) addNode(outputKey string, node *graphNode, options *graphAddNodeOpts) *Parallel {
	// 错误检查：已有错误时直接返回，避免继续添加
	if p.err != nil {
		return p
	}

	// 节点有效性检查：确保节点不为空
	if node == nil {
		p.err = fmt.Errorf("chain parallel add node invalid, node is nil")
		return p
	}

	// 初始化输出键集合：确保 outputKeys 已初始化
	if p.outputKeys == nil {
		p.outputKeys = make(map[string]bool)
	}

	// 输出键唯一性检查：防止重复键导致结果覆盖
	if _, ok := p.outputKeys[outputKey]; ok {
		p.err = fmt.Errorf("parallel add node err, duplicate output key= %s", outputKey)
		return p
	}

	// 节点信息完整性检查：确保节点信息不为空
	if node.nodeInfo == nil {
		p.err = fmt.Errorf("chain parallel add node invalid, nodeInfo is nil")
		return p
	}

	// 设置节点输出键并注册节点
	node.nodeInfo.outputKey = outputKey
	p.nodes = append(p.nodes, nodeOptionsPair{node, options})
	p.outputKeys[outputKey] = true

	return p
}
