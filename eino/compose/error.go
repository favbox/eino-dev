package compose

/*
 * error.go - 错误处理系统实现
 *
 * 核心组件：
 *   - ErrExceedMaxSteps: 执行步骤超限错误
 *   - internalError: 内部错误结构，包含错误类型和节点路径
 *   - defaultImplAction: 执行模式转换的动作常量
 *   - 错误创建和包装函数
 *
 * 设计特点：
 *   - 错误分类：区分节点级错误和图级错误
 *   - 路径追踪：通过节点路径精确定位错误位置
 *   - 错误包装：保留原始错误信息的完整性
 *   - 模式转换：支持不同执行模式间的转换
 *
 * 与其他文件关系：
 *   - 为图执行提供完整的错误处理机制
 *   - 与 NodePath 协作支持错误路径追踪
 *   - 支持中断错误的特殊处理
 *   - 为调试和监控提供错误上下文
 *
 * 使用场景：
 *   - 复杂图结构的错误定位
 *   - 执行路径的可视化调试
 *   - 错误上下文的完整保留
 *   - 自定义错误处理策略
 */

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// ====== 基础错误定义 ======

// ErrExceedMaxSteps 执行步骤超限错误 - 当图执行的步骤数超过最大限制时抛出
// 用于防止无限循环和资源耗尽
var ErrExceedMaxSteps = errors.New("exceeds max steps")

// newUnexpectedInputTypeErr 创建意外输入类型错误 - 类型检查失败的专用错误
func newUnexpectedInputTypeErr(expected reflect.Type, got reflect.Type) error {
	return fmt.Errorf("unexpected input type. expected: %v, got: %v", expected, got)
}

// ====== 执行模式转换动作 ======

// defaultImplAction 默认实现动作类型 - 表示执行模式转换的动作标识符
type defaultImplAction string

const (
	// Invoke 模式的转换动作
	actionInvokeByStream    defaultImplAction = "InvokeByStream"    // Invoke → Stream
	actionInvokeByCollect   defaultImplAction = "InvokeByCollect"   // Invoke → Collect
	actionInvokeByTransform defaultImplAction = "InvokeByTransform" // Invoke → Transform

	// Stream 模式的转换动作
	actionStreamByInvoke    defaultImplAction = "StreamByInvoke"    // Stream → Invoke
	actionStreamByTransform defaultImplAction = "StreamByTransform" // Stream → Transform
	actionStreamByCollect   defaultImplAction = "StreamByCollect"   // Stream → Collect

	// Collect 模式的转换动作
	actionCollectByTransform defaultImplAction = "CollectByTransform" // Collect → Transform
	actionCollectByInvoke    defaultImplAction = "CollectByInvoke"    // Collect → Invoke
	actionCollectByStream    defaultImplAction = "CollectByStream"    // Collect → Stream

	// Transform 模式的转换动作
	actionTransformByStream  defaultImplAction = "TransformByStream"  // Transform → Stream
	actionTransformByCollect defaultImplAction = "TransformByCollect" // Transform → Collect
	actionTransformByInvoke  defaultImplAction = "TransformByInvoke"  // Transform → Invoke
)

// ====== 错误创建函数 ======

// newStreamReadError 创建流读取错误 - 专门处理流读取失败的错误包装
func newStreamReadError(err error) error {
	return fmt.Errorf("failed to read from stream. error: %w", err)
}

// newGraphRunError 创建图运行错误 - 将任意错误包装为图级内部错误
func newGraphRunError(err error) error {
	return &internalError{
		typ:       internalErrorTypeGraphRun,
		nodePath:  NodePath{},
		origError: err,
	}
}

// wrapGraphNodeError 包装图节点错误 - 为节点错误添加路径追踪和类型信息
// 支持中断错误的透传和内部错误的路径累积
func wrapGraphNodeError(nodeKey string, err error) error {
	// 中断错误透传：保持中断错误的原始状态
	if ok := isInterruptError(err); ok {
		return err
	}

	// 检查是否已是内部错误
	var ie *internalError
	ok := errors.As(err, &ie)
	if !ok {
		// 首次包装：创建新的内部错误
		return &internalError{
			typ:       internalErrorTypeNodeRun,
			nodePath:  NodePath{path: []string{nodeKey}},
			origError: err,
		}
	}

	// 累积节点路径：前缀添加当前节点，保持调用栈顺序
	ie.nodePath.path = append([]string{nodeKey}, ie.nodePath.path...)
	return ie
}

// ====== 内部错误类型定义 ======

// internalErrorType 内部错误类型 - 区分不同层级的错误类别
type internalErrorType string

const (
	// NodeRunError 节点运行错误 - 单个节点执行失败
	internalErrorTypeNodeRun internalErrorType = "NodeRunError"

	// GraphRunError 图运行错误 - 图整体执行失败
	internalErrorTypeGraphRun internalErrorType = "GraphRunError"
)

// ====== 内部错误结构 ======

// internalError 内部错误 - 封装错误类型、节点路径和原始错误
// 支持错误的分层分类和完整的执行路径追踪
type internalError struct {
	// typ 错误类型 - 标识错误的层级（节点级或图级）
	typ internalErrorType
	// nodePath 节点路径 - 追踪错误发生的完整调用路径
	nodePath NodePath
	// origError 原始错误 - 保留错误的具体原因
	origError error
}

// Error 实现 error 接口 - 生成包含路径信息的错误描述
func (i *internalError) Error() string {
	sb := strings.Builder{}
	sb.WriteString(string("[" + i.typ + "] "))
	sb.WriteString(i.origError.Error())

	// 添加节点路径信息（用于调试和错误定位）
	if len(i.nodePath.path) > 0 {
		sb.WriteString("\n------------------------\n")
		sb.WriteString("node path: [")
		for j := 0; j < len(i.nodePath.path)-1; j++ {
			sb.WriteString(i.nodePath.path[j] + ", ")
		}
		sb.WriteString(i.nodePath.path[len(i.nodePath.path)-1])
		sb.WriteString("]")
	}
	sb.WriteString("")
	return sb.String()
}

// Unwrap 实现错误解包 - 返回原始错误以支持错误链
func (i *internalError) Unwrap() error {
	return i.origError
}
