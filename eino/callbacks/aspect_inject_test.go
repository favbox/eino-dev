package callbacks

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/internal/callbacks"
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

	// 子测试：有回调管理器的上下文环境
	//
	// 测试目标：
	//   - 验证在有回调管理器和处理器的情况下，注入函数能正确触发回调处理器
	//   - 确保所有类型的回调（普通、流式输入、流式输出）都能被正确执行
	//   - 验证回调处理器能够正确访问和处理传入的数据
	//   - 测试回调管理器的复用和处理器执行顺序
	//
	// 预期行为：
	//   - 每个注入函数调用都应该触发对应的回调处理器
	//   - 回调处理器应该能够正确访问和处理传入的参数
	//   - 流式回调处理器应该能够消费流数据并正确处理流的生命周期
	//   - 最终计数器值应该等于所有回调处理器处理的数据总和
	//
	// 计数预期：186 = 1(OnStart) + 2(OnEnd) + 3(OnError) + 45(流输入回调) + 45(流输出回调) + 45(业务代码消费流输入) + 45(业务代码消费流输出)
	t.Run("ctx with manager", func(t *testing.T) {
		// 准备测试环境：创建基础上下文和计数器
		ctx := context.Background()
		cnt := 0 // 用于统计所有回调处理器处理的数据总和

		// 创建完整的回调处理器，包含所有类型的回调处理
		handler := NewHandlerBuilder().
			// 处理组件开始事件的回调
			OnStartFn(func(ctx context.Context, info *RunInfo, input CallbackInput) context.Context {
				cnt += input.(int) // 累加输入值
				return ctx
			}).
			// 处理组件结束事件的回调
			OnEndFn(func(ctx context.Context, info *RunInfo, output CallbackOutput) context.Context {
				cnt += output.(int) // 累加输出值
				return ctx
			}).
			// 处理组件错误事件的回调
			OnErrorFn(func(ctx context.Context, info *RunInfo, err error) context.Context {
				// 从错误信息中解析数值并累加
				v, _ := strconv.ParseInt(err.Error(), 10, 64)
				cnt += int(v)
				return ctx
			}).
			// 处理流式输入开始的回调
			OnStartWithStreamInputFn(func(ctx context.Context, info *RunInfo, input *schema.StreamReader[CallbackInput]) context.Context {
				// 消费整个流式输入并累加所有数据
				for {
					i, err := input.Recv()
					if err == io.EOF {
						break
					}
					cnt += i.(int) // 累加流中的每个数值
				}
				input.Close() // 关闭流，释放资源
				return ctx
			}).
			// 处理流式输出结束的回调
			OnEndWithStreamOutputFn(func(ctx context.Context, info *RunInfo, output *schema.StreamReader[CallbackOutput]) context.Context {
				// 消费整个流式输出并累加所有数据
				for {
					o, err := output.Recv()
					if err == io.EOF {
						break
					}
					cnt += o.(int) // 累加流中的每个数值
				}
				output.Close() // 关闭流，释放资源
				return ctx
			}).Build()

		// 初始化回调管理器，将处理器注册到上下文中
		ctx = InitCallbacks(ctx, nil, handler)

		// 场景1：测试普通回调注入函数的处理器触发
		ctx = OnStart(ctx, 1)               // 应该触发 OnStart 回调，cnt += 1
		ctx = OnEnd(ctx, 2)                 // 应该触发 OnEnd 回调，cnt += 2
		ctx = OnError(ctx, fmt.Errorf("3")) // 应该触发 OnError 回调，cnt += 3

		// 场景2：测试流式输入回调的处理器触发
		// 创建输入流管道，容量为2
		isr, isw := schema.Pipe[int](2)

		// 在独立 goroutine 中发送测试数据（0-9）
		go func() {
			for i := 0; i < 10; i++ {
				isw.Send(i, nil) // 发送数字 0-9
			}
			isw.Close() // 关闭写入端
		}()

		// 复用现有的处理器来测试回调管理器的复用机制
		ctx = ReuseHandlers(ctx, &RunInfo{})

		// 通过回调注入函数处理流式输入，应该触发 OnStartWithStreamInput 回调
		var nisr *schema.StreamReader[int]
		ctx, nisr = OnStartWithStreamInput(ctx, isr)

		// 业务代码也需要消费流数据（模拟真实使用场景）
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
			cnt += i // 业务代码也消费数据，计入总数
		}
		nisr.Close() // 清理资源

		// 场景3：测试流式输出回调的处理器触发
		// 创建输出流管道，容量为2
		osr, osw := schema.Pipe[int](2)

		// 在独立 goroutine 中发送测试数据（0-9）
		go func() {
			for i := 0; i < 10; i++ {
				osw.Send(i, nil) // 发送数字 0-9
			}
			osw.Close() // 关闭写入端
		}()

		// 通过回调注入函数处理流式输出，应该触发 OnEndWithStreamOutput 回调
		var nosr *schema.StreamReader[int]
		ctx, nosr = OnEndWithStreamOutput(ctx, osr)

		// 业务代码消费流输出数据
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
			cnt += i // 业务代码也消费数据，计入总数
		}
		nosr.Close() // 清理资源

		// 验证点5：验证所有回调处理器的执行结果
		// 预期计算：
		// - 普通回调：1(OnStart) + 2(OnEnd) + 3(OnError) = 6
		// - 流式输入回调处理器：0+1+2+...+9 = 45
		// - 流式输出回调处理器：0+1+2+...+9 = 45
		// - 业务代码消费流输入：0+1+2+...+9 = 45
		// - 业务代码消费流输出：0+1+2+...+9 = 45
		// 总计：6 + 45 + 45 + 45 + 45 = 186
		assert.Equal(t, 186, cnt)
	})
}

// TestGlobalCallbacksRepeated 测试全局回调处理器的重复执行防护机制
//
// 测试目标：
//   - 验证全局回调处理器不会被重复执行
//   - 确保即使多次调用 AppendHandlers，全局处理器也只执行一次
//   - 验证回调系统的去重机制，防止重复执行导致的副作用
//
// 问题背景：
//
//	在复杂的组件调用链中，同一个上下文可能会多次调用 AppendHandlers，
//	如果没有去重机制，全局回调处理器会被重复执行，可能导致：
//	- 重复的日志记录
//	- 重复的指标收集
//	- 重复的错误处理
//	- 资源泄漏或性能问题
//
// 预期行为：
//   - 无论调用多少次 AppendHandlers，全局回调处理器都应该只执行一次
//   - 本地处理器（用户特定处理器）不受影响，正常执行
func TestGlobalCallbacksRepeated(t *testing.T) {
	// 创建计数器用于统计回调处理器的执行次数
	times := 0

	// 创建测试用的全局回调处理器
	// 每次被调用时，计数器递增
	testHandler := NewHandlerBuilder().OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
		times++ // 记录执行次数
		return ctx
	}).Build()

	// 将测试处理器添加到全局处理器列表
	// 注意：这里直接操作内部包的 GlobalHandlers，模拟全局处理器的设置
	callbacks.GlobalHandlers = append(callbacks.GlobalHandlers, testHandler)

	// 创建测试上下文
	ctx := context.Background()

	// 场景：模拟在复杂调用链中多次调用 AppendHandlers
	// 这种情况在实际应用中可能出现，例如：
	// - 组件嵌套调用
	// - 中间件多次包装
	// - 错误处理中的重试逻辑
	ctx = callbacks.AppendHandlers(ctx, &RunInfo{}) // 第一次添加处理器
	ctx = callbacks.AppendHandlers(ctx, &RunInfo{}) // 第二次添加处理器（相同上下文）

	// 触发回调执行
	// 使用内部包的 On 函数直接触发回调，模拟真实的回调执行场景
	callbacks.On(ctx, "test", callbacks.OnStartHandle[string], TimingOnStart, true)

	// 验证点：全局回调处理器应该只执行一次
	// 如果 times == 2，说明存在重复执行问题
	// 如果 times == 1，说明去重机制正常工作
	assert.Equal(t, times, 1, "全局回调处理器应该只执行一次，避免重复执行导致的副作用")
}

// TestEnsureRunInfo 测试 EnsureRunInfo 函数的 RunInfo 更新和初始化功能
//
// 测试目标：
//   - 验证 EnsureRunInfo 能够正确更新现有上下文中的 RunInfo 信息
//   - 测试 EnsureRunInfo 在空上下文中初始化回调管理器的能力
//   - 确保更新后的 RunInfo 信息能够正确传递给回调处理器
//   - 验证 RunInfo 的各个字段（Name、Type、Component）都能被正确处理
//
// 功能说明：
//
//	EnsureRunInfo 函数用于确保上下文中的 RunInfo 与给定的类型和组件匹配。
//	如果当前回调管理器不匹配或不存在，会创建新的管理器同时保留现有处理器。
//	这个函数在组件切换或类型变更时非常有用，能够动态更新运行信息。
//
// 预期行为：
//   - 对于已有回调管理器的上下文，应该更新 RunInfo 而保留处理器
//   - 对于空上下文，应该初始化新的回调管理器和 RunInfo
//   - 更新后的 RunInfo 应该在后续的回调中生效
func TestEnsureRunInfo(t *testing.T) {
	// 准备测试环境：创建基础上下文
	ctx := context.Background()

	// 创建变量用于捕获回调处理器中的 RunInfo 信息
	var name, typ, comp string

	// 场景1：在已有回调管理器的上下文中测试 EnsureRunInfo
	// 初始化回调管理器，设置初始的 RunInfo 和回调处理器
	ctx = InitCallbacks(ctx, &RunInfo{Name: "name", Type: "type", Component: "component"},
		NewHandlerBuilder().OnStartFn(func(ctx context.Context, info *RunInfo, input CallbackInput) context.Context {
			// 捕获 RunInfo 信息到外部变量，用于验证
			name = info.Name
			typ = info.Type
			comp = string(info.Component)
			return ctx
		}).Build())

	// 验证初始 RunInfo 信息：触发回调处理器，确认初始信息正确传递
	ctx = OnStart(ctx, "")
	// 验证点1：初始 RunInfo 的各个字段应该正确
	assert.Equal(t, "name", name, "初始 RunInfo 的 Name 字段应该正确")
	assert.Equal(t, "type", typ, "初始 RunInfo 的 Type 字段应该正确")
	assert.Equal(t, "component", comp, "初始 RunInfo 的 Component 字段应该正确")

	// 使用 EnsureRunInfo 更新 RunInfo 的 Type 和 Component 信息
	// 注意：这里只更新 Type 和 Component，Name 会被清空
	ctx2 := EnsureRunInfo(ctx, "type2", "component2")

	// 验证更新后的 RunInfo 信息：触发回调处理器，确认更新信息生效
	OnStart(ctx2, "")
	// 验证点2：更新后的 RunInfo 信息应该正确，Name 被清空
	assert.Equal(t, "", name, "EnsureRunInfo 后 Name 字段应该被清空")
	assert.Equal(t, "type2", typ, "EnsureRunInfo 后 Type 字段应该被更新为 'type2'")
	assert.Equal(t, "component2", comp, "EnsureRunInfo 后 Component 字段应该被更新为 'component2'")

	// 场景2：在空上下文中测试 EnsureRunInfo 的初始化能力
	// 这个场景测试当上下文中没有回调管理器时，EnsureRunInfo 是否能正确初始化

	// 添加全局回调处理器来捕获新初始化的 RunInfo 信息
	AppendGlobalHandlers(NewHandlerBuilder().OnStartFn(func(ctx context.Context, info *RunInfo, input CallbackInput) context.Context {
		// 捕获新初始化的 RunInfo 信息
		typ = info.Type
		comp = string(info.Component)
		return ctx
	}).Build())

	// 在完全空的上下文上调用 EnsureRunInfo
	// 这应该会创建新的回调管理器并设置 RunInfo
	ctx3 := EnsureRunInfo(context.Background(), "type3", "component3")

	// 验证在空上下文中初始化的效果
	OnStart(ctx3, 0)
	// 验证点3：在空上下文中初始化的 RunInfo 信息应该正确
	assert.Equal(t, "type3", typ, "空上下文中 EnsureRunInfo 后 Type 字段应该正确")
	assert.Equal(t, "component3", comp, "空上下文中 EnsureRunInfo 后 Component 字段应该正确")

	// 清理：重置全局处理器，避免影响其他测试
	callbacks.GlobalHandlers = []Handler{}
}

// TestNesting 测试回调系统的嵌套调用和处理器复用机制
//
// 测试目标：
//   - 验证回调的嵌套调用行为和执行顺序
//   - 测试 ReuseHandlers 函数的处理器复用机制
//   - 确保嵌套场景下每个回调都能被正确触发和执行
//   - 验证处理器复用时回调计数器的准确性
//
// 问题背景：
//
//	在复杂的组件调用链中，经常会出现回调的嵌套调用，例如：
//	- 组件 A 调用组件 B，两者都有回调处理器
//	- 在回调处理过程中再次触发新的回调
//	- 使用 ReuseHandlers 复用处理器时的新回调行为
//
//	如果处理不当，可能导致：
//	- 回调执行顺序混乱
//	- 处理器复用时的意外行为
//	- 回调计数器不准确
//
// 预期行为：
//   - 嵌套调用时，每个回调都应该被正确触发
//   - 处理器复用时，新旧 RunInfo 的回调都能正常工作
//   - 回调计数器准确反映执行次数
func TestNesting(t *testing.T) {
	// 准备测试环境：创建基础上下文和回调计数器
	ctx := context.Background()
	cb := &myCallback{t: t}

	// 初始化回调管理器，设置初始 RunInfo 和回调处理器
	ctx = InitCallbacks(ctx, &RunInfo{
		Name: "test",
	}, cb)

	// 场景1：测试嵌套调用（"jumped" 场景）
	//
	// 这个场景模拟了回调的嵌套调用，即在一次回调执行过程中触发另一次回调。
	// 执行流程：
	//   1. OnStart(ctx, 0) - 第一次调用 OnStart，参数为 0
	//      - 触发回调处理器的 OnStart，输入参数 0
	//      - 返回新上下文 ctx1
	//   2. OnStart(ctx1, 1) - 在第一个回调的上下文中再次调用 OnStart，参数为 1
	//      - 触发回调处理器的 OnStart，输入参数 1
	//      - 返回新上下文 ctx2（嵌套）
	//   3. OnEnd(ctx2, 1) - 结束第二次调用，参数为 1
	//      - 触发回调处理器的 OnEnd，输出参数 1
	//   4. OnEnd(ctx1, 0) - 结束第一次调用，参数为 0
	//      - 触发回调处理器的 OnEnd，输出参数 0
	//
	// 总计应该触发 4 次回调：
	//   - 第一次 OnStart（参数0）
	//   - 第二次 OnStart（参数1） - 嵌套
	//   - 第二次 OnEnd（参数1）
	//   - 第一次 OnEnd（参数0）

	// 第一层回调：开始调用，参数为 0
	ctx1 := OnStart(ctx, 0)

	// 第二层回调：在第一层回调的上下文中嵌套调用，参数为 1
	ctx2 := OnStart(ctx1, 1)

	// 结束第二层回调，输出参数为 1
	OnEnd(ctx2, 1)

	// 结束第一层回调，输出参数为 0
	OnEnd(ctx1, 0)

	// 验证点1：嵌套调用应该触发 4 次回调（2次 OnStart + 2次 OnEnd）
	assert.Equal(t, 4, cb.times, "嵌套调用应该触发 4 次回调")

	// 场景2：测试处理器复用机制（"reused" 场景）
	//
	// 这个场景测试 ReuseHandlers 函数的行为，即在已有回调管理器的情况下，
	// 使用新的 RunInfo 重新绑定处理器，并测试新上下文的回调行为。
	// 执行流程：
	//   1. OnStart(ctx, 0) - 第一次调用 OnStart，参数为 0
	//      - 触发回调处理器的 OnStart，输入参数 0
	//      - 返回新上下文 ctx1
	//   2. ReuseHandlers(ctx1, &RunInfo{Name: "test2"})
	//      - 复用现有的回调管理器，但使用新的 RunInfo{Name: "test2"}
	//      - 返回新上下文 ctx2
	//   3. OnStart(ctx2, 1) - 在复用上下文中调用 OnStart，参数为 1
	//      - 触发回调处理器的 OnStart，输入参数 1
	//      - 返回新上下文 ctx3
	//   4. OnEnd(ctx3, 1) - 结束调用，参数为 1
	//      - 触发回调处理器的 OnEnd，输出参数 1
	//   5. OnEnd(ctx1, 0) - 结束原始调用，参数为 0
	//      - 触发回调处理器的 OnEnd，输出参数 0
	//
	// 总计也应该触发 4 次回调：
	//   - 第一次 OnStart（参数0）
	//   - ReuseHandlers 后的 OnStart（参数1）
	//   - ReuseHandlers 后的 OnEnd（参数1）
	//   - 第一次 OnEnd（参数0）

	// 重置回调计数器，准备第二个场景的测试
	cb.times = 0

	// 第一层回调：开始调用，参数为 0
	ctx1 = OnStart(ctx, 0)

	// 复用处理器：使用新的 RunInfo 重新绑定回调管理器
	ctx2 = ReuseHandlers(ctx1, &RunInfo{Name: "test2"})

	// 复用上下文的回调：开始调用，参数为 1
	ctx3 := OnStart(ctx2, 1)

	// 结束复用上下文的回调，输出参数为 1
	OnEnd(ctx3, 1)

	// 结束原始调用，输出参数为 0
	OnEnd(ctx1, 0)

	// 验证点2：处理器复用后也应该触发 4 次回调
	assert.Equal(t, 4, cb.times, "处理器复用后应该触发 4 次回调")
}

// TestReuseHandlersOnEmptyCtx 测试 ReuseHandlers 函数在空上下文中的初始化能力
//
// 测试目标：
//   - 验证 ReuseHandlers 能在完全空的上下文中正确初始化回调管理器
//   - 确保在空上下文中 ReuseHandlers 能够正确设置 RunInfo 信息
//   - 验证全局回调处理器在通过 ReuseHandlers 初始化的上下文中能够正常工作
//   - 测试 ReuseHandlers 在空上下文中的初始化和回调触发机制
//
// 问题背景：
//
//	在实际应用中，经常会遇到需要在完全新的上下文中复用或初始化回调管理器的情况，
//	例如：
//	- 在请求开始时创建一个全新的回调上下文
//	- 在独立的 goroutine 中使用回调功能
//	- 在没有预先初始化回调的组件中启用回调功能
//
//	如果 ReuseHandlers 不能正确处理空上下文，可能会导致：
//	- 回调功能完全不可用
//	- 全局处理器无法被触发
//	- RunInfo 信息无法正确设置
//	- 上下文初始化失败
//
// 预期行为：
//   - ReuseHandlers 应该在空上下文中创建新的回调管理器
//   - 全局处理器应该能够正常工作
//   - RunInfo 信息应该被正确设置
//   - 回调应该只被触发一次
func TestReuseHandlersOnEmptyCtx(t *testing.T) {
	// 清理环境：确保全局处理器列表为空，避免其他测试的干扰
	// 这确保了测试的独立性，每个测试都在干净的环境中运行
	callbacks.GlobalHandlers = []Handler{}

	// 创建测试用的回调处理器实例
	// myCallback 结构体实现了 Handler 接口，用于记录回调被调用的次数
	cb := &myCallback{t: t}

	// 将测试处理器设置为全局处理器
	// 这样在后续的回调中，全局处理器会被自动触发
	AppendGlobalHandlers(cb)

	// 关键测试：在完全空的上下文中调用 ReuseHandlers
	// context.Background() 返回一个空的上下文，没有任何回调管理器或处理器
	// ReuseHandlers 应该：
	//   1. 检测到上下文为空
	//   2. 自动初始化新的回调管理器
	//   3. 设置提供的 RunInfo{Name: "test"}
	//   4. 准备接受后续的回调触发
	ctx := ReuseHandlers(context.Background(), &RunInfo{Name: "test"})

	// 触发回调执行
	// 在 ReuseHandlers 初始化后的上下文中调用 OnStart
	// 应该会：
	//   1. 触发全局处理器（cb）
	//   2. 正确设置 RunInfo 信息
	//   3. 返回处理后的上下文
	OnStart(ctx, 0)

	// 验证点：全局处理器应该只被触发一次
	// 如果 cb.times == 1，说明：
	//   - ReuseHandlers 在空上下文中正确初始化了回调管理器
	//   - 全局处理器在新的回调管理器中被正确触发
	//   - RunInfo 信息被正确传递给了回调处理器
	//
	// 如果 cb.times != 1，说明存在问题：
	//   - 可能初始化失败
	//   - 可能全局处理器没有被正确触发
	//   - 可能回调被重复触发
	assert.Equal(t, 1, cb.times, "在空上下文中 ReuseHandlers 后，全局处理器应该只被触发一次")
}

// TestAppendHandlersTwiceOnSameCtx 测试 AppendHandlers 函数的上下文独立性和处理器组合机制
//
// 测试目标：
//   - 验证在同一个初始上下文中多次调用 AppendHandlers 能创建独立的上下文
//   - 确保不同上下文的回调处理器互不干扰，独立计数
//   - 验证初始回调处理器在后续创建的上下文中被保留和触发
//   - 测试本地处理器与全局处理器的正确组合机制
//
// 问题背景：
//
//	在复杂的组件调用场景中，经常需要基于同一个基础上下文创建多个独立的回调上下文，
//	例如：
//	- 在并行处理多个任务时，每个任务需要独立的回调环境
//	- 在组件链式调用中，每个组件需要独立的回调处理器
//	- 在嵌套组件调用中，需要为不同调用分支创建独立的回调上下文
//
//	如果 AppendHandlers 不能正确处理这种情况，可能会导致：
//	- 不同上下文的回调处理器相互干扰
//	- 初始上下文中的处理器丢失或无法访问
//	- 处理器计数错误或重复触发
//	- 上下文隔离失效，影响回调的独立性
//
// 预期行为：
//   - 从同一上下文创建的新上下文应该相互独立
//   - 每个上下文的回调处理器应该独立计数，不相互干扰
//   - 初始上下文中的处理器应该在新创建的上下文中被保留
//   - 全局处理器应该在所有上下文中正常工作
func TestAppendHandlersTwiceOnSameCtx(t *testing.T) {
	// 清理环境：确保全局处理器列表为空，避免其他测试的干扰
	// 这确保了测试的独立性，每个测试都在干净的环境中运行
	callbacks.GlobalHandlers = []Handler{}

	// 创建测试用的回调处理器实例
	// cb: 初始处理器，将被包含在所有后续创建的上下文中
	// cb1: 第一个新处理器，只在 ctx1 中生效
	// cb2: 第二个新处理器，只在 ctx2 中生效
	cb := &myCallback{t: t}
	cb1 := &myCallback{t: t}
	cb2 := &myCallback{t: t}

	// 步骤1：初始化基础上下文
	// 创建一个包含初始处理器 cb 的上下文 ctx
	// 这个上下文将作为后续所有 AppendHandlers 调用的基础
	ctx := InitCallbacks(context.Background(), &RunInfo{Name: "test"}, cb)

	// 步骤2：第一次调用 AppendHandlers
	// 基于 ctx 创建第一个新上下文 ctx1，包含新的处理器 cb1
	// 注意：这里使用的是原始的 ctx，而不是 ctx1，确保测试的是真正的"同一上下文"
	ctx1 := callbacks.AppendHandlers(ctx, &RunInfo{Name: "test"}, cb1)

	// 步骤3：第二次调用 AppendHandlers
	// 再次基于 ctx（原始上下文）创建第二个新上下文 ctx2，包含新的处理器 cb2
	// 这创建了两个独立的上下文 ctx1 和 ctx2，它们都基于相同的原始上下文 ctx
	ctx2 := callbacks.AppendHandlers(ctx, &RunInfo{Name: "test"}, cb2)

	// 步骤4：在第一个上下文中触发回调
	// 在 ctx1 上触发 OnStart
	// 应该触发两个处理器：
	//   1. cb（初始处理器）- 来自基础上下文的继承
	//   2. cb1（第一个新处理器）- 来自 ctx1 的特有处理器
	// 预期结果：cb.times = 1, cb1.times = 1
	OnStart(ctx1, 0)

	// 步骤5：在第二个上下文中触发回调
	// 在 ctx2 上触发 OnStart
	// 应该触发两个处理器：
	//   1. cb（初始处理器）- 来自基础上下文的继承
	//   2. cb2（第二个新处理器）- 来自 ctx2 的特有处理器
	// 注意：cb1 不应该被触发，因为 ctx2 独立于 ctx1
	// 预期结果：cb.times = 2, cb2.times = 1
	OnStart(ctx2, 0)

	// 验证点1：初始处理器 cb 应该被触发两次
	// 说明：
	//   - cb 在 ctx1 和 ctx2 中都被保留和触发
	//   - 初始上下文中的处理器被正确继承到新创建的上下文中
	assert.Equal(t, 2, cb.times, "初始处理器 cb 应该在两个上下文中都被触发")

	// 验证点2：第一个新处理器 cb1 应该只被触发一次
	// 说明：
	//   - cb1 只在 ctx1 中生效
	//   - ctx2 不会触发 cb1
	//   - 上下文之间保持完全独立
	assert.Equal(t, 1, cb1.times, "第一个新处理器 cb1 应该只在 ctx1 中被触发一次")

	// 验证点3：第二个新处理器 cb2 应该只被触发一次
	// 说明：
	//   - cb2 只在 ctx2 中生效
	//   - ctx1 不会触发 cb2
	//   - 上下文之间保持完全独立
	assert.Equal(t, 1, cb2.times, "第二个新处理器 cb2 应该只在 ctx2 中被触发一次")

	// 总结：
	// 这个测试验证了 AppendHandlers 的两个核心特性：
	//   1. 独立性：从同一上下文创建的新上下文相互独立，互不干扰
	//   2. 继承性：新创建的上下文会保留基础上下文中的处理器
}

type myCallback struct {
	t     *testing.T
	times int
}

func (m *myCallback) OnStart(ctx context.Context, info *RunInfo, input CallbackInput) context.Context {
	m.times++
	if info == nil {
		assert.Equal(m.t, 2, m.times)
		return ctx
	}
	if info.Name == "test" {
		assert.Equal(m.t, 0, input)
	} else {
		assert.Equal(m.t, 1, input)
	}

	return ctx
}

func (m *myCallback) OnEnd(ctx context.Context, info *RunInfo, output CallbackOutput) context.Context {
	m.times++
	if info == nil {
		assert.Equal(m.t, 3, m.times)
		return ctx
	}
	if info.Name == "test" {
		assert.Equal(m.t, 0, output)
	} else {
		assert.Equal(m.t, 1, output)
	}
	return ctx
}

func (m *myCallback) OnError(ctx context.Context, info *RunInfo, err error) context.Context {
	panic("implement me")
}

func (m *myCallback) OnStartWithStreamInput(ctx context.Context, info *RunInfo, input *schema.StreamReader[CallbackInput]) context.Context {
	panic("implement me")
}

func (m *myCallback) OnEndWithStreamOutput(ctx context.Context, info *RunInfo, output *schema.StreamReader[CallbackOutput]) context.Context {
	panic("implement me")
}
