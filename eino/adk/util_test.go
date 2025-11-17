package adk

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewAsyncIteratorPair_Basic(t *testing.T) {
	// 创建一个新的迭代器-生成器对
	iterator, generator := NewAsyncIteratorPair[string]()

	// 测试发送和接收一个值
	generator.Send("test1")
	val, ok := iterator.Next()
	assert.True(t, ok)
	assert.Equal(t, "test1", val)

	// 测试发送和接收多个值
	generator.Send("test2")
	generator.Send("test3")

	val, ok = iterator.Next()
	assert.True(t, ok)
	assert.Equal(t, "test2", val)

	val, ok = iterator.Next()
	assert.True(t, ok)
	assert.Equal(t, "test3", val)
}

func TestNewAsyncIteratorPair_Close(t *testing.T) {
	iterator, generator := NewAsyncIteratorPair[int]()

	// 发送一些值
	generator.Send(1)
	generator.Send(2)

	// 关闭生成器
	generator.Close()

	// 应当还能读已有值
	val, ok := iterator.Next()
	assert.True(t, ok)
	assert.Equal(t, 1, val)

	val, ok = iterator.Next()
	assert.True(t, ok)
	assert.Equal(t, 2, val)

	// 消费完所有值后，Next 应该返回 false
	_, ok = iterator.Next()
	assert.False(t, ok)
}

func TestNewAsyncIteratorPair_Concurrency(t *testing.T) {
	iterator, generator := NewAsyncIteratorPair[int]()
	const (
		numSenders         = 5
		numReceivers       = 3
		messagesPerSenders = 100
	)

	var rwg, swg sync.WaitGroup
	rwg.Add(numReceivers)
	swg.Add(numSenders)

	// 开始发送
	for i := 0; i < numSenders; i++ {
		go func(id int) {
			defer swg.Done()
			for j := 0; j < messagesPerSenders; j++ {
				generator.Send(id*messagesPerSenders + j)
				time.Sleep(time.Microsecond) // 小延迟以增加并发机会
			}
		}(i)
	}

	// 开始接收
	received := make([]int, 0, numSenders*messagesPerSenders)
	var mu sync.Mutex

	for i := 0; i < numReceivers; i++ {
		go func() {
			defer rwg.Done()
			for {
				val, ok := iterator.Next()
				if !ok {
					return
				}
				mu.Lock()
				received = append(received, val)
				mu.Unlock()
			}
		}()
	}

	// 等待发送完成
	swg.Wait()
	generator.Close()

	// 等待接收完成
	rwg.Wait()

	// 验证我们收到了所有消息
	assert.Equal(t, numSenders*messagesPerSenders, len(received))

	// 创建一个映射来检查重复和缺失的值
	receivedMap := make(map[int]bool)
	for _, val := range received {
		receivedMap[val] = true
	}
	assert.Equal(t, numSenders*messagesPerSenders, len(receivedMap))
}

func TestGenErrorIter(t *testing.T) {
	iter := genErrorIter(fmt.Errorf("test"))
	e, ok := iter.Next()
	assert.True(t, ok)
	assert.Equal(t, "test", e.Err.Error())
	_, ok = iter.Next()
	assert.False(t, ok)
}
