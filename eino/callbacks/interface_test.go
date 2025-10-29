package callbacks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/internal/callbacks"
)

// TestAppendGlobalHandlers 测试 AppendGlobalHandlers 函数的核心功能
//
// 验证的核心功能：
//   - 全局回调处理器的追加功能
//   - 处理器列表的累积添加机制
//   - 空处理器列表的边界情况处理
//
// 测试覆盖的场景：
//  1. 从空状态开始添加处理器
//  2. 累积添加多个处理器
//  3. 添加空处理器列表的边界情况
func TestAppendGlobalHandlers(t *testing.T) {
	// 测试前清理：清空全局处理器列表，确保测试环境干净
	callbacks.GlobalHandlers = nil

	// 创建测试用的回调处理器
	// handler1: 处理组件开始事件的处理器
	handler1 := NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *RunInfo, input CallbackInput) context.Context {
			return ctx
		}).Build()

	// handler2: 处理组件结束事件的处理器
	handler2 := NewHandlerBuilder().
		OnEndFn(func(ctx context.Context, info *RunInfo, output CallbackOutput) context.Context {
			return ctx
		}).Build()

	// 场景1：测试添加第一个处理器
	AppendGlobalHandlers(handler1)

	// 验证点1：全局处理器列表长度应为1
	assert.Equal(t, 1, len(callbacks.GlobalHandlers))
	// 验证点2：确认添加的处理器在列表中
	assert.Contains(t, callbacks.GlobalHandlers, handler1)

	// 场景2：测试累积添加第二个处理器
	AppendGlobalHandlers(handler2)

	// 验证点3：全局处理器列表长度应为2
	assert.Equal(t, 2, len(callbacks.GlobalHandlers))
	// 验证点4：确认第一个处理器仍然存在（累积添加）
	assert.Contains(t, callbacks.GlobalHandlers, handler1)
	// 验证点5：确认第二个处理器被正确添加
	assert.Contains(t, callbacks.GlobalHandlers, handler2)

	// 场景3：测试边界情况 - 添加空处理器列表
	AppendGlobalHandlers([]Handler{}...)

	// 验证点6：空列表不应影响现有处理器，长度应保持为2
	assert.Equal(t, 2, len(callbacks.GlobalHandlers))
}
