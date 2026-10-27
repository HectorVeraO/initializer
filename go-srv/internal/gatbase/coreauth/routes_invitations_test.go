package coreauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
)

// invEnv is the module behind its real HTTP chain with a fake Kratos public
// and admin API at the network boundary.
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

// mixedCaseDomain exercises email normalization with an example domain in non-canonical case.
const mixedCaseDomain = "Example.com"

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

func invitationJSON(email, role string) string {
	body, _ := json.Marshal(map[string]string{"email": email, "role": role})
	return string(body)
}

func tokenJSON(token string) string {
	body, _ := json.Marshal(map[string]string{"token": token})
	return string(body)
}

func acceptNewJSON(token, password, fullName string) string {
	body, _ := json.Marshal(map[string]string{"token": token, "password": password, "fullName": fullName})
	return string(body)
}

func TestCreateTenantReturnsTenantInvitationAndOneTimeURL(t *testing.T) {
	e := newInvEnv(t, "")
	_, operator := e.actor("", "", true)

	rec := operator.send(http.MethodPost, "/api/v1/tenants", `{"slug":"tenant-a","name":"  Tenant A  ","adminEmail":"Admin.A@`+mixedCaseDomain+`"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeData[TenantCreated](t, rec)
	if created.Tenant.Slug != "tenant-a" || created.Tenant.Name != "Tenant A" || created.Tenant.Status != "active" {
		t.Fatalf("tenant = %+v", created.Tenant)
	}
	if created.Invitation.Email != "admin.a@example.com" || created.Invitation.Role != "admin" || created.Invitation.Status != "pending" {
		t.Fatalf("invitation = %+v", created.Invitation)
	}
	tokenOf(t, created.InvitationURL)
}

func TestCreateTenantRefusesANonOperator(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)

	rec := admin.send(http.MethodPost, "/api/v1/tenants", `{"slug":"tenant-b","name":"B","adminEmail":"b@example.com"}`)

	expectError(t, rec, http.StatusForbidden, "PLATFORM_OPERATOR_REQUIRED")
}

func TestCreateTenantRefusesAnAnonymousCaller(t *testing.T) {
	e := newInvEnv(t, "")

	rec := e.client().send(http.MethodPost, "/api/v1/tenants", `{"slug":"tenant-b","name":"B","adminEmail":"b@example.com"}`)

	expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
}

func TestCreateTenantTakenSlugIsConflict(t *testing.T) {
	e := newInvEnv(t, "")
	e.tenant("tenant-a", "active")
	_, operator := e.actor("", "", true)

	rec := operator.send(http.MethodPost, "/api/v1/tenants", `{"slug":"tenant-a","name":"A","adminEmail":"a@example.com"}`)

	expectError(t, rec, http.StatusConflict, "TENANT_SLUG_TAKEN")
}

func TestCreateTenantAcceptsANameAtTheLengthLimit(t *testing.T) {
	e := newInvEnv(t, "")
	_, operator := e.actor("", "", true)

	rec := operator.send(http.MethodPost, "/api/v1/tenants",
		`{"slug":"tenant-a","name":"`+strings.Repeat("a", 100)+`","adminEmail":"a@example.com"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("100-character name answered %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTenantValidationAccumulatesInFieldOrder(t *testing.T) {
	e := newInvEnv(t, "")
	_, operator := e.actor("", "", true)
	long := strings.Repeat("a", 101)
	cases := []struct {
		name string
		body string
		want []wantIssue
	}{
		{"uuid slug, empty name, bad email",
			`{"slug":"0b9c4c6e-0000-4000-8000-000000000001","name":"","adminEmail":"x"}`,
			[]wantIssue{{"slug", "FIELD_INVALID_FORMAT"}, {"name", "FIELD_REQUIRED"}, {"adminEmail", "FIELD_INVALID_FORMAT"}}},
		{"all missing", `{}`,
			[]wantIssue{{"slug", "FIELD_REQUIRED"}, {"name", "FIELD_REQUIRED"}, {"adminEmail", "FIELD_REQUIRED"}}},
		{"all null", `{"slug":null,"name":null,"adminEmail":null}`,
			[]wantIssue{{"slug", "FIELD_REQUIRED"}, {"name", "FIELD_REQUIRED"}, {"adminEmail", "FIELD_REQUIRED"}}},
		{"wrong types", `{"slug":1,"name":true,"adminEmail":[]}`,
			[]wantIssue{{"slug", "FIELD_TYPE_INVALID"}, {"name", "FIELD_TYPE_INVALID"}, {"adminEmail", "FIELD_TYPE_INVALID"}}},
		{"blank name, uppercase slug", `{"slug":"Tenant-A","name":"   ","adminEmail":"a@example.com"}`,
			[]wantIssue{{"slug", "FIELD_INVALID_FORMAT"}, {"name", "FIELD_REQUIRED"}}},
		{"NUL in name", `{"slug":"tenant-a","name":"Tenant\u0000A","adminEmail":"a@example.com"}`,
			[]wantIssue{{"name", "FIELD_INVALID_FORMAT"}}},
		{"too long", `{"slug":"tenant-a","name":"` + long + `","adminEmail":"` + strings.Repeat("a", 250) + `@example.com"}`,
			[]wantIssue{{"name", "FIELD_TOO_LONG"}, {"adminEmail", "FIELD_TOO_LONG"}}},
		{"slug edge hyphen and short", `{"slug":"-ab","name":"A","adminEmail":"a@example.com"}`,
			[]wantIssue{{"slug", "FIELD_INVALID_FORMAT"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectIssues(t, operator.send(http.MethodPost, "/api/v1/tenants", tc.body), tc.want)
		})
	}
}

func TestCreateInvitationReturnsTheURLOnceAndListingNeverShowsIt(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)

	rec := admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("Jane.Doe@"+mixedCaseDomain, "viewer"))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeData[InvitationCreated](t, rec)
	token := tokenOf(t, created.InvitationURL)
	if created.Invitation.Email != "jane.doe@example.com" || created.Invitation.Role != "viewer" || created.Invitation.Status != "pending" {
		t.Fatalf("invitation = %+v", created.Invitation)
	}

	list := admin.send(http.MethodGet, "/api/v1/invitations", "")
	page := decodeData[api.Page[InvitationView]](t, list)
	if page.TotalItems != 1 || len(page.Items) != 1 || page.Items[0].ID != created.Invitation.ID {
		t.Fatalf("page = %+v", page)
	}
	for _, forbidden := range []string{token, "invitationUrl", "token", "hash"} {
		if strings.Contains(strings.ToLower(list.Body.String()), strings.ToLower(forbidden)) {
			t.Fatalf("listing contains %q: %s", forbidden, list.Body.String())
		}
	}
}

func TestCreateInvitationValidationAccumulates(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	cases := []struct {
		name string
		body string
		want []wantIssue
	}{
		{"missing", `{}`, []wantIssue{{"email", "FIELD_REQUIRED"}, {"role", "FIELD_REQUIRED"}}},
		{"types", `{"email":5,"role":[]}`, []wantIssue{{"email", "FIELD_TYPE_INVALID"}, {"role", "FIELD_TYPE_INVALID"}}},
		{"format and role", `{"email":"not-an-email","role":"owner"}`, []wantIssue{{"email", "FIELD_INVALID_FORMAT"}, {"role", "FIELD_NOT_ALLOWED"}}},
		{"too long", `{"email":"` + strings.Repeat("a", 250) + `@example.com","role":"admin"}`, []wantIssue{{"email", "FIELD_TOO_LONG"}}},
		{"empty role", `{"email":"a@example.com","role":""}`, []wantIssue{{"role", "FIELD_REQUIRED"}}},
		{"empty email", `{"email":"","role":"admin"}`, []wantIssue{{"email", "FIELD_REQUIRED"}}},
		{"whitespace email", `{"email":"   ","role":"admin"}`, []wantIssue{{"email", "FIELD_REQUIRED"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectIssues(t, admin.send(http.MethodPost, "/api/v1/invitations", tc.body), tc.want)
		})
	}
}

func TestInvitationRoutesEnforceRoles(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, viewer := e.actor(tenant, "viewer", false)
	_, member := e.actor(tenant, "member", false)
	_, tenantless := e.actor("", "", false)
	valid := invitationJSON("jane.doe@example.com", "member")

	expectError(t, viewer.send(http.MethodPost, "/api/v1/invitations", valid), http.StatusForbidden, "INSUFFICIENT_ROLE")
	expectError(t, member.send(http.MethodGet, "/api/v1/invitations", ""), http.StatusForbidden, "INSUFFICIENT_ROLE")
	expectError(t, member.send(http.MethodDelete, "/api/v1/invitations/"+randomUUID(t), ""), http.StatusForbidden, "INSUFFICIENT_ROLE")
	expectError(t, tenantless.send(http.MethodPost, "/api/v1/invitations", valid), http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")
	expectError(t, e.client().send(http.MethodPost, "/api/v1/invitations", valid), http.StatusUnauthorized, "SESSION_INVALID")
}

func TestInviteOfAnOperatorAddressNeedsAnOperator(t *testing.T) {
	e := newInvEnv(t, "ops@example.com")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)

	rec := admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("ops@example.com", "member"))

	expectError(t, rec, http.StatusForbidden, "PLATFORM_OPERATOR_REQUIRED")
}

func TestCreateInvitationForAnActiveMemberIsConflict(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	jane, _ := e.actor(tenant, "member", false)

	rec := admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON(strings.ToUpper(jane.email), "member"))

	expectError(t, rec, http.StatusConflict, "MEMBERSHIP_ALREADY_ACTIVE")
}

func TestInvitationBodyTransportLimits(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	headers := func(contentType string) map[string]string {
		return map[string]string{csrfHeader: admin.cookies["XSRF-TOKEN"], "Content-Type": contentType}
	}

	malformed := admin.send(http.MethodPost, "/api/v1/invitations", `{"email":`)
	expectIssues(t, malformed, []wantIssue{{"", "BODY_MALFORMED"}})

	huge := `{"email":"` + strings.Repeat("a", api.MaxBodyBytes+1) + `"}`
	expectError(t, admin.send(http.MethodPost, "/api/v1/invitations", huge), http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")

	plain := admin.doBody(http.MethodPost, "/api/v1/invitations", headers("text/plain"), invitationJSON("a@example.com", "member"))
	expectError(t, plain, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
}

func TestListInvitationsFiltersByStatusAndTenant(t *testing.T) {
	e := newInvEnv(t, "")
	tenantA := e.tenant("tenant-a", "active")
	tenantB := e.tenant("tenant-b", "active")
	_, adminA := e.actor(tenantA, "admin", false)
	_, adminB := e.actor(tenantB, "admin", false)
	pending := decodeData[InvitationCreated](t, adminA.send(http.MethodPost, "/api/v1/invitations", invitationJSON("p@example.com", "member")))
	revoked := decodeData[InvitationCreated](t, adminA.send(http.MethodPost, "/api/v1/invitations", invitationJSON("r@example.com", "member")))
	adminA.send(http.MethodDelete, "/api/v1/invitations/"+revoked.Invitation.ID, "")
	adminB.send(http.MethodPost, "/api/v1/invitations", invitationJSON("other@example.com", "member"))

	cases := map[string]string{"": pending.Invitation.ID, "pending": pending.Invitation.ID, "revoked": revoked.Invitation.ID}
	for status, wantID := range cases {
		path := "/api/v1/invitations"
		if status != "" {
			path += "?status=" + status
		}
		page := decodeData[api.Page[InvitationView]](t, adminA.send(http.MethodGet, path, ""))
		if len(page.Items) != 1 || page.Items[0].ID != wantID || page.TotalItems != 1 {
			t.Fatalf("status %q: page = %+v", status, page)
		}
	}
	emptyStatus := decodeData[api.Page[InvitationView]](t, adminA.send(http.MethodGet, "/api/v1/invitations?status=", ""))
	if len(emptyStatus.Items) != 1 || emptyStatus.Items[0].ID != pending.Invitation.ID {
		t.Fatalf("empty status: page = %+v", emptyStatus)
	}
	page := decodeData[api.Page[InvitationView]](t, adminA.send(http.MethodGet, "/api/v1/invitations?status=expired", ""))
	if len(page.Items) != 0 || page.Items == nil {
		t.Fatalf("status expired: page = %+v", page)
	}

	jane := e.person()
	janeClient := e.signIn(jane)
	joined := decodeData[InvitationCreated](t, adminA.send(http.MethodPost, "/api/v1/invitations", invitationJSON(jane.email, "member")))
	if rec := janeClient.send(http.MethodPost, "/api/v1/invitations/accept", tokenJSON(tokenOf(t, joined.InvitationURL))); rec.Code != http.StatusOK {
		t.Fatalf("accept answered %d: %s", rec.Code, rec.Body.String())
	}
	page = decodeData[api.Page[InvitationView]](t, adminA.send(http.MethodGet, "/api/v1/invitations?status=accepted", ""))
	if len(page.Items) != 1 || page.Items[0].ID != joined.Invitation.ID || page.Items[0].Status != "accepted" || page.TotalItems != 1 {
		t.Fatalf("status accepted: page = %+v", page)
	}
	pendingAfter := decodeData[api.Page[InvitationView]](t, adminA.send(http.MethodGet, "/api/v1/invitations?status=pending", ""))
	if len(pendingAfter.Items) != 1 || pendingAfter.Items[0].ID != pending.Invitation.ID {
		t.Fatalf("pending after accept = %+v", pendingAfter)
	}
}

func TestListInvitationsShowsExpiredAndPagesNewestFirst(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	first := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("one@example.com", "member")))
	e.clock.advance(time.Minute)
	second := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("two@example.com", "member")))

	page := decodeData[api.Page[InvitationView]](t, admin.send(http.MethodGet, "/api/v1/invitations?size=1&page=2", ""))
	if len(page.Items) != 1 || page.Items[0].ID != first.Invitation.ID || page.TotalItems != 2 || page.Page != 2 || page.Size != 1 {
		t.Fatalf("page 2 = %+v", page)
	}
	ascending := decodeData[api.Page[InvitationView]](t, admin.send(http.MethodGet, "/api/v1/invitations?sort=createdAt", ""))
	if ascending.Items[0].ID != first.Invitation.ID || ascending.Items[1].ID != second.Invitation.ID {
		t.Fatalf("ascending = %+v", ascending)
	}

	e.exec("UPDATE ten_invitations SET expires_at = $1", e.clock.Now().Add(-time.Hour))
	expired := decodeData[api.Page[InvitationView]](t, admin.send(http.MethodGet, "/api/v1/invitations?status=expired", ""))
	if expired.TotalItems != 2 || expired.Items[0].Status != "expired" {
		t.Fatalf("expired = %+v", expired)
	}
}

func TestListInvitationsRejectsBadQueryWithAccumulatedIssues(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)

	rec := admin.send(http.MethodGet, "/api/v1/invitations?status=gone&page=0&size=abc&sort=email", "")

	expectIssues(t, rec, []wantIssue{
		{"status", "FIELD_NOT_ALLOWED"}, {"page", "FIELD_OUT_OF_RANGE"},
		{"size", "FIELD_TYPE_INVALID"}, {"sort", "FIELD_NOT_ALLOWED"},
	})
}

func TestRevokeInvitationAnswersOneNotFoundBodyForEveryUnreachableID(t *testing.T) {
	e := newInvEnv(t, "")
	tenantA := e.tenant("tenant-a", "active")
	tenantB := e.tenant("tenant-b", "active")
	_, adminA := e.actor(tenantA, "admin", false)
	_, adminB := e.actor(tenantB, "admin", false)
	foreign := decodeData[InvitationCreated](t, adminB.send(http.MethodPost, "/api/v1/invitations", invitationJSON("x@example.com", "member")))

	var bodies []*api.Error
	for _, id := range []string{foreign.Invitation.ID, randomUUID(t), "not-a-uuid"} {
		rec := adminA.send(http.MethodDelete, "/api/v1/invitations/"+url.PathEscape(id), "")
		expectError(t, rec, http.StatusNotFound, "RESOURCE_NOT_FOUND")
		bodies = append(bodies, errorBody(t, rec))
	}
	if !reflect.DeepEqual(bodies[0], bodies[1]) || !reflect.DeepEqual(bodies[1], bodies[2]) {
		t.Fatalf("bodies differ: %+v", bodies)
	}
	if got := e.scan("SELECT count(*)::text FROM ten_invitations WHERE id = $1::uuid AND revoked_at IS NULL", foreign.Invitation.ID); got != "1" {
		t.Fatalf("foreign invitation was touched")
	}
}

func TestRevokeInvitationThenRevokeAgainIsNotPending(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	created := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("a@example.com", "member")))
	token := tokenOf(t, created.InvitationURL)

	rec := admin.send(http.MethodDelete, "/api/v1/invitations/"+created.Invitation.ID, "")
	view := decodeData[InvitationView](t, rec)
	if view.Status != "revoked" || view.RevokedAt == nil {
		t.Fatalf("view = %+v", view)
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Fatal("revoke answer carries the token")
	}

	expectError(t, admin.send(http.MethodDelete, "/api/v1/invitations/"+created.Invitation.ID, ""), http.StatusConflict, "INVITATION_NOT_PENDING")
	expectError(t, e.client().send(http.MethodPost, "/api/v1/invitations/accept-new", acceptNewJSON(token, randomPassword(t), "Jane Doe")),
		http.StatusNotFound, "INVITATION_INVALID")
}

func TestAcceptInvitationRequiresTheInvitedEmailAndDoesNotSwitchTheSession(t *testing.T) {
	e := newInvEnv(t, "")
	tenantA := e.tenant("tenant-a", "active")
	tenantB := e.tenant("tenant-b", "active")
	_, admin := e.actor(tenantA, "admin", false)
	stranger, strangerClient := e.actor(tenantB, "admin", false)
	jane := e.person()
	janeClient := e.signIn(jane)
	created := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON(strings.ToUpper(jane.email), "viewer")))
	token := tokenOf(t, created.InvitationURL)
	_ = stranger

	mismatch := strangerClient.send(http.MethodPost, "/api/v1/invitations/accept", tokenJSON(token))
	expectError(t, mismatch, http.StatusForbidden, "INVITATION_EMAIL_MISMATCH")

	rec := janeClient.send(http.MethodPost, "/api/v1/invitations/accept", tokenJSON(token))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeData[AcceptResult](t, rec)
	if result.Membership.Role != "viewer" || result.Membership.Status != "active" || result.Membership.Tenant.Slug != "tenant-a" {
		t.Fatalf("membership = %+v", result.Membership)
	}
	if names := setCookieNames(rec); len(names) != 0 {
		t.Fatalf("accept set cookies %v", names)
	}
	expectError(t, janeClient.send(http.MethodPost, "/api/v1/invitations/accept", tokenJSON(token)), http.StatusNotFound, "INVITATION_INVALID")
}

func TestAcceptInvitationValidationAndUnknownToken(t *testing.T) {
	e := newInvEnv(t, "")
	_, client := e.actor("", "", false)
	cases := []struct {
		name string
		body string
		want []wantIssue
	}{
		{"missing", `{}`, []wantIssue{{"token", "FIELD_REQUIRED"}}},
		{"type", `{"token":7}`, []wantIssue{{"token", "FIELD_TYPE_INVALID"}}},
		{"empty", `{"token":""}`, []wantIssue{{"token", "FIELD_REQUIRED"}}},
		{"whitespace", `{"token":"   "}`, []wantIssue{{"token", "FIELD_INVALID_FORMAT"}}},
		{"format", `{"token":"short"}`, []wantIssue{{"token", "FIELD_INVALID_FORMAT"}}},
		{"padding", `{"token":"` + strings.Repeat("a", 42) + `="}`, []wantIssue{{"token", "FIELD_INVALID_FORMAT"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectIssues(t, client.send(http.MethodPost, "/api/v1/invitations/accept", tc.body), tc.want)
		})
	}
	unknown, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	expectError(t, client.send(http.MethodPost, "/api/v1/invitations/accept", tokenJSON(unknown)), http.StatusNotFound, "INVITATION_INVALID")
}

func TestAcceptNewCreatesTheAccountAndNeverEchoesSecrets(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	created := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("Jane.Doe@"+mixedCaseDomain, "viewer")))
	token := tokenOf(t, created.InvitationURL)
	password := randomPassword(t)
	anonymous := e.client()

	rec := anonymous.send(http.MethodPost, "/api/v1/invitations/accept-new", acceptNewJSON(token, password, "  Jane Doe  "))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeData[AcceptNewResult](t, rec)
	if result.Membership.Role != "viewer" || result.Membership.Status != "active" || result.Membership.Tenant.Slug != "tenant-a" {
		t.Fatalf("membership = %+v", result.Membership)
	}
	if result.Account.Email != "jane.doe@example.com" || result.Account.FullName == nil || *result.Account.FullName != "Jane Doe" {
		t.Fatalf("account = %+v", result.Account)
	}
	if names := setCookieNames(rec); len(names) != 0 {
		t.Fatalf("accept-new set cookies %v", names)
	}
	for _, secret := range []string{token, password} {
		if strings.Contains(rec.Body.String(), secret) || strings.Contains(e.logs.String(), secret) {
			t.Fatal("a secret leaked into the answer or the logs")
		}
	}
	expectError(t, anonymous.send(http.MethodPost, "/api/v1/invitations/accept-new", acceptNewJSON(token, password, "Jane Doe")),
		http.StatusNotFound, "INVITATION_INVALID")
	expectError(t, anonymous.send(http.MethodPost, "/api/v1/invitations/accept", tokenJSON(token)), http.StatusUnauthorized, "SESSION_INVALID")
}

func TestAcceptNewSupersededTokenIsInvalid(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	first := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("jane@example.com", "member")))
	second := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("jane@example.com", "member")))
	anonymous := e.client()

	stale := anonymous.send(http.MethodPost, "/api/v1/invitations/accept-new", acceptNewJSON(tokenOf(t, first.InvitationURL), randomPassword(t), "Jane Doe"))
	fresh := anonymous.send(http.MethodPost, "/api/v1/invitations/accept-new", acceptNewJSON(tokenOf(t, second.InvitationURL), randomPassword(t), "Jane Doe"))

	expectError(t, stale, http.StatusNotFound, "INVITATION_INVALID")
	if fresh.Code != http.StatusCreated {
		t.Fatalf("fresh token answered %d", fresh.Code)
	}
}

func TestAcceptRoutesRefuseAnExpiredInvitation(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	jane := e.person()
	janeClient := e.signIn(jane)
	created := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON(jane.email, "member")))
	token := tokenOf(t, created.InvitationURL)
	e.exec("UPDATE ten_invitations SET expires_at = $1", e.clock.Now().Add(-time.Hour))

	viaSession := janeClient.send(http.MethodPost, "/api/v1/invitations/accept", tokenJSON(token))
	viaNew := e.client().send(http.MethodPost, "/api/v1/invitations/accept-new", acceptNewJSON(token, randomPassword(t), "Jane Doe"))

	expectError(t, viaSession, http.StatusNotFound, "INVITATION_INVALID")
	expectError(t, viaNew, http.StatusNotFound, "INVITATION_INVALID")
}

func TestAcceptIntoASuspendedTenantIsForbidden(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	jane := e.person()
	janeClient := e.signIn(jane)
	created := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON(jane.email, "member")))
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant)

	rec := janeClient.send(http.MethodPost, "/api/v1/invitations/accept", tokenJSON(tokenOf(t, created.InvitationURL)))

	expectError(t, rec, http.StatusForbidden, "TENANT_SUSPENDED")
}

func TestAcceptNewValidationAccumulatesInFieldOrder(t *testing.T) {
	e := newInvEnv(t, "")
	anonymous := e.client()
	token, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		body string
		want []wantIssue
	}{
		{"all bad", `{"token":"short","password":"short","fullName":"  "}`,
			[]wantIssue{{"token", "FIELD_INVALID_FORMAT"}, {"password", "FIELD_TOO_SHORT"}, {"fullName", "FIELD_REQUIRED"}}},
		{"missing", `{}`,
			[]wantIssue{{"token", "FIELD_REQUIRED"}, {"password", "FIELD_REQUIRED"}, {"fullName", "FIELD_REQUIRED"}}},
		{"types", `{"token":1,"password":2,"fullName":3}`,
			[]wantIssue{{"token", "FIELD_TYPE_INVALID"}, {"password", "FIELD_TYPE_INVALID"}, {"fullName", "FIELD_TYPE_INVALID"}}},
		{"too long", `{"token":"` + token + `","password":"` + strings.Repeat("p", 65) + `","fullName":"` + strings.Repeat("n", 101) + `"}`,
			[]wantIssue{{"password", "FIELD_TOO_LONG"}, {"fullName", "FIELD_TOO_LONG"}}},
		{"NUL in fullName", `{"token":"` + token + `","password":"` + strings.Repeat("p", 12) + `","fullName":"Jane\u0000Doe"}`,
			[]wantIssue{{"fullName", "FIELD_INVALID_FORMAT"}}},
		{"empty strings", `{"token":"` + token + `","password":"","fullName":""}`,
			[]wantIssue{{"password", "FIELD_REQUIRED"}, {"fullName", "FIELD_REQUIRED"}}},
		{"empty token", `{"token":"","password":"` + strings.Repeat("p", 12) + `","fullName":"Jane"}`,
			[]wantIssue{{"token", "FIELD_REQUIRED"}}},
		{"spaces count in a password", `{"token":"` + token + `","password":"           x","fullName":"   "}`,
			[]wantIssue{{"fullName", "FIELD_REQUIRED"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectIssues(t, anonymous.send(http.MethodPost, "/api/v1/invitations/accept-new", tc.body), tc.want)
		})
	}
	if n := len(e.admin.received()); n != 0 {
		t.Fatalf("Kratos received %d requests for invalid bodies", n)
	}
}

func TestAcceptNewPasswordLengthCountsRunes(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	created := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("jane@example.com", "member")))

	short := e.client().send(http.MethodPost, "/api/v1/invitations/accept-new",
		acceptNewJSON(tokenOf(t, created.InvitationURL), strings.Repeat("ñ", 11), "Jane Doe"))
	rec := e.client().send(http.MethodPost, "/api/v1/invitations/accept-new",
		acceptNewJSON(tokenOf(t, created.InvitationURL), strings.Repeat("ñ", 12), "Jane Doe"))

	expectIssues(t, short, []wantIssue{{"password", "FIELD_TOO_SHORT"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("12 two-byte runes answered %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAcceptNewPasswordUpperEdge(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	created := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("jane@example.com", "member")))

	tooLong := e.client().send(http.MethodPost, "/api/v1/invitations/accept-new",
		acceptNewJSON(tokenOf(t, created.InvitationURL), strings.Repeat("p", 65), "Jane Doe"))
	atLimit := e.client().send(http.MethodPost, "/api/v1/invitations/accept-new",
		acceptNewJSON(tokenOf(t, created.InvitationURL), strings.Repeat("p", 64), "Jane Doe"))

	expectIssues(t, tooLong, []wantIssue{{"password", "FIELD_TOO_LONG"}})
	if atLimit.Code != http.StatusCreated {
		t.Fatalf("64-character password answered %d: %s", atLimit.Code, atLimit.Body.String())
	}
}

func TestCreateInvitationEmailLengthEdge(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	const domain = "@example.com"

	tooLong := admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON(strings.Repeat("a", 255-len(domain))+domain, "member"))
	atLimit := admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON(strings.Repeat("a", 254-len(domain))+domain, "member"))

	expectIssues(t, tooLong, []wantIssue{{"email", "FIELD_TOO_LONG"}})
	if atLimit.Code != http.StatusCreated {
		t.Fatalf("254-character email answered %d: %s", atLimit.Code, atLimit.Body.String())
	}
}

func TestAcceptNewWithAnExistingIdentityIsConflictAndAcceptRecovers(t *testing.T) {
	e := newInvEnv(t, "")
	tenantA := e.tenant("tenant-a", "active")
	tenantB := e.tenant("tenant-b", "active")
	_, adminA := e.actor(tenantA, "admin", false)
	w, wClient := e.actor(tenantB, "admin", false)
	e.admin.addIdentity(w.email)
	created := decodeData[InvitationCreated](t, adminA.send(http.MethodPost, "/api/v1/invitations", invitationJSON(w.email, "member")))
	token := tokenOf(t, created.InvitationURL)

	conflict := e.client().send(http.MethodPost, "/api/v1/invitations/accept-new", acceptNewJSON(token, randomPassword(t), "W User"))
	accepted := wClient.send(http.MethodPost, "/api/v1/invitations/accept", tokenJSON(token))

	expectError(t, conflict, http.StatusConflict, "IDENTITY_ALREADY_EXISTS")
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept after conflict answered %d: %s", accepted.Code, accepted.Body.String())
	}
}

func TestAcceptNewMapsKratosRefusals(t *testing.T) {
	cases := []struct {
		name   string
		status int
		check  func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{"password policy", http.StatusBadRequest, func(t *testing.T, rec *httptest.ResponseRecorder) {
			expectIssues(t, rec, []wantIssue{{"password", "PASSWORD_REJECTED"}})
		}},
		{"unavailable", http.StatusServiceUnavailable, func(t *testing.T, rec *httptest.ResponseRecorder) {
			expectError(t, rec, http.StatusServiceUnavailable, "IDENTITY_PROVIDER_UNAVAILABLE")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newInvEnv(t, "")
			tenant := e.tenant("tenant-a", "active")
			_, admin := e.actor(tenant, "admin", false)
			created := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("jane@example.com", "member")))
			token := tokenOf(t, created.InvitationURL)
			password := randomPassword(t)
			e.admin.mu.Lock()
			e.admin.rejectStatus = tc.status
			e.admin.mu.Unlock()

			rec := e.client().send(http.MethodPost, "/api/v1/invitations/accept-new", acceptNewJSON(token, password, "Jane Doe"))

			tc.check(t, rec)
			if strings.Contains(rec.Body.String(), password) || strings.Contains(e.logs.String(), password) {
				t.Fatal("the password leaked")
			}
			if got := e.scan("SELECT count(*)::text FROM ten_invitations WHERE accepted_at IS NULL AND revoked_at IS NULL"); got != "1" {
				t.Fatalf("the invitation did not stay pending")
			}
		})
	}
}

func TestAcceptNewIntoASuspendedTenantIsForbidden(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	created := decodeData[InvitationCreated](t, admin.send(http.MethodPost, "/api/v1/invitations", invitationJSON("jane@example.com", "member")))
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant)

	rec := e.client().send(http.MethodPost, "/api/v1/invitations/accept-new", acceptNewJSON(tokenOf(t, created.InvitationURL), randomPassword(t), "Jane Doe"))

	expectError(t, rec, http.StatusForbidden, "TENANT_SUSPENDED")
}

func FuzzRequestFieldReaders(f *testing.F) {
	for _, seed := range []string{"", "tenant-a", "0b9c4c6e-0000-4000-8000-000000000001", "Jane@" + mixedCaseDomain, "  ", strings.Repeat("a", 43), "ñññ", "\x00", "a@b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		encoded, err := json.Marshal(map[string]string{"v": value})
		if err != nil {
			t.Skip()
		}
		readers := map[string]func(*api.Body, *api.Problems) (string, bool){
			"slug":     func(b *api.Body, p *api.Problems) (string, bool) { return readSlug(b, p) },
			"email":    func(b *api.Body, p *api.Problems) (string, bool) { return readEmail(b, p, "v") },
			"token":    func(b *api.Body, p *api.Problems) (string, bool) { return readToken(b, p) },
			"password": func(b *api.Body, p *api.Problems) (string, bool) { return readPassword(b, p) },
			"name": func(b *api.Body, p *api.Problems) (string, bool) {
				return readTrimmedName(b, p, "v", maxFullNameLength)
			},
		}
		for name, read := range readers {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Replace(string(encoded), `"v"`, `"`+fieldOf(name)+`"`, 1)))
			req.Header.Set("Content-Type", "application/json")
			body, err := api.DecodeBody(req)
			if err != nil {
				t.Fatalf("%s: decode: %v", name, err)
			}
			var problems api.Problems
			got, ok := read(body, &problems)
			if ok != problems.Empty() {
				t.Fatalf("%s: ok=%v but issues empty=%v for %q", name, ok, problems.Empty(), value)
			}
			if ok && !validAccepted(name, value, got) {
				t.Fatalf("%s accepted %q as %q", name, value, got)
			}
		}
	})
}

func fieldOf(reader string) string {
	switch reader {
	case "slug", "token", "password":
		return reader
	}
	return "v"
}

func validAccepted(reader, raw, got string) bool {
	switch reader {
	case "slug":
		return isValidSlug(got) && got == raw
	case "token":
		return tokenPattern43.MatchString(got)
	case "email":
		return got == strings.ToLower(got) && len(got) <= maxEmailChars && strings.Contains(got, "@")
	case "password":
		return got == raw
	}
	return got == strings.TrimSpace(got) && got != "" && !strings.ContainsRune(got, 0)
}
