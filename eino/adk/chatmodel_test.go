package adk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/favbox/eino/components/tool"
	"github.com/favbox/eino/compose"
	mockModel "github.com/favbox/eino/internal/mock/components/model"
	"github.com/favbox/eino/schema"
)

// TestChatModelAgentRun 测试 ChatModelAgent 的运行方法
func TestChatModelAgentRun(t *testing.T) {
	// 运行方法的基本测试
	t.Run("基本功能", func(t *testing.T) {
		ctx := context.Background()

		// 创建一个模拟聊天模型
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)

		// 设置模拟模型的预期值
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(schema.AssistantMessage("你好，我是一个 AI 助手。", nil), nil).
			Times(1)

		// 创建一个 ChatModelAgent
		agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
			Name:        "测试智能体",
			Description: "用于单元测试的测试智能体",
			Instruction: "你是一个有用的助手。",
			Model:       cm,
		})
		assert.NoError(t, err)
		assert.NotNil(t, agent)

		// 运行智能体
		input := &AgentInput{
			Messages: []Message{
				schema.UserMessage("你好，你是谁？"),
			},
		}
		iterator := agent.Run(ctx, input)
		assert.NotNil(t, iterator)

		// 从迭代器获取事件
		event, ok := iterator.Next()
		assert.True(t, ok)
		assert.NotNil(t, event)
		assert.Nil(t, event.Err)
		assert.NotNil(t, event.Output.MessageOutput)

		// 验证消息内容
		msg := event.Output.MessageOutput.Message
		assert.NotNil(t, msg)
		assert.Equal(t, "你好，我是一个 AI 助手。", msg.Content)

		// 没有更多事件
		_, ok = iterator.Next()
		assert.False(t, ok)
	})

	// 流式输出测试
	t.Run("流式输出", func(t *testing.T) {
		ctx := context.Background()

		// 创建一个模拟聊天模型
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)

		// 为模拟响应创建一个流读取器
		sr := schema.StreamReaderFromArray([]*schema.Message{
			schema.AssistantMessage("你好", nil),
			schema.AssistantMessage("，我是", nil),
			schema.AssistantMessage("一个 AI 助手。", nil),
		})

		// 设置模拟模型的预期值
		cm.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(sr, nil).
			Times(1)

		// 创建一个 ChatModelAgent
		agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
			Name:        "测试智能体",
			Description: "用于单元测试的测试智能体",
			Instruction: "你是一个有用的助手。",
			Model:       cm,
		})
		assert.NoError(t, err)
		assert.NotNil(t, agent)

		// 运行智能体
		input := &AgentInput{
			Messages:        []Message{schema.UserMessage("你好，你是谁？")},
			EnableStreaming: true,
		}
		iterator := agent.Run(ctx, input)
		assert.NotNil(t, iterator)

		// 从迭代器获取事件
		event, ok := iterator.Next()
		assert.True(t, ok)
		assert.NotNil(t, event)
		assert.Nil(t, event.Err)
		assert.NotNil(t, event.Output)
		assert.NotNil(t, event.Output.MessageOutput)
		assert.True(t, event.Output.MessageOutput.IsStreaming)

		// 没有更多事件
		_, ok = iterator.Next()
		assert.False(t, ok)
	})

	// 测试错误处理
	t.Run("错误处理", func(t *testing.T) {
		ctx := context.Background()

		// 创建一个模拟聊天模型
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)

		// 设置模拟模型的预期值
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, errors.New("模型错误")).
			Times(1)

		// 创建一个 ChatModelAgent
		agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
			Name:        "测试智能体",
			Description: "用于单元测试的测试智能体",
			Instruction: "你是一个有用的助手。",
			Model:       cm,
		})
		assert.NoError(t, err)
		assert.NotNil(t, agent)

		// 运行智能体
		input := &AgentInput{
			Messages: []Message{
				schema.UserMessage("你好，你是谁？"),
			},
		}
		iterator := agent.Run(ctx, input)
		assert.NotNil(t, iterator)

		// 从迭代器获取事件应包含错误
		event, ok := iterator.Next()
		assert.True(t, ok)
		assert.NotNil(t, event)
		assert.NotNil(t, event.Err)
		assert.Contains(t, event.Err.Error(), "模型错误")

		// 没有更多事件
		_, ok = iterator.Next()
		assert.False(t, ok)
	})

	// 测试 WithTools
	t.Run("WithTools", func(t *testing.T) {
		ctx := context.Background()

		// 创建一个用于测试的假工具
		fakeTool := &fakeToolForTest{
			tarCount: 1,
		}

		info, err := fakeTool.Info(ctx)
		assert.NoError(t, err)

		// 创建一个模拟聊天模型
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)

		// 设置模拟模型的预期值
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(schema.AssistantMessage("使用工具",
				[]schema.ToolCall{
					{
						ID: "tool-call-1",
						Function: schema.FunctionCall{
							Name:      info.Name,
							Arguments: `{"name": "test user"}`,
						},
					},
				},
			), nil).
			Times(1)
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(schema.AssistantMessage("任务完成", nil), nil).
			Times(1)
		cm.EXPECT().WithTools(gomock.Any()).
			Return(cm, nil).AnyTimes()

		// 创建一个 ChatModelAgent
		agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
			Name:        "测试智能体",
			Description: "用于单元测试的测试智能体",
			Instruction: "你是一个有用的助手。",
			Model:       cm,
			ToolsConfig: ToolsConfig{
				ToolsNodeConfig: compose.ToolsNodeConfig{
					Tools: []tool.BaseTool{fakeTool},
				},
			},
		})
		assert.NoError(t, err)
		assert.NotNil(t, agent)

		// 运行智能体
		input := &AgentInput{
			Messages: []Message{
				schema.UserMessage("使用测试工具"),
			},
		}
		iterator := agent.Run(ctx, input)
		assert.NotNil(t, iterator)

		// 从迭代器获取事件
		// 第1个事件应当是带有工具调用的模型输出
		event1, ok := iterator.Next()
		assert.True(t, ok)
		assert.NotNil(t, event1)
		assert.Nil(t, event1.Err)
		assert.NotNil(t, event1.Output.MessageOutput)
		assert.Equal(t, schema.Assistant, event1.Output.MessageOutput.Role)

		// 第2个事件应当是工具输出
		event2, ok := iterator.Next()
		assert.True(t, ok)
		assert.NotNil(t, event2)
		assert.Nil(t, event2.Err)
		assert.NotNil(t, event2.Output.MessageOutput)
		assert.Equal(t, schema.Tool, event2.Output.MessageOutput.Role)

		// 第3个事件应当是最终模型输出
		event3, ok := iterator.Next()
		assert.True(t, ok)
		assert.NotNil(t, event3)
		assert.Nil(t, event3.Err)
		assert.NotNil(t, event3.Output.MessageOutput)
		assert.Equal(t, schema.Assistant, event3.Output.MessageOutput.Role)

		// 没有更多事件
		_, ok = iterator.Next()
		assert.False(t, ok)
	})
}

// TestExitTool 测试退出工具的功能
func TestExitTool(t *testing.T) {
	ctx := context.Background()

	// 创建一个模拟控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建一个模拟聊天模型
	cm := mockModel.NewMockToolCallingChatModel(ctrl)

	// 设置模拟模型的预期值
	// 第1次调用：模型生成带有退出工具调用的消息
	cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(schema.AssistantMessage("我将携带最终结果退出",
			[]schema.ToolCall{
				{
					ID: "tool-call-1",
					Function: schema.FunctionCall{
						Name:      "exit",
						Arguments: `{"final_result": "这是最终结果"}`,
					},
				},
			}), nil).
		Times(1)
	cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()

	// 创建一个 ChatModelAgent
	agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
		Name:        "测试智能体",
		Description: "用于单元测试的测试智能体",
		Instruction: "你是一个有用的助手。",
		Model:       cm,
		Exit:        &ExitTool{},
	})
	assert.NoError(t, err)
	assert.NotNil(t, agent)

	// 运行智能体
	input := &AgentInput{
		Messages: []Message{
			schema.UserMessage("请退出并给出最终结果"),
		},
	}
	iterator := agent.Run(ctx, input)
	assert.NotNil(t, iterator)

	// 第1个事件：带有工具调用的模型输出
	event1, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event1)
	assert.Nil(t, event1.Err)
	assert.NotNil(t, event1.Output)
	assert.NotNil(t, event1.Output.MessageOutput)
	assert.Equal(t, schema.Assistant, event1.Output.MessageOutput.Role)

	// 第2个事件：工具输出（退出）
	event2, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event2)
	assert.Nil(t, event2.Err)
	assert.NotNil(t, event2.Output)
	assert.NotNil(t, event2.Output.MessageOutput)
	assert.Equal(t, schema.Tool, event2.Output.MessageOutput.Role)

	// 验证消息内容
	assert.NotNil(t, event2.Action)
	assert.True(t, event2.Action.Exit)

	// 验证最终结果
	assert.Equal(t, "这是最终结果", event2.Output.MessageOutput.Message.Content)

	// 没有更多事件
	_, ok = iterator.Next()
	assert.False(t, ok)
}

func TestParallelReturnDirectlyToolCall(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建一个模拟聊天模型
	cm := mockModel.NewMockToolCallingChatModel(ctrl)

	// 设置模拟模型的预期值
	// 第1次调用：模型生成带有退出工具调用的消息
	cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(schema.AssistantMessage("我将携带最终结果退出",
			[]schema.ToolCall{
				{
					ID:       "tool-call-1",
					Function: schema.FunctionCall{Name: "tool1"},
				},
				{
					ID:       "tool-call-2",
					Function: schema.FunctionCall{Name: "tool2"},
				},
				{
					ID:       "tool-call-3",
					Function: schema.FunctionCall{Name: "tool3"},
				},
			}), nil).
		Times(1)
	cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()

	// 创建一个 ChatModelAgent
	agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
		Name:        "测试智能体",
		Description: "用于单元测试的测试智能体",
		Instruction: "你是一个有用的助手。",
		Model:       cm,
		ToolsConfig: ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{
					&myTool{name: "tool1", desc: "tool1", waitTime: time.Millisecond},
					&myTool{name: "tool2", desc: "tool2", waitTime: 10 * time.Millisecond},
					&myTool{name: "tool3", desc: "tool3", waitTime: 100 * time.Millisecond},
				},
			},
			ReturnDirectly: map[string]bool{
				"tool1": true,
			},
		},
	})
	assert.NoError(t, err)
	assert.NotNil(t, agent)

	r := NewRunner(ctx, RunnerConfig{
		Agent:           agent,
		EnableStreaming: false,
		CheckPointStore: nil,
	})
	iter := r.Query(ctx, "")
	times := 0
	for {
		e, ok := iter.Next()
		if !ok {
			assert.Equal(t, 4, times)
			break
		}
		if times == 3 {
			assert.Equal(t, "tool1", e.Output.MessageOutput.Message.ToolName)
		}
		times++
	}
}

type myTool struct {
	name     string
	desc     string
	waitTime time.Duration
}

func (m *myTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: m.name,
		Desc: m.desc,
	}, nil
}

func (m *myTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	time.Sleep(m.waitTime)
	return "success", nil
}
