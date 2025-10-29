// Package callbacks 为 Eino 框架中的组件执行提供回调机制。
//
// 本包允许开发者在组件执行的不同阶段注入回调处理器，实现横切关注点的处理。
// 适用于日志记录、监控指标收集、性能分析、错误处理和调试跟踪等场景。
//
// 回调时机：
//   - OnStart：组件开始执行
//   - OnEnd：组件正常结束
//   - OnError：组件执行出错
//   - OnStartWithStreamInput：流式输入开始
//   - OnEndWithStreamOutput：流式输出结束
//
// 三种使用方式：
//
//  1. HandlerBuilder：通用回调处理器构建器，支持函数式编程风格
//  2. HandlerHelper：通过 utils/callbacks 包为不同组件类型创建专用处理器
//  3. 面向切面编程：直接在组件实现中调用注入函数（OnStart、OnEnd、OnError 等）
//
// 支持全局回调和用户特定回调的灵活组合，提供类型安全的泛型设计
// 和完整的流式处理支持。
//
// 详细使用指南和示例请参考项目文档。
package callbacks
