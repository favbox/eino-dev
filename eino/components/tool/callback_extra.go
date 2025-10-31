package tool

import "github.com/favbox/eino/callbacks"

// CallbackInput 定义了工具回调的输入参数。
//
// 在工具的 OnStart 回调中，这个结构体将被传递给回调处理器，
// 包含了工具执行所需的参数和上下文信息。
//
// 设计理念：
// 工具组件的回调输入比模型组件更简单，
// 因为工具的主要任务是执行特定功能，输入主要是调用参数。
type CallbackInput struct {
	// ArgumentsInJSON 是工具调用的参数，格式为 JSON 字符串。
	//
	// 这是工具执行的核心输入，包含：
	//   - 工具所需的参数名和参数值
	//   - 由工具定义中的参数模式（JSON Schema）验证
	//   - 以字符串形式传递，便于序列化和反序列化
	//
	// 典型示例：
	//   - 搜索工具：{"query": "人工智能发展", "limit": 10}
	//   - 计算工具：{"expression": "10 * 5 + 3"}
	//   - 天气查询工具：{"city": "北京", "unit": "celsius"}
	//
	// 注意事项：
	//   - 必须是有效的 JSON 格式
	//   - 参数结构和验证规则由工具定义决定
	//   - 空字符串表示无参数或参数未提供
	//
	// 回调处理器可以使用此字段：
	//   - 记录工具调用的参数
	//   - 验证参数合理性
	//   - 分析工具使用模式
	ArgumentsInJSON string

	// Extra 是额外的回调信息。
	//
	// 用于传递与工具调用相关的额外数据，
	// 如：
	//   - 调用时间戳
	//   - 调用来源标识
	//   - 请求链路 ID
	//   - 用户身份标识
	//   - 业务上下文信息
	//
	// 这个字段提供了灵活性，
	// 允许在不修改核心结构的情况下添加新信息。
	//
	// 使用场景：
	//   - 审计日志记录
	//   - 性能监控
	//   - 错误追踪
	//   - 业务指标统计
	//
	// 常见键值对示例：
	//   - "trace_id": "abc-123-def"
	//   - "user_id": "user_456"
	//   - "source": "chat_interface"
	//   - "priority": "high"
	Extra map[string]any
}

// CallbackOutput 定义了工具回调的输出结果。
//
// 在工具的 OnEnd 回调中，这个结构体将被传递给回调处理器，
// 包含了工具执行的结果和状态信息。
//
// 设计理念：
// 工具组件的回调输出设计为简单直接的字符串格式，
// 因为工具的主要任务是产生结果，结果可以是任意格式的文本。
type CallbackOutput struct {
	// Response 是工具执行后的响应内容。
	//
	// 这是工具的最终输出结果，包含：
	//   - 工具执行的结果数据
	//   - 可以是文本、JSON、XML 等任意字符串格式
	//   - 由工具实现决定具体的格式和内容
	//
	// 典型示例：
	//   - 搜索工具：返回搜索结果的 JSON 字符串或格式化文本
	//   - 计算工具：返回计算结果的数字或表达式结果
	//   - 天气查询工具：返回天气信息的文本描述或 JSON 数据
	//   - 数据库查询工具：返回查询结果的表格或 JSON 格式
	//
	// 格式说明：
	//   - 纯文本：人类可读的描述性信息
	//   - JSON：结构化数据，便于程序解析
	//   - XML：另一种结构化数据格式
	//   - Markdown：带格式的文本（表格、列表等）
	//
	// 回调处理器可以使用此字段：
	//   - 记录工具执行结果
	//   - 分析工具输出质量
	//   - 监控工具执行时间
	//   - 验证结果格式和内容
	//
	// 注意事项：
	//   - 如果工具执行失败，此字段可能包含错误信息
	//   - 空字符串可能表示无结果或结果被截断
	//   - 长度可能有限制（由工具实现或回调系统决定）
	Response string

	// Extra 是额外的回调输出信息。
	//
	// 用于传递与工具执行结果相关的额外数据，
	// 如：
	//   - 执行耗时（毫秒）
	//   - 结果大小（字节）
	//   - 执行状态码
	//   - 错误信息（如果有）
	//   - 业务特定标识
	//
	// 这个字段与 CallbackInput.Extra 对应，
	// 允许在回调链路中传递和修改信息。
	//
	// 常见键值对示例：
	//   - "duration_ms": "123.45"
	//   - "status": "success"
	//   - "error_code": "E001"
	//   - "result_size": "1024"
	//   - "cache_hit": "false"
	//
	// 使用场景：
	//   - 性能分析和优化
	//   - 成本监控和统计
	//   - 错误诊断和排查
	//   - 业务指标收集
	Extra map[string]any
}

// ConvCallbackInput 将通用回调输入转换为工具特定的回调输入。
//
// 这是类型转换函数，用于在回调系统中统一不同来源的输入类型。
//
// 转换逻辑：
//  1. 如果输入已经是 *CallbackInput（组件实现内触发），直接返回
//  2. 如果输入是 string（图节点注入），将其包装为 *CallbackInput
//  3. 其他类型返回 nil
//
// 使用场景：
//   - 当回调被图形编排节点注入时
//   - 当在组件内部直接触发回调时
//   - 需要统一处理不同来源的回调输入时
//
// 设计特点：
// 工具组件的输入转换比模型组件更简单，
// 因为工具的参数本身就是字符串格式（JSON）。
// 这使得类型转换更加直接和高效。
//
// 参数：
//   - src: 通用回调输入（callbacks.CallbackInput）
//
// 返回：
//   - *CallbackInput: 工具特定的回调输入，或 nil（如果类型不匹配）
//
// 示例：
//
//	// 在组件实现内
//	input := &tool.CallbackInput{ArgumentsInJSON: `{"query": "test"}`}
//	converted := tool.ConvCallbackInput(input)
//	// converted == input
//
//	// 在图形节点中
//	input := `{"query": "人工智能"}`
//	converted := tool.ConvCallbackInput(input)
//	// converted 是新的 *CallbackInput，ArgumentsInJSON 字段被设置
//
//	// 错误类型
//	input := 123
//	converted := tool.ConvCallbackInput(input)
//	// converted == nil
func ConvCallbackInput(src callbacks.CallbackInput) *CallbackInput {
	switch t := src.(type) {
	case *CallbackInput: // 当回调在组件实现内触发时，输入通常已经是类型化的 *tool.CallbackInput
		return t
	case string: // 当回调被图形节点注入（不是组件实现自身触发）时，输入是字符串格式的工具参数
		return &CallbackInput{ArgumentsInJSON: t}
	default:
		return nil
	}
}

// ConvCallbackOutput 将通用回调输出转换为工具特定的回调输出。
//
// 这是类型转换函数，用于在回调系统中统一不同来源的输出类型。
//
// 转换逻辑：
//  1. 如果输出已经是 *CallbackOutput（组件实现内触发），直接返回
//  2. 如果输出是 string（图节点注入），将其包装为 *CallbackOutput
//  3. 其他类型返回 nil
//
// 使用场景：
//   - 当回调被图形编排节点注入时
//   - 当在组件内部直接触发回调时
//   - 需要统一处理不同来源的回调输出时
//
// 设计特点：
// 工具组件的输出转换也非常简单，
// 因为工具的结果本身就是字符串格式。
// 这保持了工具组件的简洁性和高效性。
//
// 参数：
//   - src: 通用回调输出（callbacks.CallbackOutput）
//
// 返回：
//   - *CallbackOutput: 工具特定的回调输出，或 nil（如果类型不匹配）
//
// 示例：
//
//	// 在组件实现内
//	output := &tool.CallbackOutput{Response: "搜索结果：..."}
//	converted := tool.ConvCallbackOutput(output)
//	// converted == output
//
//	// 在图形节点中
//	output := "计算结果：42"
//	converted := tool.ConvCallbackOutput(output)
//	// converted 是新的 *CallbackOutput，Response 字段被设置
//
//	// 错误类型
//	output := []string{"result"}
//	converted := tool.ConvCallbackOutput(output)
//	// converted == nil
func ConvCallbackOutput(src callbacks.CallbackOutput) *CallbackOutput {
	switch t := src.(type) {
	case *CallbackOutput: // 当回调在组件实现内触发时，输出通常已经是类型化的 *tool.CallbackOutput
		return t
	case string: // 当回调被图形节点注入（不是组件实现自身触发）时，输出是字符串格式的工具响应
		return &CallbackOutput{Response: t}
	default:
		return nil
	}
}
