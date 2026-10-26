package auth

import (
	"net/http"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

// CheckTenantRef returns nil when ref is empty or names the principal's tenant
// by UUID or by slug (both case-insensitive, ASCII), else 403 TENANT_MISMATCH.
func CheckTenantRef(p reqctx.Principal, ref string) error {
	if ref == "" {
		return nil
	}
	if p.TenantID != nil {
		matchesID := asciiEqualFold(ref, string(*p.TenantID))
		matchesSlug := p.TenantSlug != "" && asciiEqualFold(ref, p.TenantSlug)
		if matchesID || matchesSlug {
			return nil
		}
	}
	return api.NewError(http.StatusForbidden, api.CodeTenantMismatch, "Tenant does not match the active tenant")
}

// asciiEqualFold avoids the Unicode folds of strings.EqualFold, where U+212A
// (Kelvin sign) equals "k".
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
