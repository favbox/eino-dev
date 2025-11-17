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

// START 图起始节点标识符，保留字。
const START = "start"

// END 图终止节点标识符，保留字。
const END = "end"

// graphRunType 图运行模式类型。
type graphRunType string

const (
	// runTypePregel Pregel 模式，支持循环结构和动态执行流程。
	// 兼容 NodeTriggerMode.AnyPredecessor（任意前置节点触发）。
	runTypePregel graphRunType = "Pregel"

	// runTypeDAG 有向无环图模式，不允许循环结构。
	// 兼容 NodeTriggerMode.AllPredecessor（所有前置节点完成后触发）。
	runTypeDAG graphRunType = "DAG"
)

// String 返回图运行模式的字符串表示。
func (g graphRunType) String() string {
	return string(g)
}

// graph 图的核心数据结构。
type graph struct {
	nodes        map[string]*graphNode
	controlEdges map[string][]string
	dataEdges    map[string][]string
	branches     map[string][]*GraphBranch
	startNodes   []string
	endNodes     []string

	toValidateMap map[string][]struct {
		endNode  string
		mappings []*FieldMapping
	}

	stateType      reflect.Type
	stateGenerator func(ctx context.Context) any
	newOpts        []NewGraphOption

	expectedInputType, expectedOutputType reflect.Type

	*genericHelper

	fieldMappingRecords map[string][]*FieldMapping

	buildError error
	cmp        component

	compiled bool

	handlerOnEdges   map[string]map[string][]handlerPair
	handlerPreNode   map[string][]handlerPair
	handlerPreBranch map[string][][]handlerPair
}

type newGraphConfig struct {
	inputType, outputType reflect.Type
	gh                    *genericHelper
	cmp                   component
	stateType             reflect.Type
	stateGenerator        func(ctx context.Context) any
	newOpts               []NewGraphOption
}

func newGraphFromGeneric[I, O any](
	cmp component,
	stateGenerator func(ctx context.Context) any,
	stateType reflect.Type,
	opts []NewGraphOption,
) *graph {
	return newGraph(&newGraphConfig{
		inputType:      generic.TypeOf[I](),
		outputType:     generic.TypeOf[O](),
		gh:             newGenericHelper[I, O](),
		cmp:            cmp,
		stateType:      stateType,
		stateGenerator: stateGenerator,
		newOpts:        opts,
	})
}
func newGraph(cfg *newGraphConfig) *graph {
	return &graph{
		nodes:        make(map[string]*graphNode),
		dataEdges:    make(map[string][]string),
		controlEdges: make(map[string][]string),
		branches:     make(map[string][]*GraphBranch),
		toValidateMap: make(map[string][]struct {
			endNode  string
			mappings []*FieldMapping
		}),
		expectedInputType:   cfg.inputType,
		expectedOutputType:  cfg.outputType,
		genericHelper:       cfg.gh,
		fieldMappingRecords: make(map[string][]*FieldMapping),
		cmp:                 cfg.cmp,
		stateType:           cfg.stateType,
		stateGenerator:      cfg.stateGenerator,
		newOpts:             cfg.newOpts,
		handlerOnEdges:      make(map[string]map[string][]handlerPair),
		handlerPreNode:      make(map[string][]handlerPair),
		handlerPreBranch:    make(map[string][][]handlerPair),
	}
}
func (g *graph) component() component {
	return g.cmp
}

func isChain(cmp component) bool {
	return cmp == ComponentOfChain
}

func isWorkflow(cmp component) bool {
	return cmp == ComponentOfWorkflow
}

// ErrGraphCompiled 图编译完成后禁止修改的错误。
var ErrGraphCompiled = errors.New("graph has been compiled, cannot be modified")

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

func (g *graph) addEdgeWithMappings(startNode, endNode string, noControl bool, noData bool, mappings ...*FieldMapping) (err error) {
	if g.buildError != nil {
		return g.buildError
	}
	if g.compiled {
		return ErrGraphCompiled
	}

	if noControl && noData {
		return fmt.Errorf("edge[%s]-[%s] cannot be both noDirectDependency and noDataFlow", startNode, endNode)
	}

	defer func() {
		if err != nil {
			g.buildError = err
		}
	}()

	if startNode == END {
		return errors.New("END cannot be a start node")
	}
	if endNode == START {
		return errors.New("START cannot be an end node")
	}

	if _, ok := g.nodes[startNode]; !ok && startNode != START {
		return fmt.Errorf("edge start node '%s' needs to be added to graph first", startNode)
	}
	if _, ok := g.nodes[endNode]; !ok && endNode != END {
		return fmt.Errorf("edge end node '%s' needs to be added to graph first", endNode)
	}

	if !noControl {
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
func (g *graph) AddChatTemplateNode(key string, node prompt.ChatTemplate, opts ...GraphAddNodeOpt) error {
	gNode, options := toChatTemplateNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddToolsNode 添加工具节点到图中。
func (g *graph) AddToolsNode(key string, node *ToolsNode, opts ...GraphAddNodeOpt) error {
	gNode, options := toToolsNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddDocumentTransformerNode 添加文档转换器节点到图中。
func (g *graph) AddDocumentTransformerNode(key string, node document.Transformer, opts ...GraphAddNodeOpt) error {
	gNode, options := toDocumentTransformerNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddLambdaNode 添加 Lambda 节点到图中。
func (g *graph) AddLambdaNode(key string, node *Lambda, opts ...GraphAddNodeOpt) error {
	gNode, options := toLambdaNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddGraphNode 添加子图节点到图中。
func (g *graph) AddGraphNode(key string, node AnyGraph, opts ...GraphAddNodeOpt) error {
	gNode, options := toAnyGraphNode(node, opts...)
	return g.addNode(key, gNode, options)
}

// AddPassthroughNode 添加透传节点到图中。
func (g *graph) AddPassthroughNode(key string, opts ...GraphAddNodeOpt) error {
	gNode, options := toPassthroughNode(opts...)
	return g.addNode(key, gNode, options)
}

// AddBranch 添加分支到图中。
func (g *graph) AddBranch(startNode string, branch *GraphBranch) (err error) {
	return g.addBranch(startNode, branch, false)
}

func (g *graph) addBranch(startNode string, branch *GraphBranch, skipData bool) (err error) {
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

	if startNode == END {
		return errors.New("END cannot be a start node")
	}

	if _, ok := g.nodes[startNode]; !ok && startNode != START {
		return fmt.Errorf("branch start node '%s' needs to be added to graph first", startNode)
	}

	if _, ok := g.handlerPreBranch[startNode]; !ok {
		g.handlerPreBranch[startNode] = [][]handlerPair{}
	}
	branch.idx = len(g.handlerPreBranch[startNode])

	if startNode != START && g.nodes[startNode].executorMeta.component == ComponentOfPassthrough {
		g.nodes[startNode].cr.inputType = branch.inputType
		g.nodes[startNode].cr.outputType = branch.inputType
		g.nodes[startNode].cr.genericHelper = branch.genericHelper.forPredecessorPassthrough()
	}

	result := checkAssignable(g.getNodeOutputType(startNode), branch.inputType)
	if result == assignableTypeMustNot {
		return fmt.Errorf("condition's input type[%s] and start node[%s]'s output type[%s] are mismatched", branch.inputType.String(), startNode, g.getNodeOutputType(startNode).String())
	} else if result == assignableTypeMay {
		g.handlerPreBranch[startNode] = append(g.handlerPreBranch[startNode], []handlerPair{branch.inputConverter})
	} else {
		g.handlerPreBranch[startNode] = append(g.handlerPreBranch[startNode], []handlerPair{})
	}

	if !skipData {
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
		hasChanged := false
		for startNode := range g.toValidateMap {
			startNodeOutputType = g.getNodeOutputType(startNode)

			for i := 0; i < len(g.toValidateMap[startNode]); i++ {
				endNode := g.toValidateMap[startNode][i]

				endNodeInputType = g.getNodeInputType(endNode.endNode)
				if startNodeOutputType == nil && endNodeInputType == nil {
					continue
				}

				g.toValidateMap[startNode] = append(g.toValidateMap[startNode][:i], g.toValidateMap[startNode][i+1:]...)
				i--

				hasChanged = true

				if startNodeOutputType != nil && endNodeInputType == nil {
					g.nodes[endNode.endNode].cr.inputType = startNodeOutputType
					g.nodes[endNode.endNode].cr.outputType = g.nodes[endNode.endNode].cr.inputType
					g.nodes[endNode.endNode].cr.genericHelper = g.getNodeGenericHelper(startNode).forSuccessorPassthrough()
				} else if startNodeOutputType == nil {
					g.nodes[startNode].cr.inputType = endNodeInputType
					g.nodes[startNode].cr.outputType = g.nodes[startNode].cr.inputType
					g.nodes[startNode].cr.genericHelper = g.getNodeGenericHelper(endNode.endNode).forPredecessorPassthrough()
				} else if len(endNode.mappings) == 0 {
					result := checkAssignable(startNodeOutputType, endNodeInputType)
					if result == assignableTypeMustNot {
						return fmt.Errorf("graph edge[%s]-[%s]: start node's output type[%s] and end node's input type[%s] mismatch",
							startNode, endNode.endNode, startNodeOutputType.String(), endNodeInputType.String())
					} else if result == assignableTypeMay {
						if _, ok := g.handlerOnEdges[startNode]; !ok {
							g.handlerOnEdges[startNode] = make(map[string][]handlerPair)
						}
						g.handlerOnEdges[startNode][endNode.endNode] = append(g.handlerOnEdges[startNode][endNode.endNode], g.getNodeGenericHelper(endNode.endNode).inputConverter)
					}
					continue
				}

				if len(endNode.mappings) > 0 {
					if _, ok := g.handlerOnEdges[startNode]; !ok {
						g.handlerOnEdges[startNode] = make(map[string][]handlerPair)
					}
					g.fieldMappingRecords[endNode.endNode] = append(g.fieldMappingRecords[endNode.endNode], endNode.mappings...)

					checker, uncheckedSourcePaths, err := validateFieldMapping(g.getNodeOutputType(startNode), g.getNodeInputType(endNode.endNode), endNode.mappings)
					if err != nil {
						return err
					}

					g.handlerOnEdges[startNode][endNode.endNode] = append(g.handlerOnEdges[startNode][endNode.endNode], handlerPair{
						invoke: func(value any) (any, error) {
							return fieldMap(endNode.mappings, false, uncheckedSourcePaths)(value)
						},
						transform: streamFieldMap(endNode.mappings, uncheckedSourcePaths),
					})

					if checker != nil {
						g.handlerOnEdges[startNode][endNode.endNode] = append(g.handlerOnEdges[startNode][endNode.endNode], *checker)
					}
				}
			}
		}
		if !hasChanged {
			break
		}
	}

	return nil
}

func (g *graph) getNodeGenericHelper(name string) *genericHelper {
	if name == START {
		return g.genericHelper.forPredecessorPassthrough()
	} else if name == END {
		return g.genericHelper.forSuccessorPassthrough()
	}
	return g.nodes[name].getGenericHelper()
}

func (g *graph) getNodeInputType(name string) reflect.Type {
	if name == START {
		return g.inputType()
	} else if name == END {
		return g.outputType()
	}
	return g.nodes[name].inputType()
}

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

// getSuccessors 获取节点的所有后继节点。
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

// subGraphCompileCallback 子图编译回调结构体。
type subGraphCompileCallback struct {
	closure func(ctx context.Context, info *GraphInfo)
}

// OnFinish 编译完成时执行回调。
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

// validateDAG 使用 Kahn 算法验证 DAG 图的有效性，检测是否存在循环。
func validateDAG(chanSubscribeTo map[string]*chanCall, controlPredecessors map[string][]string) error {
	m := map[string]int{}
	for node := range chanSubscribeTo {
		if edges, ok := controlPredecessors[node]; ok {
			m[node] = len(edges)
			for _, pre := range edges {
				if pre == START {
					m[node] -= 1
				}
			}
		} else {
			m[node] = 0
		}
	}

	hasChanged := true
	for hasChanged {
		hasChanged = false
		for node := range m {
			if m[node] == 0 {
				hasChanged = true
				for _, subNode := range chanSubscribeTo[node].controls {
					if subNode == END {
						continue
					}
					m[subNode]--
				}
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
			loopStarts = append(loopStarts, k)
		}
	}
	if len(loopStarts) > 0 {
		return fmt.Errorf("%w: %s", DAGInvalidLoopErr, formatLoops(findLoops(loopStarts, chanSubscribeTo)))
	}
	return nil
}

// DAGInvalidLoopErr DAG 图中发现循环的错误。
var DAGInvalidLoopErr = errors.New("DAG is invalid, has loop")

// findLoops 使用 DFS 算法查找循环路径。
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

func formatLoops(loops [][]string) string {
	sb := strings.Builder{}
	for _, loop := range loops {
		if len(loop) == 0 {
			continue
		}
		sb.WriteString("[")
		sb.WriteString(loop[0])
		for i := 1; i < len(loop); i++ {
			sb.WriteString("->")
			sb.WriteString(loop[i])
		}
		sb.WriteString("]")
	}
	return sb.String()
}

// NewNodePath 创建节点路径对象，用于指定嵌套图中的节点位置。
func NewNodePath(nodeKeyPath ...string) *NodePath {
	return &NodePath{path: nodeKeyPath}
}

// NodePath 节点路径结构体。
type NodePath struct {
	path []string
}

// GetPath 获取完整路径数组。
func (p *NodePath) GetPath() []string {
	return p.path
}
