package scheduler

import (
	"os"
	"testing"

	"github.com/paperspell/email-assistant/internal/features"
)

func TestMain(m *testing.M) {
	// Pre-initialise the lingua-go detector once so individual test timeouts
	// are not consumed by the several-second first-call initialisation cost.
	features.DetectLanguage("warmup")
	os.Exit(m.Run())
}
