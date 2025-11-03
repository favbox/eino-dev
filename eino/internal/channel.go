package internal

import "sync"

/*
 * channel.go - 无界通道实现
 *
 * 核心组件：
 *   - UnboundedChan: 无界通道实现，支持无限容量的数据缓冲
 *
 * 设计特点：
 *   - 无界缓冲：使用动态切片作为缓冲区，无容量限制
 *   - 线程安全：通过互斥锁和条件变量保证并发安全
 *   - 阻塞语义：Receive 操作在空缓冲时阻塞，直到有数据或通道关闭
 *   - 优雅关闭：支持 Close 操作，通知所有等待的 goroutine
 *
 * 与其他包关系：
 *   - 被 compose/stream_reader.go 等流处理组件使用
 *   - 提供底层的数据传输和同步机制
 *
 * 使用场景：
 *   - 流式数据处理：需要缓存大量流数据
 *   - 生产者-消费者：解耦生产者和消费者的处理速度
 *   - 背压控制：在流式处理中实现优雅的背压机制
 *   - 异步通信：支持 goroutine 之间的异步数据传输
 *
 * 设计优势：
 *   - 无容量限制：不会因为缓冲区满而阻塞发送者
 *   - 零拷贝读取：通过移动切片起始位置实现高效读取
 *   - 条件变量：使用 sync.Cond 实现高效的等待/通知机制
 *   - 错误处理：通过返回值和 panic 明确错误语义
 *
 * 注意事项：
 *   - 内存管理：持续增长的缓冲区可能消耗大量内存
 *   - 关闭语义：关闭后不能继续发送，否则 panic
 *   - 读取语义：关闭后读取剩余数据，返回 (zero value, false)
 *
 * 实现原理：
 *   - 使用动态切片作为环形缓冲区（实际上是动态数组）
 *   - 每次读取后不立即释放内存，而是移动起始索引
 *   - 通过条件变量在空缓冲区时阻塞接收者
 *   - 通过互斥锁保护所有共享状态的访问
 */

// ====== 核心数据结构 ======

// UnboundedChan 无界通道 - 支持无限容量的线程安全通道
// 这是一个泛型实现的阻塞式无界通道，适用于流式数据处理场景
// 发送操作永远不会阻塞，接收操作在没有数据时会阻塞
type UnboundedChan[T any] struct {
	// 缓冲区：存储待接收数据的动态数组
	// 使用切片实现，支持无限增长，不会因为容量满而阻塞发送者
	buffer []T

	// 互斥锁：保护所有共享状态的访问
	// 确保并发安全，防止数据竞争
	mutex sync.Mutex

	// 条件变量：用于在缓冲区为空时阻塞接收者
	// 通过 Signal/Broadcast 实现高效的等待/通知机制
	notEmpty *sync.Cond

	// 关闭标志：标识通道是否已关闭
	// 用于优雅的关闭语义
	closed bool
}

// ====== 构造函数 ======

// NewUnboundedChan 创建无界通道实例
// 初始化一个空的无界通道，准备接收和发送数据
// 返回值：配置好的 UnboundedChan 实例，可立即使用
func NewUnboundedChan[T any]() *UnboundedChan[T] {
	ch := &UnboundedChan[T]{}
	// 初始化条件变量，与互斥锁关联
	ch.notEmpty = sync.NewCond(&ch.mutex)
	return ch
}

// ====== 发送操作 ======

// Send 发送数据到通道
// 将数据追加到缓冲区末尾，唤醒一个等待的接收者
// 如果通道已关闭，调用此方法会 panic
//
// 参数：
//   - value: 要发送的数据，类型为 T
//
// 行为：
//   - 非阻塞操作，即使缓冲区很大也立即返回
//   - 线程安全，支持多个 goroutine 并发发送
//   - 自动扩展缓冲区容量以容纳新数据
func (ch *UnboundedChan[T]) Send(value T) {
	ch.mutex.Lock()
	defer ch.mutex.Unlock()

	// 检查通道状态：关闭后不能发送
	if ch.closed {
		panic("send on closed channel")
	}

	// 添加数据到缓冲区
	ch.buffer = append(ch.buffer, value)
	// 唤醒一个等待的接收者
	ch.notEmpty.Signal()
}

// ====== 接收操作 ======

// Receive 从通道接收数据
// 从缓冲区头部取出数据，缓冲区为空时阻塞等待
// 当通道关闭且缓冲区为空时，返回 (零值, false)
//
// 返回值：
//   - T: 接收到的数据
//   - bool: 是否成功接收到数据，false 表示通道已关闭且无数据
//
// 行为：
//   - 阻塞操作：缓冲区为空时会等待，直到有数据或通道关闭
//   - 线程安全，支持多个 goroutine 并发接收
//   - 零拷贝读取：移动切片起始位置，避免频繁内存分配
func (ch *UnboundedChan[T]) Receive() (T, bool) {
	ch.mutex.Lock()
	defer ch.mutex.Unlock()

	// 等待直到有数据或通道关闭
	// 使用 for 循环防止虚假唤醒
	for len(ch.buffer) == 0 && !ch.closed {
		ch.notEmpty.Wait()
	}

	// 检查是否有数据：没有数据意味着通道已关闭
	if len(ch.buffer) == 0 {
		// 返回类型 T 的零值和 false
		var zero T
		return zero, false
	}

	// 取出缓冲区第一个元素
	val := ch.buffer[0]
	// 通过移动切片起始位置实现零拷贝
	// 避免立即释放内存，提高性能
	ch.buffer = ch.buffer[1:]
	return val, true
}

// ====== 关闭操作 ======

// Close 关闭通道
// 标记通道为已关闭，唤醒所有等待的接收者
// 支持幂等操作：重复关闭不会产生副作用
//
// 行为：
//   - 标记通道为已关闭状态
//   - 唤醒所有因等待数据而阻塞的接收者
//   - 线程安全，可以从任何 goroutine 调用
//   - 幂等性：多次调用是安全的
func (ch *UnboundedChan[T]) Close() {
	ch.mutex.Lock()
	defer ch.mutex.Unlock()

	// 只在未关闭时执行关闭逻辑
	if !ch.closed {
		ch.closed = true
		// 广播给所有等待的接收者，让它们检查通道状态
		ch.notEmpty.Broadcast()
	}
}
