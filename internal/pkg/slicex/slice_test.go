package slicex

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAny_True(t *testing.T) {
	assert.True(t, Any([]int{1, 2, 3}, func(n int) bool { return n == 2 }))
}

func TestAny_False(t *testing.T) {
	assert.False(t, Any([]int{1, 2, 3}, func(n int) bool { return n == 9 }))
}

func TestAny_Empty(t *testing.T) {
	assert.False(t, Any([]int{}, func(_ int) bool { return true }))
}

func TestAll_True(t *testing.T) {
	assert.True(t, All([]int{2, 4, 6}, func(n int) bool { return n%2 == 0 }))
}

func TestAll_False(t *testing.T) {
	assert.False(t, All([]int{2, 3, 6}, func(n int) bool { return n%2 == 0 }))
}

func TestAll_Empty(t *testing.T) {
	assert.True(t, All([]int{}, func(_ int) bool { return false }))
}

func TestMerge(t *testing.T) {
	result := Merge([]int{1, 2}, []int{3, 4}, []int{5})
	assert.Equal(t, []int{1, 2, 3, 4, 5}, result)
}

func TestMerge_Empty(t *testing.T) {
	assert.Empty(t, Merge[int]())
}

func TestTransform(t *testing.T) {
	result := Transform([]int{1, 2, 3}, func(_ int) string {
		return "x"
	})
	assert.Equal(t, []string{"x", "x", "x"}, result)
}

func TestFilter(t *testing.T) {
	result := Filter([]int{1, 2, 3, 4, 5}, func(n int) bool { return n%2 == 0 })
	assert.Equal(t, []int{2, 4}, result)
}

func TestFilter_NoneMatch(t *testing.T) {
	result := Filter([]int{1, 3, 5}, func(n int) bool { return n%2 == 0 })
	assert.Empty(t, result)
}

func TestFirst_NonEmpty(t *testing.T) {
	assert.Equal(t, 42, First([]int{42, 99}))
}

func TestFirst_Empty(t *testing.T) {
	assert.Equal(t, 0, First([]int{}))
}

func TestGet_ValidIndex(t *testing.T) {
	assert.Equal(t, "b", Get([]string{"a", "b", "c"}, 1))
}

func TestGet_OutOfBounds(t *testing.T) {
	assert.Equal(t, "", Get([]string{"a"}, 5))
}

func TestGet_NegativeIndex(t *testing.T) {
	assert.Equal(t, 0, Get([]int{1, 2, 3}, -1))
}

func TestGroupBy(t *testing.T) {
	words := []string{"cat", "car", "bar", "bat"}
	groups := GroupBy(words, func(s string) byte { return s[0] })
	assert.ElementsMatch(t, []string{"cat", "car"}, groups['c'])
	assert.ElementsMatch(t, []string{"bar", "bat"}, groups['b'])
}
