package main

import (
	"bufio"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDurationDefaultUnit(t *testing.T) {
	tests := []struct {
		name  string
		input string
		unit  time.Duration
		want  time.Duration
	}{
		{"bare number as hours", "48", time.Hour, 48 * time.Hour},
		{"bare number as minutes", "5", time.Minute, 5 * time.Minute},
		{"zero stays off", "0", time.Hour, 0},
		{"fractional bare number", "1.5", time.Hour, 90 * time.Minute},
		{"explicit unit wins over default", "30m", time.Hour, 30 * time.Minute},
		{"compound duration", "1h30m", time.Hour, 90 * time.Minute},
		{"surrounding spaces", "  12  ", time.Hour, 12 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDurationDefaultUnit(tt.input, tt.unit)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseDurationDefaultUnit_RejectsGarbage(t *testing.T) {
	for _, input := range []string{"", "abc", "48 hours", "h48"} {
		_, err := parseDurationDefaultUnit(input, time.Hour)
		assert.Error(t, err, "input %q must not parse", input)
	}
}

func TestPromptDuration_BareNumberUsesDefaultUnit(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("48\n"))

	got, err := promptDuration(sc, "  backfill", 0, time.Hour)

	require.NoError(t, err)
	assert.Equal(t, 48*time.Hour, got)
}

func TestPromptDuration_EmptyInputKeepsDefault(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("\n"))

	got, err := promptDuration(sc, "  poll interval", 10*time.Minute, time.Minute)

	require.NoError(t, err)
	assert.Equal(t, 10*time.Minute, got)
}

func TestPromptDuration_InvalidInputReportsLabel(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("48 hours\n"))

	_, err := promptDuration(sc, "  backfill", 0, time.Hour)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "backfill")
	assert.Contains(t, err.Error(), `"48 hours"`)
}
