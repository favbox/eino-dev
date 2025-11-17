/*
 * react.go - ReAct 模式核心实现，推理-行动循环
 *
 * 核心组件：
 *   - State: ReAct 状态管理，维护消息历史和工具调用状态
 *   - reactGraph: 基于 compose.Graph 构建的 ReAct 执行图
 *   - 工具调用分支: 根据模型输出决定是否执行工具或结束
 *
 * 设计特点：
 *   - ReAct 循环: ChatModel 推理 -> 工具调用检测 -> 工具执行 -> 继续推理
 *   - 直接返回工具: 支持特定工具调用后立即返回结果，不继续循环
 *   - 中断恢复: 支持中断智能体工具的执行并从检查点恢复
 *   - 迭代限制: 通过最大迭代次数防止无限循环
 *   - 状态管理: 通过 compose.ProcessState 管理共享状态
 *
 * ReAct 图结构：
 *   START -> ChatModel -> Branch(检测工具调用)
 *                           |
 *                           +-> END (无工具调用)
 *                           |
 *                           +-> ToolNode -> Branch(检测直接返回)
 *                                              |
 *                                              +-> END (直接返回工具)
 *                                              |
 *                                              +-> ChatModel (继续循环)
 */

package adk

import (
	"context"
	"errors"
	"io"

	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/schema"
)

// ErrExceedMaxIterations 表示 ReAct 循环超过最大迭代次数的错误
var ErrExceedMaxIterations = errors.New("exceeds max iterations")

// State 表示 ReAct 执行过程中的状态。
// 维护消息历史、工具调用状态、中断信息和迭代控制。
type State struct {
	// Messages 存储当前对话的所有消息历史
	Messages []Message

	// HasReturnDirectly 标记是否有直接返回工具被调用
	HasReturnDirectly bool
	// ReturnDirectlyToolCallID 记录触发直接返回的工具调用 ID
	ReturnDirectlyToolCallID string

	// ToolGenActions 存储工具生成的动作，key 为工具名称
	ToolGenActions map[string]*AgentAction

	// AgentName 当前智能体的名称
	AgentName string

	// AgentToolInterruptData 存储智能体工具的中断数据，key 为工具调用 ID
	AgentToolInterruptData map[string] /*tool call id*/ *agentToolInterruptInfo

	// RemainingIterations 剩余可执行的迭代次数，用于防止无限循环
	RemainingIterations int
}

// agentToolInterruptInfo 存储智能体工具中断时的状态信息
type agentToolInterruptInfo struct {
	LastEvent *AgentEvent // 中断前的最后一个事件
	Data      []byte      // 序列化的中断数据
}

// SendToolGenAction 将工具生成的动作存储到 ReAct 状态中。
// 通过 compose.ProcessState 安全地修改共享状态，供后续事件处理使用。
func SendToolGenAction(ctx context.Context, toolName string, action *AgentAction) error {
	return compose.ProcessState(ctx, func(ctx context.Context, st *State) error {
		st.ToolGenActions[toolName] = action

		return nil
	})
}

// popToolGenAction 从 ReAct 状态中取出并删除指定工具的动作。
// 返回动作对象，如果不存在则返回 nil。
func popToolGenAction(ctx context.Context, toolName string) *AgentAction {
	var action *AgentAction
	err := compose.ProcessState(ctx, func(ctx context.Context, st *State) error {
		action = st.ToolGenActions[toolName]
		if action != nil {
			delete(st.ToolGenActions, toolName)
		}

		return nil
	})

	if err != nil {
		panic("impossible")
	}

	return action
}

// reactConfig 配置 ReAct 图的构建参数
type reactConfig struct {
	model model.ToolCallingChatModel // 支持工具调用的聊天模型

	toolsConfig *compose.ToolsNodeConfig // 工具节点配置

	toolsReturnDirectly map[string]bool // 直接返回工具映射，key 为工具名称

	agentName string // 智能体名称

	maxIterations int // 最大迭代次数

	beforeChatModel, afterChatModel []func(context.Context, *ChatModelAgentState) error // ChatModel 调用前后的钩子函数
}

// genToolInfos 生成工具信息列表。
// 遍历配置中的所有工具，调用 Info 方法获取工具元信息。
func genToolInfos(ctx context.Context, config *compose.ToolsNodeConfig) ([]*schema.ToolInfo, error) {
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

// reactGraph 是 ReAct 执行图的类型别名，输入为消息列表，输出为单条消息
type reactGraph = *compose.Graph[[]Message, Message]

// sToolNodeOutput 是工具节点流式输出的类型别名
type sToolNodeOutput = *schema.StreamReader[[]Message]

// sGraphOutput 是 ReAct 图流式输出的类型别名
type sGraphOutput = MessageStream

// getReturnDirectlyToolCallID 从状态中获取直接返回工具的调用 ID。
// 返回工具调用 ID 和是否存在直接返回工具的标志。
func getReturnDirectlyToolCallID(ctx context.Context) (string, bool) {
	var toolCallID string
	var hasReturnDirectly bool
	handler := func(_ context.Context, st *State) error {
		toolCallID = st.ReturnDirectlyToolCallID
		hasReturnDirectly = st.HasReturnDirectly
		return nil
	}

	_ = compose.ProcessState(ctx, handler)

	return toolCallID, hasReturnDirectly
}

// newReact 构建 ReAct 执行图。
// 创建包含 ChatModel 节点、ToolNode 节点和条件分支的有向图，实现推理-行动循环。
// 图结构支持工具调用检测、直接返回工具处理和迭代限制。
func newReact(ctx context.Context, config *reactConfig) (reactGraph, error) {
	genState := func(ctx context.Context) *State {
		return &State{
			ToolGenActions:         map[string]*AgentAction{},
			AgentName:              config.agentName,
			AgentToolInterruptData: make(map[string]*agentToolInterruptInfo),
			RemainingIterations: func() int {
				if config.maxIterations <= 0 {
					return 20
				}
				return config.maxIterations
			}(),
		}
	}

	const (
		chatModel_ = "ChatModel"
		toolNode_  = "ToolNode"
	)

	g := compose.NewGraph[[]Message, Message](compose.WithGenLocalState(genState))

	toolsInfo, err := genToolInfos(ctx, config.toolsConfig)
	if err != nil {
		return nil, err
	}

	chatModel, err := config.model.WithTools(toolsInfo)
	if err != nil {
		return nil, err
	}

	toolsNode, err := compose.NewToolNode(ctx, config.toolsConfig)
	if err != nil {
		return nil, err
	}

	// modelPreHandle 在 ChatModel 调用前执行，检查迭代次数并执行前置钩子
	modelPreHandle := func(ctx context.Context, input []Message, st *State) ([]Message, error) {
		if st.RemainingIterations <= 0 {
			return nil, ErrExceedMaxIterations
		}
		st.RemainingIterations--

		s := &ChatModelAgentState{Messages: append(st.Messages, input...)}
		for _, b := range config.beforeChatModel {
			err = b(ctx, s)
			if err != nil {
				return nil, err
			}
		}
		st.Messages = s.Messages

		return st.Messages, nil
	}
	// modelPostHandle 在 ChatModel 调用后执行，更新消息历史并执行后置钩子
	modelPostHandle := func(ctx context.Context, input Message, st *State) (Message, error) {
		s := &ChatModelAgentState{Messages: append(st.Messages, input)}
		for _, a := range config.afterChatModel {
			err = a(ctx, s)
			if err != nil {
				return nil, err
			}
		}
		st.Messages = s.Messages
		return input, nil
	}
	_ = g.AddChatModelNode(chatModel_, chatModel,
		compose.WithStatePreHandler(modelPreHandle), compose.WithStatePostHandler(modelPostHandle), compose.WithNodeName(chatModel_))

	// toolPreHandle 在工具节点执行前检测是否有直接返回工具被调用
	toolPreHandle := func(ctx context.Context, input Message, st *State) (Message, error) {
		input = st.Messages[len(st.Messages)-1]
		if len(config.toolsReturnDirectly) > 0 {
			for i := range input.ToolCalls {
				toolName := input.ToolCalls[i].Function.Name
				if config.toolsReturnDirectly[toolName] {
					st.ReturnDirectlyToolCallID = input.ToolCalls[i].ID
					st.HasReturnDirectly = true
				}
			}
		}

		return input, nil
	}

	_ = g.AddToolsNode(toolNode_, toolsNode,
		compose.WithStatePreHandler(toolPreHandle), compose.WithNodeName(toolNode_))

	_ = g.AddEdge(compose.START, chatModel_)

	// toolCallCheck 检查 ChatModel 输出是否包含工具调用，决定下一步路由
	toolCallCheck := func(ctx context.Context, sMsg MessageStream) (string, error) {
		defer sMsg.Close()
		for {
			chunk, err_ := sMsg.Recv()
			if err_ != nil {
				if err_ == io.EOF {
					return compose.END, nil
				}

				return "", err_
			}

			if len(chunk.ToolCalls) > 0 {
				return toolNode_, nil
			}
		}
	}
	branch := compose.NewStreamGraphBranch(toolCallCheck, map[string]bool{compose.END: true, toolNode_: true})
	_ = g.AddBranch(chatModel_, branch)

	if len(config.toolsReturnDirectly) == 0 {
		_ = g.AddEdge(toolNode_, chatModel_)
	} else {
		const (
			toolNodeToEndConverter = "ToolNodeToEndConverter"
		)

		// cvt 转换工具节点输出，只保留直接返回工具的结果
		cvt := func(ctx context.Context, sToolCallMessages sToolNodeOutput) (sGraphOutput, error) {
			id, _ := getReturnDirectlyToolCallID(ctx)

			return schema.StreamReaderWithConvert(sToolCallMessages,
				func(in []Message) (Message, error) {

					for _, chunk := range in {
						if chunk != nil && chunk.ToolCallID == id {
							return chunk, nil
						}
					}

					return nil, schema.ErrNoValue
				}), nil
		}

		_ = g.AddLambdaNode(toolNodeToEndConverter, compose.TransformableLambda(cvt),
			compose.WithNodeName(toolNodeToEndConverter))
		_ = g.AddEdge(toolNodeToEndConverter, compose.END)

		// checkReturnDirect 检查是否触发直接返回，决定是结束还是继续循环
		checkReturnDirect := func(ctx context.Context,
			sToolCallMessages sToolNodeOutput) (string, error) {

			_, ok := getReturnDirectlyToolCallID(ctx)

			if ok {
				return toolNodeToEndConverter, nil
			}

			return chatModel_, nil
		}

		branch = compose.NewStreamGraphBranch(checkReturnDirect,
			map[string]bool{toolNodeToEndConverter: true, chatModel_: true})
		_ = g.AddBranch(toolNode_, branch)
	}

	return g, nil
}
