package coretest_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	tenantAlpha  = reqctx.TenantID("0b6f2a4e-7d1c-4c52-9a38-1f5e6d7c8b90")
	tenantBeta   = reqctx.TenantID("a1b2c3d4-e5f6-4a78-8b9c-0d1e2f3a4b5c")
	tenantGamma  = reqctx.TenantID("c3d4e5f6-0718-4a9b-8c0d-1e2f3a4b5c6d")
	tenantAbsent = reqctx.TenantID("ffffffff-ffff-4fff-8fff-ffffffffffff")
	accountOne   = reqctx.AccountID("11111111-1111-4111-8111-111111111111")
	accountTwo   = reqctx.AccountID("22222222-2222-4222-8222-222222222222")
)

func tenantRef(id reqctx.TenantID, slug string, status access.TenantStatus) access.TenantRef {
	return access.TenantRef{ID: id, Slug: slug, Name: "Tenant " + slug, Status: status}
}

func requireRejection(t *testing.T, err error, code api.Code) {
	t.Helper()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.Code != code {
		t.Fatalf("error = %v, want 403 %s", err, code)
	}
}

func TestTenantBookGrantChecksInOrder(t *testing.T) {
	ctx := context.Background()
	book := coretest.NewTenantBook(
		tenantRef(tenantAlpha, "alpha", access.TenantActive),
		tenantRef(tenantBeta, "beta", access.TenantSuspended),
		tenantRef(tenantGamma, "gamma", access.TenantActive),
	)
	book.AddMember(coretest.Member{ID: "m-1", Account: accountOne, Tenant: tenantAlpha, Role: reqctx.RoleAdmin, Status: "active"})
	book.AddMember(coretest.Member{ID: "m-2", Account: accountOne, Tenant: tenantBeta, Role: reqctx.RoleMember, Status: "revoked"})
	book.AddMember(coretest.Member{ID: "m-3", Account: accountOne, Tenant: tenantGamma, Role: reqctx.RoleViewer, Status: "revoked"})

	grant, err := book.Grant(ctx, accountOne, tenantAlpha)
	if err != nil || grant.Role != reqctx.RoleAdmin || grant.Tenant.Slug != "alpha" {
		t.Fatalf("active member: %+v, %v", grant, err)
	}

	_, err = book.Grant(ctx, accountTwo, tenantBeta)
	requireRejection(t, err, api.CodeTenantAccessDenied)
	_, err = book.Grant(ctx, accountOne, tenantAbsent)
	requireRejection(t, err, api.CodeTenantAccessDenied)
	_, err = book.Grant(ctx, accountOne, "not-a-uuid")
	requireRejection(t, err, api.CodeTenantAccessDenied)
	_, err = book.Grant(ctx, "not-a-uuid", tenantAlpha)
	requireRejection(t, err, api.CodeTenantAccessDenied)

	// A suspended tenant wins over the membership state.
	_, err = book.Grant(ctx, accountOne, tenantBeta)
	requireRejection(t, err, api.CodeTenantSuspended)
	_, err = book.Grant(ctx, accountOne, tenantGamma)
	requireRejection(t, err, api.CodeMembershipRevoked)

	book.SetTenantStatus(tenantGamma, access.TenantSuspended)
	_, err = book.Grant(ctx, accountOne, tenantGamma)
	requireRejection(t, err, api.CodeTenantSuspended)
}

func TestTenantBookSelectPrefersRememberedThenOldest(t *testing.T) {
	ctx := context.Background()
	book := coretest.NewTenantBook(
		tenantRef(tenantAlpha, "alpha", access.TenantActive),
		tenantRef(tenantBeta, "beta", access.TenantActive),
		tenantRef(tenantGamma, "gamma", access.TenantActive),
	)
	book.AddMember(coretest.Member{ID: "m-1", Account: accountOne, Tenant: tenantGamma, Role: reqctx.RoleViewer, Status: "revoked"})
	book.AddMember(coretest.Member{ID: "m-2", Account: accountOne, Tenant: tenantBeta, Role: reqctx.RoleMember, Status: "active"})
	book.AddMember(coretest.Member{ID: "m-3", Account: accountOne, Tenant: tenantAlpha, Role: reqctx.RoleAdmin, Status: "active"})

	oldest, err := book.Select(ctx, accountOne)
	if err != nil || oldest == nil || oldest.Tenant.ID != tenantBeta {
		t.Fatalf("without a remembered tenant: %+v, %v; want beta (oldest active)", oldest, err)
	}

	err = book.Remember(ctx, accountOne, tenantAlpha)
	if err != nil {
		t.Fatal(err)
	}
	remembered, err := book.Select(ctx, accountOne)
	if err != nil || remembered == nil || remembered.Tenant.ID != tenantAlpha || remembered.Role != reqctx.RoleAdmin {
		t.Fatalf("with a remembered tenant: %+v, %v; want alpha", remembered, err)
	}

	// A remembered tenant that Grant rejects falls through to the oldest.
	book.SetTenantStatus(tenantAlpha, access.TenantSuspended)
	fallback, err := book.Select(ctx, accountOne)
	if err != nil || fallback == nil || fallback.Tenant.ID != tenantBeta {
		t.Fatalf("with a rejected remembered tenant: %+v, %v; want beta", fallback, err)
	}

	none, err := book.Select(ctx, accountTwo)
	if err != nil || none != nil {
		t.Fatalf("account without memberships: %+v, %v; want nil, nil", none, err)
	}
}

func TestTenantBookRememberRejectsNonUUIDs(t *testing.T) {
	book := coretest.NewTenantBook(tenantRef(tenantAlpha, "alpha", access.TenantActive))
	err := book.Remember(context.Background(), accountOne, "not-a-uuid")
	var apiErr *api.Error
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("Remember(non-UUID) = %v; want an operational error", err)
	}
	err = book.Remember(context.Background(), "not-a-uuid", tenantAlpha)
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("Remember(non-UUID account) = %v; want an operational error", err)
	}
}

func TestTenantBookMembershipsAreActiveOnlyAndSlugOrdered(t *testing.T) {
	ctx := context.Background()
	book := coretest.NewTenantBook(
		tenantRef(tenantAlpha, "alpha", access.TenantActive),
		tenantRef(tenantBeta, "beta", access.TenantSuspended),
		tenantRef(tenantGamma, "gamma", access.TenantActive),
	)
	book.AddMember(coretest.Member{ID: "m-1", Account: accountOne, Tenant: tenantGamma, Role: reqctx.RoleViewer, Status: "active"})
	book.AddMember(coretest.Member{ID: "m-2", Account: accountOne, Tenant: tenantBeta, Role: reqctx.RoleMember, Status: "active"})
	book.AddMember(coretest.Member{ID: "m-3", Account: accountOne, Tenant: tenantAlpha, Role: reqctx.RoleAdmin, Status: "revoked"})
	book.AddMember(coretest.Member{ID: "m-4", Account: accountTwo, Tenant: tenantAlpha, Role: reqctx.RoleAdmin, Status: "active"})

	list, err := book.Memberships(ctx, accountOne)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Tenant.Slug != "beta" || list[1].Tenant.Slug != "gamma" {
		t.Fatalf("memberships = %+v; want beta (suspended tenant included) then gamma", list)
	}
	if list[0].Status != "active" || list[0].ID != "m-2" || list[0].Role != reqctx.RoleMember {
		t.Fatalf("first membership = %+v", list[0])
	}

	for _, id := range []reqctx.AccountID{"not-a-uuid", reqctx.AccountID(tenantAbsent)} {
		empty, err := book.Memberships(ctx, id)
		if err != nil || empty == nil || len(empty) != 0 {
			t.Fatalf("Memberships(%q) = %#v, %v; want empty non-nil", id, empty, err)
		}
	}
}

func TestTenantBookDirectory(t *testing.T) {
	ctx := context.Background()
	book := coretest.NewTenantBook(tenantRef(tenantBeta, "beta", access.TenantSuspended))

	ref, found, err := book.Tenant(ctx, tenantBeta)
	if err != nil || !found || ref.Slug != "beta" || ref.Status != access.TenantSuspended {
		t.Fatalf("Tenant(suspended) = %+v, %v, %v", ref, found, err)
	}
	exists, err := book.TenantExists(ctx, tenantBeta)
	if err != nil || !exists {
		t.Fatalf("TenantExists(suspended) = %v, %v", exists, err)
	}
	for _, id := range []reqctx.TenantID{tenantAbsent, "not-a-uuid", ""} {
		ref, found, err := book.Tenant(ctx, id)
		if err != nil || found || ref != (access.TenantRef{}) {
			t.Fatalf("Tenant(%q) = %+v, %v, %v; want zero, false, nil", id, ref, found, err)
		}
		exists, err := book.TenantExists(ctx, id)
		if err != nil || exists {
			t.Fatalf("TenantExists(%q) = %v, %v", id, exists, err)
		}
	}
}

func TestTenantBookPanicsOnUnknownTenants(t *testing.T) {
	book := coretest.NewTenantBook()
	for name, call := range map[string]func(){
		"AddMember":       func() { book.AddMember(coretest.Member{ID: "m-1", Account: accountOne, Tenant: tenantAlpha}) },
		"SetTenantStatus": func() { book.SetTenantStatus(tenantAlpha, access.TenantSuspended) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic for a tenant that is not in the book")
				}
			}()
			call()
		})
	}
}

func TestTenantBookIsSafeForConcurrentUse(t *testing.T) {
	ctx := context.Background()
	book := coretest.NewTenantBook(tenantRef(tenantAlpha, "alpha", access.TenantActive))
	book.AddMember(coretest.Member{ID: "m-1", Account: accountOne, Tenant: tenantAlpha, Role: reqctx.RoleAdmin, Status: "active"})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = book.Remember(ctx, accountOne, tenantAlpha)
			_, _ = book.Select(ctx, accountOne)
			_, _ = book.Memberships(ctx, accountOne)
			book.SetTenantStatus(tenantAlpha, access.TenantActive)
		}()
	}
	wg.Wait()
}

func TestAccountBookProvisionsAndFinds(t *testing.T) {
	ctx := context.Background()
	existing := accounts.Ref{ID: accountOne, Email: "user@example.com", Status: accounts.StatusActive}
	book := coretest.NewAccountBook([]string{"ops@example.com"}, existing)

	provisioned, err := book.Provision(ctx, accounts.NewAccount{Email: "Jane.Doe@example.com", Password: "synthetic-pass-phrase", FullName: "Jane Doe"})
	if err != nil {
		t.Fatal(err)
	}
	if provisioned.Email != "jane.doe@example.com" || provisioned.Status != accounts.StatusActive ||
		provisioned.PlatformOperator || provisioned.FullName == nil || *provisioned.FullName != "Jane Doe" {
		t.Fatalf("provisioned = %+v", provisioned)
	}
	if _, ok := reqctx.ParseTenantID(string(provisioned.ID)); !ok {
		t.Fatalf("provisioned id %q is not a UUID", provisioned.ID)
	}

	found, err := book.Get(ctx, []reqctx.AccountID{provisioned.ID, accountOne, accountOne, accountTwo, "not-a-uuid"})
	if err != nil || len(found) != 2 || found[provisioned.ID].Email != "jane.doe@example.com" || found[accountOne].Email != "user@example.com" {
		t.Fatalf("Get = %+v, %v", found, err)
	}
	byEmail, err := book.ByEmail(ctx, "  JANE.DOE@example.com ")
	if err != nil || len(byEmail) != 1 || byEmail[0].ID != provisioned.ID {
		t.Fatalf("ByEmail = %+v, %v", byEmail, err)
	}

	_, err = book.Provision(ctx, accounts.NewAccount{Email: "jane.doe@example.com", Password: "synthetic-pass-phrase", FullName: "Jane Doe"})
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Code != accounts.CodeIdentityAlreadyExists {
		t.Fatalf("second Provision error = %v; want 409 IDENTITY_ALREADY_EXISTS", err)
	}
}

func TestAccountBookEmptyLookups(t *testing.T) {
	ctx := context.Background()
	book := coretest.NewAccountBook(nil)
	found, err := book.Get(ctx, nil)
	if err != nil || found == nil || len(found) != 0 {
		t.Fatalf("Get(nil) = %#v, %v; want empty non-nil", found, err)
	}
	list, err := book.ByEmail(ctx, "nobody@example.com")
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("ByEmail(unknown) = %#v, %v; want empty non-nil", list, err)
	}
}

func TestAccountBookOperatorsFollowTheList(t *testing.T) {
	ctx := context.Background()
	book := coretest.NewAccountBook([]string{"ops@example.com"})
	if !book.GrantsOperator("OPS@example.com") || book.GrantsOperator("user@example.com") {
		t.Fatal("GrantsOperator does not follow the operator list")
	}
	operator, err := book.Provision(ctx, accounts.NewAccount{Email: "ops@example.com", Password: "synthetic-pass-phrase", FullName: "Ops Person"})
	if err != nil || !operator.PlatformOperator {
		t.Fatalf("listed email: %+v, %v; want a platform operator", operator, err)
	}
	plain, err := book.Provision(ctx, accounts.NewAccount{Email: "user@example.com", Password: "synthetic-pass-phrase", FullName: "Jane Doe"})
	if err != nil || plain.PlatformOperator {
		t.Fatalf("unlisted email: %+v, %v; want no operator flag", plain, err)
	}
}

func TestAccountBookFailProvision(t *testing.T) {
	ctx := context.Background()
	book := coretest.NewAccountBook(nil)
	unavailable := api.NewError(http.StatusServiceUnavailable, accounts.CodeIdentityProviderUnavailable, "Sign-in is unavailable right now.")
	book.FailProvision(unavailable)

	_, err := book.Provision(ctx, accounts.NewAccount{Email: "user@example.com", Password: "synthetic-pass-phrase", FullName: "Jane Doe"})
	if !errors.Is(err, unavailable) {
		t.Fatalf("Provision error = %v; want the configured failure", err)
	}
	list, err := book.ByEmail(ctx, "user@example.com")
	if err != nil || len(list) != 0 {
		t.Fatalf("a failed Provision left an account: %+v, %v", list, err)
	}

	operational := errors.New("database is down")
	book.FailProvision(operational)
	_, err = book.Provision(ctx, accounts.NewAccount{Email: "user@example.com", Password: "synthetic-pass-phrase", FullName: "Jane Doe"})
	if !errors.Is(err, operational) {
		t.Fatalf("Provision error = %v; want the operational failure", err)
	}
}
