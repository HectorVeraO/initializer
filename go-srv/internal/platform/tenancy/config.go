package tenancy

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// Configuration keys read by tenancy. APP_PUBLIC_BASE_URL is core's and
// APP_BOOTSTRAP_ADMIN_EMAIL is identity's, which validates its format.
const (
	keyInvitationTTL  = "APP_INVITATION_TTL"
	keyBootstrapSlug  = "APP_BOOTSTRAP_TENANT_SLUG"
	keyBootstrapName  = "APP_BOOTSTRAP_TENANT_NAME"
	keyBootstrapEmail = "APP_BOOTSTRAP_ADMIN_EMAIL"
)

const maxTenantNameLength = 100

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

// Config is the parsed tenancy configuration.
type Config struct {
	// PublicBaseURL is core's APP_PUBLIC_BASE_URL.
	PublicBaseURL *url.URL
	InvitationTTL time.Duration
	Bootstrap     BootstrapConfig
}

// BootstrapConfig is the first tenant. Enabled is true when
// APP_BOOTSTRAP_TENANT_SLUG is set.
type BootstrapConfig struct {
	Enabled    bool
	TenantSlug string
	// TenantName is APP_BOOTSTRAP_TENANT_NAME, default TenantSlug, 1-100 characters.
	TenantName string
	// AdminEmail is APP_BOOTSTRAP_ADMIN_EMAIL, trimmed and lower-cased;
	// identity validates it.
	AdminEmail string
}

// parseConfig reads every tenancy key and records each problem in r (key and
// rule, never a value). It does no I/O.
func parseConfig(r *config.Reader, publicBaseURL *url.URL) Config {
	cfg := Config{
		PublicBaseURL: publicBaseURL,
		InvitationTTL: r.Duration(keyInvitationTTL, 168*time.Hour, time.Hour, 720*time.Hour),
	}
	cfg.Bootstrap = parseBootstrap(r)
	return cfg
}

func parseBootstrap(r *config.Reader) BootstrapConfig {
	slug := r.String(keyBootstrapSlug, "")
	email := strings.ToLower(r.String(keyBootstrapEmail, ""))
	name := r.String(keyBootstrapName, slug)
	if utf8.RuneCountInString(name) > maxTenantNameLength {
		r.Invalid(keyBootstrapName, fmt.Sprintf("must be at most %d characters", maxTenantNameLength))
		name = slug
	}
	if slug == "" {
		return BootstrapConfig{}
	}
	if !isValidSlug(slug) {
		r.Invalid(keyBootstrapSlug, "must be 3 to 40 characters of a-z, 0-9 and -, not start or end with -, and not be a UUID")
	}
	if email == "" {
		r.Invalid(keyBootstrapEmail, "is required when "+keyBootstrapSlug+" is set")
	}
	return BootstrapConfig{Enabled: true, TenantSlug: slug, TenantName: name, AdminEmail: email}
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
