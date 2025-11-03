package compose

import (
	"errors"
	"io"

	"github.com/favbox/eino/internal"
	"github.com/favbox/eino/schema"
)

// RegisterStreamChunkConcatFunc 注册流块合并函数。
//
// 用途：为特定类型注册自定义的流块合并逻辑。
// 这是 Eino 框架“扩展点”机制的体现，允许用户自定义类型如何合并。
//
// 背景说明：
//
//	当流式数据需要转换为单值时（如 Invoke 模式调用流式组件），
//	需要将多个流块合并为一个结果。
//	对于简单类型（如 string、int），框架有默认合并逻辑。
//	但对于复杂类型（如 struct），需要用户自定义合并策略。
//
// 类型参数：
//   - T：流中元素的类型
//
// 参数：
//   - fn：合并函数：接收 []T，返回合并后的 T 和可能的错误
//
// 注册时机：
//   - 在进程初始化时调用
//   - 必须在使用该类型之前注册
//
// 线程安全：
//   - 非线程安全
//   - 不应在并发场景下注册
//
// 合并示例：
//
//	type Result struct {
//		Field1 string
//		Field2 int
//	}
//
// // 自定义合并策略：取最后一个元素的 Field1，累加 Field2
//
//	compose.RegisterStreamChunkConcatFunc(func(items []Result) (Result, error) {
//			if len(items) == 0 {
//				return Result{}, errors.New("no items to concat")
//			}
//
//			result := items[0]
//			for i := 0; i < len(items); i++ {
//				result.Field2 += items[i].Field2
//			}
//			result.Field1 = items[len(items)-1].Field1
//
//			return result, nil
//	})
func RegisterStreamChunkConcatFunc[T any](fn func([]T) (T, error)) {
	internal.RegisterStreamChunkConcatFunc(fn)
}

// emptyStreamConcatErr 是空流合并错误。
//
// 用途：当尝试合并空流时返回的错误。
// 这帮助调用者区分“流为空”和“流读取失败”两种情况。
var emptyStreamConcatErr = errors.New("stream reader is empty, concat failed")

// concatStreamReader 合并流读取器为单值。
//
// 用途：将流中的所有块合并为一个值。
// 这是实现“流->单值”转换的核心函数。
//
// 类型参数：
//   - T：流中元素的类型
//
// 参数：
//   - sr：流读取器
//
// 返回：
//   - T：合并后的单值
//   - error：合并过程中的错误
//
// 处理流程：
//  1. 延迟关闭流（确保资源释放）
//  2. 循环读取流中的所有块：
//     - 读取 io.EOF：结束读取
//     - 读到源名称错误：跳过继续
//     - 读到其他错误：包装并返回
//  3. 处理读取结果
//     - 空流：返回 emptyStreamConcatErr
//     - 单元素：直接返回该元素
//     - 多元素：调用自定义合并函数
//
// 错误处理：
//   - 自动跳过带源名称的错误（多流合并时的标记）
//   - 包装底层错误为新的流读取错误
//   - 处理空流特殊情况
//
// 设计优势：
//   - 自动资源管理：defer 确保流关闭
//   - 灵活合并：通过注册机制支持任意类型
//   - 错误透明：保留原始错误信息
//   - 性能优化：单元素直接返回，无额外开销
//
// 应用场景：
//   - Invoke 模式调用流式组件
//   - 流式数据的降维处理
//   - 检查点的保存和恢复
//   - 流式聚合操作
//
// 示例：
//
//	// 流式 ChatModel 转换为 Invoke 模式
//	stream := model.Stream(ctx, messages)
//	result, err := concatStreamReader[*Message](stream)
//	// 现在 result 是单个 *Message，可以按 Invoke 模式使用
func concatStreamReader[T any](sr *schema.StreamReader[T]) (T, error) {
	defer sr.Close()

	var items []T

	for {
		chunk, err := sr.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}

			if _, ok := schema.GetSourceName(err); ok {
				continue
			}

			var t T

			return t, newStreamReadError(err)
		}

		items = append(items, chunk)
	}

	if len(items) == 0 {
		var t T
		return t, emptyStreamConcatErr
	}

	if len(items) == 1 {
		return items[0], nil
	}

	res, err := internal.ConcatItems(items)
	if err != nil {
		var t T
		return t, err
	}

	return res, nil
}
