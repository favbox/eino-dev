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

/*
 * chain_branch.go - 链式分支模式实现
 *
 * 核心组件：
 *   - ChainBranch: 链式分支容器，支持条件路由和多路径执行
 *   - nodeOptionsPair: 节点与选项的配对结构
 *   - NewChainBranch/NewStreamChainBranch: 单分支工厂函数
 *   - NewChainMultiBranch/NewStreamChainMultiBranch: 多分支工厂函数
 *   - AddXXX 方法族: 为分支添加节点，支持10+种组件类型
 *
 * 设计特点：
 *   - 条件驱动路由：根据条件函数动态选择执行路径
 *   - 多分支支持：同时支持单分支和多分支两种模式
 *   - 流式分支：支持基于流式输入的分支判断
 *   - 类型安全：通过泛型确保编译期类型检查
 *   - 建造者模式：链式 API 设计，支持流畅调用
 *
 * 与其他文件关系：
 *   - 为 chain.go 提供分支构建能力
 *   - 封装 GraphBranch 的通用分支逻辑
 *   - 继承 branch.go 的分支条件判断机制
 *   - 与 component_to_graph_node.go 协作转换组件
 *
 * 使用场景：
 *   - 条件路由：根据用户输入选择不同处理路径
 *   - 多模型选择：根据成本或性能选择不同模型
 *   - A/B 测试：分流不同逻辑进行测试
 *   - 错误恢复：根据错误类型选择不同处理策略
 *   - 动态工作流：根据运行时状态调整执行流程
 *
 * 分支模式：
 *   1. 单分支：根据条件返回单一路径
 *      适合：二选一的简单场景
 *
 *   2. 多分支：根据条件返回多个路径（并行执行）
 *      适合：需要同时执行多个路径的复杂场景
 *
 *   3. 流式分支：基于流式输入的分支判断
 *      适合：需要根据流数据特征进行路由的场景
 */

// ====== 节点选项配对 ======

// nodeOptionsPair 节点与选项配对 - 封装 graphNode 和其对应的选项
// 统一管理节点的执行器和配置信息，便于在分支中批量处理
type nodeOptionsPair generic.Pair[*graphNode, *graphAddNodeOpts]

// ====== 分支容器结构 ======

// ChainBranch 链式分支容器 - 封装条件判断和节点集合
// 支持动态路由和多路径执行的分支容器，每个分支可以包含多个节点
// 分支的所有节点应遵循：要么结束 Chain，要么汇聚到其他节点
type ChainBranch struct {
	// 内部分支：封装条件判断逻辑和路径选择
	// 由 GraphBranch 实现具体的分支执行机制
	internalBranch *GraphBranch
	// 节点映射：分支中所有节点的键值对映射
	// 用于快速查找和管理分支内的节点
	key2BranchNode map[string]nodeOptionsPair
	// 构建错误：累积构建过程中的错误，避免中途失败
	err error
}

// ====== 多分支工厂函数 ======

// NewChainMultiBranch 创建多分支实例 - 基于多分支条件函数
// 支持根据输入动态选择多个执行路径，适用于复杂的条件判断场景
// 输入类型 T 由泛型参数指定，条件函数返回多个可能的结束节点
func NewChainMultiBranch[T any](cond GraphMultiBranchCondition[T]) *ChainBranch {
	// 转换函数：将 map[bool] 转换为 []string 格式
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

// NewStreamChainMultiBranch 创建流式多分支实例 - 基于流式输入的多分支条件
// 支持根据流式输入的特征进行多路径选择，适用于流式数据处理
// 流式分支可以读取流的前几个块来做出路径选择决策
func NewStreamChainMultiBranch[T any](cond StreamGraphMultiBranchCondition[T]) *ChainBranch {
	// 转换函数：将流式输入转换为 Collect 模式的条件函数
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

// ====== 单分支工厂函数 ======

// NewChainBranch 创建单分支实例 - 基于单分支条件函数
// 根据条件函数返回单一路径，是最常用的分支创建方式
// 条件函数接收输入类型 T，返回一个结束节点的键
// 示例：用于二选一的简单场景，如成本优先 vs 质量优先的模型选择
func NewChainBranch[T any](cond GraphBranchCondition[T]) *ChainBranch {
	return NewChainMultiBranch(func(ctx context.Context, in T) (endNode map[string]bool, err error) {
		ret, err := cond(ctx, in)
		if err != nil {
			return nil, err
		}
		return map[string]bool{ret: true}, nil
	})
}

// NewStreamChainBranch 创建流式单分支实例 - 基于流式输入的单分支条件
// 根据流式输入的特征返回单一路径，适用于流式数据处理
// 可以读取流的前几个块来做出决策，但最终选择一个执行路径
// 典型场景：基于流内容的类型识别或内容分类
func NewStreamChainBranch[T any](cond StreamGraphBranchCondition[T]) *ChainBranch {
	return NewStreamChainMultiBranch(func(ctx context.Context, in *schema.StreamReader[T]) (endNodes map[string]bool, err error) {
		ret, err := cond(ctx, in)
		if err != nil {
			return nil, err
		}
		return map[string]bool{ret: true}, nil
	})
}

// ====== 组件节点添加方法族 ======

// AddChatModel 添加聊天模型节点 - 在分支中添加大语言模型处理路径
// 支持不同模型的选择，如根据成本选择 gpt-4o-mini，根据质量选择 gpt-4o
// 典型场景：基于用户查询复杂度选择不同性能的模型
func (cb *ChainBranch) AddChatModel(key string, node model.BaseChatModel, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toChatModelNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddChatTemplate 添加聊天模板节点 - 在分支中添加提示词模板处理路径
// 支持不同上下文下的模板切换，如通用助手 vs 专业领域助手
// 典型场景：基于用户意图选择不同的系统提示词
func (cb *ChainBranch) AddChatTemplate(key string, node prompt.ChatTemplate, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toChatTemplateNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddToolsNode 添加工具节点 - 在分支中添加工具调用处理路径
// 支持不同工具集的选择，如基础工具 vs 专业工具
// 典型场景：基于用户需求选择可用的工具集合
func (cb *ChainBranch) AddToolsNode(key string, node *ToolsNode, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toToolsNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddLambda 添加 Lambda 节点 - 在分支中添加自定义逻辑处理路径
// 支持在分支中插入自定义的数据处理逻辑
// 典型场景：基于条件动态选择不同的数据转换策略
func (cb *ChainBranch) AddLambda(key string, node *Lambda, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toLambdaNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddEmbedding 添加嵌入模型节点 - 在分支中添加文本向量化处理路径
// 支持不同嵌入模型的选择，如高维度 vs 低维度嵌入
// 典型场景：基于精度要求选择不同的嵌入模型
func (cb *ChainBranch) AddEmbedding(key string, node embedding.Embedder, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toEmbeddingNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddRetriever 添加检索器节点 - 在分支中添加信息检索处理路径
// 支持不同检索策略的选择，如关键词检索 vs 向量检索
// 典型场景：基于查询类型选择最适合的检索方式
func (cb *ChainBranch) AddRetriever(key string, node retriever.Retriever, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toRetrieverNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddLoader 添加加载器节点 - 在分支中添加文档加载处理路径
// 支持不同文档类型的加载器选择
// 典型场景：基于文件类型选择对应的解析器
func (cb *ChainBranch) AddLoader(key string, node document.Loader, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toLoaderNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddIndexer 添加索引器节点 - 在分支中添加文档索引处理路径
// 支持不同索引策略的选择
// 典型场景：基于数据规模选择索引方式
func (cb *ChainBranch) AddIndexer(key string, node indexer.Indexer, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toIndexerNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddDocumentTransformer 添加文档转换器节点 - 在分支中添加文档转换处理路径
// 支持不同转换策略的选择，如分块、清洗、格式化
// 典型场景：基于文档特征选择合适的处理方式
func (cb *ChainBranch) AddDocumentTransformer(key string, node document.Transformer, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toDocumentTransformerNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddGraph 添加子图节点 - 在分支中添加复杂图结构处理路径
// 支持嵌套图的分支执行，适合复杂业务逻辑
// 典型场景：基于条件选择不同的子流程执行
func (cb *ChainBranch) AddGraph(key string, node AnyGraph, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toAnyGraphNode(node, opts...)
	return cb.addNode(key, gNode, options)
}

// AddPassthrough 添加直通节点 - 在分支中添加数据透传处理路径
// 输入数据直接传递到输出，不做任何处理
// 典型场景：作为分支的结束标记或数据传递节点
func (cb *ChainBranch) AddPassthrough(key string, opts ...GraphAddNodeOpt) *ChainBranch {
	gNode, options := toPassthroughNode(opts...)
	return cb.addNode(key, gNode, options)
}

// ====== 内部辅助方法 ======

// addNode 添加节点到分支 - 分支节点的统一注册方法
// 检查节点键的唯一性，避免重复添加
// 错误处理：累积构建错误，支持优雅降级
func (cb *ChainBranch) addNode(key string, node *graphNode, options *graphAddNodeOpts) *ChainBranch {
	// 错误检查：如果已有错误，直接返回避免继续添加
	if cb.err != nil {
		return cb
	}

	// 初始化节点映射：确保 key2BranchNode 已初始化
	if cb.key2BranchNode == nil {
		cb.key2BranchNode = make(map[string]nodeOptionsPair)
	}

	// 唯一性检查：防止重复键导致覆盖
	_, ok := cb.key2BranchNode[key]
	if ok {
		cb.err = fmt.Errorf("chain branch add node, duplicate branch node key= %s", key)
		return cb
	}

	// 注册节点：将节点和选项存入映射
	cb.key2BranchNode[key] = nodeOptionsPair{node, options}

	return cb
}
