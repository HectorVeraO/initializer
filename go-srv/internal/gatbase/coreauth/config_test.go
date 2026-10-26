package coreauth

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/config"
)

// parse runs parseConfig over settings plus a valid keyring and returns the
// config and the aggregated problem text ("" when none).
func parse(t *testing.T, settings map[string]string) (Config, string) {
	t.Helper()
	merged := map[string]string{keySessionKeys: randomSessionKeys(t)}
	for k, v := range settings {
		merged[k] = v
	}
	reader := config.NewReader(config.MapSource(merged))
	base, _ := url.Parse("http://localhost:10120")
	cfg := parseConfig(reader, base)
	err := reader.Err()
	if err == nil {
		return cfg, ""
	}
	return cfg, err.Error()
}

func bootstrapTriple() map[string]string {
	return map[string]string{
		keyBootstrapSlug:     "acme",
		keyBootstrapEmail:    "admin@example.com",
		keyBootstrapPassword: "change-me-local-only",
	}
}

func with(base map[string]string, key, value string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	out[key] = value
	return out
}

func TestDefaultsParseWithoutProblems(t *testing.T) {
	cfg, problems := parse(t, nil)
	if problems != "" {
		t.Fatalf("problems: %s", problems)
	}
	if cfg.SessionTTL != 10*time.Minute || cfg.SessionRefreshMaxAge != 24*time.Hour ||
		cfg.InvitationTTL != 168*time.Hour || cfg.Bootstrap.Timeout != 60*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.KratosPublicURL.String() != "http://127.0.0.1:10133" || cfg.KratosAdminURL.String() != "http://127.0.0.1:10134" {
		t.Fatalf("unexpected kratos urls")
	}
	if cfg.Bootstrap.Enabled {
		t.Fatalf("bootstrap must be disabled when unset")
	}
}

func TestSessionKeysAreRequired(t *testing.T) {
	reader := config.NewReader(config.MapSource(map[string]string{}))
	parseConfig(reader, &url.URL{})
	err := reader.Err()
	if err == nil || !strings.Contains(err.Error(), keySessionKeys) {
		t.Fatalf("want a problem naming %s, got %v", keySessionKeys, err)
	}
}

func TestDurationBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		invalid bool
	}{
		{"ttl 59s", keySessionTTL, "59s", true},
		{"ttl 1m", keySessionTTL, "1m", false},
		{"ttl 15m", keySessionTTL, "15m", false},
		{"ttl 16m", keySessionTTL, "16m", true},
		{"refresh 9m", keyRefreshMaxAge, "9m", true},
		{"refresh 10m", keyRefreshMaxAge, "10m", false},
		{"refresh 24h", keyRefreshMaxAge, "24h", false},
		{"refresh 25h", keyRefreshMaxAge, "25h", true},
		{"invitation 59m", keyInvitationTTL, "59m", true},
		{"invitation 1h", keyInvitationTTL, "1h", false},
		{"invitation 720h", keyInvitationTTL, "720h", false},
		{"invitation 721h", keyInvitationTTL, "721h", true},
		{"bootstrap timeout 4s", keyBootstrapTimeout, "4s", true},
		{"bootstrap timeout 5s", keyBootstrapTimeout, "5s", false},
		{"bootstrap timeout 10m", keyBootstrapTimeout, "10m", false},
		{"bootstrap timeout 11m", keyBootstrapTimeout, "11m", true},
		{"not a duration", keySessionTTL, "soon", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := parse(t, map[string]string{tc.key: tc.value})
			got := strings.Contains(problems, tc.key)
			if got != tc.invalid {
				t.Fatalf("invalid=%v, problems=%q", got, problems)
			}
			hasFalseConflict := strings.Contains(problems, keyRefreshMaxAge) && tc.key != keyRefreshMaxAge
			if hasFalseConflict {
				t.Fatalf("false refresh conflict: %q", problems)
			}
		})
	}
}

func TestRefreshMaxAgeBelowTTLIsRejected(t *testing.T) {
	_, problems := parse(t, map[string]string{keySessionTTL: "15m", keyRefreshMaxAge: "10m"})
	if !strings.Contains(problems, keyRefreshMaxAge) {
		t.Fatalf("problems: %q", problems)
	}
	_, problems = parse(t, map[string]string{keySessionTTL: "15m", keyRefreshMaxAge: "15m"})
	if problems != "" {
		t.Fatalf("equal values must pass: %q", problems)
	}
}

func TestKratosURLMustBeAbsoluteHTTP(t *testing.T) {
	for _, bad := range []string{"kratos:4433", "/relative", "ftp://kratos", "http://u:p@kratos"} {
		_, problems := parse(t, map[string]string{keyKratosPublicURL: bad})
		if !strings.Contains(problems, keyKratosPublicURL) {
			t.Errorf("%q accepted", bad)
		}
		if strings.Contains(problems, bad) {
			t.Errorf("problem text repeats the value %q", bad)
		}
	}
}

func TestSlugBoundaries(t *testing.T) {
	tests := []struct {
		slug  string
		valid bool
	}{
		{"ab", false},
		{"abc", true},
		{strings.Repeat("a", 40), true},
		{strings.Repeat("a", 41), false},
		{"-abc", false},
		{"abc-", false},
		{"Abc", false},
		{"ab_c", false},
		{"123e4567-e89b-42d3-a456-426614174000", false},
	}
	for _, tc := range tests {
		t.Run(tc.slug, func(t *testing.T) {
			_, problems := parse(t, with(bootstrapTriple(), keyBootstrapSlug, tc.slug))
			got := !strings.Contains(problems, keyBootstrapSlug)
			if got != tc.valid {
				t.Fatalf("valid=%v, problems=%q", got, problems)
			}
		})
	}
}

func TestBootstrapEmailRules(t *testing.T) {
	tests := []struct {
		name  string
		email string
		valid bool
	}{
		{"bare", "admin@example.com", true},
		{"upper case is lowered", "Admin@example.com", true},
		{"display name", "Jane Doe <jane@example.com>", false},
		{"angle brackets only", "<jane@example.com>", false},
		{"not an address", "jane", false},
		{"too long", strings.Repeat("a", 250) + "@example.com", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, problems := parse(t, with(bootstrapTriple(), keyBootstrapEmail, tc.email))
			got := !strings.Contains(problems, keyBootstrapEmail)
			if got != tc.valid {
				t.Fatalf("valid=%v, problems=%q", got, problems)
			}
			if tc.valid && cfg.Bootstrap.AdminEmail != strings.ToLower(tc.email) {
				t.Fatalf("email %q not lower-cased", cfg.Bootstrap.AdminEmail)
			}
		})
	}
}

func TestBootstrapEmailLengthBoundary(t *testing.T) {
	at254 := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	if len(at254) != 254 || strings.HasSuffix(at254, ".") {
		t.Fatalf("fixture is wrong: len=%d", len(at254))
	}
	_, problems := parse(t, with(bootstrapTriple(), keyBootstrapEmail, at254))
	if problems != "" {
		t.Fatalf("254 characters rejected: %q", problems)
	}
	_, problems = parse(t, with(bootstrapTriple(), keyBootstrapEmail, at254+"d"))
	if !strings.Contains(problems, keyBootstrapEmail) {
		t.Fatalf("255 characters accepted")
	}
}

func TestBootstrapPasswordLength(t *testing.T) {
	tests := []struct {
		length int
		valid  bool
	}{{11, false}, {12, true}, {64, true}, {65, false}}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.length), func(t *testing.T) {
			password := strings.Repeat("p", tc.length)
			_, problems := parse(t, with(bootstrapTriple(), keyBootstrapPassword, password))
			got := !strings.Contains(problems, keyBootstrapPassword)
			if got != tc.valid {
				t.Fatalf("valid=%v, problems=%q", got, problems)
			}
			if strings.Contains(problems, password) {
				t.Fatalf("problem text repeats the password")
			}
		})
	}
}

func TestBootstrapPasswordCountsCharactersNotBytes(t *testing.T) {
	password := strings.Repeat("é", 12)
	if utf8.RuneCountInString(password) != 12 || len(password) != 24 {
		t.Fatal("fixture is wrong")
	}
	_, problems := parse(t, with(bootstrapTriple(), keyBootstrapPassword, password))
	if problems != "" {
		t.Fatalf("problems: %q", problems)
	}
}

func TestBootstrapIsAllOrNone(t *testing.T) {
	triple := bootstrapTriple()
	cfg, problems := parse(t, triple)
	if problems != "" || !cfg.Bootstrap.Enabled {
		t.Fatalf("full triple: enabled=%v problems=%q", cfg.Bootstrap.Enabled, problems)
	}
	if cfg.Bootstrap.TenantName != "acme" {
		t.Fatalf("name must default to the slug, got %q", cfg.Bootstrap.TenantName)
	}
	for _, missing := range []string{keyBootstrapSlug, keyBootstrapEmail, keyBootstrapPassword} {
		partial := map[string]string{}
		for k, v := range triple {
			if k != missing {
				partial[k] = v
			}
		}
		cfg, problems := parse(t, partial)
		if !strings.Contains(problems, missing) {
			t.Errorf("missing %s: problems=%q", missing, problems)
		}
		if cfg.Bootstrap.Enabled {
			t.Errorf("missing %s: bootstrap enabled", missing)
		}
	}
}

func TestBootstrapTenantNameRules(t *testing.T) {
	cfg, problems := parse(t, with(bootstrapTriple(), keyBootstrapName, "  Acme Corp  "))
	if problems != "" || cfg.Bootstrap.TenantName != "Acme Corp" {
		t.Fatalf("name=%q problems=%q", cfg.Bootstrap.TenantName, problems)
	}
	_, problems = parse(t, with(bootstrapTriple(), keyBootstrapName, strings.Repeat("n", 100)))
	if problems != "" {
		t.Fatalf("100 characters: %q", problems)
	}
	_, problems = parse(t, with(bootstrapTriple(), keyBootstrapName, strings.Repeat("n", 101)))
	if !strings.Contains(problems, keyBootstrapName) {
		t.Fatalf("101 characters accepted")
	}
}

func TestOperatorEmails(t *testing.T) {
	cfg, problems := parse(t, map[string]string{keyOperatorEmails: "Ops@example.com, second@example.org,"})
	if problems != "" {
		t.Fatalf("problems: %q", problems)
	}
	if len(cfg.PlatformOperatorEmails) != 2 || cfg.PlatformOperatorEmails[0] != "ops@example.com" {
		t.Fatalf("emails: %v", cfg.PlatformOperatorEmails)
	}
	_, problems = parse(t, map[string]string{keyOperatorEmails: "ops@example.com,Jane Doe <j@example.com>"})
	if !strings.Contains(problems, keyOperatorEmails) {
		t.Fatalf("display name accepted")
	}
}

func TestSeveralProblemsAreReportedTogetherWithoutValues(t *testing.T) {
	secret := "tiny-pw"
	_, problems := parse(t, map[string]string{
		keySessionTTL:        "99h",
		keyInvitationTTL:     "1s",
		keyKratosAdminURL:    "not a url",
		keyBootstrapSlug:     "acme",
		keyBootstrapPassword: secret,
	})
	keys := []string{keySessionTTL, keyInvitationTTL, keyKratosAdminURL, keyBootstrapEmail, keyBootstrapPassword}
	for _, key := range keys {
		if !strings.Contains(problems, key) {
			t.Errorf("problems do not name %s: %q", key, problems)
		}
	}
	for _, value := range []string{"99h", "not a url", secret} {
		if strings.Contains(problems, value) {
			t.Errorf("problems leak %q", value)
		}
	}
}

func TestSecretsAreRedactedInFmtAndSlog(t *testing.T) {
	cfg, _ := parse(t, bootstrapTriple())
	password := bootstrapTriple()[keyBootstrapPassword]
	got := fmt.Sprintf("%v %+v %#v", cfg.Bootstrap.AdminPassword, cfg.Bootstrap, cfg.SessionKeys)
	if strings.Contains(got, password) || !strings.Contains(got, "[redacted]") {
		t.Fatalf("fmt output: %s", got)
	}
	var sink strings.Builder
	logger := slog.New(slog.NewTextHandler(&sink, nil))
	logger.Info("bootstrap_config", slog.Any("password", cfg.Bootstrap.AdminPassword), slog.Any("keys", cfg.SessionKeys))
	if strings.Contains(sink.String(), password) || !strings.Contains(sink.String(), "[redacted]") {
		t.Fatalf("slog output: %s", sink.String())
	}
}

func FuzzNormalizeEmail(f *testing.F) {
	for _, seed := range []string{"admin@example.com", "Jane Doe <j@example.com>", "<a@b.c>", "", "@", "a@b@c", "\"a b\"@example.com"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		reader := config.NewReader(config.MapSource(nil))
		got := normalizeEmail(reader, "K", raw)
		if got == "" {
			if reader.Err() == nil {
				t.Fatalf("empty result without a problem for %q", raw)
			}
			return
		}
		if reader.Err() != nil || got != strings.ToLower(got) || len(got) > maxEmailLength || strings.ContainsAny(got, " \t\r\n<>") {
			t.Fatalf("accepted %q as %q", raw, got)
		}
	})
}

func FuzzIsValidSlug(f *testing.F) {
	for _, seed := range []string{"acme", "ab", "123e4567-e89b-42d3-a456-426614174000", "-a-", "A_B", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, slug string) {
		if !isValidSlug(slug) {
			return
		}
		tooShortOrLong := len(slug) < 3 || len(slug) > 40
		if tooShortOrLong || strings.ToLower(slug) != slug || strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
			t.Fatalf("accepted %q", slug)
		}
	})
}
