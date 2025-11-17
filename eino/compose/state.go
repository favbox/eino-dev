package compose

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// GenLocalState 状态生成器函数类型，根据上下文生成初始状态。
type GenLocalState[S any] func(ctx context.Context) (state S)

type stateKey struct{}

// internalState 内部状态封装，包含状态值和互斥锁。
type internalState struct {
	state any
	mu    sync.Mutex
}

// StatePreHandler 状态预处理函数类型，在节点执行前调用。
// 注意：流式调用时会自动读取所有流块并合并为单个对象。
type StatePreHandler[I, S any] func(ctx context.Context, in I, state S) (I, error)

// StatePostHandler 状态后处理函数类型，在节点执行后调用。
// 注意：流式调用时会自动读取所有流块并合并为单个对象。
type StatePostHandler[O, S any] func(ctx context.Context, out O, state S) (O, error)

// StreamStatePreHandler 流式状态预处理函数类型。
type StreamStatePreHandler[I, S any] func(ctx context.Context, in *schema.StreamReader[I], state S) (*schema.StreamReader[I], error)

// StreamStatePostHandler 流式状态后处理函数类型。
type StreamStatePostHandler[O, S any] func(ctx context.Context, out *schema.StreamReader[O], state S) (*schema.StreamReader[O], error)

// convertPreHandler 转换预处理函数为可执行对象。
func convertPreHandler[I, S any](handler StatePreHandler[I, S]) *composableRunnable {
	rf := func(ctx context.Context, in I, opts ...any) (I, error) {
		cState, pMu, err := getState[S](ctx)
		if err != nil {
			return in, err
		}
		pMu.Lock()
		defer pMu.Unlock()

		return handler(ctx, in, cState)
	}

	return runnableLambda[I, I](rf, nil, nil, nil, false)
}

// convertPostHandler 转换后处理函数为可执行对象。
func convertPostHandler[O, S any](handler StatePostHandler[O, S]) *composableRunnable {
	rf := func(ctx context.Context, out O, opts ...any) (O, error) {
		cState, pMu, err := getState[S](ctx)
		if err != nil {
			return out, err
		}
		pMu.Lock()
		defer pMu.Unlock()

		return handler(ctx, out, cState)
	}

	return runnableLambda[O, O](rf, nil, nil, nil, false)
}

// streamConvertPreHandler 转换流式预处理函数为可执行对象。
func streamConvertPreHandler[I, S any](handler StreamStatePreHandler[I, S]) *composableRunnable {
	rf := func(ctx context.Context, in *schema.StreamReader[I], opts ...any) (*schema.StreamReader[I], error) {
		cState, pMu, err := getState[S](ctx)
		if err != nil {
			return in, err
		}
		pMu.Lock()
		defer pMu.Unlock()

		return handler(ctx, in, cState)
	}

	return runnableLambda[I, I](nil, nil, nil, rf, false)
}

// streamConvertPostHandler 转换流式后处理函数为可执行对象。
func streamConvertPostHandler[O, S any](handler StreamStatePostHandler[O, S]) *composableRunnable {
	rf := func(ctx context.Context, out *schema.StreamReader[O], opts ...any) (*schema.StreamReader[O], error) {
		cState, pMu, err := getState[S](ctx)
		if err != nil {
			return out, err
		}
		pMu.Lock()
		defer pMu.Unlock()

		return handler(ctx, out, cState)
	}

	return runnableLambda[O, O](nil, nil, nil, rf, false)
}

// ProcessState 并发安全地处理上下文中的状态。
// 提供的处理函数将获得状态的独占访问权限（受互斥锁保护）。
//
// 使用示例：
//
//	lambdaFunc := func(ctx context.Context, in string, opts ...any) (string, error) {
//		err := compose.ProcessState[*testState](ctx, func(ctx context.Context, state *testState) error {
//			state.Count++
//			return nil
//		})
//		if err != nil {...}
//		return in, nil
//	}
//
//	stateGraph := compose.NewStateGraph[string, string, testState](genStateFunc)
//	stateGraph.AddLambdaNode("node1", compose.InvokableLambda(lambdaFunc))
func ProcessState[S any](ctx context.Context, handler func(context.Context, S) error) error {
	s, pMu, err := getState[S](ctx)
	if err != nil {
		return fmt.Errorf("get state from context fail: %w", err)
	}
	pMu.Lock()
	defer pMu.Unlock()
	return handler(ctx, s)
}

// getState 从上下文中获取状态和互斥锁。
func getState[S any](ctx context.Context) (S, *sync.Mutex, error) {
	state := ctx.Value(stateKey{})

	if state == nil {
		var s S
		return s, nil, fmt.Errorf("have not set state")
	}

	interState := state.(*internalState)

	cState, ok := interState.state.(S)
	if !ok {
		var s S
		return s, nil, fmt.Errorf("unexpected state type. expected: %v, got: %v",
			generic.TypeOf[S](), reflect.TypeOf(interState.state))
	}

	return cState, &interState.mu, nil
}
