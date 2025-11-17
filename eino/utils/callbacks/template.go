package callbacks

import (
	"context"

	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/components"
	"github.com/favbox/eino/components/document"
	"github.com/favbox/eino/components/embedding"
	"github.com/favbox/eino/components/indexer"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/components/retriever"
	"github.com/favbox/eino/components/tool"
	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/schema"
)

// NewHandlerHelper 创建组件回调处理器构建器。
func NewHandlerHelper() *HandlerHelper {
	return &HandlerHelper{
		composeTemplates: map[components.Component]callbacks.Handler{},
	}
}

// HandlerHelper 回调处理器构建器。
// 用于为不同组件类型配置特定的回调处理器。
//
// 使用示例：
//
//	helper := NewHandlerHelper().
//		ChatModel(&ModelCallbackHandler{...}).
//		Tool(&ToolCallbackHandler{...}).
//		Handler()
//	runnable.Invoke(ctx, input, compose.WithCallbacks(helper))
type HandlerHelper struct {
	promptHandler      *PromptCallbackHandler
	chatModelHandler   *ModelCallbackHandler
	embeddingHandler   *EmbeddingCallbackHandler
	indexerHandler     *IndexerCallbackHandler
	retrieverHandler   *RetrieverCallbackHandler
	loaderHandler      *LoaderCallbackHandler
	transformerHandler *TransformerCallbackHandler
	toolHandler        *ToolCallbackHandler
	toolsNodeHandler   *ToolsNodeCallbackHandlers
	composeTemplates   map[components.Component]callbacks.Handler
}

// Handler 返回构建的回调处理器。
func (c *HandlerHelper) Handler() callbacks.Handler {
	return &handlerTemplate{c}
}

// Prompt 设置提示词组件的回调处理器。
func (c *HandlerHelper) Prompt(handler *PromptCallbackHandler) *HandlerHelper {
	c.promptHandler = handler
	return c
}

// ChatModel 设置聊天模型组件的回调处理器。
func (c *HandlerHelper) ChatModel(handler *ModelCallbackHandler) *HandlerHelper {
	c.chatModelHandler = handler
	return c
}

// Embedding 设置嵌入模型组件的回调处理器。
func (c *HandlerHelper) Embedding(handler *EmbeddingCallbackHandler) *HandlerHelper {
	c.embeddingHandler = handler
	return c
}

// Indexer 设置索引器组件的回调处理器。
func (c *HandlerHelper) Indexer(handler *IndexerCallbackHandler) *HandlerHelper {
	c.indexerHandler = handler
	return c
}

// Retriever 设置检索器组件的回调处理器。
func (c *HandlerHelper) Retriever(handler *RetrieverCallbackHandler) *HandlerHelper {
	c.retrieverHandler = handler
	return c
}

// Loader 设置加载器组件的回调处理器。
func (c *HandlerHelper) Loader(handler *LoaderCallbackHandler) *HandlerHelper {
	c.loaderHandler = handler
	return c
}

// Transformer 设置转换器组件的回调处理器。
func (c *HandlerHelper) Transformer(handler *TransformerCallbackHandler) *HandlerHelper {
	c.transformerHandler = handler
	return c
}

// Tool 设置工具组件的回调处理器。
func (c *HandlerHelper) Tool(handler *ToolCallbackHandler) *HandlerHelper {
	c.toolHandler = handler
	return c
}

// ToolsNode 设置工具节点的回调处理器。
func (c *HandlerHelper) ToolsNode(handler *ToolsNodeCallbackHandlers) *HandlerHelper {
	c.toolsNodeHandler = handler
	return c
}

// Graph 设置图编排的回调处理器。
func (c *HandlerHelper) Graph(handler callbacks.Handler) *HandlerHelper {
	c.composeTemplates[compose.ComponentOfGraph] = handler
	return c
}

// Chain 设置链编排的回调处理器。
func (c *HandlerHelper) Chain(handler callbacks.Handler) *HandlerHelper {
	c.composeTemplates[compose.ComponentOfChain] = handler
	return c
}

// Lambda 设置 Lambda 节点的回调处理器。
func (c *HandlerHelper) Lambda(handler callbacks.Handler) *HandlerHelper {
	c.composeTemplates[compose.ComponentOfLambda] = handler
	return c
}

// handlerTemplate 实现 callbacks.Handler 接口。
// 根据组件类型将回调事件分发到对应的处理器。
type handlerTemplate struct {
	*HandlerHelper
}

// OnStart 处理组件开始执行事件。
func (c *handlerTemplate) OnStart(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
	switch info.Component {
	case components.ComponentOfPrompt:
		return c.promptHandler.OnStart(ctx, info, prompt.ConvCallbackInput(input))
	case components.ComponentOfChatModel:
		return c.chatModelHandler.OnStart(ctx, info, model.ConvCallbackInput(input))
	case components.ComponentOfEmbedding:
		return c.embeddingHandler.OnStart(ctx, info, embedding.ConvCallbackInput(input))
	case components.ComponentOfIndexer:
		return c.indexerHandler.OnStart(ctx, info, indexer.ConvCallbackInput(input))
	case components.ComponentOfRetriever:
		return c.retrieverHandler.OnStart(ctx, info, retriever.ConvCallbackInput(input))
	case components.ComponentOfLoader:
		return c.loaderHandler.OnStart(ctx, info, document.ConvLoaderCallbackInput(input))
	case components.ComponentOfTransformer:
		return c.transformerHandler.OnStart(ctx, info, document.ConvTransformerCallbackInput(input))
	case components.ComponentOfTool:
		return c.toolHandler.OnStart(ctx, info, tool.ConvCallbackInput(input))
	case compose.ComponentOfToolsNode:
		return c.toolsNodeHandler.OnStart(ctx, info, convToolsNodeCallbackInput(input))
	case compose.ComponentOfGraph,
		compose.ComponentOfChain,
		compose.ComponentOfLambda:
		return c.composeTemplates[info.Component].OnStart(ctx, info, input)
	default:
		return ctx
	}
}

// OnEnd 处理组件执行结束事件。
func (c *handlerTemplate) OnEnd(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
	switch info.Component {
	case components.ComponentOfPrompt:
		return c.promptHandler.OnEnd(ctx, info, prompt.ConvCallbackOutput(output))
	case components.ComponentOfChatModel:
		return c.chatModelHandler.OnEnd(ctx, info, model.ConvCallbackOutput(output))
	case components.ComponentOfEmbedding:
		return c.embeddingHandler.OnEnd(ctx, info, embedding.ConvCallbackOutput(output))
	case components.ComponentOfIndexer:
		return c.indexerHandler.OnEnd(ctx, info, indexer.ConvCallbackOutput(output))
	case components.ComponentOfRetriever:
		return c.retrieverHandler.OnEnd(ctx, info, retriever.ConvCallbackOutput(output))
	case components.ComponentOfLoader:
		return c.loaderHandler.OnEnd(ctx, info, document.ConvLoaderCallbackOutput(output))
	case components.ComponentOfTransformer:
		return c.transformerHandler.OnEnd(ctx, info, document.ConvTransformerCallbackOutput(output))
	case components.ComponentOfTool:
		return c.toolHandler.OnEnd(ctx, info, tool.ConvCallbackOutput(output))
	case compose.ComponentOfToolsNode:
		return c.toolsNodeHandler.OnEnd(ctx, info, convToolsNodeCallbackOutput(output))
	case compose.ComponentOfGraph,
		compose.ComponentOfChain,
		compose.ComponentOfLambda:
		return c.composeTemplates[info.Component].OnEnd(ctx, info, output)
	default:
		return ctx
	}
}

// OnError 处理组件执行错误事件。
func (c *handlerTemplate) OnError(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
	switch info.Component {
	case components.ComponentOfPrompt:
		return c.promptHandler.OnError(ctx, info, err)
	case components.ComponentOfChatModel:
		return c.chatModelHandler.OnError(ctx, info, err)
	case components.ComponentOfEmbedding:
		return c.embeddingHandler.OnError(ctx, info, err)
	case components.ComponentOfIndexer:
		return c.indexerHandler.OnError(ctx, info, err)
	case components.ComponentOfRetriever:
		return c.retrieverHandler.OnError(ctx, info, err)
	case components.ComponentOfLoader:
		return c.loaderHandler.OnError(ctx, info, err)
	case components.ComponentOfTransformer:
		return c.transformerHandler.OnError(ctx, info, err)
	case components.ComponentOfTool:
		return c.toolHandler.OnError(ctx, info, err)
	case compose.ComponentOfToolsNode:
		return c.toolsNodeHandler.OnError(ctx, info, err)
	case compose.ComponentOfGraph,
		compose.ComponentOfChain,
		compose.ComponentOfLambda:
		return c.composeTemplates[info.Component].OnError(ctx, info, err)
	default:
		return ctx
	}
}

// OnStartWithStreamInput 处理组件接收流式输入的开始事件。
func (c *handlerTemplate) OnStartWithStreamInput(ctx context.Context, info *callbacks.RunInfo, input *schema.StreamReader[callbacks.CallbackInput]) context.Context {
	switch info.Component {
	// currently no components.Component receive stream as input
	case compose.ComponentOfGraph,
		compose.ComponentOfChain,
		compose.ComponentOfLambda:
		return c.composeTemplates[info.Component].OnStartWithStreamInput(ctx, info, input)
	default:
		return ctx
	}
}

// OnEndWithStreamOutput 处理组件输出流式结果的结束事件。
func (c *handlerTemplate) OnEndWithStreamOutput(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
	switch info.Component {
	case components.ComponentOfChatModel:
		return c.chatModelHandler.OnEndWithStreamOutput(ctx, info,
			schema.StreamReaderWithConvert(output, func(item callbacks.CallbackOutput) (*model.CallbackOutput, error) {
				return model.ConvCallbackOutput(item), nil
			}))
	case components.ComponentOfTool:
		return c.toolHandler.OnEndWithStreamOutput(ctx, info,
			schema.StreamReaderWithConvert(output, func(item callbacks.CallbackOutput) (*tool.CallbackOutput, error) {
				return tool.ConvCallbackOutput(item), nil
			}))
	case compose.ComponentOfToolsNode:
		return c.toolsNodeHandler.OnEndWithStreamOutput(ctx, info,
			schema.StreamReaderWithConvert(output, func(item callbacks.CallbackOutput) ([]*schema.Message, error) {
				return convToolsNodeCallbackOutput(item), nil
			}))
	case compose.ComponentOfGraph,
		compose.ComponentOfChain,
		compose.ComponentOfLambda:
		return c.composeTemplates[info.Component].OnEndWithStreamOutput(ctx, info, output)
	default:
		return ctx
	}
}

// Needed 检查指定时机是否需要执行回调。
func (c *handlerTemplate) Needed(ctx context.Context, info *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	if info == nil {
		return false
	}

	switch info.Component {
	case components.ComponentOfChatModel:
		if c.chatModelHandler != nil && c.chatModelHandler.Needed(ctx, info, timing) {
			return true
		}
	case components.ComponentOfEmbedding:
		if c.embeddingHandler != nil && c.embeddingHandler.Needed(ctx, info, timing) {
			return true
		}
	case components.ComponentOfIndexer:
		if c.indexerHandler != nil && c.indexerHandler.Needed(ctx, info, timing) {
			return true
		}
	case components.ComponentOfLoader:
		if c.loaderHandler != nil && c.loaderHandler.Needed(ctx, info, timing) {
			return true
		}
	case components.ComponentOfPrompt:
		if c.promptHandler != nil && c.promptHandler.Needed(ctx, info, timing) {
			return true
		}
	case components.ComponentOfRetriever:
		if c.retrieverHandler != nil && c.retrieverHandler.Needed(ctx, info, timing) {
			return true
		}
	case components.ComponentOfTool:
		if c.toolHandler != nil && c.toolHandler.Needed(ctx, info, timing) {
			return true
		}
	case components.ComponentOfTransformer:
		if c.transformerHandler != nil && c.transformerHandler.Needed(ctx, info, timing) {
			return true
		}
	case compose.ComponentOfToolsNode:
		if c.toolsNodeHandler != nil && c.toolsNodeHandler.Needed(ctx, info, timing) {
			return true
		}
	case compose.ComponentOfGraph,
		compose.ComponentOfChain,
		compose.ComponentOfLambda:
		handler := c.composeTemplates[info.Component]
		if handler != nil {
			checker, ok := handler.(callbacks.TimingChecker)
			if !ok || checker.Needed(ctx, info, timing) {
				return true
			}
		}
	default:
		return false
	}

	return false
}

// LoaderCallbackHandler 加载器组件的回调处理器。
type LoaderCallbackHandler struct {
	OnStart func(ctx context.Context, runInfo *callbacks.RunInfo, input *document.LoaderCallbackInput) context.Context
	OnEnd   func(ctx context.Context, runInfo *callbacks.RunInfo, output *document.LoaderCallbackOutput) context.Context
	OnError func(ctx context.Context, runInfo *callbacks.RunInfo, err error) context.Context
}

// Needed 检查指定时机是否需要执行回调。
func (ch *LoaderCallbackHandler) Needed(ctx context.Context, runInfo *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	switch timing {
	case callbacks.TimingOnStart:
		return ch.OnStart != nil
	case callbacks.TimingOnEnd:
		return ch.OnEnd != nil
	case callbacks.TimingOnError:
		return ch.OnError != nil
	default:
		return false
	}
}

// TransformerCallbackHandler 转换器组件的回调处理器。
type TransformerCallbackHandler struct {
	OnStart func(ctx context.Context, runInfo *callbacks.RunInfo, input *document.TransformerCallbackInput) context.Context
	OnEnd   func(ctx context.Context, runInfo *callbacks.RunInfo, output *document.TransformerCallbackOutput) context.Context
	OnError func(ctx context.Context, runInfo *callbacks.RunInfo, err error) context.Context
}

// Needed 检查指定时机是否需要执行回调。
func (ch *TransformerCallbackHandler) Needed(ctx context.Context, runInfo *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	switch timing {
	case callbacks.TimingOnStart:
		return ch.OnStart != nil
	case callbacks.TimingOnEnd:
		return ch.OnEnd != nil
	case callbacks.TimingOnError:
		return ch.OnError != nil
	default:
		return false
	}
}

// EmbeddingCallbackHandler 嵌入模型组件的回调处理器。
type EmbeddingCallbackHandler struct {
	OnStart func(ctx context.Context, runInfo *callbacks.RunInfo, input *embedding.CallbackInput) context.Context
	OnEnd   func(ctx context.Context, runInfo *callbacks.RunInfo, output *embedding.CallbackOutput) context.Context
	OnError func(ctx context.Context, runInfo *callbacks.RunInfo, err error) context.Context
}

// Needed 检查指定时机是否需要执行回调。
func (ch *EmbeddingCallbackHandler) Needed(ctx context.Context, runInfo *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	switch timing {
	case callbacks.TimingOnStart:
		return ch.OnStart != nil
	case callbacks.TimingOnEnd:
		return ch.OnEnd != nil
	case callbacks.TimingOnError:
		return ch.OnError != nil
	default:
		return false
	}
}

// IndexerCallbackHandler 索引器组件的回调处理器。
type IndexerCallbackHandler struct {
	OnStart func(ctx context.Context, runInfo *callbacks.RunInfo, input *indexer.CallbackInput) context.Context
	OnEnd   func(ctx context.Context, runInfo *callbacks.RunInfo, output *indexer.CallbackOutput) context.Context
	OnError func(ctx context.Context, runInfo *callbacks.RunInfo, err error) context.Context
}

// Needed 检查指定时机是否需要执行回调。
func (ch *IndexerCallbackHandler) Needed(ctx context.Context, runInfo *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	switch timing {
	case callbacks.TimingOnStart:
		return ch.OnStart != nil
	case callbacks.TimingOnEnd:
		return ch.OnEnd != nil
	case callbacks.TimingOnError:
		return ch.OnError != nil
	default:
		return false
	}
}

// ModelCallbackHandler 聊天模型组件的回调处理器。
type ModelCallbackHandler struct {
	OnStart               func(ctx context.Context, runInfo *callbacks.RunInfo, input *model.CallbackInput) context.Context
	OnEnd                 func(ctx context.Context, runInfo *callbacks.RunInfo, output *model.CallbackOutput) context.Context
	OnEndWithStreamOutput func(ctx context.Context, runInfo *callbacks.RunInfo, output *schema.StreamReader[*model.CallbackOutput]) context.Context
	OnError               func(ctx context.Context, runInfo *callbacks.RunInfo, err error) context.Context
}

// Needed 检查指定时机是否需要执行回调。
func (ch *ModelCallbackHandler) Needed(ctx context.Context, runInfo *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	switch timing {
	case callbacks.TimingOnStart:
		return ch.OnStart != nil
	case callbacks.TimingOnEnd:
		return ch.OnEnd != nil
	case callbacks.TimingOnError:
		return ch.OnError != nil
	case callbacks.TimingOnEndWithStreamOutput:
		return ch.OnEndWithStreamOutput != nil
	default:
		return false
	}
}

// PromptCallbackHandler 提示词组件的回调处理器。
type PromptCallbackHandler struct {
	OnStart func(ctx context.Context, runInfo *callbacks.RunInfo, input *prompt.CallbackInput) context.Context
	OnEnd   func(ctx context.Context, runInfo *callbacks.RunInfo, output *prompt.CallbackOutput) context.Context
	OnError func(ctx context.Context, runInfo *callbacks.RunInfo, err error) context.Context
}

// Needed 检查指定时机是否需要执行回调。
func (ch *PromptCallbackHandler) Needed(ctx context.Context, runInfo *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	switch timing {
	case callbacks.TimingOnStart:
		return ch.OnStart != nil
	case callbacks.TimingOnEnd:
		return ch.OnEnd != nil
	case callbacks.TimingOnError:
		return ch.OnError != nil
	default:
		return false
	}
}

// RetrieverCallbackHandler 检索器组件的回调处理器。
type RetrieverCallbackHandler struct {
	OnStart func(ctx context.Context, runInfo *callbacks.RunInfo, input *retriever.CallbackInput) context.Context
	OnEnd   func(ctx context.Context, runInfo *callbacks.RunInfo, output *retriever.CallbackOutput) context.Context
	OnError func(ctx context.Context, runInfo *callbacks.RunInfo, err error) context.Context
}

// Needed 检查指定时机是否需要执行回调。
func (ch *RetrieverCallbackHandler) Needed(ctx context.Context, runInfo *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	switch timing {
	case callbacks.TimingOnStart:
		return ch.OnStart != nil
	case callbacks.TimingOnEnd:
		return ch.OnEnd != nil
	case callbacks.TimingOnError:
		return ch.OnError != nil
	default:
		return false
	}
}

// ToolCallbackHandler 工具组件的回调处理器。
type ToolCallbackHandler struct {
	OnStart               func(ctx context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context
	OnEnd                 func(ctx context.Context, info *callbacks.RunInfo, output *tool.CallbackOutput) context.Context
	OnEndWithStreamOutput func(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[*tool.CallbackOutput]) context.Context
	OnError               func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context
}

// Needed 检查指定时机是否需要执行回调。
func (ch *ToolCallbackHandler) Needed(ctx context.Context, runInfo *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	switch timing {
	case callbacks.TimingOnStart:
		return ch.OnStart != nil
	case callbacks.TimingOnEnd:
		return ch.OnEnd != nil
	case callbacks.TimingOnEndWithStreamOutput:
		return ch.OnEndWithStreamOutput != nil
	case callbacks.TimingOnError:
		return ch.OnError != nil
	default:
		return false
	}
}

// ToolsNodeCallbackHandlers 工具节点的回调处理器。
type ToolsNodeCallbackHandlers struct {
	OnStart               func(ctx context.Context, info *callbacks.RunInfo, input *schema.Message) context.Context
	OnEnd                 func(ctx context.Context, info *callbacks.RunInfo, output []*schema.Message) context.Context
	OnEndWithStreamOutput func(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[[]*schema.Message]) context.Context
	OnError               func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context
}

// Needed 检查指定时机是否需要执行回调。
func (ch *ToolsNodeCallbackHandlers) Needed(ctx context.Context, runInfo *callbacks.RunInfo, timing callbacks.CallbackTiming) bool {
	switch timing {
	case callbacks.TimingOnStart:
		return ch.OnStart != nil
	case callbacks.TimingOnEnd:
		return ch.OnEnd != nil
	case callbacks.TimingOnEndWithStreamOutput:
		return ch.OnEndWithStreamOutput != nil
	case callbacks.TimingOnError:
		return ch.OnError != nil
	default:
		return false
	}
}

func convToolsNodeCallbackInput(src callbacks.CallbackInput) *schema.Message {
	switch t := src.(type) {
	case *schema.Message:
		return t
	default:
		return nil
	}
}

func convToolsNodeCallbackOutput(src callbacks.CallbackInput) []*schema.Message {
	switch t := src.(type) {
	case []*schema.Message:
		return t
	default:
		return nil
	}
}
