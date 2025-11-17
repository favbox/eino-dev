/*
 * interface.go - ADK 核心接口定义
 *
 * 核心组件：
 *   - Agent: 智能体基础接口，定义统一的运行模型
 *   - AgentEvent: 事件驱动模型，智能体通过事件流报告进度和结果
 *   - AgentAction: 控制指令，支持退出、转移、中断、循环控制
 *   - MessageVariant: 消息变体，统一处理流式和非流式消息
 *
 * 设计特点：
 *   - 事件驱动：智能体通过 AsyncIterator[*AgentEvent] 输出事件流
 *   - 流式支持：MessageVariant 封装流式和非流式消息的差异
 *   - 可序列化：所有核心类型支持 gob 编码，便于中断恢复
 *   - 可扩展：CustomizedOutput/CustomizedAction 支持自定义扩展
 *
 * 与其他文件关系：
 *   - 为 flow.go 提供智能体编排的基础抽象
 *   - 为 runner.go 提供执行引擎的输入输出定义
 *   - 为 chatmodel.go、workflow.go 提供实现规范
 */

package adk

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io"

	"github.com/favbox/eino/schema"
)

// Message 是智能体消息的类型别名，指向 schema.Message
type Message = *schema.Message

// MessageStream 是流式消息读取器的类型别名
type MessageStream = *schema.StreamReader[Message]

// ====== 消息变体 ======

// MessageVariant 统一表示流式和非流式消息，支持序列化。
// 流式消息可延迟读取，通过 GobEncode 序列化时会合并为单条消息。
type MessageVariant struct {
	// IsStreaming 标识消息是否为流式
	IsStreaming bool

	// Message 非流式消息内容，当 IsStreaming=false 时使用
	Message Message
	// MessageStream 流式消息读取器，当 IsStreaming=true 时使用
	MessageStream MessageStream

	// Role 消息角色，取值为 Assistant 或 Tool
	Role schema.RoleType
	// ToolName 工具名称，仅当 Role=Tool 时有效
	ToolName string
}

// EventFromMessage 从消息或消息流构造 AgentEvent。
// 当 msgStream 非 nil 时，优先使用 msgStream。
func EventFromMessage(msg Message, msgStream MessageStream,
	role schema.RoleType, toolName string) *AgentEvent {
	return &AgentEvent{
		Output: &AgentOutput{
			MessageOutput: &MessageVariant{
				IsStreaming:   msgStream != nil,
				Message:       msg,
				MessageStream: msgStream,
				Role:          role,
				ToolName:      toolName,
			},
		},
	}
}

type messageVariantSerialization struct {
	IsStreaming   bool
	Message       Message
	MessageStream Message
}

// GobEncode 实现 gob.GobEncoder 接口。
// 流式消息会读取所有帧并合并为单条消息后编码。
func (mv *MessageVariant) GobEncode() ([]byte, error) {
	s := &messageVariantSerialization{
		IsStreaming: mv.IsStreaming,
		Message:     mv.Message,
	}
	if mv.IsStreaming {
		var messages []Message
		for {
			frame, err := mv.MessageStream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("error receiving message stream: %w", err)
			}
			messages = append(messages, frame)
		}
		m, err := schema.ConcatMessages(messages)
		if err != nil {
			return nil, fmt.Errorf("failed to encode message: cannot concat message stream: %w", err)
		}
		s.MessageStream = m
	}
	buf := &bytes.Buffer{}
	err := gob.NewEncoder(buf).Encode(s)
	if err != nil {
		return nil, fmt.Errorf("failed to gob encode message variant: %w", err)
	}
	return buf.Bytes(), nil
}

// GobDecode 实现 gob.GobDecoder 接口。
// 流式消息被反序列化为单条消息后包装为 StreamReader。
func (mv *MessageVariant) GobDecode(b []byte) error {
	s := &messageVariantSerialization{}
	err := gob.NewDecoder(bytes.NewReader(b)).Decode(s)
	if err != nil {
		return fmt.Errorf("failed to decoding message variant: %w", err)
	}
	mv.IsStreaming = s.IsStreaming
	mv.Message = s.Message
	if s.MessageStream != nil {
		mv.MessageStream = schema.StreamReaderFromArray([]*schema.Message{s.MessageStream})
	}
	return nil
}

// GetMessage 获取完整消息内容。
// 流式消息会合并所有帧，非流式消息直接返回。
func (mv *MessageVariant) GetMessage() (Message, error) {
	var message Message
	if mv.IsStreaming {
		var err error
		message, err = schema.ConcatMessageStream(mv.MessageStream)
		if err != nil {
			return nil, err
		}
	} else {
		message = mv.Message
	}

	return message, nil
}

// ====== 智能体输出与动作 ======

// AgentAction 表示智能体执行的控制动作。
// 支持退出、中断、转移、跳出循环等操作，可通过 CustomizedAction 扩展。
type AgentAction struct {
	// Exit 为 true 时表示退出当前智能体
	Exit bool

	// Interrupted 中断信息，非 nil 时表示需要中断并保存状态
	Interrupted *InterruptInfo

	// TransferToAgent 转移到其他智能体，非 nil 时执行转移
	TransferToAgent *TransferToAgentAction

	// BreakLoop 跳出循环动作，非 nil 时终止循环
	BreakLoop *BreakLoopAction

	// CustomizedAction 自定义动作，用于扩展特定场景的控制逻辑
	CustomizedAction any
}

// TransferToAgentAction 表示转移到其他智能体的动作
type TransferToAgentAction struct {
	// DestAgentName 目标智能体名称
	DestAgentName string
}

// AgentOutput 表示智能体的输出结果
type AgentOutput struct {
	// MessageOutput 标准消息输出
	MessageOutput *MessageVariant

	// CustomizedOutput 自定义输出，用于扩展特定场景的输出格式
	CustomizedOutput any
}

// NewTransferToAgentAction 创建转移到指定智能体的动作
func NewTransferToAgentAction(destAgentName string) *AgentAction {
	return &AgentAction{TransferToAgent: &TransferToAgentAction{DestAgentName: destAgentName}}
}

// NewExitAction 创建退出动作
func NewExitAction() *AgentAction {
	return &AgentAction{Exit: true}
}

// ====== 运行路径追踪 ======

// RunStep 表示执行路径中的一个步骤，记录智能体名称
type RunStep struct {
	agentName string
}

// String 实现 Stringer 接口，返回智能体名称
func (r *RunStep) String() string {
	return r.agentName
}

// Equals 判断两个 RunStep 是否相等
func (r *RunStep) Equals(r1 RunStep) bool {
	return r.agentName == r1.agentName
}

// GobEncode 实现 gob.GobEncoder 接口
func (r *RunStep) GobEncode() ([]byte, error) {
	s := &runStepSerialization{AgentName: r.agentName}
	buf := &bytes.Buffer{}
	err := gob.NewEncoder(buf).Encode(s)
	if err != nil {
		return nil, fmt.Errorf("failed to gob encode RunStep: %w", err)
	}
	return buf.Bytes(), nil
}

// GobDecode 实现 gob.GobDecoder 接口
func (r *RunStep) GobDecode(b []byte) error {
	s := &runStepSerialization{}
	err := gob.NewDecoder(bytes.NewReader(b)).Decode(s)
	if err != nil {
		return fmt.Errorf("failed to gob decode RunStep: %w", err)
	}
	r.agentName = s.AgentName
	return nil
}

type runStepSerialization struct {
	AgentName string
}

// ====== 事件模型 ======

// AgentEvent 表示智能体运行过程中产生的事件。
// 用于报告执行进度、传递控制指令、追踪调用路径和上报错误。
type AgentEvent struct {
	// AgentName 产生此事件的智能体名称
	AgentName string

	// RunPath 完整的执行路径，记录从根智能体到当前智能体的调用链
	RunPath []RunStep

	// Output 智能体的输出结果
	Output *AgentOutput

	// Action 控制动作，指示下一步执行逻辑
	Action *AgentAction

	// Err 错误信息，非 nil 时表示执行失败
	Err error
}

// ====== 智能体接口 ======

// AgentInput 表示智能体的输入参数
type AgentInput struct {
	// Messages 输入消息列表
	Messages []Message
	// EnableStreaming 是否启用流式输出
	EnableStreaming bool
}

// Agent 是智能体的核心接口，定义统一的运行模型。
// 采用事件驱动设计，通过 AsyncIterator[*AgentEvent] 输出事件流。
// 每次 Run 调用独立执行，状态由 RunContext 管理，支持任意嵌套组合。
//
//go:generate  mockgen -destination ../internal/mock/adk/Agent_mock.go --package adk -source interface.go
type Agent interface {
	// Name 返回智能体的唯一名称，用于多智能体场景下的路由和识别
	Name(ctx context.Context) string

	// Description 返回智能体的功能描述，帮助其他智能体判断是否转移任务
	Description(ctx context.Context) string

	// Run 运行智能体，返回事件流。
	// 返回的 AgentEvent 必须是独立副本，MessageStream 必须是独占的。
	// 建议对 MessageStream 调用 SetAutomaticClose() 确保资源自动释放。
	Run(ctx context.Context, input *AgentInput, options ...AgentRunOption) *AsyncIterator[*AgentEvent]
}

// OnSubAgents 定义智能体的层次关系回调接口。
// 用于多智能体系统中建立父子关系和控制转移权限。
type OnSubAgents interface {
	// OnSetSubAgents 当智能体被设置子智能体时回调。
	// 用于初始化子智能体相关逻辑，如将子智能体注册为工具。
	OnSetSubAgents(ctx context.Context, subAgents []Agent) error

	// OnSetAsSubAgent 当智能体被设置为子智能体时回调。
	// 用于子智能体感知父智能体并建立向上转移能力。
	OnSetAsSubAgent(ctx context.Context, parent Agent) error

	// OnDisallowTransferToParent 禁止子智能体向父智能体转移时回调。
	// 用于 WorkflowAgent 等场景。
	OnDisallowTransferToParent(ctx context.Context) error
}

// ResumableAgent 支持中断恢复的智能体接口。
// 适用于长时间运行、需要人工审批或需要应对系统故障的场景。
type ResumableAgent interface {
	Agent

	// Resume 从中断点恢复智能体执行。
	// info.Data 包含智能体特定的中断状态，RunContext 由调用者恢复并注入。
	Resume(ctx context.Context, info *ResumeInfo, opts ...AgentRunOption) *AsyncIterator[*AgentEvent]
}
