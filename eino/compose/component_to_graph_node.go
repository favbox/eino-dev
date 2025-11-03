package compose

import (
	"github.com/favbox/eino/components"
	"github.com/favbox/eino/components/document"
	"github.com/favbox/eino/components/embedding"
	"github.com/favbox/eino/components/indexer"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/components/retriever"
)

/*
 * component_to_graph_node.go - 组件到图节点转换适配器
 *
 * 核心组件：
 *   - toComponentNode: 通用组件转换器（Invoke/Stream/Collect/Transform）
 *   - 8种具体组件转换器：Embedder/Retriever/Loader/Indexer/ChatModel/Prompt/Transformer/ToolsNode
 *   - 特殊节点转换器：Lambda/AnyGraph/Passthrough
 *   - toNode: 底层 graphNode 构造器
 *
 * 设计特点：
 *   - 统一转换接口：将 components 包的各种组件转换为统一的 graphNode
 *   - 多范式支持：同步（Invoke）、流式（Stream）、收集（Collect）、转换（Transform）
 *   - 类型安全转换：通过泛型确保编译期类型检查
 *   - 元数据提取：自动解析组件的执行器信息和回调配置
 *
 * 与其他文件关系：
 *   - 为 graph.go 提供节点构造能力
 *   - 连接 components（组件定义）和 compose（图编排）两个包
 *   - 支持 Workflow/Chain/Graph 的节点添加
 *
 * 使用场景：
 *   - 将模型组件（ChatModel, Embedding）转换为图节点
 *   - 将工具组件（Retriever, Indexer）转换为图节点
 *   - 将提示词组件（Prompt）转换为图节点
 *   - 将文档处理组件（Loader, Transformer）转换为图节点
 *   - 将自定义 Lambda 和 AnyGraph 转换为图节点
 */

// ====== 通用组件转换器 ======

// toComponentNode 通用组件转换器 - 将组件转换为图节点
// 支持四种执行范式：同步调用、流式、收集、转换
func toComponentNode[I, O, TOption any](
	node any,
	componentType component,
	invoke Invoke[I, O, TOption],
	stream Stream[I, O, TOption],
	collect Collect[I, O, TOption],
	transform Transform[I, O, TOption],
	opts ...GraphAddNodeOpt,
) (*graphNode, *graphAddNodeOpts) {
	meta := parseExecutorInfoFromComponent(componentType, node)
	info, options := getNodeInfo(opts...)
	run := runnableLambda(invoke, stream, collect, transform,
		!meta.isComponentCallbackEnabled,
	)

	gn := toNode(info, run, nil, meta, node, opts...)

	return gn, options
}

// ====== 组件转换器实现 ======

// toEmbeddingNode 嵌入模型转换器
func toEmbeddingNode(node embedding.Embedder, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	return toComponentNode(
		node,
		components.ComponentOfEmbedding,
		node.EmbedStrings,
		nil,
		nil,
		nil,
		opts...)
}

// toRetrieverNode 检索器转换器
func toRetrieverNode(node retriever.Retriever, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	return toComponentNode(
		node,
		components.ComponentOfRetriever,
		node.Retrieve,
		nil,
		nil,
		nil,
		opts...)
}

// toLoaderNode 文档加载器转换器
func toLoaderNode(node document.Loader, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	return toComponentNode(
		node,
		components.ComponentOfLoader,
		node.Load,
		nil,
		nil,
		nil,
		opts...)
}

// toIndexerNode 索引器转换器
func toIndexerNode(node indexer.Indexer, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	return toComponentNode(
		node,
		components.ComponentOfIndexer,
		node.Store,
		nil,
		nil,
		nil,
		opts...)
}

// toChatModelNode 聊天模型转换器 - 支持流式输出
func toChatModelNode(node model.BaseChatModel, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	return toComponentNode(
		node,
		components.ComponentOfChatModel,
		node.Generate,
		node.Stream,
		nil,
		nil,
		opts...)
}

// toChatTemplateNode 提示词模板转换器
func toChatTemplateNode(node prompt.ChatTemplate, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	return toComponentNode(
		node,
		components.ComponentOfPrompt,
		node.Format,
		nil,
		nil,
		nil,
		opts...)
}

// toDocumentTransformerNode 文档转换器转换器
func toDocumentTransformerNode(node document.Transformer, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	return toComponentNode(
		node,
		components.ComponentOfTransformer,
		node.Transform,
		nil,
		nil,
		nil,
		opts...)
}

// toToolsNode 工具节点转换器
func toToolsNode(node *ToolsNode, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	return toComponentNode(
		node,
		ComponentOfToolsNode,
		node.Invoke,
		node.Stream,
		nil,
		nil,
		opts...)
}

// ====== 特殊节点转换器 ======

// toLambdaNode Lambda 函数转换器
func toLambdaNode(node *Lambda, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	info, options := getNodeInfo(opts...)

	gn := toNode(info, node.executor, nil, node.executor.meta, node, opts...)

	return gn, options
}

// toAnyGraphNode 任意图转换器
func toAnyGraphNode(node AnyGraph, opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	meta := parseExecutorInfoFromComponent(node.component(), node)
	info, options := getNodeInfo(opts...)

	gn := toNode(info, nil, node, meta, node, opts...)

	return gn, options
}

// toPassthroughNode 直通节点转换器
func toPassthroughNode(opts ...GraphAddNodeOpt) (*graphNode, *graphAddNodeOpts) {
	node := composablePassthrough()
	info, options := getNodeInfo(opts...)
	gn := toNode(info, node, nil, node.meta, node, opts...)
	return gn, options
}

// ====== 底层节点构造器 ======

// toNode 构造 graphNode 实例 - 底层构造器
func toNode(nodeInfo *nodeInfo, executor *composableRunnable, graph AnyGraph,
	meta *executorMeta, instance any, opts ...GraphAddNodeOpt) *graphNode {

	if meta == nil {
		meta = &executorMeta{}
	}

	gn := &graphNode{
		nodeInfo: nodeInfo,

		cr:           executor,
		g:            graph,
		executorMeta: meta,

		instance: instance,
		opts:     opts,
	}

	return gn
}
