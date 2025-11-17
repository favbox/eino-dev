package compose

import (
	"context"
	"fmt"
	"reflect"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// GraphBranchCondition 单分支条件函数类型，根据输入选择单一路径。
type GraphBranchCondition[T any] func(ctx context.Context, in T) (endNode string, err error)

// StreamGraphBranchCondition 流式单分支条件函数类型，基于流输入选择单一路径。
type StreamGraphBranchCondition[T any] func(ctx context.Context, in *schema.StreamReader[T]) (endNode string, err error)

// GraphMultiBranchCondition 多分支条件函数类型，根据输入选择多个路径。
type GraphMultiBranchCondition[T any] func(ctx context.Context, in T) (endNode map[string]bool, err error)

// StreamGraphMultiBranchCondition 流式多分支条件函数类型，基于流输入选择多个路径。
type StreamGraphMultiBranchCondition[T any] func(ctx context.Context, in *schema.StreamReader[T]) (endNodes map[string]bool, err error)

// GraphBranch 图分支执行器，封装条件判断和路径执行逻辑。
type GraphBranch struct {
	invoke    func(ctx context.Context, input any) (output []string, err error)
	collect   func(ctx context.Context, input streamReader) (output []string, err error)
	inputType reflect.Type
	*genericHelper
	endNodes   map[string]bool
	idx        int
	noDataFlow bool
}

// GetEndNode 获取所有结束节点。
func (gb *GraphBranch) GetEndNode() map[string]bool {
	return gb.endNodes
}

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

// NewGraphMultiBranch 创建多分支执行器。
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

// NewStreamGraphMultiBranch 创建流式多分支执行器。
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

// NewGraphBranch 创建单分支执行器。
//
// 使用示例：
//
//	condition := func(ctx context.Context, in string) (string, error) {
//		// 根据输入决定下一个节点
//		return "next_node_key", nil
//	}
//	endNodes := map[string]bool{"path01": true, "path02": true}
//	branch := compose.NewGraphBranch(condition, endNodes)
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

// NewStreamGraphBranch 创建流式单分支执行器。
// 可以使用流的第一个块来决定下一个节点，实现快速路由。
//
// 使用示例：
//
//	condition := func(ctx context.Context, in *schema.StreamReader[T]) (string, error) {
//		// 使用流的第一个块决定下一个节点
//		return "next_node_key", nil
//	}
//	endNodes := map[string]bool{"path01": true, "path02": true}
//	branch := compose.NewStreamGraphBranch(condition, endNodes)
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
