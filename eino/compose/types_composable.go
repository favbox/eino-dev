package compose

/*
 * types_composable.go - 可组合图类型系统定义
 *
 * 核心组件：
 *   - AnyGraph: 统一图接口，抽象所有可组合/可编译的图类型
 *
 * 设计特点：
 *   - 统一接口契约：定义 Graph/Chain 的通用行为
 *   - 类型安全：通过反射获取输入输出类型
 *   - 编译时支持：支持图的编译和运行时代码生成
 *   - 组件化标识：为图提供组件类型标识
 *
 * 与其他文件关系：
 *   - 为 graph.go 定义图接口契约
 *   - 被 component_to_graph_node.go 使用
 *   - 类型系统的核心抽象层
 */

import (
	"context"
	"reflect"
)

// ====== 通用图接口 ======

// AnyGraph 可组合图统一接口 - 抽象所有可组合和可编译的图类型
// 支持 Graph[I, O] 和 Chain[I, O] 两种图模式
type AnyGraph interface {
	// 获取通用辅助器
	getGenericHelper() *genericHelper

	// 编译图为可执行对象
	compile(ctx context.Context, options *graphCompileOptions) (*composableRunnable, error)

	// 获取输入类型
	inputType() reflect.Type

	// 获取输出类型
	outputType() reflect.Type

	// 获取组件类型标识
	component() component
}
