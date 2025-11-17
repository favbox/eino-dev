/*
 * branch.go - 图分支实现，支持条件路由和多路选择
 *
 * 核心组件：
 *   - GraphBranch: 图分支，根据条件函数决定下一个执行节点
 *   - GraphBranchCondition: 单选分支条件函数，返回单个目标节点
 *   - GraphMultiBranchCondition: 多选分支条件函数，返回多个目标节点
 *
 * 设计特点：
 *   - 条件路由: 根据输入数据和业务逻辑动态决定执行路径
 *   - 单选和多选: 支持单个目标节点和多个并行目标节点
 *   - 流式支持: 支持流式输入的分支条件判断
 *   - 类型安全: 使用泛型确保输入类型的类型安全
 *   - 终点验证: 自动验证分支返回的节点是否在允许列表中
 *
 * 使用场景：
 *   - 条件判断: 根据输入内容决定不同的处理流程
 *   - 动态路由: 根据运行时状态选择执行路径
 *   - 并行分发: 将输入分发到多个并行处理节点
 */

package compose

import (
	"context"
	"fmt"
	"reflect"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// GraphBranchCondition 是单选分支的条件函数类型。
// 根据输入决定下一个执行节点，返回单个节点名称。
type GraphBranchCondition[T any] func(ctx context.Context, in T) (endNode string, err error)

// StreamGraphBranchCondition 是流式单选分支的条件函数类型。
// 支持流式输入，可以根据流的前几个数据块决定路由。
type StreamGraphBranchCondition[T any] func(ctx context.Context, in *schema.StreamReader[T]) (endNode string, err error)

// GraphMultiBranchCondition 是多选分支的条件函数类型。
// 根据输入决定多个目标节点，返回节点名称映射，支持并行执行多个分支。
type GraphMultiBranchCondition[T any] func(ctx context.Context, in T) (endNode map[string]bool, err error)

// StreamGraphMultiBranchCondition 是流式多选分支的条件函数类型。
// 支持流式输入，可以根据流的前几个数据块决定多个目标节点。
type StreamGraphMultiBranchCondition[T any] func(ctx context.Context, in *schema.StreamReader[T]) (endNodes map[string]bool, err error)

// GraphBranch 是图的分支类型，根据条件函数决定下一个执行节点。
// 封装条件判断逻辑、输入类型信息和允许的终点节点列表。
type GraphBranch struct {
	invoke         func(ctx context.Context, input any) (output []string, err error)          // 非流式调用函数
	collect        func(ctx context.Context, input streamReader) (output []string, err error) // 流式收集函数
	inputType      reflect.Type                                                               // 输入类型
	*genericHelper                                                                            // 泛型辅助器
	endNodes       map[string]bool                                                            // 允许的终点节点映射
	idx            int                                                                        // 并行分支的索引标识
	noDataFlow     bool                                                                       // 是否无数据流
}

// GetEndNode 返回分支的所有终点节点
func (gb *GraphBranch) GetEndNode() map[string]bool {
	return gb.endNodes
}

func newGraphBranch[T any](r *runnablePacker[T, []string, any], endNodes map[string]bool) *GraphBranch {
	return &GraphBranch{
		invoke: func(ctx context.Context, input any) (output []string, err error) {
			in, ok := input.(T)
			if !ok {
				// 当 nil 作为 any 类型传递时，会丢失原始类型信息，变成无类型的 nil，
				// 这会导致类型断言失败。
				// 因此如果输入是 nil 且目标类型 T 是接口，需要显式创建类型 T 的 nil。
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

// NewGraphMultiBranch 创建多选分支，支持根据条件返回多个目标节点。
// 条件函数返回的节点映射中的每个节点都会被并行执行。
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

// NewStreamGraphMultiBranch 创建流式多选分支，支持根据流式输入返回多个目标节点。
// 可以根据流的前几个数据块做出路由决策，支持并行执行多个分支。
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

// NewGraphBranch 创建单选分支，根据条件函数决定下一个执行节点。
// 条件函数返回单个节点名称，该节点必须在 endNodes 列表中。
//
// 示例：
//
//	condition := func(ctx context.Context, in string) (string, error) {
//		// 根据输入决定下一个节点的逻辑
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

// NewStreamGraphBranch 创建流式单选分支，根据流式输入的条件决定下一个执行节点。
// 可以利用流的特性，根据前几个数据块做出路由决策，无需等待完整数据。
//
// 示例：
//
//	condition := func(ctx context.Context, in *schema.StreamReader[T]) (string, error) {
//		// 根据流式输入决定下一个节点的逻辑
//		// 可以使用流的第一个数据块来决定下一个节点
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
