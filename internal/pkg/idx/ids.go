package idx

import (
	"math/rand"

	"github.com/oklog/ulid/v2"

	"github.com/paperspell/email-assistant/internal/pkg/timex"
)

// GenerateID returns a new unique ULID string.
func GenerateID() string {
	entropy := rand.New(rand.NewSource(timex.NowUTC().UnixNano())) //nolint:gosec
	return ulid.MustNew(ulid.Timestamp(timex.NowUTC()), entropy).String()
}
