package migrationlint_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/migrationlint"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/report"
)

const (
	sysManifest = `{"module":"parameters","tablePrefixes":["sys"],"unknownKey":1}`
	modDir      = "go-srv/internal/gatbase/parameters/"
	goodName    = "20260101120000_sys_create_parameters.sql"
	upLine      = "-- +goose Up\n"
)

func put(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	err := os.MkdirAll(filepath.Dir(full), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(full, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

// fromTestdata copies testdata/<name> into a fresh temp dir.
func fromTestdata(t *testing.T, name string) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("testdata", name)
	err := filepath.WalkDir(src, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(src, p)
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		put(t, root, filepath.ToSlash(rel), string(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// sysModule returns a temp repo with the sys manifest and one migration named name.
func sysModule(t *testing.T, name, sql string) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, modDir+"module.manifest.json", sysManifest)
	put(t, root, modDir+"migrations/"+name, sql)
	return root
}

func lint(t *testing.T, root, base string) ([]report.Violation, int) {
	t.Helper()
	vs, n, err := migrationlint.Lint(migrationlint.Options{Root: root, BaseRev: base})
	if err != nil {
		t.Fatal(err)
	}
	return vs, n
}

// summary renders "RULE@path:line" entries, sorted.
func summary(vs []report.Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, fmt.Sprintf("%s@%s:%d", v.Rule, v.Path, v.Line))
	}
	sort.Strings(out)
	return out
}

func rules(vs []report.Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.Rule)
	}
	sort.Strings(out)
	return out
}

func expectRules(t *testing.T, vs []report.Violation, want ...string) {
	t.Helper()
	sort.Strings(want)
	got := rules(vs)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rules = %v, want %v\n%v", got, want, summary(vs))
	}
	for _, v := range vs {
		if v.Ref != "RA.3" || v.Severity != report.SeverityError || strings.Contains(v.Path, `\`) {
			t.Fatalf("bad violation %+v", v)
		}
	}
}

func TestValidSysModulePasses(t *testing.T) {
	vs, n := lint(t, fromTestdata(t, "valid"), "")
	expectRules(t, vs)
	if n != 1 {
		t.Fatalf("files = %d", n)
	}
}

func TestNoGoSrvMeansZeroFiles(t *testing.T) {
	vs, n := lint(t, t.TempDir(), "")
	expectRules(t, vs)
	if n != 0 {
		t.Fatalf("files = %d", n)
	}
}

func TestUnprefixedTablesFailNameRule(t *testing.T) {
	vs, _ := lint(t, fromTestdata(t, "h1"), "")
	expectRules(t, vs, "TABLE_NAME_INVALID", "TABLE_NAME_INVALID")
	if vs[0].Line != 2 || vs[1].Line != 3 {
		t.Fatalf("lines: %v", summary(vs))
	}
}

func TestStrippedConstructsNeverTriggerTableRules(t *testing.T) {
	vs, _ := lint(t, fromTestdata(t, "stripped"), "")
	expectRules(t, vs)
}

func TestDestructiveStatements(t *testing.T) {
	vs, _ := lint(t, fromTestdata(t, "destructive"), "")
	var lines []string
	for _, v := range vs {
		if v.Rule != "MIGRATION_DESTRUCTIVE" {
			t.Fatalf("unexpected %v", summary(vs))
		}
		lines = append(lines, fmt.Sprint(v.Line))
	}
	if strings.Join(lines, ",") != "2,3,4,5,6,7,8,9,14,16" {
		t.Fatalf("lines = %v", lines)
	}
}

func TestStatementRules(t *testing.T) {
	cases := map[string]struct {
		sql  string
		want []string
	}{
		"quoted table":          {"CREATE TABLE \"sys_x\" (id int);", []string{"TABLE_QUOTED_IDENTIFIER"}},
		"quoted schema":         {"CREATE TABLE \"public\".sys_x (id int);", []string{"TABLE_QUOTED_IDENTIFIER"}},
		"public qualifier":      {"CREATE TABLE public.sys_x (id int);", nil},
		"other schema":          {"CREATE TABLE other.sys_x (id int);", []string{"TABLE_SCHEMA_INVALID"}},
		"three part":            {"CREATE TABLE db.public.sys_x (id int);", []string{"TABLE_SCHEMA_INVALID"}},
		"foreign key":           {"CREATE TABLE sys_x (t uuid REFERENCES ten_tenants (id));", []string{"TABLE_PREFIX_MISMATCH"}},
		"own foreign key":       {"create table if not exists sys_x (t uuid references sys_y (id));", nil},
		"prefix 1 letter":       {"CREATE TABLE s_x (id int);", []string{"TABLE_NAME_INVALID"}},
		"prefix 5 letters":      {"CREATE TABLE sysxx_x (id int);", []string{"TABLE_NAME_INVALID"}},
		"uppercase name":        {"CREATE TABLE Sys_X (id int);", []string{"TABLE_NAME_INVALID"}},
		"foreign create":        {"CREATE TABLE iam_x (id int);", []string{"TABLE_PREFIX_MISMATCH"}},
		"alter foreign":         {"ALTER TABLE ONLY iam_x ADD COLUMN a int;", []string{"TABLE_PREFIX_MISMATCH"}},
		"alter if exists":       {"alter table if exists only sys_x add column a int;", nil},
		"index foreign":         {"CREATE UNIQUE INDEX IF NOT EXISTS i ON ONLY iam_x (a);", []string{"TABLE_PREFIX_MISMATCH"}},
		"index no name":         {"CREATE INDEX ON sys_x (a);", nil},
		"index bad table":       {"CREATE INDEX i ON parameters (a);", []string{"TABLE_NAME_INVALID"}},
		"drop foreign":          {"DROP TABLE IF EXISTS sys_a, iam_b;", []string{"MIGRATION_DESTRUCTIVE", "TABLE_PREFIX_MISMATCH"}},
		"rename target foreign": {"ALTER TABLE sys_a RENAME TO iam_a;", []string{"MIGRATION_DESTRUCTIVE", "TABLE_PREFIX_MISMATCH"}},
		"alter fk":              {"ALTER TABLE sys_a ADD CONSTRAINT c FOREIGN KEY (t) REFERENCES public.ten_t (id);", []string{"TABLE_PREFIX_MISMATCH"}},
		"unrelated statement":   {"INSERT INTO anything VALUES (1);", nil},
		"uppercase keywords":    {"ALTER TABLE SYS_X ALTER COLUMN a SET NOT NULL;", []string{"MIGRATION_DESTRUCTIVE", "TABLE_NAME_INVALID"}},
		"escape string":         {"CREATE TABLE sys_x (a text DEFAULT E'it\\'s; DROP TABLE zz_q');", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			vs, _ := lint(t, sysModule(t, goodName, upLine+tc.sql+"\n"), "")
			expectRules(t, vs, tc.want...)
		})
	}
}

func TestFileRules(t *testing.T) {
	cases := map[string]struct {
		name string
		body string
		want []string
	}{
		"no prefix":       {"20260101120000_create.sql", upLine, []string{"MIGRATION_NAME_INVALID"}},
		"13 digits":       {"2026010112000_sys_a.sql", upLine, []string{"MIGRATION_NAME_INVALID"}},
		"15 digits":       {"202601011200000_sys_a.sql", upLine, []string{"MIGRATION_NAME_INVALID"}},
		"1 letter":        {"20260101120000_s_a.sql", upLine, []string{"MIGRATION_NAME_INVALID"}},
		"5 letters":       {"20260101120000_sysxx_a.sql", upLine, []string{"MIGRATION_NAME_INVALID"}},
		"uppercase name":  {"20260101120000_sys_Create.sql", upLine, []string{"MIGRATION_NAME_INVALID"}},
		"bad month":       {"20261332000000_sys_a.sql", upLine, []string{"MIGRATION_TIMESTAMP_INVALID"}},
		"bad hour":        {"20260101240000_sys_a.sql", upLine, []string{"MIGRATION_TIMESTAMP_INVALID"}},
		"goose missing":   {goodName, "CREATE TABLE sys_x (id int);\n", []string{"MIGRATION_GOOSE_UP_MISSING"}},
		"goose down only": {goodName, "-- +goose Down\n", []string{"MIGRATION_GOOSE_UP_MISSING"}},
		"goose crlf":      {goodName, "-- +goose Up\r\nCREATE TABLE sys_x (id int);\r\n", nil},
		"unregistered":    {"20260101120000_zzz_a.sql", upLine, []string{"MIGRATION_PREFIX_UNREGISTERED"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			vs, _ := lint(t, sysModule(t, tc.name, tc.body), "")
			expectRules(t, vs, tc.want...)
		})
	}
}

func TestLocationRule(t *testing.T) {
	cases := []string{
		"go-srv/db/20260101120000_sys_a.sql",
		"go-srv/internal/gatbase/parameters/20260101120000_sys_a.sql",
		"go-srv/internal/gatbase/parameters/migrations/sub/20260101120000_sys_a.sql",
		"go-srv/internal/gatbase/parameters/migrations/schema.SQL",
	}
	for _, rel := range cases {
		t.Run(rel, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, modDir+"module.manifest.json", sysManifest)
			put(t, root, rel, upLine)
			vs, n := lint(t, root, "")
			expectRules(t, vs, "MIGRATION_LOCATION_INVALID")
			if n != 1 || vs[0].Path != rel {
				t.Fatalf("n=%d %v", n, summary(vs))
			}
		})
	}
}

func TestSQLOutsideGoSrvIsNotScanned(t *testing.T) {
	root := sysModule(t, goodName, upLine)
	put(t, root, "tooling/gates/testdata/x/bad.sql", "CREATE TABLE parameters (id int);")
	vs, n := lint(t, root, "")
	expectRules(t, vs)
	if n != 1 {
		t.Fatalf("n = %d", n)
	}
}

func TestDuplicateTimestampAcrossModules(t *testing.T) {
	root := sysModule(t, goodName, upLine)
	put(t, root, "go-srv/internal/gatbase/flags/module.manifest.json", `{"module":"flags","tablePrefixes":["flg"]}`)
	put(t, root, "go-srv/internal/gatbase/flags/migrations/20260101120000_flg_create.sql", upLine)
	vs, _ := lint(t, root, "")
	expectRules(t, vs, "MIGRATION_TIMESTAMP_DUPLICATE")
	if vs[0].Path != "go-srv/internal/gatbase/parameters/migrations/"+goodName {
		t.Fatalf("%v", summary(vs))
	}
}

func TestModuleRegistration(t *testing.T) {
	cases := map[string]struct {
		manifest string
		want     []string
	}{
		"no manifest":    {"", []string{"MIGRATION_MODULE_UNREGISTERED"}},
		"empty prefixes": {`{"module":"parameters","tablePrefixes":[]}`, []string{"MIGRATION_MODULE_UNREGISTERED"}},
		"missing key":    {`{"module":"parameters"}`, []string{"MIGRATION_MODULE_UNREGISTERED"}},
		"broken json":    {`{`, []string{"MIGRATION_MODULE_UNREGISTERED"}},
		"bad prefix":     {`{"tablePrefixes":["sys","SYS1"]}`, []string{"MIGRATION_MODULE_UNREGISTERED"}},
		"reserved":       {`{"tablePrefixes":["sys","aud"]}`, []string{"MIGRATION_PREFIX_RESERVED"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if tc.manifest != "" {
				put(t, root, modDir+"module.manifest.json", tc.manifest)
			}
			put(t, root, modDir+"migrations/"+goodName, upLine)
			vs, _ := lint(t, root, "")
			expectRules(t, vs, tc.want...)
		})
	}
}

func TestEveryReservedPrefixIsRejectedEvenWithoutMigrations(t *testing.T) {
	for _, prefix := range []string{"aud", "pln", "mtr", "bil", "mkc", "jbr", "stg"} {
		root := t.TempDir()
		put(t, root, modDir+"module.manifest.json", fmt.Sprintf(`{"tablePrefixes":[%q]}`, prefix))
		vs, n := lint(t, root, "")
		expectRules(t, vs, "MIGRATION_PREFIX_RESERVED")
		if n != 0 {
			t.Fatal(n)
		}
	}
}

func TestPrefixClaimedByTwoManifests(t *testing.T) {
	root := t.TempDir()
	put(t, root, modDir+"module.manifest.json", sysManifest)
	put(t, root, "go-srv/internal/gatbase/other/module.manifest.json", `{"tablePrefixes":["sys"]}`)
	vs, _ := lint(t, root, "")
	expectRules(t, vs, "MIGRATION_PREFIX_RESERVED")
	if !strings.Contains(vs[0].Path, "parameters") {
		t.Fatalf("%v", summary(vs))
	}
}

func TestForeignPrefixInFileName(t *testing.T) {
	root := sysModule(t, "20260101120000_flg_a.sql", upLine)
	put(t, root, "go-srv/internal/gatbase/flags/module.manifest.json", `{"tablePrefixes":["flg"]}`)
	vs, _ := lint(t, root, "")
	expectRules(t, vs, "MIGRATION_PREFIX_FOREIGN")
}

func TestGatbaseDirIsNotAFixedList(t *testing.T) {
	root := t.TempDir()
	put(t, root, "go-srv/internal/gatbase/my_own/module.manifest.json", `{"tablePrefixes":["myx"]}`)
	put(t, root, "go-srv/internal/gatbase/my_own/migrations/20260101120000_myx_a.sql", upLine+"CREATE TABLE myx_a (id int);\n")
	vs, n := lint(t, root, "")
	expectRules(t, vs)
	if n != 1 {
		t.Fatal(n)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=Example Tester", "-c", "user.email=tester@example.com", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// baseRepo commits two sys migrations on main and switches to branch topic.
func baseRepo(t *testing.T) string {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	root := t.TempDir()
	put(t, root, modDir+"module.manifest.json", sysManifest)
	put(t, root, modDir+"migrations/20260101120000_sys_one.sql", upLine+"CREATE TABLE sys_one (id int);\n")
	put(t, root, modDir+"migrations/20260201120000_sys_two.sql", upLine+"CREATE TABLE sys_two (id int);\n")
	git(t, root, "init", "-b", "main")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-m", "base")
	git(t, root, "switch", "-c", "topic")
	return root
}

func commitAll(t *testing.T, root string) {
	t.Helper()
	git(t, root, "add", "-A")
	git(t, root, "commit", "--allow-empty", "-m", "change")
}

func TestBaseChecks(t *testing.T) {
	const prefix = modDir + "migrations/"
	cases := map[string]struct {
		change func(t *testing.T, root string)
		want   []string
	}{
		"untouched": {func(t *testing.T, root string) {}, nil},
		"modified": {func(t *testing.T, root string) {
			put(t, root, prefix+"20260101120000_sys_one.sql", upLine+"CREATE TABLE sys_one (id int, a int);\n")
		}, []string{"MIGRATION_MODIFIED@" + prefix + "20260101120000_sys_one.sql:0"}},
		"deleted": {func(t *testing.T, root string) {
			_ = os.Remove(filepath.Join(root, filepath.FromSlash(prefix+"20260101120000_sys_one.sql")))
		}, []string{"MIGRATION_MODIFIED@" + prefix + "20260101120000_sys_one.sql:0"}},
		"renamed": {func(t *testing.T, root string) {
			err := os.Rename(filepath.Join(root, filepath.FromSlash(prefix+"20260201120000_sys_two.sql")),
				filepath.Join(root, filepath.FromSlash(prefix+"20260301120000_sys_two.sql")))
			if err != nil {
				t.Fatal(err)
			}
		}, []string{"MIGRATION_MODIFIED@" + prefix + "20260201120000_sys_two.sql:0"}},
		"newer file added": {func(t *testing.T, root string) {
			put(t, root, prefix+"20260301120000_sys_three.sql", upLine)
		}, nil},
		"older file added": {func(t *testing.T, root string) {
			put(t, root, prefix+"20260115120000_sys_late.sql", upLine)
		}, []string{"MIGRATION_OUT_OF_ORDER@" + prefix + "20260115120000_sys_late.sql:0"}},
		"first file of another module sorts earlier": {func(t *testing.T, root string) {
			put(t, root, "go-srv/internal/gatbase/flags/module.manifest.json", `{"tablePrefixes":["flg"]}`)
			put(t, root, "go-srv/internal/gatbase/flags/migrations/20250101120000_flg_a.sql", upLine)
		}, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := baseRepo(t)
			tc.change(t, root)
			commitAll(t, root)
			vs, _ := lint(t, root, "main")
			got := summary(vs)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEmptyBaseRevSkipsHistoryRules(t *testing.T) {
	root := baseRepo(t)
	put(t, root, modDir+"migrations/20260101120000_sys_one.sql", upLine)
	commitAll(t, root)
	vs, _ := lint(t, root, "")
	expectRules(t, vs)
}

func TestBaseUsesMergeBase(t *testing.T) {
	root := baseRepo(t)
	git(t, root, "switch", "main")
	put(t, root, modDir+"migrations/20260301120000_sys_three.sql", upLine)
	commitAll(t, root)
	git(t, root, "switch", "topic")
	put(t, root, modDir+"migrations/20260215120000_sys_mid.sql", upLine)
	commitAll(t, root)
	vs, _ := lint(t, root, "main")
	// Compared with the merge base (not main's tip), the newest base file is 20260201120000.
	expectRules(t, vs)
}

func TestUnknownBaseRevIsAnError(t *testing.T) {
	root := baseRepo(t)
	_, _, err := migrationlint.Lint(migrationlint.Options{Root: root, BaseRev: "no-such-rev"})
	if err == nil {
		t.Fatal("expected an error")
	}
}
