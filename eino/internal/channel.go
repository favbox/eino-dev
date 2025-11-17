/*
 * channel.go - 无界容量 channel 实现
 *
 * 核心组件：
 *   - UnboundedChan: 无界容量的泛型 channel，使用切片作为内部缓冲区
 *
 * 设计特点：
 *   - 线程安全: 使用互斥锁和条件变量保护并发访问
 *   - 无容量限制: 发送操作永不阻塞（除非已关闭）
 *   - 阻塞接收: 当缓冲区为空时接收操作会阻塞等待
 *   - 关闭通知: 支持关闭通知，会唤醒所有等待的接收者
 */

package internal

import "sync"

// ====== 核心数据结构 ======

// UnboundedChan 实现无界容量的 channel。
// 与标准 channel 不同，发送操作永不阻塞，数据会缓存在内部切片中。
// 接收操作在缓冲区为空时会阻塞，直到有数据可用或 channel 被关闭。
type UnboundedChan[T any] struct {
	buffer   []T        // 内部缓冲区，存储待接收的数据
	mutex    sync.Mutex // 保护并发访问的互斥锁
	notEmpty *sync.Cond // 用于等待数据可用的条件变量
	closed   bool       // 标记 channel 是否已关闭
}

// NewUnboundedChan 创建并返回一个新的无界 channel
func NewUnboundedChan[T any]() *UnboundedChan[T] {
	ch := &UnboundedChan[T]{}
	ch.notEmpty = sync.NewCond(&ch.mutex)
	return ch
}

// Send 将数据发送到 channel。
// 如果 channel 已关闭，会触发 panic，行为与标准 channel 一致。
func (ch *UnboundedChan[T]) Send(value T) {
	ch.mutex.Lock()
	defer ch.mutex.Unlock()

	if ch.closed {
		panic("send on closed channel")
	}

	ch.buffer = append(ch.buffer, value)
	ch.notEmpty.Signal() // 唤醒一个等待接收的 goroutine
}

// Receive 从 channel 接收数据。
// 当缓冲区为空时会阻塞等待，直到有数据可用或 channel 被关闭。
// 返回值的第二个参数指示 channel 是否已关闭且为空，false 表示已关闭。
func (ch *UnboundedChan[T]) Receive() (T, bool) {
	ch.mutex.Lock()
	defer ch.mutex.Unlock()

	for len(ch.buffer) == 0 && !ch.closed {
		ch.notEmpty.Wait() // 等待数据可用
	}

	if len(ch.buffer) == 0 {
		// channel 已关闭且缓冲区为空
		var zero T
		return zero, false
	}

	val := ch.buffer[0]
	ch.buffer = ch.buffer[1:]
	return val, true
}

// Close 关闭 channel，会唤醒所有等待接收的 goroutine。
// 重复关闭是安全的，不会触发 panic。
func (ch *UnboundedChan[T]) Close() {
	ch.mutex.Lock()
	defer ch.mutex.Unlock()

	if !ch.closed {
		ch.closed = true
		ch.notEmpty.Broadcast() // 唤醒所有等待的 goroutine
	}
}
