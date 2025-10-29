// Package callbacks 提供组件执行过程中的回调机制。
//
// 该包允许你在组件执行的不同阶段（如开始、结束、出错）注入回调函数，
// 用于实现日志、监控、指标采集等治理功能。
//
// 核心问题：
//
//	传统组件开发中面临横切关注点处理的困难：
//	- 监控逻辑与业务逻辑耦合，污染组件代码
//	- 缺乏统一的组件生命周期管理机制
//	- 流式组件的监控和日志处理复杂
//	- 不同组件类型的回调处理难以统一
//	- 调试和追踪信息分散，难以集中管理
//
// 解决方案：
//
//	通过定义回调接口与统一的 Handler 构建器，
//	开发者可在组件执行前后插入治理逻辑，
//	实现业务与监控的解耦、行为可观测化。
//
// 用法：
//
//	// 使用 HandlerBuilder 创建回调处理器
//	handler := callbacks.NewHandlerBuilder().
//		OnStart(func(ctx context.Context, info *RunInfo, input CallbackInput) context.Context {
//			// 组件开始执行时处理
//			return ctx
//		}).
//		OnEnd(func(ctx context.Context, info *RunInfo, output CallbackOutput) context.Context {
//			// 组件执行结束时处理
//			return ctx
//		}).
//		OnError(func(ctx context.Context, info *RunInfo, err error) context.Context {
//			// 执行出错时处理
//			return ctx
//		}).
//		Build()
//
//	// 使用 HandlerHelper 快速为不同组件类型构建回调处理器
//	handler := callbacks.NewHandlerHelper().
//		ChatModel(modelHandler).
//		Prompt(promptHandler).
//		Fallback(fallbackHandler).
//		Handler()
//
// [HandlerHelper] 支持以下组件类型：
//   - Prompt 组件（prompt.CallbackHandler）
//   - ChatModel 组件（model.CallbackHandler）
//   - Embedding 组件（embedding.CallbackHandler）
//   - Indexer 组件（indexer.CallbackHandler）
//   - Retriever 组件（retriever.CallbackHandler）
//   - Loader 组件（loader.CallbackHandler）
//   - Transformer 组件（transformer.CallbackHandler）
//   - Tool 组件（tool.CallbackHandler）
//   - Graph、Chain、Lambda 等复合组件
//
// 使用示例：
//
//	runnable.Invoke(ctx, input, compose.WithCallbacks(handler))
package callbacks
