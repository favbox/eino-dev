/*
 * interrupt.go - 中断恢复机制实现
 *
 * 核心组件：
 *   - ResumeInfo: 恢复信息，包含中断时的状态数据
 *   - InterruptInfo: 中断信息，存储智能体特定的中断数据
 *   - serialization: 检查点序列化结构，封装 RunContext 和 InterruptInfo
 *   - mockStore: 内存检查点存储，用于测试和 AgentTool 场景
 *
 * 设计特点：
 *   - 完整性：保存 RunContext（执行路径）+ InterruptInfo（业务状态）
 *   - 透明性：使用 gob 编码，所有类型需提前注册
 *   - 灵活性：InterruptInfo.Data 为 any 类型，支持不同智能体的状态结构
 *
 * 中断恢复流程：
 *   1. 智能体生成 AgentEvent(Action.Interrupted)
 *   2. Runner.handleIter() 检测中断，调用 saveCheckPoint()
 *   3. 用户调用 Runner.Resume(checkPointID)
 *   4. getCheckPoint() 从存储恢复 RunContext 和 ResumeInfo
 *   5. Agent.Resume() 从中断点继续执行
 *
 * 与其他文件关系：
 *   - 为 runner.go 提供检查点的保存和加载能力
 *   - 为 workflow.go 提供 WorkflowInterruptInfo 的序列化支持
 *   - 为 react.go 提供 State 的序列化支持
 */

package adk

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"

	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/schema"
)

// ====== 恢复信息 ======

// ResumeInfo 包含恢复智能体执行所需的全部信息
type ResumeInfo struct {
	// EnableStreaming 是否启用流式输出，继承自原始输入
	EnableStreaming bool
	// InterruptInfo 嵌入中断信息
	*InterruptInfo
}

// InterruptInfo 存储中断时的状态数据。
// Data 类型由具体智能体决定：ChatModelAgent 存储 Graph 检查点，
// WorkflowAgent 存储执行进度，自定义智能体可存储任意可序列化结构。
type InterruptInfo struct {
	// Data 智能体特定的中断状态，类型由具体智能体决定
	Data any
}

// WithCheckPointID 设置检查点 ID，用于后续通过 Runner.Resume(checkPointID) 恢复执行
func WithCheckPointID(id string) AgentRunOption {
	return WrapImplSpecificOptFn(func(t *options) {
		t.checkPointID = &id
	})
}

func init() {
	// 注册 gob 类型名称，确保跨进程反序列化的类型一致性
	schema.RegisterName[*serialization]("_eino_adk_serialization")
	schema.RegisterName[*WorkflowInterruptInfo]("_eino_adk_workflow_interrupt_info")
	schema.RegisterName[*State]("_eino_adk_react_state")
}

// ====== 检查点序列化 ======

// serialization 是检查点的序列化结构，封装完整的恢复信息
type serialization struct {
	// RunCtx 运行上下文，包含执行路径、根输入、会话状态
	RunCtx *runContext
	// Info 中断信息，包含智能体特定的状态数据
	Info *InterruptInfo
}

// getCheckPoint 从存储中加载检查点。
// 返回运行上下文、恢复信息、检查点是否存在以及可能的错误。
func getCheckPoint(
	ctx context.Context,
	store compose.CheckPointStore,
	key string,
) (*runContext, *ResumeInfo, bool, error) {
	data, existed, err := store.Get(ctx, key)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to get checkpoint from store: %w", err)
	}
	if !existed {
		return nil, nil, false, nil
	}
	s := &serialization{}
	err = gob.NewDecoder(bytes.NewReader(data)).Decode(s)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to decode checkpoint: %w", err)
	}
	enableStreaming := false
	if s.RunCtx.RootInput != nil {
		enableStreaming = s.RunCtx.RootInput.EnableStreaming
	}
	return s.RunCtx, &ResumeInfo{
		EnableStreaming: enableStreaming,
		InterruptInfo:   s.Info,
	}, true, nil
}

// saveCheckPoint 将检查点保存到存储。
// 保存内容包括完整的运行上下文和智能体特定的中断状态，使用 gob 编码。
func saveCheckPoint(
	ctx context.Context,
	store compose.CheckPointStore,
	key string,
	runCtx *runContext,
	info *InterruptInfo,
) error {
	buf := &bytes.Buffer{}
	err := gob.NewEncoder(buf).Encode(&serialization{
		RunCtx: runCtx,
		Info:   info,
	})
	if err != nil {
		return fmt.Errorf("failed to encode checkpoint: %w", err)
	}
	return store.Set(ctx, key, buf.Bytes())
}

// ====== 内存检查点存储 ======

// mockCheckPointID 是 mockStore 使用的固定检查点 ID
const mockCheckPointID = "adk_react_mock_key"

// newEmptyStore 创建空的内存存储，用于首次运行
func newEmptyStore() *mockStore {
	return &mockStore{}
}

// newResumeStore 创建包含数据的内存存储，用于恢复运行
func newResumeStore(data []byte) *mockStore {
	return &mockStore{
		Data:  data,
		Valid: true,
	}
}

// mockStore 是内存检查点存储实现，用于测试和 AgentTool 场景。
// AgentTool 使用它将子智能体的中断信息临时存储在内存并传递到父智能体的 State。
type mockStore struct {
	// Data 存储的检查点数据
	Data []byte
	// Valid 标识数据是否有效
	Valid bool
}

// Get 实现 compose.CheckPointStore 接口，从内存获取检查点
func (m *mockStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	if m.Valid {
		return m.Data, true, nil
	}
	return nil, false, nil
}

// Set 实现 compose.CheckPointStore 接口，将检查点保存到内存
func (m *mockStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	m.Data = checkPoint
	m.Valid = true
	return nil
}
