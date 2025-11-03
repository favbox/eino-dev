package compose

/*
 * interrupt.go - 图执行打断与重运行机制
 *
 * 核心组件：
 *   - 打断配置：编译时配置打断节点（执行前/后打断）
 *   - 打断错误：InterruptAndRerun 及相关错误类型
 *   - 打断信息：InterruptInfo 存储执行状态和重运行配置
 *   - 错误处理：多种错误类型的提取和判断函数
 *
 * 设计特点：
 *   - 执行控制：在指定节点前后暂停图执行
 *   - 状态保存：捕获中断时的完整执行状态
 *   - 重运行支持：从打断点恢复执行或重新运行
 *   - 子图支持：支持嵌套图的独立打断处理
 *
 * 与其他文件关系：
 *   - 为 graph_run.go 提供执行控制能力
 *   - 与 checkpoint.go 协同保存/恢复执行状态
 *   - 支持 Workflow 的复杂打断恢复场景
 *
 * 使用场景：
 *   - 人工审核：在关键节点暂停等待确认
 *   - 错误恢复：从失败点重新执行特定节点
 *   - 调试分析：在指定位置捕获执行状态
 *   - 动态调整：根据中间结果修改后续执行路径
 */

import (
	"errors"
	"fmt"

	"github.com/favbox/eino/schema"
)

// ====== 打断配置选项 ======

// WithInterruptBeforeNodes 设置执行前打断节点 - 在指定节点执行前暂停
func WithInterruptBeforeNodes(nodes []string) GraphCompileOption {
	return func(options *graphCompileOptions) {
		options.interruptBeforeNodes = nodes
	}
}

// WithInterruptAfterNodes 设置执行后打断节点 - 在指定节点执行后暂停
func WithInterruptAfterNodes(nodes []string) GraphCompileOption {
	return func(options *graphCompileOptions) {
		options.interruptAfterNodes = nodes
	}
}

// ====== 打断与重运行错误 ======

// InterruptAndRerun 打断重运行错误标记 - 触发图执行的打断和重运行
var InterruptAndRerun = errors.New("interrupt and rerun")

// NewInterruptAndRerunErr 创建带附加信息的打断重运行错误
func NewInterruptAndRerunErr(extra any) error {
	return &interruptAndRerun{Extra: extra}
}

// interruptAndRerun 打断重运行错误结构体 - 封装附加信息
type interruptAndRerun struct {
	Extra any
}

// Error 返回错误描述字符串
func (i *interruptAndRerun) Error() string {
	return fmt.Sprintf("interrupt and rerun: %v", i.Extra)
}

// IsInterruptRerunError 检查是否为打断重运行错误并提取附加信息
func IsInterruptRerunError(err error) (any, bool) {
	if errors.Is(err, InterruptAndRerun) {
		return nil, true
	}
	ire := &interruptAndRerun{}
	if errors.As(err, &ire) {
		return ire.Extra, true
	}
	return nil, false
}

// ====== 打断信息结构 ======

// InterruptInfo 打断信息结构体 - 存储打断时的执行状态和重运行配置
type InterruptInfo struct {
	State           any                       // 执行状态快照
	BeforeNodes     []string                  // 执行前打断的节点列表
	AfterNodes      []string                  // 执行后打断的节点列表
	RerunNodes      []string                  // 需要重新执行的节点列表
	RerunNodesExtra map[string]any            // 重新执行节点的附加参数
	SubGraphs       map[string]*InterruptInfo // 子图的打断信息
}

func init() {
	schema.RegisterName[*InterruptInfo]("_eino_compose_interrupt_info") // TODO: check if this is really needed when refactoring adk resume
}

// ExtractInterruptInfo 从错误中提取打断信息
func ExtractInterruptInfo(err error) (info *InterruptInfo, existed bool) {
	if err == nil {
		return nil, false
	}
	var iE *interruptError
	if errors.As(err, &iE) {
		return iE.Info, true
	}
	var sIE *subGraphInterruptError
	if errors.As(err, &sIE) {
		return sIE.Info, true
	}
	return nil, false
}

// ====== 打断错误类型 ======

// interruptError 主图打断错误 - 包含打断信息
type interruptError struct {
	Info *InterruptInfo
}

// Error 返回错误描述字符串
func (e *interruptError) Error() string {
	return fmt.Sprintf("interrupt happened, info: %+v", e.Info)
}

// isSubGraphInterrupt 检查是否为子图打断错误
func isSubGraphInterrupt(err error) *subGraphInterruptError {
	if err == nil {
		return nil
	}
	var iE *subGraphInterruptError
	if errors.As(err, &iE) {
		return iE
	}
	return nil
}

// subGraphInterruptError 子图打断错误 - 包含打断信息和检查点
type subGraphInterruptError struct {
	Info       *InterruptInfo
	CheckPoint *checkpoint
}

// Error 返回错误描述字符串
func (e *subGraphInterruptError) Error() string {
	return fmt.Sprintf("interrupt happened, info: %+v", e.Info)
}

// ====== 错误判断辅助函数 ======

// isInterruptError 判断错误是否为任何类型的打断错误
func isInterruptError(err error) bool {
	if _, ok := ExtractInterruptInfo(err); ok {
		return true
	}
	if info := isSubGraphInterrupt(err); info != nil {
		return true
	}
	if _, ok := IsInterruptRerunError(err); ok {
		return true
	}

	return false
}
