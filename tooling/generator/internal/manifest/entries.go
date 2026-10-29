package manifest

import (
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// ApplyEntryOverrides returns the input a single project is planned from: the
// values of the backend entry (modules, Go module, database members, service
// keys) replace the top-level ones, with their origins. The recorded document
// is built from the input as given, so it keeps both.
func ApplyEntryOverrides(in Input) Input {
	out := Input{Selection: in.Selection, Origins: map[string]Origin{}}
	for pointer, origin := range in.Origins {
		out.Origins[pointer] = origin
	}
	s := &out.Selection
	if s.Database != nil {
		copied := *s.Database
		s.Database = &copied
	}
	s.Services = copyServices(s.Services)
	for i, entry := range in.Selection.Components {
		if !IsBackendStack(entry.Stack) {
			continue
		}
		from := "/components/" + strconv.Itoa(i)
		move := func(fromField, toPattern string) {
			dropOrigins(&out, toPattern)
			for pointer, origin := range in.Origins {
				if pointer == from+fromField || strings.HasPrefix(pointer, from+fromField+"/") {
					out.Origins[toPattern+pointer[len(from+fromField):]] = origin
				}
			}
		}
		if entry.HasModules {
			s.Modules, s.Has.Modules, s.AllModules = append([]string{}, entry.Modules...), true, false
			move("/modules", "/modules")
		}
		info, _ := LookupStack(entry.Stack)
		if entry.GoModule != "" && info.GoModule {
			s.Project.GoModule, s.Has.GoModule = entry.GoModule, true
			move("/goModule", patternGoModule)
		}
		applyEntryDatabase(&out, in, entry, from)
		for _, key := range sortedServiceKeys(entry.Services) {
			if s.Services == nil {
				s.Services = map[string]string{}
			}
			s.Services[key] = entry.Services[key]
			pointer := "/services" + issue.Pointer(key)
			out.Origins[pointer] = in.Origins[from+pointer]
		}
	}
	return out
}

// applyEntryDatabase overrides the top-level database members the entry gives.
func applyEntryDatabase(out *Input, in Input, entry Component, from string) {
	if entry.Database == nil {
		return
	}
	s := &out.Selection
	if s.Database == nil {
		s.Database = &Database{}
	}
	if entry.Database.Name != "" {
		s.Database.Name = entry.Database.Name
		out.Origins[patternDBName] = in.Origins[from+"/database/name"]
	}
	if entry.Database.User != "" {
		s.Database.User = entry.Database.User
		out.Origins[patternDBUser] = in.Origins[from+"/database/user"]
	}
}

// recordEntryModules moves the installed module list into the only backend
// entry that gave its own list, and drops the top-level list: a recorded
// selection never holds two lists. It reports whether it did.
func recordEntryModules(recorded *Selection, installed []string) bool {
	at := -1
	for i, entry := range recorded.Components {
		if IsBackendStack(entry.Stack) {
			if at >= 0 {
				return false
			}
			at = i
		}
	}
	if at < 0 || !recorded.Components[at].HasModules {
		return false
	}
	recorded.Components[at].Modules = append([]string{}, installed...)
	recorded.Modules, recorded.Has.Modules = nil, false
	return true
}
