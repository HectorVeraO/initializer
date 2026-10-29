package generator

import (
	"regexp"
	"runtime/debug"
)

// Build origins.
const (
	// OriginRelease is a binary stamped with Commit by -ldflags.
	OriginRelease = "release"
	// OriginModule is a binary built from a module version (go install pkg@version).
	OriginModule = "module"
	// OriginDevel is a binary built from a working tree, or one that says nothing.
	OriginDevel = "devel"
)

// Build describes where the running binary came from.
type Build struct {
	// Version is Version.
	Version string
	// Commit is the full or 12-hex commit, "" when unknown.
	Commit string
	// Origin is OriginRelease, OriginModule or OriginDevel.
	Origin string
	// Modified is true for a build from a working tree with changes.
	Modified bool
}

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	// pseudoVersionPattern matches vX.0.0-<time>-<rev>, vX.Y.Z-pre.0.<time>-<rev>
	// and vX.Y.(Z+1)-0.<time>-<rev>, with or without +dirty.
	pseudoVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]*)?[-.]\d{14}-([0-9a-f]{12})(\+dirty)?$`)
	tagVersionPattern    = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

// CurrentBuild resolves the Build of the running binary: a Commit set by
// -ldflags is a release; else a vcs.revision setting is a devel build with that
// commit and vcs.modified; else a pseudo-version of the main module is a module
// build with its 12-hex suffix; else a tag is a module build with no commit;
// else devel with no commit (go run, go test).
func CurrentBuild() Build {
	info, ok := debug.ReadBuildInfo()
	return buildFrom(info, ok, Commit)
}

// buildFrom is CurrentBuild over explicit inputs, for table tests. A commit
// that is not 7 to 40 lowercase hex digits is ignored, so it never reaches a
// git argument.
func buildFrom(info *debug.BuildInfo, ok bool, ldflagsCommit string) Build {
	build := Build{Version: Version, Origin: OriginDevel}
	if commitPattern.MatchString(ldflagsCommit) {
		build.Origin = OriginRelease
		build.Commit = ldflagsCommit
		return build
	}
	if !ok || info == nil {
		return build
	}
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	if commitPattern.MatchString(revision) {
		build.Commit = revision
		build.Modified = modified == "true"
		return build
	}
	if found := pseudoVersionPattern.FindStringSubmatch(info.Main.Version); found != nil {
		build.Origin = OriginModule
		build.Commit = found[1]
		build.Modified = found[2] != ""
		return build
	}
	if tagVersionPattern.MatchString(info.Main.Version) {
		build.Origin = OriginModule
	}
	return build
}
