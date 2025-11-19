package supervisor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/favbox/eino/adk"
	mockAdk "github.com/favbox/eino/internal/mock/adk"
	"github.com/favbox/eino/schema"
)

// TestNewSupervisor 测试 New 函数创建监督者多智能体系统的功能。
func TestNewSupervisor(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 智能体
	supervisorAgent := mockAdk.NewMockAgent(ctrl)
	subAgent1 := mockAdk.NewMockAgent(ctrl)
	subAgent2 := mockAdk.NewMockAgent(ctrl)

	// 设置智能体名称期望
	supervisorAgent.EXPECT().Name(gomock.Any()).Return("SupervisorAgent").AnyTimes()
	subAgent1.EXPECT().Name(gomock.Any()).Return("SubAgent1").AnyTimes()
	subAgent2.EXPECT().Name(gomock.Any()).Return("SubAgent2").AnyTimes()

	// 设置监督者第一次运行：转移到 SubAgent1
	aMsg, tMsg := adk.GenTransferMessages(ctx, "SubAgent1")
	i, g := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	g.Send(adk.EventFromMessage(aMsg, nil, schema.Assistant, ""))
	event := adk.EventFromMessage(tMsg, nil, schema.Tool, tMsg.ToolName)
	event.Action = &adk.AgentAction{TransferToAgent: &adk.TransferToAgentAction{DestAgentName: "SubAgent1"}}
	g.Send(event)
	g.Close()
	supervisorAgent.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).Return(i).Times(1)

	// 设置 SubAgent1 的运行输出
	i, g = adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	subAgent1Msg := schema.AssistantMessage("SubAgent1", nil)
	g.Send(adk.EventFromMessage(subAgent1Msg, nil, schema.Assistant, ""))
	g.Close()
	subAgent1.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).Return(i).Times(1)

	// 设置监督者第二次运行：转移到 SubAgent2
	aMsg, tMsg = adk.GenTransferMessages(ctx, "SubAgent2 message")
	i, g = adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	g.Send(adk.EventFromMessage(aMsg, nil, schema.Assistant, ""))
	event = adk.EventFromMessage(tMsg, nil, schema.Tool, tMsg.ToolName)
	event.Action = &adk.AgentAction{TransferToAgent: &adk.TransferToAgentAction{DestAgentName: "SubAgent2"}}
	g.Send(event)
	g.Close()
	supervisorAgent.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).Return(i).Times(1)

	// 设置 SubAgent2 的运行输出
	i, g = adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	subAgent2Msg := schema.AssistantMessage("SubAgent2 message", nil)
	g.Send(adk.EventFromMessage(subAgent2Msg, nil, schema.Assistant, ""))
	g.Close()
	subAgent2.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).Return(i).Times(1)

	// 设置监督者第三次运行：完成任务
	i, g = adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	finishMsg := schema.AssistantMessage("finish", nil)
	g.Send(adk.EventFromMessage(finishMsg, nil, schema.Assistant, ""))
	g.Close()
	supervisorAgent.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).Return(i).Times(1)

	// 创建监督者配置
	conf := &Config{
		Supervisor: supervisorAgent,
		SubAgents:  []adk.Agent{subAgent1, subAgent2},
	}

	// 创建多智能体系统
	multiAgent, err := New(ctx, conf)
	assert.NoError(t, err)
	assert.NotNil(t, multiAgent)
	assert.Equal(t, "SupervisorAgent", multiAgent.Name(ctx))

	// 运行多智能体系统
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: multiAgent})
	aIter := runner.Run(ctx, []adk.Message{schema.UserMessage("test")})

	// 验证：转移到 SubAgent1
	event, ok := aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SupervisorAgent", event.AgentName)
	assert.Equal(t, schema.Assistant, event.Output.MessageOutput.Role)
	assert.NotEqual(t, 0, len(event.Output.MessageOutput.Message.ToolCalls))

	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SupervisorAgent", event.AgentName)
	assert.Equal(t, schema.Tool, event.Output.MessageOutput.Role)
	assert.Equal(t, "SubAgent1", event.Action.TransferToAgent.DestAgentName)

	// 验证：SubAgent1 的输出
	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SubAgent1", event.AgentName)
	assert.Equal(t, schema.Assistant, event.Output.MessageOutput.Role)
	assert.Equal(t, subAgent1Msg.Content, event.Output.MessageOutput.Message.Content)

	// 验证：SubAgent1 转移回监督者
	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SubAgent1", event.AgentName)
	assert.Equal(t, schema.Assistant, event.Output.MessageOutput.Role)
	assert.NotEqual(t, 0, len(event.Output.MessageOutput.Message.ToolCalls))

	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SubAgent1", event.AgentName)
	assert.Equal(t, schema.Tool, event.Output.MessageOutput.Role)
	assert.Equal(t, "SupervisorAgent", event.Action.TransferToAgent.DestAgentName)

	// 验证：转移到 SubAgent2
	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SupervisorAgent", event.AgentName)
	assert.Equal(t, schema.Assistant, event.Output.MessageOutput.Role)
	assert.NotEqual(t, 0, len(event.Output.MessageOutput.Message.ToolCalls))

	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SupervisorAgent", event.AgentName)
	assert.Equal(t, schema.Tool, event.Output.MessageOutput.Role)
	assert.Equal(t, "SubAgent2", event.Action.TransferToAgent.DestAgentName)

	// 验证：SubAgent2 的输出
	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SubAgent2", event.AgentName)
	assert.Equal(t, schema.Assistant, event.Output.MessageOutput.Role)
	assert.Equal(t, subAgent2Msg.Content, event.Output.MessageOutput.Message.Content)

	// 验证：SubAgent2 转移回监督者
	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SubAgent2", event.AgentName)
	assert.Equal(t, schema.Assistant, event.Output.MessageOutput.Role)
	assert.NotEqual(t, 0, len(event.Output.MessageOutput.Message.ToolCalls))

	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SubAgent2", event.AgentName)
	assert.Equal(t, schema.Tool, event.Output.MessageOutput.Role)
	assert.Equal(t, "SupervisorAgent", event.Action.TransferToAgent.DestAgentName)

	// 验证：监督者完成任务
	event, ok = aIter.Next()
	assert.True(t, ok)
	assert.Equal(t, "SupervisorAgent", event.AgentName)
	assert.Equal(t, schema.Assistant, event.Output.MessageOutput.Role)
	assert.Equal(t, finishMsg.Content, event.Output.MessageOutput.Message.Content)
}
