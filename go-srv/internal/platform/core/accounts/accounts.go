// Package accounts is the core seam for human accounts: lookups and
// provisioning that a module other than the account owner may use. The
// account owner (identity) provides both ports.
package accounts

import (
	"context"
	"errors"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// Status is an account's status; the set is closed.
type Status string

const (
	StatusActive   Status = "active"
	StatusLocked   Status = "locked"
	StatusDisabled Status = "disabled"
)

// Ref is what a module other than the account owner may know about an
// account.
type Ref struct {
	ID reqctx.AccountID
	// Email is lower-cased.
	Email            string
	FullName         *string
	Status           Status
	PlatformOperator bool
}

// Directory reads accounts.
type Directory interface {
	// Get returns the accounts of ids. An id with no account, or that is not
	// a UUID, is absent from the map; that is not an error. A nil or empty
	// ids returns an empty, non-nil map without I/O. Duplicates are allowed.
	Get(ctx context.Context, ids []reqctx.AccountID) (map[reqctx.AccountID]Ref, error)
	// ByEmail returns every account whose email equals the lower-cased,
	// trimmed email, oldest first (created_at, then id). Email is not unique
	// in the account store, so the result is a list; it is empty, never nil,
	// when none matches.
	ByEmail(ctx context.Context, email string) ([]Ref, error)
}

// NewAccount is a person who accepts an invitation without an account.
type NewAccount struct {
	// Email is lower-cased by the caller (the invitation's email).
	Email string
	// Password reaches only the identity provider; it is never stored,
	// logged or echoed.
	Password string
	// FullName is trimmed and non-empty.
	FullName string
}

// Provisioner creates accounts on behalf of another module.
type Provisioner interface {
	// Provision creates the identity-provider identity and the account, and
	// commits the account before it returns. The account is a platform
	// operator when GrantsOperator(a.Email) is true. Rejections:
	//   409 IDENTITY_ALREADY_EXISTS when an identity or account exists for
	//       the email;
	//   422 VALIDATION_FAILED with issue PASSWORD_REJECTED on "password"
	//       when the provider's password policy refuses it;
	//   503 IDENTITY_PROVIDER_UNAVAILABLE when the provider cannot answer.
	// Any other error is operational.
	Provision(ctx context.Context, a NewAccount) (Ref, error)
	// GrantsOperator reports whether email is on the account owner's
	// operator list, so whether Provision would make it a platform
	// operator. email is compared lower-cased.
	GrantsOperator(email string) bool
}

// Codes of the Provisioner contract. The registry rows stay in
// api.Registry (raisedBy identity).
const (
	CodeIdentityAlreadyExists       api.Code = "IDENTITY_ALREADY_EXISTS"
	CodePasswordRejected            api.Code = "PASSWORD_REJECTED"
	CodeIdentityProviderUnavailable api.Code = "IDENTITY_PROVIDER_UNAVAILABLE"
)

var (
	// NoDirectory finds nobody: Get is an empty map, ByEmail an empty list.
	NoDirectory Directory = noDirectory{}
	// NoProvisioner refuses: Provision returns ErrNoProvisioner
	// (operational, answered 500); GrantsOperator is true, so a caller that
	// requires an operator list match fails closed for every address.
	NoProvisioner Provisioner = noProvisioner{}
)

// ErrNoProvisioner is the operational error of NoProvisioner.Provision.
var ErrNoProvisioner = errors.New("accounts: no module provides account provisioning")

type noDirectory struct{}

func (noDirectory) Get(context.Context, []reqctx.AccountID) (map[reqctx.AccountID]Ref, error) {
	return map[reqctx.AccountID]Ref{}, nil
}

func (noDirectory) ByEmail(context.Context, string) ([]Ref, error) {
	return []Ref{}, nil
}

type noProvisioner struct{}

func (noProvisioner) Provision(context.Context, NewAccount) (Ref, error) {
	return Ref{}, ErrNoProvisioner
}

func (noProvisioner) GrantsOperator(string) bool { return true }
