package semver_test

import (
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/semver"
)

func TestParseVersionRejects(t *testing.T) {
	bad := []string{
		"01.0.0", "1.0", "v1.0.0", "1.0.0-rc.1", "1.0.0+b", "-1.0.0", " 1.0.0", "1.0.0 ",
		"", "1.0.0.0", "1..0", "a.b.c", "1.0.00", "+1.0.0", "1.0.0\n",
		"18446744073709551616.0.0", "1.18446744073709551616.0", "1.0.99999999999999999999",
	}
	for _, input := range bad {
		if _, err := semver.ParseVersion(input); err == nil {
			t.Errorf("ParseVersion(%q) accepted", input)
		}
	}
}

func TestParseVersionAccepts(t *testing.T) {
	cases := map[string]semver.Version{
		"0.0.0":                    {},
		"1.2.3":                    {Major: 1, Minor: 2, Patch: 3},
		"10.20.30":                 {Major: 10, Minor: 20, Patch: 30},
		"18446744073709551615.0.0": {Major: 18446744073709551615},
	}
	for input, want := range cases {
		got, err := semver.ParseVersion(input)
		if err != nil || got != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	v := func(a, b, c uint64) semver.Version { return semver.Version{Major: a, Minor: b, Patch: c} }
	cases := []struct {
		a, b semver.Version
		want int
	}{
		{v(1, 0, 0), v(1, 0, 0), 0},
		{v(1, 0, 0), v(2, 0, 0), -1},
		{v(1, 2, 0), v(1, 1, 9), 1},
		{v(1, 2, 3), v(1, 2, 4), -1},
		{v(0, 0, 0), v(0, 0, 1), -1},
	}
	for _, tc := range cases {
		if got := tc.a.Compare(tc.b); got != tc.want {
			t.Errorf("%v.Compare(%v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestRangeContains(t *testing.T) {
	cases := []struct {
		rng      string
		in, out  []string
		wantText string
	}{
		{"^1.2.3", []string{"1.2.3", "1.9.0", "1.2.4"}, []string{"2.0.0", "1.2.2", "0.9.9", "1.1.9"}, "^1.2.3"},
		{"^0.1.0", []string{"0.1.0", "0.1.9"}, []string{"0.2.0", "0.0.9", "1.1.0"}, "^0.1.0"},
		{"^0.0.3", []string{"0.0.3"}, []string{"0.0.4", "0.0.2", "0.1.3", "1.0.3"}, "^0.0.3"},
		{"1.2.3", []string{"1.2.3"}, []string{"1.2.4", "1.2.2", "1.3.3", "2.2.3"}, "1.2.3"},
		{"^18446744073709551615.0.0", []string{"18446744073709551615.5.5"}, []string{"1.0.0"}, "^18446744073709551615.0.0"},
	}
	for _, tc := range cases {
		r, err := semver.ParseRange(tc.rng)
		if err != nil {
			t.Fatalf("ParseRange(%q): %v", tc.rng, err)
		}
		if r.String() != tc.wantText {
			t.Errorf("String() = %q, want %q", r.String(), tc.wantText)
		}
		for _, text := range tc.in {
			v, _ := semver.ParseVersion(text)
			if !r.Contains(v) {
				t.Errorf("%s should contain %s", tc.rng, text)
			}
		}
		for _, text := range tc.out {
			v, _ := semver.ParseVersion(text)
			if r.Contains(v) {
				t.Errorf("%s should not contain %s", tc.rng, text)
			}
		}
	}
}

func TestParseRangeRejects(t *testing.T) {
	for _, input := range []string{"", "^", "~1.0.0", ">=1.0.0", "^^1.0.0", "^1.0", "*", "1.x", "^ 1.0.0", " ^1.0.0", "^v1.0.0", "^1.0.0-rc.1"} {
		if _, err := semver.ParseRange(input); err == nil {
			t.Errorf("ParseRange(%q) accepted", input)
		}
	}
}

func FuzzParseVersion(f *testing.F) {
	for _, seed := range []string{"0.0.0", "1.2.3", "01.0.0", "1.0", "v1.0.0", "18446744073709551616.0.0", "", "1.0.0-rc.1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		v, err := semver.ParseVersion(input)
		if err != nil {
			return
		}
		if v.String() != input {
			t.Fatalf("round trip: %q -> %q", input, v.String())
		}
	})
}

func FuzzParseRange(f *testing.F) {
	for _, seed := range []string{"1.2.3", "^1.2.3", "^0.1.0", "^0.0.3", "^", "~1", "", "^18446744073709551615.18446744073709551615.18446744073709551615"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		r, err := semver.ParseRange(input)
		if err != nil {
			return
		}
		if !r.Contains(r.Base()) {
			t.Fatalf("%q does not contain its base %v", input, r.Base())
		}
		if r.String() != input {
			t.Fatalf("round trip: %q -> %q", input, r.String())
		}
	})
}
