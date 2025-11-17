package agent

import (
	"errors"

	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/schema"
)

// ChatModelWithTools 为聊天模型配置工具信息。
// 如果 toolInfos 为空，直接返回原模型。
//
// 使用示例：
//
//	model, err := ChatModelWithTools(myModel, toolInfos)
//	if err != nil {...}
func ChatModelWithTools(toolCallingModel model.ToolCallingChatModel,
	toolInfos []*schema.ToolInfo) (model.BaseChatModel, error) {

	if toolCallingModel == nil {
		return nil, errors.New("toolCallingModel is nil")
	}

	if len(toolInfos) == 0 {
		return toolCallingModel, nil
	}

	return toolCallingModel.WithTools(toolInfos)
}
