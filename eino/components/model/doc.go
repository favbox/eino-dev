// Package model 提供了聊天组件的核心接口和类型定义。
//
// 模型组件是 Eino 框架的核心组件之一，用于处理自然语言对话和生成响应。
//
// 主要接口：
//
//   - BaseChatModel：基础聊天模型接口，定义了 Generate 和 Stream 两个核心方法
//   - ToolCallingChatModel：工具调用模型接口，扩展了基础模型并添加了工具调用能力
//
// 设计特点：
//
//   - 支持同步和流式两种生成模式
//   - 提供不可变的工具绑定方式（ToolCallingChatModel）
//   - 兼容 OpenAI 格式的消息结构
//   - 支持自定义模型选项配置
//
// 使用示例：
//
//   - 创建 ChatModel 实例
//   - 配置模型选项（如温度、最大令牌数等）
//   - 准备输入消息列表
//   - 调用 Generate 或 Stream 方法生成响应
//
// 推荐使用 ToolCallingChatModel 接口，因为它提供了更安全的工具绑定机制。
package model
