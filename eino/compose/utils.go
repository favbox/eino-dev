package compose

/*
 * utils.go - 工具函数集合
 *
 * 核心功能：
 *   - 回调机制：提供统一的回调处理和生命周期管理
 *   - 类型检查：支持可赋值性检查和类型兼容性验证
 *   - 选项管理：处理图的运行时选项分发和映射
 *   - 工具函数：提供常用的数据转换和操作工具
 *
 * 设计特点：
 *   - 统一的回调生命周期：开始/结束/错误处理
 *   - 支持流式和非流式模式的回调处理
 *   - 自动适配组件的回调能力
 *   - 灵活的选项传递和路径分发
 *
 * 与其他文件关系：
 *   - 为 graph_run.go 提供回调机制支持
 *   - 与 runnable.go 协同实现可执行对象的回调
 *   - 为各种节点类型提供统一的回调接口
 */

import (
	"context"
	"fmt"
	"reflect"

	"github.com/favbox/eino/callbacks"
	icb "github.com/favbox/eino/internal/callbacks"
	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// ====== 回调处理核心 ======

// on 回调函数类型，统一处理上下文和数据的转换
type on[T any] func(context.Context, T) (context.Context, T)

// onStart 处理开始回调
func onStart[T any](ctx context.Context, input T) (context.Context, T) {
	return icb.On(ctx, input, icb.OnStartHandle[T], callbacks.TimingOnStart, true)
}

// onEnd 处理结束回调
func onEnd[T any](ctx context.Context, output T) (context.Context, T) {
	return icb.On(ctx, output, icb.OnEndHandle[T], callbacks.TimingOnEnd, false)
}

// onStartWithStreamInput 处理流式输入的开始回调
func onStartWithStreamInput[T any](ctx context.Context, input *schema.StreamReader[T]) (
	context.Context, *schema.StreamReader[T]) {

	return icb.On(ctx, input, icb.OnStartWithStreamInputHandle[T], callbacks.TimingOnStartWithStreamInput, true)
}

// genericOnStartWithStreamInputHandle 通用流式输入开始回调处理器
func genericOnStartWithStreamInputHandle(ctx context.Context, input streamReader,
	runInfo *icb.RunInfo, handlers []icb.Handler) (context.Context, streamReader) {

	handlers = generic.Reverse(handlers)

	cpy := input.copy

	handle := func(ctx context.Context, handler icb.Handler, in streamReader) context.Context {
		in_, ok := unpackStreamReader[icb.CallbackInput](in)
		if !ok {
			panic("impossible")
		}

		return handler.OnStartWithStreamInput(ctx, runInfo, in_)
	}

	return icb.OnWithStreamHandle(ctx, input, handlers, cpy, handle)
}

// genericOnStartWithStreamInput 通用流式输入开始回调
func genericOnStartWithStreamInput(ctx context.Context, input streamReader) (context.Context, streamReader) {
	return icb.On(ctx, input, genericOnStartWithStreamInputHandle, callbacks.TimingOnStartWithStreamInput, true)
}

// onEndWithStreamOutput 处理流式输出的结束回调
func onEndWithStreamOutput[T any](ctx context.Context, output *schema.StreamReader[T]) (
	context.Context, *schema.StreamReader[T]) {

	return icb.On(ctx, output, icb.OnEndWithStreamOutputHandle[T], callbacks.TimingOnEndWithStreamOutput, false)
}

// genericOnEndWithStreamOutputHandle 通用流式输出结束回调处理器
func genericOnEndWithStreamOutputHandle(ctx context.Context, output streamReader,
	runInfo *icb.RunInfo, handlers []icb.Handler) (context.Context, streamReader) {

	cpy := output.copy

	handle := func(ctx context.Context, handler icb.Handler, out streamReader) context.Context {
		out_, ok := unpackStreamReader[icb.CallbackOutput](out)
		if !ok {
			panic("impossible")
		}

		return handler.OnEndWithStreamOutput(ctx, runInfo, out_)
	}

	return icb.OnWithStreamHandle(ctx, output, handlers, cpy, handle)
}

// genericOnEndWithStreamOutput 通用流式输出结束回调
func genericOnEndWithStreamOutput(ctx context.Context, output streamReader) (context.Context, streamReader) {
	return icb.On(ctx, output, genericOnEndWithStreamOutputHandle, callbacks.TimingOnEndWithStreamOutput, false)
}

// onError 处理错误回调
func onError(ctx context.Context, err error) (context.Context, error) {
	return icb.On(ctx, err, icb.OnErrorHandle, callbacks.TimingOnError, false)
}

// ====== 带回调的执行包装器 ======

// runWithCallbacks 通用回调包装器，统一处理开始/结束/错误回调
func runWithCallbacks[I, O, TOption any](r func(context.Context, I, ...TOption) (O, error),
	onStart on[I], onEnd on[O], onError on[error]) func(context.Context, I, ...TOption) (O, error) {

	return func(ctx context.Context, input I, opts ...TOption) (output O, err error) {
		ctx, input = onStart(ctx, input)

		output, err = r(ctx, input, opts...)
		if err != nil {
			ctx, err = onError(ctx, err)
			return output, err
		}

		ctx, output = onEnd(ctx, output)

		return output, nil
	}
}

// invokeWithCallbacks 为同步接口添加回调包装
func invokeWithCallbacks[I, O, TOption any](i Invoke[I, O, TOption]) Invoke[I, O, TOption] {
	return runWithCallbacks(i, onStart[I], onEnd[O], onError)
}

// onGraphStart 图开始回调，根据执行模式选择处理方式
func onGraphStart(ctx context.Context, input any, isStream bool) (context.Context, any) {
	if isStream {
		return genericOnStartWithStreamInput(ctx, input.(streamReader))
	}
	return onStart(ctx, input)
}

// onGraphEnd 图结束回调，根据执行模式选择处理方式
func onGraphEnd(ctx context.Context, output any, isStream bool) (context.Context, any) {
	if isStream {
		return genericOnEndWithStreamOutput(ctx, output.(streamReader))
	}
	return onEnd(ctx, output)
}

// onGraphError 图错误回调
func onGraphError(ctx context.Context, err error) (context.Context, error) {
	return onError(ctx, err)
}

// streamWithCallbacks 为流式接口添加回调包装
func streamWithCallbacks[I, O, TOption any](s Stream[I, O, TOption]) Stream[I, O, TOption] {
	return runWithCallbacks(s, onStart[I], onEndWithStreamOutput[O], onError)
}

// collectWithCallbacks 为聚合接口添加回调包装
func collectWithCallbacks[I, O, TOption any](c Collect[I, O, TOption]) Collect[I, O, TOption] {
	return runWithCallbacks(c, onStartWithStreamInput[I], onEnd[O], onError)
}

// transformWithCallbacks 为转换接口添加回调包装
func transformWithCallbacks[I, O, TOption any](t Transform[I, O, TOption]) Transform[I, O, TOption] {
	return runWithCallbacks(t, onStartWithStreamInput[I], onEndWithStreamOutput[O], onError)
}

// ====== 回调初始化 ======

// initGraphCallbacks 初始化图的回调处理
func initGraphCallbacks(ctx context.Context, info *nodeInfo, meta *executorMeta, opts ...Option) context.Context {
	ri := &callbacks.RunInfo{}
	if meta != nil {
		ri.Component = meta.component
		ri.Type = meta.componentImplType
	}

	if info != nil {
		ri.Name = info.name
	}

	var cbs []callbacks.Handler
	for i := range opts {
		if len(opts[i].handler) != 0 && len(opts[i].paths) == 0 {
			cbs = append(cbs, opts[i].handler...)
		}
	}

	if len(cbs) == 0 {
		return icb.ReuseHandlers(ctx, ri)
	}

	return icb.AppendHandlers(ctx, ri, cbs...)
}

// initNodeCallbacks 初始化节点的回调处理，支持路径分发
func initNodeCallbacks(ctx context.Context, key string, info *nodeInfo, meta *executorMeta, opts ...Option) context.Context {
	ri := &callbacks.RunInfo{}
	if meta != nil {
		ri.Component = meta.component
		ri.Type = meta.componentImplType
	}

	if info != nil {
		ri.Name = info.name
	}

	var cbs []callbacks.Handler
	for i := range opts {
		if len(opts[i].handler) != 0 {
			if len(opts[i].paths) != 0 {
				for _, k := range opts[i].paths {
					if len(k.path) == 1 && k.path[0] == key {
						cbs = append(cbs, opts[i].handler...)
						break
					}
				}
			}
		}
	}

	if len(cbs) == 0 {
		return icb.ReuseHandlers(ctx, ri)
	}

	return icb.AppendHandlers(ctx, ri, cbs...)
}

// ====== 流式转换工具 ======

// streamChunkConvertForCBOutput 将输出转换为回调输出
func streamChunkConvertForCBOutput[O any](o O) (callbacks.CallbackOutput, error) {
	return o, nil
}

// streamChunkConvertForCBInput 将输入转换为回调输入
func streamChunkConvertForCBInput[I any](i I) (callbacks.CallbackInput, error) {
	return i, nil
}

// toAnyList 转换为任意类型切片
func toAnyList[T any](in []T) []any {
	ret := make([]any, len(in))
	for i := range in {
		ret[i] = in[i]
	}
	return ret
}

// ====== 类型检查 ======

// assignableType 可赋值性检查类型
type assignableType uint8

const (
	assignableTypeMustNot assignableType = iota // 绝对不可赋值
	assignableTypeMust                          // 绝对可赋值
	assignableTypeMay                           // 可能可赋值（需要运行时检查）
)

// checkAssignable 检查类型可赋值性
func checkAssignable(input, arg reflect.Type) assignableType {
	if arg == nil || input == nil {
		return assignableTypeMustNot
	}

	if arg == input {
		return assignableTypeMust
	}

	if arg.Kind() == reflect.Interface && input.Implements(arg) {
		return assignableTypeMust
	}
	if input.Kind() == reflect.Interface {
		if arg.Implements(input) {
			return assignableTypeMay
		}
		return assignableTypeMustNot
	}

	return assignableTypeMustNot
}

// ====== 选项管理 ======

// extractOption 提取并分发选项到目标节点
func extractOption(nodes map[string]*chanCall, opts ...Option) (map[string][]any, error) {
	optMap := map[string][]any{}
	for _, opt := range opts {
		if len(opt.paths) == 0 {
			// 通用选项，按类型过滤
			if len(opt.options) == 0 {
				continue
			}
			for name, c := range nodes {
				if c.action.optionType == nil {
					// 子图
					optMap[name] = append(optMap[name], opt)
				} else if reflect.TypeOf(opt.options[0]) == c.action.optionType { // 假设选项类型一致
					optMap[name] = append(optMap[name], opt.options...)
				}
			}
		}
		for _, path := range opt.paths {
			if len(path.path) == 0 {
				return nil, fmt.Errorf("call option has designated an empty path")
			}

			var curNode *chanCall
			var ok bool
			if curNode, ok = nodes[path.path[0]]; !ok {
				return nil, fmt.Errorf("option has designated an unknown node: %s", path)
			}
			curNodeKey := path.path[0]

			if len(path.path) == 1 {
				if len(opt.options) == 0 {
					// 子图通用回调已在 initNodeCallback 中添加到 ctx，不会传递到子图
					// 节点回调也不会传递
					continue
				}
				if curNode.action.optionType == nil {
					nOpt := opt.deepCopy()
					nOpt.paths = []*NodePath{}
					optMap[curNodeKey] = append(optMap[curNodeKey], nOpt)
				} else {
					// 指定到组件
					if curNode.action.optionType != reflect.TypeOf(opt.options[0]) { // 假设选项类型一致
						return nil, fmt.Errorf("option type[%s] is different from which the designated node[%s] expects[%s]",
							reflect.TypeOf(opt.options[0]).String(), path, curNode.action.optionType.String())
					}
					optMap[curNodeKey] = append(optMap[curNodeKey], opt.options...)
				}
			} else {
				if curNode.action.optionType != nil {
					// 组件
					return nil, fmt.Errorf("cannot designate sub path of a component, path:%s", path)
				}
				// 指定到子图的节点
				nOpt := opt.deepCopy()
				nOpt.paths = []*NodePath{NewNodePath(path.path[1:]...)}
				optMap[curNodeKey] = append(optMap[curNodeKey], nOpt)
			}
		}
	}

	return optMap, nil
}

// mapToList 将映射转换为切片
func mapToList(m map[string]any) []any {
	ret := make([]any, 0, len(m))
	for _, v := range m {
		ret = append(ret, v)
	}
	return ret
}
