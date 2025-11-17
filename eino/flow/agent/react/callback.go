package react

import (
	"github.com/favbox/eino/callbacks"
	template "github.com/favbox/eino/utils/callbacks"
)

// BuildAgentCallback 构建智能体回调处理器。
// 将模型回调处理器和工具回调处理器组合为完整的智能体回调。
//
// 使用示例：
//
//	callback := BuildAgentCallback(modelHandler, toolHandler)
//	agent, err := react.NewAgent(ctx, &react.AgentConfig{})
//	if err != nil {...}
//	agent.Generate(ctx, input, agent.WithComposeOptions(compose.WithCallbacks(callback)))
func BuildAgentCallback(modelHandler *template.ModelCallbackHandler, toolHandler *template.ToolCallbackHandler) callbacks.Handler {
	return template.NewHandlerHelper().ChatModel(modelHandler).Tool(toolHandler).Handler()
}
