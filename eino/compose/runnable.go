package compose

/*
 * runnable.go - 可执行对象接口与包装器
 *
 * 核心组件：
 *   - Runnable: 可执行对象接口，定义四种数据流模式
 *   - composableRunnable: 可执行对象包装器，统一封装执行逻辑
 *   - runnablePacker: 可执行对象打包器，处理接口适配
 *
 * 设计特点：
 *   - 四种数据流模式：Invoke/Stream/Collect/Transform
 *   - 自动适配：支持组件实现部分接口的向下兼容
 *   - 类型安全：使用泛型确保类型安全的转换
 *   - 回调支持：集成回调机制增强执行能力
 *
 * 数据流模式：
 *   - ping => pong（同步输入同步输出）
 *   - ping => stream output（同步输入流式输出）
 *   - stream input => pong（流式输入同步输出）
 *   - stream input => stream output（流式输入流式输出）
 *
 * 与其他文件关系：
 *   - 为 Graph、Chain 提供统一的可执行接口
 *   - 与 graph_node.go 协同实现节点执行
 *   - 与 generic_helper.go 提供类型辅助操作
 */

import (
	"context"
	"fmt"
	"reflect"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// ====== 可执行对象接口 ======

// Runnable 可执行对象接口，定义四种数据流模式。
// Graph、Chain 可编译为 Runnable
type Runnable[I, O any] interface {
	Invoke(ctx context.Context, input I, opts ...Option) (output O, err error)
	Stream(ctx context.Context, input I, opts ...Option) (output *schema.StreamReader[O], err error)
	Collect(ctx context.Context, input *schema.StreamReader[I], opts ...Option) (output O, err error)
	Transform(ctx context.Context, input *schema.StreamReader[I], opts ...Option) (output *schema.StreamReader[O], err error)
}

// ====== 基础函数类型 ======

// invoke 同步执行函数类型
type invoke func(ctx context.Context, input any, opts ...any) (output any, err error)

// transform 流式转换函数类型
type transform func(ctx context.Context, input streamReader, opts ...any) (output streamReader, err error)

// ====== 可执行对象包装器 ======

// composableRunnable 可执行对象包装器，封装用户提供的所有可执行对象
// 一个实例对应一个可执行对象实例
// 包含执行信息、类型信息、执行器元数据
// 用于 graphNode、ChainBranch、StatePreHandler、StatePostHandler 等
type composableRunnable struct {
	i invoke    // 同步执行函数
	t transform // 流式转换函数

	inputType  reflect.Type // 输入类型
	outputType reflect.Type // 输出类型
	optionType reflect.Type // 选项类型

	*genericHelper // 泛型辅助操作

	isPassthrough bool // 是否透传模式

	meta *executorMeta // 执行器元数据

	// 仅在图节点中可用
	// 如果不在图节点中，此字段为 nil
	nodeInfo *nodeInfo // 节点信息
}

// ====== 可执行对象打包器 ======

// runnableLambda 创建可执行对象包装器
func runnableLambda[I, O, TOption any](i Invoke[I, O, TOption], s Stream[I, O, TOption], c Collect[I, O, TOption],
	t Transform[I, O, TOption], enableCallback bool) *composableRunnable {
	rp := newRunnablePacker(i, s, c, t, enableCallback)

	return rp.toComposableRunnable()
}

// runnablePacker 可执行对象打包器，封装四种数据流模式接口
type runnablePacker[I, O, TOption any] struct {
	i Invoke[I, O, TOption]    // 同步执行接口
	s Stream[I, O, TOption]    // 流式接口
	c Collect[I, O, TOption]   // 聚合接口
	t Transform[I, O, TOption] // 转换接口
}

func (rp *runnablePacker[I, O, TOption]) wrapRunnableCtx(ctxWrapper func(ctx context.Context, opts ...TOption) context.Context) {
	i, s, c, t := rp.i, rp.s, rp.c, rp.t
	rp.i = func(ctx context.Context, input I, opts ...TOption) (output O, err error) {
		ctx = ctxWrapper(ctx, opts...)
		return i(ctx, input, opts...)
	}
	rp.s = func(ctx context.Context, input I, opts ...TOption) (output *schema.StreamReader[O], err error) {
		ctx = ctxWrapper(ctx, opts...)
		return s(ctx, input, opts...)
	}
	rp.c = func(ctx context.Context, input *schema.StreamReader[I], opts ...TOption) (output O, err error) {
		ctx = ctxWrapper(ctx, opts...)
		return c(ctx, input, opts...)
	}

	rp.t = func(ctx context.Context, input *schema.StreamReader[I], opts ...TOption) (output *schema.StreamReader[O], err error) {
		ctx = ctxWrapper(ctx, opts...)
		return t(ctx, input, opts...)
	}
}

func (rp *runnablePacker[I, O, TOption]) toComposableRunnable() *composableRunnable {
	inputType := generic.TypeOf[I]()
	outputType := generic.TypeOf[O]()
	optionType := generic.TypeOf[TOption]()
	c := &composableRunnable{
		genericHelper: newGenericHelper[I, O](),
		inputType:     inputType,
		outputType:    outputType,
		optionType:    optionType,
	}

	i := func(ctx context.Context, input any, opts ...any) (output any, err error) {
		in, ok := input.(I)
		if !ok {
			// When a nil is passed as an 'any' type, its original type information is lost,
			// becoming an untyped nil. This would cause type assertions to fail.
			// So if the input is nil and the target type I is an interface, we need to explicitly create a nil of type I.
			if input == nil && reflect.TypeOf((*I)(nil)).Elem().Kind() == reflect.Interface {
				var i I
				in = i
			} else {
				panic(newUnexpectedInputTypeErr(inputType, reflect.TypeOf(input)))
			}
		}

		tos, err := convertOption[TOption](opts...)
		if err != nil {
			return nil, err
		}
		return rp.Invoke(ctx, in, tos...)
	}

	t := func(ctx context.Context, input streamReader, opts ...any) (output streamReader, err error) {
		in, ok := unpackStreamReader[I](input)
		if !ok {
			panic(newUnexpectedInputTypeErr(reflect.TypeOf(in), input.getType()))
		}

		tos, err := convertOption[TOption](opts...)
		if err != nil {
			return nil, err
		}

		out, err := rp.Transform(ctx, in, tos...)
		if err != nil {
			return nil, err
		}

		return packStreamReader(out), nil
	}

	c.i = i
	c.t = t

	return c
}

// Invoke 同步执行：ping => pong
func (rp *runnablePacker[I, O, TOption]) Invoke(ctx context.Context,
	input I, opts ...TOption) (output O, err error) {
	return rp.i(ctx, input, opts...)
}

// Stream 流式执行：ping => stream output
func (rp *runnablePacker[I, O, TOption]) Stream(ctx context.Context,
	input I, opts ...TOption) (output *schema.StreamReader[O], err error) {

	return rp.s(ctx, input, opts...)
}

// Collect 聚合执行：stream input => pong
func (rp *runnablePacker[I, O, TOption]) Collect(ctx context.Context,
	input *schema.StreamReader[I], opts ...TOption) (output O, err error) {
	return rp.c(ctx, input, opts...)
}

// Transform 转换执行：stream input => stream output
func (rp *runnablePacker[I, O, TOption]) Transform(ctx context.Context,
	input *schema.StreamReader[I], opts ...TOption) (output *schema.StreamReader[O], err error) {
	return rp.t(ctx, input, opts...)
}

// defaultImplConcatStreamReader 默认流读取器合并实现
func defaultImplConcatStreamReader[T any](
	sr *schema.StreamReader[T]) (T, error) {

	c, err := concatStreamReader(sr)
	if err != nil {
		var t T
		return t, fmt.Errorf("concat stream reader fail: %w", err)
	}

	return c, nil
}

// invokeByStream 通过流式接口实现同步执行
func invokeByStream[I, O, TOption any](s Stream[I, O, TOption]) Invoke[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output O, err error) {
		sr, err := s(ctx, input, opts...)
		if err != nil {
			return output, err
		}

		return defaultImplConcatStreamReader(sr)
	}
}

// invokeByCollect 通过聚合接口实现同步执行
func invokeByCollect[I, O, TOption any](c Collect[I, O, TOption]) Invoke[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output O, err error) {
		sr := schema.StreamReaderFromArray([]I{input})

		return c(ctx, sr, opts...)
	}
}

// invokeByTransform 通过转换接口实现同步执行
func invokeByTransform[I, O, TOption any](t Transform[I, O, TOption]) Invoke[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output O, err error) {
		srInput := schema.StreamReaderFromArray([]I{input})

		srOutput, err := t(ctx, srInput, opts...)
		if err != nil {
			return output, err
		}

		return defaultImplConcatStreamReader(srOutput)
	}
}

// streamByTransform 通过转换接口实现流式执行
func streamByTransform[I, O, TOption any](t Transform[I, O, TOption]) Stream[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output *schema.StreamReader[O], err error) {
		srInput := schema.StreamReaderFromArray([]I{input})

		return t(ctx, srInput, opts...)
	}
}

// streamByInvoke 通过同步接口实现流式执行
func streamByInvoke[I, O, TOption any](i Invoke[I, O, TOption]) Stream[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output *schema.StreamReader[O], err error) {
		out, err := i(ctx, input, opts...)
		if err != nil {
			return nil, err
		}

		return schema.StreamReaderFromArray([]O{out}), nil
	}
}

// streamByCollect 通过聚合接口实现流式执行
func streamByCollect[I, O, TOption any](c Collect[I, O, TOption]) Stream[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output *schema.StreamReader[O], err error) {
		srInput := schema.StreamReaderFromArray([]I{input})
		out, err := c(ctx, srInput, opts...)
		if err != nil {
			return nil, err
		}

		return schema.StreamReaderFromArray([]O{out}), nil
	}
}

// collectByTransform 通过转换接口实现聚合执行
func collectByTransform[I, O, TOption any](t Transform[I, O, TOption]) Collect[I, O, TOption] {
	return func(ctx context.Context, input *schema.StreamReader[I], opts ...TOption) (output O, err error) {
		srOutput, err := t(ctx, input, opts...)
		if err != nil {
			return output, err
		}

		return defaultImplConcatStreamReader(srOutput)
	}
}

// collectByInvoke 通过同步接口实现聚合执行
func collectByInvoke[I, O, TOption any](i Invoke[I, O, TOption]) Collect[I, O, TOption] {
	return func(ctx context.Context, input *schema.StreamReader[I], opts ...TOption) (output O, err error) {
		in, err := defaultImplConcatStreamReader(input)
		if err != nil {
			return output, err
		}

		return i(ctx, in, opts...)
	}
}

// collectByStream 通过流式接口实现聚合执行
func collectByStream[I, O, TOption any](s Stream[I, O, TOption]) Collect[I, O, TOption] {
	return func(ctx context.Context, input *schema.StreamReader[I], opts ...TOption) (output O, err error) {
		in, err := defaultImplConcatStreamReader(input)
		if err != nil {
			return output, err
		}

		srOutput, err := s(ctx, in, opts...)
		if err != nil {
			return output, err
		}

		return defaultImplConcatStreamReader(srOutput)
	}
}

// transformByStream 通过流式接口实现转换执行
func transformByStream[I, O, TOption any](s Stream[I, O, TOption]) Transform[I, O, TOption] {
	return func(ctx context.Context, input *schema.StreamReader[I],
		opts ...TOption) (output *schema.StreamReader[O], err error) {
		in, err := defaultImplConcatStreamReader(input)
		if err != nil {
			return output, err
		}

		return s(ctx, in, opts...)
	}
}

// transformByCollect 通过聚合接口实现转换执行
func transformByCollect[I, O, TOption any](c Collect[I, O, TOption]) Transform[I, O, TOption] {
	return func(ctx context.Context, input *schema.StreamReader[I],
		opts ...TOption) (output *schema.StreamReader[O], err error) {
		out, err := c(ctx, input, opts...)
		if err != nil {
			return output, err
		}

		return schema.StreamReaderFromArray([]O{out}), nil
	}
}

// transformByInvoke 通过同步接口实现转换执行
func transformByInvoke[I, O, TOption any](i Invoke[I, O, TOption]) Transform[I, O, TOption] {
	return func(ctx context.Context, input *schema.StreamReader[I],
		opts ...TOption) (output *schema.StreamReader[O], err error) {
		in, err := defaultImplConcatStreamReader(input)
		if err != nil {
			return output, err
		}

		out, err := i(ctx, in, opts...)
		if err != nil {
			return output, err
		}

		return schema.StreamReaderFromArray([]O{out}), nil
	}
}

// newRunnablePacker 创建可执行对象打包器，自动适配接口
func newRunnablePacker[I, O, TOption any](i Invoke[I, O, TOption], s Stream[I, O, TOption],
	c Collect[I, O, TOption], t Transform[I, O, TOption], enableCallback bool) *runnablePacker[I, O, TOption] {

	r := &runnablePacker[I, O, TOption]{}

	if enableCallback {
		if i != nil {
			i = invokeWithCallbacks(i)
		}

		if s != nil {
			s = streamWithCallbacks(s)
		}

		if c != nil {
			c = collectWithCallbacks(c)
		}

		if t != nil {
			t = transformWithCallbacks(t)
		}
	}

	if i != nil {
		r.i = i
	} else if s != nil {
		r.i = invokeByStream(s)
	} else if c != nil {
		r.i = invokeByCollect(c)
	} else {
		r.i = invokeByTransform(t)
	}

	if s != nil {
		r.s = s
	} else if t != nil {
		r.s = streamByTransform(t)
	} else if i != nil {
		r.s = streamByInvoke(i)
	} else {
		r.s = streamByCollect(c)
	}

	if c != nil {
		r.c = c
	} else if t != nil {
		r.c = collectByTransform(t)
	} else if i != nil {
		r.c = collectByInvoke(i)
	} else {
		r.c = collectByStream(s)
	}

	if t != nil {
		r.t = t
	} else if s != nil {
		r.t = transformByStream(s)
	} else if c != nil {
		r.t = transformByCollect(c)
	} else {
		r.t = transformByInvoke(i)
	}

	return r
}

// toGenericRunnable 转换为泛型可执行对象
func toGenericRunnable[I, O any](cr *composableRunnable, ctxWrapper func(ctx context.Context, opts ...Option) context.Context) (
	*runnablePacker[I, O, Option], error) {
	i := func(ctx context.Context, input I, opts ...Option) (output O, err error) {
		out, err := cr.i(ctx, input, toAnyList(opts)...)
		if err != nil {
			return output, err
		}

		to, ok := out.(O)
		if !ok {
			// When a nil is passed as an 'any' type, its original type information is lost,
			// becoming an untyped nil. This would cause type assertions to fail.
			// So if the output is nil and the target type O is an interface, we need to explicitly create a nil of type O.
			if out == nil && generic.TypeOf[O]().Kind() == reflect.Interface {
				var o O
				to = o
			} else {
				panic(newUnexpectedInputTypeErr(generic.TypeOf[O](), reflect.TypeOf(out)))
			}
		}
		return to, nil
	}

	t := func(ctx context.Context, input *schema.StreamReader[I],
		opts ...Option) (output *schema.StreamReader[O], err error) {
		in := packStreamReader(input)
		out, err := cr.t(ctx, in, toAnyList(opts)...)

		if err != nil {
			return nil, err
		}

		output, ok := unpackStreamReader[O](out)
		if !ok {
			panic("impossible")
		}

		return output, nil
	}

	r := newRunnablePacker(i, nil, nil, t, false)
	r.wrapRunnableCtx(ctxWrapper)

	return r, nil
}

// inputKeyedComposableRunnable 创建输入键值包装的可执行对象
func inputKeyedComposableRunnable(key string, r *composableRunnable) *composableRunnable {
	wrapper := *r
	wrapper.genericHelper = wrapper.genericHelper.forMapInput()
	i := r.i
	wrapper.i = func(ctx context.Context, input any, opts ...any) (output any, err error) {
		v, ok := input.(map[string]any)[key]
		if !ok {
			return nil, fmt.Errorf("cannot find input key: %s", key)
		}
		out, err := i(ctx, v, opts...)
		if err != nil {
			return nil, err
		}

		return out, nil
	}

	t := r.t
	wrapper.t = func(ctx context.Context, input streamReader, opts ...any) (output streamReader, err error) {
		nInput, ok := r.inputStreamFilter(key, input)
		if !ok {
			return nil, fmt.Errorf("inputStreamFilter failed, key= %s, node name= %s, err= %w", key, r.nodeInfo.name, err)
		}
		out, err := t(ctx, nInput, opts...)
		if err != nil {
			return nil, err
		}

		return out, nil
	}

	wrapper.inputType = generic.TypeOf[map[string]any]()
	return &wrapper
}

// outputKeyedComposableRunnable 创建输出键值包装的可执行对象
func outputKeyedComposableRunnable(key string, r *composableRunnable) *composableRunnable {
	wrapper := *r
	wrapper.genericHelper = wrapper.genericHelper.forMapOutput()
	i := r.i
	wrapper.i = func(ctx context.Context, input any, opts ...any) (output any, err error) {
		out, err := i(ctx, input, opts...)
		if err != nil {
			return nil, err
		}

		return map[string]any{key: out}, nil
	}

	t := r.t
	wrapper.t = func(ctx context.Context, input streamReader, opts ...any) (output streamReader, err error) {
		out, err := t(ctx, input, opts...)
		if err != nil {
			return nil, err
		}

		return out.withKey(key), nil
	}

	wrapper.outputType = generic.TypeOf[map[string]any]()

	return &wrapper
}

// composablePassthrough 创建透传可执行对象，输入直接透传到输出
func composablePassthrough() *composableRunnable {
	r := &composableRunnable{isPassthrough: true, nodeInfo: &nodeInfo{}}

	r.i = func(ctx context.Context, input any, opts ...any) (output any, err error) {
		return input, nil
	}

	r.t = func(ctx context.Context, input streamReader, opts ...any) (output streamReader, err error) {
		return input, nil
	}

	r.meta = &executorMeta{
		component:                  ComponentOfPassthrough,
		isComponentCallbackEnabled: false,
		componentImplType:          "Passthrough",
	}

	return r
}
