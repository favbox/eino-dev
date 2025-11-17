# ADK (Agent Development Kit) 架构设计分析

## 一、整体架构概览

ADK 是 Eino 框架的核心智能体开发包，提供完整的智能体构建、运行和编排能力。其架构遵循**分层设计、事件驱动、状态管理**三大核心原则。

```
┌─────────────────────────────────────────────────────────────┐
│                    应用层 (Prebuilt)                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │  Supervisor  │  │ Plan-Execute │  │    Deep      │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
├─────────────────────────────────────────────────────────────┤
│                    编排层 (Core ADK)                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ FlowAgent    │  │ WorkflowAgent│  │  ChatModel   │      │
│  │ (转移控制)   │  │ (结构化执行) │  │   Agent      │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
├─────────────────────────────────────────────────────────────┤
│                    运行时层 (Runtime)                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Runner     │  │  RunContext  │  │  Session     │      │
│  │ (执行引擎)   │  │ (上下文管理) │  │ (状态存储)   │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
├─────────────────────────────────────────────────────────────┤
│                    基础层 (Foundation)                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ AsyncIterator│  │  AgentEvent  │  │  Interrupt   │      │
│  │ (流处理)     │  │ (事件传递)   │  │ (中断恢复)   │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

## 二、核心组件设计

### 2.1 Agent 接口体系

```eino/adk/interface.go#L101-117
type Agent interface {
    Name(ctx context.Context) string
    Description(ctx context.Context) string
    Run(ctx context.Context, input *AgentInput, options ...AgentRunOption) *AsyncIterator[*AgentEvent]
}

type ResumableAgent interface {
    Agent
    Resume(ctx context.Context, info *ResumeInfo, opts ...AgentRunOption) *AsyncIterator[*AgentEvent]
}

type OnSubAgents interface {
    OnSetSubAgents(ctx context.Context, subAgents []Agent) error
    OnSetAsSubAgent(ctx context.Context, parent Agent) error
    OnDisallowTransferToParent(ctx context.Context) error
}
```

**设计特点**：
1. **统一接口**：所有智能体实现相同接口，支持任意组合
2. **流式输出**：通过 `AsyncIterator[*AgentEvent]` 实现事件驱动
3. **可恢复性**：`ResumableAgent` 支持中断后恢复执行
4. **层次关系**：`OnSubAgents` 处理父子智能体关系

### 2.2 事件驱动模型

```eino/adk/interface.go#L77-96
type AgentEvent struct {
    AgentName string           // 事件来源智能体
    RunPath   []RunStep        // 执行路径（多智能体追踪）
    Output    *AgentOutput     // 输出数据
    Action    *AgentAction     // 控制指令
    Err       error            // 错误信息
}

type AgentAction struct {
    Exit            bool                    // 退出指令
    Interrupted     *InterruptInfo          // 中断信息
    TransferToAgent *TransferToAgentAction  // 转移指令
    BreakLoop       *BreakLoopAction        // 跳出循环
    CustomizedAction any                    // 自定义动作
}
```

**核心机制**：
- **事件流**：智能体通过事件流向外部报告进度和结果
- **控制流**：通过 `AgentAction` 实现智能体间的流程控制
- **路径追踪**：`RunPath` 记录多智能体执行轨迹

### 2.3 FlowAgent - 多智能体编排核心

```eino/adk/flow.go#L36-47
type flowAgent struct {
    Agent                    // 被包装的智能体
    subAgents   []*flowAgent // 子智能体列表
    parentAgent *flowAgent   // 父智能体引用
    
    disallowTransferToParent bool            // 是否禁止向父智能体转移
    historyRewriter          HistoryRewriter // 历史消息重写器
    checkPointStore          compose.CheckPointStore // 检查点存储
}
```

**核心能力**：

#### 智能体转移机制
```eino/adk/flow.go#L292-322
func (a *flowAgent) run(...) {
    // 1. 执行当前智能体
    for event := range aIter {
        if event.Action != nil && event.Action.TransferToAgent != nil {
            destName = event.Action.TransferToAgent.DestAgentName
        }
        generator.Send(event)
    }
    
    // 2. 如果需要转移，查找目标智能体
    if destName != "" {
        agentToRun := a.getAgent(ctx, destName)
        // 3. 递归执行目标智能体
        subAIter := agentToRun.Run(ctx, nil, opts...)
        for subEvent := range subAIter {
            generator.Send(subEvent)
        }
    }
}
```

#### 历史消息重写
```eino/adk/flow.go#L178-229
func (a *flowAgent) genAgentInput(ctx context.Context, runCtx *runContext, skipTransferMessages bool) (*AgentInput, error) {
    // 1. 收集所有事件中的消息
    for _, event := range runCtx.Session.getEvents() {
        if belongToRunPath(event.RunPath, runPath) {
            historyEntries = append(historyEntries, &HistoryEntry{
                AgentName: event.AgentName,
                Message:   msg,
            })
        }
    }
    
    // 2. 通过 historyRewriter 重写消息
    messages, err := a.historyRewriter(ctx, historyEntries)
    return &AgentInput{Messages: messages}, nil
}
```

**设计洞察**：
- **透明转发**：FlowAgent 作为中间层，将子智能体事件透明转发给调用者
- **上下文注入**：自动为子智能体注入历史消息和运行上下文
- **路径管理**：维护完整的执行路径，支持复杂的多智能体调用链

### 2.4 WorkflowAgent - 结构化执行

```eino/adk/workflow.go#L28-38
type workflowAgent struct {
    name        string
    description string
    subAgents   []*flowAgent
    
    mode workflowAgentMode  // 执行模式：Sequential/Loop/Parallel
    maxIterations int       // 最大循环次数
}
```

**三种执行模式**：

#### Sequential - 顺序执行
```eino/adk/workflow.go#L100-168
func (a *workflowAgent) runSequential(...) (exit, interrupted bool) {
    for i := 0; i < len(a.subAgents); i++ {
        subIterator := a.subAgents[i].Run(ctx, input, opts...)
        
        for event := range subIterator {
            if event.Action.Interrupted != nil {
                // 包装中断信息，保存当前进度
                return true, true
            }
            if event.Action.Exit {
                return true, false
            }
            generator.Send(event)
        }
    }
    return false, false
}
```

#### Loop - 循环执行
```eino/adk/workflow.go#L211-226
func (a *workflowAgent) runLoop(...) {
    iterations := 0
    for iterations < a.maxIterations || a.maxIterations == 0 {
        exit, interrupted := a.runSequential(ctx, input, generator, intInfo, iterations, opts...)
        if interrupted || exit {
            return
        }
        iterations++
    }
}
```

#### Parallel - 并行执行
```eino/adk/workflow.go#L228-277
func (a *workflowAgent) runParallel(...) {
    var wg sync.WaitGroup
    for i, runner := range runners {
        wg.Add(1)
        go func(idx int, runner func) {
            iterator := runner(ctx)
            for event := range iterator {
                if event.Action.Interrupted != nil {
                    interruptMap[idx] = event.Action.Interrupted
                }
                generator.Send(event)
            }
        }(i, runner)
    }
    wg.Wait()
}
```

**设计优势**：
- **结构化控制**：明确的执行流程，便于调试和监控
- **中断恢复**：保存执行进度，支持从断点继续
- **灵活组合**：三种模式可以任意嵌套组合

### 2.5 ChatModelAgent - ReAct 智能体

```eino/adk/chatmodel.go#L180-206
type ChatModelAgent struct {
    name        string
    model       model.ToolCallingChatModel
    toolsConfig ToolsConfig
    
    beforeChatModels, afterChatModels []func(context.Context, *ChatModelAgentState) error
    
    subAgents   []Agent  // 作为工具的子智能体
    exit        tool.BaseTool
}
```

**ReAct 循环实现**：
```eino/adk/react.go#L79-172
func newReact(ctx context.Context, config *reactConfig) (reactGraph, error) {
    g := compose.NewGraph[[]Message, Message]()
    
    // 1. ChatModel 节点：生成工具调用
    g.AddChatModelNode("ChatModel", chatModel)
    
    // 2. ToolNode 节点：执行工具
    g.AddToolsNode("ToolNode", toolsNode)
    
    // 3. 条件分支：检查是否有工具调用
    toolCallCheck := func(ctx context.Context, sMsg MessageStream) (string, error) {
        if hasToolCalls(sMsg) {
            return "ToolNode", nil
        }
        return compose.END, nil
    }
    g.AddBranch("ChatModel", compose.NewStreamGraphBranch(toolCallCheck))
    
    // 4. 工具执行后回到 ChatModel
    g.AddEdge("ToolNode", "ChatModel")
    
    return g, nil
}
```

**设计亮点**：
- **基于 Graph 编排**：利用 Compose 框架的 Graph 能力
- **工具即智能体**：子智能体自动封装为工具（通过 `agent_tool.go`）
- **中间件扩展**：通过 `AgentMiddleware` 扩展能力

### 2.6 AgentTool - 智能体工具化

```eino/adk/agent_tool.go#L57-94
type agentTool struct {
    agent Agent
    fullChatHistoryAsInput bool
    inputSchema *schema.ParamsOneOf
}

func (at *agentTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
    // 1. 检查是否需要恢复
    if bResume {
        iter = runner.Resume(ctx, checkPointID, opts...)
    } else {
        // 2. 正常运行
        iter = runner.Run(ctx, input, opts...)
    }
    
    // 3. 处理中断
    if lastEvent.Action.Interrupted != nil {
        // 保存中断信息到 State
        compose.ProcessState(ctx, func(ctx context.Context, st *State) error {
            st.AgentToolInterruptData[toolCallID] = interruptInfo
            return nil
        })
        return "", compose.InterruptAndRerun
    }
    
    // 4. 返回结果
    return lastEvent.Output.MessageOutput.Message.Content, nil
}
```

**关键机制**：
- **智能体复用**：任何智能体都可作为工具在其他智能体中使用
- **中断传递**：子智能体的中断会传递到父智能体
- **嵌套支持**：支持无限层级的智能体嵌套

## 三、运行时系统

### 3.1 Runner - 执行引擎

```eino/adk/runner.go#L24-62
type Runner struct {
    a               Agent
    enableStreaming bool
    store           compose.CheckPointStore
}

func (r *Runner) Run(ctx context.Context, messages []Message, opts ...AgentRunOption) *AsyncIterator[*AgentEvent] {
    // 1. 创建新的 RunContext
    ctx = ctxWithNewRunCtx(ctx)
    
    // 2. 执行智能体
    iter := toFlowAgent(ctx, r.a).Run(ctx, input, opts...)
    
    // 3. 如果有 CheckPointStore，处理中断保存
    if r.store != nil {
        go r.handleIter(ctx, iter, gen, checkPointID)
    }
    
    return iter
}
```

**中断处理**：
```eino/adk/runner.go#L92-116
func (r *Runner) handleIter(...) {
    var interruptedInfo *InterruptInfo
    for event := range aIter {
        if event.Action.Interrupted != nil {
            interruptedInfo = event.Action.Interrupted
        }
        gen.Send(event)
    }
    
    // 智能体执行完毕，如果有中断，保存检查点
    if interruptedInfo != nil && checkPointID != nil {
        saveCheckPoint(ctx, r.store, *checkPointID, runCtx, interruptedInfo)
    }
}
```

### 3.2 RunContext - 上下文管理

```eino/adk/runctx.go#L147-153
type runContext struct {
    RootInput *AgentInput  // 根输入（不变）
    RunPath   []RunStep    // 执行路径（记录调用链）
    Session   *runSession  // 会话状态（共享）
}
```

**生命周期管理**：
```eino/adk/runctx.go#L182-197
func initRunCtx(ctx context.Context, agentName string, input *AgentInput) (context.Context, *runContext) {
    runCtx := getRunCtx(ctx)
    if runCtx != nil {
        runCtx = runCtx.deepCopy()  // 深拷贝，避免修改父上下文
    } else {
        runCtx = &runContext{Session: newRunSession()}
    }
    
    // 添加当前智能体到路径
    runCtx.RunPath = append(runCtx.RunPath, RunStep{agentName})
    
    if runCtx.isRoot() {
        runCtx.RootInput = input
    }
    
    return setRunCtx(ctx, runCtx), runCtx
}
```

### 3.3 Session - 状态存储

```eino/adk/runctx.go#L18-25
type runSession struct {
    Events []*agentEventWrapper  // 所有事件历史
    Values map[string]any        // 会话变量
    interruptRunCtxs []*runContext // 中断上下文
    mtx sync.Mutex               // 并发保护
}
```

**事件追踪**：
```eino/adk/runctx.go#L127-133
func (rs *runSession) addEvent(event *AgentEvent) {
    rs.mtx.Lock()
    rs.Events = append(rs.Events, &agentEventWrapper{
        AgentEvent: event,
    })
    rs.mtx.Unlock()
}
```

**设计优势**：
- **共享状态**：所有子智能体共享同一个 Session
- **事件回溯**：完整保存事件历史，支持历史重写
- **并发安全**：通过 Mutex 保护并发访问

## 四、中断与恢复机制

### 4.1 中断流程

```
用户调用
   ↓
Runner.Run()
   ↓
Agent.Run() → 生成 AgentEvent(Action.Interrupted)
   ↓
Runner.handleIter() → 检测到中断
   ↓
saveCheckPoint() → 保存 RunContext + InterruptInfo
   ↓
返回中断事件给用户
```

### 4.2 恢复流程

```
用户调用 Runner.Resume(checkPointID)
   ↓
getCheckPoint() → 从 Store 恢复 RunContext
   ↓
setRunCtx() → 恢复上下文
   ↓
Agent.Resume() → 从中断点继续执行
   ↓
返回新的事件流
```

### 4.3 序列化设计

```eino/adk/interrupt.go#L52-58
type serialization struct {
    RunCtx *runContext
    Info   *InterruptInfo
}

// 使用 gob 编码
gob.NewEncoder(buf).Encode(&serialization{
    RunCtx: runCtx,
    Info:   info,
})
```

**特殊处理**：
```eino/adk/runctx.go#L32-43
func (a *agentEventWrapper) GobEncode() ([]byte, error) {
    // 如果是流式消息，先合并成单条消息再序列化
    if a.Output.MessageOutput.IsStreaming {
        a.Output.MessageOutput.MessageStream = 
            schema.StreamReaderFromArray([]Message{a.concatenatedMessage})
    }
    // ... gob 编码
}
```

## 五、预置智能体模式

### 5.1 Supervisor - 监督者模式

```eino/adk/prebuilt/supervisor/supervisor.go#L22-44
func New(ctx context.Context, conf *Config) (adk.Agent, error) {
    supervisorName := conf.Supervisor.Name(ctx)
    
    // 为每个子智能体配置确定性转移
    for _, subAgent := range conf.SubAgents {
        subAgents = append(subAgents, adk.AgentWithDeterministicTransferTo(ctx, &adk.DeterministicTransferConfig{
            Agent:        subAgent,
            ToAgentNames: []string{supervisorName},  // 只能转移到 Supervisor
        }))
    }
    
    return adk.SetSubAgents(ctx, conf.Supervisor, subAgents)
}
```

**架构特点**：
- **星型拓扑**：所有子智能体只与 Supervisor 通信
- **集中控制**：Supervisor 负责任务分发和结果汇总
- **单点协调**：避免子智能体间的直接通信

### 5.2 Plan-Execute - 计划执行模式

```eino/adk/prebuilt/planexecute/plan_execute.go#L848-866
func New(ctx context.Context, cfg *Config) (adk.Agent, error) {
    // 1. 创建 Execute-Replan 循环
    loop, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
        Name:          "execute_replan",
        SubAgents:     []adk.Agent{cfg.Executor, cfg.Replanner},
        MaxIterations: maxIterations,
    })
    
    // 2. 创建 Plan → Loop 顺序执行
    return adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
        Name:      "plan_execute_replan",
        SubAgents: []adk.Agent{cfg.Planner, loop},
    })
}
```

**执行流程**：
```
Planner (生成计划)
   ↓
┌─────────────────┐
│  Execute-Replan │ ← Loop
│  ┌──────────┐   │
│  │ Executor │   │  执行一个步骤
│  └────┬─────┘   │
│       ↓         │
│  ┌──────────┐   │
│  │Replanner │   │  评估进度，决定是否继续
│  └──────────┘   │
└─────────────────┘
```

## 六、核心设计模式

### 6.1 装饰器模式 (FlowAgent)

```eino/adk/flow.go#L107-115
func toFlowAgent(ctx context.Context, agent Agent, opts ...AgentOption) *flowAgent {
    var fa *flowAgent
    if fa, ok = agent.(*flowAgent); !ok {
        fa = &flowAgent{Agent: agent}  // 包装原始智能体
    } else {
        fa = fa.deepCopy()  // 如果已经是 FlowAgent，深拷贝
    }
    return fa
}
```

**作用**：
- 在不修改原始智能体的前提下增强功能
- 添加历史重写、转移控制等能力
- 支持多层嵌套装饰

### 6.2 适配器模式 (AgentTool)

```eino/adk/agent_tool.go#L57-94
type agentTool struct {
    agent Agent  // 适配的目标
}

func (at *agentTool) InvokableRun(...) (string, error) {
    // 将 Tool 接口适配为 Agent 接口
    iter := runner.Run(ctx, input, opts...)
    // 将 AgentEvent 转换为 Tool 返回值
    return lastEvent.Output.MessageOutput.Message.Content, nil
}
```

**作用**：
- 将 `Agent` 接口适配为 `tool.BaseTool` 接口
- 使智能体可以作为工具被其他智能体调用
- 实现智能体的递归组合

### 6.3 生成器模式 (AsyncIterator)

```eino/adk/utils.go
type AsyncIterator[T any] struct {
    ch chan T
}

func (iter *AsyncIterator[T]) Next() (T, bool) {
    val, ok := <-iter.ch
    return val, ok
}

type AsyncGenerator[T any] struct {
    ch chan T
}

func (gen *AsyncGenerator[T]) Send(val T) {
    gen.ch <- val
}
```

**作用**：
- 实现异步事件流
- 解耦生产者和消费者
- 支持流式输出和中断

### 6.4 模板方法模式 (WorkflowAgent)

```eino/adk/workflow.go#L58-84
func (a *workflowAgent) Run(...) *AsyncIterator[*AgentEvent] {
    switch a.mode {
    case workflowAgentModeSequential:
        a.runSequential(...)  // 模板方法1
    case workflowAgentModeLoop:
        a.runLoop(...)        // 模板方法2
    case workflowAgentModeParallel:
        a.runParallel(...)    // 模板方法3
    }
}
```

**作用**：
- 定义算法骨架，将具体实现延迟到子方法
- 三种执行模式共享相同的生命周期管理
- 便于扩展新的执行模式

## 七、核心设计优势

### 7.1 分层清晰

```
应用层：预置模式（Supervisor、Plan-Execute）
   ↓ 依赖
编排层：FlowAgent、WorkflowAgent、ChatModelAgent
   ↓ 依赖
运行时：Runner、RunContext、Session
   ↓ 依赖
基础层：AsyncIterator、AgentEvent、MessageVariant
```

每层职责明确，依赖关系单向，便于维护和扩展。

### 7.2 事件驱动

所有智能体通过 `AsyncIterator[*AgentEvent]` 通信：
- **解耦**：生产者和消费者完全解耦
- **流式**：支持实时输出，用户体验更好
- **可观测**：所有事件都可被监控和记录

### 7.3 状态管理

通过 `RunContext` 和 `Session` 实现状态隔离：
- **不可变输入**：`RootInput` 在整个执行过程中不变
- **共享状态**：所有子智能体共享 `Session`
- **路径追踪**：`RunPath` 记录完整调用链

### 7.4 可组合性

```
Agent (接口)
   ↓ 实现
ChatModelAgent (ReAct 智能体)
   ↓ 包装
FlowAgent (添加转移能力)
   ↓ 组合
WorkflowAgent (Sequential/Loop/Parallel)
   ↓ 组合
Supervisor / Plan-Execute (预置模式)
```

任意智能体都可以嵌套组合，实现复杂业务逻辑。

### 7.5 中断恢复

完整的 Checkpoint 机制：
- **序列化**：RunContext、Session、Event 全部可序列化
- **增量保存**：只保存必要的状态信息
- **精确恢复**：恢复后从中断点继续执行

## 八、与 Compose 框架的关系

ADK 大量复用 Compose 框架能力：

```
ADK                      Compose
────────────────────    ────────────────────
ChatModelAgent    →     Graph (ReAct 循环)
RunContext        →     State (状态管理)
AgentTool         →     ToolsNode (工具执行)
CheckPointStore   →     CheckPointStore (持久化)
```

**设计哲学**：
- ADK 专注于智能体抽象和编排逻辑
- Compose 提供底层的图执行和状态管理
- 两层分工明确，各司其职

## 九、总结

ADK 的架构设计体现了以下核心思想：

1. **统一抽象**：`Agent` 接口是一切的基础
2. **分层设计**：应用层、编排层、运行时、基础层职责清晰
3. **事件驱动**：通过 `AgentEvent` 实现解耦和流式处理
4. **状态管理**：`RunContext` + `Session` 提供完整的上下文管理
5. **可组合性**：任意智能体可嵌套组合，实现复杂逻辑
6. **中断恢复**：完整的 Checkpoint 机制支持长时间运行
7. **模式复用**：预置模式（Supervisor、Plan-Execute）降低使用门槛

这是一个**设计优雅、扩展性强、工程化完善**的智能体开发框架。

