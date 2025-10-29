package callbacks

import "context"

// CtxManagerKey 上下文管理器键类型。
type CtxManagerKey struct{}

// CtxRunInfoKey 上下文运行信息键类型。
type CtxRunInfoKey struct{}

// manager - 回调管理器结构体。
// 负责管理组件执行过程中的回调处理器链。
type manager struct {
	// globalHandlers 全局回调处理器集合。
	// 所有组件共享的回调逻辑，按优先级执行
	globalHandlers []Handler

	// handlers 组件专用回调处理器集合
	// 当前组件特有的回调逻辑，在全局处理器后执行
	handlers []Handler

	// runInfo 运行信息
	// 包含组件名称、类型等执行上下文信息
	runInfo *RunInfo
}

// GlobalHandlers 全局回调处理器集合。
// 存储全局共享的回调处理器，在所有组件执行时都会被调用
var GlobalHandlers []Handler

// newManager 创建新的回调管理器实例。
// 合并全局处理器和组件专用处理器，返回管理器是否可用
func newManager(runInfo *RunInfo, handlers ...Handler) (*manager, bool) {
	// 检查是否没有任何处理器需要管理
	if len(handlers)+len(GlobalHandlers) == 0 {
		return nil, false
	}

	// 复制全局处理器以避免修改原始数据
	hs := make([]Handler, len(GlobalHandlers))
	copy(hs, GlobalHandlers)

	// 返回初始化的管理器实例
	return &manager{
		globalHandlers: hs,
		handlers:       handlers,
		runInfo:        runInfo,
	}, true
}

// withRunInfo 创建指定运行信息的新管理器副本。
// 保持处理器配置不变，仅更新运行时信息
func (m *manager) withRunInfo(runInfo *RunInfo) *manager {
	// 检查管理器是否为空
	if m == nil {
		return nil
	}

	// 创建管理器的深拷贝，替换运行信息
	n := *m
	n.runInfo = runInfo
	return &n
}

// managerFromCtx 从上下文中提取回调管理器
// 尝试从 context 中获取之前存储的管理器实例
func managerFromCtx(ctx context.Context) (*manager, bool) {
	// 从上下文中获取管理器实例
	v := ctx.Value(CtxManagerKey{})
	m, ok := v.(*manager)

	// 验证管理器存在且不为空
	if ok && m != nil {
		n := *m
		return &n, true
	}

	return nil, false
}

// ctxWithManager 将回调管理器存储到上下文中
// 返回包含管理器的新上下文实例
func ctxWithManager(ctx context.Context, manager *manager) context.Context {
	return context.WithValue(ctx, CtxManagerKey{}, manager)
}
