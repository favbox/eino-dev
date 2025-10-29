/*
Package components 是 Eino 支持的基本组件。
*/
package components

// Component 表示 Eino 框架中不同类型的组建类型
type Component string

const (
	// ComponentOfPrompt 提示词模板组件，用于动态生成和格式化提示词
	ComponentOfPrompt Component = "ChatTemplate"
	// ComponentOfChatModel 聊天模型组件，用于对话生成和自然语言处理
	ComponentOfChatModel Component = "ChatModel"
	// ComponentOfEmbedding 嵌入模型组件，用于文本等向量化表示
	ComponentOfEmbedding Component = "Embedding"
	// ComponentOfIndexer 索引器组件，用于构建和管理向量索引
	ComponentOfIndexer Component = "Indexer"
	// ComponentOfRetriever 检索器组件，用于信息检索和相似度搜索
	ComponentOfRetriever Component = "Retriever"
	// ComponentOfLoader 文档加载器组件，用于从各种数据源加载文档
	ComponentOfLoader Component = "Loader"
	// ComponentOfTransformer 文档转换器组件，用于文档内容的处理和转换
	ComponentOfTransformer Component = "DocumentTransformer"
	// ComponentOfTool 工具组件，用于执行特定的功能操作
	ComponentOfTool Component = "Tool"
)
