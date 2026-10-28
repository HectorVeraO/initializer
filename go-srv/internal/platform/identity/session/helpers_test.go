package session_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/session"
)

var testNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

const testKid = "test-a"

// testKey is a fresh random signing key and the keyring text that holds it.
type testKey struct {
	kid  string
	seed []byte
}

func newTestKey(t testing.TB, kid string) testKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	return testKey{kid: kid, seed: seed}
}

func (k testKey) entry() string {
	return k.kid + ":" + base64.RawURLEncoding.EncodeToString(k.seed)
}

func (k testKey) sign(input string) string {
	private := ed25519.NewKeyFromSeed(k.seed)
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, []byte(input)))
}

// forge signs arbitrary header and claims objects with the key, for tokens the
// codec itself would never produce.
func (k testKey) forge(t testing.TB, header, claims any) string {
	t.Helper()
	h, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	c, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	return input + "." + k.sign(input)
}

func (k testKey) goodHeader() map[string]any {
	return map[string]any{"alg": "Ed25519", "typ": "JWT", "kid": k.kid}
}

func newCodec(t testing.TB, at time.Time, keys ...testKey) *session.Codec {
	t.Helper()
	entries := ""
	for index, key := range keys {
		if index > 0 {
			entries += ","
		}
		entries += key.entry()
	}
	ring, problems := session.ParseKeyring(entries)
	if len(problems) > 0 {
		t.Fatalf("keyring: %v", problems)
	}
	return session.NewCodec(ring, clock.Fixed(at))
}

func randomID(t testing.TB) string {
	t.Helper()
	id, err := session.NewRandomID()
	if err != nil {
		t.Fatalf("random id: %v", err)
	}
	return id
}

func ptr(s string) *string {
	return &s
}

func tenantlessClaims(t testing.TB) session.Claims {
	t.Helper()
	return session.Claims{
		AccountID:       randomID(t),
		SessionID:       randomID(t),
		KratosSessionID: randomID(t),
		CSRF:            randomID(t),
		IssuedAt:        testNow,
		ExpiresAt:       testNow.Add(10 * time.Minute),
	}
}

func tenantClaims(t testing.TB) session.Claims {
	c := tenantlessClaims(t)
	c.TenantID = ptr(randomID(t))
	c.TenantSlug = ptr("acme")
	c.Role = ptr("admin")
	c.PlatformOperator = true
	return c
}

// rawClaims is a valid claim object as a map, for tests that break one key.
func rawClaims(t testing.TB) map[string]any {
	t.Helper()
	return map[string]any{
		"iss": "platform-bff", "aud": "platform-bff",
		"sub": randomID(t), "sid": randomID(t), "ksid": randomID(t),
		"tid": nil, "tslug": nil, "role": nil, "plat": false,
		"csrf": randomID(t),
		"iat":  testNow.Unix(), "exp": testNow.Add(10 * time.Minute).Unix(),
	}
}
