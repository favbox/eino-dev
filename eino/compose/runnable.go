package compose

import (
	"context"
	"fmt"
	"reflect"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// Runnable 可执行对象的统一接口。
// Graph、Chain 等都可以编译为 Runnable。
//
// 支持四种数据流模式，可自动降级兼容：
//  1. Invoke：同步输入 → 同步输出
//  2. Stream：同步输入 → 流输出
//  3. Collect：流输入 → 同步输出
//  4. Transform：流输入 → 流输出
//
// 注意：组件只需实现部分方法，框架会自动补全其他模式。
type Runnable[I, O any] interface {
	// Invoke 同步执行模式。
	Invoke(ctx context.Context, input I, opts ...Option) (output O, err error)

	// Stream 流式输出模式。
	Stream(ctx context.Context, input I, opts ...Option) (output *schema.StreamReader[O], err error)

	// Collect 流式输入模式。
	Collect(ctx context.Context, input *schema.StreamReader[I], opts ...Option) (output O, err error)

	// Transform 流式转换模式。
	Transform(ctx context.Context, input *schema.StreamReader[I], opts ...Option) (output *schema.StreamReader[O], err error)
}

type invoke func(ctx context.Context, input any, opts ...any) (output any, err error)

type transform func(ctx context.Context, input streamReader, opts ...any) (output streamReader, err error)

type composableRunnable struct {
	i invoke
	t transform

	inputType  reflect.Type
	outputType reflect.Type
	optionType reflect.Type

	*genericHelper

	isPassthrough bool
	meta          *executorMeta
	nodeInfo      *nodeInfo
}

func runnableLambda[I, O, TOption any](i Invoke[I, O, TOption], s Stream[I, O, TOption], c Collect[I, O, TOption],
	t Transform[I, O, TOption], enableCallback bool) *composableRunnable {
	rp := newRunnablePacker(i, s, c, t, enableCallback)

	return rp.toComposableRunnable()
}

type runnablePacker[I, O, TOption any] struct {
	i Invoke[I, O, TOption]
	s Stream[I, O, TOption]
	c Collect[I, O, TOption]
	t Transform[I, O, TOption]
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
			// 当 nil 作为 'any' 类型传递时，会丢失原始类型信息，
			// 变成一个无类型的 nil。这会导致类型断言失败。
			// 因此，如果输入为 nil 且目标类型 I 是接口类型，
			// 我们需要显式创建一个类型 I 的 nil。
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

// Invoke 是同步执行模式。
//
// 类似 "ping => pong" 的直接调用模式。
// 输入和输出都是同步的，适合大多数场景。
//
// 实现：
//
//	直接调用打包的 i 函数（Invoke 接口的实现）。
func (rp *runnablePacker[I, O, TOption]) Invoke(ctx context.Context,
	input I, opts ...TOption) (output O, err error) {
	return rp.i(ctx, input, opts...)
}

// Stream 是流式输出模式。
//
// 类似 "ping => stream output"。
// 输入是同步的，输出是流式的，适合需要实时响应的场景。
//
// 实现：
//
//	直接调用打包的 s 函数（Stream 接口的实现）。
func (rp *runnablePacker[I, O, TOption]) Stream(ctx context.Context,
	input I, opts ...TOption) (output *schema.StreamReader[O], err error) {

	return rp.s(ctx, input, opts...)
}

// Collect 是流式输入模式。
//
// 类似 "stream input => pong"。
// 输入是流式的，输出是同步的，适合需要流式输入处理的场景。
//
// 实现：
//
//	直接调用打包的 c 函数（Collect 接口的实现）。
func (rp *runnablePacker[I, O, TOption]) Collect(ctx context.Context,
	input *schema.StreamReader[I], opts ...TOption) (output O, err error) {
	return rp.c(ctx, input, opts...)
}

// Transform 是流式转换模式。
//
// 类似 "stream input => stream output"。
// 输入和输出都是流式的，适合流式数据处理流水线。
//
// 实现：
//
//	直接调用打包的 t 函数（Transform 接口的实现）。
func (rp *runnablePacker[I, O, TOption]) Transform(ctx context.Context,
	input *schema.StreamReader[I], opts ...TOption) (output *schema.StreamReader[O], err error) {
	return rp.t(ctx, input, opts...)
}

// defaultImplConcatStreamReader 将流式读取器连接为单值。
//
// 这是默认的流合并实现，将流中的所有数据合并为一个值。
// 如果流为空或读取失败，会返回错误。
//
// 类型参数：
//   - T：流中数据的类型
//
// 参数：
//   - sr：流式读取器
//
// 返回：
//   - T：合并后的单值
//   - error：错误信息
func defaultImplConcatStreamReader[T any](
	sr *schema.StreamReader[T]) (T, error) {

	c, err := concatStreamReader(sr)
	if err != nil {
		var t T
		return t, fmt.Errorf("concat stream reader fail: %w", err)
	}

	return c, nil
}

// invokeByStream 通过 Stream 实现 Invoke。
//
// 自动补全机制：将流式输出转换为同步输出。
// 使用场景：组件只实现了 Stream，但需要 Invoke 模式。
//
// 转换过程：
//  1. 调用 Stream 方法获取流式输出
//  2. 将流式输出转换为单值输出
//  3. 返回转换后的结果
func invokeByStream[I, O, TOption any](s Stream[I, O, TOption]) Invoke[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output O, err error) {
		sr, err := s(ctx, input, opts...)
		if err != nil {
			return output, err
		}

		return defaultImplConcatStreamReader(sr)
	}
}

func invokeByCollect[I, O, TOption any](c Collect[I, O, TOption]) Invoke[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output O, err error) {
		sr := schema.StreamReaderFromArray([]I{input})

		return c(ctx, sr, opts...)
	}
}

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

func streamByTransform[I, O, TOption any](t Transform[I, O, TOption]) Stream[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output *schema.StreamReader[O], err error) {
		srInput := schema.StreamReaderFromArray([]I{input})

		return t(ctx, srInput, opts...)
	}
}

func streamByInvoke[I, O, TOption any](i Invoke[I, O, TOption]) Stream[I, O, TOption] {
	return func(ctx context.Context, input I, opts ...TOption) (output *schema.StreamReader[O], err error) {
		out, err := i(ctx, input, opts...)
		if err != nil {
			return nil, err
		}

		return schema.StreamReaderFromArray([]O{out}), nil
	}
}

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

func collectByTransform[I, O, TOption any](t Transform[I, O, TOption]) Collect[I, O, TOption] {
	return func(ctx context.Context, input *schema.StreamReader[I], opts ...TOption) (output O, err error) {
		srOutput, err := t(ctx, input, opts...)
		if err != nil {
			return output, err
		}

		return defaultImplConcatStreamReader(srOutput)
	}
}

func collectByInvoke[I, O, TOption any](i Invoke[I, O, TOption]) Collect[I, O, TOption] {
	return func(ctx context.Context, input *schema.StreamReader[I], opts ...TOption) (output O, err error) {
		in, err := defaultImplConcatStreamReader(input)
		if err != nil {
			return output, err
		}

		return i(ctx, in, opts...)
	}
}

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

// newRunnablePacker 创建新的 Runnable 打包器。
//
// 这是 Eino 框架最核心的自动补全机制！
// 即使组件只实现了部分接口，也能自动补全其他三种模式。
//
// 参数：
//   - i：Invoke 接口实现（可选）
//   - s：Stream 接口实现（可选）
//   - c：Collect 接口实现（可选）
//   - t：Transform 接口实现（可选）
//   - enableCallback：是否启用回调
//
// 自动补全规则：
//  1. 如果只实现了 Stream，可以自动推导出 Invoke、Collect、Transform
//  2. 如果只实现了 Invoke，可以自动推导出 Stream、Collect、Transform
//  3. 以此类推，任何单接口实现都能补全为完整 Runnable
//
// 回调处理：
//   - 如果启用回调，会为每个接口实现包装回调逻辑
//   - 回调包括：OnStart、OnEnd、OnError、OnStartWithStreamInput、OnEndWithStreamOutput
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

// toGenericRunnable 转换为通用类型的 Runnable 打包器。
//
// 将 composableRunnable 转换为特定类型的 runnablePacker，
// 支持上下文包装和类型转换。
//
// 类型参数：
//   - I：输入类型
//   - O：输出类型
//
// 参数：
//   - cr：可执行的 Runnable 实例
//   - ctxWrapper：上下文包装函数
//
// 返回：
//   - *runnablePacker[I, O, Option]：转换后的打包器
//   - error：错误信息
func toGenericRunnable[I, O any](cr *composableRunnable, ctxWrapper func(ctx context.Context, opts ...Option) context.Context) (
	*runnablePacker[I, O, Option], error) {
	i := func(ctx context.Context, input I, opts ...Option) (output O, err error) {
		out, err := cr.i(ctx, input, toAnyList(opts)...)
		if err != nil {
			return output, err
		}

		to, ok := out.(O)
		if !ok {
			// 当 nil 作为 'any' 类型传递时，会丢失原始类型信息，
			// 变成一个无类型的 nil。这会导致类型断言失败。
			// 因此，如果输出为 nil 且目标类型 O 是接口类型，
			// 我们需要显式创建一个类型 O 的 nil。
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

// inputKeyedComposableRunnable 创建基于输入键的 Runnable。
//
// 用于从 map[string]any 类型的输入中提取特定键的值，
// 实现键值路由和数据流控制。
//
// 参数：
//   - key：要提取的输入键
//   - r：原始的 composableRunnable 实例
//
// 返回：
//   - *composableRunnable：包装后的 Runnable 实例
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

// outputKeyedComposableRunnable 创建基于输出键的 Runnable。
//
// 用于将输出结果包装为 map[string]any 类型，
// 在输出中添加指定的键值对。
//
// 参数：
//   - key：要添加的输出键
//   - r：原始的 composableRunnable 实例
//
// 返回：
//   - *composableRunnable：包装后的 Runnable 实例
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
