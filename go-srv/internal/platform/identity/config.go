package identity

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
)

// Configuration keys read by identity. APP_PUBLIC_BASE_URL is core's.
const (
	keyKratosPublicURL   = "APP_KRATOS_PUBLIC_URL"
	keyKratosAdminURL    = "APP_KRATOS_ADMIN_URL"
	keySessionKeys       = "APP_SESSION_KEYS"
	keySessionTTL        = "APP_SESSION_TTL"
	keyRefreshMaxAge     = "APP_SESSION_REFRESH_MAX_AGE"
	keyBootstrapEmail    = "APP_BOOTSTRAP_ADMIN_EMAIL"
	keyBootstrapPassword = "APP_BOOTSTRAP_ADMIN_PASSWORD"
	keyBootstrapTimeout  = "APP_BOOTSTRAP_TIMEOUT"
	keyOperatorEmails    = "APP_PLATFORM_OPERATOR_EMAILS"
)

const (
	defaultKratosPublicURL = "http://127.0.0.1:10133"
	defaultKratosAdminURL  = "http://127.0.0.1:10134"
	maxEmailLength         = 254
	minPasswordLength      = 12
	maxPasswordLength      = 64
)

// Config is the parsed identity configuration.
type Config struct {
	// PublicBaseURL is core's APP_PUBLIC_BASE_URL.
	PublicBaseURL *url.URL
	// KratosPublicURL and KratosAdminURL are nil only when invalid.
	KratosPublicURL *url.URL
	KratosAdminURL  *url.URL
	// SessionKeys is the raw keyring text; session.ParseKeyring validates it.
	SessionKeys          config.Secret
	SessionTTL           time.Duration
	SessionRefreshMaxAge time.Duration
	Bootstrap            BootstrapConfig
	// PlatformOperatorEmails are bare, lower-cased addresses.
	PlatformOperatorEmails []string
}

// BootstrapConfig is parsed from APP_BOOTSTRAP_ADMIN_*. Enabled is true when
// both the email and the password are set.
type BootstrapConfig struct {
	Enabled       bool
	AdminEmail    string
	AdminPassword config.Secret
	Timeout       time.Duration
}

// parseConfig reads every identity key and records each problem in r (key
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
	email := r.String(keyBootstrapEmail, "")
	password, hasPassword := r.OptionalSecret(keyBootstrapPassword)
	timeout := r.Duration(keyBootstrapTimeout, 60*time.Second, 5*time.Second, 10*time.Minute)

	hasEmail := email != ""
	if !hasEmail && !hasPassword {
		return BootstrapConfig{Timeout: timeout}
	}
	const rule = "is required when another APP_BOOTSTRAP_ADMIN_* variable is set"
	if !hasEmail {
		r.Invalid(keyBootstrapEmail, rule)
	}
	if !hasPassword {
		r.Invalid(keyBootstrapPassword, rule)
	}

	if hasEmail {
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
		Enabled:       hasEmail && hasPassword,
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
