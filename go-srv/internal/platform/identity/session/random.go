package session

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

const randomIDBytes = 16

// NewRandomID returns 128 random bits from crypto/rand, base64url without padding (sid, csrf).
func NewRandomID() (string, error) {
	raw := make([]byte, randomIDBytes)
	_, err := rand.Read(raw)
	if err != nil {
		return "", fmt.Errorf("session: random id: %w", err)
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	return id, nil
}

// CSRFMatches reports whether header equals claims.CSRF (crypto/subtle constant time); empty never matches.
func CSRFMatches(header string, claims Claims) bool {
	if header == "" || claims.CSRF == "" {
		return false
	}
	isEqual := subtle.ConstantTimeCompare([]byte(header), []byte(claims.CSRF)) == 1
	return isEqual
}
