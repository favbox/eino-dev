package schema

import (
	"encoding/gob"
	"reflect"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/internal/serialization"
)

func init() {
	// 注册 Eino 框架的核心类型到序列化系统
	// 确保所有内置类型都能正确持久化和恢复
	RegisterName[Message]("_eino_message")
	RegisterName[[]*Message]("_eino_message_slice")
	RegisterName[Document]("_eino_document")
	RegisterName[RoleType]("_eino_role_type")
	RegisterName[ToolCall]("_eino_tool_call")
	RegisterName[FunctionCall]("_eino_function_call")
	RegisterName[ResponseMeta]("_eino_response_meta")
	RegisterName[TokenUsage]("_eino_token_usage")
	RegisterName[LogProbs]("_eino_log_probs")
	RegisterName[ChatMessagePartType]("_eino_chat_message_type")
	RegisterName[MessageInputPart]("_eino_message_input_part")
	RegisterName[MessageInputImage]("_eino_message_input_image")
	RegisterName[MessageInputAudio]("_eino_message_input_audio")
	RegisterName[MessageInputVideo]("_eino_message_input_video")
	RegisterName[MessageInputFile]("_eino_message_input_file")
	RegisterName[MessageOutputPart]("_eino_message_output_part")
	RegisterName[MessageOutputImage]("_eino_message_output_image")
	RegisterName[MessageOutputAudio]("_eino_message_output_audio")
	RegisterName[MessageOutputVideo]("_eino_message_output_video")
	RegisterName[MessagePartCommon]("_eino_message_part_common")
	RegisterName[ImageURLDetail]("_eino_image_url_detail")
	RegisterName[PromptTokenDetails]("_eino_prompt_token_details")
}

// RegisterName - 使用指定名称注册类型到序列化系统。
//   - 用于需要在图或 ADK 检查点中持久化的类型，支持向后兼容
//   - 推荐在类型声明文件的 init() 函数中调用
func RegisterName[T any](name string) {
	gob.RegisterName(name, generic.NewInstance[T]())

	err := serialization.GenericRegister[T](name)
	if err != nil {
		panic(err)
	}
}

// getTypeName - 获取反射类型的完整名称
// 处理指针类型和命名类型的路径解析
func getTypeName(rt reflect.Type) string {
	name := rt.String()

	// 为命名类型（或其指针）添加导入路径限定
	// 解引用一个指针以查找命名类型
	star := ""
	if rt.Name() == "" {
		if pt := rt; pt.Kind() == reflect.Pointer {
			star = "*"
			rt = pt.Elem()
		}
	}
	if rt.Name() != "" {
		if rt.PkgPath() == "" {
			name = star + rt.Name()
		} else {
			name = star + rt.PkgPath() + "." + rt.Name()
		}
	}
	return name
}

// Register - 自动命名并注册类型到序列化系统
// 推荐用于新类型的注册，自动确定类型名称
// 推荐在类型声明文件的 init() 函数中调用
func Register[T any]() {
	value := generic.NewInstance[T]()

	gob.Register(value)

	name := getTypeName(reflect.TypeOf(value))

	err := serialization.GenericRegister[T](name)
	if err != nil {
		panic(err)
	}
}
