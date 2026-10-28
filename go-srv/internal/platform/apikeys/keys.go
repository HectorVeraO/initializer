package apikeys

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	keyPrefixLabel  = "ak_live_"
	keyIDChars      = 16
	keySecretChars  = 52
	keyIDBytes      = 10
	keySecretBytes  = 32
	keyPrefixLength = len(keyPrefixLabel) + keyIDChars
	keyLength       = keyPrefixLength + 1 + keySecretChars

	hashSchemeSHA256 = "sha256"

	statusActive  = "active"
	statusExpired = "expired"
	statusRevoked = "revoked"
)

var (
	keyPattern  = regexp.MustCompile(`^ak_live_[a-z2-7]{16}_[a-z2-7]{52}$`)
	base32Lower = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// newAPIKey returns a fresh key: the "ak_live_" label, 16 characters of id and 52
// of secret, both lower-case base32 from crypto/rand.
func newAPIKey() (string, error) {
	idPart, err := randomBase32(keyIDBytes)
	if err != nil {
		return "", err
	}
	secretPart, err := randomBase32(keySecretBytes)
	if err != nil {
		return "", err
	}
	key := keyPrefixLabel + idPart + "_" + secretPart
	return key, nil
}

func randomBase32(size int) (string, error) {
	raw := make([]byte, size)
	_, err := rand.Read(raw)
	if err != nil {
		return "", fmt.Errorf("apikeys: random api key: %w", err)
	}
	encoded := strings.ToLower(base32Lower.EncodeToString(raw))
	return encoded, nil
}

// splitAPIKey strictly parses a key into its public prefix (first 24
// characters) and its secret (last 52).
func splitAPIKey(value string) (string, string, bool) {
	isWellFormed := len(value) == keyLength && keyPattern.MatchString(value)
	if !isWellFormed {
		return "", "", false
	}
	return value[:keyPrefixLength], value[keyPrefixLength+1:], true
}

// hashAPIKeySecret is the sha256 scheme: SHA-256 over the ASCII secret.
func hashAPIKeySecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// apiKeyStatusAt derives the status of a key at now.
func apiKeyStatusAt(expiresAt, revokedAt *time.Time, now time.Time) string {
	switch {
	case revokedAt != nil:
		return statusRevoked
	case expiresAt != nil && !expiresAt.After(now):
		return statusExpired
	}
	return statusActive
}
