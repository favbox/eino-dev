package compose

import (
	"context"
	"errors"
	"reflect"

	"github.com/favbox/eino/components"
	"github.com/favbox/eino/internal/generic"
)

// executorMeta 执行器元信息结构体 - 封装用户提供的原始可执行对象信息
// 设计意图：区分组件的原生能力与图框架装饰的能力，避免重复执行回调
// 关键字段：
//   - component: 组件类型标识
//   - isComponentCallbackEnabled: 组件是否支持原生回调（避免重复装饰）
//   - componentImplType: 组件实现类型名称（用于调试和监控）
type executorMeta struct {
	// 自动识别，基于 addNode 方式确定
	component component

	// 组件是否支持原生回调执行能力
	// 如果支持，则对应图节点不会执行框架回调
	// 组件的值来自 callbacks.Checker
	isComponentCallbackEnabled bool

	// 组件实现类型：
	// - 组件：来自 components.Typer
	// - Lambda：来自用户显式配置
	// - 默认为空：从实例推断类名或函数名（不保证准确）
	componentImplType string
}

// nodeInfo 节点信息结构体 - 管理节点元数据和装饰器
// 设计意图：封装节点的元信息（名称、键映射、前后置处理器、编译选项）
// 关键字段：
//   - name: 节点显示名称（不唯一，用于调试）
//   - inputKey/outputKey: 输入输出键映射（支持结构化数据）
//   - preProcessor/postProcessor: 节点前后装饰器（状态处理器）
//   - compileOption: 子图编译选项（AnyGraph 类型节点需要）
type nodeInfo struct {
	// 节点显示名称，用于日志和调试，不唯一
	// 来自 WithNodeName() 选项
	name string

	// 输入输出键：支持从结构体中提取特定字段
	inputKey  string
	outputKey string

	// 节点前后置处理器：装饰节点执行逻辑
	preProcessor, postProcessor *composableRunnable

	// 编译选项：如果是 AnyGraph 子图，需要自己的编译配置
	compileOption *graphCompileOptions
}

// graphNode 图节点完整信息结构体 - 封装节点的所有属性和关系
// 设计意图：统一管理节点的执行器、元信息、选项和通用辅助系统
// 关键字段：
//   - cr: 可执行对象（优先使用，支持链式调用）
//   - g: 子图 AnyGraph（备选，用于子图节点）
//   - nodeInfo: 节点元信息和装饰器
//   - executorMeta: 执行器元信息
//   - instance: 原始用户实例（保留引用）
//   - opts: 节点创建选项（传递到子图编译）
type graphNode struct {
	// 可执行对象：封装节点的执行逻辑
	cr *composableRunnable

	// 子图：如果是 AnyGraph 类型节点，保存子图引用
	// 与 cr 互斥使用（子图需要编译，组件直接使用）
	g AnyGraph

	// 节点元信息：名称、键映射、前后置处理器
	nodeInfo *nodeInfo
	// 执行器元信息：组件类型、回调能力、类型名称
	executorMeta *executorMeta

	// 原始用户实例：保持对用户提供对象的引用
	instance any
	// 节点创建选项：传递给子图编译或组件配置
	opts []GraphAddNodeOpt
}

// getGenericHelper 获取节点的通用辅助系统 - 支持子图和组件两种模式
// 设计意图：优先从子图获取通用辅助系统，备选从可执行对象获取，并支持键映射转换
// 逻辑优先级：
//  1. 子图 AnyGraph 的通用辅助系统
//  2. 可执行对象的通用辅助系统
//  3. 如果配置了 inputKey，转换为 map 输入模式
//  4. 如果配置了 outputKey，转换为 map 输出模式
func (gn *graphNode) getGenericHelper() *genericHelper {
	var ret *genericHelper
	if gn.g != nil {
		// 子图模式：使用子图的通用辅助系统
		ret = gn.g.getGenericHelper()
	} else if gn.cr != nil {
		// 组件模式：使用可执行对象的通用辅助系统
		ret = gn.cr.genericHelper
	} else {
		return nil
	}

	// 键映射处理：将类型转换为 map 模式以支持结构化数据
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

// inputType 获取节点的输入类型 - 支持键映射和子图类型推断
// 类型推断优先级：
//  1. 如果配置了 inputKey，返回 map[string]any（支持结构化数据提取）
//  2. 子图模式：返回子图的输入类型
//  3. 组件模式：返回可执行对象的输入类型
func (gn *graphNode) inputType() reflect.Type {
	// 键映射模式：从结构体提取特定字段，输入为 map
	if gn.nodeInfo != nil && len(gn.nodeInfo.inputKey) != 0 {
		return generic.TypeOf[map[string]any]()
	}
	// 优先级跟随编译：子图优先，然后是组件
	if gn.g != nil {
		return gn.g.inputType()
	} else if gn.cr != nil {
		return gn.cr.inputType
	}

	return nil
}

// outputType 获取节点的输出类型 - 支持键映射和子图类型推断
// 类型推断优先级：
//  1. 如果配置了 outputKey，返回 map[string]any（支持结构化数据提取）
//  2. 子图模式：返回子图的输出类型
//  3. 组件模式：返回可执行对象的输出类型
func (gn *graphNode) outputType() reflect.Type {
	// 键映射模式：从结构体提取特定字段，输出为 map
	if gn.nodeInfo != nil && len(gn.nodeInfo.outputKey) != 0 {
		return generic.TypeOf[map[string]any]()
	}
	// 优先级跟随编译：子图优先，然后是组件
	if gn.g != nil {
		return gn.g.outputType()
	} else if gn.cr != nil {
		return gn.cr.outputType
	}

	return nil
}

// compileIfNeeded 按需编译节点 - 支持子图编译和键映射装饰
// 编译流程：
//  1. 子图模式：编译子图 AnyGraph，缓存结果到 cr 字段
//  2. 组件模式：直接使用已存在的可执行对象
//  3. 绑定元信息：将 executorMeta 和 nodeInfo 绑定到可执行对象
//  4. 键映射装饰：如果配置了 outputKey/inputKey，包装为键映射可执行对象
func (gn *graphNode) compileIfNeeded(ctx context.Context) (*composableRunnable, error) {
	var r *composableRunnable
	if gn.g != nil {
		// 子图模式：编译子图并缓存
		cr, err := gn.g.compile(ctx, gn.nodeInfo.compileOption)
		if err != nil {
			return nil, err
		}

		r = cr
		gn.cr = cr // 缓存编译结果
	} else if gn.cr != nil {
		// 组件模式：直接使用已有可执行对象
		r = gn.cr
	} else {
		return nil, errors.New("no graph or component provided")
	}

	// 绑定节点元信息：执行器和节点信息
	r.meta = gn.executorMeta
	r.nodeInfo = gn.nodeInfo

	// 键映射装饰：支持从结构体提取特定字段
	if gn.nodeInfo.outputKey != "" {
		r = outputKeyedComposableRunnable(gn.nodeInfo.outputKey, r)
	}

	if gn.nodeInfo.inputKey != "" {
		r = inputKeyedComposableRunnable(gn.nodeInfo.inputKey, r)
	}

	return r, nil
}

// parseExecutorInfoFromComponent 从组件解析执行器元信息 - 自动检测组件能力
// 设计意图：通过组件接口自动识别组件类型、回调能力和实现类型
// 检测逻辑：
//  1. 优先从 components.GetType 获取类型名称
//  2. 备选使用反射解析类型名称
//  3. 从 components.IsCallbacksEnabled 检查回调能力
//  4. 返回完整的执行器元信息
func parseExecutorInfoFromComponent(c component, executor any) *executorMeta {
	// 类型解析：优先使用组件注册系统，备选反射解析
	componentImplType, ok := components.GetType(executor)
	if !ok {
		// 反射解析：支持任意类型的组件
		componentImplType = generic.ParseTypeName(reflect.ValueOf(executor))
	}

	return &executorMeta{
		// 组件类型标识
		component: c,
		// 回调能力检查：避免重复装饰
		isComponentCallbackEnabled: components.IsCallbacksEnabled(executor),
		// 组件实现类型名称
		componentImplType: componentImplType,
	}
}

// getNodeInfo 从选项中提取节点信息 - 聚合所有节点元数据
// 设计意图：从 GraphAddNodeOpt 中提取并聚合节点相关信息，构建完整的 nodeInfo
// 提取内容包括：
//   - 节点名称（用于调试显示）
//   - 输入输出键（支持结构化数据）
//   - 前后置处理器（状态处理装饰器）
//   - 编译选项（子图专用）
func getNodeInfo(opts ...GraphAddNodeOpt) (*nodeInfo, *graphAddNodeOpts) {
	// 解析选项：提取所有配置参数
	opt := getGraphAddNodeOpts(opts...)

	// 构建节点信息：聚合所有元数据
	return &nodeInfo{
		// 节点基本信息
		name:      opt.nodeOptions.nodeName,
		inputKey:  opt.nodeOptions.inputKey,
		outputKey: opt.nodeOptions.outputKey,
		// 处理器装饰器
		preProcessor:  opt.processor.statePreHandler,
		postProcessor: opt.processor.statePostHandler,
		// 子图编译配置
		compileOption: newGraphCompileOptions(opt.nodeOptions.graphCompileOption...),
	}, opt
}
