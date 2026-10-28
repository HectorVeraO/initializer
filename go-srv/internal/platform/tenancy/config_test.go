package tenancy

import (
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
)

// parse runs parseConfig over settings and returns the config and the
// aggregated problem text ("" when none).
func parse(t *testing.T, settings map[string]string) (Config, string) {
	t.Helper()
	reader := config.NewReader(config.MapSource(settings))
	cfg := parseConfig(reader, mustParseURL(t, publicBase))
	err := reader.Err()
	if err == nil {
		return cfg, ""
	}
	return cfg, err.Error()
}

func slugSettings() map[string]string {
	return map[string]string{
		keyBootstrapSlug:  "acme",
		keyBootstrapEmail: "admin@example.com",
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

func TestDefaultsParseWithoutProblemsAndBootstrapIsOff(t *testing.T) {
	cfg, problems := parse(t, nil)

	if problems != "" {
		t.Fatalf("problems: %s", problems)
	}
	if cfg.InvitationTTL != 168*time.Hour {
		t.Fatalf("invitation ttl = %v, want 168h", cfg.InvitationTTL)
	}
	if cfg.Bootstrap.Enabled {
		t.Fatal("bootstrap must be disabled when the slug is unset")
	}
}

func TestInvitationTTLBoundaries(t *testing.T) {
	tests := []struct {
		value   string
		invalid bool
	}{{"59m", true}, {"1h", false}, {"720h", false}, {"721h", true}, {"soon", true}}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			_, problems := parse(t, map[string]string{keyInvitationTTL: tc.value})

			if got := strings.Contains(problems, keyInvitationTTL); got != tc.invalid {
				t.Fatalf("invalid=%v, problems=%q", got, problems)
			}
		})
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
			_, problems := parse(t, with(slugSettings(), keyBootstrapSlug, tc.slug))

			if got := !strings.Contains(problems, keyBootstrapSlug); got != tc.valid {
				t.Fatalf("valid=%v, problems=%q", got, problems)
			}
		})
	}
}

func TestBootstrapNeedsTheAdminEmailWhenTheSlugIsSet(t *testing.T) {
	cfg, problems := parse(t, map[string]string{keyBootstrapSlug: "acme"})

	if !strings.Contains(problems, keyBootstrapEmail) ||
		!strings.Contains(problems, "is required when APP_BOOTSTRAP_TENANT_SLUG is set") {
		t.Fatalf("problems = %q, want the admin email required", problems)
	}
	if !cfg.Bootstrap.Enabled {
		t.Fatal("the slug alone enables the bootstrap, whatever else is wrong")
	}
}

func TestBootstrapEmailIsTrimmedLowerCasedAndNeverFormatChecked(t *testing.T) {
	cfg, problems := parse(t, with(slugSettings(), keyBootstrapEmail, "  Not.An.Address  "))

	if problems != "" {
		t.Fatalf("problems = %q: identity owns the email format, tenancy reports no format problem", problems)
	}
	if cfg.Bootstrap.AdminEmail != "not.an.address" {
		t.Fatalf("admin email = %q", cfg.Bootstrap.AdminEmail)
	}
}

func TestBootstrapEnabledOnlyBySlug(t *testing.T) {
	cfg, problems := parse(t, slugSettings())

	if problems != "" || !cfg.Bootstrap.Enabled {
		t.Fatalf("enabled=%v problems=%q", cfg.Bootstrap.Enabled, problems)
	}
	if cfg.Bootstrap.TenantSlug != "acme" || cfg.Bootstrap.TenantName != "acme" || cfg.Bootstrap.AdminEmail != "admin@example.com" {
		t.Fatalf("bootstrap = %+v, want the name to default to the slug", cfg.Bootstrap)
	}
	cfg, _ = parse(t, map[string]string{keyBootstrapEmail: "admin@example.com"})
	if cfg.Bootstrap.Enabled {
		t.Fatal("an admin email without a slug must not enable the tenant bootstrap")
	}
}

func TestBootstrapTenantNameLimitAppliesWithoutBootstrap(t *testing.T) {
	_, problems := parse(t, map[string]string{keyBootstrapName: strings.Repeat("n", 101)})

	if !strings.Contains(problems, keyBootstrapName) {
		t.Fatalf("101 characters accepted without bootstrap: %q", problems)
	}
}

func TestBootstrapTenantNameRules(t *testing.T) {
	cfg, problems := parse(t, with(slugSettings(), keyBootstrapName, "  Acme Corp  "))
	if problems != "" || cfg.Bootstrap.TenantName != "Acme Corp" {
		t.Fatalf("name=%q problems=%q", cfg.Bootstrap.TenantName, problems)
	}
	_, problems = parse(t, with(slugSettings(), keyBootstrapName, strings.Repeat("n", 100)))
	if problems != "" {
		t.Fatalf("100 characters: %q", problems)
	}
	cfg, problems = parse(t, with(slugSettings(), keyBootstrapName, strings.Repeat("n", 101)))
	if !strings.Contains(problems, keyBootstrapName) {
		t.Fatalf("101 characters accepted")
	}
	if cfg.Bootstrap.TenantName != "acme" {
		t.Fatalf("an over-long name must fall back to the slug, got %q", cfg.Bootstrap.TenantName)
	}
}

func TestSeveralProblemsAreReportedTogetherWithoutValues(t *testing.T) {
	_, problems := parse(t, map[string]string{
		keyInvitationTTL: "1s",
		keyBootstrapSlug: "Not A Slug",
		keyBootstrapName: strings.Repeat("n", 101),
	})

	for _, key := range []string{keyInvitationTTL, keyBootstrapSlug, keyBootstrapName, keyBootstrapEmail} {
		if !strings.Contains(problems, key) {
			t.Errorf("problems do not name %s: %q", key, problems)
		}
	}
	for _, value := range []string{"Not A Slug", "1s"} {
		if strings.Contains(problems, value) {
			t.Errorf("problems leak %q", value)
		}
	}
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
