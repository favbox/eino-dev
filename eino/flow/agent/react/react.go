package react

import (
	"context"
	"io"

	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/flow/agent"
	"github.com/favbox/eino/schema"
)

// state ReAct 智能体的执行状态。
type state struct {
	// Messages 累积的消息历史。
	Messages []*schema.Message
	// ReturnDirectlyToolCallID 标记直接返回的工具调用 ID。
	ReturnDirectlyToolCallID string
}

func init() {
	schema.RegisterName[*state]("_eino_react_state")
}

// 图中的节点标识符。
const (
	nodeKeyTools = "tools"
	nodeKeyModel = "chat"
)

// MessageModifier 在调用模型前修改输入消息。
type MessageModifier func(ctx context.Context, input []*schema.Message) []*schema.Message

// AgentConfig ReAct 智能体配置。
type AgentConfig struct {
	// ToolCallingModel 支持工具调用的聊天模型。
	ToolCallingModel model.ToolCallingChatModel

	// ToolsConfig 工具节点配置。
	ToolsConfig compose.ToolsNodeConfig

	// MessageModifier 每次模型调用前的消息修饰器。
	// 适用于添加系统提示词等固定内容。
	MessageModifier MessageModifier

	// MessageRewriter 状态级消息重写器。
	// 处理累积的历史消息，适合上下文压缩等场景。
	// 注意：如果同时设置了 MessageModifier 和 MessageRewriter，MessageRewriter 会先执行。
	MessageRewriter MessageModifier

	// MaxStep ReAct 循环的最大执行步数，用于防止无限循环。
	// 默认值：12（节点数 + 10）。
	MaxStep int `json:"max_step"`

	// ToolReturnDirectly 配置直接返回结果的工具列表。
	ToolReturnDirectly map[string]struct{}

	// StreamToolCallChecker 检测流式输出中是否包含工具调用。
	// 默认实现适用于首个分块中包含工具调用的模型（如 OpenAI）。
	// 对于其他模型（如 Claude），需要提供自定义实现。
	StreamToolCallChecker func(ctx context.Context, modelOutput *schema.StreamReader[*schema.Message]) (bool, error)

	// GraphName 图名称标识符。
	// 默认值："ReActAgent"。
	GraphName string
	// ModelNodeName 模型节点名称。
	// 默认值："ChatModel"。
	ModelNodeName string
	// ToolsNodeName 工具节点名称。
	// 默认值："Tools"。
	ToolsNodeName string
}

// firstChunkStreamToolCallChecker 检查首个流式分块中是否包含工具调用。
func firstChunkStreamToolCallChecker(_ context.Context, sr *schema.StreamReader[*schema.Message]) (bool, error) {
	defer sr.Close()

	for {
		msg, err := sr.Recv()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}

		if len(msg.ToolCalls) > 0 {
			return true, nil
		}

		if len(msg.Content) == 0 {
			continue
		}

		return false, nil
	}
}

const (
	GraphName     = "ReActAgent"
	ModelNodeName = "ChatModel"
	ToolsNodeName = "Tools"
)

// SetReturnDirectly 设置当前工具调用为直接返回。
// 可在工具执行内部调用，优先级高于 AgentConfig.ToolReturnDirectly。
func SetReturnDirectly(ctx context.Context) error {
	return compose.ProcessState(ctx, func(ctx context.Context, s *state) error {
		s.ReturnDirectlyToolCallID = compose.GetToolCallID(ctx)
		return nil
	})
}

// Agent ReAct 智能体实现。
// 通过聊天模型处理用户消息，检测工具调用，执行工具，循环直到完成。
//
// 使用示例：
//
//	agent, err := react.NewAgent(ctx, &react.AgentConfig{
//		ToolCallingModel: myModel,
//		ToolsConfig: compose.ToolsNodeConfig{
//			Tools: []tool.BaseTool{searchTool, calculatorTool},
//		},
//	})
//	if err != nil {...}
//	msg, err := agent.Generate(ctx, []*schema.Message{
//		{Role: schema.User, Content: "how to build agent with eino"},
//	})
//	if err != nil {...}
//	println(msg.Content)
type Agent struct {
	runnable         compose.Runnable[[]*schema.Message, *schema.Message]
	graph            *compose.Graph[[]*schema.Message, *schema.Message]
	graphAddNodeOpts []compose.GraphAddNodeOpt
}

// NewAgent 创建 ReAct 智能体。
// 对于不在首个流式分块中输出工具调用的模型（如 Claude），需要自定义 StreamToolCallChecker。
func NewAgent(ctx context.Context, config *AgentConfig) (_ *Agent, err error) {
	var (
		chatModel       model.BaseChatModel
		toolsNode       *compose.ToolsNode
		toolInfos       []*schema.ToolInfo
		toolCallChecker = config.StreamToolCallChecker
		messageModifier = config.MessageModifier
	)

	graphName := GraphName
	if config.GraphName != "" {
		graphName = config.GraphName
	}

	modelNodeName := ModelNodeName
	if config.ModelNodeName != "" {
		modelNodeName = config.ModelNodeName
	}

	toolsNodeName := ToolsNodeName
	if config.ToolsNodeName != "" {
		toolsNodeName = config.ToolsNodeName
	}

	if toolCallChecker == nil {
		toolCallChecker = firstChunkStreamToolCallChecker
	}

	if toolInfos, err = genToolInfos(ctx, config.ToolsConfig); err != nil {
		return nil, err
	}

	if chatModel, err = agent.ChatModelWithTools(config.ToolCallingModel, toolInfos); err != nil {
		return nil, err
	}

	if toolsNode, err = compose.NewToolNode(ctx, &config.ToolsConfig); err != nil {
		return nil, err
	}

	graph := compose.NewGraph[[]*schema.Message, *schema.Message](compose.WithGenLocalState(func(ctx context.Context) *state {
		return &state{Messages: make([]*schema.Message, 0, config.MaxStep+1)}
	}))

	modelPreHandle := func(ctx context.Context, input []*schema.Message, state *state) ([]*schema.Message, error) {
		state.Messages = append(state.Messages, input...)

		if config.MessageRewriter != nil {
			state.Messages = config.MessageRewriter(ctx, state.Messages)
		}

		if messageModifier == nil {
			return state.Messages, nil
		}

		modifiedInput := make([]*schema.Message, len(state.Messages))
		copy(modifiedInput, state.Messages)
		return messageModifier(ctx, modifiedInput), nil
	}

	if err = graph.AddChatModelNode(nodeKeyModel, chatModel, compose.WithStatePreHandler(modelPreHandle), compose.WithNodeName(modelNodeName)); err != nil {
		return nil, err
	}

	if err = graph.AddEdge(compose.START, nodeKeyModel); err != nil {
		return nil, err
	}

	toolsNodePreHandle := func(ctx context.Context, input *schema.Message, state *state) (*schema.Message, error) {
		if input == nil {
			return state.Messages[len(state.Messages)-1], nil
		}
		state.Messages = append(state.Messages, input)
		state.ReturnDirectlyToolCallID = getReturnDirectlyToolCallID(input, config.ToolReturnDirectly)
		return input, nil
	}
	if err = graph.AddToolsNode(nodeKeyTools, toolsNode, compose.WithStatePreHandler(toolsNodePreHandle), compose.WithNodeName(toolsNodeName)); err != nil {
		return nil, err
	}

	modelPostBranchCondition := func(ctx context.Context, sr *schema.StreamReader[*schema.Message]) (endNode string, err error) {
		if isToolCall, err := toolCallChecker(ctx, sr); err != nil {
			return "", err
		} else if isToolCall {
			return nodeKeyTools, nil
		}
		return compose.END, nil
	}

	if err = graph.AddBranch(nodeKeyModel, compose.NewStreamGraphBranch(modelPostBranchCondition, map[string]bool{nodeKeyTools: true, compose.END: true})); err != nil {
		return nil, err
	}

	if err = buildReturnDirectly(graph); err != nil {
		return nil, err
	}

	compileOpts := []compose.GraphCompileOption{compose.WithMaxRunSteps(config.MaxStep), compose.WithNodeTriggerMode(compose.AnyPredecessor), compose.WithGraphName(graphName)}
	runnable, err := graph.Compile(ctx, compileOpts...)
	if err != nil {
		return nil, err
	}

	return &Agent{
		runnable:         runnable,
		graph:            graph,
		graphAddNodeOpts: []compose.GraphAddNodeOpt{compose.WithGraphCompileOptions(compileOpts...)},
	}, nil
}

// buildReturnDirectly 构建工具直接返回的处理逻辑。
func buildReturnDirectly(graph *compose.Graph[[]*schema.Message, *schema.Message]) (err error) {
	directReturn := func(ctx context.Context, msgs *schema.StreamReader[[]*schema.Message]) (*schema.StreamReader[*schema.Message], error) {
		return schema.StreamReaderWithConvert(msgs, func(msgs []*schema.Message) (*schema.Message, error) {
			var msg *schema.Message
			err = compose.ProcessState[*state](ctx, func(_ context.Context, state *state) error {
				for i := range msgs {
					if msgs[i] != nil && msgs[i].ToolCallID == state.ReturnDirectlyToolCallID {
						msg = msgs[i]
						return nil
					}
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			if msg == nil {
				return nil, schema.ErrNoValue
			}
			return msg, nil
		}), nil
	}

	nodeKeyDirectReturn := "direct_return"
	if err = graph.AddLambdaNode(nodeKeyDirectReturn, compose.TransformableLambda(directReturn)); err != nil {
		return err
	}

	// 此分支检查被调用的工具是否应该直接返回。它要么导向END，要么返回到ChatModel
	err = graph.AddBranch(nodeKeyTools, compose.NewStreamGraphBranch(func(ctx context.Context, msgsStream *schema.StreamReader[[]*schema.Message]) (endNode string, err error) {
		msgsStream.Close()

		err = compose.ProcessState[*state](ctx, func(_ context.Context, state *state) error {
			if len(state.ReturnDirectlyToolCallID) > 0 {
				endNode = nodeKeyDirectReturn
			} else {
				endNode = nodeKeyModel
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		return endNode, nil
	}, map[string]bool{nodeKeyModel: true, nodeKeyDirectReturn: true}))
	if err != nil {
		return err
	}

	return graph.AddEdge(nodeKeyDirectReturn, compose.END)
}

// genToolInfos 从工具配置中提取工具信息。
func genToolInfos(ctx context.Context, config compose.ToolsNodeConfig) ([]*schema.ToolInfo, error) {
	toolInfos := make([]*schema.ToolInfo, 0, len(config.Tools))
	for _, t := range config.Tools {
		tl, err := t.Info(ctx)
		if err != nil {
			return nil, err
		}

		toolInfos = append(toolInfos, tl)
	}

	return toolInfos, nil
}

// getReturnDirectlyToolCallID 获取直接返回的工具调用 ID。
func getReturnDirectlyToolCallID(input *schema.Message, toolReturnDirectly map[string]struct{}) string {
	if len(toolReturnDirectly) == 0 {
		return ""
	}

	for _, toolCall := range input.ToolCalls {
		if _, ok := toolReturnDirectly[toolCall.Function.Name]; ok {
			return toolCall.ID
		}
	}

	return ""
}

// Generate 生成回复消息。
func (r *Agent) Generate(ctx context.Context, input []*schema.Message, opts ...agent.AgentOption) (*schema.Message, error) {
	return r.runnable.Invoke(ctx, input, agent.GetComposeOptions(opts...)...)
}

// Stream 流式生成回复消息。
func (r *Agent) Stream(ctx context.Context, input []*schema.Message, opts ...agent.AgentOption) (output *schema.StreamReader[*schema.Message], err error) {
	return r.runnable.Stream(ctx, input, agent.GetComposeOptions(opts...)...)
}

// ExportGraph 导出智能体的底层图结构。
func (r *Agent) ExportGraph() (compose.AnyGraph, []compose.GraphAddNodeOpt) {
	return r.graph, r.graphAddNodeOpts
}
