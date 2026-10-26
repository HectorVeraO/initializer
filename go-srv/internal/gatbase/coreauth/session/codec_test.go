package session_test

import (
	"encoding/base64"
	"errors"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/session"
)

func mustEncode(t *testing.T, codec *session.Codec, claims session.Claims) string {
	t.Helper()
	token, err := codec.Encode(claims)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return token
}

func TestRoundTripTenantlessAndTenantClaims(t *testing.T) {
	codec := newCodec(t, testNow, newTestKey(t, testKid))
	for name, claims := range map[string]session.Claims{
		"tenantless": tenantlessClaims(t),
		"tenant":     tenantClaims(t),
	} {
		got, err := codec.Verify(mustEncode(t, codec, claims))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got, claims) {
			t.Errorf("%s: got %+v want %+v", name, got, claims)
		}
	}
}

func TestRoundTripProperty(t *testing.T) {
	codec := newCodec(t, testNow, newTestKey(t, testKid))
	roles := []string{"admin", "member", "viewer"}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		claims := tenantlessClaims(t)
		if rng.IntN(2) == 0 {
			claims.TenantID = ptr(randomID(t))
			claims.TenantSlug = ptr("t" + randomID(t)[:6])
			claims.Role = ptr(roles[rng.IntN(len(roles))])
		}
		claims.PlatformOperator = rng.IntN(2) == 0
		claims.IssuedAt = testNow.Add(-time.Duration(rng.IntN(300)) * time.Second)
		claims.ExpiresAt = testNow.Add(time.Duration(1+rng.IntN(900)) * time.Second)
		got, err := codec.Verify(mustEncode(t, codec, claims))
		if err != nil || !reflect.DeepEqual(got, claims) {
			t.Fatalf("round trip failed: %v\n got %+v\nwant %+v", err, got, claims)
		}
	}
}

func TestEncodeDropsSubSecondPrecision(t *testing.T) {
	codec := newCodec(t, testNow, newTestKey(t, testKid))
	claims := tenantlessClaims(t)
	claims.IssuedAt = testNow.Add(500 * time.Millisecond)
	claims.ExpiresAt = claims.IssuedAt.Add(time.Minute)
	got, err := codec.Verify(mustEncode(t, codec, claims))
	if err != nil || !got.IssuedAt.Equal(testNow) {
		t.Fatalf("got %v %v", got.IssuedAt, err)
	}
}

func TestEncodeRejectsATTLThatTruncatesToZeroSeconds(t *testing.T) {
	codec := newCodec(t, testNow, newTestKey(t, testKid))
	claims := tenantlessClaims(t)
	claims.IssuedAt = testNow
	claims.ExpiresAt = testNow.Add(500 * time.Millisecond)
	_, err := codec.Encode(claims)
	if err == nil {
		t.Fatal("sub-second TTL encoded into a token Verify would reject")
	}
}

func TestEncodeRejectsInvalidClaims(t *testing.T) {
	codec := newCodec(t, testNow, newTestKey(t, testKid))
	bad := tenantClaims(t)
	bad.Role = ptr("root")
	_, err := codec.Encode(bad)
	if err == nil {
		t.Fatal("unknown role encoded")
	}
}

func TestRule1RequiresThreeSegmentsAndBoundedSize(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	token := mustEncode(t, codec, tenantlessClaims(t))
	parts := strings.Split(token, ".")
	cases := map[string]string{
		"empty":         "",
		"two segments":  parts[0] + "." + parts[1],
		"four segments": token + ".AAAA",
		"oversized":     token + strings.Repeat("A", 4096),
		"padded sig":    parts[0] + "." + parts[1] + "." + parts[2] + "==",
	}
	for name, bad := range cases {
		_, err := codec.Verify(bad)
		if !errors.Is(err, session.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRule1OversizedTokenWithValidSignatureIsRejected(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	claims := rawClaims(t)
	claims["csrf"] = strings.Repeat("x", 4200)
	_, err := codec.Verify(key.forge(t, key.goodHeader(), claims))
	if !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestRule2HeaderMustBeExactlyAlgTypKid(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	with := func(mutate func(h map[string]any)) map[string]any {
		h := key.goodHeader()
		mutate(h)
		return h
	}
	cases := map[string]map[string]any{
		"alg none":      with(func(h map[string]any) { h["alg"] = "none" }),
		"alg EdDSA":     with(func(h map[string]any) { h["alg"] = "EdDSA" }),
		"alg lowercase": with(func(h map[string]any) { h["alg"] = "ed25519" }),
		"typ other":     with(func(h map[string]any) { h["typ"] = "JWS" }),
		"extra field":   with(func(h map[string]any) { h["jku"] = "https://example.com/keys" }),
		"missing typ":   with(func(h map[string]any) { delete(h, "typ") }),
		"missing kid":   with(func(h map[string]any) { delete(h, "kid") }),
		"kid not text":  with(func(h map[string]any) { h["kid"] = 7 }),
	}
	for name, header := range cases {
		_, err := codec.Verify(key.forge(t, header, rawClaims(t)))
		if !errors.Is(err, session.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRule2ForgedGoodHeaderBaselineVerifies(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	_, err := codec.Verify(key.forge(t, key.goodHeader(), rawClaims(t)))
	if err != nil {
		t.Fatalf("baseline forged token rejected: %v", err)
	}
}

func TestRule3KidMustNameAKeyInTheKeyring(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	header := key.goodHeader()
	header["kid"] = "not-in-ring"
	_, err := codec.Verify(key.forge(t, header, rawClaims(t)))
	if !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestRule4SignatureIsCheckedBeforeClaims(t *testing.T) {
	key, attacker := newTestKey(t, testKid), newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	// Claims are well formed, only the signing key differs: the reason must
	// be the signature, not a claim rule.
	_, err := codec.Verify(attacker.forge(t, attacker.goodHeader(), rawClaims(t)))
	var verifyErr *session.VerifyError
	if !errors.As(err, &verifyErr) || verifyErr.Reason != "bad signature" {
		t.Fatalf("got %v", err)
	}
	// Broken claims under a bad signature still report the signature.
	broken := rawClaims(t)
	broken["extra"] = 1
	_, err = codec.Verify(attacker.forge(t, attacker.goodHeader(), broken))
	if !errors.As(err, &verifyErr) || verifyErr.Reason != "bad signature" {
		t.Fatalf("claims read before the signature check: %v", err)
	}
}

func flipByte(t *testing.T, segment string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[len(raw)/2] ^= 0x01
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestTamperOfAnySegmentByOneByteIsRejected(t *testing.T) {
	codec := newCodec(t, testNow, newTestKey(t, testKid))
	parts := strings.Split(mustEncode(t, codec, tenantClaims(t)), ".")
	for index, name := range []string{"header", "claims", "signature"} {
		tampered := append([]string(nil), parts...)
		tampered[index] = flipByte(t, parts[index])
		_, err := codec.Verify(strings.Join(tampered, "."))
		if !errors.Is(err, session.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNonCanonicalSignatureEncodingIsRejected(t *testing.T) {
	codec := newCodec(t, testNow, newTestKey(t, testKid))
	token := mustEncode(t, codec, tenantlessClaims(t))
	// The last character of a 64-byte signature carries two unused bits;
	// changing only those bits must not yield a second valid token text.
	last := token[len(token)-1]
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	index := strings.IndexByte(alphabet, last)
	variant := alphabet[index^1]
	_, err := codec.Verify(token[:len(token)-1] + string(variant))
	if !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestRule5ClaimsRejectUnknownFieldsAndWrongIssuerOrAudience(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	mutations := map[string]func(c map[string]any){
		"unknown claim": func(c map[string]any) { c["email"] = "user@example.com" },
		"wrong iss":     func(c map[string]any) { c["iss"] = "someone" },
		"wrong aud":     func(c map[string]any) { c["aud"] = "someone" },
		"missing iss":   func(c map[string]any) { delete(c, "iss") },
		"missing exp":   func(c map[string]any) { delete(c, "exp") },
		"missing plat":  func(c map[string]any) { delete(c, "plat") },
		"exp not after": func(c map[string]any) { c["exp"] = c["iat"] },
	}
	for name, mutate := range mutations {
		claims := rawClaims(t)
		mutate(claims)
		_, err := codec.Verify(key.forge(t, key.goodHeader(), claims))
		if !errors.Is(err, session.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRule5IssuedAtMayBeAtMost60SecondsAhead(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	for offset, wantOK := range map[int64]bool{60: true, 61: false} {
		claims := rawClaims(t)
		claims["iat"] = testNow.Unix() + offset
		claims["exp"] = testNow.Unix() + offset + 600
		_, err := codec.Verify(key.forge(t, key.goodHeader(), claims))
		if (err == nil) != wantOK {
			t.Errorf("iat +%ds: err=%v, wantOK=%v", offset, err, wantOK)
		}
		if err != nil && !errors.Is(err, session.ErrInvalid) {
			t.Errorf("iat +%ds: %v", offset, err)
		}
	}
}

func TestRule5ExpiryIsStrictWithNoLeeway(t *testing.T) {
	key := newTestKey(t, testKid)
	claims := tenantlessClaims(t)
	signer := newCodec(t, testNow, key)
	token := mustEncode(t, signer, claims)
	justBefore := newCodec(t, claims.ExpiresAt.Add(-time.Second), key)
	_, err := justBefore.Verify(token)
	if err != nil {
		t.Fatalf("one second before exp: %v", err)
	}
	atExp := newCodec(t, claims.ExpiresAt, key)
	_, err = atExp.Verify(token)
	if !errors.Is(err, session.ErrExpired) || errors.Is(err, session.ErrInvalid) {
		t.Fatalf("at exp: %v", err)
	}
}

func TestRule6RequiredClaimsAndTenantTriple(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	mutations := map[string]func(c map[string]any){
		"empty sub":          func(c map[string]any) { c["sub"] = "" },
		"empty sid":          func(c map[string]any) { c["sid"] = "" },
		"empty ksid":         func(c map[string]any) { c["ksid"] = "" },
		"empty csrf":         func(c map[string]any) { c["csrf"] = "" },
		"tid without role":   func(c map[string]any) { c["tid"] = randomID(t); c["tslug"] = "acme" },
		"tid without tslug":  func(c map[string]any) { c["tid"] = randomID(t); c["role"] = "admin" },
		"role without tid":   func(c map[string]any) { c["role"] = "admin" },
		"unknown role":       func(c map[string]any) { c["tid"] = randomID(t); c["tslug"] = "acme"; c["role"] = "owner" },
		"role wrong case":    func(c map[string]any) { c["tid"] = randomID(t); c["tslug"] = "acme"; c["role"] = "Admin" },
		"sub of wrong type":  func(c map[string]any) { c["sub"] = 5 },
		"plat of wrong type": func(c map[string]any) { c["plat"] = "yes" },
	}
	for name, mutate := range mutations {
		claims := rawClaims(t)
		mutate(claims)
		_, err := codec.Verify(key.forge(t, key.goodHeader(), claims))
		if !errors.Is(err, session.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	full := rawClaims(t)
	full["tid"], full["tslug"], full["role"] = randomID(t), "acme", "viewer"
	_, err := codec.Verify(key.forge(t, key.goodHeader(), full))
	if err != nil {
		t.Errorf("valid tenant triple rejected: %v", err)
	}
}

func TestVerifyAllowExpiredHonorsMaxAgeToTheSecond(t *testing.T) {
	key := newTestKey(t, testKid)
	claims := tenantlessClaims(t)
	token := mustEncode(t, newCodec(t, testNow, key), claims)
	maxAge := time.Hour
	cases := map[string]struct {
		now    time.Time
		wantOK bool
	}{
		"before exp":      {claims.IssuedAt.Add(time.Minute), true},
		"expired in age":  {claims.IssuedAt.Add(30 * time.Minute), true},
		"at max age":      {claims.IssuedAt.Add(maxAge), true},
		"one second past": {claims.IssuedAt.Add(maxAge + time.Second), false},
	}
	for name, c := range cases {
		_, err := newCodec(t, c.now, key).VerifyAllowExpired(token, maxAge)
		if (err == nil) != c.wantOK {
			t.Errorf("%s: err=%v wantOK=%v", name, err, c.wantOK)
		}
		if err != nil && !errors.Is(err, session.ErrExpired) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestVerifyAllowExpiredStillRejectsBadSignature(t *testing.T) {
	key, attacker := newTestKey(t, testKid), newTestKey(t, testKid)
	token := mustEncode(t, newCodec(t, testNow, attacker), tenantlessClaims(t))
	_, err := newCodec(t, testNow, key).VerifyAllowExpired(token, time.Hour)
	if !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestVerifyErrorExposesKidAndReasonWithoutToken(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	token := mustEncode(t, codec, tenantlessClaims(t))
	parts := strings.Split(token, ".")
	parts[2] = flipByte(t, parts[2])
	tampered := strings.Join(parts, ".")
	_, err := codec.Verify(tampered)
	var verifyErr *session.VerifyError
	if !errors.As(err, &verifyErr) {
		t.Fatalf("not a VerifyError: %v", err)
	}
	if verifyErr.Kid != testKid || verifyErr.Reason == "" || strings.Contains(err.Error(), parts[1]) {
		t.Errorf("kid=%q reason=%q err=%v", verifyErr.Kid, verifyErr.Reason, err)
	}
}

func TestHostileKidIsNeverEchoedInTheError(t *testing.T) {
	key := newTestKey(t, testKid)
	codec := newCodec(t, testNow, key)
	header := key.goodHeader()
	header["kid"] = "evil\nline"
	_, err := codec.Verify(key.forge(t, header, rawClaims(t)))
	var verifyErr *session.VerifyError
	if !errors.As(err, &verifyErr) || verifyErr.Kid != "" || strings.Contains(err.Error(), "evil") {
		t.Fatalf("got %v", err)
	}
}

func TestNewRandomIDIsUniqueAnd128Bits(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		id := randomID(t)
		raw, err := base64.RawURLEncoding.Strict().DecodeString(id)
		if err != nil || len(raw) != 16 || seen[id] {
			t.Fatalf("id %q: %v", id, err)
		}
		seen[id] = true
	}
}

func TestCSRFMatches(t *testing.T) {
	claims := tenantlessClaims(t)
	other := tenantlessClaims(t)
	oneOff := claims.CSRF[:len(claims.CSRF)-1] + "A"
	if claims.CSRF[len(claims.CSRF)-1] == 'A' {
		oneOff = claims.CSRF[:len(claims.CSRF)-1] + "B"
	}
	cases := map[string]struct {
		header string
		claims session.Claims
		want   bool
	}{
		"same":            {claims.CSRF, claims, true},
		"another token":   {other.CSRF, claims, false},
		"empty header":    {"", claims, false},
		"one char change": {oneOff, claims, false},
		"prefix":          {claims.CSRF[:5], claims, false},
		"both empty":      {"", session.Claims{}, false},
	}
	for name, c := range cases {
		if got := session.CSRFMatches(c.header, c.claims); got != c.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

func FuzzVerify(f *testing.F) {
	key := newTestKey(f, testKid)
	codec := newCodec(f, testNow, key)
	valid, err := codec.Encode(tenantClaims(f))
	if err != nil {
		f.Fatalf("encode: %v", err)
	}
	other := newCodec(f, testNow, newTestKey(f, testKid))
	f.Add(valid)
	f.Add("")
	f.Add("a.b.c")
	f.Add(strings.Repeat(".", 5))
	parts := strings.Split(valid, ".")
	f.Add(parts[0] + "." + parts[1] + ".")
	f.Fuzz(func(t *testing.T, token string) {
		_, errOther := other.Verify(token)
		if errOther == nil {
			t.Fatalf("token verified without its key: %q", token)
		}
		_, _ = codec.Verify(token)
		_, _ = codec.VerifyAllowExpired(token, time.Hour)
	})
}

func TestEncodeRejectsATokenOverTheSizeLimit(t *testing.T) {
	codec := newCodec(t, testNow, newTestKey(t, "k1"))
	claims := tenantlessClaims(t)
	claims.CSRF = strings.Repeat("a", 5000)
	token, err := codec.Encode(claims)
	if err == nil {
		t.Fatalf("Encode minted an oversized token of %d bytes", len(token))
	}
	if token != "" {
		t.Fatal("Encode returned a token alongside the size error")
	}
}
