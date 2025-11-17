package compose

/*
 * generic_helper.go - 泛型辅助工具
 *
 * 核心功能：
 *   - 提供类型安全的泛型辅助操作
 *   - 管理输入输出类型的转换和验证
 *   - 支持流式和非流式数据处理
 *   - 处理字段映射和键值映射
 *
 * 设计特点：
 *   - 使用泛型实现类型安全的操作
 *   - 提供默认的过滤器、转换器和检查器
 *   - 支持 Map 输入输出的特殊处理
 *   - 提供透传模式支持
 *
 * 与其他文件关系：
 *   - 为 graph_run.go 提供类型辅助
 *   - 与 field_mapping.go 协同处理字段映射
 *   - 支持各种节点类型的类型操作
 */

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// ====== 泛型辅助核心 ======

// newGenericHelper 创建泛型辅助实例
func newGenericHelper[I, O any]() *genericHelper {
	return &genericHelper{
		inputStreamFilter:  defaultStreamMapFilter[I],
		outputStreamFilter: defaultStreamMapFilter[O],
		inputConverter: handlerPair{
			invoke:    defaultValueChecker[I],
			transform: defaultStreamConverter[I],
		},
		outputConverter: handlerPair{
			invoke:    defaultValueChecker[O],
			transform: defaultStreamConverter[O],
		},
		inputFieldMappingConverter: handlerPair{
			invoke:    buildFieldMappingConverter[I](),
			transform: buildStreamFieldMappingConverter[I](),
		},
		outputFieldMappingConverter: handlerPair{
			invoke:    buildFieldMappingConverter[O](),
			transform: buildStreamFieldMappingConverter[O](),
		},
		inputStreamConvertPair:  defaultStreamConvertPair[I](),
		outputStreamConvertPair: defaultStreamConvertPair[O](),
		inputZeroValue:          zeroValueFromGeneric[I],
		outputZeroValue:         zeroValueFromGeneric[O],
		inputEmptyStream:        emptyStreamFromGeneric[I],
		outputEmptyStream:       emptyStreamFromGeneric[O],
	}
}

// genericHelper 泛型辅助结构体，封装类型相关的操作和转换器
type genericHelper struct {
	// 流过滤器：处理输入输出流的过滤
	inputStreamFilter, outputStreamFilter streamMapFilter

	// 类型转换器：处理类型验证和转换（用于 assignableTypeMay 情况）
	inputConverter, outputConverter handlerPair

	// 字段映射转换器：处理字段映射场景下的结构体转换
	inputFieldMappingConverter, outputFieldMappingConverter handlerPair

	// 流转换对：支持流式和非流式互转（用于检查点）
	inputStreamConvertPair, outputStreamConvertPair streamConvertPair

	// 零值和空流工厂：用于初始化
	inputZeroValue, outputZeroValue     func() any
	inputEmptyStream, outputEmptyStream func() streamReader
}

// ====== 泛型辅助方法 ======

// forMapInput 转换为 Map 输入模式
func (g *genericHelper) forMapInput() *genericHelper {
	return &genericHelper{
		outputStreamFilter:          g.outputStreamFilter,
		outputConverter:             g.outputConverter,
		outputFieldMappingConverter: g.outputFieldMappingConverter,
		outputStreamConvertPair:     g.outputStreamConvertPair,
		outputZeroValue:             g.outputZeroValue,
		outputEmptyStream:           g.outputEmptyStream,

		inputStreamFilter: defaultStreamMapFilter[map[string]any],
		inputConverter: handlerPair{
			invoke:    defaultValueChecker[map[string]any],
			transform: defaultStreamConverter[map[string]any],
		},
		inputFieldMappingConverter: handlerPair{
			invoke:    buildFieldMappingConverter[map[string]any](),
			transform: buildStreamFieldMappingConverter[map[string]any](),
		},
		inputStreamConvertPair: defaultStreamConvertPair[map[string]any](),
		inputZeroValue:         zeroValueFromGeneric[map[string]any],
		inputEmptyStream:       emptyStreamFromGeneric[map[string]any],
	}
}

func (g *genericHelper) forMapOutput() *genericHelper {
	return &genericHelper{
		inputStreamFilter:          g.inputStreamFilter,
		inputConverter:             g.inputConverter,
		inputFieldMappingConverter: g.inputFieldMappingConverter,
		inputStreamConvertPair:     g.inputStreamConvertPair,
		inputZeroValue:             g.inputZeroValue,
		inputEmptyStream:           g.inputEmptyStream,

		outputStreamFilter: defaultStreamMapFilter[map[string]any],
		outputConverter: handlerPair{
			invoke:    defaultValueChecker[map[string]any],
			transform: defaultStreamConverter[map[string]any],
		},
		outputFieldMappingConverter: handlerPair{
			invoke:    buildFieldMappingConverter[map[string]any](),
			transform: buildStreamFieldMappingConverter[map[string]any](),
		},
		outputStreamConvertPair: defaultStreamConvertPair[map[string]any](),
		outputZeroValue:         zeroValueFromGeneric[map[string]any],
		outputEmptyStream:       emptyStreamFromGeneric[map[string]any],
	}
}

func (g *genericHelper) forPredecessorPassthrough() *genericHelper {
	return &genericHelper{
		inputStreamFilter:           g.inputStreamFilter,
		outputStreamFilter:          g.inputStreamFilter,
		inputConverter:              g.inputConverter,
		outputConverter:             g.inputConverter,
		inputFieldMappingConverter:  g.inputFieldMappingConverter,
		outputFieldMappingConverter: g.inputFieldMappingConverter,
		inputStreamConvertPair:      g.inputStreamConvertPair,
		outputStreamConvertPair:     g.inputStreamConvertPair,
		inputZeroValue:              g.inputZeroValue,
		outputZeroValue:             g.inputZeroValue,
		inputEmptyStream:            g.inputEmptyStream,
		outputEmptyStream:           g.inputEmptyStream,
	}
}

func (g *genericHelper) forSuccessorPassthrough() *genericHelper {
	return &genericHelper{
		inputStreamFilter:           g.outputStreamFilter,
		outputStreamFilter:          g.outputStreamFilter,
		inputConverter:              g.outputConverter,
		outputConverter:             g.outputConverter,
		inputFieldMappingConverter:  g.outputFieldMappingConverter,
		outputFieldMappingConverter: g.outputFieldMappingConverter,
		inputStreamConvertPair:      g.outputStreamConvertPair,
		outputStreamConvertPair:     g.outputStreamConvertPair,
		inputZeroValue:              g.outputZeroValue,
		outputZeroValue:             g.outputZeroValue,
		inputEmptyStream:            g.outputEmptyStream,
		outputEmptyStream:           g.outputEmptyStream,
	}
}

type streamMapFilter func(key string, isr streamReader) (streamReader, bool)

type valueHandler func(value any) (any, error)
type streamHandler func(streamReader) streamReader

type handlerPair struct {
	invoke    valueHandler
	transform streamHandler
}

type streamConvertPair struct {
	concatStream  func(sr streamReader) (any, error)
	restoreStream func(any) (streamReader, error)
}

func defaultStreamConvertPair[T any]() streamConvertPair {
	var t T
	return streamConvertPair{
		concatStream: func(sr streamReader) (any, error) {
			tsr, ok := unpackStreamReader[T](sr)
			if !ok {
				return nil, fmt.Errorf("cannot convert sr to streamReader[%T]", t)
			}
			value, err := concatStreamReader(tsr)
			if err != nil {
				if errors.Is(err, emptyStreamConcatErr) {
					return nil, nil
				}
				return nil, err
			}
			return value, nil
		},
		restoreStream: func(a any) (streamReader, error) {
			if a == nil {
				return packStreamReader(schema.StreamReaderFromArray([]T{})), nil
			}
			value, ok := a.(T)
			if !ok {
				return nil, fmt.Errorf("cannot convert value[%T] to streamReader[%T]", a, t)
			}
			return packStreamReader(schema.StreamReaderFromArray([]T{value})), nil
		},
	}
}

func defaultStreamMapFilter[T any](key string, isr streamReader) (streamReader, bool) {
	sr, ok := unpackStreamReader[map[string]any](isr)
	if !ok {
		return nil, false
	}

	cvt := func(m map[string]any) (T, error) {
		var t T
		v, ok_ := m[key]
		if !ok_ {
			return t, schema.ErrNoValue
		}
		vv, ok_ := v.(T)
		if !ok_ {
			return t, fmt.Errorf(
				"[defaultStreamMapFilter]fail, key[%s]'s value type[%s] isn't expected type[%s]",
				key, reflect.TypeOf(v).String(),
				generic.TypeOf[T]().String())
		}
		return vv, nil
	}

	ret := schema.StreamReaderWithConvert[map[string]any, T](sr, cvt)

	return packStreamReader(ret), true
}

func defaultStreamConverter[T any](reader streamReader) streamReader {
	return packStreamReader(schema.StreamReaderWithConvert(reader.toAnyStreamReader(), func(v any) (T, error) {
		vv, ok := v.(T)
		if !ok {
			var t T
			return t, fmt.Errorf("runtime type check fail, expected type: %T, actual type: %T", t, v)
		}
		return vv, nil
	}))
}

func defaultValueChecker[T any](v any) (any, error) {
	nValue, ok := v.(T)
	if !ok {
		var t T
		return nil, fmt.Errorf("runtime type check fail, expected type: %T, actual type: %T", t, v)
	}
	return nValue, nil
}

func zeroValueFromGeneric[T any]() any {
	var t T
	return t
}

func emptyStreamFromGeneric[T any]() streamReader {
	var t T
	sr, sw := schema.Pipe[T](1)
	sw.Send(t, nil)
	sw.Close()
	return packStreamReader(sr)
}
