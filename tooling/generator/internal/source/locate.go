package source

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// markerFile is the file whose first line declares the generator module.
var markerFile = filepath.Join("tooling", "generator", "go.mod")

// Locate returns the source root by the precedence --root, INITIALIZER_ROOT,
// the walk up from the working directory, the walk up from the binary's
// folder. flagRoot is "" when --root is absent. A flag or env value that is not
// valid is an *Error, never a silent fall-through to the next rule.
func Locate(flagRoot string, host Host) (Root, error) {
	if host.Getwd == nil {
		return Root{}, newError(CodeUnreadable, "the working directory is not available", ExitInternal)
	}
	cwd, err := host.Getwd()
	if err != nil {
		return Root{}, newError(CodeUnreadable, "cannot read the working directory: "+err.Error(), ExitInternal)
	}
	mainModule := host.MainModule
	if mainModule == "" {
		mainModule = MainModule
	}

	if flagRoot != "" {
		dir := absolute(flagRoot, cwd)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return Root{}, newError(CodeRootInvalid, fmt.Sprintf("--root %s is not a directory", flagRoot), ExitUsage)
		}
		return newRoot(dir, FromFlag, mainModule), nil
	}

	if host.Getenv != nil {
		if value := host.Getenv(EnvRoot); value != "" {
			dir := absolute(value, cwd)
			if !hasMarker(dir, mainModule) {
				message := fmt.Sprintf("%s=%s is not an initializer checkout (no tooling/generator/go.mod declaring %s)",
					EnvRoot, value, mainModule)
				return Root{}, newError(CodeRootInvalid, message, ExitUsage)
			}
			return newRoot(dir, FromEnv, mainModule), nil
		}
	}

	if dir, found := walkUp(absolute(cwd, cwd), mainModule); found {
		return newRoot(dir, FromWorkingDir, mainModule), nil
	}

	exeDir := "(unknown)"
	if host.Executable != nil {
		if exe, err := host.Executable(); err == nil {
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			exeDir = filepath.Dir(exe)
			if dir, found := walkUp(exeDir, mainModule); found {
				return newRoot(dir, FromExecutable, mainModule), nil
			}
		}
	}

	message := fmt.Sprintf("no initializer checkout found above %s or above %s", slash(cwd), slash(exeDir))
	return Root{}, newError(CodeRootNotFound, message, ExitUsage)
}

// newRoot builds the Root of dir.
func newRoot(dir string, how How, mainModule string) Root {
	return Root{Dir: dir, How: how, FS: os.DirFS(dir), Marked: hasMarker(dir, mainModule)}
}

// absolute returns path made absolute against base and cleaned.
func absolute(path, base string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
}

// slash renders a path with forward slashes.
func slash(path string) string {
	return filepath.ToSlash(path)
}

// walkUp returns the nearest folder, start included, that holds the marker.
func walkUp(start, mainModule string) (string, bool) {
	dir := start
	for {
		if hasMarker(dir, mainModule) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// markerReadLimit bounds the bytes read from a candidate go.mod.
const markerReadLimit = 4096

// hasMarker reports whether dir holds tooling/generator/go.mod whose first line
// is `module <mainModule>`.
func hasMarker(dir, mainModule string) bool {
	file, err := os.Open(filepath.Join(dir, markerFile))
	if err != nil {
		return false
	}
	defer file.Close()
	line, err := bufio.NewReader(io.LimitReader(file, markerReadLimit)).ReadString('\n')
	if err != nil && err != io.EOF {
		return false
	}
	return strings.TrimRight(line, "\r\n") == "module "+mainModule
}
