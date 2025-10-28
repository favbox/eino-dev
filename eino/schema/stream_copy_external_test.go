package schema

import (
	"fmt"
	"io"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestStream1 - 验证流复制的时间同步特性，测试多并发接收的数据完整性和时间间隔稳定性。
func TestStream1(t *testing.T) {
	// 测试验证的核心总结：验证 Copy() 方法在时间敏感场景下的性能表现，
	// 确保复制后的多个子流能够保持数据的完整性和时间的一致性，
	// 同时评估系统调度对流接收行为的影响，
	// 体现流复制系统在实时数据处理场景下的时间同步能力和并发安全性保证。

	// 关键设计意图：
	//
	// 验证流复制系统在不同时间间隔下的表现
	// 1. 正常情况：接收间隔应接近发送间隔（3ms）
	// 2. 异常情况：系统调度可能导致接收间隔波动
	// 3. 系统稳定性：count值反映系统的调度稳定性

	// 测试场景设计：
	//
	//  发送端：  0ms → 3ms → 6ms → 9ms → ... (固定间隔)
	//          ↓   ↓   ↓   ↓
	//  接收端1：?ms → ?ms → ?ms → ?ms (变量间隔)
	//  接收端2：?ms → ?ms → ?ms → ?ms (变量间隔)

	// 设置单核运行环境，避免并发调度干扰
	runtime.GOMAXPROCS(1)

	// 创建无缓冲的整数管道，模拟实时数据流
	sr, sw := Pipe[int](0)

	// 发送端goroutine：发送100个数据，间隔3ms
	go func() {
		for i := 0; i < 100; i++ {
			sw.Send(i, nil)                  // 发送数据
			time.Sleep(3 * time.Millisecond) // 3ms间隔
		}
		sw.Close() // 关闭发送端
	}()

	// 创建流复制：原始流复制为2个子流
	copied := sr.Copy(2)

	// 初始化时间戳和计数器
	var (
		now   = time.Now().UnixMilli() // 当前时间戳
		ts    = []int64{now, now}      // 两个接收流的时间戳数组
		tsOld = []int64{now, now}      // 两个接收流的上次时间戳
	)
	var count int32 // 时间间隔异常计数器

	// 并发控制：等待两个接收goroutine完成
	wg := sync.WaitGroup{}
	wg.Add(2)

	// 第一个接收goroutine：监测时间间隔并验证数据完整性
	go func() {
		i := 0
		s := copied[0] // 获取第一个子流
		for {
			n, e := s.Recv() // 接收数据
			if e != nil {
				if e == io.EOF {
					break // 流正常结束
				}
			}

			// 更新时间戳并计算接收间隔
			tsOld[0] = ts[0]
			ts[0] = time.Now().UnixMilli()
			interval := ts[0] - tsOld[0]

			// 如果间隔>=6ms（超过发送间隔2倍），视为异常
			if interval >= 6 {
				atomic.AddInt32(&count, 1) // 原子操作增加计数
			}

			assert.Equal(t, i, n) // 验证数据顺序正确性
			i++
		}
		wg.Done()
	}()

	// 第二个接收goroutine：同样的逻辑，监测第二个子流
	go func() {
		i := 0
		s := copied[1] // 获取第二个子流
		for {
			n, e := s.Recv() // 接收数据
			if e != nil {
				if e == io.EOF {
					break // 流正常结束
				}
			}

			// 更新时间戳并计算接收间隔
			tsOld[1] = ts[1]
			ts[1] = time.Now().UnixMilli()
			interval := ts[1] - tsOld[1]

			// 如果间隔>=6ms，视为异常
			if interval >= 6 {
				atomic.AddInt32(&count, 1) // 原子操作增加计数
			}

			assert.Equal(t, i, n) // 验证数据顺序正确性
			i++
		}
		wg.Done()
	}()

	wg.Wait()                  // 等待两个goroutine完成
	t.Logf("count= %d", count) // 输出时间异常次数
}

type info struct {
	idx     int
	ts      int64
	after   int64
	content string
}

// TestCopyDelay - 验证流复制在延迟场景下的时间特性和数据同步一致性，测试并发接收的时序分析。
func TestCopyDelay(t *testing.T) {
	// 测试验证的核心总结：验证 Copy() 方法在存在发送延迟的场景下的时间同步性能，
	// 确保复制后的多个子流能够一致地处理延迟数据，通过微秒级时间精度分析并发接收的时序特征，
	// 评估流复制系统在网络延迟、系统调度等真实环境下的稳定性和一致性表现。

	// 设置多核环境以测试并发调度
	runtime.GOMAXPROCS(10)

	n := 3 // 复制的子流数量

	// 创建无缓冲的字符串流
	s := newStream[string](0)

	// 创建流复制：原始流复制为3个子流
	scp := s.asReader().Copy(n)

	// 发送端goroutine：模拟带延迟的数据发送
	go func() {
		s.send("1", nil)        // 立即发送第一个数据
		s.send("2", nil)        // 立即发送第二个数据
		time.Sleep(time.Second) // 1秒延迟
		s.send("3", nil)        // 发送第三个数据
		s.closeSend()           // 关闭发送端
	}()

	// 并发控制：等待所有接收goroutine完成
	wg := sync.WaitGroup{}
	wg.Add(n)

	// 为每个子流创建独立的接收记录
	infoList := make([][]info, n)

	// 创建n个并发接收goroutine
	for i := 0; i < n; i++ {
		j := i // 闭包变量捕获
		go func() {
			defer func() {
				scp[j].Close() // 关闭对应的子流
				wg.Done()      // 标记完成
			}()

			// 循环接收子流数据
			for {
				lastTime := time.Now()    // 记录接收开始时间
				str, err := scp[j].Recv() // 接收数据
				if err == io.EOF {
					break // 流正常结束
				}
				now := time.Now() // 记录接收完成时间

				// 记录接收事件信息
				infoList[j] = append(infoList[j], info{
					idx:     j,                                // 子流索引
					ts:      now.UnixMicro(),                  // 精确到微秒的时间戳
					after:   now.Sub(lastTime).Milliseconds(), // 接收耗时
					content: str,                              // 接收的内容
				})
			}
		}()
	}

	wg.Wait() // 等待所有goroutine完成

	// 将所有子流的接收信息合并到一个切片中
	infos := make([]info, 0)
	for _, infoL := range infoList {
		infos = append(infos, infoL...) // 展开所有信息
	}

	// 按时间戳排序，便于分析时序关系
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ts < infos[j].ts
	})

	// 输出分析结果
	for _, info := range infos {
		fmt.Printf("child[%d] ts[%d] after[%5dms] content[%s]\n",
			info.idx, info.ts, info.after, info.content)
	}
}
