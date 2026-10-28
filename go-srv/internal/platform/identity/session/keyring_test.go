package session_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/session"
)

func joinProblems(problems []error) string {
	var parts []string
	for _, problem := range problems {
		parts = append(parts, problem.Error())
	}
	return strings.Join(parts, " | ")
}

func TestParseKeyringReportsEveryProblemAtOnce(t *testing.T) {
	good := newTestKey(t, "ok")
	value := strings.Join([]string{
		good.entry(),
		"",
		"ok:" + strings.SplitN(good.entry(), ":", 2)[1],
		"bad kid!:" + strings.SplitN(good.entry(), ":", 2)[1],
		"short:QUJD",
		"phrase:passphrase:too-short",
	}, ",")
	ring, problems := session.ParseKeyring(value)
	if ring != nil {
		t.Fatal("a bad value produced a keyring")
	}
	text := joinProblems(problems)
	for _, want := range []string{"entry 2 is empty", "duplicate kid", "kid must match", "seed must be 32 bytes", "passphrase must be 24 to 128"} {
		if !strings.Contains(text, want) {
			t.Errorf("problems do not mention %q: %s", want, text)
		}
	}
	if strings.Contains(text, strings.SplitN(good.entry(), ":", 2)[1]) || strings.Contains(text, "too-short") {
		t.Errorf("problems leak key material: %s", text)
	}
}

func TestParseKeyringRejectsEmptyValue(t *testing.T) {
	for _, value := range []string{"", "   "} {
		ring, problems := session.ParseKeyring(value)
		if ring != nil || len(problems) == 0 {
			t.Errorf("value %q accepted", value)
		}
	}
}

func TestParseKeyringRejectsEntryWithoutSeparator(t *testing.T) {
	_, problems := session.ParseKeyring("justakid")
	if len(problems) == 0 {
		t.Fatal("entry without a seed accepted")
	}
}

func TestParseKeyringRejectsPaddedSeed(t *testing.T) {
	key := newTestKey(t, "pad")
	_, problems := session.ParseKeyring(key.entry() + "=")
	if len(problems) == 0 {
		t.Fatal("padded seed accepted")
	}
}

func TestPassphraseEntryDerivesTheSameKeyEveryTime(t *testing.T) {
	phrase := "pp-" + randomID(t) + randomID(t)
	entry := "local-1:passphrase:" + phrase
	first, problems := session.ParseKeyring(entry)
	if len(problems) > 0 || !first.HasPassphrase() {
		t.Fatalf("passphrase keyring: %v", problems)
	}
	second, _ := session.ParseKeyring(entry)
	claims := tenantlessClaims(t)
	token, err := session.NewCodec(first, clock.Fixed(testNow)).Encode(claims)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := session.NewCodec(second, clock.Fixed(testNow)).Verify(token)
	if err != nil || got.AccountID != claims.AccountID {
		t.Fatalf("second parse does not verify the first token: %v", err)
	}
}

func TestPassphraseLengthBoundaries(t *testing.T) {
	cases := map[int]bool{23: false, 24: true, 128: true, 129: false}
	for length, wantOK := range cases {
		_, problems := session.ParseKeyring("k:passphrase:" + strings.Repeat("a", length))
		if (len(problems) == 0) != wantOK {
			t.Errorf("length %d: ok=%v, want %v", length, len(problems) == 0, wantOK)
		}
	}
}

func TestSeedKeyringDoesNotReportPassphrase(t *testing.T) {
	ring, _ := session.ParseKeyring(newTestKey(t, "a").entry())
	if ring.HasPassphrase() {
		t.Fatal("seed entry reported as passphrase")
	}
}

func TestFirstEntrySignsAndEveryEntryVerifies(t *testing.T) {
	first, second := newTestKey(t, "new"), newTestKey(t, "old")
	both := newCodec(t, testNow, first, second)
	oldOnly := newCodec(t, testNow, second)
	token, err := oldOnly.Encode(tenantlessClaims(t))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	_, err = both.Verify(token)
	if err != nil {
		t.Fatalf("token of the second key rejected: %v", err)
	}
	fresh, err := both.Encode(tenantlessClaims(t))
	if err != nil {
		t.Fatalf("encode with the first key: %v", err)
	}
	_, err = oldOnly.Verify(fresh)
	if !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("token of the first key must not verify with only the second: %v", err)
	}
}

func TestUnknownKidFails(t *testing.T) {
	signer := newCodec(t, testNow, newTestKey(t, "other"))
	token, err := signer.Encode(tenantlessClaims(t))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	_, err = newCodec(t, testNow, newTestKey(t, "mine")).Verify(token)
	if !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("unknown kid: %v", err)
	}
}
