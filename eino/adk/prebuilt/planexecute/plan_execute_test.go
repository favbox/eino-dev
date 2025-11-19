package planexecute

import (
	"context"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/favbox/eino/adk"
	"github.com/favbox/eino/components/model"
	mockAdk "github.com/favbox/eino/internal/mock/adk"
	mockModel "github.com/favbox/eino/internal/mock/components/model"
	"github.com/favbox/eino/schema"
)

// TestNewPlannerWithFormattedOutput 测试使用 ChatModelWithFormattedOutput 创建规划器的功能。
func TestNewPlannerWithFormattedOutput(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 聊天模型
	mockChatModel := mockModel.NewMockBaseChatModel(ctrl)

	// 创建规划器配置
	conf := &PlannerConfig{
		ChatModelWithFormattedOutput: mockChatModel,
	}

	// 创建规划器
	p, err := NewPlanner(ctx, conf)
	assert.NoError(t, err)
	assert.NotNil(t, p)

	// 验证规划器的名称和描述
	assert.Equal(t, "Planner", p.Name(ctx))
	assert.Equal(t, "a planner agent", p.Description(ctx))
}

// TestNewPlannerWithToolCalling 测试使用 ToolCallingChatModel 创建规划器的功能。
func TestNewPlannerWithToolCalling(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 工具调用聊天模型
	mockToolCallingModel := mockModel.NewMockToolCallingChatModel(ctrl)
	mockToolCallingModel.EXPECT().WithTools(gomock.Any()).Return(mockToolCallingModel, nil).Times(1)

	// 创建规划器配置
	conf := &PlannerConfig{
		ToolCallingChatModel: mockToolCallingModel,
		// 使用默认的指令和工具信息
	}

	// 创建规划器
	p, err := NewPlanner(ctx, conf)
	assert.NoError(t, err)
	assert.NotNil(t, p)

	// 验证规划器的名称和描述
	assert.Equal(t, "Planner", p.Name(ctx))
	assert.Equal(t, "a planner agent", p.Description(ctx))
}

// TestPlannerRunWithFormattedOutput 测试使用 ChatModelWithFormattedOutput 创建的规划器的 Run 方法。
func TestPlannerRunWithFormattedOutput(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 聊天模型
	mockChatModel := mockModel.NewMockBaseChatModel(ctrl)

	// 创建计划响应
	planJSON := `{"steps":["Step 1", "Step 2", "Step 3"]}`
	planMsg := schema.AssistantMessage(planJSON, nil)
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(planMsg, nil)
	sw.Close()

	// 模拟 Stream 方法
	mockChatModel.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(sr, nil).Times(1)

	// 创建规划器配置
	conf := &PlannerConfig{
		ChatModelWithFormattedOutput: mockChatModel,
	}

	// 创建规划器
	p, err := NewPlanner(ctx, conf)
	assert.NoError(t, err)

	// 运行规划器
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: p})
	iterator := runner.Run(ctx, []adk.Message{schema.UserMessage("Plan this task")})

	// 从迭代器获取事件
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.Nil(t, event.Err)
	msg, _, err := adk.GetMessage(event)
	assert.NoError(t, err)
	assert.Equal(t, planMsg.Content, msg.Content)

	event, ok = iterator.Next()
	assert.False(t, ok)

	// 验证计划内容
	plan := defaultNewPlan(ctx)
	err = plan.UnmarshalJSON([]byte(msg.Content))
	assert.NoError(t, err)
	plan_ := plan.(*defaultPlan)
	assert.Equal(t, 3, len(plan_.Steps))
	assert.Equal(t, "Step 1", plan_.Steps[0])
	assert.Equal(t, "Step 2", plan_.Steps[1])
	assert.Equal(t, "Step 3", plan_.Steps[2])
}

// TestPlannerRunWithToolCalling 测试使用 ToolCallingChatModel 创建的规划器的 Run 方法。
func TestPlannerRunWithToolCalling(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 工具调用聊天模型
	mockToolCallingModel := mockModel.NewMockToolCallingChatModel(ctrl)

	// 创建包含计划的工具调用响应
	planArgs := `{"steps":["Step 1", "Step 2", "Step 3"]}`
	toolCall := schema.ToolCall{
		ID:   "tool_call_id",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "Plan", // 应该匹配 PlanToolInfo.Name
			Arguments: planArgs,
		},
	}

	toolCallMsg := schema.AssistantMessage("", nil)
	toolCallMsg.ToolCalls = []schema.ToolCall{toolCall}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(toolCallMsg, nil)
	sw.Close()

	// 模拟 WithTools 方法，返回用于 Generate 的模型
	mockToolCallingModel.EXPECT().WithTools(gomock.Any()).Return(mockToolCallingModel, nil).Times(1)

	// 模拟 Stream 方法，返回工具调用消息
	mockToolCallingModel.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(sr, nil).Times(1)

	// 使用 ToolCallingChatModel 创建规划器配置
	conf := &PlannerConfig{
		ToolCallingChatModel: mockToolCallingModel,
		// 使用默认的指令和工具信息
	}

	// 创建规划器
	p, err := NewPlanner(ctx, conf)
	assert.NoError(t, err)

	// 运行规划器
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: p})
	iterator := runner.Run(ctx, []adk.Message{schema.UserMessage("no input")})

	// 从迭代器获取事件
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.Nil(t, event.Err)

	msg, _, err := adk.GetMessage(event)
	assert.NoError(t, err)
	assert.Equal(t, planArgs, msg.Content)

	_, ok = iterator.Next()
	assert.False(t, ok)

	// 验证计划内容
	plan := defaultNewPlan(ctx)
	err = plan.UnmarshalJSON([]byte(msg.Content))
	assert.NoError(t, err)
	plan_ := plan.(*defaultPlan)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(plan_.Steps))
	assert.Equal(t, "Step 1", plan_.Steps[0])
	assert.Equal(t, "Step 2", plan_.Steps[1])
	assert.Equal(t, "Step 3", plan_.Steps[2])
}

// TestNewExecutor 测试 NewExecutor 函数创建执行器的功能。
func TestNewExecutor(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 工具调用聊天模型
	mockToolCallingModel := mockModel.NewMockToolCallingChatModel(ctrl)

	// 创建执行器配置
	conf := &ExecutorConfig{
		Model:         mockToolCallingModel,
		MaxIterations: 3,
	}

	// 创建执行器
	executor, err := NewExecutor(ctx, conf)
	assert.NoError(t, err)
	assert.NotNil(t, executor)

	// 验证执行器的名称和描述
	assert.Equal(t, "Executor", executor.Name(ctx))
	assert.Equal(t, "an executor agent", executor.Description(ctx))
}

// TestExecutorRun 测试执行器的 Run 方法。
func TestExecutorRun(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 工具调用聊天模型
	mockToolCallingModel := mockModel.NewMockToolCallingChatModel(ctrl)

	// 在会话中存储计划
	plan := &defaultPlan{Steps: []string{"Step 1", "Step 2", "Step 3"}}
	adk.AddSessionValue(ctx, PlanSessionKey, plan)

	// 设置 mock 模型的期望行为
	// 模型应返回最后一条用户消息作为响应
	mockToolCallingModel.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			// 查找最后一条用户消息
			var lastUserMessage string
			for _, msg := range messages {
				if msg.Role == schema.User {
					lastUserMessage = msg.Content
				}
			}
			// 返回最后一条用户消息作为模型的响应
			return schema.AssistantMessage(lastUserMessage, nil), nil
		}).Times(1)

	// 创建执行器配置
	conf := &ExecutorConfig{
		Model:         mockToolCallingModel,
		MaxIterations: 3,
	}

	// 创建执行器
	executor, err := NewExecutor(ctx, conf)
	assert.NoError(t, err)

	// 运行执行器
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: executor})
	iterator := runner.Run(ctx, []adk.Message{schema.UserMessage("no input")},
		adk.WithSessionValues(map[string]any{
			PlanSessionKey:      plan,
			UserInputSessionKey: []adk.Message{schema.UserMessage("no input")},
		}),
	)

	// 从迭代器获取事件
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.Nil(t, event.Err)
	assert.NotNil(t, event.Output)
	assert.NotNil(t, event.Output.MessageOutput)
	msg, _, err := adk.GetMessage(event)
	assert.NoError(t, err)
	t.Logf("executor model input msg:\n %s\n", msg.Content)

	_, ok = iterator.Next()
	assert.False(t, ok)
}

// TestNewReplanner 测试 NewReplanner 函数创建重规划器的功能。
func TestNewReplanner(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 工具调用聊天模型
	mockToolCallingModel := mockModel.NewMockToolCallingChatModel(ctrl)
	// 模拟 WithTools 方法
	mockToolCallingModel.EXPECT().WithTools(gomock.Any()).Return(mockToolCallingModel, nil).Times(1)

	// 创建计划和响应工具
	planTool := &schema.ToolInfo{
		Name: "Plan",
		Desc: "Plan tool",
	}

	respondTool := &schema.ToolInfo{
		Name: "Respond",
		Desc: "Respond tool",
	}

	// 创建重规划器配置
	conf := &ReplannerConfig{
		ChatModel:   mockToolCallingModel,
		PlanTool:    planTool,
		RespondTool: respondTool,
	}

	// 创建重规划器
	rp, err := NewReplanner(ctx, conf)
	assert.NoError(t, err)
	assert.NotNil(t, rp)

	// 验证重规划器的名称和描述
	assert.Equal(t, "Replanner", rp.Name(ctx))
	assert.Equal(t, "a replanner agent", rp.Description(ctx))
}

// TestReplannerRunWithPlan 测试重规划器使用 plan_tool 的能力。
func TestReplannerRunWithPlan(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 工具调用聊天模型
	mockToolCallingModel := mockModel.NewMockToolCallingChatModel(ctrl)

	// 创建计划和响应工具
	planTool := &schema.ToolInfo{
		Name: "Plan",
		Desc: "Plan tool",
	}

	respondTool := &schema.ToolInfo{
		Name: "Respond",
		Desc: "Respond tool",
	}

	// 为 Plan 工具创建工具调用响应
	planArgs := `{"steps":["Updated Step 1", "Updated Step 2"]}`
	toolCall := schema.ToolCall{
		ID:   "tool_call_id",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      planTool.Name,
			Arguments: planArgs,
		},
	}

	toolCallMsg := schema.AssistantMessage("", nil)
	toolCallMsg.ToolCalls = []schema.ToolCall{toolCall}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(toolCallMsg, nil)
	sw.Close()

	// 模拟 Generate 方法
	mockToolCallingModel.EXPECT().WithTools(gomock.Any()).Return(mockToolCallingModel, nil).Times(1)
	mockToolCallingModel.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(sr, nil).Times(1)

	// 创建重规划器配置
	conf := &ReplannerConfig{
		ChatModel:   mockToolCallingModel,
		PlanTool:    planTool,
		RespondTool: respondTool,
	}

	// 创建重规划器
	rp, err := NewReplanner(ctx, conf)
	assert.NoError(t, err)

	// 在会话中存储必要的值
	plan := &defaultPlan{Steps: []string{"Step 1", "Step 2", "Step 3"}}

	rp, err = agentOutputSessionKVs(ctx, rp)
	assert.NoError(t, err)

	// 运行重规划器
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: rp})
	iterator := runner.Run(ctx, []adk.Message{schema.UserMessage("no input")},
		adk.WithSessionValues(map[string]any{
			PlanSessionKey:         plan,
			ExecutedStepSessionKey: "Execution result",
			UserInputSessionKey:    []adk.Message{schema.UserMessage("User input")},
		}),
	)

	// 从迭代器获取事件
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.Nil(t, event.Err)

	event, ok = iterator.Next()
	assert.True(t, ok)
	kvs := event.Output.CustomizedOutput.(map[string]any)
	assert.Greater(t, len(kvs), 0)

	// 验证更新的计划已存储在会话中
	planValue, ok := kvs[PlanSessionKey]
	assert.True(t, ok)
	updatedPlan, ok := planValue.(*defaultPlan)
	assert.True(t, ok)
	assert.Equal(t, 2, len(updatedPlan.Steps))
	assert.Equal(t, "Updated Step 1", updatedPlan.Steps[0])
	assert.Equal(t, "Updated Step 2", updatedPlan.Steps[1])

	// 验证执行结果已更新
	executeResultsValue, ok := kvs[ExecutedStepsSessionKey]
	assert.True(t, ok)
	executeResults, ok := executeResultsValue.([]ExecutedStep)
	assert.True(t, ok)
	assert.Equal(t, 1, len(executeResults))
	assert.Equal(t, "Step 1", executeResults[0].Step)
	assert.Equal(t, "Execution result", executeResults[0].Result)

	_, ok = iterator.Next()
	assert.False(t, ok)
}

// TestReplannerRunWithRespond 测试重规划器使用 respond_tool 的能力。
func TestReplannerRunWithRespond(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 工具调用聊天模型
	mockToolCallingModel := mockModel.NewMockToolCallingChatModel(ctrl)

	// 创建计划和响应工具
	planTool := &schema.ToolInfo{
		Name: "Plan",
		Desc: "Plan tool",
	}

	respondTool := &schema.ToolInfo{
		Name: "Respond",
		Desc: "Respond tool",
	}

	// 为 Respond 工具创建工具调用响应
	responseArgs := `{"response":"This is the final response to the user"}`
	toolCall := schema.ToolCall{
		ID:   "tool_call_id",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      respondTool.Name,
			Arguments: responseArgs,
		},
	}

	toolCallMsg := schema.AssistantMessage("", nil)
	toolCallMsg.ToolCalls = []schema.ToolCall{toolCall}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(toolCallMsg, nil)
	sw.Close()

	// 模拟 Generate 方法
	mockToolCallingModel.EXPECT().WithTools(gomock.Any()).Return(mockToolCallingModel, nil).Times(1)
	mockToolCallingModel.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(sr, nil).Times(1)

	// 创建重规划器配置
	conf := &ReplannerConfig{
		ChatModel:   mockToolCallingModel,
		PlanTool:    planTool,
		RespondTool: respondTool,
	}

	// 创建重规划器
	rp, err := NewReplanner(ctx, conf)
	assert.NoError(t, err)

	// 在会话中存储必要的值
	plan := &defaultPlan{Steps: []string{"Step 1", "Step 2", "Step 3"}}

	// 运行重规划器
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: rp})
	iterator := runner.Run(ctx, []adk.Message{schema.UserMessage("no input")},
		adk.WithSessionValues(map[string]any{
			PlanSessionKey:         plan,
			ExecutedStepSessionKey: "Execution result",
			UserInputSessionKey:    []adk.Message{schema.UserMessage("User input")},
		}),
	)

	// 从迭代器获取事件
	event, ok := iterator.Next()
	assert.True(t, ok)
	assert.Nil(t, event.Err)
	msg, _, err := adk.GetMessage(event)
	assert.NoError(t, err)
	assert.Equal(t, responseArgs, msg.Content)

	// 验证生成了退出动作
	event, ok = iterator.Next()
	assert.True(t, ok)
	assert.NotNil(t, event.Action)
	assert.NotNil(t, event.Action.BreakLoop)
	assert.False(t, event.Action.BreakLoop.Done)

	_, ok = iterator.Next()
	assert.False(t, ok)
}

// TestNewPlanExecuteAgent 测试 New 函数创建计划-执行智能体的功能。
func TestNewPlanExecuteAgent(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 智能体
	mockPlanner := mockAdk.NewMockAgent(ctrl)
	mockExecutor := mockAdk.NewMockAgent(ctrl)
	mockReplanner := mockAdk.NewMockAgent(ctrl)

	// 设置 mock 智能体的期望行为
	mockPlanner.EXPECT().Name(gomock.Any()).Return("Planner").AnyTimes()
	mockPlanner.EXPECT().Description(gomock.Any()).Return("a planner agent").AnyTimes()

	mockExecutor.EXPECT().Name(gomock.Any()).Return("Executor").AnyTimes()
	mockExecutor.EXPECT().Description(gomock.Any()).Return("an executor agent").AnyTimes()

	mockReplanner.EXPECT().Name(gomock.Any()).Return("Replanner").AnyTimes()
	mockReplanner.EXPECT().Description(gomock.Any()).Return("a replanner agent").AnyTimes()

	conf := &Config{
		Planner:   mockPlanner,
		Executor:  mockExecutor,
		Replanner: mockReplanner,
	}

	// 创建计划-执行智能体
	agent, err := New(ctx, conf)
	assert.NoError(t, err)
	assert.NotNil(t, agent)
}

// TestPlanExecuteAgentWithReplan 测试带有重规划功能的计划-执行智能体。
func TestPlanExecuteAgentWithReplan(t *testing.T) {
	ctx := context.Background()

	// 创建 mock 控制器
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建 mock 智能体
	mockPlanner := mockAdk.NewMockAgent(ctrl)
	mockExecutor := mockAdk.NewMockAgent(ctrl)
	mockReplanner := mockAdk.NewMockAgent(ctrl)

	// 设置 mock 智能体的期望行为
	mockPlanner.EXPECT().Name(gomock.Any()).Return("Planner").AnyTimes()
	mockPlanner.EXPECT().Description(gomock.Any()).Return("a planner agent").AnyTimes()

	mockExecutor.EXPECT().Name(gomock.Any()).Return("Executor").AnyTimes()
	mockExecutor.EXPECT().Description(gomock.Any()).Return("an executor agent").AnyTimes()

	mockReplanner.EXPECT().Name(gomock.Any()).Return("Replanner").AnyTimes()
	mockReplanner.EXPECT().Description(gomock.Any()).Return("a replanner agent").AnyTimes()

	// 创建原始计划
	originalPlan := &defaultPlan{Steps: []string{"Step 1", "Step 2", "Step 3"}}
	// 创建更新后的计划（重规划后步骤更少）
	updatedPlan := &defaultPlan{Steps: []string{"Updated Step 2", "Updated Step 3"}}
	// 创建执行结果
	originalExecuteResult := "Execution result for Step 1"
	updatedExecuteResult := "Execution result for Updated Step 2"

	// 创建用户输入
	userInput := []adk.Message{schema.UserMessage("User task input")}

	finalResponse := &Response{Response: "Final response to user after executing all steps"}

	// 模拟规划器的 Run 方法，设置原始计划
	mockPlanner.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
			iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

			// 在会话中设置计划
			adk.AddSessionValue(ctx, PlanSessionKey, originalPlan)
			adk.AddSessionValue(ctx, UserInputSessionKey, userInput)

			// 发送消息事件
			planJSON, _ := sonic.MarshalString(originalPlan)
			msg := schema.AssistantMessage(planJSON, nil)
			event := adk.EventFromMessage(msg, nil, schema.Assistant, "")
			generator.Send(event)
			generator.Close()

			return iterator
		},
	).Times(1)

	// 模拟执行器的 Run 方法，设置执行结果
	mockExecutor.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
			iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

			plan, _ := adk.GetSessionValue(ctx, PlanSessionKey)
			currentPlan := plan.(*defaultPlan)
			var msg adk.Message
			// 检查这是否是第一次重规划（原始计划有 3 个步骤）
			if len(currentPlan.Steps) == 3 {
				msg = schema.AssistantMessage(originalExecuteResult, nil)
				adk.AddSessionValue(ctx, ExecutedStepSessionKey, originalExecuteResult)
			} else {
				msg = schema.AssistantMessage(updatedExecuteResult, nil)
				adk.AddSessionValue(ctx, ExecutedStepSessionKey, updatedExecuteResult)
			}
			event := adk.EventFromMessage(msg, nil, schema.Assistant, "")
			generator.Send(event)
			generator.Close()

			return iterator
		},
	).Times(2)

	// 模拟重规划器的 Run 方法，首先更新计划，然后响应用户
	mockReplanner.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
			iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

			// 第一次调用：更新计划
			// 从会话中获取当前计划
			plan, _ := adk.GetSessionValue(ctx, PlanSessionKey)
			currentPlan := plan.(*defaultPlan)

			// 检查这是否是第一次重规划（原始计划有 3 个步骤）
			if len(currentPlan.Steps) == 3 {
				// 发送包含更新计划的消息事件
				planJSON, _ := sonic.MarshalString(updatedPlan)
				msg := schema.AssistantMessage(planJSON, nil)
				event := adk.EventFromMessage(msg, nil, schema.Assistant, "")
				generator.Send(event)

				// 在会话中设置更新的计划和执行结果
				adk.AddSessionValue(ctx, PlanSessionKey, updatedPlan)
				adk.AddSessionValue(ctx, ExecutedStepsSessionKey, []ExecutedStep{{
					Step:   currentPlan.Steps[0],
					Result: originalExecuteResult,
				}})
			} else {
				// 第二次调用：响应用户
				responseJSON, err := sonic.MarshalString(finalResponse)
				assert.NoError(t, err)
				msg := schema.AssistantMessage(responseJSON, nil)
				event := adk.EventFromMessage(msg, nil, schema.Assistant, "")
				generator.Send(event)

				// 发送退出动作
				action := adk.NewExitAction()
				generator.Send(&adk.AgentEvent{Action: action})
			}

			generator.Close()
			return iterator
		},
	).Times(2)

	conf := &Config{
		Planner:   mockPlanner,
		Executor:  mockExecutor,
		Replanner: mockReplanner,
	}

	// 创建计划-执行智能体
	agent, err := New(ctx, conf)
	assert.NoError(t, err)
	assert.NotNil(t, agent)

	// 运行智能体
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	iterator := runner.Run(ctx, userInput)

	// 收集所有事件
	var events []*adk.AgentEvent
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		events = append(events, event)
	}

	// 验证事件
	assert.Greater(t, len(events), 0)

	for i, event := range events {
		eventJSON, e := sonic.MarshalString(event)
		assert.NoError(t, e)
		t.Logf("event %d:\n%s", i, eventJSON)
	}
}
