package internal

/*
 * channel_test.go - 无界通道单元测试
 *
 * 测试覆盖范围：
 *   1. 基本操作测试：Send、Receive、Close
 *   2. 错误处理测试：向已关闭通道发送的 panic 行为
 *   3. 关闭语义测试：从已关闭通道接收的正确语义
 *   4. 并发安全测试：多生产者多消费者的并发场景
 *   5. 阻塞语义测试：空通道接收的阻塞行为
 *
 * 测试原则：
 *   - 每个测试独立运行，不依赖其他测试
 *   - 覆盖正常路径和异常路径
 *   - 验证关键行为和边界条件
 *   - 确保并发安全性
 *
 * 性能考量：
 *   - 并发测试使用适度的消息数量，避免测试时间过长
 *   - 使用时间控制确保测试及时完成
 *   - 验证零拷贝读取等性能关键路径
 */

import (
	"sync"
	"testing"
	"time"
)

// ====== 基础功能测试 ======

// TestUnboundedChan_Send 测试无界通道的发送功能
// 验证单值和多值发送的正确性，确保缓冲区正确存储数据
func TestUnboundedChan_Send(t *testing.T) {
	ch := NewUnboundedChan[string]()

	// 测试1：发送单个值
	// 验证点：缓冲区长度正确、数据值正确
	ch.Send("test")
	if len(ch.buffer) != 1 {
		t.Errorf("缓冲区长度应为 1，但实际为 %d", len(ch.buffer))
	}
	if ch.buffer[0] != "test" {
		t.Errorf("期望值为 'test'，实际为 '%s'", ch.buffer[0])
	}

	// 测试2：发送多个值
	// 验证点：动态扩展缓冲区、数据追加到末尾
	ch.Send("test2")
	ch.Send("test3")
	if len(ch.buffer) != 3 {
		t.Errorf("缓冲区长度应为 3，但实际为 %d", len(ch.buffer))
	}
}

// TestUnboundedChan_SendPanic 测试向已关闭通道发送的 panic 行为
// 验证错误处理语义：向已关闭通道发送数据会 panic
func TestUnboundedChan_SendPanic(t *testing.T) {
	ch := NewUnboundedChan[int]()
	ch.Close()

	// 验证点：向已关闭通道发送应该 panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("向已关闭通道发送应该触发 panic")
		}
	}()

	ch.Send(1)
}

// TestUnboundedChan_Receive 测试无界通道的接收功能
// 验证数据按照发送顺序正确接收，实现 FIFO（先进先出）语义
func TestUnboundedChan_Receive(t *testing.T) {
	ch := NewUnboundedChan[int]()

	// 准备测试数据
	ch.Send(1)
	ch.Send(2)

	// 测试1：接收第一个值
	// 验证点：接收成功（ok=true）、值正确（val=1）、FIFO 顺序
	val, ok := ch.Receive()
	if !ok {
		t.Error("接收应该成功")
	}
	if val != 1 {
		t.Errorf("期望值为 1，但实际为 %d", val)
	}

	// 测试2：接收第二个值
	// 验证点：继续接收成功、值正确（val=2）
	val, ok = ch.Receive()
	if !ok {
		t.Error("接收应该成功")
	}
	if val != 2 {
		t.Errorf("期望值为 2，但实际为 %d", val)
	}
}

// TestUnboundedChan_ReceiveFromClosed 测试从已关闭通道接收的语义
// 验证三种情况：空通道关闭、有数据通道关闭、消费完数据后的行为
func TestUnboundedChan_ReceiveFromClosed(t *testing.T) {
	// 测试1：空通道关闭
	// 验证点：返回 (零值, false)，表示通道已关闭且无数据
	ch := NewUnboundedChan[int]()
	ch.Close()
	val, ok := ch.Receive()
	if ok {
		t.Error("从已关闭的空通道接收应返回 ok=false")
	}
	if val != 0 {
		t.Errorf("期望零值 0，但实际为 %d", val)
	}

	// 测试2：有数据的通道关闭
	// 验证点：关闭后仍能接收剩余数据，返回 (数据, true)
	ch = NewUnboundedChan[int]()
	ch.Send(42)
	ch.Close()

	val, ok = ch.Receive()
	if !ok {
		t.Error("接收应成功")
	}
	if val != 42 {
		t.Errorf("期望值 42，但实际为 %d", val)
	}

	// 测试3：消费完所有数据后再次接收
	// 验证点：返回 (零值, false)，表示通道已关闭且无数据
	val, ok = ch.Receive()
	if ok {
		t.Error("从已关闭的空通道接收应返回 ok=false")
	}
}

// TestUnboundedChan_Close 测试通道关闭功能
// 验证关闭标志的正确设置和幂等性（重复关闭不 panic）
func TestUnboundedChan_Close(t *testing.T) {
	ch := NewUnboundedChan[int]()

	// 测试1：正常关闭
	// 验证点：closed 标志被正确设置为 true
	ch.Close()
	if !ch.closed {
		t.Error("通道应标记为已关闭")
	}

	// 测试2：重复关闭
	// 验证点：关闭操作是幂等的，不会 panic
	ch.Close()
}

// ====== 并发安全测试 ======

// TestUnboundedChan_Concurrency 测试无界通道的并发安全性
// 使用 5 个生产者、3 个消费者、每生产者 100 条消息的并发场景
// 验证：无数据丢失、无重复数据、线程安全
func TestUnboundedChan_Concurrency(t *testing.T) {
	ch := NewUnboundedChan[int]()
	const numSenders = 5
	const numReceivers = 3
	const messagesPerSender = 100

	var rwg, swg sync.WaitGroup
	rwg.Add(numReceivers)
	swg.Add(numSenders)

	// 启动生产者 goroutine
	for i := 0; i < numSenders; i++ {
		go func(id int) {
			defer swg.Done()
			for j := 0; j < messagesPerSender; j++ {
				// 发送唯一的消息（避免冲突）
				ch.Send(id*messagesPerSender + j)
				time.Sleep(time.Microsecond) // 模拟并发延迟
			}
		}(i)
	}

	// 启动消费者 goroutine
	received := make([]int, 0, numSenders*messagesPerSender)
	var mu sync.Mutex

	for i := 0; i < numReceivers; i++ {
		go func() {
			defer rwg.Done()
			for {
				val, ok := ch.Receive()
				if !ok {
					// 通道已关闭且无数据
					return
				}
				// 线程安全地记录接收到的数据
				mu.Lock()
				received = append(received, val)
				mu.Unlock()
			}
		}()
	}

	// 等待所有生产者完成
	swg.Wait()
	ch.Close()

	// 等待所有消费者完成
	rwg.Wait()

	// 验证1：检查消息总数
	// 验证点：接收的消息数量等于发送的消息数量
	if len(received) != numSenders*messagesPerSender {
		t.Errorf("期望接收 %d 条消息，但实际接收 %d 条", numSenders*messagesPerSender, len(received))
	}

	// 验证2：检查重复和缺失
	// 验证点：使用 Map 检测是否有重复消息或缺失消息
	receivedMap := make(map[int]bool)
	for _, val := range received {
		receivedMap[val] = true
	}

	if len(receivedMap) != numSenders*messagesPerSender {
		t.Error("检测到重复或缺失的消息")
	}
}

// TestUnboundedChan_BlockingReceive 测试无界通道的阻塞语义
// 验证：在空通道上接收会阻塞，发送数据后自动唤醒
func TestUnboundedChan_BlockingReceive(t *testing.T) {
	ch := NewUnboundedChan[int]()

	// 测试1：空通道接收阻塞
	// 验证点：在空通道上接收应该阻塞，不应立即完成
	receiveDone := make(chan bool)
	go func() {
		ch.Receive()
		receiveDone <- true
	}()

	// 使用 select 检查是否阻塞
	select {
	case <-receiveDone:
		t.Error("在空通道上接收应该阻塞")
	case <-time.After(50 * time.Millisecond):
		// 预期：接收操作阻塞，50ms 内未完成
	}

	// 测试2：发送数据唤醒接收
	// 验证点：发送数据后，阻塞的接收操作应立即完成
	ch.Send(1)

	// 检查接收是否完成
	select {
	case <-receiveDone:
		// 预期：接收操作完成
	case <-time.After(50 * time.Millisecond):
		t.Error("接收操作应该被唤醒")
	}
}
