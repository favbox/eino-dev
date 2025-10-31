package embedding

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvEmbedding(t *testing.T) {
	assert.NotNil(t, ConvCallbackInput(&CallbackInput{}))
	assert.NotNil(t, ConvCallbackInput([]string{}))
	assert.Nil(t, ConvCallbackInput("asd"))

	assert.NotNil(t, ConvCallbackOutput(&CallbackOutput{}))
	assert.NotNil(t, ConvCallbackOutput([][]float64{}))
	assert.Nil(t, ConvCallbackOutput("asd"))
}
