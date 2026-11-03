package render

import "testing"

// TestGoSrvValue pins the value go-srv reads: every relative segment of a path
// list is prefixed, because go-srv runs from its component folder.
func TestGoSrvValue(t *testing.T) {
	cases := []struct {
		name string
		pair EnvPair
		want string
	}{
		{
			name: "a project-root relative path",
			pair: EnvPair{Key: "APP_FLAGS_FILE", Value: "specs/feature-flags/tck/fixtures/flags.yaml"},
			want: "../specs/feature-flags/tck/fixtures/flags.yaml",
		},
		{
			name: "a bare file name under a file key",
			pair: EnvPair{Key: "APP_FLAGS_FILE", Value: "flags.yaml"},
			want: "../flags.yaml",
		},
		{
			name: "a list prefixes every relative segment",
			pair: EnvPair{Key: "APP_FLAGS_FILE", Value: "flags.yaml,specs/dispatch/tck/fixtures/flags.yaml"},
			want: "../flags.yaml,../specs/dispatch/tck/fixtures/flags.yaml",
		},
		{
			name: "a list trims the blank space around a segment",
			pair: EnvPair{Key: "APP_FLAGS_FILE", Value: "flags.yaml, specs/dispatch/tck/fixtures/flags.yaml "},
			want: "../flags.yaml,../specs/dispatch/tck/fixtures/flags.yaml",
		},
		{
			name: "a list trims a value that is not a path",
			pair: EnvPair{Key: "APP_BOOTSTRAP_ADMIN_PASSWORD", Value: " change-me-local-only , other "},
			want: "change-me-local-only,other",
		},
		{
			name: "a list keeps an absolute segment",
			pair: EnvPair{Key: "APP_FLAGS_FILE", Value: "/etc/flags.yaml,flags.yaml"},
			want: "/etc/flags.yaml,../flags.yaml",
		},
		{
			name: "a list keeps an empty segment for the loader to reject",
			pair: EnvPair{Key: "APP_FLAGS_FILE", Value: "flags.yaml,,specs/x.yaml"},
			want: "../flags.yaml,,../specs/x.yaml",
		},
		{
			name: "a list turns a blank segment into an empty one",
			pair: EnvPair{Key: "APP_FLAGS_FILE", Value: "flags.yaml, ,specs/x.yaml"},
			want: "../flags.yaml,,../specs/x.yaml",
		},
		{
			name: "an absolute path",
			pair: EnvPair{Key: "APP_FLAGS_FILE", Value: "/etc/flags.yaml"},
			want: "/etc/flags.yaml",
		},
		{
			name: "a URL",
			pair: EnvPair{Key: "APP_PUBLIC_BASE_URL", Value: "http://localhost:10120"},
			want: "http://localhost:10120",
		},
		{
			name: "a bare value that is not a path",
			pair: EnvPair{Key: "APP_BOOTSTRAP_ADMIN_PASSWORD", Value: "change-me-local-only"},
			want: "change-me-local-only",
		},
		{
			name: "a bare value under a key that does not name a path",
			pair: EnvPair{Key: "APP_BOOTSTRAP_TENANT_SLUG", Value: "acme"},
			want: "acme",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pair.goSrvValue()
			if got != tc.want {
				t.Fatalf("%s = %q, want %q", tc.pair.Key, got, tc.want)
			}
		})
	}
}
