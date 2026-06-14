package features

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Pre-initialise the lingua-go detector once so individual tests are not
	// slowed down by the several-second first-call initialisation cost.
	DetectLanguage("warmup")
	os.Exit(m.Run())
}
