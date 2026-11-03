package coretest

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	memberActive    = "active"
	memberSuspended = "suspended"
)

// Member is one membership row of the TenantBook fake.
type Member struct {
	ID      string
	Account reqctx.AccountID
	Tenant  reqctx.TenantID
	Role    reqctx.Role
	// Status is "active", "suspended" or "revoked".
	Status string
}

// TenantBook is an in-memory access.TenantDirectory and access.TenantAccess
// that follows the port contracts: Grant's check order and codes, Select's
// remembered-then-oldest order (oldest is the order of AddMember calls),
// Memberships' active-only list ordered by tenant slug then id. It is safe
// for concurrent use. Tenant and account ids must be canonical lowercase
// UUIDs, as the ports hand them out.
type TenantBook struct {
	mu         sync.Mutex
	tenants    map[reqctx.TenantID]access.TenantRef
	members    []Member
	remembered map[reqctx.AccountID]reqctx.TenantID
}

var (
	_ access.TenantDirectory = (*TenantBook)(nil)
	_ access.TenantAccess    = (*TenantBook)(nil)
)

// NewTenantBook returns a book holding tenants and no memberships.
func NewTenantBook(tenants ...access.TenantRef) *TenantBook {
	book := &TenantBook{
		tenants:    make(map[reqctx.TenantID]access.TenantRef, len(tenants)),
		remembered: map[reqctx.AccountID]reqctx.TenantID{},
	}
	for _, tenant := range tenants {
		book.tenants[tenant.ID] = tenant
	}
	return book
}

// AddMember adds m; a tenant id not in the book is a programmer error and
// panics.
func (b *TenantBook) AddMember(m Member) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, known := b.tenants[m.Tenant]
	if !known {
		panic(fmt.Sprintf("coretest: AddMember: tenant %q is not in the book", m.Tenant))
	}
	b.members = append(b.members, m)
}

// SetTenantStatus changes the status of a tenant in the book; a tenant id not
// in the book is a programmer error and panics.
func (b *TenantBook) SetTenantStatus(id reqctx.TenantID, status access.TenantStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	tenant, known := b.tenants[id]
	if !known {
		panic(fmt.Sprintf("coretest: SetTenantStatus: tenant %q is not in the book", id))
	}
	tenant.Status = status
	b.tenants[id] = tenant
}

// TenantExists reports whether the tenant is in the book, whatever its status.
func (b *TenantBook) TenantExists(ctx context.Context, id reqctx.TenantID) (bool, error) {
	_, found, err := b.Tenant(ctx, id)
	return found, err
}

// Tenant returns the tenant of id, or false when the book has none.
func (b *TenantBook) Tenant(_ context.Context, id reqctx.TenantID) (access.TenantRef, bool, error) {
	canonical, isUUID := reqctx.ParseTenantID(string(id))
	if !isUUID {
		return access.TenantRef{}, false, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	tenant, found := b.tenants[canonical]
	return tenant, found, nil
}

// Grant follows access.TenantAccess.Grant.
func (b *TenantBook) Grant(_ context.Context, account reqctx.AccountID, tenant reqctx.TenantID) (access.TenantGrant, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.grantLocked(account, tenant)
}

func (b *TenantBook) grantLocked(account reqctx.AccountID, tenant reqctx.TenantID) (access.TenantGrant, error) {
	denied := api.NewError(http.StatusForbidden, api.CodeTenantAccessDenied, "The account has no access to this tenant.")
	accountID, accountOK := reqctx.ParseTenantID(string(account))
	tenantID, tenantOK := reqctx.ParseTenantID(string(tenant))
	if !accountOK || !tenantOK {
		return access.TenantGrant{}, denied
	}
	ref, known := b.tenants[tenantID]
	if !known {
		return access.TenantGrant{}, denied
	}
	var membership *Member
	for i := range b.members {
		isMatch := string(b.members[i].Account) == string(accountID) && b.members[i].Tenant == tenantID
		if isMatch {
			membership = &b.members[i]
			break
		}
	}
	if membership == nil {
		return access.TenantGrant{}, denied
	}
	if ref.Status == access.TenantSuspended {
		return access.TenantGrant{}, api.NewError(http.StatusForbidden, api.CodeTenantSuspended, "The tenant is suspended.")
	}
	if membership.Status != memberActive {
		message := "The membership was revoked."
		if membership.Status == memberSuspended {
			message = "The membership is suspended."
		}
		return access.TenantGrant{}, api.NewError(http.StatusForbidden, api.CodeMembershipRevoked, message)
	}
	return access.TenantGrant{Tenant: ref, Role: membership.Role}, nil
}

// Select follows access.TenantAccess.Select.
func (b *TenantBook) Select(_ context.Context, account reqctx.AccountID) (*access.TenantGrant, error) {
	accountID, isUUID := reqctx.ParseTenantID(string(account))
	if !isUUID {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	remembered, hasRemembered := b.remembered[reqctx.AccountID(accountID)]
	if hasRemembered {
		grant, err := b.grantLocked(account, remembered)
		if err == nil {
			return &grant, nil
		}
	}
	for _, member := range b.members {
		if string(member.Account) != string(accountID) || member.Status != memberActive {
			continue
		}
		grant, err := b.grantLocked(account, member.Tenant)
		if err == nil {
			return &grant, nil
		}
	}
	return nil, nil
}

// Remember follows access.TenantAccess.Remember.
func (b *TenantBook) Remember(_ context.Context, account reqctx.AccountID, tenant reqctx.TenantID) error {
	accountID, accountOK := reqctx.ParseTenantID(string(account))
	tenantID, tenantOK := reqctx.ParseTenantID(string(tenant))
	if !accountOK || !tenantOK {
		return errors.New("coretest: remember: ids must be UUIDs")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remembered[reqctx.AccountID(accountID)] = tenantID
	return nil
}

// Memberships follows access.TenantAccess.Memberships.
func (b *TenantBook) Memberships(_ context.Context, account reqctx.AccountID) ([]access.MembershipRef, error) {
	list := []access.MembershipRef{}
	accountID, isUUID := reqctx.ParseTenantID(string(account))
	if !isUUID {
		return list, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, member := range b.members {
		if string(member.Account) != string(accountID) || member.Status != memberActive {
			continue
		}
		list = append(list, access.MembershipRef{
			ID:     member.ID,
			Tenant: b.tenants[member.Tenant],
			Role:   member.Role,
			Status: memberActive,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Tenant.Slug != list[j].Tenant.Slug {
			return list[i].Tenant.Slug < list[j].Tenant.Slug
		}
		return list[i].ID < list[j].ID
	})
	return list, nil
}

// AccountBook is an in-memory accounts.Directory and accounts.Provisioner.
// Provision adds the account (a new UUID, status active, operator per
// GrantsOperator) so Get and ByEmail see it; an email already in the book
// answers 409 IDENTITY_ALREADY_EXISTS. It is safe for concurrent use.
type AccountBook struct {
	mu           sync.Mutex
	operators    map[string]bool
	byID         map[reqctx.AccountID]accounts.Ref
	order        []reqctx.AccountID
	provisionErr error
}

var (
	_ accounts.Directory   = (*AccountBook)(nil)
	_ accounts.Provisioner = (*AccountBook)(nil)
)

// NewAccountBook returns a book holding refs; operatorEmails is the
// lower-cased list GrantsOperator reads. The order of refs is the order of
// creation that ByEmail reports.
func NewAccountBook(operatorEmails []string, refs ...accounts.Ref) *AccountBook {
	book := &AccountBook{
		operators: make(map[string]bool, len(operatorEmails)),
		byID:      make(map[reqctx.AccountID]accounts.Ref, len(refs)),
	}
	for _, email := range operatorEmails {
		book.operators[email] = true
	}
	for _, ref := range refs {
		book.byID[ref.ID] = ref
		book.order = append(book.order, ref.ID)
	}
	return book
}

// FailProvision makes every later Provision return err (an *api.Error for a
// rejection, any other error for an operational failure).
func (b *AccountBook) FailProvision(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.provisionErr = err
}

// Get follows accounts.Directory.Get.
func (b *AccountBook) Get(_ context.Context, ids []reqctx.AccountID) (map[reqctx.AccountID]accounts.Ref, error) {
	found := make(map[reqctx.AccountID]accounts.Ref, len(ids))
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range ids {
		canonical, isUUID := reqctx.ParseTenantID(string(id))
		if !isUUID {
			continue
		}
		ref, ok := b.byID[reqctx.AccountID(canonical)]
		if ok {
			found[ref.ID] = ref
		}
	}
	return found, nil
}

// ByEmail follows accounts.Directory.ByEmail; the book's order is oldest first.
func (b *AccountBook) ByEmail(_ context.Context, email string) ([]accounts.Ref, error) {
	wanted := strings.ToLower(strings.TrimSpace(email))
	list := []accounts.Ref{}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range b.order {
		ref := b.byID[id]
		if ref.Email == wanted {
			list = append(list, ref)
		}
	}
	return list, nil
}

// Provision follows accounts.Provisioner.Provision.
func (b *AccountBook) Provision(_ context.Context, a accounts.NewAccount) (accounts.Ref, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.provisionErr != nil {
		return accounts.Ref{}, b.provisionErr
	}
	email := strings.ToLower(strings.TrimSpace(a.Email))
	for _, ref := range b.byID {
		if ref.Email == email {
			return accounts.Ref{}, api.NewError(http.StatusConflict, accounts.CodeIdentityAlreadyExists,
				"An account with this email already exists. Sign in and accept the invitation.")
		}
	}
	name := a.FullName
	ref := accounts.Ref{
		ID:               reqctx.AccountID(newUUID()),
		Email:            email,
		FullName:         &name,
		Status:           accounts.StatusActive,
		PlatformOperator: b.operators[email],
	}
	b.byID[ref.ID] = ref
	b.order = append(b.order, ref.ID)
	return ref, nil
}

// GrantsOperator follows accounts.Provisioner.GrantsOperator.
func (b *AccountBook) GrantsOperator(email string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.operators[strings.ToLower(email)]
}

// newUUID returns a random version 4 UUID in canonical lowercase text.
func newUUID() string {
	var raw [16]byte
	_, err := rand.Read(raw[:])
	if err != nil {
		panic(fmt.Sprintf("coretest: read random bytes: %v", err))
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}
