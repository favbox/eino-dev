# Callbacks 回调机制

本包为 Eino 框架中的组件执行提供回调机制，允许开发者在组件执行的不同阶段注入回调处理器，实现横切关注点的处理。

## 概述

回调机制是面向切面编程(AOP)在 Eino 框架中的具体实现，允许在不修改组件业务逻辑的情况下，在组件执行的关键时机插入自定义处理逻辑。

### 回调时机

- **OnStart**：组件开始执行时
- **OnEnd**：组件正常结束时
- **OnError**：组件执行出错时
- **OnStartWithStreamInput**：流式输入开始时
- **OnEndWithStreamOutput**：流式输出结束时

### 核心特性

- 统一的回调接口设计，支持所有组件类型
- 面向切面编程(AOP)的设计思想，实现业务逻辑与横切关注点的分离
- 支持全局回调和用户特定回调的灵活组合
- 完整的流式处理支持，自动处理流的生命周期管理
- 类型安全的设计，通过泛型确保编译时类型检查
- 高性能的回调链执行，支持处理器执行顺序的精确控制

## 使用方式

本包提供三种构建回调处理器的方式：

### 1. 使用 HandlerBuilder 构建通用回调处理器

适用于需要统一处理多种组件类型的场景，支持函数式编程风格：

```go
handler := callbacks.NewHandlerBuilder().
    OnStart(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
        // 处理组件开始事件
        log.Printf("Component started: %s", info.Name)
        return ctx
    }).
    OnEnd(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
        // 处理组件结束事件
        log.Printf("Component completed: %s", info.Name)
        return ctx
    }).
    OnError(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
        // 处理组件错误事件
        log.Printf("Component error: %s, error: %v", info.Name, err)
        return ctx
    }).
    OnStartWithStreamInput(func(ctx context.Context, info *callbacks.RunInfo, input *schema.StreamReader[callbacks.CallbackInput]) context.Context {
        // 处理流式输入开始事件
        log.Printf("Stream input started: %s", info.Name)
        return ctx
    }).
    OnEndWithStreamOutput(func(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
        // 处理流式输出结束事件
        log.Printf("Stream output ended: %s", info.Name)
        return ctx
    }).
    Build()

// 使用处理器
runnable.Invoke(ctx, input, compose.WithCallbacks(handler))
```

**使用场景**：
- 需要统一处理多种组件类型的通用逻辑
- 实现框架级别的横切关注点（如日志、监控）
- 快速原型开发和简单场景

### 2. 使用 HandlerHelper 构建组件专用回调处理器

通过 `utils/callbacks` 包的 HandlerHelper 为不同组件类型创建专用处理器，支持类型安全的回调输入/输出处理：

```go
import "github.com/favbox/eino/utils/callbacks"

// 创建模型组件专用处理器
modelHandler := &model.CallbackHandler{
    OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *model.CallbackInput) context.Context {
        log.Printf("Model execution started: %s", info.Name)
        // 可以直接访问模型特定的输入字段
        log.Printf("Input messages count: %d", len(input.Messages))
        return ctx
    },
    OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *model.CallbackOutput) context.Context {
        log.Printf("Model execution completed: %s", info.Name)
        // 可以直接访问模型特定的输出字段
        if output.Message != nil {
            log.Printf("Response: %s", output.Message.Content)
        }
        return ctx
    },
}

// 创建提示词组件专用处理器
promptHandler := &prompt.CallbackHandler{
    OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *prompt.CallbackOutput) context.Context {
        log.Printf("Prompt execution completed: %s", info.Name)
        log.Printf("Prompt result: %s", output.Result)
        return ctx
    },
}

// 创建工具组件专用处理器
toolHandler := &tool.CallbackHandler{
    OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context {
        log.Printf("Tool execution started: %s", input.ToolName)
        return ctx
    },
    OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *tool.CallbackOutput) context.Context {
        log.Printf("Tool execution completed: %s", info.Name)
        log.Printf("Tool result: %v", output.Result)
        return ctx
    },
}

// 使用 HandlerHelper 构建组合处理器
handler := callbacks.NewHandlerHelper().
    ChatModel(modelHandler).
    Prompt(promptHandler).
    Tool(toolHandler).
    Fallback(genericHandler).  // 通用处理器作为后备
    Handler()

// 使用处理器
// runnable.Invoke(ctx, input, compose.WithCallbacks(handler))
```

**HandlerHelper 支持的组件类型**：
- **Prompt 组件** (通过 `prompt.CallbackHandler`)
- **ChatModel 组件** (通过 `model.CallbackHandler`)
- **Embedding 组件** (通过 `embedding.CallbackHandler`)
- **Indexer 组件** (通过 `indexer.CallbackHandler`)
- **Retriever 组件** (通过 `retriever.CallbackHandler`)
- **Document Loader 组件** (通过 `loader.CallbackHandler`)
- **Document Transformer 组件** (通过 `transformer.CallbackHandler`)
- **Tool 组件** (通过 `tool.CallbackHandler`)
- **Graph、Chain、ToolsNode、Lambda** (通过通用 `Handler`)

**使用场景**：
- 需要为不同组件类型实现专门的回调逻辑
- 需要访问组件特定的输入/输出字段
- 实现复杂的组件特定的横切关注点

### 3. 使用面向切面编程的注入函数

组件开发者可以在组件实现中直接调用注入函数，实现回调功能的无缝集成：

```go
func (t *testChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (resp *schema.Message, err error) {
    defer func() {
        if err != nil {
            // 错误处理回调
            callbacks.OnError(ctx, err)
        }
    }()

    // 开始回调
    ctx = callbacks.OnStart(ctx, &model.CallbackInput{
        Messages: input,
        Tools:    nil,
        Extra:    map[string]any{
            "model": t.name,
            "timestamp": time.Now(),
        },
    })

    // 执行业务逻辑
    resp, err = t.doGenerate(ctx, input, opts...)
    if err != nil {
        return nil, err
    }

    // 结束回调
    ctx = callbacks.OnEnd(ctx, &model.CallbackOutput{
        Message: resp,
        Extra: map[string]any{
            "tokens_used": t.calculateTokens(resp),
            "latency_ms": time.Since(startTime).Milliseconds(),
        },
    })

    return resp, nil
}

// 流式处理的例子
func (t *testChatModel) StreamGenerate(ctx context.Context, input *schema.StreamReader[*model.CallbackInput]) (*schema.StreamReader[*model.CallbackOutput], error) {
    // 流式输入开始回调
    ctx, newInput := callbacks.OnStartWithStreamInput(ctx, input)

    // 执行流式业务逻辑...

    // 流式输出结束回调
    ctx, output := callbacks.OnEndWithStreamOutput(ctx, resultStream)

    return output, nil
}
```

**可用的注入函数**：
- `OnStart[T any]`：在组件开始时注入回调逻辑
- `OnEnd[T any]`：在组件正常结束时注入回调逻辑
- `OnError`：在组件出错时注入回调逻辑
- `OnStartWithStreamInput[T any]`：在流式输入开始时注入回调逻辑
- `OnEndWithStreamOutput[T any]`：在流式输出结束时注入回调逻辑

**使用场景**：
- 组件开发者在组件实现中直接集成回调功能
- 需要精细化控制回调时机和逻辑
- 实现组件级别的横切关注点

## 全局回调管理

支持设置全局回调处理器，对所有组件生效：

```go
// 设置全局回调处理器
globalHandler := callbacks.NewHandlerBuilder().
    OnStart(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
        // 全局开始处理逻辑
        log.Printf("[GLOBAL] Component started: %s", info.Name)
        return ctx
    }).
    OnError(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
        // 全局错误处理逻辑
        log.Printf("[GLOBAL] Component error: %s, error: %v", info.Name, err)
        // 可以在这里添加监控、告警等逻辑
        return ctx
    }).
    Build()

// 添加全局处理器（会保留现有的全局处理器）
callbacks.AppendGlobalHandlers(globalHandler)

// 或者替换所有全局处理器（已废弃，不推荐）
// callbacks.InitCallbackHandlers([]Handler{globalHandler})
```

**全局回调特点**：
- 对所有组件生效，包括嵌套的组件调用
- 在用户特定回调之前执行
- 适用于框架级别的横切关注点处理
- 可以设置多个全局处理器，按添加顺序执行

## 最佳实践

### 1. 选择合适的使用方式

- **HandlerBuilder**：实现通用的横切关注点处理（如日志、监控）
- **HandlerHelper**：为不同组件类型实现专门的回调逻辑
- **面向切面编程注入函数**：在组件内部实现精细化的回调控制

### 2. 性能考虑

```go
// 好的实践：轻量级的回调处理
handler := callbacks.NewHandlerBuilder().
    OnStart(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
        // 使用异步方式记录日志，避免阻塞主流程
        go func() {
            logger.Info("component started", "name", info.Name)
        }()
        return ctx
    }).
    Build()

// 避免的实践：在回调中执行重逻辑
handler := callbacks.NewHandlerBuilder().
    OnStart(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
        // 避免在回调中执行耗时操作
        result := someHeavyOperation() // ❌ 可能影响性能
        return ctx
    }).
    Build()
```

### 3. 错误处理

```go
handler := callbacks.NewHandlerBuilder().
    OnError(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
        // 记录错误信息
        logger.Error("component error",
            "name", info.Name,
            "error", err,
            "component", info.Component)

        // 根据错误类型进行不同处理
        if isRetryableError(err) {
            // 可重试错误的处理逻辑
            return context.WithValue(ctx, "retryable", true)
        }

        return ctx
    }).
    Build()
```

### 4. 流式处理注意事项

```go
func (t *streamComponent) ProcessStream(ctx context.Context, input *schema.StreamReader[Data]) (*schema.StreamReader[Result], error) {
    // 流式输入开始回调
    ctx, newInput := callbacks.OnStartWithStreamInput(ctx, input)

    // 处理流时要注意：
    // 1. 确保流被正确关闭
    // 2. 避免在回调中修改流的内容
    // 3. 处理流中的错误

    resultStream, err := t.processStreamData(ctx, newInput)
    if err != nil {
        return nil, err
    }

    // 流式输出结束回调
    ctx, output := callbacks.OnEndWithStreamOutput(ctx, resultStream)

    return output, nil
}
```

### 5. 测试回调处理器

```go
func TestCallbackHandler(t *testing.T) {
    var startCalled, endCalled bool

    handler := callbacks.NewHandlerBuilder().
        OnStart(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
            startCalled = true
            return ctx
        }).
        OnEnd(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
            endCalled = true
            return ctx
        }).
        Build()

    // 测试组件
    testComponent := NewTestComponent()

    // 执行测试
    ctx := context.Background()
    result, err := testComponent.Invoke(ctx, "test input", compose.WithCallbacks(handler))

    // 验证回调是否被正确调用
    assert.True(t, startCalled, "OnStart should be called")
    assert.True(t, endCalled, "OnEnd should be called")
    assert.NoError(t, err)
    assert.NotNil(t, result)
}
```

## 常见场景示例

### 1. 日志记录

```go
// 统一日志处理器
loggingHandler := callbacks.NewHandlerBuilder().
    OnStart(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
        logger.Info("component started",
            "name", info.Name,
            "type", info.Type,
            "component", fmt.Sprintf("%T", info.Component))
        return ctx
    }).
    OnEnd(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
        logger.Info("component completed",
            "name", info.Name,
            "type", info.Type)
        return ctx
    }).
    OnError(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
        logger.Error("component failed",
            "name", info.Name,
            "type", info.Type,
            "error", err)
        return ctx
    }).
    Build()
```

### 2. 监控指标收集

```go
// 监控指标处理器
metricsHandler := callbacks.NewHandlerBuilder().
    OnStart(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
        // 记录开始时间
        return context.WithValue(ctx, "start_time", time.Now())
    }).
    OnEnd(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
        // 计算执行时间并记录指标
        if startTime, ok := ctx.Value("start_time").(time.Time); ok {
            duration := time.Since(startTime)
            metrics.RecordComponentLatency(info.Name, duration)
            metrics.IncrementComponentCount(info.Name, "success")
        }
        return ctx
    }).
    OnError(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
        // 记录错误指标
        metrics.IncrementComponentCount(info.Name, "error")
        return ctx
    }).
    Build()
```

### 3. 缓存处理

```go
// 缓存处理器
cacheHandler := &model.CallbackHandler{
    OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *model.CallbackInput) context.Context {
        // 检查缓存
        if cached := cache.Get(input.Messages); cached != nil {
            return context.WithValue(ctx, "cached_result", cached)
        }
        return ctx
    },
    OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *model.CallbackOutput) context.Context {
        // 存储到缓存
        if cached := ctx.Value("cached_result"); cached == nil && output.Message != nil {
            cache.Set(output.Message)
        }
        return ctx
    },
}
```

## 总结

Eino 的回调机制提供了灵活而强大的横切关注点处理能力。通过合理选择使用方式、遵循最佳实践，可以有效地实现日志记录、监控、缓存、错误处理等功能，同时保持代码的清晰性和性能。

选择合适的使用方式：
- **框架开发** → HandlerBuilder + 全局回调
- **应用开发** → HandlerHelper + 组件特定处理器
- **组件开发** → 面向切面编程注入函数

记住始终考虑性能影响，避免在热路径中执行重逻辑，并通过充分的测试确保回调逻辑的正确性。