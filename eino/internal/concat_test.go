package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConcat(t *testing.T) {
	t.Run("连接包含空值的map块", func(t *testing.T) {
		c1 := map[string]any{
			"a": map[string]any{
				"b": map[string]any{
					"c1": nil,
					"c2": "haha0",
				},
			},
		}
		c2 := map[string]any{
			"a": map[string]any{
				"b": map[string]any{
					"c2": "c2",
				},
			},
		}
		m, err := ConcatItems([]map[string]any{c1, c2})
		assert.Nil(t, err)
		assert.Equal(t, map[string]any{
			"a": map[string]any{
				"b": map[string]any{
					"c1": nil,
					"c2": "haha0c2",
				},
			},
		}, m)
	})
}
