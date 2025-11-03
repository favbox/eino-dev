package compose

import (
	"reflect"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/schema"
)

// streamReader 是流式读取器的抽象接口。
//
// 用途：统一不同类型流式读取器的操作。
// 这是 Eino 框架“适配器模式”的体现，将具体的 *schema.StreamReader[T]
// 包装为统一接口，支持任意类型的流操作。
//
// 设计思想：
//   - 类型擦除：隐藏具体类型 T，实现统一接口
//   - 功能组合：提供流操作的常用方法
//   - 零开销抽象：通过接口和多态实现抽象
type streamReader interface {
	copy(n int) []streamReader                            // 复制流
	getType() reflect.Type                                // 获取流类型
	getChunkType() reflect.Type                           // 获取流中元素类型
	withKey(string) streamReader                          // 添加键名，用于 map 转换
	merge([]streamReader) streamReader                    // 合并流
	mergeWithNames([]streamReader, []string) streamReader // 带名称合并
	toAnyStreamReader() *schema.StreamReader[any]         // 转为 any 类型
	close()                                               // 关闭流
}

// streamReaderPacker 是流式读取器的打包器。
//
// 用途：将 *schema.StreamReader[T] 结构体包装为 streamReader 接口。
// 这是“装饰器模式”的实现，为基础流添加额外功能。
//
// 类型参数：
//   - T：流中元素的类型
//
// 设计特点：
//   - 泛型实现：支持任意类型
//   - 轻量级包装：无额外状态，只包装一个字段
//   - 类型安全：编译时保证类型正确
//
// 应用场景：
//   - 统一不同类型的流接口
//   - 为流操作添加额外功能
//   - 类型擦除和动态分发
type streamReaderPacker[T any] struct {
	sr *schema.StreamReader[T]
}

// copy 复制流。
//
// 用途：创建流的 n 个副本。
// 这是实现多路输出的关键方法。
//
// 参数：
//   - n：副本数量
//
// 返回：
//   - 包含 n 个流副本的切片
//
// 实现逻辑：
//  1. 创建结果切片，容量为 n
//  2. 调用 srp.sr.Copy(n) 复制底层流
//  3. 将每个副本包装为 streamReaderPacker[T]
//  4. 返回包装后的 streamReader 接口切片
//
// 应用场景：
//   - 分支流的创建
//   - 并行处理同一流数据
//   - 流的广播式分发
func (srp streamReaderPacker[T]) copy(n int) []streamReader {
	ret := make([]streamReader, n)
	srs := srp.sr.Copy(n)

	for i := 0; i < n; i++ {
		ret[i] = streamReaderPacker[T]{srs[i]}
	}

	return ret
}

// getType 获取流类型。
//
// 用途：获取流的类型信息。
//
// 返回：
//   - reflect.Type：*schema.StreamReader[T] 的类型
//
// 应用场景：
//   - 类型检查和验证
//   - 反射操作
//   - 调试和日志记录
func (srp streamReaderPacker[T]) getType() reflect.Type {
	return reflect.TypeOf(srp.sr)
}

// getChunkType 获取流元素类型。
//
// 用途：获取流中元素的类型 T。
//
// 返回：
//   - reflect.Type：元素类型 T 的类型信息
//
// 设计意义：
//   - 类型系统的一部分
//   - 支持泛型的类型信息获取
//   - 用于运行时类型检查
func (srp streamReaderPacker[T]) getChunkType() reflect.Type {
	return generic.TypeOf[T]()
}

// withKey 添加键名。
//
// 用途：为流添加键名，转换为 map 类型。
// 这是实现"键值流"转换的方法。
//
// 参数：
//   - key：键名
//
// 返回：
//   - streamReader：包装为 map 的流
//
// 转换逻辑：
//   - 创建转换函数：将 T 转换为 map[string]any
//   - 使用 schema.StreamReaderWithConvert 进行转换
//   - 返回包装后的流
//
// 应用场景：
//   - Workflow 的字段映射
//   - 流数据的键值化
//   - 统一流格式
//
// 示例：
//
//	输入流：[msg1, msg2]
//	添加键名 "messages" 后：
//	输出流：[{messages: msg1}, {messages: msg2}]
func (srp streamReaderPacker[T]) withKey(key string) streamReader {
	cvt := func(v T) (map[string]any, error) {
		return map[string]any{key: v}, nil
	}
	ret := schema.StreamReaderWithConvert[T, map[string]any](srp.sr, cvt)
	return packStreamReader(ret)
}

// toStreamReaders 转换为 StreamReader 切片。
//
// 用途：将 streamReader 切片转换为 *schema.StreamReader[T] 切片。
// 这是内部辅助方法，用于流的合并操作。
//
// 参数：
//   - srs：streamReader 切片
//
// 返回：
//   - *schema.StreamReader[T] 切片，如果类型不匹配返回 nil
//
// 实现逻辑：
//  1. 创建结果切片，长度为 len(srs)+1
//  2. 第一个元素是当前流 srp.sr
//  3. 对每个 streamReader 进行类型检查和解包
//  4. 如果类型不匹配，返回 nil
//
// 注意事项：
//   - 这是内部方法，通常不直接调用
//   - 类型不匹配时返回 nil 而不是 panic
func (srp streamReaderPacker[T]) toStreamReaders(srs []streamReader) []*schema.StreamReader[T] {
	ret := make([]*schema.StreamReader[T], len(srs)+1)
	ret[0] = srp.sr
	for i := 1; i < len(ret); i++ {
		sr, ok := unpackStreamReader[T](srs[i-1])
		if !ok {
			return nil
		}

		ret[i] = sr
	}

	return ret
}

// merge 合并多个流。
//
// 用途：将当前流与多个流合并为一个流。
// 这是实现流合并的核心方法。
//
// 参数：
//   - isrs：要合并的其他流
//
// 返回：
//   - streamReader：合并后的流
//
// 实现逻辑：
//  1. 调用 toStreamReaders 转换为统一类型
//  2. 使用 schema.MergeStreamReaders 合并流
//  3. 将结果包装为 streamReader
//
// 应用场景：
//   - 多路流的合并
//   - 分支流的汇合
//   - 并行处理结果的聚合
func (srp streamReaderPacker[T]) merge(isrs []streamReader) streamReader {
	srs := srp.toStreamReaders(isrs)

	sr := schema.MergeStreamReaders(srs)

	return packStreamReader(sr)
}

// mergeWithNames 带名称合并多个流。
//
// 用途：将当前流与多个流按名称合并。
// 这是支持命名流合并的高级方法。
//
// 参数：
//   - isrs：要合并的其他流
//   - names：对应的名称列表
//
// 返回：
//   - streamReader：合并后的命名流
//
// 设计意义：
//   - 支持命名流
//   - 保留流的标识信息
//   - 便于区分不同源的流
//
// 应用场景：
//   - 多智能体输出的合并
//   - 多数据源流的合并
//   - 保留流来源信息的场
func (srp streamReaderPacker[T]) mergeWithNames(isrs []streamReader, names []string) streamReader {
	srs := srp.toStreamReaders(isrs)

	sr := schema.InternalMergeNamedStreamReaders(srs, names)

	return packStreamReader(sr)
}

// toAnyStreamReader 转换为 any 类型的流。
//
// 用途：将泛型流转换为 *schema.StreamReader[any]。
// 这是实现类型擦除的关键方法。
//
// 返回：
//   - *schema.StreamReader[any]：转换后的 any 流
//
// 转换逻辑：
//   - 创建转换函数：将 T 转换为 any
//   - 使用 schema.StreamReaderWithConvert 进行转换
//   - 返回 any 类型的流
//
// 设计意义：
//   - 类型擦除：隐藏具体类型 T
//   - 统一接口：所有流都可以转换为 any 类型
//   - 动态分发：支持运行时类型检查
//
// 应用场景：
//   - 接口转换
//   - 反射操作
//   - 动态类型处理
func (srp streamReaderPacker[T]) toAnyStreamReader() *schema.StreamReader[any] {
	return schema.StreamReaderWithConvert(srp.sr, func(t T) (any, error) {
		return t, nil
	})
}

// close 关闭底层的 *schema.StreamReader[T]。
func (srp streamReaderPacker[T]) close() {
	srp.sr.Close()
}

// packStreamReader 打包流。
//
// 用途：将 *schema.StreamReader[T] 包装为 streamReader 接口。
// 这是创建 streamReader 的标准方法。
//
// 类型参数：
//   - T：流中元素的类型
//
// 参数：
//   - sr：原始流
//
// 返回：
//   - streamReader：包装后的流接口
//
// 设计特点：
//   - 工厂方法：统一的创建方式
//   - 类型安全：编译时保证类型正确
//   - 简洁接口：隐藏实现细节
func packStreamReader[T any](sr *schema.StreamReader[T]) streamReader {
	return streamReaderPacker[T]{sr}
}

// unpackStreamReader 解包流。
//
// 用途：从 streamReader 接口中提取 *schema.StreamReader[T] 结构体。
// 这是读取 streamReader 的标准方法。
//
// 参数类型：
//   - T：流中元素的类型
//
// 参数：
//   - isr：流接口
//
// 返回：
//   - *schema.StreamReader[T]：提取的流
//   - bool：是否提取成功
//
// 解包逻辑：
//  1. 类型断言：尝试转换为 streamReaderPacker[T]
//  2. 如果成功：返回内部流
//  3. 如果失败且 T 是接口：转换为 any 类型后返回
//  4. 其他情况：返回失败
//
// 应用场景：
//   - 从接口中提取具体流
//   - 类型检查和验证
//   - 泛型函数的参数处理
//
// 设计优势：
//   - 优雅处理接口转换
//   - 支持接口类型的特殊处理
//   - 避免 panic，返回错误状态
func unpackStreamReader[T any](isr streamReader) (*schema.StreamReader[T], bool) {
	c, ok := isr.(streamReaderPacker[T])
	if ok {
		return c.sr, true
	}

	typ := generic.TypeOf[T]()
	if typ.Kind() == reflect.Interface {
		return schema.StreamReaderWithConvert(isr.toAnyStreamReader(), func(t any) (T, error) {
			return t.(T), nil
		}), true
	}

	return nil, false
}
