package compose

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

/*
 * state.go - 图状态管理系统
 *
 * 核心组件：
 *   - 状态生成：GenLocalState 函数类型用于创建初始状态
 *   - 状态封装：internalState 结构体封装状态值和互斥锁
 *   - 状态处理器：Pre/Post 处理器支持流式和非流式模式
 *   - 状态访问：ProcessState 提供并发安全的状态操作接口
 *   - 状态获取：getState 从上下文提取状态及互斥锁
 *
 * 设计特点：
 *   - 并发安全：所有状态访问通过互斥锁保护
 *   - 类型安全：通过泛型确保编译期类型检查
 *   - 上下文传递：状态通过 Context 键值对传递
 *   - 流程集成：支持节点执行前后的状态处理
 *   - 流式支持：自动处理流式状态处理器
 *
 * 与其他文件关系：
 *   - 为 graph.go 提供状态管理能力
 *   - 与 checkpoint.go 协同保存/恢复状态
 *   - 支持 StateGraph 的持久化状态场景
 *   - 与 interrupt.go 协作处理打断时的状态保存
 *
 * 使用场景：
 *   - 长期状态保持：跨多次调用的累积信息
 *   - 并发安全访问：多节点共享状态的线程安全操作
 *   - 节点预处理：执行前读取或修改状态
 *   - 节点后处理：执行后更新或持久化状态
 */

// ====== 状态生成器 ======

// GenLocalState 状态生成器函数类型 - 根据上下文生成初始状态
type GenLocalState[S any] func(ctx context.Context) (state S)

// ====== 内部状态管理 ======

type stateKey struct{} // 状态上下文键 - 用于从 Context 提取状态

// internalState 内部状态结构体 - 封装状态值和互斥锁
type internalState struct {
	state any        // 状态值（任意类型）
	mu    sync.Mutex // 互斥锁保证并发安全
}

// ====== 状态处理器定义 ======

// StatePreHandler 状态预处理函数类型 - 节点执行前调用，支持读取/修改状态
// 注意：如果用户使用流式调用但提供 StatePreHandler，它会读取所有流块并合并为单个对象
type StatePreHandler[I, S any] func(ctx context.Context, in I, state S) (I, error)

// StatePostHandler 状态后处理函数类型 - 节点执行后调用，支持读取/修改状态
// 注意：如果用户使用流式调用但提供 StatePostHandler，它会读取所有流块并合并为单个对象
type StatePostHandler[O, S any] func(ctx context.Context, out O, state S) (O, error)

// StreamStatePreHandler 流式状态预处理函数类型 - 节点执行前处理流式输入和输出
type StreamStatePreHandler[I, S any] func(ctx context.Context, in *schema.StreamReader[I], state S) (*schema.StreamReader[I], error)

// StreamStatePostHandler 流式状态后处理函数类型 - 节点执行后处理流式输入和输出
type StreamStatePostHandler[O, S any] func(ctx context.Context, out *schema.StreamReader[O], state S) (*schema.StreamReader[O], error)

// ====== 状态处理器转换器 ======

// convertPreHandler 转换预处理函数为可执行对象
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

// convertPostHandler 转换后处理函数为可执行对象
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

// streamConvertPreHandler 转换流式预处理函数为可执行对象
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

// streamConvertPostHandler 转换流式后处理函数为可执行对象
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

// ====== 状态操作接口 ======

// ProcessState 并发安全地处理上下文中的状态 - 访问和修改状态的标准方式
// 提供的处理函数将获得状态的独占访问权限（受互斥锁保护）
// 注意：如果状态类型不匹配或上下文中未找到状态，此方法将报告错误
// 示例：
//
//	lambdaFunc := func(ctx context.Context, in string, opts ...any) (string, error) {
//		err := compose.ProcessState[*testState](ctx, func(state *testState) error {
//			// 以并发安全的方式操作状态
//			state.Count++
//			return nil
//		})
//		if err != nil {
//			return "", err
//		}
//		return in, nil
//	}
//
//	stateGraph := compose.NewStateGraph[string, string, testState](genStateFunc)
//	stateGraph.AddNode("node1", lambdaFunc)
func ProcessState[S any](ctx context.Context, handler func(context.Context, S) error) error {
	s, pMu, err := getState[S](ctx)
	if err != nil {
		return fmt.Errorf("get state from context fail: %w", err)
	}
	pMu.Lock()
	defer pMu.Unlock()
	return handler(ctx, s)
}

// ====== 状态获取辅助函数 ======

// getState 从上下文中获取状态和互斥锁
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
