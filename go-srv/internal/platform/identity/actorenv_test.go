package identity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
)

// invEnv is the module behind its real HTTP chain with a fake Kratos public
// and admin API at the network boundary, and signed-in actors.
type invEnv struct {
	*sessEnv
	admin *fakeKratos
}

func newInvEnv(t *testing.T, operatorEmails string) *invEnv {
	t.Helper()
	admin := newFakeKratos(t)
	env := newSessEnv(t, sessEnvOptions{adminURL: admin.server.URL, operatorEmails: operatorEmails})
	return &invEnv{sessEnv: env, admin: admin}
}

var invStart = time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)

// actor creates a person, makes them a member of tenantID with role (when
// tenantID is not empty) and signs them in.
func (e *invEnv) actor(tenantID, role string, operator bool) (person, *sessClient) {
	e.t.Helper()
	p := e.person()
	if operator {
		e.exec("UPDATE iam_accounts SET is_platform_operator = true WHERE id = $1::uuid", p.accountID)
	}
	if tenantID != "" {
		e.member(tenantID, p.accountID, role, "active", invStart)
	}
	return p, e.signIn(p)
}

func (c *sessClient) send(method, path, body string) *httptest.ResponseRecorder {
	c.e.t.Helper()
	return c.doBody(method, path, map[string]string{csrfHeader: c.cookies["XSRF-TOKEN"]}, body)
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) *api.Error {
	t.Helper()
	var envelope api.Envelope[json.RawMessage]
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	if err != nil || envelope.Error == nil {
		t.Fatalf("not an error envelope (status %d): %s", rec.Code, rec.Body.String())
	}
	return envelope.Error
}

type wantIssue struct{ field, code string }

func expectIssues(t *testing.T, rec *httptest.ResponseRecorder, want []wantIssue) {
	t.Helper()
	expectError(t, rec, http.StatusUnprocessableEntity, "VALIDATION_FAILED")
	var got []wantIssue
	for _, issue := range errorBody(t, rec).Issues {
		got = append(got, wantIssue{issue.Field, string(issue.Code)})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}
