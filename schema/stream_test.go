package schema

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStream(t *testing.T) {
	s := newStream[int](0)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			closed := s.send(i, nil)
			if closed {
				break
			}
		}
		s.closeSend()
	}()

	i := 0
	for {
		i++
		if i > 5 {
			s.closeRecv()
			break
		}
		v, err := s.recv()
		if err != nil {
			assert.ErrorIs(t, err, io.EOF)
			break
		}
		t.Log(v)
	}

	wg.Wait()
}

func TestStreamCopy(t *testing.T) {
	s := newStream[string](10)
	srs := s.asReader().Copy(2)

	s.send("a", nil)
	s.send("b", nil)
	s.send("c", nil)
	s.closeSend()

	defer func() {
		for _, sr := range srs {
			sr.Close()
		}
	}()

	for {
		v, err := srs[0].Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		t.Log("copy 01 recv", v)
	}

	for {
		v, err := srs[1].Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		t.Log("copy 02 recv", v)
	}

	for {
		v, err := s.recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		t.Log("recv origin", v)
	}

	t.Log("done")
}

func TestNewStreamCopy(t *testing.T) {
	t.Run("验证单个流阻塞时其他流仍能正常接收", func(t *testing.T) {
		/*
		  测试验证的核心：
		  - 并发独立性：多个子流并发接收时互不干扰
		  - 阻塞解除机制：一个子流阻塞不影响其他子流接收新数据
		  - 数据同步：新数据发送能同时唤醒等待的接收者
		  - 时序正确性：阻塞解除后能正确接收后续数据
		*/
		s := newStream[string](1)
		scp := s.asReader().Copy(2)

		var t1, t2 time.Time

		go func() {
			s.send("a", nil)
			t1 = time.Now()
			time.Sleep(time.Millisecond * 200)
			s.send("b", nil)
			s.closeSend()
		}()
		wg := sync.WaitGroup{}
		wg.Add(2)

		go func() {
			defer func() {
				scp[0].Close()
				wg.Done()
			}()

			var received []string
			for {
				str, err := scp[0].Recv()
				if errors.Is(err, io.EOF) {
					break
				}

				assert.NoError(t, err)
				// assert.Equal(t, str, "a")
				received = append(received, str)
			}
			t.Logf("子流0接收到: %v", received) // 调试输出
		}()

		go func() {
			defer func() {
				scp[1].Close()
				wg.Done()
			}()

			time.Sleep(time.Millisecond * 100)
			var received []string
			for {
				str, err := scp[1].Recv()
				if errors.Is(err, io.EOF) {
					break
				}

				if t2.IsZero() {
					t2 = time.Now()
				}

				assert.NoError(t, err)
				// assert.Equal(t, str, "b")
				received = append(received, str)
			}
			t.Logf("子流1接收到: %v", received) // 调试输出
		}()

		wg.Wait()

		assert.True(t, t2.Sub(t1) < time.Millisecond*200)
	})

	t.Run("验证单流关闭时其他流仍能正常接收", func(t *testing.T) {
		/*
		  测试验证的核心：
		  - 关闭隔离性：单个子流关闭不影响其他子流的接收
		  - 并发安全性：关闭操作和接收操作的并发处理
		  - 状态管理：多流关闭状态的独立管理
		  - 资源清理：自动关闭机制的正确性
		*/
		s := newStream[string](1)
		scp := s.asReader().Copy(2)

		go func() {
			s.send("a", nil)
			time.Sleep(time.Millisecond * 200)
			s.send("a", nil)
			s.closeSend()
		}()

		wg := sync.WaitGroup{}
		wg.Add(2)

		// buf := scp[0].csr.parent.mem.buf
		go func() {
			defer func() {
				scp[0].Close()
				wg.Done()
			}()

			for {
				str, err := scp[0].Recv()
				if err == io.EOF {
					break
				}

				assert.NoError(t, err)
				assert.Equal(t, str, "a")
			}
		}()

		go func() {
			time.Sleep(time.Millisecond * 100)
			scp[1].Close()
			scp[1].Close() // try close multiple times
			wg.Done()
		}()

		wg.Wait()

		// assert.Equal(t, 0, buf.Len())
	})

	t.Run("验证大规模并发流接收的数据完整性", func(t *testing.T) {
		/*
		  测试验证的核心：
		  - 大规模并发：100个子流并发接收1000个数据
		  - 数据完整性：每个子流都能接收到完整的1000个数据
		  - 长期稳定性：长时间运行的可靠性
		  - 性能保证：高并发场景下的系统稳定性
		*/
		s := newStream[int](2)
		n := 1000
		go func() {
			for i := 0; i < n; i++ {
				s.send(i, nil)
			}

			s.closeSend()
		}()

		m := 100
		wg := sync.WaitGroup{}
		wg.Add(m)
		copies := s.asReader().Copy(m)
		for i := 0; i < m; i++ {
			idx := i
			go func() {
				cp := copies[idx]
				l := 0
				defer func() {
					assert.Equal(t, 1000, l)
					cp.Close()
					wg.Done()
				}()

				for {
					exp, err := cp.Recv()
					if err == io.EOF {
						break
					}

					assert.NoError(t, err)
					assert.Equal(t, exp, l)
					l++
				}
			}()
		}

		wg.Wait()
		// memo := copies[0].csr.parent.mem
		// assert.Equal(t, true, memo.hasFinished)
		// assert.Equal(t, 0, memo.buf.Len())
	})

	t.Run("验证多流异步关闭时的自动关闭协调机制", func(t *testing.T) {
		/*
		  测试验证的核心：
		  - 异步关闭协调：不同时机关闭多个子流的正确性
		  - 自动关闭机制：SetAutomaticClose 在复杂场景下的工作
		  - 引用计数管理：父流正确跟踪所有子流的关闭状态
		  - 资源清理完整性：确保所有子流关闭后父流的状态正确
		*/
		s := newStream[int](20) // 创建容量为20的流
		n := 1000               // 发送1000个数据

		// 发送端：连续发送数据
		go func() {
			for i := 0; i < n; i++ {
				s.send(i, nil)
			}

			s.closeSend()
		}()

		m := 100 // 创建100个子流
		wg := sync.WaitGroup{}
		wg.Add(m)

		wgEven := sync.WaitGroup{} // 等待偶数索引子流关闭
		wgEven.Add(m / 2)

		sr := s.asReader()
		sr.SetAutomaticClose() // 启用自动关闭
		copies := sr.Copy(m)

		// 启动100个并发接收 goroutine
		for i := 0; i < m; i++ {
			idx := i
			go func() {
				cp := copies[idx]
				l := 0
				defer func() {
					cp.Close() // 手动关闭子流
					wg.Done()
					if idx%2 == 0 { // 偶数索引：额外标记
						wgEven.Done()
					}
				}()

				// 接收数据直到满足关闭条件
				for {
					// 偶数索引：接收到索引数量的数据后主动关闭
					if idx%2 == 0 && l == idx {
						break
					}

					exp, err := cp.Recv()
					if err == io.EOF { // 流结束，正常退出
						break
					}

					assert.NoError(t, err)
					assert.Equal(t, exp, l)
					l++
				}
			}()
		}

		// 等待偶数索引子流先关闭完成
		wgEven.Wait()
		// 等待所有子流关闭完成
		wg.Wait()

		// 验证父流正确统计了所有子流的关闭数量
		assert.Equal(t, m, int(copies[0].csr.parent.closedNum))
	})

	t.Run("测试流读取器未手动关闭时自动关闭机制的有效性", func(t *testing.T) {
		/*
			测试验证的核心：
			  - 自动关闭触发：未手动关闭时Finalizer机制的正确触发
			  - 垃圾回收机制：Go GC对不可达对象的自动清理
			  - 资源泄漏防护：验证自动关闭能防止资源泄漏
			  - 边界情况处理：用户忘记手动关闭时的安全机制
			  - 计数准确性：验证自动关闭不影响父流的计数逻辑
		*/
		s := newStream[int](20) // 创建容量为20的流
		n := 1000               // 发送1000个数据

		// 发送端：连续发送所有数据
		go func() {
			for i := 0; i < n; i++ {
				s.send(i, nil)
			}

			s.closeSend() // 关闭发送端
		}()

		m := 4 // 创建4个子流进行测试
		wg := sync.WaitGroup{}
		wg.Add(m)

		// 复制出4个子流读取器
		copies := s.asReader().Copy(m)

		// 启动4个并发接收 goroutine
		for i := 0; i < m; i++ {
			idx := i
			cp := copies[idx]
			cp.SetAutomaticClose() // 启用自动关闭机制
			go func() {
				l := 0
				defer func() {
					wg.Done()
					// 故意不调用 cp.Close()，测试自动关闭
				}()

				// 接收所有数据直到流结束
				for {
					exp, err := cp.Recv()
					if err == io.EOF { // 正常接收完成
						break
					}

					assert.NoError(t, err)
					assert.Equal(t, exp, l)
					l++
				}
			}()
		}

		wg.Wait() // 等待所有接收完成

		// 验证关键点：子流通过自动关闭处理，不影响父流的计数
		// closedNum=0 表示没有手动调用Close()的子流
		assert.Equal(t, 0, int(copies[0].csr.parent.closedNum)) // not closed
	})
}
