// Package session issues and verifies the stateless BFF session token.
// The token format is internal to go-srv and is not part of the wire contract.
package session

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	passphraseMarker    = "passphrase:"
	passphraseDomain    = "gatbase-session-key\x00"
	minPassphraseLength = 24
	maxPassphraseLength = 128
)

var kidPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// Keyring holds one signing key and any number of verify-only keys. Immutable.
type Keyring struct {
	signingKID    string
	signingKey    ed25519.PrivateKey
	verifyKeys    map[string]ed25519.PublicKey
	hasPassphrase bool
}

// ParseKeyring parses GATBASE_SESSION_KEYS: comma-separated "<kid>:<base64url 32-byte seed>"
// or "<kid>:passphrase:<text of 24 to 128 characters>" (local only: the seed is
// SHA-256("gatbase-session-key\x00" + text); Keyring.HasPassphrase reports it so Provide
// rejects it under an https public URL; the text cannot contain a comma). The first entry signs, every entry verifies.
// It returns every problem at once: empty value, duplicate kid, kid not matching
// ^[A-Za-z0-9._-]{1,32}$, seed not 32 bytes, passphrase out of length.
// No error text contains a seed or a passphrase.
func ParseKeyring(value string) (*Keyring, []error) {
	if strings.TrimSpace(value) == "" {
		return nil, []error{errors.New("must not be empty")}
	}
	ring := &Keyring{verifyKeys: map[string]ed25519.PublicKey{}}
	var problems []error
	for index, entry := range strings.Split(value, ",") {
		position := index + 1
		entry = strings.TrimSpace(entry)
		if entry == "" {
			problems = append(problems, fmt.Errorf("entry %d is empty", position))
			continue
		}
		kid, material, hasSeparator := strings.Cut(entry, ":")
		if !hasSeparator {
			problems = append(problems, fmt.Errorf("entry %d must be <kid>:<seed> (entries split on commas, so a passphrase cannot contain one)", position))
			continue
		}
		entryProblems := 0
		if !kidPattern.MatchString(kid) {
			problems = append(problems, fmt.Errorf("entry %d: kid must match %s", position, kidPattern))
			entryProblems++
		}
		_, isDuplicate := ring.verifyKeys[kid]
		if isDuplicate {
			problems = append(problems, fmt.Errorf("entry %d: duplicate kid", position))
			entryProblems++
		}
		seed, isPassphrase, err := parseSeed(material)
		if err != nil {
			problems = append(problems, fmt.Errorf("entry %d: %w", position, err))
			entryProblems++
		}
		if entryProblems > 0 {
			continue
		}
		ring.add(kid, seed)
		ring.hasPassphrase = ring.hasPassphrase || isPassphrase
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return ring, nil
}

// add registers a key; the first one added signs.
func (k *Keyring) add(kid string, seed []byte) {
	private := ed25519.NewKeyFromSeed(seed)
	public, _ := private.Public().(ed25519.PublicKey)
	if k.signingKey == nil {
		k.signingKID = kid
		k.signingKey = private
	}
	k.verifyKeys[kid] = public
}

// parseSeed turns the text after the kid into a 32-byte seed.
func parseSeed(material string) (seed []byte, isPassphrase bool, err error) {
	passphrase, found := strings.CutPrefix(material, passphraseMarker)
	if found {
		length := utf8.RuneCountInString(passphrase)
		isLengthOK := length >= minPassphraseLength && length <= maxPassphraseLength
		if !isLengthOK {
			return nil, true, fmt.Errorf("passphrase must be %d to %d characters", minPassphraseLength, maxPassphraseLength)
		}
		sum := sha256.Sum256([]byte(passphraseDomain + passphrase))
		return sum[:], true, nil
	}
	decoded, decodeErr := base64.RawURLEncoding.Strict().DecodeString(material)
	if decodeErr != nil || len(decoded) != ed25519.SeedSize {
		return nil, false, fmt.Errorf("seed must be %d bytes of unpadded base64url", ed25519.SeedSize)
	}
	return decoded, false, nil
}

// HasPassphrase reports whether any entry derives its seed from a passphrase.
func (k *Keyring) HasPassphrase() bool {
	return k.hasPassphrase
}
