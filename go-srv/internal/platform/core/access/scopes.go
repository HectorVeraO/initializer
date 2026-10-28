package access

import (
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

var scopeName = regexp.MustCompile(`^[a-z][a-z0-9-]*:[a-z][a-z0-9-]*$`)

// ScopeLevel says which API keys may hold a scope. The zero value is invalid.
type ScopeLevel uint8

const (
	// ScopeTenant: only a key bound to a tenant may hold it; a route whose
	// Scope has this level requires a tenant from a key.
	ScopeTenant ScopeLevel = iota + 1
	// ScopeAny: a key with or without a tenant may hold it.
	ScopeAny
)

// String is "tenant" or "any"; "" for an invalid level.
func (l ScopeLevel) String() string {
	switch l {
	case ScopeTenant:
		return "tenant"
	case ScopeAny:
		return "any"
	}
	return ""
}

// Scope is one registered API key scope.
type Scope struct {
	// Name matches ^[a-z][a-z0-9-]*:[a-z][a-z0-9-]*$, e.g. "tenant:read".
	Name        string
	Description string
	Level       ScopeLevel
}

// ScopeRegistry holds the scopes modules register during phase 1.
type ScopeRegistry struct {
	mu     sync.RWMutex
	frozen bool
	scopes map[string]Scope
}

// NewScopeRegistry returns an empty, unfrozen registry.
func NewScopeRegistry() *ScopeRegistry {
	return &ScopeRegistry{scopes: map[string]Scope{}}
}

// Register adds a scope during phase 1. A bad name, an invalid level, a
// duplicate, or a call after Freeze returns an error that stops startup.
func (r *ScopeRegistry) Register(name, description string, level ScopeLevel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("access: scope %q registered after Freeze", name)
	}
	if !scopeName.MatchString(name) {
		return fmt.Errorf("access: scope name %q does not match %s", name, scopeName)
	}
	if level.String() == "" {
		return fmt.Errorf("access: scope %q has an invalid level", name)
	}
	if _, exists := r.scopes[name]; exists {
		return fmt.Errorf("access: scope %q is already registered", name)
	}
	r.scopes[name] = Scope{Name: name, Description: description, Level: level}
	return nil
}

// Freeze ends phase 1; the core host calls it after every Provide.
func (r *ScopeRegistry) Freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

// Known reports whether name is registered.
func (r *ScopeRegistry) Known(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.scopes[name]
	return ok
}

// Level returns the level of a registered scope; false when name is not
// registered.
func (r *ScopeRegistry) Level(name string) (ScopeLevel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	scope, ok := r.scopes[name]
	return scope.Level, ok
}

// All returns every registered scope sorted by name.
func (r *ScopeRegistry) All() []Scope {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Scope, 0, len(r.scopes))
	for _, s := range r.scopes {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CheckMint adds one issue to p for every registered scope in scopes that a
// key bound to tenant may not hold: when tenant is nil, each ScopeTenant
// scope gets one FIELD_NOT_ALLOWED issue at "<field>[i]" with message
// "Requires a key bound to a tenant". Unregistered names are skipped (the
// caller already reports them). A key with a tenant may hold every level.
func (r *ScopeRegistry) CheckMint(p *api.Problems, field string, scopes []string, tenant *reqctx.TenantID) {
	if tenant != nil {
		return
	}
	for i, name := range scopes {
		level, known := r.Level(name)
		if known && level == ScopeTenant {
			p.Add(fmt.Sprintf("%s[%d]", field, i), api.CodeFieldNotAllowed, "Requires a key bound to a tenant")
		}
	}
}
