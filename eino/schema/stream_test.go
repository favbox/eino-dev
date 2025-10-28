package schema

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 验证流的基础发送接收和主动关闭机制
func TestStream(t *testing.T) {
	// 测试验证的核心总结：
	//  验证流的基础通信机制，确保数据能够正确地从发送端传输到接收端，
	//  同时验证接收方主动关闭流的控制能力，
	//  测试无缓冲流的同步通信特性、双向关闭机制以及资源清理的正确性，
	//  体现流通信的可靠性和可控性保证。
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

// 验证流复制后的广播分发机制和独立接收能力
func TestStreamCopy(t *testing.T) {
	//  测试验证的核心总结：验证流复制后的广播分发机制，
	//  确保通过 Copy(2) 创建的子流能够独立、完整地接收原始流的所有数据，
	//  每个子流的接收操作互不干扰，并且所有流都能正确处理流结束信号，
	//  体现流复制系统在数据广播分发、独立接收和生命周期管理方面的可靠性保证。
	s := newStream[string](10)
	srs := s.asReader().Copy(2)

	s.send("a", nil)
	s.send("b", nil)
	s.send("c", nil)
	s.closeSend()

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

// checkStream - 验证流数据的正确性和完整性，确保接收到预期的0-9序列。
func checkStream(s *StreamReader[int]) error {
	defer s.Close()

	for i := 0; i < 10; i++ {
		chunk, err := s.Recv()
		if err != nil {
			return err
		}
		if chunk != i {
			return fmt.Errorf("receive err, expected:%d, actual: %d", i, chunk)
		}
	}
	_, err := s.Recv()
	if err != io.EOF {
		return fmt.Errorf("close chan fail")
	}
	return nil
}

// testStreamN - 测试指定容量和复制数量的流，验证三级链式复制的数据完整性。
func testStreamN(cap, n int) error {
	s := newStream[int](cap)
	go func() {
		for i := 0; i < 10; i++ {
			s.send(i, nil)
		}
		s.closeSend()
	}()

	vs := s.asReader().Copy(n) // 第一级复制：验证第一个子流
	err := checkStream(vs[0])
	if err != nil {
		return err
	}

	vs = vs[1].Copy(n) // 第二级复制：验证第一个孙流
	err = checkStream(vs[0])
	if err != nil {
		return err
	}
	vs = vs[1].Copy(n) // 第三级复制：验证第一个重孙流
	err = checkStream(vs[0])
	if err != nil {
		return err
	}
	return nil
}

// 验证流的多级链式复制机制和数据完整性保证
func TestCopy(t *testing.T) {
	// 测试验证的核心总结：验证流的多级链式复制机制的可靠性，
	// 确保通过 Copy-of-Copy 的方式创建多级复制链时，
	// 每一级复制都能正确、完整地接收原始数据，
	// 并在各种容量参数和复制数量的组合下保持数据一致性和顺序性，
	// 体现流复制系统在复杂链式场景下的数据完整性和系统稳定性保证。
	for i := 0; i < 10; i++ {
		for j := 2; j < 10; j++ {
			err := testStreamN(i, j)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestCopy5 - 验证复制流对关闭信号的重复处理能力，确保EOF信号的一致性。
func TestCopy5(t *testing.T) {
	// 测试验证的核心总结：验证复制流在处理流结束信号时的正确性和一致性，
	// 确保复制流能够正确处理原始流的关闭信号，并在重复调用Recv()时保持EOF信号的稳定性，
	// 体现流复制系统在生命周期管理和错误处理方面的可靠性保证。

	// 创建无缓冲流，确保发送和接收的严格同步
	s := newStream[int](0)

	// 发送端goroutine：发送0-9的数据并关闭发送
	go func() {
		for i := 0; i < 10; i++ {
			closed := s.send(i, nil)
			if closed {
				fmt.Printf("has closed")
			}
		}
		s.closeSend() // 关闭发送，通知接收方数据发送完毕
	}()

	// 创建5个子流的复制
	vs := s.asReader().Copy(5)
	time.Sleep(time.Second) // 等待数据发送完成

	defer func() {
		// 确保所有子流都被正确关闭
		for _, v := range vs {
			v.Close()
		}
	}()

	// 验证第一个子流的数据正确性
	for i := 0; i < 10; i++ {
		chunk, err := vs[0].Recv()
		if err != nil {
			t.Fatal(err)
		}
		if chunk != i {
			t.Fatalf("receive err, expected:%d, actual: %d", i, chunk)
		}
	}

	// 验证流结束信号：第一次接收应该返回EOF
	_, err := vs[0].Recv()
	if err != io.EOF {
		t.Fatalf("复制的流读取器无法返回EOF")
	}

	// 验证流结束信号：重复接收也应该返回EOF（而不是其他错误）
	_, err = vs[0].Recv()
	if err != io.EOF {
		t.Fatalf("复制的流读取器不能重复返回EOF")
	}
}

// TestStreamReaderWithConvert - 验证流读取器的数据转换功能，测试转换错误处理和自动关闭机制。
func TestStreamReaderWithConvert(t *testing.T) {
	// 测试验证的核心总结：验证 StreamReaderWithConvert 函数的流数据转换能力，
	// 确保在流处理过程中能够正确执行数据转换、处理转换错误、过滤失败数据，
	// 并通过自动关闭机制管理资源生命周期，体现流转换系统的错误隔离、数据过滤和自动化管理能力。

	// 创建容量为2的整数流
	s := newStream[int](2)

	var cntA int
	var e error

	// 转换函数：模拟转换错误，当输入为1时返回错误
	convA := func(src int) (int, error) {
		if src == 1 {
			return 0, fmt.Errorf("mock err") // 模拟转换失败
		}

		return src, nil // 其他值正常转换
	}

	// 创建带转换功能的流读取器，并启用自动关闭
	sta := StreamReaderWithConvert[int, int](s.asReader(), convA)
	sta.SetAutomaticClose() // 设置自动关闭，流结束时自动释放资源

	// 发送测试数据
	s.send(1, nil) // 会触发转换错误
	s.send(2, nil) // 正常转换
	s.closeSend()  // 关闭发送端

	// 循环接收转换后的数据
	for {
		item, err := sta.Recv()
		if err != nil {
			if err == io.EOF {
				break // 流正常结束
			}

			e = err // 保存转换错误，继续处理后续数据
			continue
		}

		cntA += item
	}

	// 验证结果
	assert.NotNil(t, e)      // 确保转换错误被正确捕获
	assert.Equal(t, cntA, 2) // 只有转换成功的数据被累加
}

// TestArrayStreamCombined - 验证数组流与流式读取器的合并功能，测试不同类型流的统一处理能力。
func TestArrayStreamCombined(t *testing.T) {
	// 测试验证的核心总结：验证 MergeStreamReaders 函数对不同类型流读取器的合并能力，
	// 确保数组流（静态数据）和流式读取器（动态数据）能够被统一合并为单一的数据流，
	// 并通过完整性、唯一性和正确性的验证，
	// 体现流合并系统在类型抽象、多源聚合和统一处理方面的强大能力。

	// 创建数组流读取器：包含静态数据 [0, 1, 2]
	asr := &StreamReader[int]{
		typ: readerTypeArray,
		ar: &arrayReader[int]{
			arr:   []int{0, 1, 2}, // 静态数组数据
			index: 0,              // 起始读取位置
		},
	}

	// 创建动态流：包含动态数据 [3, 4, 5]
	s := newStream[int](3)
	for i := 3; i < 6; i++ {
		s.send(i, nil) // 发送动态数据
	}
	s.closeSend() // 关闭动态流

	// 合并两个不同类型的流读取器
	nSR := MergeStreamReaders([]*StreamReader[int]{asr, s.asReader()})
	nSR.SetAutomaticClose() // 启用自动关闭

	// 使用记录数组验证数据的完整性和唯一性
	record := make([]bool, 6)
	for i := 0; i < 6; i++ {
		chunk, err := nSR.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if record[chunk] {
			t.Fatal("record duplicated") // 检查重复数据
		}
		record[chunk] = true // 标记已接收的数据
	}

	// 验证合并流正确结束，返回EOF信号
	_, err := nSR.Recv()
	if err != io.EOF {
		t.Fatal("reader haven't finish correctly")
	}

	// 验证所有数据都被正确接收（0,1,2,3,4,5）
	for i := range record {
		if !record[i] {
			t.Fatal("record missing") // 检查缺失数据
		}
	}
}

// TestMultiStream - 验证多流读取器的数据聚合和顺序控制能力，测试流ID和数据编码的正确解析。
func TestMultiStream(t *testing.T) {
	//  测试验证的核心总结：验证 newMultiStreamReader 函数的多流聚合能力，
	//  确保能够同时处理多个不同容量的流，保持每个流内部数据的严格顺序，
	//  并通过巧妙的编码解码机制验证数据完整性和顺序正确性，
	//  体现多流处理系统在数据聚合、顺序控制和并发处理方面的可靠性保证。

	var sts []*stream[int]
	sum := 0

	// 创建10个不同容量的流，每个流包含编码的流ID和数据序列
	for i := 0; i < 10; i++ {
		size := rand.Intn(10) + 1 // 随机容量1-10
		sum += size               // 累计总数据量

		st := newStream[int](size) // 创建指定容量的流

		// 向每个流发送编码数据：高位存流ID，低位存数据序号
		for j := 1; j <= size; j++ {
			// 编码格式：(流ID << 16) | 数据序号
			encoded := (j & 0xffff) | (i << 16)
			st.send(encoded, nil)
		}
		st.closeSend()        // 关闭单个流
		sts = append(sts, st) // 添加到流列表
	}

	// 创建多流读取器，聚合所有流的数据
	mst := newMultiStreamReader(sts)

	// 使用数组记录每个流的接收进度
	receiveList := make([]int, 10)

	// 接收所有聚合数据，验证顺序正确性
	for i := 0; i < sum; i++ {
		chunk, err := mst.recv()
		if err != nil {
			t.Fatal(err)
		}

		// 解码数据：提取流ID和数据序号
		streamID := chunk >> 16   // 高16位：流ID
		dataSeq := chunk & 0xffff // 低16位：数据序号

		// 验证数据顺序：每个流的数据必须按序接收
		if receiveList[streamID] >= dataSeq {
			t.Fatal("out of order") // 顺序错误，接收的序号小于已接收的序号
		}
		receiveList[streamID] = dataSeq // 更新流的接收进度
	}

	// 验证多流读取器正确结束，返回EOF
	_, err := mst.recv()
	if err != io.EOF {
		t.Fatal("end stream haven't return EOF")
	}
}

// TestMergeNamedStreamReaders - 验证命名流读取器的合并功能，重点测试SourceEOF错误处理和源标识机制。
func TestMergeNamedStreamReaders(t *testing.T) {
	t.Run("验证命名流合并时基础源流结束错误处理机制", func(t *testing.T) {
		// 测试验证的核心总结：验证 MergeNamedStreamReaders 在处理命名流合并时的基础 SourceEOF 错误处理机制，
		// 确保系统能够正确识别源流结束信号、提取源流名称、在部分流结束后继续处理其他流数据，
		// 并最终保证数据完整性和源流可追溯性，
		// 体现命名流合并系统在错误处理、数据分类和并发安全方面的可靠性保证。

		// 创建两个命名流的管道（读写端分离）
		sr1, sw1 := Pipe[string](2)
		sr2, sw2 := Pipe[string](2)

		// 定义命名流映射，为每个流分配唯一标识名称
		namedStreams := map[string]*StreamReader[string]{
			"stream1": sr1,
			"stream2": sr2,
		}
		// 合并命名流，创建统一的读取接口
		mergedSR := MergeNamedStreamReaders(namedStreams)
		mergedSR.SetAutomaticClose() // 启用自动关闭机制

		// 第一个流goroutine：发送少量数据后立即关闭
		go func() {
			defer sw1.Close()
			sw1.Send("data1-1", nil)
			sw1.Send("data1-2", nil)
			// stream1 数据发送完毕，流结束
		}()

		// 第二个流goroutine：发送更多数据，模拟异步数据源
		go func() {
			defer sw2.Close()
			sw2.Send("data2-1", nil)
			sw2.Send("data2-2", nil)
			sw2.Send("data2-3", nil)
			// stream2 数据发送完毕，流结束
		}()

		// 初始化数据跟踪：接收数据映射和EOF源流列表
		receivedData := make(map[string][]string) // 按源流分类存储数据
		eofSources := make([]string, 0, 2)        // 记录已结束的源流

		// 主循环：接收合并流的数据和错误信号
		for {
			chunk, err := mergedSR.Recv()
			if err != nil {
				// 关键验证点1：检查是否为SourceEOF错误
				if sourceName, ok := GetSourceName(err); ok {
					eofSources = append(eofSources, sourceName)
					t.Logf("收到源流 %s 的结束信号", sourceName)
					continue // 关键：继续接收其他流的数据
				}

				// 关键验证点2：检查是否为全局EOF（所有流都结束）
				if errors.Is(err, io.EOF) {
					break // 合并流完全结束
				}

				// 处理其他未预期的错误
				t.Errorf("接收数据时发生错误: %v", err)
				break
			}

			// 数据分类：根据前缀将数据归属到对应的源流
			if len(chunk) >= 5 {
				prefix := chunk[:5]
				if prefix == "data1" {
					receivedData["stream1"] = append(receivedData["stream1"], chunk)
				} else if prefix == "data2" {
					receivedData["stream2"] = append(receivedData["stream2"], chunk)
				}
			}
		}

		// 关键验证点3：确保收到两个源流的SourceEOF错误
		if len(eofSources) != 2 {
			t.Errorf("期望收到2个SourceEOF错误，实际收到 %d 个", len(eofSources))
		}

		// 关键验证点4：验证源流名称的正确性
		expectedSources := map[string]bool{"stream1": false, "stream2": false}
		for _, source := range eofSources {
			if _, exists := expectedSources[source]; !exists {
				t.Errorf("收到未预期的源流名称: %s", source)
			} else {
				expectedSources[source] = true
			}
		}

		// 关键验证点5：确保所有预期的源流名称都被记录
		for sourceName, received := range expectedSources {
			if !received {
				t.Errorf("未收到源流 %s 的结束信号", sourceName)
			}
		}

		// 关键验证点6：验证数据完整性和来源正确性
		if len(receivedData["stream1"]) != 2 {
			t.Errorf("期望stream1有2条数据，实际收到 %d 条", len(receivedData["stream1"]))
		}

		if len(receivedData["stream2"]) != 3 {
			t.Errorf("期望stream2有3条数据，实际收到 %d 条", len(receivedData["stream2"]))
		}
	})

	t.Run("验证空流与数据流合并时的源流结束处理机制", func(t *testing.T) {
		// 测试验证的核心总结：验证 MergeNamedStreamReaders 在处理空流与数据流混合场景时的正确性，
		// 确保空流能够正确产生 SourceEOF 信号并提取源流名称，同时不影响其他数据流的数据接收和完整性，
		// 体现命名流合并系统在边界条件处理、错误识别和数据隔离方面的健壮性保证。
		// 这个测试特别针对实际应用中某些数据源可能为空或不提供数据的常见场景。

		// 创建两个流：一个将作为空流，一个包含数据
		sr1, sw1 := Pipe[string](2)
		sr2, sw2 := Pipe[string](2)

		// 立即关闭第一个流，创建空流场景
		sw1.Close() // sr1 现在是空流，没有任何数据

		// 定义命名流映射：包含一个空流和一个数据流
		namedStreams := map[string]*StreamReader[string]{
			"empty": sr1, // 空流，立即结束
			"data":  sr2, // 数据流，包含测试数据
		}
		// 合并命名流，测试空流的处理
		mergedSR := MergeNamedStreamReaders(namedStreams)
		mergedSR.SetAutomaticClose()

		// 向数据流发送测试数据
		go func() {
			defer sw2.Close()
			sw2.Send("test-data", nil) // 发送一条测试数据
		}()

		// 初始化跟踪：EOF源流映射和接收数据列表
		eofSources := make(map[string]bool, 2) // 记录收到EOF的源流
		receivedData := make([]string, 0, 1)   // 记录接收到的数据

		// 主循环：接收合并流的数据和错误信号
		for {
			chunk, err := mergedSR.Recv()
			if err != nil {
				// 关键验证点1：检查是否为SourceEOF错误
				if sourceName, ok := GetSourceName(err); ok {
					eofSources[sourceName] = true
					t.Logf("收到源流 '%s' 的结束信号", sourceName)
					continue // 继续处理其他流
				}

				// 关键验证点2：检查是否为全局EOF
				if errors.Is(err, io.EOF) {
					break // 所有流都结束
				}

				// 处理其他未预期的错误
				t.Errorf("接收数据时发生错误: %v", err)
				break
			}

			// 记录接收到的数据
			receivedData = append(receivedData, chunk)
		}

		// 关键验证点3：确保收到两个源流的SourceEOF错误
		if len(eofSources) != 2 {
			t.Errorf("期望收到2个SourceEOF错误，实际收到 %d 个", len(eofSources))
		}

		// 关键验证点4：验证空流名称的正确识别
		if _, exist := eofSources["empty"]; !exist {
			t.Errorf("期望收到'empty'流的EOF，实际收到: %v", eofSources)
		}
		// 关键验证点5：验证数据流名称的正确识别
		if _, exist := eofSources["data"]; !exist {
			t.Errorf("期望收到'data'流的EOF，实际收到: %v", eofSources)
		}

		// 关键验证点6：验证数据流的正确接收
		if len(receivedData) != 1 || receivedData[0] != "test-data" {
			t.Errorf("期望接收'test-data'，实际收到: %v", receivedData)
		}
	})

	t.Run("验证数组流与普通流混合合并时的类型统一处理机制", func(t *testing.T) {
		// 测试验证的核心总结：验证 MergeNamedStreamReaders 在处理数组流与普通流混合场景时的类型统一处理能力，
		// 确保基于静态数组的流和基于动态channel的流能够无缝合并，通过统一的接口提供一致的行为和错误处理，
		// 体现流处理系统在类型抽象、接口统一和混合场景处理方面的设计优雅性和实用价值。
		// 这个测试特别针对批处理数据与实时数据混合的常见业务场景。

		// 创建三个不同类型的流：两个普通流，一个数组流
		sr1, sw1 := Pipe[string](2) // 普通流1
		sr2, sw2 := Pipe[string](2) // 普通流2
		// 数组流：基于静态数组创建的流读取器
		sr3 := StreamReaderFromArray([]string{"data3-1", "data3-2", "data3-3"})

		// 定义命名流映射：混合不同底层实现的流
		namedStreams := map[string]*StreamReader[string]{
			"stream1": sr1, // 基于channel的动态流
			"stream2": sr2, // 基于channel的动态流
			"stream3": sr3, // 基于数组的静态流
		}
		// 合并不同类型的流，验证统一接口的处理能力
		mergedSR := MergeNamedStreamReaders(namedStreams)
		mergedSR.SetAutomaticClose()

		// 在goroutine中按顺序发送和关闭流
		go func() {
			// 第一个流：发送1个数据后关闭
			sw1.Send("data1", nil)
			sw1.Close()

			// 第二个流：发送2个数据后关闭
			sw2.Send("data2-1", nil)
			sw2.Send("data2-2", nil)
			sw2.Close()
			// 注意：数组流sr3不需要手动发送，数据已预设在数组中
		}()

		// 初始化跟踪：EOF接收顺序和数据计数
		eofOrder := make([]string, 0, 3) // 记录EOF接收的顺序
		dataCount := 0                   // 统计接收到的数据总量

		// 主循环：接收合并流的数据和错误信号
		for {
			_, err := mergedSR.Recv()
			if err != nil {
				// 关键验证点1：检查是否为SourceEOF错误
				if sourceName, ok := GetSourceName(err); ok {
					eofOrder = append(eofOrder, sourceName)
					t.Logf("收到源流 '%s' 的结束信号", sourceName)
					continue // 继续处理其他流
				}

				// 关键验证点2：检查是否为全局EOF
				if errors.Is(err, io.EOF) {
					break // 所有流都结束
				}

				// 处理其他未预期的错误
				t.Errorf("接收数据时发生错误: %v", err)
				break
			}

			// 统计接收到的数据（不关心具体内容，只关心数量）
			dataCount++
		}

		// 关键验证点3：确保收到所有源流的SourceEOF错误
		if len(eofOrder) != 3 {
			t.Errorf("期望收到3个SourceEOF错误，实际收到 %d 个", len(eofOrder))
		}

		// 关键验证点4：验证数据总数正确性
		// stream1: 1个数据 + stream2: 2个数据 + stream3: 3个数据 = 6个数据
		if dataCount != 6 {
			t.Errorf("期望收到6个数据项，实际收到 %d 个", dataCount)
		}
	})

	t.Run("验证命名流合并时的错误传播和识别机制", func(t *testing.T) {
		// 测试验证的核心总结： 验证 MergeNamedStreamReaders 在处理错误传播时的正确性和可靠性，
		// 确保源流的业务错误能够准确传播到合并流并被正确识别和处理，
		// 同时系统能够区分业务错误和系统信号（SourceEOF、io.EOF），实现精准的错误处理和容错机制，
		// 体现命名流合并系统在错误管理、故障隔离和容错处理方面的强大能力。
		// 这个测试特别针对多数据源环境下的错误处理和系统稳定性保证。

		// 创建两个流：一个正常流，一个错误流
		sr1, sw1 := Pipe[string](2) // 正常数据流
		sr2, sw2 := Pipe[string](2) // 错误数据流

		// 定义命名流映射：模拟正常和异常数据源
		namedStreams := map[string]*StreamReader[string]{
			"normal": sr1, // 正常工作的数据源
			"error":  sr2, // 会产生错误的数据源
		}
		// 合并命名流，测试错误的传播机制
		mergedSR := MergeNamedStreamReaders(namedStreams)
		defer mergedSR.Close() // 确保资源清理

		// 创建测试错误
		testError := errors.New("test error")

		// 向正常流发送数据
		go func() {
			defer sw1.Close()
			sw1.Send("normal-data", nil) // 发送正常数据，无错误
		}()

		// 向错误流发送错误
		go func() {
			defer sw2.Close()
			sw2.Send("", testError) // 发送空数据和错误
		}()

		// 跟踪接收到的业务错误（排除SourceEOF）
		var receivedError error

		// 主循环：接收合并流的数据和错误
		for {
			_, err := mergedSR.Recv()
			if err != nil {
				// 关键验证点1：跳过SourceEOF错误，专注于业务错误
				if _, ok := GetSourceName(err); ok {
					t.Logf("收到源流结束信号，继续处理")
					continue // 跳过SourceEOF，继续处理
				}

				// 关键验证点2：检查是否为全局EOF
				if errors.Is(err, io.EOF) {
					break // 所有流都正常结束
				}

				// 关键验证点3：捕获第一个非EOF、非SourceEOF的业务错误
				receivedError = err
				t.Logf("收到业务错误: %v", err)
				break // 收到业务错误后结束处理
			}
		}

		// 关键验证点4：验证错误内容和传播正确性
		if receivedError == nil || receivedError.Error() != testError.Error() {
			t.Errorf("期望收到错误 '%v'，实际收到 '%v'", testError, receivedError)
		}
	})
}
