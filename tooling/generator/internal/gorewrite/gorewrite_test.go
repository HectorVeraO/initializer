package gorewrite

import (
	"bytes"
	"errors"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

const (
	source = "example.com/fixture/srv"
	target = "example.com/acme/backend"
)

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func assertGofmtClean(t *testing.T, out []byte) {
	t.Helper()
	formatted, err := format.Source(out)
	if err != nil {
		t.Fatalf("output does not format: %v", err)
	}
	if !bytes.Equal(formatted, out) {
		t.Fatalf("output is not gofmt-clean:\n%s", out)
	}
}

func TestRewriteImportsFixtures(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		want      string
		wantCount int
	}{
		{"all import forms", "forms.in.go", "forms.out.go", 5},
		{"external test package", "external_test.in.go", "external_test.out.go", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := readFixture(t, tc.in)
			want := readFixture(t, tc.want)
			got, count, err := RewriteImports(tc.in, in, source, target)
			if err != nil {
				t.Fatalf("RewriteImports: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("output differs from %s:\n%s", tc.want, got)
			}
			if count != tc.wantCount {
				t.Fatalf("count = %d, want %d", count, tc.wantCount)
			}
			assertGofmtClean(t, got)
		})
	}
}

func TestRewriteImportsForms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single", "package p\n\nimport \"example.com/fixture/srv/x\"\n", "package p\n\nimport \"example.com/acme/backend/x\"\n"},
		{"exact module path", "package p\n\nimport \"example.com/fixture/srv\"\n", "package p\n\nimport \"example.com/acme/backend\"\n"},
		{"alias", "package p\n\nimport c \"example.com/fixture/srv/x\"\n", "package p\n\nimport c \"example.com/acme/backend/x\"\n"},
		{"dot", "package p\n\nimport . \"example.com/fixture/srv/x\"\n", "package p\n\nimport . \"example.com/acme/backend/x\"\n"},
		{"blank", "package p\n\nimport _ \"example.com/fixture/srv/x\"\n", "package p\n\nimport _ \"example.com/acme/backend/x\"\n"},
		{"raw string keeps quote style", "package p\n\nimport `example.com/fixture/srv/x`\n", "package p\n\nimport `example.com/acme/backend/x`\n"},
		{"only package clause", "package p\n", "package p\n"},
		{"standard library only", "package p\n\nimport \"fmt\"\n", "package p\n\nimport \"fmt\"\n"},
		{"code after imports without the module path is untouched", "package p\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n", "package p\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := RewriteImports("p.go", []byte(tc.in), source, target)
			if err != nil {
				t.Fatalf("RewriteImports: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRewriteImportsCounts(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty package", "package p\n", 0},
		{"only module imports", "package p\n\nimport (\n\t\"example.com/fixture/srv/a\"\n\t\"example.com/fixture/srv/b\"\n)\n", 2},
		{"mixed declarations", "package p\n\nimport \"fmt\"\n\nimport \"example.com/fixture/srv/a\"\n", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, count, err := RewriteImports("p.go", []byte(tc.in), source, target)
			if err != nil {
				t.Fatalf("RewriteImports: %v", err)
			}
			if count != tc.want {
				t.Fatalf("count = %d, want %d", count, tc.want)
			}
		})
	}
}

func TestRewriteImportsLengthChange(t *testing.T) {
	in := "package p\n\nimport (\n\t\"example.com/fixture/srv/a\"\n\t\"example.com/fixture/srv/b\"\n\t\"example.com/fixture/srv/c\"\n)\n"
	tests := []struct {
		name string
		to   string
	}{
		{"longer", "example.com/a/very/much/longer/module/path"},
		{"shorter", "a.io/m"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, count, err := RewriteImports("p.go", []byte(in), source, tc.to)
			if err != nil {
				t.Fatalf("RewriteImports: %v", err)
			}
			want := strings.ReplaceAll(in, source, tc.to)
			if string(got) != want || count != 3 {
				t.Fatalf("got %q count %d, want %q count 3", got, count, want)
			}
		})
	}
}

func TestRewriteImportsIdentity(t *testing.T) {
	in := readFixture(t, "forms.in.go")
	got, count, err := RewriteImports("forms.go", in, source, source)
	if err != nil {
		t.Fatalf("RewriteImports: %v", err)
	}
	if !bytes.Equal(got, in) || count != 5 {
		t.Fatalf("identity changed bytes or count %d != 5", count)
	}
}

func TestRewriteImportsRefusals(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"string literal", "package p\n\nvar s = \"example.com/fixture/srv\"\n", ErrModulePathLeak},
		{"line comment", "package p\n\n// see example.com/fixture/srv\n", ErrModulePathLeak},
		{"block comment", "package p\n\n/* example.com/fixture/srv */\n", ErrModulePathLeak},
		{"go generate line", "package p\n\n//go:generate go run example.com/fixture/srv/cmd/gen\n", ErrModulePathLeak},
		{"struct tag", "package p\n\ntype T struct {\n\tA string `json:\"example.com/fixture/srv\"`\n}\n", ErrModulePathLeak},
		{"comment at end of file", "package p\n\n// example.com/fixture/srv", ErrModulePathLeak},
		{"strict prefix sibling", "package p\n\nimport \"example.com/fixture/srvx/a\"\n", ErrModulePathLeak},
		{"escaped path", "package p\n\nimport \"example.com/fixture/\\x73rv/a\"\n", ErrModulePathLeak},
		{"mixed group", "package p\n\nimport (\n\t\"fmt\"\n\t\"example.com/fixture/srv/a\"\n)\n", ErrImportGroup},
		{"module path in code after imports", "package p\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint(\"example.com/fixture/srv\")\n", ErrModulePathLeak},
		{"doc-commented module import in mixed group", "package p\n\nimport (\n\t\"fmt\"\n\t// c\n\t\"example.com/fixture/srv/a\"\n)\n", ErrImportGroup},
		{"trailing comment", "package p\n\nimport (\n\t\"example.com/fixture/srv/a\" // why\n)\n", ErrImportGroup},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, count, err := RewriteImports("p.go", []byte(tc.in), source, target)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if got != nil || count != 0 {
				t.Fatalf("partial output returned: %q %d", got, count)
			}
			if !strings.Contains(err.Error(), "p.go:") {
				t.Fatalf("error lacks position: %v", err)
			}
		})
	}
}

func TestRewriteImportsErrorNamesFirstLine(t *testing.T) {
	in := "package p\n\n// ok\n// leak example.com/fixture/srv\n"
	_, _, err := RewriteImports("p.go", []byte(in), source, target)
	if err == nil || !strings.HasPrefix(err.Error(), "p.go:4:") {
		t.Fatalf("err = %v, want position p.go:4", err)
	}
}

func TestRewriteImportsParseError(t *testing.T) {
	in := readFixture(t, "broken.go.txt")
	_, _, err := RewriteImports("broken.go", in, source, target)
	isSentinel := errors.Is(err, ErrModulePathLeak) || errors.Is(err, ErrImportGroup)
	if err == nil || isSentinel || !strings.HasPrefix(err.Error(), "broken.go:") {
		t.Fatalf("want a parse error positioned at broken.go, got %v", err)
	}
}

func TestRewriteImportsArguments(t *testing.T) {
	in := []byte("package p\n")
	tests := []struct {
		name     string
		from, to string
	}{
		{"empty from", "", target},
		{"empty to", source, ""},
		{"to without dot in host", source, "acme/backend"},
		{"to with trailing slash", source, "example.com/acme/"},
		{"to with dot segment", source, "example.com/../x"},
		{"to with uppercase host", source, "Example.com/x"},
		{"to with space", source, "example.com/a b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := RewriteImports("p.go", in, tc.from, tc.to); err == nil {
				t.Fatal("expected an argument error")
			}
			if _, err := RewriteModuleLine([]byte("module "+tc.from+"\n"), tc.from, tc.to); err == nil {
				t.Fatal("expected an argument error from RewriteModuleLine")
			}
		})
	}
}

func TestRewriteImportsRoundTrip(t *testing.T) {
	property := func(names []uint8) bool {
		var b strings.Builder
		b.WriteString("package p\n\nimport (\n\t\"fmt\"\n\n")
		letters := "abcdefghij"
		for i, n := range names {
			if i >= 8 {
				break
			}
			b.WriteString("\t\"" + source + "/" + string(letters[int(n)%len(letters)]) + string(letters[i]) + "\"\n")
		}
		b.WriteString(")\n")
		in := []byte(b.String())
		if len(names) == 0 {
			in = []byte("package p\n\nimport \"fmt\"\n")
		}
		forward, count, err := RewriteImports("p.go", in, source, target)
		if err != nil {
			return false
		}
		if want := min(len(names), 8); count != want {
			return false
		}
		back, backCount, err := RewriteImports("p.go", forward, target, source)
		return err == nil && backCount == count && bytes.Equal(back, in)
	}
	if err := quick.Check(property, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRewriteModuleLine(t *testing.T) {
	good := "module example.com/fixture/srv\n\ngo 1.26\n\nrequire example.com/other v1.0.0\n"
	got, err := RewriteModuleLine([]byte(good), source, target)
	if err != nil {
		t.Fatalf("RewriteModuleLine: %v", err)
	}
	want := strings.Replace(good, source, target, 1)
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	single, err := RewriteModuleLine([]byte("module "+source), source, target)
	if err != nil || string(single) != "module "+target {
		t.Fatalf("single line: %q, %v", single, err)
	}
}

func TestRewriteModuleLineRefusals(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"first line is a comment", "// c\nmodule example.com/fixture/srv\n", nil},
		{"other module", "module example.com/other\n", nil},
		{"trailing comment", "module example.com/fixture/srv // x\n", nil},
		{"longer module path", "module example.com/fixture/srv/v2\n", nil},
		{"second occurrence", "module example.com/fixture/srv\n\nreplace example.com/other => example.com/fixture/srv/x\n", ErrModulePathLeak},
		{"crlf", "module example.com/fixture/srv\r\n\r\ngo 1.26\r\n", nil},
		{"empty", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RewriteModuleLine([]byte(tc.in), source, target)
			if err == nil || got != nil {
				t.Fatalf("expected a refusal, got %q, %v", got, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCheckFile(t *testing.T) {
	cleanGo := []byte("package p\n\nimport \"example.com/fixture/srv/a\"\n")
	tests := []struct {
		name     string
		path     string
		src      string
		wantRule string
		wantLine int
	}{
		{"clean go file", "go-srv/a.go", string(cleanGo), "", 0},
		{"go file under testdata is a plain file", "go-srv/testdata/a.go", string(cleanGo), "R1", 3},
		{"leak in go comment", "go-srv/a.go", "package p\n// example.com/fixture/srv\n", "R1", 2},
		{"mixed group", "go-srv/a.go", "package p\n\nimport (\n\t\"fmt\"\n\t\"example.com/fixture/srv/a\"\n)\n", "R2", 4},
		{"trailing comment", "go-srv/a.go", "package p\n\nimport (\n\t\"example.com/fixture/srv/a\" // c\n)\n", "R2", 4},
		{"unparseable go file", "go-srv/a.go", "package p\n\nimport (\n", "R1", 1},
		{"go.mod once", GoModPath, "module example.com/fixture/srv\n", "", 0},
		{"go.mod replace", GoModPath, "module example.com/fixture/srv\nreplace a => example.com/fixture/srv/x\n", "R1", 2},
		{"go.mod without module", GoModPath, "module example.com/other\n", "R1", 1},
		{"go.mod module line only prefixed", GoModPath, "module example.com/fixture/srv/v2\n", "R1", 1},
		{"other file leak", "docs/readme.md", "line one\nsee example.com/fixture/srv\n", "R1", 2},
		{"other file clean", "docs/readme.md", "nothing here\n", "", 0},
		{"windows separators", "go-srv\\go.mod", "module example.com/fixture/srv\n", "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := CheckFile(tc.path, []byte(tc.src), source)
			if tc.wantRule == "" {
				if len(findings) != 0 {
					t.Fatalf("unexpected findings %+v", findings)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatal("expected a finding")
			}
			first := findings[0]
			if first.Rule != tc.wantRule || first.Line != tc.wantLine || first.Detail == "" {
				t.Fatalf("first finding = %+v, want rule %s line %d", first, tc.wantRule, tc.wantLine)
			}
		})
	}
}

func TestCheckFileEmptyModule(t *testing.T) {
	if findings := CheckFile("a.go", []byte("package p\n"), ""); len(findings) == 0 {
		t.Fatal("empty module must be reported")
	}
}

func TestShouldRewrite(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"go-srv/a.go", true},
		{"go-srv/a_test.go", true},
		{"go-srv/testdata/a.go", false},
		{"go-srv/pkg/testdata/deep/a.go", false},
		{"testdata/a.go", false},
		{"go-srv\\testdata\\a.go", false},
		{"go-srv/a.txt", false},
		{"go-srv/go.mod", false},
		{"go-srv/mytestdata/a.go", true},
	}
	for _, tc := range tests {
		if got := ShouldRewrite(tc.path); got != tc.want {
			t.Errorf("ShouldRewrite(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestShouldRewriteFixtureTree(t *testing.T) {
	var rewritten []string
	root := filepath.Join("testdata", "tree")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if !entry.IsDir() && ShouldRewrite(relative) {
			rewritten = append(rewritten, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(rewritten) != 1 || rewritten[0] != "a.go" {
		t.Fatalf("rewritten = %v, want [a.go]", rewritten)
	}
}
