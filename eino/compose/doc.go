/*
 * compose 包 - 智能体编排与图计算框架
 *
 * 概述：
 *   Eino 框架的核心编排包，提供强大的图计算和智能体编排能力。
 *   支持构建复杂的工作流，将各种组件（模型、工具、检索器等）灵活组合。
 *
 * 核心架构：
 *   compose 包采用"组件 + 编排"的分离设计：
 *     - components/ 目录：定义各种 AI 组件的接口和实现
 *     - compose/ 目录：提供编排能力，将组件组合为工作流
 *
 * 三大编排模式：
 *
 *   1. Chain（链式）
 *      - 简单线性编排，按顺序执行各组件
 *      - API：`chain.AppendXX(...).AppendXX(...)`
 *      - 特点：简单直观，适合线性流程
 *      - 示例：Prompt → Model → Tools → Output
 *
 *   2. Graph（图式）
 *      - 复杂有向图，支持分支、循环、并行
 *      - API：基于通用图构建，灵活连接节点
 *      - 特点：功能强大，支持复杂依赖关系
 *      - 两种执行模式：
 *        * Pregel：支持循环，任意前置触发
 *        * DAG：无环图，所有前置完成后触发
 *
 *   3. Workflow（工作流）
 *      - 基于字段映射的声明式编排
 *      - API：节点式声明 + AddInput/AddDependency 依赖管理
 *      - 特点：精细化数据流控制，支持字段级映射
 *      - 示例：精确控制每个字段的来源和去向
 *
 * 四大执行范式：
 *   所有组件和编排都支持四种执行模式，自动适配和转换：
 *
 *   1. Invoke（同步）
 *      输入：普通值 → 输出：普通值
 *      场景：标准同步调用，简单数据处理
 *
 *   2. Stream（流式输出）
 *      输入：普通值 → 输出：流
 *      场景：LLM 流式响应，实时数据生成
 *
 *   3. Collect（流式输入）
 *      输入：流 → 输出：普通值
 *      场景：流式数据聚合，批处理
 *
 *   4. Transform（流式转换）
 *      输入：流 → 输出：流
 *      场景：流式数据处理，实时转换流水线
 *
 * 核心特性：
 *
 *   ✨ 类型安全
 *      - 基于泛型的编译期类型检查
 *      - 避免运行时类型错误
 *      - 编译器为你把关，无需担心类型不匹配
 *
 *   🔄 流式处理
 *      - 完整的流式处理机制
 *      - 自动处理流与非流的转换
 *      - 多流自动合并和拆分
 *
 *   🎯 字段映射
 *      - 精细到字段级别的数据流控制
 *      - 支持嵌套结构和动态映射
 *      - 结构体字段直接映射，无缝数据传递
 *
 *   🔗 依赖管理
 *      - 数据依赖与执行依赖分离
 *      - 支持普通/间接/分支三种依赖类型
 *      - 灵活的执行顺序控制
 *
 *   🎭 回调机制
 *      - 横切关注点的统一处理
 *      - 支持开始/结束/错误/流输入/流输出五种回调时机
 *      - 自动为组件注入回调能力
 *
 *   💾 状态管理
 *      - 节点间共享和传递状态
 *      - 并发安全的访问控制
 *      - 支持状态持久化和恢复
 *
 *   ⚡ 中断重运行
 *      - 支持在指定节点前后中断执行
 *      - 保存执行状态，支持从断点恢复
 *      - 人工审核、错误恢复场景的利器
 *
 *   📍 检查点
 *      - 完整的状态持久化机制
 *      - 支持断点续传和错误恢复
 *      - 长图执行的可靠保障
 *
 *   🔀 分支控制
 *      - 条件分支和多分支支持
 *      - 流式分支动态路径选择
 *      - 灵活的路径控制机制
 *
 * 常用组件：
 *   - 模型组件：ChatModel、EmbeddingModel
 *   - 工具组件：Retriever、Indexer、ToolsNode
 *   - 提示组件：Prompt、ChatTemplate
 *   - 文档组件：Loader、Transformer
 *   - 自定义组件：Lambda、AnyGraph
 *
 * 快速开始：
 *
 *   1. Chain 示例：
 *
 *      chain := compose.NewChain[string, string]()
 *      chain.
 *        AppendChatTemplate(prompt).
 *        AppendChatModel(model).
 *        AppendToolsNode(tools)
 *
 *      r, _ := chain.Compile(ctx)
 *      out, _ := r.Invoke(ctx, "hello")
 *
 *   2. Graph 示例：
 *
 *      g := compose.NewGraph[string, string]()
 *      g.AddChatTemplate("prompt", prompt)
 *      g.AddChatModel("model", model)
 *      g.AddEdge("prompt", "model")
 *
 *      r, _ := g.Compile(ctx)
 *      out, _ := r.Invoke(ctx, "hello")
 *
 *   3. Workflow 示例：
 *
 *      wf := compose.NewWorkflow[Input, Output]()
 *      node := wf.AddChatModel("model", model)
 *      node.AddInput("start", compose.ToField("question"))
 *
 *      r, _ := wf.Compile(ctx)
 *      out, _ := r.Invoke(ctx, input)
 *
 * 设计理念：
 *   compose 包的设计遵循以下原则：
 *
 *   1. 组合优于继承
 *      - 通过组合各种组件构建复杂工作流
 *      - 而不是继承单个庞大类
 *
 *   2. 接口优于抽象类
 *      - 定义简洁明确的接口
 *      - 组件只需要实现必要的方法
 *
 *   3. 异步优于同步
 *      - 默认支持流式和异步处理
 *      - 同步模式作为特殊流式的降级
 *
 *   4. 类型安全优于运行检查
 *      - 编译期类型检查
 *      - 避免运行时错误
 *
 *   5. 声明式优于命令式
 *      - Workflow 模式的声明式 API
 *      - 描述"做什么"而非"怎么做"
 *
 * 相关包：
 *   - github.com/cloudwego/eino/components: 组件定义和实现
 *   - github.com/cloudwego/eino/schema: 通用数据结构
 *   - github.com/cloudwego/eino/callbacks: 回调处理机制
 *   - github.com/cloudwego/eino/adk: 智能体开发包
 *
 * 最佳实践：
 *   1. 根据场景选择编排模式：
 *      - 简单流程 → Chain
 *      - 复杂依赖 → Graph
 *      - 精细控制 → Workflow
 *
 *   2. 合理使用字段映射：
 *      - Workflow 模式充分利用字段映射
 *      - 避免在 Graph 中使用过多的 map[string]any
 *
 *   3. 善用回调机制：
 *      - 记录执行时间、监控指标
 *      - 在关键节点添加审计日志
 *
 *   4. 注意执行模式：
 *      - 明确节点的执行范式
 *      - 避免不必要的模式转换
 *
 * 参考文档：
 *   - 官方文档：https://www.cloudwego.io/docs/eino/
 *   - GitHub：https://github.com/cloudwego/eino
 *   - 示例：https://github.com/cloudwego/eino-examples
 */

package compose
