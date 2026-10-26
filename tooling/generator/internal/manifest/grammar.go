package manifest

import (
	"regexp"
	"strings"
)

// Stack kinds in the built-in stack table.
const (
	StackGoSrv = "go-srv"
	StackNgWeb = "ng-web"
)

const maxIDLength = 31

var (
	idPattern          = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	tablePrefixPattern = regexp.MustCompile(`^[a-z]{2,4}$`)
	envKeyPattern      = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	projectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	goSegmentPattern   = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
)

var tckTagTable = map[string]bool{
	"@core":          true,
	"@core-auth":     true,
	"@otel":          true,
	"@parameters":    true,
	"@feature-flags": true,
}

// IsBackendStack reports whether stack is a backend stack of the stack table.
func IsBackendStack(stack string) bool {
	return stack == StackGoSrv
}

// IsFrontendStack reports whether stack is a frontend stack of the stack table.
func IsFrontendStack(stack string) bool {
	return stack == StackNgWeb
}

func isKnownStack(stack string) bool {
	return IsBackendStack(stack) || IsFrontendStack(stack)
}

func validID(id string) bool {
	return len(id) <= maxIDLength && idPattern.MatchString(id)
}

// validRepoPath applies the path grammar of files, exclude, path and target;
// one trailing slash (a directory entry) is allowed.
func validRepoPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") {
		return false
	}
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c < 0x20 || c == 0x7f || c == '\\' || c == ':' {
			return false
		}
	}
	trimmed := strings.TrimSuffix(path, "/")
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// validGoModulePath applies the goModule grammar of a selection.
func validGoModulePath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	segments := strings.Split(path, "/")
	first := segments[0]
	if !strings.Contains(first, ".") || first != strings.ToLower(first) {
		return false
	}
	for _, segment := range segments {
		if segment == "." || segment == ".." {
			return false
		}
		if !goSegmentPattern.MatchString(segment) {
			return false
		}
	}
	return true
}
