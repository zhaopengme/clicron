package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewID returns a random 128-bit identifier encoded as lowercase hex.
// Falls back to a timestamp string if the random source fails.
func NewID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}
