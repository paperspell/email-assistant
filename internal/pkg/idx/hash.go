package idx

import (
	"crypto/sha256"
	"encoding/base32"

	"github.com/paperspell/email-assistant/internal/pkg/flow"
)

// Hash returns the first half of the SHA-256 hash of data, base32-encoded without padding.
func Hash(data string) string {
	hasher := sha256.New()
	flow.Must(hasher.Write([]byte(data)))
	sum := hasher.Sum(nil)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:len(sum)/2])
}
