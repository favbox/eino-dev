package components

// Typer 接口用于获取组件实现的类型名称。
//
// 如果组件实现了该接口，默认情况下组件实例的完整名称为 {Typer}{Component} 格式。
// 推荐使用驼峰明明发（Camel Case）为 Typer 命名。
type Typer interface {
	// GetType 返回组件实现的类型名称。
	// 用于组件注册、识别和显示。
	GetType() string
}

// GetType 尝试从组件实例中提取类型名称。
//
// 如果组件实现了 Typer 接口，返回去类型名称和 true；
// 否则返回空字符串和 false。
//
// 该函数常用于组件注册、调试和类型检查场景。
func GetType(component any) (string, bool) {
	if typer, ok := component.(Typer); ok {
		return typer.GetType(), true
	}

	return "", false
}

// Checker 接口用于控制组件中的回调切面状态。
//
// 当组件实现该接口并返回 true 时，框架不会启动默认的回调切面。
// 相反，组件将自行决定回调执行的位置和注入的信息。
//
// 这种设计允许组件对回调机制进行精细化控制，避免不必要的开销。
type Checker interface {
	// IsCallbacksEnabled 检查组件是否启用了回调机制。
	//
	// 返回 true 表示组件使用自定义的回调控制逻辑；
	// 返回 false 表示使用框架的默认回调切面。
	IsCallbacksEnabled() bool
}

// IsCallbacksEnabled 检查组件实例是否启用了回调机制。
//
// 如果组件实现了 Checker 接口，返回其配置值；
// 否则返回 false，表示使用默认的回调机制。
func IsCallbacksEnabled(i any) bool {
	if checker, ok := i.(Checker); ok {
		return checker.IsCallbacksEnabled()
	}
	return false
}

// Component 类型定义了不同种类组件的名称标识符。
//
// 用于组件注册、类型识别和系统内组件分类。
type Component string

const (
	// 模具提：模型——工具——提示词

	ComponentOfChatModel Component = "ChatModel"    // 聊天模型组件
	ComponentOfTool      Component = "Tool"         // 工具组件
	ComponentOfPrompt    Component = "ChatTemplate" // 提示词模板组件

	// 文嵌检索：文档——嵌入——检索器——索引器

	ComponentOfLoader      Component = "Loader"              // 文档加载器组件
	ComponentOfTransformer Component = "DocumentTransformer" // 文档转换器组件
	ComponentOfEmbedding   Component = "Embedding"           // 嵌入模型组件
	ComponentOfRetriever   Component = "Retriever"           // 检索器组件
	ComponentOfIndexer     Component = "Indexer"             // 索引器组件
)
