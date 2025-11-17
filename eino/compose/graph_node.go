package compose

import (
	"context"
	"errors"
	"reflect"

	"github.com/favbox/eino/components"
	"github.com/favbox/eino/internal/generic"
)

/*
 * graph_node.go - Graph 节点定义与编译
 *
 * 核心组件：
 *   - executorMeta: 执行器元数据，记录组件类型和回调能力
 *   - nodeInfo: 节点信息，包含名称、键值和处理器
 *   - graphNode: 完整的图节点，封装可执行对象和图实例
 *
 * 设计特点：
 *   - 支持组件和子图两种节点类型
 *   - 提供编译时类型检查和运行时类型推导
 *   - 支持节点前后置处理器装饰
 *   - 处理输入输出键值映射（用于 Workflow 模式）
 *
 * 与其他文件关系：
 *   - 为 Graph 提供节点抽象和编译能力
 *   - 与 graph.go 协同构建图拓扑
 *   - 与 graph_run.go 配合执行节点
 */

// ====== 执行器元数据 ======

// executorMeta 执行器元数据，记录用户提供的原始可执行对象信息
type executorMeta struct {
	// 组件类型：根据 addNode 方式自动识别
	component component

	// 组件回调能力：标识用户提供的可执行对象是否能自行执行回调切面
	// 如果可以，对应图节点的回调将不会被执行（组件值来自 callbacks.Checker）
	isComponentCallbackEnabled bool

	// 组件实现类型：组件值来自 components.Typer，lambda 来自用户显式配置
	// 如果为空，则尝试推断实例中的类名或函数名（不保证）
	componentImplType string
}

// nodeInfo 节点信息，包含节点的元数据和配置
type nodeInfo struct {
	// 节点名称：用于显示目的，不保证唯一性，通过 WithNodeName() 传递
	name string

	// 输入输出键：用于 Workflow 模式的键值映射
	inputKey  string
	outputKey string

	// 节点前后置处理器：装饰节点的执行逻辑
	preProcessor, postProcessor *composableRunnable

	// 编译选项：如果节点是 AnyGraph，需要自身的编译选项
	compileOption *graphCompileOptions
}

// graphNode 图节点，包含节点在图中的完整信息
type graphNode struct {
	// 可执行对象：节点的实际执行逻辑
	cr *composableRunnable

	// 子图：如果节点是子图，存储子图实例
	g AnyGraph

	// 节点信息和执行器元数据
	nodeInfo     *nodeInfo
	executorMeta *executorMeta

	// 实例和选项：原始实例和添加节点时的选项
	instance any
	opts     []GraphAddNodeOpt
}

func (gn *graphNode) getGenericHelper() *genericHelper {
	var ret *genericHelper
	if gn.g != nil {
		ret = gn.g.getGenericHelper()
	} else if gn.cr != nil {
		ret = gn.cr.genericHelper
	} else {
		return nil
	}

	if gn.nodeInfo != nil {
		if len(gn.nodeInfo.inputKey) > 0 {
			ret = ret.forMapInput()
		}
		if len(gn.nodeInfo.outputKey) > 0 {
			ret = ret.forMapOutput()
		}
	}
	return ret
}

func (gn *graphNode) inputType() reflect.Type {
	if gn.nodeInfo != nil && len(gn.nodeInfo.inputKey) != 0 {
		return generic.TypeOf[map[string]any]()
	}
	// priority follow compile
	if gn.g != nil {
		return gn.g.inputType()
	} else if gn.cr != nil {
		return gn.cr.inputType
	}

	return nil
}

func (gn *graphNode) outputType() reflect.Type {
	if gn.nodeInfo != nil && len(gn.nodeInfo.outputKey) != 0 {
		return generic.TypeOf[map[string]any]()
	}
	// priority follow compile
	if gn.g != nil {
		return gn.g.outputType()
	} else if gn.cr != nil {
		return gn.cr.outputType
	}

	return nil
}

func (gn *graphNode) compileIfNeeded(ctx context.Context) (*composableRunnable, error) {
	var r *composableRunnable
	if gn.g != nil {
		cr, err := gn.g.compile(ctx, gn.nodeInfo.compileOption)
		if err != nil {
			return nil, err
		}

		r = cr
		gn.cr = cr
	} else if gn.cr != nil {
		r = gn.cr
	} else {
		return nil, errors.New("no graph or component provided")
	}

	r.meta = gn.executorMeta
	r.nodeInfo = gn.nodeInfo

	if gn.nodeInfo.outputKey != "" {
		r = outputKeyedComposableRunnable(gn.nodeInfo.outputKey, r)
	}

	if gn.nodeInfo.inputKey != "" {
		r = inputKeyedComposableRunnable(gn.nodeInfo.inputKey, r)
	}

	return r, nil
}

func parseExecutorInfoFromComponent(c component, executor any) *executorMeta {

	componentImplType, ok := components.GetType(executor)
	if !ok {
		componentImplType = generic.ParseTypeName(reflect.ValueOf(executor))
	}

	return &executorMeta{
		component:                  c,
		isComponentCallbackEnabled: components.IsCallbacksEnabled(executor),
		componentImplType:          componentImplType,
	}
}

func getNodeInfo(opts ...GraphAddNodeOpt) (*nodeInfo, *graphAddNodeOpts) {

	opt := getGraphAddNodeOpts(opts...)

	return &nodeInfo{
		name:          opt.nodeOptions.nodeName,
		inputKey:      opt.nodeOptions.inputKey,
		outputKey:     opt.nodeOptions.outputKey,
		preProcessor:  opt.processor.statePreHandler,
		postProcessor: opt.processor.statePostHandler,
		compileOption: newGraphCompileOptions(opt.nodeOptions.graphCompileOption...),
	}, opt
}
