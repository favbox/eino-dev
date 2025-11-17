/*
 * agent_tool.go - 智能体工具包装器，将智能体适配为工具接口
 *
 * 核心组件：
 *   - AgentTool: 将 Agent 适配为 BaseTool 接口的包装器
 *   - AgentToolOptions: 配置智能体工具的行为选项
 *   - 中断恢复机制: 通过检查点存储支持智能体工具的中断和恢复
 *
 * 设计特点：
 *   - 适配器模式: 将智能体封装为可在更大系统中复用的工具
 *   - 输入模式可配: 支持完整对话历史或单条请求作为输入
 *   - 中断恢复: 集成检查点存储，支持长时间运行的智能体中断后恢复
 *   - 历史重写: 转移对话历史时自动重写消息角色信息
 *   - 选项传递: 支持将特定智能体的运行选项通过工具选项传递
 *
 * 使用场景：
 *   - 在多智能体系统中将子智能体注册为父智能体的工具
 *   - 在工具链中嵌入智能体能力
 *   - 实现智能体间的任务转移和协作
 */

package adk

import (
	"context"
	"errors"
	"fmt"

	"github.com/bytedance/sonic"

	"github.com/favbox/eino/components/tool"
	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/schema"
)

// defaultAgentToolParam 定义智能体工具的默认输入参数模式。
// 包含一个名为 "request" 的字符串参数，用于传递待处理的请求。
var (
	defaultAgentToolParam = schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"request": {
			Desc:     "request to be processed",
			Required: true,
			Type:     schema.String,
		},
	})
)

// AgentToolOptions 配置智能体工具的行为选项
type AgentToolOptions struct {
	fullChatHistoryAsInput bool                // 是否使用完整对话历史作为输入
	agentInputSchema       *schema.ParamsOneOf // 自定义输入参数模式
}

// AgentToolOption 是配置智能体工具的选项函数类型
type AgentToolOption func(*AgentToolOptions)

// WithFullChatHistoryAsInput 配置智能体工具使用完整对话历史作为输入。
// 启用后，工具会将父智能体的完整消息历史传递给子智能体，而不是只传递当前请求。
func WithFullChatHistoryAsInput() AgentToolOption {
	return func(options *AgentToolOptions) {
		options.fullChatHistoryAsInput = true
	}
}

// WithAgentInputSchema 自定义智能体工具的输入参数模式。
// 默认使用 defaultAgentToolParam，自定义后可以接受更复杂的结构化输入。
func WithAgentInputSchema(schema *schema.ParamsOneOf) AgentToolOption {
	return func(options *AgentToolOptions) {
		options.agentInputSchema = schema
	}
}

// NewAgentTool 创建智能体工具，将 Agent 适配为 BaseTool 接口。
// 返回的工具可以在工具链或其他智能体中使用，支持配置输入模式和参数模式。
func NewAgentTool(_ context.Context, agent Agent, options ...AgentToolOption) tool.BaseTool {
	opts := &AgentToolOptions{}
	for _, opt := range options {
		opt(opts)
	}

	return &agentTool{
		agent:                  agent,
		fullChatHistoryAsInput: opts.fullChatHistoryAsInput,
		inputSchema:            opts.agentInputSchema,
	}
}

// agentTool 实现 BaseTool 接口，将智能体包装为工具
type agentTool struct {
	agent Agent // 被包装的智能体

	fullChatHistoryAsInput bool                // 是否使用完整对话历史
	inputSchema            *schema.ParamsOneOf // 输入参数模式
}

// Info 返回智能体工具的信息，包含智能体的名称、描述和输入参数模式
func (at *agentTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	param := at.inputSchema
	if param == nil {
		param = defaultAgentToolParam
	}

	return &schema.ToolInfo{
		Name:        at.agent.Name(ctx),
		Desc:        at.agent.Description(ctx),
		ParamsOneOf: param,
	}, nil
}

// InvokableRun 执行智能体工具，支持中断恢复和两种输入模式。
// 工作流程：
//  1. 检查是否有待恢复的中断数据
//  2. 如果有中断数据，从检查点恢复执行
//  3. 否则根据配置生成输入消息（完整历史或单条请求）
//  4. 使用 Runner 执行智能体并收集事件
//  5. 如果遇到中断，保存检查点数据到 State
//  6. 返回智能体的最终输出内容
func (at *agentTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// 检查是否有中断数据需要恢复
	var intData *agentToolInterruptInfo
	var bResume bool
	err := compose.ProcessState(ctx, func(ctx context.Context, s *State) error {
		toolCallID := compose.GetToolCallID(ctx)
		intData, bResume = s.AgentToolInterruptData[toolCallID]
		if bResume {
			delete(s.AgentToolInterruptData, toolCallID)
		}
		return nil
	})
	if err != nil {
		// 无法恢复，正常执行
		bResume = false
	}

	// 创建检查点存储和事件迭代器
	var ms *mockStore
	var iter *AsyncIterator[*AgentEvent]
	if bResume {
		// 从中断点恢复执行
		ms = newResumeStore(intData.Data)

		iter, err = newInvokableAgentToolRunner(at.agent, ms).Resume(ctx, mockCheckPointID, getOptionsByAgentName(at.agent.Name(ctx), opts)...)
		if err != nil {
			return "", err
		}
	} else {
		// 正常执行，准备输入消息
		ms = newEmptyStore()
		var input []Message
		if at.fullChatHistoryAsInput {
			// 使用完整对话历史作为输入
			history, err := getReactChatHistory(ctx, at.agent.Name(ctx))
			if err != nil {
				return "", err
			}

			input = history
		} else {
			// 使用单条请求作为输入
			if at.inputSchema == nil {
				// 默认输入模式，从 JSON 中提取 request 字段
				type request struct {
					Request string `json:"request"`
				}

				req := &request{}
				err = sonic.UnmarshalString(argumentsInJSON, req)
				if err != nil {
					return "", err
				}
				argumentsInJSON = req.Request
			}
			input = []Message{
				schema.UserMessage(argumentsInJSON),
			}
		}

		iter = newInvokableAgentToolRunner(at.agent, ms).Run(ctx, input, append(getOptionsByAgentName(at.agent.Name(ctx), opts), WithCheckPointID(mockCheckPointID))...)
	}

	// 收集所有事件，保留最后一个事件用于结果提取
	var lastEvent *AgentEvent
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			return "", event.Err
		}

		lastEvent = event
	}

	// 处理中断情况，保存检查点数据到 State
	if lastEvent != nil && lastEvent.Action != nil && lastEvent.Action.Interrupted != nil {
		data, existed, err_ := ms.Get(ctx, mockCheckPointID)
		if err_ != nil {
			return "", fmt.Errorf("failed to get interrupt info: %w", err_)
		}
		if !existed {
			return "", fmt.Errorf("interrupt has happened, but cannot find interrupt info")
		}
		err = compose.ProcessState(ctx, func(ctx context.Context, st *State) error {
			st.AgentToolInterruptData[compose.GetToolCallID(ctx)] = &agentToolInterruptInfo{
				LastEvent: lastEvent,
				Data:      data,
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("failed to save agent tool checkpoint to state: %w", err)
		}
		return "", compose.InterruptAndRerun
	}

	// 提取智能体的最终输出内容
	if lastEvent == nil {
		return "", errors.New("no event returned")
	}

	var ret string
	if lastEvent.Output != nil {
		if output := lastEvent.Output.MessageOutput; output != nil {
			if !output.IsStreaming {
				ret = output.Message.Content
			} else {
				msg, err := schema.ConcatMessageStream(output.MessageStream)
				if err != nil {
					return "", err
				}
				ret = msg.Content
			}
		}
	}

	return ret, nil
}

// agentToolOptions 包装智能体运行选项，用于将 AgentRunOption 转换为 tool.Option。
// 存储智能体名称和对应的运行选项，支持为特定智能体工具传递专用选项。
type agentToolOptions struct {
	agentName string           // 智能体名称
	opts      []AgentRunOption // 该智能体的运行选项
}

// withAgentToolOptions 创建智能体专用的工具选项。
// 将智能体名称和运行选项包装为 tool.Option，供后续提取使用。
func withAgentToolOptions(agentName string, opts []AgentRunOption) tool.Option {
	return tool.WrapImplSpecificOptFn(func(opt *agentToolOptions) {
		opt.agentName = agentName
		opt.opts = opts
	})
}

// getOptionsByAgentName 从工具选项列表中提取指定智能体的运行选项。
// 遍历所有工具选项，筛选出匹配智能体名称的选项并返回。
func getOptionsByAgentName(agentName string, opts []tool.Option) []AgentRunOption {
	var ret []AgentRunOption
	for _, opt := range opts {
		o := tool.GetImplSpecificOptions[agentToolOptions](nil, opt)
		if o != nil && o.agentName == agentName {
			ret = append(ret, o.opts...)
		}
	}
	return ret
}

// getReactChatHistory 获取 ReAct 对话历史并重写消息角色。
// 从 State 中提取消息历史，移除最后的工具调用消息，添加转移消息，
// 并将 Assistant 和 Tool 角色的消息重写为源智能体的视角。
func getReactChatHistory(ctx context.Context, destAgentName string) ([]Message, error) {
	var messages []Message
	var agentName string
	err := compose.ProcessState(ctx, func(ctx context.Context, st *State) error {
		messages = make([]Message, len(st.Messages)-1)
		copy(messages, st.Messages[:len(st.Messages)-1]) // 移除最后的 Assistant 消息（工具调用消息）
		agentName = st.AgentName
		return nil
	})

	a, t := GenTransferMessages(ctx, destAgentName)
	messages = append(messages, a, t)
	history := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == schema.System {
			continue
		}

		if msg.Role == schema.Assistant || msg.Role == schema.Tool {
			msg = rewriteMessage(msg, agentName)
		}

		history = append(history, msg)
	}

	return history, err
}

// newInvokableAgentToolRunner 创建非流式智能体工具运行器。
// 返回配置为非流式模式的 Runner，用于在工具调用中执行智能体。
func newInvokableAgentToolRunner(agent Agent, store compose.CheckPointStore) *Runner {
	return &Runner{
		a:               agent,
		enableStreaming: false,
		store:           store,
	}
}
