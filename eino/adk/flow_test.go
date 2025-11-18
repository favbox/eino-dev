/*
 * flow_test.go - flowAgent 多智能体编排功能测试
 *
 * 测试内容：
 *   - TransferToAgent: 测试智能体间的任务转移功能
 *   - 父子智能体关系: 验证 SetSubAgents 建立的层次结构
 *   - 事件流: 验证转移过程中产生的事件顺序和内容
 *
 * 测试场景：
 *   - 父智能体调用 TransferToAgent 工具将任务转移给子智能体
 *   - 子智能体接收任务并生成响应
 *   - 验证整个转移过程的事件流
 *
 * 核心验证点：
 *   - 父子关系正确建立（parentAgent.subAgents, childAgent.parentAgent）
 *   - TransferToAgent 工具调用正确执行
 *   - 事件顺序：父智能体输出 → 工具调用 → 子智能体输出
 *   - 转移动作包含正确的目标智能体名称
 */

package adk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	mockModel "github.com/favbox/eino/internal/mock/components/model"
	"github.com/favbox/eino/schema"
)

// TestTransferToAgent 测试智能体转移功能。
// 验证父智能体通过 TransferToAgent 工具将任务转移给子智能体的完整流程。
//
// 测试流程：
//  1. 创建父子两个 ChatModelAgent
//  2. 通过 SetSubAgents 建立父子关系
//  3. 父智能体调用 TransferToAgent 工具
//  4. 子智能体接收任务并响应
//  5. 验证事件流和转移动作
func TestTransferToAgent(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 为父子智能体创建 mock 模型
	parentModel := mockModel.NewMockToolCallingChatModel(ctrl)
	childModel := mockModel.NewMockToolCallingChatModel(ctrl)

	// 设置父模型的期望行为
	// 第一次调用：父模型生成包含 TransferToAgent 工具调用的消息
	parentModel.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(schema.AssistantMessage("I'll transfer this to the child agent",
			[]schema.ToolCall{
				{
					ID: "tool-call-1",
					Function: schema.FunctionCall{
						Name:      TransferToAgentToolName,
						Arguments: `{"agent_name": "ChildAgent"}`,
					},
				},
			}), nil).
		Times(1)

	// 设置子模型的期望行为
	// 第二次调用：子模型生成响应消息（无工具调用）
	childModel.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(schema.AssistantMessage("Hello from child agent", nil), nil).
		Times(1)

	// 两个模型都需要实现 WithTools 方法（ReAct 模式需要）
	parentModel.EXPECT().WithTools(gomock.Any()).Return(parentModel, nil).AnyTimes()
	childModel.EXPECT().WithTools(gomock.Any()).Return(childModel, nil).AnyTimes()

	// 创建父智能体
	parentAgent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
		Name:        "ParentAgent",
		Description: "Parent agent that will transfer to child",
		Instruction: "You are a parent agent.",
		Model:       parentModel,
	})
	assert.NoError(t, err)
	assert.NotNil(t, parentAgent)

	// 创建子智能体
	childAgent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
		Name:        "ChildAgent",
		Description: "Child agent that handles specific tasks",
		Instruction: "You are a child agent.",
		Model:       childModel,
	})
	assert.NoError(t, err)
	assert.NotNil(t, childAgent)

	// 建立父子关系：将 childAgent 设置为 parentAgent 的子智能体
	// SetSubAgents 返回的 flowAgent 是经过装饰的智能体，包含多智能体编排能力
	flowAgent, err := SetSubAgents(ctx, parentAgent, []Agent{childAgent})
	assert.NoError(t, err)
	assert.NotNil(t, flowAgent)

	// 验证父子关系已正确建立
	assert.NotNil(t, parentAgent.subAgents)  // 父智能体有子智能体列表
	assert.NotNil(t, childAgent.parentAgent) // 子智能体知道自己的父智能体

	// 运行父智能体（实际上运行的是 flowAgent）
	input := &AgentInput{
		Messages: []Message{
			schema.UserMessage("Please transfer this to the child agent"),
		},
	}
	iterator := flowAgent.Run(ctx, input)
	assert.NotNil(t, iterator)

	// ==================== 事件流验证 ====================

	// 事件 1：父智能体的 ChatModel 输出（包含 TransferToAgent 工具调用）
	event1, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event1)
	assert.Nil(t, event1.Err)
	assert.NotNil(t, event1.Output)
	assert.NotNil(t, event1.Output.MessageOutput)
	assert.Equal(t, schema.Assistant, event1.Output.MessageOutput.Role) // 角色：助手

	// 事件 2：TransferToAgent 工具执行结果
	// Role 为 Tool，表示这是工具调用的输出
	event2, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event2)
	assert.Nil(t, event2.Err)
	assert.NotNil(t, event2.Output)
	assert.NotNil(t, event2.Output.MessageOutput)
	assert.Equal(t, schema.Tool, event2.Output.MessageOutput.Role) // 角色：工具

	// 验证转移动作：包含 TransferToAgent 动作，目标智能体为 ChildAgent
	assert.NotNil(t, event2.Action)
	assert.NotNil(t, event2.Action.TransferToAgent)
	assert.Equal(t, "ChildAgent", event2.Action.TransferToAgent.DestAgentName)

	// 事件 3：子智能体的 ChatModel 输出（最终响应）
	// 任务已转移到子智能体，由子智能体生成回复
	event3, ok := iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event3)
	assert.Nil(t, event3.Err)
	assert.NotNil(t, event3.Output)
	assert.NotNil(t, event3.Output.MessageOutput)
	assert.Equal(t, schema.Assistant, event3.Output.MessageOutput.Role) // 角色：助手

	// 验证子智能体的消息内容
	msg := event3.Output.MessageOutput.Message
	assert.NotNil(t, msg)
	assert.Equal(t, "Hello from child agent", msg.Content)

	// 验证没有更多事件（流程结束）
	_, ok = iterator.Next()
	assert.False(t, ok)
}
