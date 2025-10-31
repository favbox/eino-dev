package retriever

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/schema"
)

func TestConvRetriever(t *testing.T) {
	assert.NotNil(t, ConvCallbackInput(&CallbackInput{}))
	assert.NotNil(t, ConvCallbackInput("asd"))
	assert.Nil(t, ConvCallbackInput([]string{}))

	assert.NotNil(t, ConvCallbackOutput(&CallbackOutput{}))
	assert.NotNil(t, ConvCallbackOutput([]*schema.Document{}))
	assert.Nil(t, ConvCallbackOutput("asd"))
}
