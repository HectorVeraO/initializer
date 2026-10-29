package manifest

import (
	"sort"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// Flags are the selection values given on the command line.
type Flags struct {
	Name, GoModule string
	// DisplayName is the --display-name value.
	DisplayName     string
	Has             FlagPresence
	Stacks, Modules []string
	AllModules      bool
	Components      []ComponentFlag
	// DBName and DBUser are the --db-name and --db-user values.
	DBName, DBUser string
	// Services holds the --service values in command-line order.
	Services []KeyValue
	// PortBand is the --port-band value as typed.
	PortBand string
}

// FlagPresence records which flags were given; an empty --modules= is given.
type FlagPresence struct {
	Name, GoModule, Stacks, Modules bool
	DBName, DBUser                  bool
	DisplayName                     bool
	PortBand                        bool
}

// KeyValue is one parsed KEY=NAME flag value.
type KeyValue struct {
	Key, Value string
}

// ComponentFlag is one parsed --component value.
type ComponentFlag struct {
	// Index is the 0-based position among the --component flags.
	Index int
	// Arg is the value as typed.
	Arg string
	// Entry is the head of the value.
	Entry Component
	// Set holds the keys given after the head; empty until the entry fields exist.
	Set map[string]bool
}

// FlagsInput builds the Input of the selection flags. Origin.Source is
// "flags"; a value that breaks its grammar is an issue at flags#<pointer>.
func FlagsInput(f Flags) (Input, []issue.Issue) {
	d := newDecoder(FlagsSource, CodeSelectionInvalid)
	in := Input{Origins: map[string]Origin{}}
	s := &in.Selection
	s.Schema = 1
	s.AllModules = f.AllModules
	origin := func(pointer, arg string) {
		in.Origins[pointer] = Origin{Source: FlagsSource, Pointer: pointer, Arg: arg}
	}
	if f.Has.Name {
		s.Project.Name, s.Has.Name = f.Name, true
		origin(patternName, "--name")
		checkValue(d, patternName, patternName, f.Name)
	}
	if f.Has.GoModule {
		s.Project.GoModule, s.Has.GoModule = f.GoModule, true
		origin(patternGoModule, "--go-module")
		checkValue(d, patternGoModule, patternGoModule, f.GoModule)
	}
	if f.Has.DisplayName {
		s.DisplayName, s.Schema = f.DisplayName, MaxSchema
		origin(patternDisplay, "--display-name")
		checkValue(d, patternDisplay, patternDisplay, f.DisplayName)
	}
	if f.Has.Stacks {
		s.Stacks, s.Has.Stacks = nonNil(f.Stacks), true
		origin("/stacks", "--stacks")
		for i, stack := range f.Stacks {
			pointer := "/stacks/" + strconv.Itoa(i)
			origin(pointer, "--stacks")
			checkValue(d, patternStacksItem, pointer, stack)
		}
	}
	if f.Has.Modules {
		s.Modules, s.Has.Modules = nonNil(f.Modules), true
		origin("/modules", "--modules")
		for i, module := range f.Modules {
			pointer := "/modules/" + strconv.Itoa(i)
			origin(pointer, "--modules")
			checkValue(d, patternModuleItem, pointer, module)
		}
	}
	if f.AllModules {
		origin("/modules", "--all-modules")
	}
	if len(f.Components) > 0 {
		s.Schema = 2
		origin("/components", "--component")
	}
	for i, flag := range f.Components {
		pointer := "/components/" + strconv.Itoa(i)
		s.Components = append(s.Components, flag.Entry)
		arg := "--component " + flag.Arg
		for _, suffix := range []string{"", "/stack", "/name"} {
			in.Origins[pointer+suffix] = Origin{Source: FlagsSource, Pointer: pointer, Arg: arg}
		}
		if flag.Entry.Name != "" {
			checkValue(d, patternCompName, pointer, flag.Entry.Name)
		}
	}
	flagsInputInfra(&in, d, f)
	if f.Has.PortBand {
		s.Schema = MaxSchema
		in.Origins[patternPortBand] = Origin{Source: FlagsSource, Pointer: patternPortBand, Arg: "--port-band"}
		if band, valid := parsePortBand(f.PortBand); valid {
			s.Ports = &Ports{Band: band}
		} else {
			d.addCode(CodeSelectionInvalid, patternPortBand, portBandMessage)
		}
	}
	return in, d.result()
}

// flagsInputInfra fills the database and services of the flags input.
func flagsInputInfra(in *Input, d *decoder, f Flags) {
	s := &in.Selection
	origin := func(pointer, arg string) {
		in.Origins[pointer] = Origin{Source: FlagsSource, Pointer: pointer, Arg: arg}
	}
	if f.Has.DBName {
		s.Database = &Database{Name: f.DBName}
		origin(patternDBName, "--db-name")
		checkValue(d, patternDBName, patternDBName, f.DBName)
	}
	if f.Has.DBUser {
		if s.Database == nil {
			s.Database = &Database{}
		}
		s.Database.User = f.DBUser
		origin(patternDBUser, "--db-user")
		checkValue(d, patternDBUser, patternDBUser, f.DBUser)
	}
	if len(f.Services) > 0 {
		s.Services = map[string]string{}
		origin("/services", "--service")
	}
	for _, service := range f.Services {
		pointer := "/services" + issue.Pointer(service.Key)
		s.Services[service.Key] = service.Value
		origin(pointer, "--service "+service.Key+"="+service.Value)
		checkValue(d, patternService, pointer, service.Value)
	}
	if s.Database != nil || len(s.Services) > 0 {
		s.Schema = MaxSchema
	}
}

// checkValue reports the grammar problem of the value at pattern as an issue
// at pointer.
func checkValue(d *decoder, pattern, pointer, value string) {
	if code, message, bad := valueProblem(pattern, value); bad {
		d.addCode(code, pointer, message)
	}
}

// CheckMerged reports the catalog-free rules that need the merged input: the
// stack counts. They apply only to a stacks list somebody gave; a defaulted
// list is checked by the plan.
func CheckMerged(in Input) []issue.Issue {
	if !in.Selection.Has.Stacks {
		return nil
	}
	backends, frontends := 0, 0
	for _, stack := range in.Selection.Stacks {
		switch {
		case IsBackendStack(stack):
			backends++
		case IsFrontendStack(stack):
			frontends++
		}
	}
	var issues []issue.Issue
	origin := in.Origins["/stacks"]
	add := func(message string) {
		issues = append(issues, issue.Issue{
			Code:    CodeSelectionInvalid,
			Path:    issue.Path(origin.Source, origin.Pointer),
			Line:    origin.Line,
			Message: message,
		})
	}
	if backends != 1 {
		add("must contain exactly one backend stack")
	}
	if frontends > 1 {
		add("must contain at most one frontend stack")
	}
	issue.Sort(issues)
	return issues
}

// RecordedSelection builds the document initializer.selection.json holds: the
// merged input as given, with the installed module list of the backend stack
// as the one exception (modules are always recorded). Name, Go module, stacks
// and components are recorded only when given; stacks are sorted. The schema
// is 1 when every recorded key exists in schema 1 and the schema 1 required
// keys are all present, else 2.
func RecordedSelection(in Input, installedModules []string) Selection {
	given := in.Selection
	recorded := Selection{
		Modules: append([]string{}, installedModules...),
		Has:     Presence{Modules: true},
	}
	if given.Has.Name {
		recorded.Project.Name, recorded.Has.Name = given.Project.Name, true
	}
	if given.Has.GoModule {
		recorded.Project.GoModule, recorded.Has.GoModule = given.Project.GoModule, true
	}
	recorded.DisplayName = given.DisplayName
	if given.Has.Stacks {
		recorded.Stacks = append([]string{}, given.Stacks...)
		sort.Strings(recorded.Stacks)
		recorded.Has.Stacks = true
	}
	recorded.Components = append([]Component(nil), given.Components...)
	if given.Database != nil {
		copied := *given.Database
		recorded.Database = &copied
	}
	recorded.Services = copyServices(given.Services)
	if given.Ports != nil {
		copied := *given.Ports
		recorded.Ports = &copied
	}
	hasInfra := recorded.Database != nil || len(recorded.Services) > 0 || recorded.Ports != nil
	isFirstSchema := recorded.Has.Name && recorded.Has.Stacks && len(recorded.Components) == 0 && !hasInfra && recorded.DisplayName == ""
	recorded.Schema = MaxSchema
	if isFirstSchema {
		recorded.Schema = 1
	}
	return recorded
}

// OriginPath renders where the value at pointer came from as an issue path
// and line. A pointer with no origin of its own takes the nearest parent's.
func (in Input) OriginPath(pointer string) (path string, line int, found bool) {
	for current := pointer; ; {
		if origin, has := in.Origins[current]; has && origin.Source != "" {
			at := origin.Pointer
			if at == "" {
				at = current
			}
			return issue.Path(origin.Source, at), origin.Line, true
		}
		cut := strings.LastIndexByte(current, '/')
		if cut < 0 {
			return "", 0, false
		}
		current = current[:cut]
	}
}
