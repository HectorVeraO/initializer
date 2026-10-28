package access

import (
	"fmt"
	"regexp"
	"sort"
	"sync"
)

var scopeName = regexp.MustCompile(`^[a-z][a-z0-9-]*:[a-z][a-z0-9-]*$`)

// Scope is one registered API key scope.
type Scope struct {
	// Name matches ^[a-z][a-z0-9-]*:[a-z][a-z0-9-]*$, e.g. "tenant:read".
	Name        string
	Description string
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

// Register adds a scope during phase 1. A bad name, a duplicate, or a call
// after Freeze returns an error that stops startup.
func (r *ScopeRegistry) Register(name, description string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("access: scope %q registered after Freeze", name)
	}
	if !scopeName.MatchString(name) {
		return fmt.Errorf("access: scope name %q does not match %s", name, scopeName)
	}
	if _, exists := r.scopes[name]; exists {
		return fmt.Errorf("access: scope %q is already registered", name)
	}
	r.scopes[name] = Scope{Name: name, Description: description}
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
