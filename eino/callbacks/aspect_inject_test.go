package callbacks

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/schema"
)

// TestAspectInject 测试面向切面编程的注入函数在各种场景下的行为
func TestAspectInject(t *testing.T) {
	// 子测试：无回调管理器的上下文环境
	//
	// 测试目标：
	//   - 验证在没有回调管理器的情况下，注入函数的优雅降级行为
	//   - 确保在没有处理器的情况下，注入函数不会影响正常的业务流程
	//   - 验证流式处理在无回调管理器时的正确性
	//
	// 预期行为：
	//   - 所有注入函数应该静默通过，不影响执行流程
	//   - 流式数据应该原样传递，不丢失或改变
	//   - 即使没有回调管理器，上下文和流对象也应该正常工作
	t.Run("ctx without manager", func(t *testing.T) {
		// 准备测试环境：创建一个没有回调管理器的纯净上下文
		ctx := context.Background()

		// 场景1：测试普通回调注入函数的降级行为
		// 这些调用在没有回调管理器时应该静默通过，不影响业务逻辑
		ctx = OnStart(ctx, 1)               // 开始回调注入
		ctx = OnEnd(ctx, 2)                 // 结束回调注入
		ctx = OnError(ctx, fmt.Errorf("3")) // 错误回调注入

		// 场景2：测试流式输入处理的降级行为
		// 创建输入流管道，容量为2，用于测试流式回调注入
		isr, isw := schema.Pipe[int](2)

		// 在独立的 goroutine 中发送测试数据
		go func() {
			for i := 0; i < 10; i++ {
				isw.Send(i, nil) // 发送数字 0-9
			}
			isw.Close() // 关闭写入端，表示数据发送完毕
		}()

		// 通过回调注入函数处理流式输入
		var nisr *schema.StreamReader[int]
		ctx, nisr = OnStartWithStreamInput(ctx, isr)

		// 验证流式数据的完整性和顺序
		j := 0
		for {
			i, err := nisr.Recv()
			if err == io.EOF {
				break // 流正常结束
			}

			// 验证点1：流式数据接收无错误
			assert.NoError(t, err)
			// 验证点2：数据顺序和内容正确（0,1,2,3...）
			assert.Equal(t, j, i)
			j++
		}
		nisr.Close() // 清理资源

		// 场景3：测试流式输出处理的降级行为
		// 创建输出流管道，测试 OnEndWithStreamOutput 的行为
		osr, osw := schema.Pipe[int](2)

		// 在独立的 goroutine 中发送测试数据
		go func() {
			for i := 0; i < 10; i++ {
				osw.Send(i, nil) // 发送数字 0-9
			}
			osw.Close() // 关闭写入端
		}()

		// 通过回调注入函数处理流式输出
		var nosr *schema.StreamReader[int]
		ctx, nosr = OnEndWithStreamOutput(ctx, osr)

		// 验证流式输出的完整性和顺序
		j = 0
		for {
			i, err := nosr.Recv()
			if err == io.EOF {
				break // 流正常结束
			}

			// 验证点3：流式输出接收无错误
			assert.NoError(t, err)
			// 验证点4：输出数据顺序和内容正确（0,1,2,3...）
			assert.Equal(t, j, i)
			j++
		}
		nosr.Close() // 清理资源
	})
}
