# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

Eino (发音类似 "I know") 是一个用 Go 语言开发的 LLM 应用开发框架。它提供了丰富的组件抽象、强大的编排框架、简洁的 API 设计，以及完整的流处理和扩展机制。

## 常用命令

### 测试命令
- `go test -race ./...` - 运行所有测试（包括竞态检测）
- `go test -bench=. -benchmem -run=none ./...` - 运行基准测试
- `go test -v -coverprofile=coverage.out ./...` - 运行测试并生成覆盖率报告

### 代码质量检查
- `golangci-lint run` - 运行代码规范检查
- `go mod tidy` - 整理模块依赖

### 构建命令
- `go build ./...` - 构建所有包

## 核心架构

### 组件系统 (components/)
Eino 提供了丰富的组件抽象，每个组件都有统一的接口设计：

- **model/**: 聊天模型组件，包含 BaseChatModel 和 ToolCallingChatModel 接口
- **tool/**: 工具组件，支持函数工具和流式工具
- **retriever/**: 检索器组件，用于信息检索
- **embedding/**: 嵌入模型组件，用于文本向量化
- **prompt/**: 提示词模板组件，支持 ChatTemplate
- **document/**: 文档处理组件，包含加载器和解析器
- **indexer/**: 索引器组件，用于向量存储和检索

### 编排框架 (compose/)
Eino 的核心编排能力，支持三种主要的编排模式：

- **Chain**: 简单的链式有向图，只能向前执行
- **Graph**: 支持循环或非循环的有向图，功能强大且灵活
- **Workflow**: 支持结构体字段级别数据映射的非循环图

### 流处理 (schema/)
完整的流式处理机制：
- 自动连接流块给下游节点
- 自动将非流式数据包装成流
- 自动合并多个流
- 支持四种流式范式：Invoke、Stream、Collect、Transform

**核心流组件**：
- **StreamReader[T]**: 流式读取器，提供 `Recv()` 和 `Close()` 方法
- **StreamWriter[T]**: 流式写入器，提供 `Send()` 和 `Close()` 方法
- **Pipe()**: 创建流管道，返回 StreamReader 和 StreamWriter
- **MergeStreamReaders()**: 合并多个 StreamReader 为一个

### 回调机制 (callbacks/)
Eino 的核心横切关注点处理机制，提供统一的回调处理能力：

- **Handler 接口**: 定义了 5 种回调时机的处理器
    - OnStart: 组件开始执行时
    - OnEnd: 组件正常结束时
    - OnError: 组件执行出错时
    - OnStartWithStreamInput: 处理流式输入开始时
    - OnEndWithStreamOutput: 处理流式输出结束时
- **HandlerBuilder**: 流式构建器模式，方便创建自定义回调处理器
- **组件模板**: utils/callbacks/template 提供针对不同组件类型的专用处理器
- **全局回调**: 支持全局回调处理器，对所有组件生效
- **方面注入**: 编排框架会自动为不支持回调的组件注入回调能力

### ADK 智能体开发包 (adk/)
Agent Development Kit (ADK) 是 Eino 的核心智能体开发包，提供完整的智能体构建和运行能力：

- **核心接口**: Agent 接口定义了智能体的基本行为，支持流式事件输出
- **Agent Tool**: 将智能体封装为可调用的工具，支持在更大的智能体系统中嵌套使用
- **Runner**: 智能体运行器，支持流式和非流式执行，集成检查点存储
- **消息处理**: 统一的消息变体 (MessageVariant) 处理流式和非流式消息
- **预置智能体架构**:
    - **planexecute/**: 计划-执行模式智能体，支持结构化规划和步骤执行
    - **supervisor/**: 监督者模式多智能体系统，实现层次化智能体协调

### 多智能体系统架构 (Multi-Agent Architecture)
Eino 提供三种分层的多智能体系统设计模式，适应不同复杂度的应用需求：

#### **Host-Specialist 模式** (`flow/agent/multiagent/host`)
- **设计思想**: 基于工具调用的专家路由系统
- **核心机制**: Host 智能体通过 `ToolCallingChatModel` 将专家智能体注册为工具，根据用户输入选择最合适的专家
- **适用场景**: 客服分诊、医疗分类、技术支持路由等专家咨询系统
- **特点**: 实现简单、性能好、延迟低，但专家间协作能力有限

#### **Supervisor 模式** (`adk/prebuilt/supervisor`)
- **设计思想**: 基于转移控制的层次化协调系统
- **核心机制**: 监督者智能体通过 `TransferToAgentAction` 明确控制智能体转移，子智能体只能与监督者通信
- **适用场景**: 复杂对话流程、项目管理、业务流程处理等需要严格协调的任务
- **特点**: 协调能力强、流程可控，但实现复杂度中等

#### **Plan-Execute 模式** (`adk/prebuilt/planexecute`)
- **设计思想**: 基于结构化规划的循环执行系统
- **核心机制**: 三个智能体分工合作（规划器制定计划、执行器执行步骤、再规划器动态调整）
- **适用场景**: 项目管理、研究分析、复杂问题分解等需要结构化执行的任务
- **特点**: 结构化执行、动态调整能力强，但实现复杂且延迟较高

#### **架构层次关系**
```
应用场景层:
├─ Host-Specialist (Flow层，简单路由)
├─ Supervisor (ADK层，协调控制)
└─ Plan-Execute (ADK层，项目执行)

框架支撑层:
├─ ADK 框架 (事件驱动、状态管理)
├─ Flow 编排 (Graph组合、工具调用)
└─ Compose 核心 (组件编排、流处理)
```

#### **选择建议**
- **初级团队/简单场景**: Host-Specialist 模式
- **中级团队/协调需求**: Supervisor 模式
- **高级团队/复杂项目**: Plan-Execute 模式

### 预置流程 (flow/)
提供常用的 AI 应用流程：
- **agent/**: 智能体实现，包含 ReAct 模式和多智能体
- **retriever/**: 高级检索器，如多查询检索器、父文档检索器等
- **indexer/**: 高级索引器实现

## 开发注意事项

### 组件接口设计
- 所有组件都支持统一的回调机制 (Callbacks)
- 组件接口分为基础接口和扩展接口
- 优先使用不可变的设计模式，避免状态修改和并发问题
- 使用 HandlerBuilder 构建自定义回调处理器
- 通过 TimingChecker 优化回调处理性能

### 智能体开发
- 使用 ADK 包构建智能体，实现统一的 Agent 接口
- 利用 Agent Tool 将智能体封装为可复用组件
- 使用 Runner 执行智能体，支持流式和非流式模式
- 预置智能体模式：计划-执行、监督者多智能体等

### 编排最佳实践
- 使用 Graph 编排复杂业务逻辑
- 利用 Field Mapping 实现精细的数据流控制
- 合理使用 Branch 和 State 处理条件逻辑

### 流处理
- 所有组件都支持流式和非流式两种模式
- 编排框架会自动处理流的转换和合并
- 在需要实时响应的场景下优先使用流式 API

### 测试策略
- 所有公共接口都需要对应的单元测试
- 使用 mockgen 生成 mock 对象进行测试
- 基准测试用于监控性能变化

### 代码注释规范

中文注释应遵循奥卡姆剃刀原则："如无必要，勿增实体"。

#### 注释核心原则
根据 Go 中文注释最佳实践优化这些文件的注释。遵循以下原则:

1. 所有导出的类型、函数、方法都应该有注释
2. 注释应该以被注释的名称开头
3. 注释应该完整、清晰地说明功能
4. 使用标准的文档格式
5. 添加必要的示例代码
6. 对复杂逻辑添加行内注释说明

#### 维护策略

- **注释即设计**：先写注释再写代码，注释体现业务意图
- **定期审查**：代码审查时检查注释必要性，移除显而易见的注释
- **工具辅助**：使用 `go doc` 检查文档覆盖度，自动化检查注释规范

## 依赖管理

- Go 1.18+
- 主要依赖：kin-openapi v0.118.0（为了保持 Go 1.18 兼容性）
- 使用 go modules 管理依赖

## 相关仓库

- [eino-ext](https://github.com/cloudwego/eino-ext): 组件实现、回调处理器、开发工具
- [eino-examples](https://github.com/cloudwego/eino-examples): 示例应用和最佳实践