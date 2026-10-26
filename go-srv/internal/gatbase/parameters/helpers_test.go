package parameters_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/parameters"
)

// fixedNow is the instant the test clock answers.
var fixedNow = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

func randomUUID(t testing.TB) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// principal is an account actor with an active tenant.
func principal(t testing.TB, tenant string) reqctx.Principal {
	t.Helper()
	account := randomUUID(t)
	tenantID := reqctx.TenantID(tenant)
	return reqctx.Principal{
		Kind:      reqctx.ActorAccount,
		ActorID:   account,
		AccountID: reqctx.AccountID(account),
		TenantID:  &tenantID,
		Role:      reqctx.RoleAdmin,
	}
}

// globalPrincipal is an account actor with no active tenant.
func globalPrincipal(t testing.TB) reqctx.Principal {
	t.Helper()
	account := randomUUID(t)
	return reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: account, AccountID: reqctx.AccountID(account)}
}

// body decodes s through api.DecodeBody like a route would.
func body(t testing.TB, s string) *api.Body {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(s))
	request.Header.Set("Content-Type", "application/json")
	decoded, err := api.DecodeBody(request)
	if err != nil {
		t.Fatalf("decode body %q: %v", s, err)
	}
	return decoded
}

// newService opens a service over a fresh database.
func newService(t testing.TB) (*parameters.Service, *pgxpool.Pool) {
	t.Helper()
	pool := parameters.NewTestPool(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return parameters.NewService(pool, logger, clock.Fixed(fixedNow)), pool
}

// newLoggedService is newService with a buffer that receives the log lines.
func newLoggedService(t testing.TB) (*parameters.Service, *pgxpool.Pool, *bytes.Buffer) {
	t.Helper()
	pool := parameters.NewTestPool(t)
	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buffer, nil))
	return parameters.NewService(pool, logger, clock.Fixed(fixedNow)), pool, buffer
}

// offlineService never reaches a database: a lazy pool that is not used when
// the request is rejected before any query.
func offlineService(t testing.TB) *parameters.Service {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("lazy pool: %v", err)
	}
	t.Cleanup(pool.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return parameters.NewService(pool, logger, clock.Fixed(fixedNow))
}

// asAPIError returns the *api.Error inside err or fails the test.
func asAPIError(t testing.TB, err error) *api.Error {
	t.Helper()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("want an *api.Error, got %v", err)
	}
	return apiErr
}

// issueList renders the issues as "field:CODE" in order.
func issueList(apiErr *api.Error) []string {
	list := make([]string, 0, len(apiErr.Issues))
	for _, issue := range apiErr.Issues {
		list = append(list, fmt.Sprintf("%s:%s", issue.Field, issue.Code))
	}
	return list
}

// requireValidation asserts a 422 VALIDATION_FAILED with exactly want issues.
func requireValidation(t testing.TB, err error, want []string) {
	t.Helper()
	apiErr := asAPIError(t, err)
	if apiErr.Status != http.StatusUnprocessableEntity || apiErr.Code != api.CodeValidationFailed {
		t.Fatalf("want 422 VALIDATION_FAILED, got %d %s", apiErr.Status, apiErr.Code)
	}
	got := issueList(apiErr)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("issues:\n got  %v\n want %v", got, want)
	}
}

// requireStatus asserts an *api.Error with the status and code.
func requireStatus(t testing.TB, err error, status int, code api.Code) *api.Error {
	t.Helper()
	apiErr := asAPIError(t, err)
	if apiErr.Status != status || apiErr.Code != code {
		t.Fatalf("want %d %s, got %d %s", status, code, apiErr.Status, apiErr.Code)
	}
	return apiErr
}

// mustCreate creates a parameter from a JSON body.
func mustCreate(t testing.TB, svc *parameters.Service, p reqctx.Principal, s string) parameters.View {
	t.Helper()
	view, err := svc.Create(context.Background(), p, body(t, s))
	if err != nil {
		t.Fatalf("create %s: %v", s, err)
	}
	return view
}

// pageOf is the first page of 100 sorted by key.
func pageOf(size int) api.PageQuery {
	return api.PageQuery{Page: 1, Size: size, SortField: "key"}
}
