package compose

import (
	"context"
	"fmt"
	"reflect"

	"github.com/favbox/eino/components/document"
	"github.com/favbox/eino/components/embedding"
	"github.com/favbox/eino/components/indexer"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/components/retriever"
	"github.com/favbox/eino/schema"
)

/*
 * workflow.go - 工作流模式实现
 *
 * 核心组件：
 *   - WorkflowNode: 工作流节点，支持依赖声明和字段映射
 *   - Workflow: 基于泛型的工作流构建器，封装 Graph 实现
 *   - dependencyType: 三种依赖类型（普通/非直接/分支）
 *   - WorkflowBranch: 工作流分支封装
 *
 * 设计特点：
 *   - 字段级映射：支持精细到字段级别的数据流控制
 *   - 依赖分离：将执行依赖与数据依赖解耦
 *   - 链式构建：支持方法链式调用
 *   - 编译优化：构建时验证依赖合法性
 *
 * 与其他文件关系：
 *   - 封装 generic_graph.go 的 Graph 实现
 *   - 集成 field_mapping.go 的字段映射系统
 *   - 提供比 Chain 更高层次的编排能力
 *
 * 使用场景：
 *   - 精细化数据流控制：仅传递需要的字段
 *   - 复杂依赖管理：独立配置执行顺序和数据流向
 *   - 可视化编排：清晰的依赖声明式构建
 */

// ====== Workflow 节点定义 ======

// WorkflowNode 工作流节点 - 支持依赖声明和字段映射的节点
// 每个节点可独立配置数据来源和执行顺序，支持静态值和字段映射
type WorkflowNode struct {
	g                *graph
	key              string
	addInputs        []func() error
	staticValues     map[string]any
	dependencySetter func(fromNodeKey string, typ dependencyType)
	mappedFieldPath  map[string]any
}

// ====== Workflow 定义 ======

// Workflow 工作流构建器 - 替代 AddEdge 的声明式依赖管理
// 基于 NodeTriggerMode(AllPredecessor)，不支持循环
// 支持三种依赖类型：普通依赖、非直接依赖、分支依赖
type Workflow[I, O any] struct {
	g                *graph
	workflowNodes    map[string]*WorkflowNode
	workflowBranches []*WorkflowBranch
	dependencies     map[string]map[string]dependencyType
}

// ====== 依赖类型定义 ======

// dependencyType 依赖类型枚举 - 定义节点间的依赖关系
type dependencyType int

const (
	// normalDependency 普通依赖 - 同时建立数据和执行依赖
	normalDependency dependencyType = iota

	// noDirectDependency 非直接依赖 - 仅建立数据映射，不建立直接执行依赖
	noDirectDependency

	// branchDependency 分支依赖 - 来自分支结构的依赖
	branchDependency
)

// ====== Workflow 工厂方法 ======

// NewWorkflow 创建工作流实例 - 支持泛型输入输出类型
func NewWorkflow[I, O any](opts ...NewGraphOption) *Workflow[I, O] {
	options := &newGraphOptions{}
	for _, opt := range opts {
		opt(options)
	}

	wf := &Workflow[I, O]{
		g: newGraphFromGeneric[I, O](
			ComponentOfWorkflow,
			options.withState,
			options.stateType,
			opts,
		),
		workflowNodes: make(map[string]*WorkflowNode),
		dependencies:  make(map[string]map[string]dependencyType),
	}

	return wf
}

// Compile 编译工作流为可执行对象
func (wf *Workflow[I, O]) Compile(ctx context.Context, opts ...GraphCompileOption) (Runnable[I, O], error) {
	return compileAnyGraph[I, O](ctx, wf, opts...)
}

// ====== Workflow 节点添加方法族 ======

// AddChatModelNode 添加聊天模型节点
func (wf *Workflow[I, O]) AddChatModelNode(key string, chatModel model.BaseChatModel, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddChatModelNode(key, chatModel, opts...)
	return wf.initNode(key)
}

// AddChatTemplateNode 添加聊天模板节点
func (wf *Workflow[I, O]) AddChatTemplateNode(key string, chatTemplate prompt.ChatTemplate, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddChatTemplateNode(key, chatTemplate, opts...)
	return wf.initNode(key)
}

// AddToolsNode 添加工具节点
func (wf *Workflow[I, O]) AddToolsNode(key string, tools *ToolsNode, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddToolsNode(key, tools, opts...)
	return wf.initNode(key)
}

// AddRetrieverNode 添加检索器节点
func (wf *Workflow[I, O]) AddRetrieverNode(key string, retriever retriever.Retriever, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddRetrieverNode(key, retriever, opts...)
	return wf.initNode(key)
}

// AddEmbeddingNode 添加嵌入模型节点
func (wf *Workflow[I, O]) AddEmbeddingNode(key string, embedding embedding.Embedder, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddEmbeddingNode(key, embedding, opts...)
	return wf.initNode(key)
}

// AddIndexerNode 添加索引器节点
func (wf *Workflow[I, O]) AddIndexerNode(key string, indexer indexer.Indexer, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddIndexerNode(key, indexer, opts...)
	return wf.initNode(key)
}

// AddLoaderNode 添加文档加载器节点
func (wf *Workflow[I, O]) AddLoaderNode(key string, loader document.Loader, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddLoaderNode(key, loader, opts...)
	return wf.initNode(key)
}

// AddDocumentTransformerNode 添加文档转换器节点
func (wf *Workflow[I, O]) AddDocumentTransformerNode(key string, transformer document.Transformer, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddDocumentTransformerNode(key, transformer, opts...)
	return wf.initNode(key)
}

// AddGraphNode 添加图节点（支持链或图）
func (wf *Workflow[I, O]) AddGraphNode(key string, graph AnyGraph, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddGraphNode(key, graph, opts...)
	return wf.initNode(key)
}

// AddLambdaNode 添加 Lambda 节点
func (wf *Workflow[I, O]) AddLambdaNode(key string, lambda *Lambda, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddLambdaNode(key, lambda, opts...)
	return wf.initNode(key)
}

// End 获取 END 节点 - 用于连接工作流结束
func (wf *Workflow[I, O]) End() *WorkflowNode {
	if node, ok := wf.workflowNodes[END]; ok {
		return node
	}
	return wf.initNode(END)
}

// AddPassthroughNode 添加直通节点
func (wf *Workflow[I, O]) AddPassthroughNode(key string, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddPassthroughNode(key, opts...)
	return wf.initNode(key)
}

// ====== WorkflowNode 依赖管理方法 ======

// AddInput 添加节点依赖 - 同时建立数据依赖和执行依赖
// 配置数据从前置节点到当前节点的流向，确保当前节点在前置节点完成后才执行
// 参数:
//   - fromNodeKey: 前置节点键
//   - inputs: 字段映射列表，指定数据流向；为空时使用整个前置输出
//
// 示例:
func (n *WorkflowNode) AddInput(fromNodeKey string, inputs ...*FieldMapping) *WorkflowNode {
	return n.addDependencyRelation(fromNodeKey, inputs, &workflowAddInputOpts{})
}

// workflowAddInputOpts 依赖添加选项 - 控制依赖建立的细节
type workflowAddInputOpts struct {
	// noDirectDependency 非直接依赖 - 仅建立数据映射，不建立直接执行依赖
	noDirectDependency bool
	// dependencyWithoutInput 纯执行依赖 - 仅建立执行依赖，不进行数据映射
	dependencyWithoutInput bool
}

// WorkflowAddInputOpt 依赖添加选项函数类型
type WorkflowAddInputOpt func(*workflowAddInputOpts)

// getAddInputOpts 解析依赖添加选项
func getAddInputOpts(opts []WorkflowAddInputOpt) *workflowAddInputOpts {
	opt := &workflowAddInputOpts{}
	for _, o := range opts {
		o(opt)
	}
	return opt
}

// WithNoDirectDependency 非直接依赖选项 - 分离执行依赖与数据依赖
// 前置节点仍会在当前节点前完成，但通过间接路径而非直接依赖
// 设计原理：
//  1. 节点依赖有两个目的：执行顺序和数据流
//  2. 此选项将两者分离：建立数据映射，但执行顺序通过其他节点间接保证
//
// 重要使用场景：
//   - 分支场景：连接分支两侧的节点时必须使用，避免绕过分支的错误依赖
//   - 避免冗余依赖：当已存在路径时，使用间接依赖减少复杂度
func WithNoDirectDependency() WorkflowAddInputOpt {
	return func(opt *workflowAddInputOpts) {
		opt.noDirectDependency = true
	}
}

// AddInputWithOptions 添加节点依赖（自定义选项）- 精细化控制依赖关系
func (n *WorkflowNode) AddInputWithOptions(fromNodeKey string, inputs []*FieldMapping, opts ...WorkflowAddInputOpt) *WorkflowNode {
	return n.addDependencyRelation(fromNodeKey, inputs, getAddInputOpts(opts))
}

// AddDependency 添加执行依赖 - 仅建立执行顺序，不传递数据
// 当前节点等待前置节点完成，但不接收其数据
// 使用场景：
//   - 初始化依赖：前置节点执行setup，后置节点等待完成后开始
//   - 状态同步：确保执行顺序，但无数据传递
func (n *WorkflowNode) AddDependency(fromNodeKey string) *WorkflowNode {
	return n.addDependencyRelation(fromNodeKey, nil, &workflowAddInputOpts{dependencyWithoutInput: true})
}

// SetStaticValue 设置静态值 - 编译期确定的常量值
// 整个工作流生命周期内保持不变
// 示例：设置固定查询参数、配置值等
func (n *WorkflowNode) SetStaticValue(path FieldPath, value any) *WorkflowNode {
	n.staticValues[path.join()] = value
	return n
}

// ====== 内部依赖处理方法 ======

// addDependencyRelation 添加依赖关系 - 根据选项类型分发处理逻辑
// 延迟执行：通过 addInputs 列表延迟到编译时执行，避免构建时立即验证
func (n *WorkflowNode) addDependencyRelation(fromNodeKey string, inputs []*FieldMapping, options *workflowAddInputOpts) *WorkflowNode {
	// 绑定源节点键：设置字段映射的源节点
	for _, input := range inputs {
		input.fromNodeKey = fromNodeKey
	}

	// 非直接依赖：建立数据映射但无直接执行依赖
	if options.noDirectDependency {
		n.addInputs = append(n.addInputs, func() error {
			var paths []FieldPath
			for _, input := range inputs {
				paths = append(paths, input.targetPath())
			}
			if err := n.checkAndAddMappedPath(paths); err != nil {
				return err
			}

			// addEdgeWithMappings 参数：skipFrom=true, skipTo=false
			if err := n.g.addEdgeWithMappings(fromNodeKey, n.key, true, false, inputs...); err != nil {
				return err
			}
			n.dependencySetter(fromNodeKey, noDirectDependency)
			return nil
		})
	} else if options.dependencyWithoutInput {
		// 纯执行依赖：仅建立执行顺序，不进行数据映射
		n.addInputs = append(n.addInputs, func() error {
			if len(inputs) > 0 {
				return fmt.Errorf("dependency without input should not have inputs. node: %s, fromNode: %s, inputs: %v", n.key, fromNodeKey, inputs)
			}
			// addEdgeWithMappings 参数：skipFrom=false, skipTo=true，无字段映射
			if err := n.g.addEdgeWithMappings(fromNodeKey, n.key, false, true); err != nil {
				return err
			}
			n.dependencySetter(fromNodeKey, normalDependency)
			return nil
		})
	} else {
		// 普通依赖：同时建立数据和执行依赖
		n.addInputs = append(n.addInputs, func() error {
			var paths []FieldPath
			for _, input := range inputs {
				paths = append(paths, input.targetPath())
			}
			if err := n.checkAndAddMappedPath(paths); err != nil {
				return err
			}

			// addEdgeWithMappings 参数：skipFrom=false, skipTo=false，完整的字段映射
			if err := n.g.addEdgeWithMappings(fromNodeKey, n.key, false, false, inputs...); err != nil {
				return err
			}
			n.dependencySetter(fromNodeKey, normalDependency)
			return nil
		})
	}

	return n
}

// checkAndAddMappedPath 验证并添加字段映射路径 - 防止映射冲突
// 通过树形结构追踪已映射的字段路径，确保无重复映射
func (n *WorkflowNode) checkAndAddMappedPath(paths []FieldPath) error {
	// 初始化映射追踪结构
	if v, ok := n.mappedFieldPath[""]; ok {
		if _, ok = v.(struct{}); ok {
			return fmt.Errorf("entire output has already been mapped for node: %s", n.key)
		}
	} else {
		// 无路径时标记为整个输出已映射
		if len(paths) == 0 {
			n.mappedFieldPath[""] = struct{}{}
			return nil
		} else {
			// 有路径时初始化为映射树结构
			n.mappedFieldPath[""] = map[string]any{}
		}
	}

	// 逐路径验证和构建映射树
	for _, targetPath := range paths {
		m := n.mappedFieldPath[""].(map[string]any)
		var traversed FieldPath
		for i, path := range targetPath {
			traversed = append(traversed, path)

			// 检查路径冲突：如果字段已被映射为终端，则冲突
			if v, ok := m[path]; ok {
				if _, ok = v.(struct{}); ok {
					return fmt.Errorf("two terminal field paths conflict for node %s: %v, %v", n.key, traversed, targetPath)
				}
			}

			// 构建映射树：非终端字段创建子映射，终端字段标记为完成
			if i < len(targetPath)-1 {
				m[path] = make(map[string]any)
				m = m[path].(map[string]any)
			} else {
				m[path] = struct{}{}
			}
		}
	}

	return nil
}

// ====== 分支处理 ======

// WorkflowBranch 工作流分支 - 封装 GraphBranch 并记录源节点
type WorkflowBranch struct {
	fromNodeKey string
	*GraphBranch
}

// AddBranch 添加工作流分支 - 与 Graph 分支的关键区别
// Workflow 分支不会自动传递输入给选中的节点，需要显式定义字段映射
func (wf *Workflow[I, O]) AddBranch(fromNodeKey string, branch *GraphBranch) *WorkflowBranch {
	wb := &WorkflowBranch{
		fromNodeKey: fromNodeKey,
		GraphBranch: branch,
	}

	wf.workflowBranches = append(wf.workflowBranches, wb)
	return wb
}

// AddEnd 连接 END 节点 - 已弃用，建议使用 End() 方法
// 使用 *Workflow[I,O].End() 获取 WorkflowNode 实例进行连接
func (wf *Workflow[I, O]) AddEnd(fromNodeKey string, inputs ...*FieldMapping) *Workflow[I, O] {
	for _, input := range inputs {
		input.fromNodeKey = fromNodeKey
	}
	_ = wf.g.addEdgeWithMappings(fromNodeKey, END, false, false, inputs...)
	return wf
}

// ====== 编译逻辑 ======

// compile 编译工作流为可执行对象 - 处理依赖、字段映射和静态值
func (wf *Workflow[I, O]) compile(ctx context.Context, options *graphCompileOptions) (*composableRunnable, error) {
	// 构建错误检查：返回构建时累积的错误
	if wf.g.buildError != nil {
		return nil, wf.g.buildError
	}

	// 处理分支依赖：为每个分支的结束节点建立分支依赖
	for _, wb := range wf.workflowBranches {
		for endNode := range wb.endNodes {
			if endNode == END {
				// END 节点的分支依赖特殊处理
				if _, ok := wf.dependencies[END]; !ok {
					wf.dependencies[END] = make(map[string]dependencyType)
				}
				wf.dependencies[END][wb.fromNodeKey] = branchDependency
			} else {
				// 普通节点的分支依赖设置
				n := wf.workflowNodes[endNode]
				n.dependencySetter(wb.fromNodeKey, branchDependency)
			}
		}
		// 添加分支到图中（第三个参数为 true，表示工作流模式）
		_ = wf.g.addBranch(wb.fromNodeKey, wb.GraphBranch, true)
	}

	// 执行延迟的输入添加：执行所有 addInputs 回调
	for _, n := range wf.workflowNodes {
		for _, addInput := range n.addInputs {
			if err := addInput(); err != nil {
				return nil, err
			}
		}
		n.addInputs = nil
	}

	// 处理静态值：为每个节点的静态值创建处理器
	for _, n := range wf.workflowNodes {
		if len(n.staticValues) > 0 {
			value := make(map[string]any, len(n.staticValues))
			var paths []FieldPath
			for path, v := range n.staticValues {
				value[path] = v
				paths = append(paths, splitFieldPath(path))
			}

			if err := n.checkAndAddMappedPath(paths); err != nil {
				return nil, err
			}

			// 创建静态值处理器：合并输入和静态值
			pair := handlerPair{
				invoke: func(in any) (any, error) {
					values := []any{in, value}
					return mergeValues(values, nil)
				},
				transform: func(in streamReader) streamReader {
					sr := schema.StreamReaderFromArray([]map[string]any{value})
					newS, err := mergeValues([]any{in, packStreamReader(sr)}, nil)
					if err != nil {
						// 创建错误流：遇到错误时发送错误并关闭
						errSR, errSW := schema.Pipe[map[string]any](1)
						errSW.Send(nil, err)
						errSW.Close()
						return packStreamReader(errSR)
					}

					return newS.(streamReader)
				},
			}

			// 记录字段映射和前置处理器
			for i := range paths {
				wf.g.fieldMappingRecords[n.key] = append(wf.g.fieldMappingRecords[n.key], ToFieldPath(paths[i]))
			}

			wf.g.handlerPreNode[n.key] = []handlerPair{pair}
		}
	}

	// TODO: 验证间接边的合法性

	// 委托给内部图完成最终编译
	return wf.g.compile(ctx, options)
}

// ====== 内部辅助方法 ======

// initNode 初始化工作流节点 - 创建节点并设置依赖设置器
func (wf *Workflow[I, O]) initNode(key string) *WorkflowNode {
	n := &WorkflowNode{
		g:            wf.g,
		key:          key,
		staticValues: make(map[string]any),
		// 依赖设置器：捕获节点键，动态记录依赖关系
		dependencySetter: func(fromNodeKey string, typ dependencyType) {
			if _, ok := wf.dependencies[key]; !ok {
				wf.dependencies[key] = make(map[string]dependencyType)
			}
			wf.dependencies[key][fromNodeKey] = typ
		},
		mappedFieldPath: make(map[string]any),
	}
	wf.workflowNodes[key] = n
	return n
}

// ====== AnyGraph 接口实现 ======

func (wf *Workflow[I, O]) getGenericHelper() *genericHelper {
	return wf.g.getGenericHelper()
}

func (wf *Workflow[I, O]) inputType() reflect.Type {
	return wf.g.inputType()
}

func (wf *Workflow[I, O]) outputType() reflect.Type {
	return wf.g.outputType()
}

func (wf *Workflow[I, O]) component() component {
	return wf.g.component()
}
