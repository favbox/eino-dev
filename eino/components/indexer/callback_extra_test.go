package indexer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/schema"
)

func TestConvIndexer(t *testing.T) {
	assert.NotNil(t, ConvCallbackInput(&CallbackInput{}))
	assert.NotNil(t, ConvCallbackInput([]*schema.Document{}))
	assert.Nil(t, ConvCallbackInput("asd"))

	assert.NotNil(t, ConvCallbackOutput(&CallbackOutput{}))
	assert.NotNil(t, ConvCallbackOutput([]string{}))
	assert.Nil(t, ConvCallbackOutput("asd"))
}
