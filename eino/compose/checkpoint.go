/*
 * checkpoint.go - 图执行检查点系统
 *
 * 核心组件：
 *   - CheckPointStore: 检查点存储接口
 *   - Serializer: 序列化/反序列化接口
 *   - checkpoint: 检查点数据结构
 *   - checkPointer: 检查点管理器
 *   - streamConverter: 流转换器
 *
 * 设计特点：
 *   - 状态保存/恢复：支持图的中间状态持久化
 *   - 流式支持：自动处理流式和非流式数据的转换
 *   - 断点续传：支持从检查点恢复执行
 *   - 上下文传递：通过 Context 键值对管理检查点状态
 *   - 子图支持：支持嵌套图的检查点管理
 *
 * 与其他文件关系：
 *   - 为 graph_run.go 提供检查点能力
 *   - 与 dag.go/pregel.go 的通道状态协同
 *   - 支持 Workflow 的复杂状态管理
 *
 * 使用场景：
 *   - 长图执行的中间状态保存
 *   - 错误恢复和断点续传
 *   - A/B 测试和迭代调试
 *   - 分布式执行的状态同步
 */

package compose

import (
	"context"
	"fmt"

	"github.com/favbox/eino/internal/serialization"
	"github.com/favbox/eino/schema"
)

func init() {
	schema.RegisterName[*checkpoint]("_eino_checkpoint")
	schema.RegisterName[*dagChannel]("_eino_dag_channel")
	schema.RegisterName[*pregelChannel]("_eino_pregel_channel")
	schema.RegisterName[dependencyState]("_eino_dependency_state")
}

// CheckPointStore 检查点存储接口 - 定义检查点的获取和设置操作
type CheckPointStore interface {
	// Get 获取检查点数据
	Get(ctx context.Context, checkPointID string) ([]byte, bool, error)
	// Set 保存检查点数据
	Set(ctx context.Context, checkPointID string, checkPoint []byte) error
}

// Serializer 序列化器接口 - 定义对象的序列化和反序列化操作
type Serializer interface {
	// Marshal 序列化对象为字节数组
	Marshal(v any) ([]byte, error)
	// Unmarshal 反序列化字节数组为对象
	Unmarshal(data []byte, v any) error
}

// WithCheckPointStore 设置检查点存储实现
func WithCheckPointStore(store CheckPointStore) GraphCompileOption {
	return func(o *graphCompileOptions) {
		o.checkPointStore = store
	}
}

func WithSerializer(serializer Serializer) GraphCompileOption {
	return func(o *graphCompileOptions) {
		o.serializer = serializer
	}
}

// WithCheckPointID 设置检查点ID - 用于标识检查点
func WithCheckPointID(checkPointID string) Option {
	return Option{
		checkPointID: &checkPointID,
	}
}

// WithWriteToCheckPointID 设置写入的检查点ID
// 如果未提供，则使用 WithCheckPointID 中的检查点ID进行写入
// 适用于从现有检查点加载但将进度保存到新检查点的场景
func WithWriteToCheckPointID(checkPointID string) Option {
	return Option{
		writeToCheckPointID: &checkPointID,
	}
}

// WithForceNewRun 强制全新运行 - 忽略所有检查点
func WithForceNewRun() Option {
	return Option{
		forceNewRun: true,
	}
}

// StateModifier 状态修改器函数类型
type StateModifier func(ctx context.Context, path NodePath, state any) error

// WithStateModifier 设置状态修改器
func WithStateModifier(sm StateModifier) Option {
	return Option{
		stateModifier: sm,
	}
}

type checkpoint struct {
	Channels       map[string]channel
	Inputs         map[string] /*node key*/ any /*input*/
	State          any
	SkipPreHandler map[string]bool
	RerunNodes     []string

	ToolsNodeExecutedTools map[string] /*tool node key*/ map[string] /*tool call id*/ string

	SubGraphs map[string]*checkpoint
}

type nodePathKey struct{}
type stateModifierKey struct{}
type checkPointKey struct{} // *checkpoint

func getNodeKey(ctx context.Context) (*NodePath, bool) {
	if key, ok := ctx.Value(nodePathKey{}).(*NodePath); ok {
		return key, true
	}
	return nil, false
}

func setNodeKey(ctx context.Context, key string) context.Context {
	path, existed := getNodeKey(ctx)
	if !existed || len(path.path) == 0 {
		return context.WithValue(ctx, nodePathKey{}, NewNodePath(key))
	}
	return context.WithValue(ctx, nodePathKey{}, NewNodePath(append(path.path, key)...))
}

func clearNodeKey(ctx context.Context) context.Context {
	return context.WithValue(ctx, nodePathKey{}, nil)
}

func getStateModifier(ctx context.Context) StateModifier {
	if sm, ok := ctx.Value(stateModifierKey{}).(StateModifier); ok {
		return sm
	}
	return nil
}

func setStateModifier(ctx context.Context, modifier StateModifier) context.Context {
	return context.WithValue(ctx, stateModifierKey{}, modifier)
}

func getCheckPointFromStore(ctx context.Context, id string, cpr *checkPointer) (cp *checkpoint, err error) {
	cp, existed, err := cpr.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}

	return cp, nil
}

func setCheckPointToCtx(ctx context.Context, cp *checkpoint) context.Context {
	return context.WithValue(ctx, checkPointKey{}, cp)
}

func getCheckPointFromCtx(ctx context.Context) *checkpoint {
	if cp, ok := ctx.Value(checkPointKey{}).(*checkpoint); ok {
		return cp
	}
	return nil
}

func forwardCheckPoint(ctx context.Context, nodeKey string) context.Context {
	cp := getCheckPointFromCtx(ctx)
	if cp == nil {
		return ctx
	}
	if subCP, ok := cp.SubGraphs[nodeKey]; ok {
		delete(cp.SubGraphs, nodeKey) // only forward once
		return context.WithValue(ctx, checkPointKey{}, subCP)
	}
	return context.WithValue(ctx, checkPointKey{}, (*checkpoint)(nil))
}

func newCheckPointer(
	inputPairs, outputPairs map[string]streamConvertPair,
	store CheckPointStore,
	serializer Serializer,
) *checkPointer {
	if serializer == nil {
		serializer = &serialization.InternalSerializer{}
	}
	return &checkPointer{
		sc:         newStreamConverter(inputPairs, outputPairs),
		store:      store,
		serializer: serializer,
	}
}

type checkPointer struct {
	sc         *streamConverter
	store      CheckPointStore
	serializer Serializer
}

func (c *checkPointer) get(ctx context.Context, id string) (*checkpoint, bool, error) {
	data, existed, err := c.store.Get(ctx, id)
	if err != nil || existed == false {
		return nil, existed, err
	}

	cp := &checkpoint{}
	err = c.serializer.Unmarshal(data, cp)
	if err != nil {
		return nil, false, err
	}

	return cp, true, nil
}

func (c *checkPointer) set(ctx context.Context, id string, cp *checkpoint) error {
	data, err := c.serializer.Marshal(cp)
	if err != nil {
		return err
	}

	return c.store.Set(ctx, id, data)
}

// convertCheckPoint if value in checkpoint is streamReader, convert it to non-stream
func (c *checkPointer) convertCheckPoint(cp *checkpoint, isStream bool) (err error) {
	for _, ch := range cp.Channels {
		err = ch.convertValues(func(m map[string]any) error {
			return c.sc.convertOutputs(isStream, m)
		})
		if err != nil {
			return err
		}
	}

	err = c.sc.convertInputs(isStream, cp.Inputs)
	if err != nil {
		return err
	}

	return nil
}

// convertCheckPoint convert values in checkpoint to streamReader if needed
func (c *checkPointer) restoreCheckPoint(cp *checkpoint, isStream bool) (err error) {
	for _, ch := range cp.Channels {
		err = ch.convertValues(func(m map[string]any) error {
			return c.sc.restoreOutputs(isStream, m)
		})
		if err != nil {
			return err
		}
	}

	err = c.sc.restoreInputs(isStream, cp.Inputs)
	if err != nil {
		return err
	}

	return nil
}

func newStreamConverter(inputPairs, outputPairs map[string]streamConvertPair) *streamConverter {
	return &streamConverter{
		inputPairs:  inputPairs,
		outputPairs: outputPairs,
	}
}

type streamConverter struct {
	inputPairs, outputPairs map[string]streamConvertPair
}

func (s *streamConverter) convertInputs(isStream bool, values map[string]any) error {
	return convert(values, s.inputPairs, isStream)
}

func (s *streamConverter) restoreInputs(isStream bool, values map[string]any) error {
	return restore(values, s.inputPairs, isStream)
}

func (s *streamConverter) convertOutputs(isStream bool, values map[string]any) error {
	return convert(values, s.outputPairs, isStream)
}

func (s *streamConverter) restoreOutputs(isStream bool, values map[string]any) error {
	return restore(values, s.outputPairs, isStream)
}

func convert(values map[string]any, convPairs map[string]streamConvertPair, isStream bool) error {
	if !isStream {
		return nil
	}
	for key, v := range values {
		convPair, ok := convPairs[key]
		if !ok {
			return fmt.Errorf("checkpoint conv stream fail, node[%s] have not been registered", key)
		}
		sr, ok := v.(streamReader)
		if !ok {
			return fmt.Errorf("checkpoint conv stream fail, value of [%s] isn't stream", key)
		}
		nValue, err := convPair.concatStream(sr)
		if err != nil {
			return err
		}
		values[key] = nValue
	}
	return nil
}

func restore(values map[string]any, convPairs map[string]streamConvertPair, isStream bool) error {
	if !isStream {
		return nil
	}
	for key, v := range values {
		convPair, ok := convPairs[key]
		if !ok {
			return fmt.Errorf("checkpoint restore stream fail, node[%s] have not been registered", key)
		}
		sr, err := convPair.restoreStream(v)
		if err != nil {
			return err
		}
		values[key] = sr
	}
	return nil
}
