package schema

import (
	"github.com/eino-contrib/jsonschema"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// DataType - 工具参数的数据类型，遵循 OpenAPI 3.0 规范
type DataType string

const (
	Object  DataType = "object"  // 对象类型：复杂的键值对结构
	Number  DataType = "number"  // 数字类型：包含小数的数值
	Integer DataType = "integer" // 整数类型：不含小数的数值
	String  DataType = "string"  // 字符串类型：文本数据
	Array   DataType = "array"   // 数组类型：有序的元素集合
	Null    DataType = "null"    // 空值类型：表示无值
	Boolean DataType = "boolean" // 布尔类型：真/假值
)

// ToolChoice - 模型工具调用行为的控制策略
type ToolChoice string

const (
	// ToolChoiceForbidden - 禁止工具调用：模型不能调用任何工具
	// 对应 OpenAI Chat Completion 的 "none" 模式
	ToolChoiceForbidden ToolChoice = "forbidden"

	// ToolChoiceAllowed - 自主选择工具调用：模型可选择生成消息或调用工具
	// 对应 OpenAI Chat Completion 的 "auto" 模式
	ToolChoiceAllowed ToolChoice = "allowed"

	// ToolChoiceForced - 强制工具调用：模型必须调用一个或多个工具
	// 对应 OpenAI Chat Completion 的 "required" 模式
	ToolChoiceForced ToolChoice = "forced"
)

// ToolInfo - 工具的完整信息描述，用于模型理解和调用工具
type ToolInfo struct {
	// Name - 工具的唯一名称，需清晰表达工具用途
	Name string

	// Desc - 工具使用说明，指导模型何时/为何/如何使用工具。
	// 可包含示例来帮助模型理解
	Desc string

	// Extra - 工具的扩展信息，用于存储自定义元数据
	Extra map[string]any

	// ParamsOneOf - 工具参数定义。
	// 为 nil 时表示工具无需输入参数
	*ParamsOneOf
}

// ParameterInfo - 工具参数的完整描述，定义参数的类型、约束和说明
type ParameterInfo struct {
	// Type - 参数的数据类型（string, number, object 等）
	Type DataType

	// ElemInfo - 数组元素的类型信息，仅用于数组类型参数
	ElemInfo *ParameterInfo

	// SubParams - 对象的子参数集合，仅用于对象类型参数
	SubParams map[string]*ParameterInfo

	// Desc - 参数的用途说明，指导用户正确理解和使用
	Desc string

	// Enum - 参数的可选值列表，仅用于字符串类型参数
	Enum []string

	// Required - 参数是否为必填项
	Required bool
}

// ParamsOneOf - 工具参数描述的联合类型，支持三种描述方式（必须选择其中一种）
//  1. 简单描述：使用 NewParamsOneOfByParams() - 适合快速原型开发
//  2. 复杂描述：使用 NewParamsOneOfByJSONSchema() - 通用标准（跨系统集成）的 JSON 描述
type ParamsOneOf struct {
	// params - 直观的参数描述，使用 NewParamsOneOfByParams() 设置
	params map[string]*ParameterInfo

	// jsonschema - JSON Schema 标准的参数描述，使用 NewParamsOneOfByJSONSchema() 设置
	jsonschema *jsonschema.Schema
}

// NewParamsOneOfByParams - 基于 map 参数映射创建工具参数描述。
// 这是最直观且常用的参数定义方式
func NewParamsOneOfByParams(params map[string]*ParameterInfo) *ParamsOneOf {
	return &ParamsOneOf{
		params: params,
	}
}

// NewParamsOneOfByJSONSchema - 基于 JSON Schema 标准创建工具参数描述
// 适合需要跨系统集成和标准化验证的场景
func NewParamsOneOfByJSONSchema(s *jsonschema.Schema) *ParamsOneOf {
	return &ParamsOneOf{
		jsonschema: s,
	}
}

// ToJSONSchema - 将 ParamsOneOf 统一转换为 JSON Schema 格式，
// 支持从 params 转为 JSON Schema，为模型提供统一的参数描述格式。
func (p *ParamsOneOf) ToJSONSchema() (*jsonschema.Schema, error) {
	if p == nil {
		return nil, nil
	}

	if p.params != nil {
		// 从 ParameterInfo 映射转换为 JSON Schema
		sc := &jsonschema.Schema{
			Properties: orderedmap.New[string, *jsonschema.Schema](),
			Type:       string(Object),
			Required:   make([]string, 0, len(p.params)),
		}

		for k := range p.params {
			v := p.params[k]
			sc.Properties.Set(k, paramInfoToJSONSchema(v))
			if v.Required {
				sc.Required = append(sc.Required, k)
			}
		}

		return sc, nil
	}

	// 直接返回 JSON Schema（已经是目标格式）
	return p.jsonschema, nil
}

// paramInfoToJSONSchema - 将 ParameterInfo 递归转换为 JSON Schema
// 支持基本类型、枚举、数组、对象等完整结构的转换
func paramInfoToJSONSchema(paramInfo *ParameterInfo) *jsonschema.Schema {
	// 基础字段映射：类型和描述
	js := &jsonschema.Schema{
		Type:        string(paramInfo.Type),
		Description: paramInfo.Desc,
	}

	// 处理枚举值约束
	if len(paramInfo.Enum) > 0 {
		js.Enum = make([]any, len(paramInfo.Enum))
		for i, enum := range paramInfo.Enum {
			js.Enum[i] = enum
		}
	}

	// 递归处理数组元素类型
	if paramInfo.ElemInfo != nil {
		js.Items = paramInfoToJSONSchema(paramInfo.ElemInfo)
	}

	// 递归处理对象的子参数
	if len(paramInfo.SubParams) > 0 {
		required := make([]string, 0, len(paramInfo.SubParams))
		js.Properties = orderedmap.New[string, *jsonschema.Schema]()
		for k, v := range paramInfo.SubParams {
			item := paramInfoToJSONSchema(v)
			js.Properties.Set(k, item)
			if v.Required {
				required = append(required, k)
			}
		}

		js.Required = required
	}

	return js
}
