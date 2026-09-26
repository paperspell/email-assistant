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
		unit  string
		want  time.Duration
	}{
		{"bare number as hours", "48", "h", 48 * time.Hour},
		{"bare number as minutes", "5", "m", 5 * time.Minute},
		{"zero stays off", "0", "h", 0},
		{"fractional bare number", "1.5", "h", 90 * time.Minute},
		{"explicit unit wins over default", "30m", "h", 30 * time.Minute},
		{"compound duration", "1h30m", "h", 90 * time.Minute},
		{"surrounding spaces", "  12  ", "h", 12 * time.Hour},
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
	// NaN/Inf and unitless negatives must stay errors: they are delegated to
	// time.ParseDuration rather than being completed with a unit.
	for _, input := range []string{"", "abc", "48 hours", "h48", "NaN", "Inf", "-5", "+5", "1e3"} {
		_, err := parseDurationDefaultUnit(input, "h")
		assert.Error(t, err, "input %q must not parse", input)
	}
}

func TestPromptDuration_BareNumberUsesDefaultUnit(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("48\n"))

	got, err := promptDuration(sc, "  backfill", 0, "h")

	require.NoError(t, err)
	assert.Equal(t, 48*time.Hour, got)
}

func TestPromptDuration_EmptyInputKeepsDefault(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("\n"))

	got, err := promptDuration(sc, "  poll interval", 10*time.Minute, "m")

	require.NoError(t, err)
	assert.Equal(t, 10*time.Minute, got)
}

func TestPromptDuration_InvalidInputReportsLabel(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("48 hours\n"))

	_, err := promptDuration(sc, "  backfill", 0, "h")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "backfill")
	assert.Contains(t, err.Error(), `"48 hours"`)
}

func TestKnownProvider(t *testing.T) {
	tests := []struct {
		email    string
		wantHost string
		wantOK   bool
	}{
		{"friend@gmail.com", "imap.gmail.com", true},
		{"Friend@GMAIL.com", "imap.gmail.com", true}, // domain is case-insensitive
		{"x@googlemail.com", "imap.gmail.com", true},
		{"x@outlook.com", "outlook.office365.com", true},
		{"x@icloud.com", "imap.mail.me.com", true},
		{"x@yandex.ru", "imap.yandex.ru", true},
		{"x@fastmail.com", "imap.fastmail.com", true},
		{"contact@paperspell.space", "", false}, // Workspace on a custom domain: ask
		{"contact@ppspell.com", "", false},      // hosting provider: ask
		{"not-an-address", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got, ok := knownProvider(tt.email)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantHost, got.host)
			if ok {
				assert.Equal(t, 993, got.port, "every preset is implicit TLS on 993")
			}
		})
	}
}

func TestKnownProvider_GmailSaysAppPassword(t *testing.T) {
	// The account password never works for Gmail IMAP; the wizard must say so
	// before the user types it and gets an opaque login failure.
	got, ok := knownProvider("a@gmail.com")
	require.True(t, ok)
	assert.Contains(t, got.hint, "app password")
	assert.Contains(t, got.hint, "apppasswords")
}
