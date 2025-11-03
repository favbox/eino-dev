package compose

import (
	"context"
	"reflect"

	"github.com/favbox/eino/components"
)

/*
 * introspect.go - 内省系统实现
 *
 * 核心组件：
 *   - GraphNodeInfo: 节点信息封装，包含组件实例、类型、映射等元数据
 *   - GraphInfo: 图信息封装，包含节点、边、分支等完整图结构
 *   - GraphCompileCallback: 编译完成回调接口
 *
 * 设计特点：
 *   - 元数据收集：自动收集图的构建过程信息
 *   - 完整图结构：支持控制流和数据流的完整表示
 *   - 类型信息：包含反射类型的完整类型信息
 *   - 回调机制：编译完成后的通知机制
 *
 * 与其他文件关系：
 *   - 为图的编译过程提供元数据支撑
 *   - 与字段映射系统协作记录数据流信息
 *   - 为图执行提供完整的结构化表示
 *   - 支持调试、可视化和监控工具
 *
 * 使用场景：
 *   - 编译时验证：检查图的构建合法性
 *   - 调试工具：提供图结构的可视化
 *   - 监控分析：收集图执行的统计信息
 *   - 自定义回调：在编译完成后执行自定义逻辑
 */

// ====== 节点信息定义 ======

// GraphNodeInfo 节点信息 - 封装添加图节点时的完整信息
// 包含组件实例、类型信息、映射关系等所有节点相关元数据
type GraphNodeInfo struct {
	// Component 组件类型标识 - 节点的组件类别（如工具、模型等）
	Component components.Component
	// Instance 组件实例 - 实际的组件对象实例
	Instance any
	// GraphAddNodeOpts 节点选项 - 添加节点时使用的配置选项
	GraphAddNodeOpts []GraphAddNodeOpt
	// InputType, OutputType 输入输出类型 - 主要用于 Lambda 节点
	// Lambda 节点的类型无法通过组件类型推断，需要显式记录
	InputType, OutputType reflect.Type
	// Name 节点名称 - 人类可读的节点标识符
	Name string
	// InputKey, OutputKey 输入输出键 - 节点的输入输出标识符
	InputKey, OutputKey string
	// GraphInfo 所属图信息 - 指向父图的结构信息
	GraphInfo *GraphInfo
	// Mappings 字段映射列表 - 节点的字段映射关系
	Mappings []*FieldMapping
}

// ====== 图信息定义 ======

// GraphInfo 图信息 - 封装编译图时的完整信息
// 用于编译回调中获取节点信息和实例，支持图的完整可观测性
// 包含图的所有结构化信息：节点、边、分支等
type GraphInfo struct {
	// CompileOptions 编译选项 - 编译时使用的配置选项
	CompileOptions []GraphCompileOption
	// Nodes 节点映射 - 节点键到节点信息的映射
	Nodes map[string]GraphNodeInfo // node key -> node info
	// Edges 控制边映射 - 控制流的边关系
	// 键：起始节点键，值：结束节点键列表
	Edges map[string][]string // edge start node key -> edge end node key, control edges
	// DataEdges 数据边映射 - 数据流的边关系
	DataEdges map[string][]string
	// Branches 分支映射 - 条件分支的结构信息
	// 键：分支起始节点键，值：分支列表
	Branches map[string][]GraphBranch // branch start node key -> branch
	// InputType, OutputType 输入输出类型 - 图的整体类型
	InputType, OutputType reflect.Type
	// Name 图名称 - 人类可读的图标识符
	Name string

	// NewGraphOptions 新图选项 - 创建图时使用的选项
	NewGraphOptions []NewGraphOption
	// GenStateFn 状态生成函数 - 生成图执行状态的函数
	GenStateFn func(context.Context) any
}

// ====== 编译回调接口 ======

// GraphCompileCallback 编译完成回调接口 - 图编译完成后的通知机制
// 用户可以通过实现此接口在编译完成后获取完整的图信息
// 用于实现自定义的可视化、验证、监控等功能
type GraphCompileCallback interface {
	// OnFinish 编译完成回调 - 当图编译完成时被调用
	// 参数：
	//   - ctx: 上下文信息
	//   - info: 完整的图信息，包含所有节点、边、分支等结构
	OnFinish(ctx context.Context, info *GraphInfo)
}
