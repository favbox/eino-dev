package compose

/*
 * workflow.go - 工作流构建器实现
 *
 * 核心组件：
 *   - Workflow: 工作流构建器，替代 AddEdge 提供声明式依赖管理
 *   - WorkflowNode: 工作流节点，支持字段映射和依赖配置
 *   - WorkflowBranch: 工作流分支，支持条件执行路径
 *
 * 设计特点：
 *   - 声明式依赖管理：通过 AddInput 声明数据流和执行依赖
 *   - 字段级映射：支持精细的字段映射而非整对象传递
 *   - 内部使用 NodeTriggerMode(AllPredecessor)，不支持循环
 *   - 支持静态值设置和分支条件执行
 *
 * 依赖类型：
 *   - normalDependency: 正常依赖（数据+执行）
 *   - noDirectDependency: 无直接依赖（仅数据流）
 *   - branchDependency: 分支依赖
 *
 * 与其他文件关系：
 *   - 内部使用 graph.go 的图构建能力
 *   - 与 field_mapping.go 协同实现字段映射
 *   - 为用户提供了更友好的工作流构建接口
 */
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

// ====== 工作流节点定义 ======

// WorkflowNode 工作流节点，封装节点配置和依赖关系
type WorkflowNode struct {
	// 节点配置
	g                *graph                                       // 所属图实例
	key              string                                       // 节点键标识
	addInputs        []func() error                               // 输入添加函数列表
	staticValues     map[string]any                               // 静态值映射
	dependencySetter func(fromNodeKey string, typ dependencyType) // 依赖设置函数
	mappedFieldPath  map[string]any                               // 字段路径映射
}

// ====== 工作流定义 ======

// Workflow 工作流构建器，封装图并提供声明式依赖管理
// 内部使用 NodeTriggerMode(AllPredecessor)，不支持循环
type Workflow[I, O any] struct {
	// 工作流状态
	g                *graph                               // 底层图实例
	workflowNodes    map[string]*WorkflowNode             // 工作流节点映射
	workflowBranches []*WorkflowBranch                    // 工作流分支列表
	dependencies     map[string]map[string]dependencyType // 依赖关系映射
}

// ====== 依赖类型定义 ======

// dependencyType 依赖类型枚举
type dependencyType int

const (
	normalDependency   dependencyType = iota // 正常依赖（数据+执行）
	noDirectDependency                       // 无直接依赖（仅数据流）
	branchDependency                         // 分支依赖
)

// ====== Workflow 工厂方法 ======

// NewWorkflow 创建新的工作流实例
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

// ====== 节点添加方法 ======

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

// AddGraphNode 添加子图节点
func (wf *Workflow[I, O]) AddGraphNode(key string, graph AnyGraph, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddGraphNode(key, graph, opts...)
	return wf.initNode(key)
}

// AddLambdaNode 添加 Lambda 节点
func (wf *Workflow[I, O]) AddLambdaNode(key string, lambda *Lambda, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddLambdaNode(key, lambda, opts...)
	return wf.initNode(key)
}

// End 获取终止节点
func (wf *Workflow[I, O]) End() *WorkflowNode {
	if node, ok := wf.workflowNodes[END]; ok {
		return node
	}
	return wf.initNode(END)
}

// AddPassthroughNode 添加透传节点
func (wf *Workflow[I, O]) AddPassthroughNode(key string, opts ...GraphAddNodeOpt) *WorkflowNode {
	_ = wf.g.AddPassthroughNode(key, opts...)
	return wf.initNode(key)
}

// ====== 输入和依赖管理 ======

// AddInput 添加输入依赖，建立数据流和执行依赖关系
// 参数：
//   - fromNodeKey: 前置节点键
//   - inputs: 字段映射列表，指定数据流方式
func (n *WorkflowNode) AddInput(fromNodeKey string, inputs ...*FieldMapping) *WorkflowNode {
	return n.addDependencyRelation(fromNodeKey, inputs, &workflowAddInputOpts{})
}

// workflowAddInputOpts 输入添加选项配置
type workflowAddInputOpts struct {
	noDirectDependency     bool // 无直接依赖：仅数据流，无执行依赖
	dependencyWithoutInput bool // 无输入依赖：仅执行依赖，无数据流
}

// WorkflowAddInputOpt 输入添加选项函数类型
type WorkflowAddInputOpt func(*workflowAddInputOpts)

// getAddInputOpts 获取输入添加选项
func getAddInputOpts(opts []WorkflowAddInputOpt) *workflowAddInputOpts {
	opt := &workflowAddInputOpts{}
	for _, o := range opts {
		o(opt)
	}
	return opt
}

// WithNoDirectDependency 创建无直接依赖的选项
// 作用：仅建立数据流依赖，不建立直接执行依赖
func WithNoDirectDependency() WorkflowAddInputOpt {
	return func(opt *workflowAddInputOpts) {
		opt.noDirectDependency = true
	}
}

// AddInputWithOptions 添加带选项的输入依赖
func (n *WorkflowNode) AddInputWithOptions(fromNodeKey string, inputs []*FieldMapping, opts ...WorkflowAddInputOpt) *WorkflowNode {
	return n.addDependencyRelation(fromNodeKey, inputs, getAddInputOpts(opts))
}

// AddDependency 添加执行依赖（无数据流）
// 作用：仅建立执行依赖，不传递数据
func (n *WorkflowNode) AddDependency(fromNodeKey string) *WorkflowNode {
	return n.addDependencyRelation(fromNodeKey, nil, &workflowAddInputOpts{dependencyWithoutInput: true})
}

// SetStaticValue 设置静态值
// 作用：为字段路径设置编译时确定的静态值
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

// WorkflowBranch 工作流分支，封装分支信息和源节点
type WorkflowBranch struct {
	fromNodeKey  string // 源节点键
	*GraphBranch        // 分支信息
}

// AddBranch 添加分支到工作流
// 注意：工作流分支与图分支的重要区别：
//   - 图分支：自动将输入传递给选中的节点
//   - 工作流分支：不自动传递输入，需节点自己定义字段映射
func (wf *Workflow[I, O]) AddBranch(fromNodeKey string, branch *GraphBranch) *WorkflowBranch {
	wb := &WorkflowBranch{
		fromNodeKey: fromNodeKey,
		GraphBranch: branch,
	}

	wf.workflowBranches = append(wf.workflowBranches, wb)
	return wb
}

// ====== 编译和初始化 ======

// compile 编译工作流为可执行对象
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

// initNode 初始化工作流节点
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

// getGenericHelper 获取泛型辅助对象
func (wf *Workflow[I, O]) getGenericHelper() *genericHelper {
	return wf.g.getGenericHelper()
}

// inputType 获取输入类型
func (wf *Workflow[I, O]) inputType() reflect.Type {
	return wf.g.inputType()
}

// outputType 获取输出类型
func (wf *Workflow[I, O]) outputType() reflect.Type {
	return wf.g.outputType()
}

// component 获取组件类型
func (wf *Workflow[I, O]) component() component {
	return wf.g.component()
}
