package safe

import "fmt"

// panicErr 包装 panic 信息和堆栈跟踪的错误类型。
type panicErr struct {
	info  any    // panic 信息
	stack []byte // 堆栈跟踪信息
}

func (p *panicErr) Error() string {
	return fmt.Sprintf("panic error: %v, \nstack: %s", p.info, string(p.stack))
}

// NewPanicErr 创建新的 panic 错误。
// 包装 panic 信息和堆栈跟踪，实现 error 接口，可打印完整错误信息。
func NewPanicErr(info any, stack []byte) error {
	return &panicErr{
		info,
		stack,
	}
}
