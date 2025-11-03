package compose

import (
	"context"
	"fmt"
	"reflect"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

/*
 * branch.go - 图分支逻辑实现
 *
 * 核心组件：
 *   - GraphBranchCondition: 单分支条件函数类型
 *   - StreamGraphBranchCondition: 流式单分支条件函数类型
 *   - GraphMultiBranchCondition: 多分支条件函数类型
 *   - StreamGraphMultiBranchCondition: 流式多分支条件函数类型
 *   - GraphBranch: 分支执行器结构体
 *
 * 设计特点：
 *   - 条件驱动分支：根据条件函数动态选择执行路径
 *   - 多样化分支支持：单分支、多分支、流式分支三种模式
 *   - 类型安全：泛型设计确保编译期类型检查
 *   - 路径验证：确保分支返回的节点在预定义范围内
 *
 * 与其他文件关系：
 *   - 为 graph.go 提供分支逻辑实现
 *   - 支持 DAG/Chain/Workflow 的条件分支场景
 *   - 与 dag.go 的 reportSkip 机制协同工作
 */

// ====== 条件类型定义 ======

// GraphBranchCondition 单分支条件函数类型 - 根据输入选择单一路径
type GraphBranchCondition[T any] func(ctx context.Context, in T) (endNode string, err error)

// StreamGraphBranchCondition 流式单分支条件函数类型 - 基于流输入选择单一路径
type StreamGraphBranchCondition[T any] func(ctx context.Context, in *schema.StreamReader[T]) (endNode string, err error)

// GraphMultiBranchCondition 多分支条件函数类型 - 根据输入选择多个路径
type GraphMultiBranchCondition[T any] func(ctx context.Context, in T) (endNode map[string]bool, err error)

// StreamGraphMultiBranchCondition 流式多分支条件函数类型 - 基于流输入选择多个路径
type StreamGraphMultiBranchCondition[T any] func(ctx context.Context, in *schema.StreamReader[T]) (endNodes map[string]bool, err error)

// ====== 分支执行器 ======

// GraphBranch 图分支执行器 - 封装条件判断和路径执行逻辑
type GraphBranch struct {
	invoke    func(ctx context.Context, input any) (output []string, err error)
	collect   func(ctx context.Context, input streamReader) (output []string, err error)
	inputType reflect.Type
	*genericHelper
	endNodes   map[string]bool
	idx        int // 并行分支区分索引
	noDataFlow bool
}

// ====== 分支方法 ======

// GetEndNode 获取所有结束节点
func (gb *GraphBranch) GetEndNode() map[string]bool {
	return gb.endNodes
}

// ====== 内部构造函数 ======

// 创建 GraphBranch 实例
func newGraphBranch[T any](r *runnablePacker[T, []string, any], endNodes map[string]bool) *GraphBranch {
	return &GraphBranch{
		invoke: func(ctx context.Context, input any) (output []string, err error) {
			in, ok := input.(T)
			if !ok {
				// When a nil is passed as an 'any' type, its original type information is lost,
				// becoming an untyped nil. This would cause type assertions to fail.
				// So if the input is nil and the target type T is an interface, we need to explicitly create a nil of type T.
				if input == nil && generic.TypeOf[T]().Kind() == reflect.Interface {
					var i T
					in = i
				} else {
					panic(newUnexpectedInputTypeErr(generic.TypeOf[T](), reflect.TypeOf(input)))
				}
			}
			return r.Invoke(ctx, in)
		},
		collect: func(ctx context.Context, input streamReader) (output []string, err error) {
			in, ok := unpackStreamReader[T](input)
			if !ok {
				panic(newUnexpectedInputTypeErr(generic.TypeOf[T](), input.getType()))
			}
			return r.Collect(ctx, in)
		},
		inputType:     generic.TypeOf[T](),
		genericHelper: newGenericHelper[T, T](),
		endNodes:      endNodes,
	}
}

// ====== 公共工厂函数 ======

// NewGraphMultiBranch 创建多分支执行器
func NewGraphMultiBranch[T any](condition GraphMultiBranchCondition[T], endNodes map[string]bool) *GraphBranch {
	condRun := func(ctx context.Context, in T, opts ...any) ([]string, error) {
		ends, err := condition(ctx, in)
		if err != nil {
			return nil, err
		}
		ret := make([]string, 0, len(ends))
		for end := range ends {
			if !endNodes[end] {
				return nil, fmt.Errorf("branch invocation returns unintended end node: %s", end)
			}
			ret = append(ret, end)
		}

		return ret, nil
	}

	return newGraphBranch(newRunnablePacker(condRun, nil, nil, nil, false), endNodes)
}

// NewStreamGraphMultiBranch 创建流式多分支执行器
func NewStreamGraphMultiBranch[T any](condition StreamGraphMultiBranchCondition[T],
	endNodes map[string]bool) *GraphBranch {

	condRun := func(ctx context.Context, in *schema.StreamReader[T], opts ...any) ([]string, error) {
		ends, err := condition(ctx, in)
		if err != nil {
			return nil, err
		}

		ret := make([]string, 0, len(ends))
		for end := range ends {
			if !endNodes[end] {
				return nil, fmt.Errorf("branch invocation returns unintended end node: %s", end)
			}
			ret = append(ret, end)
		}
		return ret, nil
	}

	return newGraphBranch(newRunnablePacker(nil, nil, condRun, nil, false), endNodes)
}

// NewGraphBranch 创建单分支执行器 - 简化版多分支
// e.g.
//
//	condition := func(ctx context.Context, in string) (string, error) {
//		// logic to determine the next node
//		return "next_node_key", nil
//	}
//	endNodes := map[string]bool{"path01": true, "path02": true}
//	branch := compose.NewGraphBranch(condition, endNodes)
//
//	graph.AddBranch("key_of_node_before_branch", branch)
func NewGraphBranch[T any](condition GraphBranchCondition[T], endNodes map[string]bool) *GraphBranch {
	return NewGraphMultiBranch(func(ctx context.Context, in T) (endNode map[string]bool, err error) {
		ret, err := condition(ctx, in)
		if err != nil {
			return nil, err
		}
		return map[string]bool{ret: true}, nil
	}, endNodes)
}

// NewStreamGraphBranch 创建流式单分支执行器 - 简化版流式多分支
// e.g.
//
//	condition := func(ctx context.Context, in *schema.StreamReader[T]) (string, error) {
//		// logic to determine the next node.
//		// to use the feature of stream, you can use the first chunk to determine the next node.
//		return "next_node_key", nil
//	}
//	endNodes := map[string]bool{"path01": true, "path02": true}
//	branch := compose.NewStreamGraphBranch(condition, endNodes)
//
//	graph.AddBranch("key_of_node_before_branch", branch)
func NewStreamGraphBranch[T any](condition StreamGraphBranchCondition[T], endNodes map[string]bool) *GraphBranch {
	return NewStreamGraphMultiBranch(func(ctx context.Context, in *schema.StreamReader[T]) (endNode map[string]bool, err error) {
		ret, err := condition(ctx, in)
		if err != nil {
			return nil, err
		}
		return map[string]bool{ret: true}, nil
	}, endNodes)
}
