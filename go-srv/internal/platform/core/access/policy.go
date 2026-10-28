// Package auth is the one authorization surface of core: the route Policy,
// the Guard that enforces it after the mux match, and the resolver ports the
// identity module implements.
package access

import (
	"errors"
	"fmt"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// Kind is the credential a route accepts.
type Kind uint8

const (
	// KindNone is public: credentials are never read.
	KindNone Kind = iota + 1
	// KindSession requires a valid session.
	KindSession
	// KindSessionAllowExpired accepts a session whose signature verifies even if
	// expired (refresh, logout).
	KindSessionAllowExpired
	// KindAPIKey requires an API key holding Scope.
	KindAPIKey
	// KindSessionOrAPIKey requires a valid session, or an API key holding Scope.
	KindSessionOrAPIKey
)

// Policy is the access rule of one route.
type Policy struct {
	Kind Kind
	// Scope is required for KindAPIKey and KindSessionOrAPIKey and must be
	// registered in the ScopeRegistry; empty for every other kind.
	Scope string
	// MinRole, when set, requires an active tenant and, for a session, a role at
	// least MinRole. API keys are checked by Scope instead. Not allowed with
	// KindNone, KindAPIKey or KindSessionAllowExpired.
	MinRole reqctx.Role
	// PlatformOperator requires a platform operator's session. Allowed only with
	// KindSession and an empty MinRole; it ignores tenant and role.
	PlatformOperator bool
}

// Public is the policy of a route that reads no credentials.
var Public = Policy{Kind: KindNone}

func (k Kind) needsScope() bool {
	return k == KindAPIKey || k == KindSessionOrAPIKey
}

// Validate reports a policy that breaks the rules of Policy (a programmer
// error). scopes may be nil only for policies that need no scope.
func (p Policy) Validate(scopes *ScopeRegistry) error {
	var problems []error
	if p.Kind < KindNone || p.Kind > KindSessionOrAPIKey {
		return fmt.Errorf("access: invalid kind %d", p.Kind)
	}
	if p.Kind.needsScope() {
		if p.Scope == "" {
			problems = append(problems, errors.New("access: scope is required for this kind"))
		} else if scopes == nil || !scopes.Known(p.Scope) {
			problems = append(problems, fmt.Errorf("access: scope %q is not registered", p.Scope))
		}
	} else if p.Scope != "" {
		problems = append(problems, errors.New("access: scope is not allowed for this kind"))
	}
	if p.MinRole != "" {
		if !p.MinRole.Valid() {
			problems = append(problems, fmt.Errorf("access: invalid min role %q", p.MinRole))
		}
		rejectsRole := p.Kind == KindNone || p.Kind == KindAPIKey || p.Kind == KindSessionAllowExpired
		if rejectsRole {
			problems = append(problems, errors.New("access: min role is not allowed for this kind"))
		}
	}
	if p.PlatformOperator {
		if p.Kind != KindSession {
			problems = append(problems, errors.New("access: platform operator requires KindSession"))
		}
		if p.MinRole != "" {
			problems = append(problems, errors.New("access: platform operator excludes min role"))
		}
	}
	return errors.Join(problems...)
}
