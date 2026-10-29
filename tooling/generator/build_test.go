package generator

import (
	"regexp"
	"runtime/debug"
	"testing"
)

func settings(pairs ...string) []debug.BuildSetting {
	var out []debug.BuildSetting
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, debug.BuildSetting{Key: pairs[i], Value: pairs[i+1]})
	}
	return out
}

func TestBuildFrom(t *testing.T) {
	const full = "22e304a0dde9f1c4b3a2918e7d6c5b4a39281706"
	cases := map[string]struct {
		info    *debug.BuildInfo
		ok      bool
		ldflags string
		want    Build
	}{
		"ldflags commit is a release": {
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, ok: true, ldflags: full,
			want: Build{Version: Version, Commit: full, Origin: OriginRelease},
		},
		"ldflags beats vcs": {
			info: &debug.BuildInfo{Settings: settings("vcs.revision", "aaaaaaaaaaaa")}, ok: true, ldflags: "bbbbbbbbbbbb",
			want: Build{Version: Version, Commit: "bbbbbbbbbbbb", Origin: OriginRelease},
		},
		"vcs revision is devel": {
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: settings("vcs.revision", full, "vcs.modified", "false")}, ok: true,
			want: Build{Version: Version, Commit: full, Origin: OriginDevel},
		},
		"vcs modified": {
			info: &debug.BuildInfo{Settings: settings("vcs.revision", full, "vcs.modified", "true")}, ok: true,
			want: Build{Version: Version, Commit: full, Origin: OriginDevel, Modified: true},
		},
		"vcs revision beats a stamped pseudo-version": {
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261029120000-0123456789ab+dirty"}, Settings: settings("vcs.revision", full, "vcs.modified", "true")}, ok: true,
			want: Build{Version: Version, Commit: full, Origin: OriginDevel, Modified: true},
		},
		"pseudo-version": {
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261029120000-0123456789ab"}}, ok: true,
			want: Build{Version: Version, Commit: "0123456789ab", Origin: OriginModule},
		},
		"pseudo-version after a tag": {
			info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.4-0.20261029120000-0123456789ab"}}, ok: true,
			want: Build{Version: Version, Commit: "0123456789ab", Origin: OriginModule},
		},
		"pseudo-version dirty": {
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261029120000-0123456789ab+dirty"}}, ok: true,
			want: Build{Version: Version, Commit: "0123456789ab", Origin: OriginModule, Modified: true},
		},
		"tag": {
			info: &debug.BuildInfo{Main: debug.Module{Version: "v1.4.0"}}, ok: true,
			want: Build{Version: Version, Origin: OriginModule},
		},
		"devel with nothing": {
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, ok: true,
			want: Build{Version: Version, Origin: OriginDevel},
		},
		"empty version": {
			info: &debug.BuildInfo{}, ok: true,
			want: Build{Version: Version, Origin: OriginDevel},
		},
		"no build info": {
			want: Build{Version: Version, Origin: OriginDevel},
		},
		"nil info reported ok": {
			ok:   true,
			want: Build{Version: Version, Origin: OriginDevel},
		},
		"ldflags that is not a commit is ignored": {
			ldflags: "--upload-pack=evil",
			want:    Build{Version: Version, Origin: OriginDevel},
		},
		"vcs revision that is not hex is ignored": {
			info: &debug.BuildInfo{Settings: settings("vcs.revision", "main")}, ok: true,
			want: Build{Version: Version, Origin: OriginDevel},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := buildFrom(tc.info, tc.ok, tc.ldflags); got != tc.want {
				t.Fatalf("buildFrom = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCurrentBuildNamesTheGeneratorVersion(t *testing.T) {
	if got := CurrentBuild().Version; got != Version {
		t.Fatalf("Version = %q, want %q", got, Version)
	}
}

var hexOrEmpty = regexp.MustCompile(`^[0-9a-f]*$`)

func FuzzBuildFrom(f *testing.F) {
	f.Add("v0.0.0-20261029120000-0123456789ab+dirty", "0123456789abcdef", "true", "")
	f.Add("(devel)", "", "", "abcdef1")
	f.Add("v1.2.3", "", "", "")
	f.Add("v1.2.4-0.20261029120000-0123456789ab", "x", "y", "-x")
	f.Fuzz(func(t *testing.T, mainVersion, revision, modified, ldflags string) {
		info := &debug.BuildInfo{
			Main:     debug.Module{Version: mainVersion},
			Settings: settings("vcs.revision", revision, "vcs.modified", modified),
		}
		got := buildFrom(info, true, ldflags)
		if !hexOrEmpty.MatchString(got.Commit) || len(got.Commit) > 40 {
			t.Fatalf("commit %q is not empty or hex", got.Commit)
		}
		if got.Version != Version {
			t.Fatalf("version %q", got.Version)
		}
		switch got.Origin {
		case OriginRelease, OriginModule, OriginDevel:
		default:
			t.Fatalf("origin %q", got.Origin)
		}
	})
}
