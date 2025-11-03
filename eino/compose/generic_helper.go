package compose

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// newGenericHelper 创建泛型辅助实例。
//
// 这是 Eino 框架类型安全的核心！
// 为组合组件提供泛型操作辅助，处理类型转换和验证。
//
// 背景说明：
// 在 Eino 中，我们需要组合不同的组件（如 ChatModel、Retriever 等）。
// 每个组件都有输入类型 I 和输出类型 O。
// 但在组合过程中，需要处理：
//   - 类型转换：any → I/O
//   - 流处理：streamReader ↔ 单值
//   - 字段映射：map[string]any → struct
//   - 类型检查：运行时类型验证
//
// genericHelper 就是这些泛型操作的统一辅助工具。
//
// 类型参数：
//   - I：输入类型（如 ChatModel 的输入是 []Message）
//   - O：输出类型（如 ChatModel 的输出是 *Message）
//
// 设计特点：
//   - 不可变：所有字段都是只读的，变换生成新实例
//   - 线程安全：无可变状态，可以并发使用
//   - 职责单一：每个字段负责一个特定功能
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

// genericHelper 是类型安全的泛型操作辅助工具。
//
// 这是 Eino 框架的"类型守护者"！
// 负责在组件组合过程中处理所有与类型相关的操作，
// 确保类型安全，避免运行时类型错误。
//
// 设计理念：
//  1. 职责单一：每个字段只负责一种特定功能
//  2. 对称设计：输入和输出有对称的处理逻辑
//  3. 不可变：所有字段都是只读的，通过变换生成新实例
//  4. 线程安全：无可变状态，可以安全并发使用
//
// 场景示例：
//
//	假设我们有一个 Chain：ChatTemplate → ChatModel
//	- ChatTemplate 输出：string
//	- ChatModel 输入：[]Message
//	- 需要将 string 转换为 []Message（通过提示词格式化）
//
//	genericHelper 会负责：
//	  1. 检查类型匹配：确保 ChatTemplate 输出的 string 确实可以转换为 ChatModel 需要的 []Message
//	  2. 转换数据：调用转换器将 string 转换为 []Message
//	  3. 流处理：如果需要流式处理，会处理 streamReader 与单值的转换
type genericHelper struct {
	// inputStreamFilter 是输入流过滤器。
	// 用途：当设置输入键时，从 map[string]any 中提取特定字段的值。
	//
	// 场景：Workflow 模式中，从 map 输入中提取特定字段。
	// 例如：map["user_input": "hello", "context": "..."]
	//      通过 inputStreamFilter("user_input") 可以提取 "hello" 字段。
	//
	// 转换过程：
	//   map[string]any → T （类型安全检查）
	inputStreamFilter, outputStreamFilter streamMapFilter

	// inputConverter 是输入类型转换器。
	// 用途：验证和转换前置节点的输出类型到当前节点的输入类型。
	//
	// 场景：节点连接时，确保数据传递的类型安全。
	// 例如：节点 A 输出 string，节点 B 需要 []Message
	//      inputConverter 会：
	//        1. 检查 string 是否可以转换为 []Message
	//        2. 调用转换器进行实际转换（如果需要）
	//
	// 包含两个处理器：
	//   - invoke：处理同步调用的类型转换
	//   - transform：处理流式调用的类型转换
	inputConverter, outputConverter handlerPair

	// inputFieldMappingConverter 是输入字段映射转换器。
	// 用途：当启用字段映射时，将 map 输入转换为期望的结构体。
	//
	// 场景：Workflow 模式的字段映射功能。
	// 例如：用户输入 map["name": "Alice", "age": 30]
	//      需要映射到 User{name: "Alice", age: 30}
	//
	// 包含两个处理器：
	//   - invoke：处理同步调用的字段映射
	//   - transform：处理流式调用的字段映射
	inputFieldMappingConverter, outputFieldMappingConverter handlerPair

	// inputStreamConvertPair 是输入流转换对。
	// 用途：在流和单值之间进行双向转换，用于检查点机制。
	//
	// 场景：状态检查点。
	//   - save checkpoint：将流合并为单值存储
	//   - restore checkpoint：将单值恢复为流继续处理
	//
	// 包含两个转换函数：
	//   - concatStream：流 → 单值
	//   - restoreStream：单值 → 流
	inputStreamConvertPair, outputStreamConvertPair streamConvertPair

	// inputZeroValue 是输入类型的零值生成器。
	// 用途：获取输入类型 I 的零值。
	//
	// 应用场景：
	//   - 初始化状态
	//   - 错误处理的默认值
	//   - 类型反射操作
	inputZeroValue, outputZeroValue func() any

	// inputEmptyStream 是输入类型的空流生成器。
	// 用途：创建输入类型 I 的空流（包含一个零值元素）。
	//
	// 应用场景：
	//   - 初始化流式输入
	//   - 错误恢复的默认流
	//   - 流式处理链的起点
	inputEmptyStream, outputEmptyStream func() streamReader
}

// forMapInput 变换为 map 输入模式。
//
// 用途：当节点需要从 map[string]any 提取特定字段时使用。
// 这是 Workflow 模式的核心变换方法。
//
// 变换逻辑：
//   - 输出部分保持不变：复用原有的输出处理逻辑
//   - 输入部分更新为 map 类型：
//   - inputStreamFilter：用于从 map 中提取特定键的值
//   - inputConverter：处理 map 类型的转换
//   - inputFieldMappingConverter：处理 map 到结构体的映射
//   - inputZeroValue/inputEmptyStream：生成 map 类型的默认值
//
// 使用场景：
//
//	Workflow wf := NewWorkflow[Input, Output]()
//	node := wf.AddChatModelNode("model", model)
//	node.MapField("prompt", "user_input")  // 内部会使用 forMapInput
//
// 不可变设计：
//
//	原实例 g 保持不变，返回新的实例。
//	这是函数式编程的风格，避免副作用。
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

// forMapOutput 变换为 map 输出模式。
//
// 用途：当节点输出需要包装为 map[string]any 时使用。
// 这是 Workflow 模式中字段映射输出的核心方法。
//
// 变换逻辑：
//   - 输入部分保持不变：复用原有的输入处理逻辑
//   - 输出部分更新为 map 类型：
//   - outputStreamFilter：用于添加输出键到 map
//   - outputConverter：处理 map 类型的转换
//   - outputFieldMappingConverter：处理结构体到 map 的映射
//   - outputZeroValue/outputEmptyStream：生成 map 类型的默认值
//
// 使用场景：
//
//	node := wf.AddLambdaNode("transform", transformFn)
//	node.MapField("result", "output_field")  // 内部会使用 forMapOutput
//
// 不可变设计：
//
//	原实例 g 保持不变，返回新的实例。
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

// forPredecessorPassthrough 前置透传模式。
//
// 用途：当前节点的输出直接传递给后继节点，不做类型转换。
// 保持输入类型和输出类型一致。
//
// 变换逻辑：
//   - 输入和输出使用相同的处理逻辑：
//   - outputStreamFilter = inputStreamFilter
//   - outputConverter = inputConverter
//   - outputFieldMappingConverter = inputFieldMappingConverter
//   - outputStreamConvertPair = inputStreamConvertPair
//   - outputZeroValue = inputZeroValue
//   - outputEmptyStream = inputEmptyStream
//
// 使用场景：
//
//	在图的某个分支中，前端节点的输出类型直接传递给后端节点，
//	不需要任何转换或映射。
//
// 示例：
//
//	节点 A (输出 string) → 节点 B (输入 string)
//	节点 B 使用 forPredecessorPassthrough，表示它直接接收节点 A 的输出，
//	不做任何类型转换。
//
// 设计意义：
//
//	避免不必要的类型检查和转换，提升性能。
//	当确定数据不需要转换时，使用此模式。
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

// forSuccessorPassthrough 后继透传模式。
//
// 用途：当前节点的输出直接传递给后继节点，不做类型转换。
// 但保持输出类型的一致性。
//
// 变换逻辑：
//   - 输入和输出使用相同的输出处理逻辑：
//   - inputStreamFilter = outputStreamFilter
//   - inputConverter = outputConverter
//   - inputFieldMappingConverter = outputFieldMappingConverter
//   - inputStreamConvertPair = outputStreamConvertPair
//   - inputZeroValue = outputZeroValue
//   - inputEmptyStream = outputEmptyStream
//
// 使用场景：
//
//	在图的某个分支中，当前节点的输出类型需要保持一致，
//	无论输入是什么类型。
//
// 示例：
//
//	节点 A (输出 string) → 节点 B (输出 string) → 节点 C (输入 string)
//	节点 B 使用 forSuccessorPassthrough，表示它保持输出类型一致。
//	无论它的输入是什么，输出都保持相同的处理方式。
//
// 与 forPredecessorPassthrough 的区别：
//   - forPredecessorPassthrough：输入和输出使用相同的输入处理逻辑
//   - forSuccessorPassthrough：输入和输出使用相同的输出处理逻辑
//
// 设计意义：
//
//	当节点只是"传递"数据，不改变其输出格式时使用。
func (g *genericHelper) forSuccessorPassthrough() *genericHelper {
	return &genericHelper{
		inputStreamFilter:           g.outputStreamFilter,
		outputStreamFilter:          g.outputStreamFilter,
		inputConverter:              g.outputConverter,
		outputConverter:             g.outputConverter,
		inputFieldMappingConverter:  g.outputFieldMappingConverter,
		outputFieldMappingConverter: g.outputFieldMappingConverter,
		inputStreamConvertPair:      g.inputStreamConvertPair,
		outputStreamConvertPair:     g.outputStreamConvertPair,
		inputZeroValue:              g.inputZeroValue,
		outputZeroValue:             g.outputZeroValue,
		inputEmptyStream:            g.inputEmptyStream,
		outputEmptyStream:           g.outputEmptyStream,
	}
}

// streamMapFilter 是流映射过滤器的函数类型。
//
// 用途：从流中提取特定键对应的值。
// 主要用于 Workflow 模式的字段映射。
//
// 参数：
//   - key：要提取的键名
//   - isr：输入流（通常是 map[string]any 类型的流）
//
// 返回：
//   - streamReader：提取后的流
//   - bool：是否提取成功
//
// 设计特点：
//   - 类型安全：包含类型检查和转换
//   - 错误处理：返回错误而非 panic
//   - 流式处理：保持流式特性
type streamMapFilter func(key string, isr streamReader) (streamReader, bool)

// valueHandler 是值处理器的函数类型。
//
// 用途：处理单个值的类型转换和验证。
// 主要用于 Invoke 模式的同步处理。
//
// 参数：
//   - value：输入值（any 类型）
//
// 返回：
//   - any：转换后的值
//   - error：转换过程中的错误
//
// 使用场景：
//   - 节点连接时的类型验证
//   - 输入输出的类型转换
//   - 运行时类型安全检查
type valueHandler func(value any) (any, error)

// streamHandler 是流处理器的函数类型。
//
// 用途：处理流数据的类型转换和验证。
// 主要用于 Transform 模式的流式处理。
//
// 参数：
//   - reader：输入流
//
// 返回：
//   - streamReader：转换后的流
//
// 使用场景：
//   - 流式数据的类型转换
//   - 流式处理链的类型验证
//   - 运行时流类型安全检查streamHandler
type streamHandler func(streamReader) streamReader

// handlerPair 是处理器对。
//
// 用途：同时支持同步（invoke）和流式（transform）两种处理模式。
// 这是 Eino 框架的"双模式"设计核心！
//
// 背景：
//
//	Eino 支持四种执行模式：Invoke、Stream、Collect、Transform
//	其中 Invoke 和 Stream 使用 valueHandler
//	Collect 和 Transform 使用 streamHandler
//
// 包含：
//   - invoke：同步处理器（用于 Invoke/Stream）
//   - transform：流式处理器（用于 Collect/Transform）
//
// 设计优势：
//   - 统一抽象：同一结构支持两种模式
//   - 类型安全：编译时保证类型正确
//   - 灵活扩展：可以独立配置两种模式
type handlerPair struct {
	invoke    valueHandler  // 同步处理器
	transform streamHandler // 流式处理器
}

// streamConvertPair 是流转换对。
//
// 用途：在流和单值之间进行双向转换。
// 主要用于检查点（checkpoint）机制。
//
// 背景：
//
//	在状态管理中，需要将流数据保存为检查点（单值），
//	之后又需要从检查点恢复为流继续处理。
//
// 包含：
//   - concatStream：流合并为单值（save checkpoint）
//   - restoreStream：单值恢复为流（restore checkpoint）
//
// 使用场景：
//   - 状态检查点：保存和恢复流式状态
//   - 调试：流数据的快照和回放
//   - 容错：错误恢复时的状态重建
type streamConvertPair struct {
	concatStream  func(sr streamReader) (any, error) // 流 → 单值
	restoreStream func(any) (streamReader, error)    // 单值 → 流
}

// defaultStreamConvertPair 创建默认的流转换对。
//
// 用途：为类型 T 创建标准的流 ↔ 单值转换器。
//
// 转换逻辑：
//
//  1. concatStream：
//     - 将流合并为单值
//     - 支持空流处理（返回 nil）
//     - 类型安全检查
//
//  2. restoreStream：
//     - 将单值包装为流
//     - nil 值处理（返回空流）
//     - 类型安全检查
//
// 类型参数：
//   - T：流中数据的类型
//
// 使用场景：
//   - 状态检查点的保存和恢复
//   - 流式数据的持久化
//   - 错误恢复时的状态重建
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
					return nil, nil // 空流返回 nil
				}
				return nil, err
			}
			return value, nil
		},
		restoreStream: func(a any) (streamReader, error) {
			if a == nil {
				// nil 值包装为空流
				return packStreamReader(schema.StreamReaderFromArray([]T{})), nil
			}
			value, ok := a.(T)
			if !ok {
				return nil, fmt.Errorf("cannot convert value[%T] to streamReader[%T]", a, t)
			}
			// 单值包装为单元素流
			return packStreamReader(schema.StreamReaderFromArray([]T{value})), nil
		},
	}
}

// defaultStreamMapFilter 创建默认的流映射过滤器。
//
// 用途：从 map 类型的流中提取特定键的值。
// 这是 Workflow 模式字段映射的核心实现。
//
// 转换过程：
//  1. 解包流：从 streamReader 中提取 map[string]any 类型的流
//  2. 创建转换函数：从 map 中提取键的值，并进行类型检查
//  3. 应用转换：使用 StreamReaderWithConvert 进行流式转换
//
// 类型参数：
//   - T：提取后数据的类型
//
// 参数：
//   - key：要提取的键名
//   - isr：输入流（map[string]any 类型）
//
// 返回：
//   - streamReader：提取后的流（T 类型）
//   - bool：提取是否成功
//
// 错误处理：
//   - 键不存在：返回 schema.ErrNoValue
//   - 类型不匹配：返回详细错误信息
//
// 使用场景：
//
//	Workflow wf := NewWorkflow[Input, Output]()
//	node := wf.AddChatModelNode("model", model)
//	node.MapField("prompt", "user_input")  // 内部使用此函数
func defaultStreamMapFilter[T any](key string, isr streamReader) (streamReader, bool) {
	// 1. 尝试从流中提取 map[string]any
	sr, ok := unpackStreamReader[map[string]any](isr)
	if !ok {
		// 不是 map 流，无法提取
		return nil, false
	}

	// 2. 创建转换函数：从 map 中提取特定键
	cvt := func(m map[string]any) (T, error) {
		var t T
		v, ok_ := m[key]
		if !ok_ {
			// 键不存在，跳过此元素
			return t, schema.ErrNoValue
		}

		// 类型检查：确保值是期望的类型
		vv, ok_ := v.(T)
		if !ok_ {
			return t, fmt.Errorf(
				"[defaultStreamMapFilter]fail, key[%s]'s value type[%s] isn't expected type[%s]",
				key, reflect.TypeOf(v).String(),
				generic.TypeOf[T]().String())
		}
		return vv, nil
	}

	// 3. 应用转换：使用流转换器
	ret := schema.StreamReaderWithConvert[map[string]any, T](sr, cvt)

	return packStreamReader(ret), true
}

// defaultStreamConverter 创建默认的流式类型转换器。
//
// 用途：将流中的数据进行类型转换和验证。
// 主要用于运行时类型安全检查。
//
// 转换过程：
//  1. 将流转换为 any 类型：使用 toAnyStreamReader()
//
// 2. 应用转换函数：进行类型断言和转换
//   - 类型匹配：返回转换后的值
//   - 类型不匹配：返回错误
//
// 类型参数：
//   - T：目标类型
//
// 参数：
//   - reader：输入流
//
// 返回：
//   - streamReader：转换后的流
//
// 设计特点：
//   - 零拷贝：使用 toAnyStreamReader 避免重复转换
//   - 类型安全：严格进行类型断言检查
//   - 错误传播：类型错误会沿流传播
//
// 使用场景：
//   - 节点连接时的流类型验证
//   - 流式数据的类型安全转换
//   - 运行时类型检查
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

// defaultValueChecker 创建默认的值检查器。
//
// 用途：对单个值进行类型转换和验证。
// 主要用于 Invoke 模式的同步处理。
//
// 转换过程：
//  1. 类型断言：尝试将 any 转换为 T
//  2. 类型检查：如果断言失败，返回错误
//  3. 返回值：返回转换后的值
//
// 类型参数：
//   - T：目标类型
//
// 参数：
//   - v：输入值（any 类型）
//
// 返回：
//   - any：转换后的值
//   - error：转换过程中的错误
//
// 设计特点：
//   - 简单直接：只进行类型断言
//   - 类型安全：失败时返回详细错误
//   - 性能优化：直接使用类型断言，无额外开销
//
// 使用场景：
//   - Invoke 模式的输入验证
//   - 节点连接时的类型检查
//   - 运行时类型安全保证
func defaultValueChecker[T any](v any) (any, error) {
	// 类型断言检查
	nValue, ok := v.(T)
	if !ok {
		var t T
		return nil, fmt.Errorf("runtime type check fail, expected type: %T, actual type: %T", t, v)
	}
	return nValue, nil
}

// zeroValueFromGeneric 从泛型类型获取零值。
//
// 用途：获取类型 T 的零值。
// 使用泛型确保类型安全。
//
// 类型参数：
//   - T：目标类型
//
// 返回：
//   - any：类型 T 的零值
//
// 实现原理：
//   - Go 泛型会自动展开为具体类型
//   - var t T 声明一个 T 类型的变量
//   - 返回该变量的零值
//
// 使用场景：
//   - 初始化状态结构体
//   - 错误处理的默认值
//   - 类型反射操作
//   - 默认值填充
//
// 示例：
//
//	zeroValueFromGeneric[int]()     // 返回 0
//	zeroValueFromGeneric[string]()  // 返回 ""
//	zeroValueFromGeneric[[]int]]() // 返回 nil
func zeroValueFromGeneric[T any]() any {
	var t T
	return t
}

// emptyStreamFromGeneric 创建空流。
//
// 用途：创建包含一个零值元素的流。
// 这是创建空流的标准方法。
//
// 类型参数：
//   - T：流中元素的类型
//
// 返回：
//   - streamReader：包含零值的流
//
// 创建过程：
//  1. 创建管道：使用 schema.Pipe[T](1)
//  2. 发送零值：sw.Send(zero, nil)
//  3. 关闭发送端：sw.Close()
//  4. 返回接收端：packStreamReader(sr)
//
// 使用场景：
//   - 初始化流式输入
//   - 错误恢复的默认流
//   - 流式处理链的起点
//   - 测试中的空流创建
//
// 示例：
//
//	emptyStreamFromGeneric[string]()  // 返回包含 "" 的流
//	emptyStreamFromGeneric[int]()    // 返回包含 0 的流
func emptyStreamFromGeneric[T any]() streamReader {
	var t T
	// 创建管道
	sr, sw := schema.Pipe[T](1)
	// 发送零值
	sw.Send(t, nil)
	// 关闭发送端
	sw.Close()
	// 返回接收端
	return packStreamReader(sr)
}
