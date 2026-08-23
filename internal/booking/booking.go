package booking

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type Booking struct {
	ID    string    `json:"id"`
	Room  string    `json:"room"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("сгенерировать id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
