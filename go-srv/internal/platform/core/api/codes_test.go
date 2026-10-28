package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
)

func TestRegistryHasEveryCodeOnceInScreamingSnake(t *testing.T) {
	snake := regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)
	seen := map[api.Code]bool{}
	for _, info := range api.Registry() {
		if !snake.MatchString(string(info.Code)) {
			t.Errorf("%s is not SCREAMING_SNAKE", info.Code)
		}
		if seen[info.Code] {
			t.Errorf("%s registered twice", info.Code)
		}
		seen[info.Code] = true
		if info.RaisedBy == "" {
			t.Errorf("%s has no RaisedBy", info.Code)
		}
		if info.Issue && info.Status != http.StatusUnprocessableEntity {
			t.Errorf("issue code %s has status %d", info.Code, info.Status)
		}
	}
}

func TestRegistryMatchesTheDesignTable(t *testing.T) {
	want := map[api.Code]int{
		"SESSION_INVALID": 401, "API_KEY_INVALID_OR_REVOKED": 401, "CSRF_TOKEN_INVALID": 403,
		"INSUFFICIENT_SCOPE": 403, "INSUFFICIENT_ROLE": 403, "PLATFORM_OPERATOR_REQUIRED": 403,
		"ACTIVE_TENANT_REQUIRED": 403, "TENANT_MISMATCH": 403, "TENANT_ACCESS_DENIED": 403,
		"TENANT_SUSPENDED": 403, "MEMBERSHIP_REVOKED": 403, "ACCOUNT_LOCKED_OR_DISABLED": 403,
		"INVITATION_EMAIL_MISMATCH": 403, "RESOURCE_NOT_FOUND": 404, "ROUTE_NOT_FOUND": 404,
		"FLAG_NOT_DEFINED_IN_MANIFEST": 404, "INVITATION_INVALID": 404, "CREDENTIAL_CONFLICT": 409,
		"ACTIVE_TENANT_CHANGED": 409, "LAST_TENANT_ADMIN": 409, "MEMBERSHIP_ALREADY_ACTIVE": 409,
		"INVITATION_NOT_PENDING": 409, "INVITATION_REQUIRED": 409, "TENANT_SLUG_TAKEN": 409,
		"IDENTITY_ALREADY_EXISTS": 409, "SYSTEM_PARAMETER_PROTECTED": 409, "PARAMETER_ALREADY_EXISTS": 409,
		"PARAMETER_OVERRIDES_VIOLATE_CONSTRAINTS": 409, "PAYLOAD_TOO_LARGE": 413,
		"UNSUPPORTED_MEDIA_TYPE": 415, "VALIDATION_FAILED": 422, "INTERNAL_ERROR": 500,
		"SERVICE_UNAVAILABLE": 503, "IDENTITY_PROVIDER_UNAVAILABLE": 503,
	}
	issues := []api.Code{
		"BODY_MALFORMED", "FIELD_REQUIRED", "FIELD_TYPE_INVALID", "FIELD_TOO_SHORT", "FIELD_TOO_LONG",
		"FIELD_OUT_OF_RANGE", "FIELD_INVALID_FORMAT", "FIELD_NOT_ALLOWED", "FIELD_DUPLICATE",
		"FIELD_MIN_ITEMS", "PARAMETER_KEY_INVALID", "PARAMETER_TYPE_MISMATCH", "PARAMETER_OUT_OF_RANGE",
		"PARAMETER_VALUE_NOT_ALLOWED", "PARAMETER_CONSTRAINT_INVALID", "PARAMETER_FIELD_IMMUTABLE",
		"PASSWORD_REJECTED", "SCOPE_LEVEL_MISMATCH",
	}
	byCode := map[api.Code]api.CodeInfo{}
	for _, info := range api.Registry() {
		byCode[info.Code] = info
	}
	for code, status := range want {
		info, ok := byCode[code]
		if !ok || info.Status != status || info.Issue {
			t.Errorf("%s: %+v, want status %d", code, info, status)
		}
	}
	for _, code := range issues {
		if info, ok := byCode[code]; !ok || !info.Issue {
			t.Errorf("issue code %s: %+v", code, info)
		}
	}
	if raisedBy := byCode["SCOPE_LEVEL_MISMATCH"].RaisedBy; raisedBy != "api-keys" {
		t.Errorf("SCOPE_LEVEL_MISMATCH raisedBy = %q, want api-keys", raisedBy)
	}
	if len(want)+len(issues) != len(api.Registry()) {
		t.Errorf("expected %d codes, registry has %d", len(want)+len(issues), len(api.Registry()))
	}
}

// Retired names live in testdata/retired-codes.txt, which the scan skips.
func TestNoRetiredCodeAppearsUnderCore(t *testing.T) {
	list, err := os.ReadFile(filepath.Join("testdata", "retired-codes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var patterns []*regexp.Regexp
	for _, name := range strings.Fields(string(list)) {
		patterns = append(patterns, regexp.MustCompile(`\b`+name+`\b`))
	}
	if len(patterns) != 20 {
		t.Fatalf("retired list has %d names, want 20", len(patterns))
	}
	err = filepath.Walk("..", func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() && info.Name() == "testdata" {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, pattern := range patterns {
			if pattern.Match(content) {
				t.Errorf("%s contains retired code %s", path, pattern)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
