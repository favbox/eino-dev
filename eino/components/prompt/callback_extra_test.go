package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/schema"
)

func TestConvPrompt(t *testing.T) {
	assert.NotNil(t, ConvCallbackInput(&CallbackInput{}))
	assert.NotNil(t, ConvCallbackInput(map[string]any{}))
	assert.Nil(t, ConvCallbackInput("asd"))

	assert.NotNil(t, ConvCallbackOutput(&CallbackOutput{}))
	assert.NotNil(t, ConvCallbackOutput([]*schema.Message{}))
	assert.Nil(t, ConvCallbackOutput("asd"))
}
