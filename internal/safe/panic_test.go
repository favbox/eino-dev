package safe

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewPanicErr(t *testing.T) {
	err := NewPanicErr("info", []byte("stack"))
	assert.Equal(t, "panic error: info, \nstack: stack", err.Error())
}
