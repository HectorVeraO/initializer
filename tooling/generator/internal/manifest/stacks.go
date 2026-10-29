package manifest

import "sort"

// Stack roles of the stack table.
const (
	RoleBackend  = "backend"
	RoleFrontend = "frontend"
)

// StackInfo is one row of the built-in stack table: the single source of a
// stack's source root, role, role suffix and in-root layout.
type StackInfo struct {
	// ID is the stack id, also the source root folder in this repo: "go-srv".
	ID string
	// Role is RoleBackend or RoleFrontend.
	Role string
	// Root is the source root in this repo, with a trailing slash: "go-srv/".
	Root string
	// Suffix is the H1 role suffix of the default component name: "srv".
	Suffix string
	// ModulesRoot is relative to Root, with a trailing slash: "internal/platform/".
	ModulesRoot string
	// CompositionRoot is relative to Root: "cmd/server/modules.go".
	CompositionRoot string
	// GoModule reports whether a component of this stack needs a Go module path.
	GoModule bool
}

var stackTable = []StackInfo{
	{ID: StackGoSrv, Role: RoleBackend, Root: "go-srv/", Suffix: "srv", ModulesRoot: "internal/platform/", CompositionRoot: "cmd/server/modules.go", GoModule: true},
	{ID: StackNgWeb, Role: RoleFrontend, Root: "ng-web/", Suffix: "web", ModulesRoot: "src/app/platform/", CompositionRoot: "src/app/platform/modules.ts", GoModule: false},
}

// Stacks returns a copy of the stack table sorted by ID.
func Stacks() []StackInfo {
	out := make([]StackInfo, len(stackTable))
	copy(out, stackTable)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// LookupStack returns the row of a stack id and false for an unknown id.
func LookupStack(id string) (StackInfo, bool) {
	for _, info := range stackTable {
		if info.ID == id {
			return info, true
		}
	}
	return StackInfo{}, false
}
