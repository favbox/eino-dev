package prompt

import "github.com/favbox/eino/schema"

type CallbackInput struct {
	Variables map[string]any
	Templates []schema.MessagesTemplate
	Extra     map[string]any
}

type CallbackOutput struct {
	Result    []*schema.Message
	Templates []schema.MessagesTemplate
	Extra     map[string]any
}
