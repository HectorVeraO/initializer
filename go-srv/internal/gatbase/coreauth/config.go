package coreauth

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

// Configuration keys read by core-auth. GATBASE_PUBLIC_BASE_URL is core's.
const (
	keyKratosPublicURL   = "GATBASE_KRATOS_PUBLIC_URL"
	keyKratosAdminURL    = "GATBASE_KRATOS_ADMIN_URL"
	keySessionKeys       = "GATBASE_SESSION_KEYS"
	keySessionTTL        = "GATBASE_SESSION_TTL"
	keyRefreshMaxAge     = "GATBASE_SESSION_REFRESH_MAX_AGE"
	keyInvitationTTL     = "GATBASE_INVITATION_TTL"
	keyBootstrapSlug     = "GATBASE_BOOTSTRAP_TENANT_SLUG"
	keyBootstrapEmail    = "GATBASE_BOOTSTRAP_ADMIN_EMAIL"
	keyBootstrapPassword = "GATBASE_BOOTSTRAP_ADMIN_PASSWORD"
	keyBootstrapName     = "GATBASE_BOOTSTRAP_TENANT_NAME"
	keyBootstrapTimeout  = "GATBASE_BOOTSTRAP_TIMEOUT"
	keyOperatorEmails    = "GATBASE_PLATFORM_OPERATOR_EMAILS"
)

const (
	defaultKratosPublicURL = "http://127.0.0.1:10133"
	defaultKratosAdminURL  = "http://127.0.0.1:10134"
	maxEmailLength         = 254
	minPasswordLength      = 12
	maxPasswordLength      = 64
	maxTenantNameLength    = 100
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

// Config is the parsed core-auth configuration.
type Config struct {
	// PublicBaseURL is core's GATBASE_PUBLIC_BASE_URL.
	PublicBaseURL *url.URL
	// KratosPublicURL and KratosAdminURL are nil only when invalid.
	KratosPublicURL *url.URL
	KratosAdminURL  *url.URL
	// SessionKeys is the raw keyring text; session.ParseKeyring validates it.
	SessionKeys          config.Secret
	SessionTTL           time.Duration
	SessionRefreshMaxAge time.Duration
	InvitationTTL        time.Duration
	Bootstrap            BootstrapConfig
	// PlatformOperatorEmails are bare, lower-cased addresses.
	PlatformOperatorEmails []string
}

// BootstrapConfig is parsed from GATBASE_BOOTSTRAP_*. Enabled is false when none of the
// three required variables is set.
type BootstrapConfig struct {
	Enabled       bool
	TenantSlug    string
	TenantName    string
	AdminEmail    string
	AdminPassword config.Secret
	Timeout       time.Duration
}

// parseConfig reads every core-auth key and records each problem in r (key
// and rule, never a value). It does no I/O.
func parseConfig(r *config.Reader, publicBaseURL *url.URL) Config {
	cfg := Config{
		PublicBaseURL:   publicBaseURL,
		KratosPublicURL: r.URL(keyKratosPublicURL, defaultKratosPublicURL),
		KratosAdminURL:  r.URL(keyKratosAdminURL, defaultKratosAdminURL),
		SessionKeys:     r.Secret(keySessionKeys),
		SessionTTL:      r.Duration(keySessionTTL, 10*time.Minute, time.Minute, 15*time.Minute),
		SessionRefreshMaxAge: r.Duration(keyRefreshMaxAge, 24*time.Hour,
			10*time.Minute, 24*time.Hour),
		InvitationTTL: r.Duration(keyInvitationTTL, 168*time.Hour, time.Hour, 720*time.Hour),
	}
	// A rejected value falls back to its default, and no default can fail this
	// comparison, so it reports only a real conflict between two valid values.
	if cfg.SessionRefreshMaxAge < cfg.SessionTTL {
		r.Invalid(keyRefreshMaxAge, "must be at least "+keySessionTTL)
	}
	cfg.Bootstrap = parseBootstrap(r)
	cfg.PlatformOperatorEmails = parseOperatorEmails(r)
	return cfg
}

func parseBootstrap(r *config.Reader) BootstrapConfig {
	slug := r.String(keyBootstrapSlug, "")
	email := r.String(keyBootstrapEmail, "")
	password, hasPassword := r.OptionalSecret(keyBootstrapPassword)
	timeout := r.Duration(keyBootstrapTimeout, 60*time.Second, 5*time.Second, 10*time.Minute)
	name := r.String(keyBootstrapName, slug)
	if utf8.RuneCountInString(name) > maxTenantNameLength {
		r.Invalid(keyBootstrapName, fmt.Sprintf("must be at most %d characters", maxTenantNameLength))
		name = slug
	}

	required := []struct {
		key   string
		isSet bool
	}{
		{keyBootstrapSlug, slug != ""},
		{keyBootstrapEmail, email != ""},
		{keyBootstrapPassword, hasPassword},
	}
	setCount := 0
	for _, item := range required {
		if item.isSet {
			setCount++
		}
	}
	if setCount == 0 {
		return BootstrapConfig{Timeout: timeout}
	}
	for _, item := range required {
		if !item.isSet {
			r.Invalid(item.key, "is required when another GATBASE_BOOTSTRAP_* admin variable is set")
		}
	}

	if slug != "" && !isValidSlug(slug) {
		r.Invalid(keyBootstrapSlug, "must be 3 to 40 characters of a-z, 0-9 and -, not start or end with -, and not be a UUID")
	}
	if email != "" {
		email = normalizeEmail(r, keyBootstrapEmail, email)
	}
	if hasPassword {
		length := utf8.RuneCountInString(password.Reveal())
		isLengthOK := length >= minPasswordLength && length <= maxPasswordLength
		if !isLengthOK {
			r.Invalid(keyBootstrapPassword,
				fmt.Sprintf("must be %d to %d characters", minPasswordLength, maxPasswordLength))
		}
	}
	return BootstrapConfig{
		Enabled:       setCount == len(required),
		TenantSlug:    slug,
		TenantName:    name,
		AdminEmail:    email,
		AdminPassword: password,
		Timeout:       timeout,
	}
}

func parseOperatorEmails(r *config.Reader) []string {
	raw := r.List(keyOperatorEmails)
	var emails []string
	for _, item := range raw {
		normalized := normalizeEmail(r, keyOperatorEmails, item)
		if normalized != "" {
			emails = append(emails, normalized)
		}
	}
	return emails
}

// isValidSlug applies the tenant slug rules: the pattern of ten_tenants and
// not UUID-shaped, so a tenant reference is never ambiguous.
func isValidSlug(slug string) bool {
	if !slugPattern.MatchString(slug) {
		return false
	}
	_, isUUID := reqctx.ParseTenantID(slug)
	return !isUUID
}

// normalizeEmail returns the lower-cased bare address, or "" after recording a
// problem for key. A display name, angle brackets, a comment or more than
// maxEmailLength characters are rejected.
func normalizeEmail(r *config.Reader, key, raw string) string {
	address, err := mail.ParseAddress(raw)
	isBare := err == nil && address.Name == "" && address.Address == raw
	isShortEnough := len(raw) <= maxEmailLength
	if !isBare || !isShortEnough || strings.ContainsAny(raw, " \t\r\n") {
		r.Invalid(key, fmt.Sprintf("must be a bare email address of at most %d characters", maxEmailLength))
		return ""
	}
	return strings.ToLower(raw)
}
