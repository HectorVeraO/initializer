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

func parse(name string) (*template.Template, error) {
	text, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", name, err)
	}
	funcs := template.FuncMap{"join": strings.Join}
	parsed, err := template.New(name).Funcs(funcs).Option("missingkey=error").Parse(string(text))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	return parsed, nil
}

func execute(name string, data any) ([]byte, error) {
	parsed, err := parse(name)
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

// EnvPair is one TCK environment variable.
type EnvPair struct {
	Key, Value string
}

// ReadmeInput is the data of the project README. The caller sorts every list;
// the template does not.
type ReadmeInput struct {
	Name             string
	GeneratorVersion string
	Modules          []ReadmeModule
	TCKTags          []string
	TCKEnv           []EnvPair
}

// Readme renders the project README.md.
func Readme(in ReadmeInput) ([]byte, error) {
	var fields []string
	fields = append(fields, in.Name, in.GeneratorVersion)
	for _, module := range in.Modules {
		fields = append(fields, module.Stack, module.Module, module.Version)
	}
	fields = append(fields, in.TCKTags...)
	for _, pair := range in.TCKEnv {
		fields = append(fields, pair.Key, pair.Value)
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
	return execute("README.md.tmpl", in)
}
