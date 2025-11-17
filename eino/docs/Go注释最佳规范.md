# 🧭 Go 文档注释缩进规范（GoDoc Indentation Guidelines）

> 版本：v1.0
> 适用范围：所有 Go 源文件（`package`、`func`、`type` 等文档注释）
> 兼容环境：`gofmt` · `pkg.go.dev` · `GoLand IDE`

---

## 🧩 一、核心原则

1. **所有 GoDoc 注释以 `//` 开头，禁止使用 `/* ... */` 多行注释**。
2. **缩进与空白由 `gofmt` 决定**，不得自行用空格调整。
3. **在 GoLand 中格式化 (`Reformat Code`) 不得破坏缩进。**

    * 确保「Code Style → Go → Tabs and Indents → Use tab character」已启用。
    * `.editorconfig` 可选，不强制。

---

## 📘 二、缩进规则总览

| 内容类型    | 示例                   | 缩进规则        | 说明               |
| ------- | -------------------- | ----------- | ---------------- |
| **包注释** | `// Package foo ...` | 无缩进（首列）     | 必须从第一列开始         |
| **段落**  | 普通句子 + 空行分隔          | 无缩进         | 每段之间空一行          |
| **列表项** | `//    - 项目`            | 1 个 **tab** | 使用短横线（`-`）而非 `•` |
| **代码块** | `//    <代码>`            | 1 个 **tab** | 代码块中每行均以 tab 开头  |

---

## 📗 三、正确示例

```go
// Package callbacks provides callback mechanisms for component execution in Eino.
//
// Overview:
//
//    This package allows developers to inject callback handlers at different stages
//    of component execution, such as start, end, and error handling.
//
// Features:
//
//    - Unified lifecycle management for components
//    - Easy logging and metrics collection
//    - Support for both normal and stream inputs/outputs
//
// Example:
//
//    handler := callbacks.NewHandlerBuilder().
//        OnStart(func(ctx context.Context) context.Context {
//            // Handle start
//            return ctx
//        }).
//        OnEnd(func(ctx context.Context) context.Context {
//            // Handle end
//            return ctx
//        }).
//        Build()
package callbacks
```

✅ **显示结果：**

* 在 pkg.go.dev、GoLand、VSCode 中对齐一致；
* `gofmt` 运行后不会有 diff；
* `Reformat Code` 不会插入空格或破坏缩进。

---

## 📙 四、错误示例与原因

| 错误写法                    | 问题                           |
| ----------------------- | ---------------------------- |
| `//   - 项目`（空格缩进）       | GoDoc 不识别为列表，会显示为普通段落        |
| `// • 项目`               | 非 ASCII 字符，GoDoc 不解析为 bullet |
| `//    code line`（4 空格） | GoDoc 不识别为代码块                |
| `/* ... */`             | 多行注释不被 GoDoc 渲染为文档           |
| `//  包说明`（带前导空格）        | 包注释无法识别为 pkg-level 文档        |

---

## 📒 五、推荐开发习惯

1. **始终运行 `gofmt` 或 `goimports`。**
2. **保持包注释位于文件开头，紧接 `package` 语句。**
3. **在 GoLand 中：**

    * `Editor → Code Style → Go → Tabs and Indents → Use tab character` ✅
    * `Editor → Code Style → Go → Continuation indent` → 0
    * 关闭 `.editorconfig` 或确保其与 gofmt 一致（`indent_style=tab`）。

---

## 📘 六、团队统一检查（可选）

可在 `.golangci.yaml` 中开启 `gofmt` 检查：

```yaml
linters:
  enable:
    - gofmt
    - goimports
```

若要阻止空格缩进引入 diff，可加入 CI 检查：

```bash
gofmt -l .
```

---

## ✅ 七、快速模板（推荐复制）

```go
// Package foo demonstrates correct GoDoc indentation.
//
// Overview:
//
//    This package provides a unified callback framework.
//
// Features:
//
//    - Lifecycle hooks
//    - Stream input/output support
//    - Error handling
//
// Example:
//
//    handler := callbacks.NewHandlerBuilder().
//        OnStart(func(ctx context.Context) context.Context {
//            return ctx
//        }).
//        Build()
package foo
```

---