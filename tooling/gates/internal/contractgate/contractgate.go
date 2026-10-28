// Package contractgate checks specs/<spec>/ structure and breaking changes (RA.4, L16, L23).
package contractgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/repo"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/report"
)

// Rule codes reported by Check.
const (
	RuleMissing           = "CONTRACT_MISSING"
	RuleManifestMissing   = "CONTRACT_MANIFEST_MISSING"
	RuleTCKMissing        = "CONTRACT_TCK_MISSING"
	RuleExternalRef       = "CONTRACT_EXTERNAL_REF"
	RuleStabilityExcluded = "CONTRACT_STABILITY_EXCLUDED"
	RuleVersionDrift      = "CONTRACT_VERSION_DRIFT"
	RuleTCKTagInvalid     = "CONTRACT_TCK_TAG_INVALID"
	RuleBreakingUnmarked  = "CONTRACT_BREAKING_UNMARKED"
	RuleBreakingNotMajor  = "CONTRACT_BREAKING_NOT_MAJOR"
)

const (
	specsDir         = "specs"
	contractFile     = "openapi.yaml"
	manifestFile     = "spec.manifest.json"
	tckDir           = "tck"
	tckSpecSuffix    = ".spec.ts"
	defaultOasdiff   = "oasdiff"
	refPlain         = "RA.4"
	refSelfContained = "L16"
	refMajor         = "L23"
	maxWalkDepth     = 512
	maxOutputLines   = 10
	oasdiffBreaking  = 1
)

var (
	ignoredDirs = map[string]bool{
		"tck-support": true, "scripts": true, "node_modules": true,
		"test-results": true, "playwright-report": true,
	}
	allowedTags    = map[string]bool{"@core": true, "@otel": true, "@identity": true, "@parameters": true, "@feature-flags": true}
	excludedLevels = map[string]bool{"draft": true, "alpha": true}
	majorPattern   = regexp.MustCompile(`^(\d+)\.\d+\.\d+`)
	breakingKeys   = []string{"BREAKING CHANGE", "BREAKING-CHANGE"}
)

// Options for one run.
type Options struct {
	Root       string
	BaseRev    string // diff base is merge-base(BaseRev, HEAD)
	OasdiffBin string // default "oasdiff" from PATH
}

type manifest struct {
	Spec    string   `json:"spec"`
	Version string   `json:"version"`
	TCKTags []string `json:"tckTags"`
}

// specInfo is what the structural pass learns about one spec.
type specInfo struct {
	name     string
	manifest *manifest
}

// Check validates every specs/<spec>/ structure and, for each openapi.yaml
// changed since the merge base of opts.BaseRev and HEAD, breaking changes.
// An error means the gate could not run; violations are findings.
func Check(ctx context.Context, opts Options) ([]report.Violation, error) {
	if opts.Root == "" {
		return nil, errors.New("root is required")
	}
	if opts.BaseRev == "" {
		return nil, errors.New("base revision is required")
	}
	specs, violations, err := checkStructure(opts.Root)
	if err != nil {
		return nil, err
	}
	breaking, err := checkBreaking(ctx, opts, specs)
	if err != nil {
		return nil, err
	}
	violations = append(violations, breaking...)
	sort.SliceStable(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
	return violations, nil
}

func violation(rule, ref, file string, line int, format string, args ...any) report.Violation {
	return report.Violation{
		Rule: rule, Ref: ref, Severity: report.SeverityError,
		Path: filepath.ToSlash(file), Line: line, Message: fmt.Sprintf(format, args...),
	}
}

func checkStructure(root string) ([]specInfo, []report.Violation, error) {
	entries, err := os.ReadDir(filepath.Join(root, specsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", specsDir, err)
	}
	var violations []report.Violation
	var specs []specInfo
	for _, entry := range entries {
		if !entry.IsDir() || ignoredDirs[entry.Name()] {
			continue
		}
		name := entry.Name()
		dir := path.Join(specsDir, name)
		contractPath := path.Join(dir, contractFile)
		contract, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contractPath)))
		if errors.Is(err, fs.ErrNotExist) {
			violations = append(violations, violation(RuleMissing, refPlain, dir, 0, "%s has no %s", dir, contractFile))
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", contractPath, err)
		}
		info, found, err := checkSpec(root, name, contractPath, contract)
		if err != nil {
			return nil, nil, err
		}
		violations = append(violations, found...)
		specs = append(specs, info)
	}
	violations = append(violations, checkTagOwnership(specs)...)
	return specs, violations, nil
}

func checkSpec(root, name, contractPath string, contract []byte) (specInfo, []report.Violation, error) {
	dir := path.Join(specsDir, name)
	info := specInfo{name: name}
	var violations []report.Violation

	var doc yaml.Node
	err := yaml.Unmarshal(contract, &doc)
	if err != nil {
		return info, nil, fmt.Errorf("parse %s: %w", contractPath, err)
	}
	violations = append(violations, scanNode(&doc, contractPath, 0)...)

	manifestPath := path.Join(dir, manifestFile)
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifestPath)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		violations = append(violations, violation(RuleManifestMissing, refPlain, manifestPath, 0, "%s has no %s", dir, manifestFile))
	case err != nil:
		return info, nil, fmt.Errorf("read %s: %w", manifestPath, err)
	default:
		parsed, found := checkManifest(name, manifestPath, raw, &doc, contractPath)
		info.manifest = parsed
		violations = append(violations, found...)
	}

	hasTCK, err := hasTCKSpec(filepath.Join(root, filepath.FromSlash(dir), tckDir))
	if err != nil {
		return info, nil, err
	}
	if !hasTCK {
		violations = append(violations, violation(RuleTCKMissing, refPlain, path.Join(dir, tckDir), 0,
			"%s holds no *%s file", path.Join(dir, tckDir), tckSpecSuffix))
	}
	return info, violations, nil
}

func checkManifest(name, manifestPath string, raw []byte, doc *yaml.Node, contractPath string) (*manifest, []report.Violation) {
	var parsed manifest
	err := json.Unmarshal(raw, &parsed)
	if err != nil {
		return nil, []report.Violation{violation(RuleManifestMissing, refPlain, manifestPath, 0, "%s is not valid: %v", manifestFile, err)}
	}
	var violations []report.Violation
	if parsed.Spec != name {
		violations = append(violations, violation(RuleManifestMissing, refPlain, manifestPath, 0,
			"manifest spec %q differs from folder %q", parsed.Spec, name))
	}
	contractVersion, line := infoVersion(doc)
	if contractVersion != parsed.Version {
		violations = append(violations, violation(RuleVersionDrift, refPlain, contractPath, line,
			"info.version %q differs from %s version %q", contractVersion, manifestFile, parsed.Version))
	}
	if len(parsed.TCKTags) == 0 {
		violations = append(violations, violation(RuleTCKTagInvalid, refPlain, manifestPath, 0, "tckTags is empty"))
	}
	for _, tag := range parsed.TCKTags {
		if !allowedTags[tag] {
			violations = append(violations, violation(RuleTCKTagInvalid, refPlain, manifestPath, 0, "tag %q is not an allowed TCK tag", tag))
		}
	}
	return &parsed, violations
}

// checkTagOwnership reports a tag claimed by a second spec; specs are in
// directory order, so the first claimer keeps the tag.
func checkTagOwnership(specs []specInfo) []report.Violation {
	owner := map[string]string{}
	var violations []report.Violation
	for _, spec := range specs {
		if spec.manifest == nil {
			continue
		}
		seen := map[string]bool{}
		for _, tag := range spec.manifest.TCKTags {
			if seen[tag] {
				continue
			}
			seen[tag] = true
			first, claimed := owner[tag]
			if !claimed {
				owner[tag] = spec.name
				continue
			}
			violations = append(violations, violation(RuleTCKTagInvalid, refPlain,
				path.Join(specsDir, spec.name, manifestFile), 0, "tag %q is already claimed by spec %q", tag, first))
		}
	}
	return violations
}

func hasTCKSpec(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), tckSpecSuffix) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("scan %s: %w", filepath.ToSlash(dir), err)
	}
	return found, nil
}

// scanNode reports external $ref values and excluded stability levels.
// Aliases are not followed, so the walk terminates; depth is bounded anyway.
func scanNode(node *yaml.Node, file string, depth int) []report.Violation {
	if node == nil || depth > maxWalkDepth {
		return nil
	}
	var violations []report.Violation
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			isScalar := value.Kind == yaml.ScalarNode
			switch {
			case key.Value == "$ref" && isScalar && !strings.HasPrefix(value.Value, "#"):
				violations = append(violations, violation(RuleExternalRef, refSelfContained, file, value.Line,
					"$ref %q is not local; specs are self-contained", value.Value))
			case key.Value == "x-stability-level" && isScalar && excludedLevels[value.Value]:
				violations = append(violations, violation(RuleStabilityExcluded, refSelfContained, file, value.Line,
					"x-stability-level %q is skipped by oasdiff", value.Value))
			}
		}
	}
	for _, child := range node.Content {
		violations = append(violations, scanNode(child, file, depth+1)...)
	}
	return violations
}

// infoVersion returns info.version of an OpenAPI document and its line.
func infoVersion(doc *yaml.Node) (string, int) {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	infoNode := mapValue(root, "info")
	version := mapValue(infoNode, "version")
	if version == nil {
		return "", 0
	}
	return version.Value, version.Line
}

func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func checkBreaking(ctx context.Context, opts Options, specs []specInfo) ([]report.Violation, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	mergeBase, err := repo.MergeBase(opts.Root, opts.BaseRev, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("find merge base with %s: %w", opts.BaseRev, err)
	}
	changes, err := repo.DiffNameStatus(opts.Root, mergeBase, "HEAD", specsDir)
	if err != nil {
		return nil, err
	}
	modified := map[string]bool{}
	for _, change := range changes {
		if change.Kind == repo.ChangeModified {
			modified[change.Path] = true
		}
	}
	var violations []report.Violation
	for _, spec := range specs {
		contractPath := path.Join(specsDir, spec.name, contractFile)
		if !modified[contractPath] {
			continue
		}
		found, err := checkSpecBreaking(ctx, opts, mergeBase, spec.name)
		if err != nil {
			return nil, err
		}
		violations = append(violations, found...)
	}
	return violations, nil
}

func checkSpecBreaking(ctx context.Context, opts Options, mergeBase, name string) ([]report.Violation, error) {
	dir := path.Join(specsDir, name)
	contractPath := path.Join(dir, contractFile)
	baseContract, found, err := repo.ShowFile(opts.Root, mergeBase, contractPath)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	output, breaking, err := runOasdiff(ctx, opts, baseContract, contractPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", contractPath, err)
	}
	if !breaking {
		return nil, nil
	}
	summary := summarize(output)

	var violations []report.Violation
	markers, err := repo.TrailerValues(opts.Root, mergeBase, "HEAD", dir+"/", breakingKeys)
	if err != nil {
		return nil, err
	}
	if len(markers) == 0 {
		violations = append(violations, violation(RuleBreakingUnmarked, refPlain, contractPath, 0,
			"breaking change without a BREAKING CHANGE: trailer on a commit touching %s/: %s", dir, summary))
	}
	notMajor, reason, err := majorNotBumped(opts.Root, mergeBase, name, baseContract)
	if err != nil {
		return nil, err
	}
	if notMajor {
		violations = append(violations, violation(RuleBreakingNotMajor, refMajor, path.Join(dir, manifestFile), 0,
			"breaking change needs a MAJOR bump: %s: %s", reason, summary))
	}
	return violations, nil
}

// runOasdiff compares base with the contract at contractPath. The boolean is
// true when oasdiff reports ERR level changes (exit 1); any other non-zero
// exit is an error.
func runOasdiff(ctx context.Context, opts Options, base []byte, contractPath string) (string, bool, error) {
	bin := opts.OasdiffBin
	if bin == "" {
		bin = defaultOasdiff
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return "", false, fmt.Errorf("oasdiff not found (%s): %w", bin, err)
	}
	tmp, err := os.MkdirTemp("", "gates-contracts-")
	if err != nil {
		return "", false, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	basePath := filepath.Join(tmp, "base.yaml")
	err = os.WriteFile(basePath, base, 0o600)
	if err != nil {
		return "", false, fmt.Errorf("write base spec: %w", err)
	}
	headPath := filepath.Join(opts.Root, filepath.FromSlash(contractPath))
	cmd := exec.CommandContext(ctx, resolved, "breaking", "--fail-on", "ERR", "--flatten-allof", basePath, headPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return "", false, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == oasdiffBreaking {
		return stdout.String(), true, nil
	}
	return "", false, fmt.Errorf("oasdiff failed: %w: %s", err, strings.TrimSpace(stderr.String()))
}

// summarize folds oasdiff's text output into one line of at most maxOutputLines entries.
func summarize(output string) string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) > maxOutputLines {
		omitted := len(lines) - maxOutputLines
		lines = append(lines[:maxOutputLines], fmt.Sprintf("... %d more", omitted))
	}
	return strings.Join(lines, " | ")
}

// majorNotBumped reports whether the manifest major at HEAD is not above the
// one at the merge base (falling back to the base openapi info.version when
// the base had no manifest), with the reason.
func majorNotBumped(root, mergeBase, name string, baseContract []byte) (bool, string, error) {
	dir := path.Join(specsDir, name)
	headRaw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), manifestFile))
	if errors.Is(err, fs.ErrNotExist) {
		return true, manifestFile + " is missing", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("read %s: %w", path.Join(dir, manifestFile), err)
	}
	var head manifest
	err = json.Unmarshal(headRaw, &head)
	if err != nil {
		return true, manifestFile + " is not valid", nil
	}
	headMajor, ok := majorOf(head.Version)
	if !ok {
		return true, fmt.Sprintf("version %q is not MAJOR.MINOR.PATCH", head.Version), nil
	}
	baseVersion, err := baseVersionOf(root, mergeBase, dir, baseContract)
	if err != nil {
		return false, "", err
	}
	baseMajor, ok := majorOf(baseVersion)
	if !ok {
		return true, fmt.Sprintf("base version %q is not MAJOR.MINOR.PATCH", baseVersion), nil
	}
	if headMajor <= baseMajor {
		return true, fmt.Sprintf("version %s keeps MAJOR %d", head.Version, baseMajor), nil
	}
	return false, "", nil
}

func baseVersionOf(root, mergeBase, dir string, baseContract []byte) (string, error) {
	raw, found, err := repo.ShowFile(root, mergeBase, path.Join(dir, manifestFile))
	if err != nil {
		return "", err
	}
	if found {
		var base manifest
		err = json.Unmarshal(raw, &base)
		if err == nil {
			return base.Version, nil
		}
	}
	var doc yaml.Node
	err = yaml.Unmarshal(baseContract, &doc)
	if err != nil {
		return "", nil
	}
	version, _ := infoVersion(&doc)
	return version, nil
}

func majorOf(version string) (int, bool) {
	match := majorPattern.FindStringSubmatch(version)
	if match == nil {
		return 0, false
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return major, true
}
