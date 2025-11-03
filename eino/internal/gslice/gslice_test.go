package gslice

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToMap(t *testing.T) {
	type Foo struct {
		ID   int
		Name string
	}
	mapper := func(f Foo) (int, string) { return f.ID, f.Name }
	assert.Equal(t, map[int]string{}, ToMap([]Foo{}, mapper))
	assert.Equal(t, map[int]string{}, ToMap(nil, mapper))
	assert.Equal(t,
		map[int]string{1: "one", 2: "two", 3: "three"},
		ToMap([]Foo{{1, "one"}, {2, "two"}, {3, "three"}}, mapper))
}
