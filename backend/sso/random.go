package sso

import (
	"crypto/rand"
	"fmt"
)

// generateRandomBytes generates n cryptographically random bytes.
func generateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("failed to read random bytes: %w", err)
	}
	return b, nil
}
