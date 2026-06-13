package idx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateID_NonEmpty(t *testing.T) {
	id := GenerateID()
	assert.NotEmpty(t, id)
}

func TestGenerateID_Unique(t *testing.T) {
	ids := make(map[string]struct{}, 100)
	for range 100 {
		id := GenerateID()
		_, exists := ids[id]
		assert.False(t, exists, "duplicate ID: %s", id)
		ids[id] = struct{}{}
	}
}

func TestHash_Deterministic(t *testing.T) {
	h1 := Hash("hello")
	h2 := Hash("hello")
	assert.Equal(t, h1, h2)
}

func TestHash_DifferentInputs(t *testing.T) {
	assert.NotEqual(t, Hash("hello"), Hash("world"))
}

func TestHash_NonEmpty(t *testing.T) {
	assert.NotEmpty(t, Hash("test"))
}
