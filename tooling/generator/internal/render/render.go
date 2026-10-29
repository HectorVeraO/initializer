// Package render renders the project files the generator writes itself from
// embedded templates.
package render

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"regexp"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var (
	goModulePattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+(/[A-Za-z0-9._~-]+)*$`)
	folderPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
)

func parse(name string, extra ...template.FuncMap) (*template.Template, error) {
	text, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", name, err)
	}
	funcs := template.FuncMap{"join": strings.Join}
	for _, more := range extra {
		for key, fn := range more {
			funcs[key] = fn
		}
	}
	parsed, err := template.New(name).Funcs(funcs).Option("missingkey=error").Parse(string(text))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	return parsed, nil
}

func execute(name string, data any, extra ...template.FuncMap) ([]byte, error) {
	parsed, err := parse(name, extra...)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := parsed.Execute(&buffer, data); err != nil {
		return nil, fmt.Errorf("render %s: %w", name, err)
	}
	return buffer.Bytes(), nil
}

// ModulesGo renders go-srv/cmd/server/modules.go for the Go module path
// goModule. folders are the Go folder names of the installed modules other
// than core, in catalog order. The result is gofmt-formatted.
func ModulesGo(goModule string, folders []string) ([]byte, error) {
	if goModule == "" {
		return nil, errors.New("goModule must not be empty")
	}
	if !goModulePattern.MatchString(goModule) {
		return nil, fmt.Errorf("goModule %q is not a plain module path", goModule)
	}
	seen := map[string]bool{}
	for _, folder := range folders {
		if !folderPattern.MatchString(folder) || folder == "core" {
			return nil, fmt.Errorf("folder %q is not an installable module folder", folder)
		}
		if seen[folder] {
			return nil, fmt.Errorf("folder %q is listed twice", folder)
		}
		seen[folder] = true
	}
	raw, err := execute("modules.go.tmpl", struct {
		GoModule string
		Folders  []string
	}{goModule, folders})
	if err != nil {
		return nil, err
	}
	formatted, err := format.Source(raw)
	if err != nil {
		return nil, fmt.Errorf("format modules.go: %w", err)
	}
	return formatted, nil
}

// ReadmeModule is one row of the modules table.
type ReadmeModule struct {
	Stack, Module, Version string
}

// EnvPair is one TCK environment variable. A relative path value is relative
// to the project root, where the TCK runs.
type EnvPair struct {
	Key, Value string
}

// goSrvValue returns the value as go-srv reads it when started from go-srv/,
// the working directory .deploy/local/README.md documents: a project-root
// relative path gets the "../" prefix; any other value stays as is.
func (p EnvPair) goSrvValue() string {
	isPath := strings.Contains(p.Value, "/") && !strings.Contains(p.Value, "://")
	isRelative := !strings.HasPrefix(p.Value, "/")
	if isPath && isRelative {
		return "../" + p.Value
	}
	return p.Value
}

// goSrvEnv returns the pairs with their values as go-srv reads them.
func goSrvEnv(pairs []EnvPair) []EnvPair {
	out := make([]EnvPair, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, EnvPair{Key: pair.Key, Value: pair.goSrvValue()})
	}
	return out
}

// ReadmeRun is the data of the Run, host-dev and TCK sections of the project README.
type ReadmeRun struct {
	// PortLow and PortHigh are the first and last host port of the band (10100, 10199).
	PortLow, PortHigh int
	// Instances holds one entry per backend component in lock order; exactly one
	// until multiple backend components are supported.
	Instances []ReadmeInstance
}

// ReadmeInstance is one backend component and its bound frontend.
type ReadmeInstance struct {
	// Backend is the backend component name: its folder and its compose service.
	Backend string
	// Web is the bound frontend component name; "" when there is none.
	Web string
	// OriginURL is the public origin, "http://localhost:10120".
	OriginURL string
	// HasIdentity reports whether the backend installs identity (cookie sentence).
	HasIdentity bool
	// DevSrvTask, DevWebTask and TCKTask are the mise task names ("dev:srv",
	// "dev:web", "tck"); DevWebTask is "" when Web is "".
	DevSrvTask, DevWebTask, TCKTask string
}

// validate returns an error unless the run can be rendered.
func (run ReadmeRun) validate() error {
	if run.PortLow >= run.PortHigh {
		return fmt.Errorf("port range %d-%d must have PortLow below PortHigh", run.PortLow, run.PortHigh)
	}
	if len(run.Instances) == 0 {
		return errors.New("the README needs at least one backend instance")
	}
	for position, instance := range run.Instances {
		if instance.Backend == "" {
			return fmt.Errorf("instance %d has no backend name", position)
		}
		hasNoWeb := instance.Web == ""
		hasNoWebTask := instance.DevWebTask == ""
		if hasNoWeb != hasNoWebTask {
			return fmt.Errorf("instance %d: DevWebTask must be empty exactly when Web is empty", position)
		}
		if instance.OriginURL == "" || instance.DevSrvTask == "" || instance.TCKTask == "" {
			return fmt.Errorf("instance %d: OriginURL, DevSrvTask and TCKTask must not be empty", position)
		}
	}
	return nil
}

// ReadmeInput is the data of the project README. The caller sorts every list;
// the template does not.
type ReadmeInput struct {
	Name             string
	GeneratorVersion string
	Modules          []ReadmeModule
	TCKTags          []string
	TCKEnv           []EnvPair
	Run              ReadmeRun
}

// Readme renders the project README.md.
func Readme(in ReadmeInput) ([]byte, error) {
	if err := in.Run.validate(); err != nil {
		return nil, err
	}
	var fields []string
	fields = append(fields, in.Name, in.GeneratorVersion)
	for _, module := range in.Modules {
		fields = append(fields, module.Stack, module.Module, module.Version)
	}
	fields = append(fields, in.TCKTags...)
	for _, pair := range in.TCKEnv {
		fields = append(fields, pair.Key, pair.Value)
	}
	for _, instance := range in.Run.Instances {
		fields = append(fields, instance.Backend, instance.Web, instance.OriginURL,
			instance.DevSrvTask, instance.DevWebTask, instance.TCKTask)
	}
	for _, field := range fields {
		if strings.ContainsAny(field, "\r\n\x00") {
			return nil, errors.New("README fields must not contain line breaks or NUL")
		}
	}
	for _, pair := range in.TCKEnv {
		if strings.Contains(pair.Key, "`") || strings.Contains(pair.Value, "`") {
			return nil, errors.New("TCK env pairs must not contain a backtick")
		}
	}
	in.TCKEnv = goSrvEnv(in.TCKEnv)
	return execute("README.md.tmpl", in)
}

// moduleIDPattern is the module id grammar shared by both stacks.
var moduleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// WebSymbol returns the wiring symbol of an Angular module: the camelCase id
// plus "Module" (feature-flags -> featureFlagsModule).
func WebSymbol(id string) (string, error) {
	if !moduleIDPattern.MatchString(id) {
		return "", fmt.Errorf("module id %q is not a valid id", id)
	}
	parts := strings.Split(id, "-")
	var symbol strings.Builder
	symbol.WriteString(parts[0])
	for _, part := range parts[1:] {
		symbol.WriteString(strings.ToUpper(part[:1]))
		symbol.WriteString(part[1:])
	}
	symbol.WriteString("Module")
	return symbol.String(), nil
}

// ModulesTS renders ng-web/src/app/platform/modules.ts. ids are the ids of
// every installed Angular module, core first, in catalog order.
func ModulesTS(ids []string) ([]byte, error) {
	if len(ids) == 0 || ids[0] != "core" {
		return nil, errors.New("the first module must be core")
	}
	type entry struct{ ID, Symbol string }
	var entries []entry
	var symbols []string
	seen := map[string]bool{}
	symbolOwner := map[string]string{}
	for _, id := range ids {
		symbol, err := WebSymbol(id)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, fmt.Errorf("module %q is listed twice", id)
		}
		seen[id] = true
		if owner, taken := symbolOwner[symbol]; taken {
			return nil, fmt.Errorf("modules %q and %q map to the same symbol %q", owner, id, symbol)
		}
		symbolOwner[symbol] = id
		entries = append(entries, entry{id, symbol})
		symbols = append(symbols, symbol)
	}
	return execute("modules.ts.tmpl", struct {
		Modules []entry
		Symbols []string
	}{entries, symbols})
}
