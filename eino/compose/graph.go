package compose

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/favbox/eino/components/document"
	"github.com/favbox/eino/components/embedding"
	"github.com/favbox/eino/components/indexer"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/components/retriever"
	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/internal/gmap"
)

// ========== 核心常量定义 ==========

// START 图起始节点标识符 - 所有图的执行入口点，保留字
const START = "start"

// END 图终止节点标识符 - 所有执行路径的终点，保留字
const END = "end"

// ========== 图运行模式定义 ==========

// graphRunType 是控制图运行模式的自定义类型。
// 设计意图：支持两种不同的图执行模式，以适应不同的业务场景。
type graphRunType string

const (
	// runTypePregel 是适合大规模图处理任务的运行模式。
	// 设计意图：支持带有循环的图结构，使用 Pregel 算法思想进行消息传递和迭代计算。
	// 适用场景：
	//   - 支持图中的循环结构
	//   - 适用于动态执行流程
	//   - 大规模图数据处理
	//   - 迭代计算和收敛算法
	// 兼容性：兼容 NodeTriggerType.AnyPredecessor（任意前置节点触发）
	runTypePregel graphRunType = "Pregel"

	// runTypeDAG 是有向无环图的运行模式。
	// 设计意图：表示为有向无环图，适合可以表示为 DAG 的任务。
	// 适用场景：
	//   - 固定执行流程
	//   - 不允许循环的拓扑结构
	//   - 静态图分析
	//   - Workflow 模式的强制执行
	// 兼容性：兼容 NodeTriggerType.AllPredecessor（所有前置节点完成后触发）
	runTypeDAG graphRunType = "DAG"
)

// String 返回图运行模式的字符串表示。
func (g graphRunType) String() string {
	return string(g)
}

// ========== Graph 核心结构体定义 ==========

// graph 是图的核心数据结构，封装了所有与图相关的状态和逻辑。
// 设计意图：提供完整的图构建、验证和编译功能，支持复杂的图结构和执行模式。
// 特点：
//   - 支持动态图构建（Pregel 模式）和静态图分析（DAG 模式）
//   - 完整的状态管理和类型验证机制
//   - 支持数据流、控制流和条件分支
//   - 内置字段映射和类型转换机制
//
// 字段说明：
//   - nodes: 图中所有节点的映射，键为节点键，值为节点对象
//   - controlEdges: 控制边映射，表示节点间的执行依赖关系（但不传递数据）
//   - dataEdges: 数据边映射，表示节点间的数据传递关系
//   - branches: 分支映射，表示条件执行的分支路径
//   - startNodes: 起始节点列表，从 START 开始的直接后继节点
//   - endNodes: 终止节点列表，以 END 结尾的直接前驱节点
//   - toValidateMap: 类型验证映射表，记录待验证的边和字段映射
//   - stateType/stateGenerator: 状态管理相关，支持状态共享和持久化
//   - expectedInputType/expectedOutputType: 图的预期输入输出类型
//   - genericHelper: 通用辅助系统，提供类型转换和流处理能力
//   - fieldMappingRecords: 字段映射记录，支持结构体级别的数据映射
//   - buildError: 构建错误信息，用于错误传播
//   - cmp: 组件类型标识（Graph/Chain/Workflow）
//   - compiled: 编译状态标记，防止重复编译
//   - handler*: 各种处理器映射，支持运行时类型转换和数据处理
type graph struct {
	// 核心图结构
	nodes        map[string]*graphNode     // 节点映射表
	controlEdges map[string][]string       // 控制边映射（执行依赖）
	dataEdges    map[string][]string       // 数据边映射（数据传递）
	branches     map[string][]*GraphBranch // 分支映射（条件执行）
	startNodes   []string                  // 起始节点列表
	endNodes     []string                  // 终止节点列表

	// 类型验证和映射
	toValidateMap map[string][]struct {
		endNode  string
		mappings []*FieldMapping
	} // 待验证映射表

	// 状态管理
	stateType      reflect.Type                  // 状态类型
	stateGenerator func(ctx context.Context) any // 状态生成器
	newOpts        []NewGraphOption              // 图创建选项

	// 类型系统
	expectedInputType, expectedOutputType reflect.Type // 预期输入输出类型

	*genericHelper // 通用辅助系统（类型转换、流处理等）

	// 字段映射
	fieldMappingRecords map[string][]*FieldMapping // 字段映射记录

	// 错误和状态
	buildError error     // 构建错误
	cmp        component // 组件类型标识

	compiled bool // 编译状态标记

	// 处理器映射
	handlerOnEdges   map[string]map[string][]handlerPair // 边上的处理器
	handlerPreNode   map[string][]handlerPair            // 节点前置处理器
	handlerPreBranch map[string][][]handlerPair          // 分支前置处理器
}

// ========== Graph 构造函数定义 ==========

// newGraphConfig 是创建图时的配置结构体。
// 设计意图：封装图创建所需的所有参数，提供类型安全和清晰的接口。
// 字段说明：
//   - inputType/outputType: 图的输入输出类型，基于泛型自动推断
//   - gh: 通用辅助系统，包含类型转换和流处理能力
//   - cmp: 组件类型标识（Graph/Chain/Workflow）
//   - stateType/stateGenerator: 状态管理相关
//   - newOpts: 图创建时的额外选项
type newGraphConfig struct {
	inputType, outputType reflect.Type                  // 图的输入输出类型
	gh                    *genericHelper                // 通用辅助系统
	cmp                   component                     // 组件类型标识
	stateType             reflect.Type                  // 状态类型
	stateGenerator        func(ctx context.Context) any // 状态生成器
	newOpts               []NewGraphOption              // 图创建选项
}

// newGraphFromGeneric 是基于泛型类型创建图的工厂函数。
// 设计意图：提供类型安全的图创建接口，自动处理泛型类型推断。
// 泛型参数：
//   - I: 图的输入类型
//   - O: 图的输出类型
//
// 参数：
//   - cmp: 组件类型标识
//   - stateGenerator: 状态生成器，可选
//   - stateType: 状态类型
//   - opts: 图创建选项列表
//
// 返回：配置完整的图对象
func newGraphFromGeneric[I, O any](
	cmp component,
	stateGenerator func(ctx context.Context) any,
	stateType reflect.Type,
	opts []NewGraphOption,
) *graph {
	return newGraph(&newGraphConfig{
		inputType:      generic.TypeOf[I](),      // 自动推断输入类型
		outputType:     generic.TypeOf[O](),      // 自动推断输出类型
		gh:             newGenericHelper[I, O](), // 创建通用辅助系统
		cmp:            cmp,
		stateType:      stateType,
		stateGenerator: stateGenerator,
		newOpts:        opts,
	})
}

// newGraph 是图的构造函数，基于配置对象创建图实例。
// 设计意图：统一初始化所有图字段，确保图的初始状态一致性。
// 参数：
//   - cfg: 图配置对象，包含所有初始化参数
//
// 返回：初始化完成的图对象，所有字段都已准备好接受节点和边的添加
func newGraph(cfg *newGraphConfig) *graph {
	return &graph{
		// 核心图结构初始化
		nodes:        make(map[string]*graphNode),     // 空节点映射
		dataEdges:    make(map[string][]string),       // 空数据边映射
		controlEdges: make(map[string][]string),       // 空控制边映射
		branches:     make(map[string][]*GraphBranch), // 空分支映射

		// 类型验证和映射
		toValidateMap: make(map[string][]struct {
			endNode  string
			mappings []*FieldMapping
		}), // 空验证映射表

		// 类型系统
		expectedInputType:  cfg.inputType,  // 设置预期输入类型
		expectedOutputType: cfg.outputType, // 设置预期输出类型
		genericHelper:      cfg.gh,         // 设置通用辅助系统

		// 字段映射
		fieldMappingRecords: make(map[string][]*FieldMapping), // 空字段映射记录

		// 错误和状态
		cmp: cfg.cmp, // 设置组件类型

		// 状态管理
		stateType:      cfg.stateType,      // 设置状态类型
		stateGenerator: cfg.stateGenerator, // 设置状态生成器
		newOpts:        cfg.newOpts,        // 设置图选项

		// 处理器映射
		handlerOnEdges:   make(map[string]map[string][]handlerPair), // 边上处理器
		handlerPreNode:   make(map[string][]handlerPair),            // 节点前置处理器
		handlerPreBranch: make(map[string][][]handlerPair),          // 分支前置处理器
	}
}

// component 获取图的组件类型标识 - 返回Graph/Chain/Workflow等类型
func (g *graph) component() component {
	return g.cmp
}

// isChain 检查组件是否为Chain类型
func isChain(cmp component) bool {
	return cmp == ComponentOfChain
}

// isWorkflow 检查组件是否为Workflow类型
func isWorkflow(cmp component) bool {
	return cmp == ComponentOfWorkflow
}

// ErrGraphCompiled 图编译完成后禁止修改错误 - 尝试修改已编译的图时返回
var ErrGraphCompiled = errors.New("graph has been compiled, cannot be modified")

// addNode 向图中添加节点 - 执行节点添加的核心逻辑和验证检查
func (g *graph) addNode(key string, node *graphNode, options *graphAddNodeOpts) (err error) {
	if g.buildError != nil {
		return g.buildError
	}

	if g.compiled {
		return ErrGraphCompiled
	}

	defer func() {
		if err != nil {
			g.buildError = err
		}
	}()

	if key == END || key == START {
		return fmt.Errorf("node '%s' is reserved, cannot add manually", key)
	}

	if _, ok := g.nodes[key]; ok {
		return fmt.Errorf("node '%s' already present", key)
	}

	// check options
	if options.needState {
		if g.stateGenerator == nil {
			return fmt.Errorf("node '%s' needs state but graph state is not enabled", key)
		}
	}

	if options.nodeOptions.nodeKey != "" {
		if !isChain(g.cmp) {
			return errors.New("only chain support node key option")
		}
	}
	// end: check options

	// check pre- / post-handler type
	if options.processor != nil {
		if options.processor.statePreHandler != nil {
			// check state type
			if g.stateType != options.processor.preStateType {
				return fmt.Errorf("node[%s]'s pre handler state type[%v] is different from graph[%v]", key, options.processor.preStateType, g.stateType)
			}
			// check input type
			if node.inputType() == nil && options.processor.statePreHandler.outputType != reflect.TypeOf((*any)(nil)).Elem() {
				return fmt.Errorf("passthrough node[%s]'s pre handler type isn't any", key)
			} else if node.inputType() != nil && node.inputType() != options.processor.statePreHandler.outputType {
				return fmt.Errorf("node[%s]'s pre handler type[%v] is different from its input type[%v]", key, options.processor.statePreHandler.outputType, node.inputType())
			}
		}
		if options.processor.statePostHandler != nil {
			// check state type
			if g.stateType != options.processor.postStateType {
				return fmt.Errorf("node[%s]'s post handler state type[%v] is different from graph[%v]", key, options.processor.postStateType, g.stateType)
			}
			// check input type
			if node.outputType() == nil && options.processor.statePostHandler.inputType != reflect.TypeOf((*any)(nil)).Elem() {
				return fmt.Errorf("passthrough node[%s]'s post handler type isn't any", key)
			} else if node.outputType() != nil && node.outputType() != options.processor.statePostHandler.inputType {
				return fmt.Errorf("node[%s]'s post handler type[%v] is different from its output type[%v]", key, options.processor.statePostHandler.inputType, node.outputType())
			}
		}
	}

	g.nodes[key] = node

	return nil
}

// ========== 核心执行抽象：边添加方法 ==========

// addEdgeWithMappings 是图构建的核心方法，用于在节点间添加边并处理类型映射。
// 设计意图：建立节点间的连接关系，支持控制边和数据边两种类型的边，并自动处理类型验证和映射。
// 参数：
//   - startNode: 起始节点键，边的起点
//   - endNode: 终止节点键，边的终点
//   - noControl: 是否禁用控制边（仅数据边）
//   - noData: 是否禁用数据边（仅控制边）
//   - mappings: 字段映射列表，用于结构体级别的数据转换
//
// 返回：
//   - error: 边添加过程中的错误，包括节点不存在、边重复、类型不匹配等
//
// 处理流程：
//  1. 预检查：验证图编译状态和输入参数合法性
//  2. 节点存在性检查：确保起始和终止节点都已添加到图中
//  3. 控制边处理：添加执行依赖关系，更新 startNodes/endNodes
//  4. 数据边处理：添加数据传递关系，启动类型推断验证
//  5. 字段映射：支持结构体级别的数据转换和验证
//
// 关键特性：
//   - START/END 虚拟节点：自动处理图的入口和出口
//   - 去重检查：防止重复添加相同的边
//   - 类型推断：自动推断节点的输入输出类型
//   - 字段映射：支持灵活的字段级数据映射
//   - 错误传播：累积构建过程中的所有错误
func (g *graph) addEdgeWithMappings(startNode, endNode string, noControl bool, noData bool, mappings ...*FieldMapping) (err error) {
	// 错误传播：如果之前有构建错误，直接返回
	if g.buildError != nil {
		return g.buildError
	}
	// 检查图是否已编译，编译后的图不能修改
	if g.compiled {
		return ErrGraphCompiled
	}

	// 参数验证：边不能同时禁用控制和数据依赖
	if noControl && noData {
		return fmt.Errorf("edge[%s]-[%s] cannot be both noDirectDependency and noDataFlow", startNode, endNode)
	}

	// 错误累积：将当前错误加入构建错误
	defer func() {
		if err != nil {
			g.buildError = err
		}
	}()

	// ========== 步骤1: 虚拟节点验证 ==========
	// 验证 START/END 虚拟节点的正确使用
	if startNode == END {
		return errors.New("END cannot be a start node")
	}
	if endNode == START {
		return errors.New("START cannot be an end node")
	}

	// ========== 步骤2: 节点存在性检查 ==========
	// 确保起始和终止节点都已添加到图中
	if _, ok := g.nodes[startNode]; !ok && startNode != START {
		return fmt.Errorf("edge start node '%s' needs to be added to graph first", startNode)
	}
	if _, ok := g.nodes[endNode]; !ok && endNode != END {
		return fmt.Errorf("edge end node '%s' needs to be added to graph first", endNode)
	}

	// ========== 步骤3: 控制边处理 ==========
	// 添加执行依赖关系，但不在节点间传递数据
	if !noControl {
		// 检查控制边是否已存在（去重）
		for i := range g.controlEdges[startNode] {
			if g.controlEdges[startNode][i] == endNode {
				return fmt.Errorf("control edge[%s]-[%s] have been added yet", startNode, endNode)
			}
		}

		// 添加控制边
		g.controlEdges[startNode] = append(g.controlEdges[startNode], endNode)
		// 更新图的起始和终止节点列表
		if startNode == START {
			g.startNodes = append(g.startNodes, endNode)
		}
		if endNode == END {
			g.endNodes = append(g.endNodes, startNode)
		}
	}

	// ========== 步骤4: 数据边处理 ==========
	// 添加数据传递关系，触发类型推断和验证
	if !noData {
		// 检查数据边是否已存在（去重）
		for i := range g.dataEdges[startNode] {
			if g.dataEdges[startNode][i] == endNode {
				return fmt.Errorf("data edge[%s]-[%s] have been added yet", startNode, endNode)
			}
		}

		// 将边加入验证映射表，启动类型推断
		g.addToValidateMap(startNode, endNode, mappings)
		// 执行类型验证和推断
		err = g.updateToValidateMap()
		if err != nil {
			return err
		}
		// 添加数据边
		g.dataEdges[startNode] = append(g.dataEdges[startNode], endNode)
	}

	return nil // 边添加成功
}

// ========== 组件节点添加方法：嵌入器 ==========

// AddEmbeddingNode 添加嵌入器节点到图中。
// 设计意图：将嵌入器组件封装为图节点，支持在图结构中使用文本嵌入功能
func (g *graph) AddEmbeddingNode(key string, node embedding.Embedder, opts ...GraphAddNodeOpt) error {
	gNode, options := toEmbeddingNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// ========== 组件节点添加方法：检索器 ==========

// AddRetrieverNode 添加检索器节点到图中。
// 设计意图：将检索器组件封装为图节点，支持在图结构中使用信息检索功能
func (g *graph) AddRetrieverNode(key string, node retriever.Retriever, opts ...GraphAddNodeOpt) error {
	gNode, options := toRetrieverNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// ========== 组件节点添加方法：文档加载器 ==========

// AddLoaderNode 添加文档加载器节点到图中。
// 设计意图：将文档加载器组件封装为图节点，支持在图结构中使用文档加载功能
func (g *graph) AddLoaderNode(key string, node document.Loader, opts ...GraphAddNodeOpt) error {
	gNode, options := toLoaderNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// ========== 组件节点添加方法：索引器 ==========

// AddIndexerNode 添加索引器节点到图中。
// 设计意图：将索引器组件封装为图节点，支持在图结构中使用向量索引功能。
func (g *graph) AddIndexerNode(key string, node indexer.Indexer, opts ...GraphAddNodeOpt) error {
	gNode, options := toIndexerNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// ========== 组件节点添加方法：聊天模型 ==========

// AddChatModelNode 添加聊天模型节点到图中。
// 设计意图：将聊天模型组件封装为图节点，支持在图结构中使用大语言模型功能
func (g *graph) AddChatModelNode(key string, node model.BaseChatModel, opts ...GraphAddNodeOpt) error {
	gNode, options := toChatModelNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddChatTemplateNode 添加聊天模板节点到图中。
// 设计意图：将聊天模板组件封装为图节点，支持结构化消息构建和参数化提示词
func (g *graph) AddChatTemplateNode(key string, node prompt.ChatTemplate, opts ...GraphAddNodeOpt) error {
	gNode, options := toChatTemplateNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddToolsNode 添加工具节点到图中。
// 设计意图：将工具节点组件封装为图节点，支持在图结构中动态管理工具集合
func (g *graph) AddToolsNode(key string, node *ToolsNode, opts ...GraphAddNodeOpt) error {
	gNode, options := toToolsNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddDocumentTransformerNode 添加文档转换器节点到图中。
// 设计意图：将文档转换器组件封装为图节点，支持在图结构中处理文档格式转换
func (g *graph) AddDocumentTransformerNode(key string, node document.Transformer, opts ...GraphAddNodeOpt) error {
	gNode, options := toDocumentTransformerNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddLambdaNode 添加 Lambda 节点到图中。
// 设计意图：将 Lambda 函数封装为图节点，支持四种执行模式的灵活组合
func (g *graph) AddLambdaNode(key string, node *Lambda, opts ...GraphAddNodeOpt) error {
	gNode, options := toLambdaNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddGraphNode 添加子图节点到图中。
// 设计意图：将子图（Graph/Chain/StateChain）封装为节点，支持图的嵌套组合和模块化设计
func (g *graph) AddGraphNode(key string, node AnyGraph, opts ...GraphAddNodeOpt) error {
	gNode, options := toAnyGraphNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddPassthroughNode 添加透传节点到图中。
// 设计意图：将透传节点封装为图节点，用于在图中传递数据而不进行任何处理
func (g *graph) AddPassthroughNode(key string, opts ...GraphAddNodeOpt) error {
	gNode, options := toPassthroughNode(opts...)
	return g.addNode(key, gNode, options)
}

// AddBranch 添加分支到图中。
// 设计意图：为图添加条件执行分支，支持基于条件的动态路径选择
func (g *graph) AddBranch(startNode string, branch *GraphBranch) (err error) {
	return g.addBranch(startNode, branch, false)
}

// addBranch 向图中添加分支 - 执行分支添加的核心逻辑和类型检查
func (g *graph) addBranch(startNode string, branch *GraphBranch, skipData bool) (err error) {
	// 错误传播：如果之前有构建错误，直接返回
	if g.buildError != nil {
		return g.buildError
	}

	// 检查图是否已编译，编译后的图不能修改
	if g.compiled {
		return ErrGraphCompiled
	}

	// 错误累积：将当前错误加入构建错误
	defer func() {
		if err != nil {
			g.buildError = err
		}
	}()

	// 验证 START/END 虚拟节点的正确使用
	if startNode == END {
		return errors.New("END cannot be a start node")
	}

	// 确保起始节点已添加到图中
	if _, ok := g.nodes[startNode]; !ok && startNode != START {
		return fmt.Errorf("branch start node '%s' needs to be added to graph first", startNode)
	}

	// 初始化分支前置处理器映射
	if _, ok := g.handlerPreBranch[startNode]; !ok {
		g.handlerPreBranch[startNode] = [][]handlerPair{}
	}
	branch.idx = len(g.handlerPreBranch[startNode])

	// 透传节点类型推断：如果起始节点是透传节点，更新其类型
	if startNode != START && g.nodes[startNode].executorMeta.component == ComponentOfPassthrough {
		g.nodes[startNode].cr.inputType = branch.inputType
		g.nodes[startNode].cr.outputType = branch.inputType
		g.nodes[startNode].cr.genericHelper = branch.genericHelper.forPredecessorPassthrough()
	}

	// 分支条件类型检查：验证起始节点输出类型与分支输入类型的兼容性
	result := checkAssignable(g.getNodeOutputType(startNode), branch.inputType)
	if result == assignableTypeMustNot {
		return fmt.Errorf("condition's input type[%s] and start node[%s]'s output type[%s] are mismatched", branch.inputType.String(), startNode, g.getNodeOutputType(startNode).String())
	} else if result == assignableTypeMay {
		// 类型可能兼容，添加运行时转换处理器
		g.handlerPreBranch[startNode] = append(g.handlerPreBranch[startNode], []handlerPair{branch.inputConverter})
	} else {
		// 类型完全兼容，无需额外处理器
		g.handlerPreBranch[startNode] = append(g.handlerPreBranch[startNode], []handlerPair{})
	}

	if !skipData {
		// 数据流处理：验证每个分支结束节点，更新类型推断和节点列表
		for endNode := range branch.endNodes {
			if _, ok := g.nodes[endNode]; !ok {
				if endNode != END {
					return fmt.Errorf("branch end node '%s' needs to be added to graph first", endNode)
				}
			}

			// 触发类型验证和推断
			g.addToValidateMap(startNode, endNode, nil)
			e := g.updateToValidateMap()
			if e != nil {
				return e
			}

			// 更新图的起始和终止节点列表
			if startNode == START {
				g.startNodes = append(g.startNodes, endNode)
			}
			if endNode == END {
				g.endNodes = append(g.endNodes, startNode)
			}
		}
	} else {
		// 跳过数据流处理：仅更新节点列表，标记无数据流
		for endNode := range branch.endNodes {
			if startNode == START {
				g.startNodes = append(g.startNodes, endNode)
			}
			if endNode == END {
				g.endNodes = append(g.endNodes, startNode)
			}
		}
		branch.noDataFlow = true
	}

	// 将分支添加到图的分支映射中
	g.branches[startNode] = append(g.branches[startNode], branch)

	return nil
}

// addToValidateMap 将边添加到待验证映射表 - 启动类型推断流程
func (g *graph) addToValidateMap(startNode, endNode string, mapping []*FieldMapping) {
	g.toValidateMap[startNode] = append(g.toValidateMap[startNode], struct {
		endNode  string
		mappings []*FieldMapping
	}{endNode: endNode, mappings: mapping})
}

// ========== 核心执行抽象：类型推断和验证方法 ==========

// updateToValidateMap 在节点更新后检查验证映射表，执行类型推断和验证。
// 设计意图：通过迭代算法自动推断节点的输入输出类型，是图构建过程中类型系统的核心机制。
// 触发时机：每次添加边时自动调用，支持延迟类型推断
// 算法原理：基于固定点迭代的链式类型推断
//  1. 遍历待验证映射表中的所有边
//  2. 检查起始和终止节点是否有已知的类型
//  3. 单向推断：如果只有一端有类型，自动推断另一端的类型
//  4. 双向验证：如果两端都有类型，验证类型兼容性
//  5. 重复迭代：直到没有更多类型信息可以推断（固定点）
//
// 关键特性：
//   - 链式推断：支持多个 passthrough 节点的链式类型传播
//   - 迭代算法：在最坏情况下每次只更新一个节点，逐步收敛
//   - 类型安全：严格验证类型兼容性，拒绝不兼容的类型连接
//   - 字段映射：支持结构体级别的字段映射和类型转换
//   - 运行时检查：对于可能兼容但不明确的类型，添加运行时检查
//
// 错误处理：
//   - 类型不匹配：明确指出不兼容的类型和位置
//   - 无法推断：编译时发现类型推断失败，阻止构建完成
func (g *graph) updateToValidateMap() error {
	var startNodeOutputType, endNodeInputType reflect.Type
	// ========== 固定点迭代循环 ==========
	// 持续迭代直到没有更多类型信息可以推断
	for {
		hasChanged := false // 标记本轮是否有类型更新
		// 遍历所有待验证的起始节点
		for startNode := range g.toValidateMap {
			// 获取起始节点的输出类型
			startNodeOutputType = g.getNodeOutputType(startNode)

			// 遍历该起始节点的所有终止节点
			for i := 0; i < len(g.toValidateMap[startNode]); i++ {
				endNode := g.toValidateMap[startNode][i]

				// 获取终止节点的输入类型
				endNodeInputType = g.getNodeInputType(endNode.endNode)
				// 如果两端都未知类型，跳过等待更多信息
				if startNodeOutputType == nil && endNodeInputType == nil {
					continue
				}

				// ========== 从待验证映射表中移除已处理的边 ==========
				// 将当前边从映射表中移除，准备处理
				g.toValidateMap[startNode] = append(g.toValidateMap[startNode][:i], g.toValidateMap[startNode][i+1:]...)
				i-- // 调整索引，补偿被移除的元素

				hasChanged = true // 标记本轮有处理

				// ========== 步骤1: 单向类型推断 ==========
				// 假设 START 和 END 的类型不会为空（已有明确类型）
				// 情况1: 起始节点有类型，终止节点未知类型
				if startNodeOutputType != nil && endNodeInputType == nil {
					// 推断终止节点的输入输出类型（passthrough 节点输入输出相同）
					g.nodes[endNode.endNode].cr.inputType = startNodeOutputType
					g.nodes[endNode.endNode].cr.outputType = g.nodes[endNode.endNode].cr.inputType
					// 更新通用辅助系统
					g.nodes[endNode.endNode].cr.genericHelper = g.getNodeGenericHelper(startNode).forSuccessorPassthrough()
				} else if startNodeOutputType == nil /* && endNodeInputType != nil */ {
					// ========== 步骤2: 单向类型推断（反向） ==========
					// 情况2: 起始节点未知类型，终止节点有类型
					// 推断起始节点的输入输出类型（passthrough 节点输入输出相同）
					g.nodes[startNode].cr.inputType = endNodeInputType
					g.nodes[startNode].cr.outputType = g.nodes[startNode].cr.inputType
					// 更新通用辅助系统
					g.nodes[startNode].cr.genericHelper = g.getNodeGenericHelper(endNode.endNode).forPredecessorPassthrough()
				} else if len(endNode.mappings) == 0 {
					// ========== 步骤3: 双向类型验证 ==========
					// 情况3: 两端都有类型，且没有字段映射
					// 通用节点类型检查
					result := checkAssignable(startNodeOutputType, endNodeInputType)
					// 类型完全不兼容
					if result == assignableTypeMustNot {
						return fmt.Errorf("graph edge[%s]-[%s]: start node's output type[%s] and end node's input type[%s] mismatch",
							startNode, endNode.endNode, startNodeOutputType.String(), endNodeInputType.String())
					} else if result == assignableTypeMay {
						// ========== 类型可能兼容，需要运行时检查 ==========
						// 添加运行时类型检查边
						if _, ok := g.handlerOnEdges[startNode]; !ok {
							g.handlerOnEdges[startNode] = make(map[string][]handlerPair)
						}
						g.handlerOnEdges[startNode][endNode.endNode] = append(g.handlerOnEdges[startNode][endNode.endNode], g.getNodeGenericHelper(endNode.endNode).inputConverter)
					}
					// ========== 类型完全兼容，无需额外处理 ==========
					continue
				}

				// ========== 步骤3: 字段映射处理 ==========
				// 情况4: 存在字段映射，需要特殊处理
				if len(endNode.mappings) > 0 {
					// 初始化边处理器映射
					if _, ok := g.handlerOnEdges[startNode]; !ok {
						g.handlerOnEdges[startNode] = make(map[string][]handlerPair)
					}
					// 记录字段映射到目标节点
					g.fieldMappingRecords[endNode.endNode] = append(g.fieldMappingRecords[endNode.endNode], endNode.mappings...)

					// 验证字段映射的合法性
					checker, uncheckedSourcePaths, err := validateFieldMapping(g.getNodeOutputType(startNode), g.getNodeInputType(endNode.endNode), endNode.mappings)
					if err != nil {
						return err
					}

					// 创建字段映射处理器（支持同步和流式转换）
					g.handlerOnEdges[startNode][endNode.endNode] = append(g.handlerOnEdges[startNode][endNode.endNode], handlerPair{
						invoke: func(value any) (any, error) {
							return fieldMap(endNode.mappings, false, uncheckedSourcePaths)(value)
						},
						transform: streamFieldMap(endNode.mappings, uncheckedSourcePaths),
					})

					// 如果有额外的类型检查器，添加到处理器链
					if checker != nil {
						g.handlerOnEdges[startNode][endNode.endNode] = append(g.handlerOnEdges[startNode][endNode.endNode], *checker)
					}
				}
			}
		}
		// ========== 迭代终止条件 ==========
		// 如果本轮没有类型更新，说明已达到固定点，可以终止
		if !hasChanged {
			break
		}
	}

	return nil // 类型推断和验证成功完成
}

// getNodeGenericHelper 获取节点通用辅助系统 - 处理START/END虚拟节点
func (g *graph) getNodeGenericHelper(name string) *genericHelper {
	if name == START {
		return g.genericHelper.forPredecessorPassthrough()
	} else if name == END {
		return g.genericHelper.forSuccessorPassthrough()
	}
	return g.nodes[name].getGenericHelper()
}

// getNodeInputType 获取节点输入类型 - 处理START/END虚拟节点
func (g *graph) getNodeInputType(name string) reflect.Type {
	if name == START {
		return g.inputType()
	} else if name == END {
		return g.outputType()
	}
	return g.nodes[name].inputType()
}

// getNodeOutputType 获取节点输出类型 - 处理START/END虚拟节点
func (g *graph) getNodeOutputType(name string) reflect.Type {
	if name == START {
		return g.inputType()
	} else if name == END {
		return g.outputType()
	}
	return g.nodes[name].outputType()
}

// inputType 获取图的输入类型
func (g *graph) inputType() reflect.Type {
	return g.expectedInputType
}

// outputType 获取图的输出类型
func (g *graph) outputType() reflect.Type {
	return g.expectedOutputType
}

// ========== 核心执行抽象：图编译方法 ==========

// compile 是图编译的核心方法，将构建好的图转换为可执行的 runnable。
// 设计意图：完成从静态图结构到动态执行引擎的转换，是执行抽象层的关键环节。
// 参数：
//   - ctx: 上下文对象，用于传递编译过程中的取消和超时信息
//   - opt: 编译选项，包含中断控制、检查点、合并配置等
//
// 返回：
//   - *composableRunnable: 编译后的可执行对象，支持四种执行模式
//   - error: 编译过程中的错误
//
// 编译流程：
//  1. 运行模式选择：根据图类型和配置选择 Pregel 或 DAG 模式
//  2. 前置验证：检查图的基本完整性（起始节点、终止节点、类型推断）
//  3. 节点编译：将每个节点编译为可执行的 chanCall
//  4. 关系构建：构建前置节点映射和后续节点映射
//  5. 执行引擎创建：创建 runner 对象，配置执行参数
//  6. DAG 验证：对 DAG 模式进行循环检测
//  7. 默认配置：设置默认的执行步数限制
//
// 关键机制：
//   - 两种运行模式：Pregel（支持循环）+ DAG（静态分析）
//   - Eager 模式：Workflow 和 DAG 强制启用，预执行所有节点
//   - 检查点机制：支持断点恢复和状态持久化
//   - 中断控制：支持优雅和强制中断
func (g *graph) compile(ctx context.Context, opt *graphCompileOptions) (*composableRunnable, error) {
	// 错误传播：如果之前有构建错误，直接返回
	if g.buildError != nil {
		return nil, g.buildError
	}

	// ========== 步骤1: 运行模式选择 ==========
	// 默认使用 Pregel 模式（支持动态图和循环结构）
	runType := runTypePregel
	cb := pregelChannelBuilder
	// Chain 和 Workflow 不支持节点触发模式选项
	if isChain(g.cmp) || isWorkflow(g.cmp) {
		if opt != nil && opt.nodeTriggerMode != "" {
			return nil, errors.New(fmt.Sprintf("%s doesn't support node trigger mode option", g.cmp))
		}
	}
	// Workflow 或显式要求 AllPredecessor 触发时，使用 DAG 模式
	if (opt != nil && opt.nodeTriggerMode == AllPredecessor) || isWorkflow(g.cmp) {
		runType = runTypeDAG
		cb = dagChannelBuilder
	}

	// ========== 步骤2: Eager 模式选择 ==========
	// Eager 模式：预执行所有节点，不等待数据触发
	eager := false
	if isWorkflow(g.cmp) || runType == runTypeDAG {
		eager = true // Workflow 和 DAG 强制启用 Eager 模式
	}
	if opt != nil && opt.eagerDisabled {
		eager = false // 用户可选择禁用 Eager 模式
	}

	// ========== 步骤3: 前置验证 ==========
	// 验证图的基本完整性
	if len(g.startNodes) == 0 {
		return nil, errors.New("start node not set")
	}
	if len(g.endNodes) == 0 {
		return nil, errors.New("end node not set")
	}

	// 验证所有节点的类型推断是否完成
	// toValidateMap 非空表示有节点无法推断类型
	for _, v := range g.toValidateMap {
		if len(v) > 0 {
			return nil, fmt.Errorf("some node's input or output types cannot be inferred: %v", g.toValidateMap)
		}
	}

	// ========== 步骤4: 字段映射验证 ==========
	// 验证字段映射的合法性
	for key := range g.fieldMappingRecords {
		// 不允许将多个字段映射到同一个目标字段
		toMap := make(map[string]bool)
		for _, mapping := range g.fieldMappingRecords[key] {
			if _, ok := toMap[mapping.to]; ok {
				return nil, fmt.Errorf("duplicate mapping target field: %s of node[%s]", mapping.to, key)
			}
			toMap[mapping.to] = true
		}

		// 将字段映射转换器添加到节点前置处理器
		g.handlerPreNode[key] = append(g.handlerPreNode[key], g.getNodeGenericHelper(key).inputFieldMappingConverter)
	}

	// ========== 步骤5: 节点编译 ==========
	// 将每个节点编译为可执行的 chanCall
	key2SubGraphs := g.beforeChildGraphsCompile(opt)
	chanSubscribeTo := make(map[string]*chanCall)
	for name, node := range g.nodes {
		node.beforeChildGraphCompile(name, key2SubGraphs)

		// 编译节点为可执行对象
		r, err := node.compileIfNeeded(ctx)
		if err != nil {
			return nil, err
		}

		// 创建 chanCall，封装节点执行逻辑和连接信息
		chCall := &chanCall{
			action:   r,                    // 节点执行器
			writeTo:  g.dataEdges[name],    // 数据边连接
			controls: g.controlEdges[name], // 控制边连接

			preProcessor:  node.nodeInfo.preProcessor,  // 前置处理器
			postProcessor: node.nodeInfo.postProcessor, // 后置处理器
		}

		// 添加分支信息
		branches := g.branches[name]
		if len(branches) > 0 {
			branchRuns := make([]*GraphBranch, 0, len(branches))
			branchRuns = append(branchRuns, branches...)
			chCall.writeToBranches = branchRuns
		}

		chanSubscribeTo[name] = chCall
	}

	dataPredecessors := make(map[string][]string)
	controlPredecessors := make(map[string][]string)
	for start, ends := range g.controlEdges {
		for _, end := range ends {
			if _, ok := controlPredecessors[end]; !ok {
				controlPredecessors[end] = []string{start}
			} else {
				controlPredecessors[end] = append(controlPredecessors[end], start)
			}
		}
	}
	for start, ends := range g.dataEdges {
		for _, end := range ends {
			if _, ok := dataPredecessors[end]; !ok {
				dataPredecessors[end] = []string{start}
			} else {
				dataPredecessors[end] = append(dataPredecessors[end], start)
			}
		}
	}
	for start, branches := range g.branches {
		for _, branch := range branches {
			for end := range branch.endNodes {
				if _, ok := controlPredecessors[end]; !ok {
					controlPredecessors[end] = []string{start}
				} else {
					controlPredecessors[end] = append(controlPredecessors[end], start)
				}

				if !branch.noDataFlow {
					if _, ok := dataPredecessors[end]; !ok {
						dataPredecessors[end] = []string{start}
					} else {
						dataPredecessors[end] = append(dataPredecessors[end], start)
					}
				}
			}
		}
	}

	inputChannels := &chanCall{
		writeTo:         g.dataEdges[START],
		controls:        g.controlEdges[START],
		writeToBranches: make([]*GraphBranch, len(g.branches[START])),
	}
	copy(inputChannels.writeToBranches, g.branches[START])

	var mergeConfigs map[string]FanInMergeConfig
	if opt != nil {
		mergeConfigs = opt.mergeConfigs
	}
	if mergeConfigs == nil {
		mergeConfigs = make(map[string]FanInMergeConfig)
	}

	r := &runner{
		chanSubscribeTo:     chanSubscribeTo,
		controlPredecessors: controlPredecessors,
		dataPredecessors:    dataPredecessors,

		inputChannels: inputChannels,

		eager: eager,

		chanBuilder: cb,

		inputType:     g.inputType(),
		outputType:    g.outputType(),
		genericHelper: g.genericHelper,

		preBranchHandlerManager: &preBranchHandlerManager{h: g.handlerPreBranch},
		preNodeHandlerManager:   &preNodeHandlerManager{h: g.handlerPreNode},
		edgeHandlerManager:      &edgeHandlerManager{h: g.handlerOnEdges},

		mergeConfigs: mergeConfigs,
	}

	successors := make(map[string][]string)
	for ch := range r.chanSubscribeTo {
		successors[ch] = getSuccessors(r.chanSubscribeTo[ch])
	}
	r.successors = successors

	if g.stateGenerator != nil {
		r.runCtx = func(ctx context.Context) context.Context {
			return context.WithValue(ctx, stateKey{}, &internalState{
				state: g.stateGenerator(ctx),
			})
		}
	}

	if runType == runTypeDAG {
		err := validateDAG(r.chanSubscribeTo, controlPredecessors)
		if err != nil {
			return nil, err
		}
		r.dag = true
	}

	if opt != nil {
		inputPairs := make(map[string]streamConvertPair)
		outputPairs := make(map[string]streamConvertPair)
		for key, c := range r.chanSubscribeTo {
			inputPairs[key] = c.action.inputStreamConvertPair
			outputPairs[key] = c.action.outputStreamConvertPair
		}
		inputPairs[END] = r.outputConvertStreamPair
		outputPairs[START] = r.inputConvertStreamPair
		r.checkPointer = newCheckPointer(inputPairs, outputPairs, opt.checkPointStore, opt.serializer)

		r.interruptBeforeNodes = opt.interruptBeforeNodes
		r.interruptAfterNodes = opt.interruptAfterNodes
		r.options = *opt
	}

	// default options
	if r.dag && r.options.maxRunSteps > 0 {
		return nil, fmt.Errorf("cannot set max run steps in dag mode")
	} else if !r.dag && r.options.maxRunSteps == 0 {
		r.options.maxRunSteps = len(r.chanSubscribeTo) + 10
	}

	g.compiled = true

	g.onCompileFinish(ctx, opt, key2SubGraphs)

	return r.toComposableRunnable(), nil
}

// getSuccessors 获取节点的所有后继节点 - 包括数据边、控制边和分支边连接的所有节点
func getSuccessors(c *chanCall) []string {
	ret := make([]string, len(c.writeTo))
	copy(ret, c.writeTo)
	ret = append(ret, c.controls...)
	for _, branch := range c.writeToBranches {
		for node := range branch.endNodes {
			ret = append(ret, node)
		}
	}
	return ret
}

// subGraphCompileCallback 子图编译回调结构体 - 用于在子图编译完成时执行回调
type subGraphCompileCallback struct {
	closure func(ctx context.Context, info *GraphInfo)
}

// OnFinish 编译完成时执行回调
func (s *subGraphCompileCallback) OnFinish(ctx context.Context, info *GraphInfo) {
	s.closure(ctx, info)
}

// beforeChildGraphsCompile 子图编译前准备操作 - 为回调机制初始化映射表
func (g *graph) beforeChildGraphsCompile(opt *graphCompileOptions) map[string]*GraphInfo {
	if opt == nil || len(opt.callbacks) == 0 {
		return nil
	}

	return make(map[string]*GraphInfo)
}

// beforeChildGraphCompile 节点编译前准备操作 - 为子图节点设置编译回调
func (gn *graphNode) beforeChildGraphCompile(nodeKey string, key2SubGraphs map[string]*GraphInfo) {
	if gn.g == nil || key2SubGraphs == nil {
		return
	}

	subGraphCallback := func(ctx2 context.Context, subGraph *GraphInfo) {
		key2SubGraphs[nodeKey] = subGraph
	}

	gn.nodeInfo.compileOption.callbacks = append(gn.nodeInfo.compileOption.callbacks, &subGraphCompileCallback{closure: subGraphCallback})
}

// toGraphInfo 将图转换为图信息对象 - 收集图的所有信息用于调试和可视化
func (g *graph) toGraphInfo(opt *graphCompileOptions, key2SubGraphs map[string]*GraphInfo) *GraphInfo {
	gInfo := &GraphInfo{
		CompileOptions: opt.origOpts,
		Nodes:          make(map[string]GraphNodeInfo, len(g.nodes)),
		Edges:          gmap.Clone(g.controlEdges),
		DataEdges:      gmap.Clone(g.dataEdges),
		Branches: gmap.Map(g.branches, func(startNode string, branches []*GraphBranch) (string, []GraphBranch) {
			branchInfo := make([]GraphBranch, 0, len(branches))
			for _, b := range branches {
				branchInfo = append(branchInfo, GraphBranch{
					invoke:        b.invoke,
					collect:       b.collect,
					inputType:     b.inputType,
					genericHelper: b.genericHelper,
					endNodes:      gmap.Clone(b.endNodes),
				})
			}
			return startNode, branchInfo
		}),
		InputType:       g.expectedInputType,
		OutputType:      g.expectedOutputType,
		Name:            opt.graphName,
		GenStateFn:      g.stateGenerator,
		NewGraphOptions: g.newOpts,
	}

	for key := range g.nodes {
		gNode := g.nodes[key]
		if gNode.executorMeta.component == ComponentOfPassthrough {
			gInfo.Nodes[key] = GraphNodeInfo{
				Component:        gNode.executorMeta.component,
				GraphAddNodeOpts: gNode.opts,
				InputType:        gNode.cr.inputType,
				OutputType:       gNode.cr.outputType,
				Name:             gNode.nodeInfo.name,
				InputKey:         gNode.cr.nodeInfo.inputKey,
				OutputKey:        gNode.cr.nodeInfo.outputKey,
			}
			continue
		}

		gNodeInfo := &GraphNodeInfo{
			Component:        gNode.executorMeta.component,
			Instance:         gNode.instance,
			GraphAddNodeOpts: gNode.opts,
			InputType:        gNode.cr.inputType,
			OutputType:       gNode.cr.outputType,
			Name:             gNode.nodeInfo.name,
			InputKey:         gNode.cr.nodeInfo.inputKey,
			OutputKey:        gNode.cr.nodeInfo.outputKey,
			Mappings:         g.fieldMappingRecords[key],
		}

		if gi, ok := key2SubGraphs[key]; ok {
			gNodeInfo.GraphInfo = gi
		}

		gInfo.Nodes[key] = *gNodeInfo
	}

	return gInfo
}

// onCompileFinish 编译完成时执行回调 - 通知所有注册的回调处理器
func (g *graph) onCompileFinish(ctx context.Context, opt *graphCompileOptions, key2SubGraphs map[string]*GraphInfo) {
	if opt == nil {
		return
	}

	if len(opt.callbacks) == 0 {
		return
	}

	gInfo := g.toGraphInfo(opt, key2SubGraphs)

	for _, cb := range opt.callbacks {
		cb.OnFinish(ctx, gInfo)
	}
}

// getGenericHelper 获取图的通用辅助系统
func (g *graph) getGenericHelper() *genericHelper {
	return g.genericHelper
}

// GetType 获取图的类型 - 留空实现，子类可覆盖
func (g *graph) GetType() string {
	return ""
}

// transferTask 任务转移优化算法 - 将无依赖的任务前移，提高并行执行效率
// 设计意图：通过任务重新排序优化图执行性能，将无依赖的任务组前移
// 算法思路：从后往前遍历任务组，找到无依赖的任务并将其转移到合适的位置
func transferTask(script [][]string, invertedEdges map[string][]string) [][]string {
	utilMap := map[string]bool{}
	// 从后往前遍历，优先处理后面的任务
	for i := len(script) - 1; i >= 0; i-- {
		for j := 0; j < len(script[i]); j++ {
			// 去重：跳过已处理的节点
			if _, ok := utilMap[script[i][j]]; ok {
				script[i] = append(script[i][:j], script[i][j+1:]...)
				j--
				continue
			}
			utilMap[script[i][j]] = true

			target := i
			// 查找可转移的位置：向后查找无依赖的任务组
			for k := i + 1; k < len(script); k++ {
				hasDependencies := false
				for l := range script[k] {
					for _, dependency := range invertedEdges[script[i][j]] {
						if script[k][l] == dependency {
							hasDependencies = true
							break
						}
					}
					if hasDependencies {
						break
					}
				}
				if hasDependencies {
					break
				}
				target = k
			}
			// 任务转移：将无依赖的任务前移
			if target != i {
				script[target] = append(script[target], script[i][j])
				script[i] = append(script[i][:j], script[i][j+1:]...)
				j--
			}
		}
	}

	return script
}

// ========== 核心执行抽象：DAG 验证方法 ==========

// validateDAG 使用拓扑排序算法验证 DAG 图的有效性，检测是否存在循环。
// 设计意图：确保 DAG 模式的图结构严格无环，是静态分析模式的核心安全机制。
// 参数：
//   - chanSubscribeTo: 节点订阅映射，包含所有可执行节点和其连接信息
//   - controlPredecessors: 控制前置节点映射，记录每个节点的直接前置依赖
//
// 返回：
//   - error: 如果发现循环，返回详细错误信息；否则返回 nil
//
// 算法原理：基于 Kahn 算法（拓扑排序的改进版）
//  1. 计算每个节点的入度（前置节点数量）
//  2. 从入度为 0 的节点开始，逐步移除
//  3. 检查是否有节点无法被移除（入度始终 > 0）
//  4. 无法移除的节点即构成循环
//
// 关键处理：
//   - START 节点不入度计算（虚拟起始点）
//   - END 节点不出度计算（虚拟终止点）
//   - 同时考虑控制边和分支边的依赖关系
//
// 错误信息：
//   - 包含所有循环的路径，便于调试和修正
//   - 格式化输出显示循环路径，如 [A->B->C->A]
func validateDAG(chanSubscribeTo map[string]*chanCall, controlPredecessors map[string][]string) error {
	// ========== 步骤1: 初始化节点入度 ==========
	// 计算每个节点的初始入度（前置依赖数量）
	m := map[string]int{}
	for node := range chanSubscribeTo {
		if edges, ok := controlPredecessors[node]; ok {
			m[node] = len(edges) // 设置初始入度
			// START 节点是虚拟起始点，不计入入度
			for _, pre := range edges {
				if pre == START {
					m[node] -= 1
				}
			}
		} else {
			m[node] = 0 // 无前置依赖的节点入度为 0
		}
	}

	// ========== 步骤2: Kahn 算法拓扑排序 ==========
	// 逐步移除入度为 0 的节点，检测循环
	hasChanged := true
	for hasChanged {
		hasChanged = false
		for node := range m {
			// 找到入度为 0 的节点（无前置依赖，可以执行）
			if m[node] == 0 {
				hasChanged = true
				// 移除该节点，更新其后续节点的入度
				for _, subNode := range chanSubscribeTo[node].controls {
					if subNode == END {
						continue // END 是虚拟终止点，不处理
					}
					m[subNode]-- // 减少后续节点的入度
				}
				// 处理分支边
				for _, subBranch := range chanSubscribeTo[node].writeToBranches {
					for subNode := range subBranch.endNodes {
						if subNode == END {
							continue
						}
						m[subNode]--
					}
				}
				m[node] = -1 // 标记为已处理
			}
		}
	}

	// ========== 步骤3: 检测循环 ==========
	// 检查是否有节点未被移除（入度 > 0），这些节点构成循环
	var loopStarts []string
	for k, v := range m {
		if v > 0 {
			loopStarts = append(loopStarts, k) // 记录循环的起始节点
		}
	}
	// 如果发现循环，返回详细错误信息
	if len(loopStarts) > 0 {
		return fmt.Errorf("%w: %s", DAGInvalidLoopErr, formatLoops(findLoops(loopStarts, chanSubscribeTo)))
	}
	return nil // 验证通过，无循环
}

// DAGInvalidLoopErr DAG模式循环错误 - DAG图中发现循环时返回此错误
var DAGInvalidLoopErr = errors.New("DAG is invalid, has loop")

// findLoops 查找循环路径 - 使用DFS算法遍历图结构检测循环
func findLoops(startNodes []string, chanCalls map[string]*chanCall) [][]string {
	// 构建控制后继关系图
	controlSuccessors := map[string][]string{}
	for node, ch := range chanCalls {
		controlSuccessors[node] = append(controlSuccessors[node], ch.controls...)
		for _, b := range ch.writeToBranches {
			for end := range b.endNodes {
				controlSuccessors[node] = append(controlSuccessors[node], end)
			}
		}
	}

	visited := map[string]bool{}
	var dfs func(path []string) [][]string
	dfs = func(path []string) [][]string {
		var ret [][]string
		pathEnd := path[len(path)-1]
		successors, ok := controlSuccessors[pathEnd]
		if !ok {
			return nil
		}
		for _, successor := range successors {
			visited[successor] = true

			if successor == END {
				continue
			}

			var looped bool
			// 循环检测：检查路径中是否已存在当前后继节点
			for i, node := range path {
				if node == successor {
					ret = append(ret, append(path[i:], successor))
					looped = true
					break
				}
			}
			if looped {
				continue
			}

			// 递归遍历：从当前后继节点继续搜索
			ret = append(ret, dfs(append(path, successor))...)
		}
		return ret
	}

	var ret [][]string
	for _, node := range startNodes {
		if !visited[node] {
			ret = append(ret, dfs([]string{node})...)
		}
	}
	return ret
}

// formatLoops 格式化循环路径 - 将检测到的循环转换为可读字符串表示
// 设计意图：将DFS算法找到的循环路径数组转换为清晰的错误信息格式
// 算法思路：遍历每个循环路径，用 "->" 连接节点，用 "[]" 包裹整个循环
func formatLoops(loops [][]string) string {
	sb := strings.Builder{}
	for _, loop := range loops {
		if len(loop) == 0 {
			continue
		}
		sb.WriteString("[")
		sb.WriteString(loop[0])
		// 循环路径连接：用箭头连接路径中的每个节点
		for i := 1; i < len(loop); i++ {
			sb.WriteString("->")
			sb.WriteString(loop[i])
		}
		sb.WriteString("]")
	}
	return sb.String()
}

// NewNodePath 创建节点路径对象 - 用于精确指定嵌套图中的节点位置
// 设计意图：支持多级路径指定，如 "sub_graph_node_key", "node_key_within_sub_graph"
func NewNodePath(nodeKeyPath ...string) *NodePath {
	return &NodePath{path: nodeKeyPath}
}

// NodePath 节点路径结构体 - 封装路径信息
type NodePath struct {
	path []string
}

// GetPath 获取完整路径数组 - 返回路径中的所有节点键
func (p *NodePath) GetPath() []string {
	return p.path
}
