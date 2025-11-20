// Package planexecute 提供了基于计划-执行-重规划模式的智能体系统实现。
//
// 该模式通过三个阶段协同工作来解决复杂问题：
//  1. 规划（Planning）：生成包含清晰、可执行步骤的结构化计划
//  2. 执行（Execution）：执行计划中的第一步
//  3. 重规划（Replanning）：评估进度，根据结果决定完成任务或修订计划
//
// 这种方法通过迭代优化实现复杂问题的求解。
package planexecute

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/bytedance/sonic"

	"github.com/favbox/eino/adk"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/internal/safe"
	"github.com/favbox/eino/schema"
)

func init() {
	schema.RegisterName[*defaultPlan]("_eino_adk_plan_execute_default_plan")
}

// Plan 表示包含一系列可执行步骤的执行计划。
// 支持 JSON 序列化和反序列化，并提供对第一步的访问。
type Plan interface {
	// FirstStep 返回计划中要执行的第一步。
	FirstStep() string

	// Marshaler 将 Plan 序列化为 JSON。
	// 生成的 JSON 可用于提示词模板。
	json.Marshaler
	// Unmarshaler 将 JSON 内容反序列化为 Plan。
	// 这会将结构化聊天模型或工具调用的输出处理为 Plan 结构。
	json.Unmarshaler
}

// NewPlan 是创建新 Plan 实例的函数类型。
type NewPlan func(ctx context.Context) Plan

// defaultPlan 是 Plan 接口的默认实现。
//
// JSON Schema:
//
//	{
//	  "type": "object",
//	  "properties": {
//	    "steps": {
//	      "type": "array",
//	      "items": {
//	        "type": "string"
//	      },
//	      "description": "要执行的有序操作列表。每个步骤应清晰、可执行，并按逻辑顺序排列。"
//	    }
//	  },
//	  "required": ["steps"]
//	}
type defaultPlan struct {
	// Steps 包含要执行的有序操作列表。
	// 每个步骤应清晰、可执行，并按逻辑顺序排列。
	Steps []string `json:"steps"`
}

// FirstStep 返回计划中的第一步，如果没有步骤则返回空字符串。
func (p *defaultPlan) FirstStep() string {
	if len(p.Steps) == 0 {
		return ""
	}
	return p.Steps[0]
}

func (p *defaultPlan) MarshalJSON() ([]byte, error) {
	type planTyp defaultPlan
	return sonic.Marshal((*planTyp)(p))
}

func (p *defaultPlan) UnmarshalJSON(bytes []byte) error {
	type planTyp defaultPlan
	return sonic.Unmarshal(bytes, (*planTyp)(p))
}

// Response 表示返回给用户的最终响应。
// 该结构用于对模型生成的最终响应进行 JSON 序列化/反序列化。
type Response struct {
	// Response 是提供给用户的完整响应。
	// 此字段为必填项。
	Response string `json:"response"`
}

var (
	// PlanToolInfo 定义了可与 ToolCallingChatModel 一起使用的 Plan 工具的 schema。
	// 该 schema 指示模型生成包含有序步骤的结构化计划。
	PlanToolInfo = schema.ToolInfo{
		Name: "Plan",
		Desc: "Plan with a list of steps to execute in order. Each step should be clear, actionable, and arranged in a logical sequence. The output will be used to guide the execution process.",
		ParamsOneOf: schema.NewParamsOneOfByParams(
			map[string]*schema.ParameterInfo{
				"steps": {
					Type:     schema.Array,
					ElemInfo: &schema.ParameterInfo{Type: schema.String},
					Desc:     "different steps to follow, should be in sorted order",
					Required: true,
				},
			},
		),
	}

	// RespondToolInfo 定义了可与 ToolCallingChatModel 一起使用的响应工具的 schema。
	// 该 schema 指示模型生成对用户的直接响应。
	RespondToolInfo = schema.ToolInfo{
		Name: "Respond",
		Desc: "Generate a direct response to the user. Use this tool when you have all the information needed to provide a final answer.",
		ParamsOneOf: schema.NewParamsOneOfByParams(
			map[string]*schema.ParameterInfo{
				"response": {
					Type:     schema.String,
					Desc:     "The complete response to provide to the user",
					Required: true,
				},
			},
		),
	}

	// PlannerPrompt 是规划器的提示词模板。
	// 它为规划器提供上下文和指导，说明如何生成 Plan。
	PlannerPrompt = prompt.FromMessages(schema.FString,
		schema.SystemMessage(`You are an expert planning agent. Given an objective, create a comprehensive step-by-step plan to achieve the objective.

## YOUR TASK
Analyze the objective and generate a strategic plan that breaks down the goal into manageable, executable steps.

## PLANNING REQUIREMENTS
Each step in your plan must be:
- **Specific and actionable**: Clear instructions that can be executed without ambiguity
- **Self-contained**: Include all necessary context, parameters, and requirements
- **Independently executable**: Can be performed by an agent without dependencies on other steps
- **Logically sequenced**: Arranged in optimal order for efficient execution
- **Objective-focused**: Directly contribute to achieving the main goal

## PLANNING GUIDELINES
- Eliminate redundant or unnecessary steps
- Include relevant constraints, parameters, and success criteria for each step
- Ensure the final step produces a complete answer or deliverable
- Anticipate potential challenges and include mitigation strategies
- Structure steps to build upon each other logically
- Provide sufficient detail for successful execution

## QUALITY CRITERIA
- Plan completeness: Does it address all aspects of the objective?
- Step clarity: Can each step be understood and executed independently?
- Logical flow: Do steps follow a sensible progression?
- Efficiency: Is this the most direct path to the objective?
- Adaptability: Can the plan handle unexpected results or changes?`),
		schema.MessagesPlaceholder("input", false),
	)

	// ExecutorPrompt 是执行器的提示词模板。
	// 它为执行器提供上下文和指导，说明如何执行任务。
	ExecutorPrompt = prompt.FromMessages(schema.FString,
		schema.SystemMessage(`You are a diligent and meticulous executor agent. Follow the given plan and execute your tasks carefully and thoroughly.`),
		schema.UserMessage(`## OBJECTIVE
{input}
## Given the following plan:
{plan}
## COMPLETED STEPS & RESULTS
{executed_steps}
## Your task is to execute the first step, which is:
{step}`))

	// ReplannerPrompt 是重规划器的提示词模板。
	// 它为重规划器提供上下文和指导，说明如何重新生成 Plan。
	ReplannerPrompt = prompt.FromMessages(schema.FString,
		schema.SystemMessage(
			`You are going to review the progress toward an objective. Analyze the current state and determine the optimal next action.

## YOUR TASK
Based on the progress above, you MUST choose exactly ONE action:

### Option 1: COMPLETE (if objective is fully achieved)
Call '{respond_tool}' with:
- A comprehensive final answer
- Clear conclusion summarizing how the objective was met
- Key insights from the execution process

### Option 2: CONTINUE (if more work is needed)
Call '{plan_tool}' with a revised plan that:
- Contains ONLY remaining steps (exclude completed ones)
- Incorporates lessons learned from executed steps
- Addresses any gaps or issues discovered
- Maintains logical step sequence

## PLANNING REQUIREMENTS
Each step in your plan must be:
- **Specific and actionable**: Clear instructions that can be executed without ambiguity
- **Self-contained**: Include all necessary context, parameters, and requirements
- **Independently executable**: Can be performed by an agent without dependencies on other steps
- **Logically sequenced**: Arranged in optimal order for efficient execution
- **Objective-focused**: Directly contribute to achieving the main goal

## PLANNING GUIDELINES
- Eliminate redundant or unnecessary steps
- Adapt strategy based on new information
- Include relevant constraints, parameters, and success criteria for each step

## DECISION CRITERIA
- Has the original objective been completely satisfied?
- Are there any remaining requirements or sub-goals?
- Do the results suggest a need for strategy adjustment?
- What specific actions are still required?`),
		schema.UserMessage(`## OBJECTIVE
{input}

## ORIGINAL PLAN
{plan}

## COMPLETED STEPS & RESULTS
{executed_steps}`),
	)
)

const (
	// UserInputSessionKey 是用户输入的会话键。
	UserInputSessionKey = "UserInput"

	// PlanSessionKey 是计划的会话键。
	PlanSessionKey = "Plan"

	// ExecutedStepSessionKey 是执行结果的会话键。
	ExecutedStepSessionKey = "ExecutedStep"

	// ExecutedStepsSessionKey 是执行结果列表的会话键。
	ExecutedStepsSessionKey = "ExecutedSteps"
)

// PlannerConfig 提供创建规划器智能体的配置选项。
// 有两种方式配置规划器以生成结构化的 Plan 输出：
//  1. 使用 ChatModelWithFormattedOutput：预配置为以 Plan 格式输出的模型
//  2. 使用 ToolCallingChatModel + ToolInfo：使用工具调用生成 Plan 结构的模型
type PlannerConfig struct {
	// ChatModelWithFormattedOutput 是预配置为以 Plan 格式输出的模型。
	// 通过配置模型直接输出结构化数据来创建。
	// 参考示例：https://github.com/cloudwego/eino-ext/blob/main/components/model/openai/examples/structured/structured.go
	ChatModelWithFormattedOutput model.BaseChatModel

	// ToolCallingChatModel 是支持工具调用能力的模型。
	// 当与 ToolInfo 一起提供时，将使用工具调用生成 Plan 结构。
	ToolCallingChatModel model.ToolCallingChatModel

	// ToolInfo 定义使用工具调用时 Plan 结构的 schema。
	// 可选。如果未提供，将使用 PlanToolInfo 作为默认值。
	ToolInfo *schema.ToolInfo

	// GenInputFn 是为规划器生成输入消息的函数。
	// 可选。如果未提供，将使用 defaultGenPlannerInputFn。
	GenInputFn GenPlannerModelInputFn

	// NewPlan 创建用于 JSON 的新 Plan 实例。
	// 返回的 Plan 将用于反序列化模型生成的 JSON 输出。
	// 可选。如果未提供，将使用 defaultNewPlan。
	NewPlan NewPlan
}

// GenPlannerModelInputFn 是为规划器生成输入消息的函数类型。
type GenPlannerModelInputFn func(ctx context.Context, userInput []adk.Message) ([]adk.Message, error)

func defaultNewPlan(ctx context.Context) Plan {
	return &defaultPlan{}
}

func defaultGenPlannerInputFn(ctx context.Context, userInput []adk.Message) ([]adk.Message, error) {
	msgs, err := PlannerPrompt.Format(ctx, map[string]any{
		"input": userInput,
	})
	if err != nil {
		return nil, err
	}
	return msgs, nil
}

type planner struct {
	toolCall   bool
	chatModel  model.BaseChatModel
	genInputFn GenPlannerModelInputFn
	newPlan    NewPlan
}

func (p *planner) Name(_ context.Context) string {
	return "Planner"
}

func (p *planner) Description(_ context.Context) string {
	return "a planner agent"
}

func argToContent(msg adk.Message) (adk.Message, error) {
	if len(msg.ToolCalls) == 0 {
		return nil, schema.ErrNoValue
	}

	return schema.AssistantMessage(msg.ToolCalls[0].Function.Arguments, nil), nil
}

func (p *planner) Run(ctx context.Context, input *adk.AgentInput,
	_ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {

	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	adk.AddSessionValue(ctx, UserInputSessionKey, input.Messages)

	go func() {
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				e := safe.NewPanicErr(panicErr, debug.Stack())
				generator.Send(&adk.AgentEvent{Err: e})
			}

			generator.Close()
		}()

		c := compose.NewChain[*adk.AgentInput, Plan]().
			AppendLambda(
				compose.InvokableLambda(func(ctx context.Context, input *adk.AgentInput) (output []adk.Message, err error) {
					return p.genInputFn(ctx, input.Messages)
				}),
			).
			AppendChatModel(p.chatModel).
			AppendLambda(
				compose.CollectableLambda(func(ctx context.Context, sr *schema.StreamReader[adk.Message]) (adk.Message, error) {
					if input.EnableStreaming {
						ss := sr.Copy(2)
						var sOutput *schema.StreamReader[*schema.Message]
						if p.toolCall {
							sOutput = schema.StreamReaderWithConvert(ss[0], argToContent)
						} else {
							sOutput = ss[0]
						}

						generator.Send(adk.EventFromMessage(nil, sOutput, schema.Assistant, ""))

						return schema.ConcatMessageStream(ss[1])
					}

					msg, err := schema.ConcatMessageStream(sr)
					if err != nil {
						return nil, err
					}

					var output adk.Message
					if p.toolCall {
						if len(msg.ToolCalls) == 0 {
							return nil, fmt.Errorf("no tool call")
						}
						output = schema.AssistantMessage(msg.ToolCalls[0].Function.Arguments, nil)
					} else {
						output = msg
					}

					generator.Send(adk.EventFromMessage(output, nil, schema.Assistant, ""))

					return msg, nil
				}),
			).
			AppendLambda(
				compose.InvokableLambda(func(ctx context.Context, msg adk.Message) (plan Plan, err error) {
					var planJSON string
					if p.toolCall {
						if len(msg.ToolCalls) == 0 {
							return nil, fmt.Errorf("no tool call")
						}
						planJSON = msg.ToolCalls[0].Function.Arguments
					} else {
						planJSON = msg.Content
					}

					plan = p.newPlan(ctx)
					err = plan.UnmarshalJSON([]byte(planJSON))
					if err != nil {
						return nil, fmt.Errorf("unmarshal plan error: %w", err)
					}

					adk.AddSessionValue(ctx, PlanSessionKey, plan)

					return plan, nil
				}),
			)

		var opts []compose.Option
		if p.toolCall {
			opts = append(opts, compose.WithChatModelOption(model.WithToolChoice(schema.ToolChoiceForced)))
		}

		r, err := c.Compile(ctx, compose.WithGraphName(p.Name(ctx)))
		if err != nil { // unexpected
			generator.Send(&adk.AgentEvent{Err: err})
			return
		}

		_, err = r.Stream(ctx, input, opts...)
		if err != nil {
			generator.Send(&adk.AgentEvent{Err: err})
			return
		}
	}()

	return iterator
}

// NewPlanner 根据提供的配置创建新的规划器智能体。
// 规划器智能体使用 ChatModelWithFormattedOutput 或 ToolCallingChatModel+ToolInfo
// 来生成结构化的 Plan 输出。
//
// 如果提供了 ChatModelWithFormattedOutput，将直接使用它。
// 如果提供了 ToolCallingChatModel，将使用 ToolInfo（或默认的 PlanToolInfo）配置它
// 以生成结构化的 Plan 输出。
func NewPlanner(_ context.Context, cfg *PlannerConfig) (adk.Agent, error) {
	var chatModel model.BaseChatModel
	var toolCall bool
	if cfg.ChatModelWithFormattedOutput != nil {
		chatModel = cfg.ChatModelWithFormattedOutput
	} else {
		toolCall = true
		toolInfo := cfg.ToolInfo
		if toolInfo == nil {
			toolInfo = &PlanToolInfo
		}

		var err error
		chatModel, err = cfg.ToolCallingChatModel.WithTools([]*schema.ToolInfo{toolInfo})
		if err != nil {
			return nil, err
		}
	}

	inputFn := cfg.GenInputFn
	if inputFn == nil {
		inputFn = defaultGenPlannerInputFn
	}

	planParser := cfg.NewPlan
	if planParser == nil {
		planParser = defaultNewPlan
	}

	return &planner{
		toolCall:   toolCall,
		chatModel:  chatModel,
		genInputFn: inputFn,
		newPlan:    planParser,
	}, nil
}

// ExecutionContext 是执行器和规划器的输入信息。
type ExecutionContext struct {
	UserInput     []adk.Message
	Plan          Plan
	ExecutedSteps []ExecutedStep
}

// GenModelInputFn 是为执行器和规划器生成输入消息的函数。
type GenModelInputFn func(ctx context.Context, in *ExecutionContext) ([]adk.Message, error)

// ExecutorConfig 提供创建执行器智能体的配置选项。
type ExecutorConfig struct {
	// Model 是执行器使用的聊天模型。
	Model model.ToolCallingChatModel

	// ToolsConfig 指定执行器可用的工具。
	ToolsConfig adk.ToolsConfig

	// MaxIterations 定义 ChatModel 生成周期的上限。
	// 如果超过此限制，智能体将以错误终止。
	// 可选。默认为 20。
	MaxIterations int

	// GenInputFn 为执行器生成输入消息。
	// 可选。如果未提供，将使用 defaultGenExecutorInputFn。
	GenInputFn GenModelInputFn
}

// ExecutedStep 表示已执行的步骤。
type ExecutedStep struct {
	Step   string
	Result string
}

// NewExecutor 创建新的执行器智能体。
func NewExecutor(ctx context.Context, cfg *ExecutorConfig) (adk.Agent, error) {

	genInputFn := cfg.GenInputFn
	if genInputFn == nil {
		genInputFn = defaultGenExecutorInputFn
	}
	genInput := func(ctx context.Context, instruction string, _ *adk.AgentInput) ([]adk.Message, error) {

		plan, ok := adk.GetSessionValue(ctx, PlanSessionKey)
		if !ok {
			panic("impossible: plan not found")
		}
		plan_ := plan.(Plan)

		userInput, ok := adk.GetSessionValue(ctx, UserInputSessionKey)
		if !ok {
			panic("impossible: user input not found")
		}
		userInput_ := userInput.([]adk.Message)

		var executedSteps_ []ExecutedStep
		executedStep, ok := adk.GetSessionValue(ctx, ExecutedStepsSessionKey)
		if ok {
			executedSteps_ = executedStep.([]ExecutedStep)
		}

		in := &ExecutionContext{
			UserInput:     userInput_,
			Plan:          plan_,
			ExecutedSteps: executedSteps_,
		}

		msgs, err := genInputFn(ctx, in)
		if err != nil {
			return nil, err
		}

		return msgs, nil
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "Executor",
		Description:   "an executor agent",
		Model:         cfg.Model,
		ToolsConfig:   cfg.ToolsConfig,
		GenModelInput: genInput,
		MaxIterations: cfg.MaxIterations,
		OutputKey:     ExecutedStepSessionKey,
	})
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func defaultGenExecutorInputFn(ctx context.Context, in *ExecutionContext) ([]adk.Message, error) {

	planContent, err := in.Plan.MarshalJSON()
	if err != nil {
		return nil, err
	}

	userMsgs, err := ExecutorPrompt.Format(ctx, map[string]any{
		"input":          formatInput(in.UserInput),
		"plan":           string(planContent),
		"executed_steps": formatExecutedSteps(in.ExecutedSteps),
		"step":           in.Plan.FirstStep(),
	})
	if err != nil {
		return nil, err
	}

	return userMsgs, nil
}

type replanner struct {
	chatModel   model.ToolCallingChatModel
	planTool    *schema.ToolInfo
	respondTool *schema.ToolInfo

	genInputFn GenModelInputFn
	newPlan    NewPlan
}

// ReplannerConfig 提供创建重规划器智能体的配置选项。
type ReplannerConfig struct {
	// ChatModel 是支持工具调用能力的模型。
	// 它将配置 PlanTool 和 RespondTool 以生成更新的计划或响应。
	ChatModel model.ToolCallingChatModel

	// PlanTool 定义可与 ToolCallingChatModel 一起使用的 Plan 工具的 schema。
	// 可选。如果未提供，将使用默认的 PlanToolInfo。
	PlanTool *schema.ToolInfo

	// RespondTool 定义可与 ToolCallingChatModel 一起使用的响应工具的 schema。
	// 可选。如果未提供，将使用默认的 RespondToolInfo。
	RespondTool *schema.ToolInfo

	// GenInputFn 为重规划器生成输入消息。
	// 可选。如果未提供，将使用 buildGenReplannerInputFn。
	GenInputFn GenModelInputFn

	// NewPlan 创建新的 Plan 实例。
	// 返回的 Plan 将用于反序列化 PlanTool 生成的模型 JSON 输出。
	// 可选。如果未提供，将使用 defaultNewPlan。
	NewPlan NewPlan
}

// formatInput 将输入消息格式化为字符串。
func formatInput(input []adk.Message) string {
	var sb strings.Builder
	for _, msg := range input {
		sb.WriteString(msg.Content)
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatExecutedSteps(results []ExecutedStep) string {
	var sb strings.Builder
	for _, result := range results {
		sb.WriteString(fmt.Sprintf("Step: %s\nResult: %s\n\n", result.Step, result.Result))
	}

	return sb.String()
}

func (r *replanner) Name(_ context.Context) string {
	return "Replanner"
}

func (r *replanner) Description(_ context.Context) string {
	return "a replanner agent"
}

func (r *replanner) genInput(ctx context.Context) ([]adk.Message, error) {

	executedStep, ok := adk.GetSessionValue(ctx, ExecutedStepSessionKey)
	if !ok {
		panic("impossible: execute result not found")
	}
	executedStep_ := executedStep.(string)

	plan, ok := adk.GetSessionValue(ctx, PlanSessionKey)
	if !ok {
		panic("impossible: plan not found")
	}
	plan_ := plan.(Plan)
	step := plan_.FirstStep()

	var executedSteps_ []ExecutedStep
	executedSteps, ok := adk.GetSessionValue(ctx, ExecutedStepsSessionKey)
	if ok {
		executedSteps_ = executedSteps.([]ExecutedStep)
	}

	executedSteps_ = append(executedSteps_, ExecutedStep{
		Step:   step,
		Result: executedStep_,
	})
	adk.AddSessionValue(ctx, ExecutedStepsSessionKey, executedSteps_)

	userInput, ok := adk.GetSessionValue(ctx, UserInputSessionKey)
	if !ok {
		panic("impossible: user input not found")
	}
	userInput_ := userInput.([]adk.Message)

	in := &ExecutionContext{
		UserInput:     userInput_,
		Plan:          plan_,
		ExecutedSteps: executedSteps_,
	}
	genInputFn := r.genInputFn
	if genInputFn == nil {
		genInputFn = buildGenReplannerInputFn(r.planTool.Name, r.respondTool.Name)
	}
	msgs, err := genInputFn(ctx, in)
	if err != nil {
		return nil, err
	}

	return msgs, nil
}

func (r *replanner) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				e := safe.NewPanicErr(panicErr, debug.Stack())
				generator.Send(&adk.AgentEvent{Err: e})
			}

			generator.Close()
		}()

		callOpt := model.WithToolChoice(schema.ToolChoiceForced)

		c := compose.NewChain[struct{}, any]().
			AppendLambda(
				compose.InvokableLambda(func(ctx context.Context, input struct{}) (output []adk.Message, err error) {
					return r.genInput(ctx)
				}),
			).
			AppendChatModel(r.chatModel).
			AppendLambda(
				compose.CollectableLambda(func(ctx context.Context, sr *schema.StreamReader[adk.Message]) (adk.Message, error) {
					if input.EnableStreaming {
						ss := sr.Copy(2)
						sOutput := schema.StreamReaderWithConvert(ss[0], argToContent)
						generator.Send(adk.EventFromMessage(nil, sOutput, schema.Assistant, ""))
						return schema.ConcatMessageStream(ss[1])
					}

					msg, err := schema.ConcatMessageStream(sr)
					if err != nil {
						return nil, err
					}
					if len(msg.ToolCalls) > 0 {
						output := schema.AssistantMessage(msg.ToolCalls[0].Function.Arguments, nil)
						generator.Send(adk.EventFromMessage(output, nil, schema.Assistant, ""))
					}
					return msg, nil
				}),
			).
			AppendLambda(
				compose.InvokableLambda(func(ctx context.Context, msg adk.Message) (msgOrPlan any, err error) {
					if len(msg.ToolCalls) == 0 {
						return nil, fmt.Errorf("no tool call")
					}

					// exit
					if msg.ToolCalls[0].Function.Name == r.respondTool.Name {
						action := adk.NewBreakLoopAction(r.Name(ctx))
						generator.Send(&adk.AgentEvent{Action: action})
						return msg, nil
					}

					// replan
					if msg.ToolCalls[0].Function.Name != r.planTool.Name {
						return nil, fmt.Errorf("unexpected tool call: %s", msg.ToolCalls[0].Function.Name)
					}

					plan := r.newPlan(ctx)
					if err = plan.UnmarshalJSON([]byte(msg.ToolCalls[0].Function.Arguments)); err != nil {
						return nil, fmt.Errorf("unmarshal plan error: %w", err)
					}

					adk.AddSessionValue(ctx, PlanSessionKey, plan)

					return plan, nil
				}),
			)

		runnable, err := c.Compile(ctx, compose.WithGraphName(r.Name(ctx)))
		if err != nil {
			generator.Send(&adk.AgentEvent{Err: err})
			return
		}

		_, err = runnable.Stream(ctx, struct{}{}, compose.WithChatModelOption(callOpt))
		if err != nil {
			generator.Send(&adk.AgentEvent{Err: err})
			return
		}
	}()

	return iterator
}

func buildGenReplannerInputFn(planToolName, respondToolName string) GenModelInputFn {
	return func(ctx context.Context, in *ExecutionContext) ([]adk.Message, error) {
		planContent, err := in.Plan.MarshalJSON()
		if err != nil {
			return nil, err
		}
		msgs, err := ReplannerPrompt.Format(ctx, map[string]any{
			"plan":           string(planContent),
			"input":          formatInput(in.UserInput),
			"executed_steps": formatExecutedSteps(in.ExecutedSteps),
			"plan_tool":      planToolName,
			"respond_tool":   respondToolName,
		})
		if err != nil {
			return nil, err
		}

		return msgs, nil
	}
}

// NewReplanner 根据提供的配置创建新的重规划器智能体。
// 重规划器智能体使用 model.ToolCallingChatModel 配合 PlanTool 和 RespondTool
// 来评估执行进度并决定是继续修订计划还是完成任务。
//
// 如果提供了 PlanTool，将使用它；否则使用默认的 PlanToolInfo。
// 如果提供了 RespondTool，将使用它；否则使用默认的 RespondToolInfo。
// ChatModel 将配置这两个工具以支持计划更新和响应生成。
func NewReplanner(_ context.Context, cfg *ReplannerConfig) (adk.Agent, error) {
	planTool := cfg.PlanTool
	if planTool == nil {
		planTool = &PlanToolInfo
	}

	respondTool := cfg.RespondTool
	if respondTool == nil {
		respondTool = &RespondToolInfo
	}

	chatModel, err := cfg.ChatModel.WithTools([]*schema.ToolInfo{planTool, respondTool})
	if err != nil {
		return nil, err
	}

	planParser := cfg.NewPlan
	if planParser == nil {
		planParser = defaultNewPlan
	}

	return &replanner{
		chatModel:   chatModel,
		planTool:    planTool,
		respondTool: respondTool,
		genInputFn:  cfg.GenInputFn,
		newPlan:     planParser,
	}, nil
}

// Config 提供创建计划-执行-重规划智能体的配置选项。
type Config struct {
	// Planner 指定生成计划的智能体。
	// 可以使用提供的 NewPlanner 创建规划器智能体。
	Planner adk.Agent

	// Executor 指定执行由规划器或重规划器生成的计划的智能体。
	// 可以使用提供的 NewExecutor 创建执行器智能体。
	Executor adk.Agent

	// Replanner 指定重新规划计划的智能体。
	// 可以使用提供的 NewReplanner 创建重规划器智能体。
	Replanner adk.Agent

	// MaxIterations 定义"执行-重规划"循环的最大次数。
	// 可选。如果未提供，将使用 10 作为默认值。
	MaxIterations int
}

// New 使用给定的配置创建新的计划-执行-重规划智能体。
// 计划-执行-重规划模式分为三个阶段工作：
// 1. 规划：生成包含清晰、可执行步骤的结构化计划
// 2. 执行：执行计划的第一步
// 3. 重规划：评估进度，决定完成任务或修订计划
// 这种方法通过迭代优化实现复杂问题的求解。
func New(ctx context.Context, cfg *Config) (adk.Agent, error) {
	maxIterations := cfg.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 10
	}
	loop, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
		Name:          "execute_replan",
		SubAgents:     []adk.Agent{cfg.Executor, cfg.Replanner},
		MaxIterations: maxIterations,
	})
	if err != nil {
		return nil, err
	}

	return adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
		Name:      "plan_execute_replan",
		SubAgents: []adk.Agent{cfg.Planner, loop},
	})
}
