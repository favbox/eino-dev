/*
 * runctx.go - 运行上下文管理
 *
 * 核心组件：
 *   - runContext: 运行上下文，记录执行路径、根输入和会话状态
 *   - runSession: 会话状态，存储事件历史、会话变量和中断信息
 *   - agentEventWrapper: 事件包装器，支持序列化和消息合并
 *
 * 设计特点：
 *   - 上下文隔离：每个智能体调用独立的 runContext，但共享 runSession
 *   - 路径追踪：RunPath 记录完整的智能体调用链
 *   - 并发安全：通过 sync.Mutex 保护共享状态
 *   - 可序列化：支持 gob 编码，用于中断恢复
 *
 * 与其他文件关系：
 *   - 为 flow.go 提供多智能体调用的上下文管理
 *   - 为 runner.go 提供会话状态的存储和恢复
 *   - 为 interrupt.go 提供中断信息的保存
 */

package adk

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"sync"

	"github.com/favbox/eino/schema"
)

// runSession 表示一次智能体运行的会话状态。
// 所有子智能体共享同一个 runSession，用于事件收集和会话变量共享。
// 并发安全，通过 mtx 保护所有字段的访问。
type runSession struct {
	Events []*agentEventWrapper
	Values map[string]any

	interruptRunCtxs []*runContext // won't consider concurrency now

	mtx sync.Mutex
}

// agentEventWrapper 包装 AgentEvent，支持序列化和流式消息合并。
// concatenatedMessage 用于序列化时缓存合并后的流式消息。
type agentEventWrapper struct {
	*AgentEvent
	mu                  sync.Mutex
	concatenatedMessage Message
}

type otherAgentEventWrapperForEncode agentEventWrapper

// GobEncode 实现 gob.GobEncoder 接口。
// 流式消息会使用 concatenatedMessage 重新包装为 StreamReader。
func (a *agentEventWrapper) GobEncode() ([]byte, error) {
	if a.concatenatedMessage != nil && a.Output != nil && a.Output.MessageOutput != nil && a.Output.MessageOutput.IsStreaming {
		a.Output.MessageOutput.MessageStream = schema.StreamReaderFromArray([]Message{a.concatenatedMessage})
	}

	buf := &bytes.Buffer{}
	err := gob.NewEncoder(buf).Encode((*otherAgentEventWrapperForEncode)(a))
	if err != nil {
		return nil, fmt.Errorf("failed to gob encode agent event wrapper: %w", err)
	}
	return buf.Bytes(), nil
}

// GobDecode 实现 gob.GobDecoder 接口。
func (a *agentEventWrapper) GobDecode(b []byte) error {
	return gob.NewDecoder(bytes.NewReader(b)).Decode((*otherAgentEventWrapperForEncode)(a))
}

func newRunSession() *runSession {
	return &runSession{
		Values: make(map[string]any),
	}
}

func getInterruptRunCtxs(ctx context.Context) []*runContext {
	session := getSession(ctx)
	if session == nil {
		return nil
	}
	return session.getInterruptRunCtxs()
}

// appendInterruptRunCtx 添加中断运行上下文到会话。
func appendInterruptRunCtx(ctx context.Context, interruptRunCtx *runContext) {
	session := getSession(ctx)
	if session == nil {
		return
	}
	session.appendInterruptRunCtx(interruptRunCtx)
}

// replaceInterruptRunCtx 替换会话中的中断运行上下文。
func replaceInterruptRunCtx(ctx context.Context, interruptRunCtx *runContext) {
	session := getSession(ctx)
	if session == nil {
		return
	}
	session.replaceInterruptRunCtx(interruptRunCtx)
}

// GetSessionValues 获取会话中的所有变量。
// 返回变量的副本，避免并发修改问题。
func GetSessionValues(ctx context.Context) map[string]any {
	session := getSession(ctx)
	if session == nil {
		return map[string]any{}
	}

	return session.getValues()
}

// AddSessionValue 添加会话变量。
// 用于在多智能体间共享数据。
func AddSessionValue(ctx context.Context, key string, value any) {
	session := getSession(ctx)
	if session == nil {
		return
	}

	session.addValue(key, value)
}

// AddSessionValues 批量添加会话变量。
func AddSessionValues(ctx context.Context, kvs map[string]any) {
	session := getSession(ctx)
	if session == nil {
		return
	}

	session.addValues(kvs)
}

// GetSessionValue 获取指定的会话变量。
// 返回值和是否存在的标识。
func GetSessionValue(ctx context.Context, key string) (any, bool) {
	session := getSession(ctx)
	if session == nil {
		return nil, false
	}

	return session.getValue(key)
}

func (rs *runSession) addEvent(event *AgentEvent) {
	rs.mtx.Lock()
	rs.Events = append(rs.Events, &agentEventWrapper{
		AgentEvent: event,
	})
	rs.mtx.Unlock()
}

func (rs *runSession) getEvents() []*agentEventWrapper {
	rs.mtx.Lock()
	events := rs.Events
	rs.mtx.Unlock()

	return events
}

func (rs *runSession) getInterruptRunCtxs() []*runContext {
	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	return rs.interruptRunCtxs
}

func (rs *runSession) appendInterruptRunCtx(runCtx *runContext) {
	rs.mtx.Lock()
	rs.interruptRunCtxs = append(rs.interruptRunCtxs, runCtx)
	rs.mtx.Unlock()
}

func (rs *runSession) replaceInterruptRunCtx(interruptRunCtx *runContext) {
	// 移除路径属于新 runCtx 的旧 runCtx，然后添加新 runCtx
	rs.mtx.Lock()
	for i := 0; i < len(rs.interruptRunCtxs); i++ {
		rc := rs.interruptRunCtxs[i]
		if belongToRunPath(interruptRunCtx.RunPath, rc.RunPath) {
			rs.interruptRunCtxs = append(rs.interruptRunCtxs[:i], rs.interruptRunCtxs[i+1:]...)
			i--
		}
	}
	rs.interruptRunCtxs = append(rs.interruptRunCtxs, interruptRunCtx)
	rs.mtx.Unlock()
}

func (rs *runSession) getValues() map[string]any {
	rs.mtx.Lock()
	values := make(map[string]any, len(rs.Values))
	for k, v := range rs.Values {
		values[k] = v
	}
	rs.mtx.Unlock()

	return values
}

func (rs *runSession) addValue(key string, value any) {
	rs.mtx.Lock()
	rs.Values[key] = value
	rs.mtx.Unlock()
}

func (rs *runSession) addValues(kvs map[string]any) {
	rs.mtx.Lock()
	for k, v := range kvs {
		rs.Values[k] = v
	}
	rs.mtx.Unlock()
}

func (rs *runSession) getValue(key string) (any, bool) {
	rs.mtx.Lock()
	value, ok := rs.Values[key]
	rs.mtx.Unlock()

	return value, ok
}

// runContext 表示智能体运行的上下文信息。
// 每个智能体调用都有独立的 runContext，但共享同一个 runSession。
type runContext struct {
	RootInput *AgentInput
	RunPath   []RunStep

	Session *runSession
}

func (rc *runContext) isRoot() bool {
	return len(rc.RunPath) == 1
}

func (rc *runContext) deepCopy() *runContext {
	copied := &runContext{
		RootInput: rc.RootInput,
		RunPath:   make([]RunStep, len(rc.RunPath)),
		Session:   rc.Session,
	}

	copy(copied.RunPath, rc.RunPath)

	return copied
}

type runCtxKey struct{}

func getRunCtx(ctx context.Context) *runContext {
	runCtx, ok := ctx.Value(runCtxKey{}).(*runContext)
	if !ok {
		return nil
	}
	return runCtx
}

func setRunCtx(ctx context.Context, runCtx *runContext) context.Context {
	return context.WithValue(ctx, runCtxKey{}, runCtx)
}

// initRunCtx 初始化运行上下文。
// 如果已有 runContext，则深拷贝并添加当前智能体到 RunPath。
// 如果是根智能体，设置 RootInput。
func initRunCtx(ctx context.Context, agentName string, input *AgentInput) (context.Context, *runContext) {
	runCtx := getRunCtx(ctx)
	if runCtx != nil {
		runCtx = runCtx.deepCopy()
	} else {
		runCtx = &runContext{Session: newRunSession()}
	}

	runCtx.RunPath = append(runCtx.RunPath, RunStep{agentName})
	if runCtx.isRoot() {
		runCtx.RootInput = input
	}

	return setRunCtx(ctx, runCtx), runCtx
}

// ClearRunCtx 清除多智能体的运行上下文。
// 当自定义智能体内部包含多智能体系统，且该自定义智能体作为另一个多智能体系统的子智能体时，
// 不应将外部的 runContext 传递给内部的多智能体系统，此函数帮助隔离上下文。
func ClearRunCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, runCtxKey{}, nil)
}

func ctxWithNewRunCtx(ctx context.Context) context.Context {
	return setRunCtx(ctx, &runContext{Session: newRunSession()})
}

func getSession(ctx context.Context) *runSession {
	runCtx := getRunCtx(ctx)
	if runCtx != nil {
		return runCtx.Session
	}

	return nil
}
