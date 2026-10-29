package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

// infraDryRun runs a dry run against the real checkout and returns the exit
// code, stdout and stderr. extra holds the flags that follow the common ones.
func infraDryRun(t *testing.T, extra ...string) (code int, stdout, stderr string) {
	t.Helper()
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	args := []string{"generate", out, "--root", root, "--catalog", catalogPath, "--dry-run", "--go-module", "example.com/acme"}
	return execute(t, append(args, extra...)...)
}

// infraDryRunJSON runs a dry run and decodes the names of its document.
func infraDryRunJSON(t *testing.T, extra ...string) dryRunDocument {
	t.Helper()
	code, stdout, stderr := infraDryRun(t, append(extra, "--format", "json")...)
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	var doc dryRunDocument
	decodeDocument(t, stdout, &doc)
	return doc
}

func TestTheProjectNamesTheDatabaseAndServices(t *testing.T) {
	doc := infraDryRunJSON(t, "--name", "acme", "--stacks", "go-srv,ng-web", "--all-modules")
	want := map[string]string{
		"database": "acme", "dbUser": "acme", "envPrefix": "ACME", "project": "acme", "displayName": "acme",
		"service.postgres": "acme-postgres", "service.postgres-init": "acme-postgres-init",
		"service.kratos-migrate": "acme-kratos-migrate", "service.kratos": "acme-kratos",
		"component.go-srv": "acme-srv", "component.ng-web": "acme-web",
	}
	if len(doc.Names) != len(want) {
		t.Fatalf("names = %v, want %v", doc.Names, want)
	}
	for slot, value := range want {
		if doc.Names[slot] != value {
			t.Errorf("names[%s] = %q, want %q", slot, doc.Names[slot], value)
		}
	}
}

func TestAHyphenatedProjectNamesTheDatabaseWithUnderscores(t *testing.T) {
	doc := infraDryRunJSON(t, "--name", "acme-backend", "--stacks", "go-srv,ng-web", "--all-modules")
	want := map[string]string{
		"database": "acme_backend", "dbUser": "acme_backend", "envPrefix": "ACME_BACKEND",
		"service.postgres": "acme-backend-postgres", "service.kratos-migrate": "acme-backend-kratos-migrate",
	}
	for slot, value := range want {
		if doc.Names[slot] != value {
			t.Errorf("names[%s] = %q, want %q", slot, doc.Names[slot], value)
		}
	}
}

func TestGenerateDeliversTheRenamedInfrastructure(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", out, "--root", root, "--catalog", catalogPath, "--name", "acme",
		"--go-module", "example.com/acme", "--stacks", "go-srv,ng-web", "--all-modules")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	read := func(path string) string {
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	env := read(".deploy/local/.example.env")
	for _, want := range []string{"ACME_PG_PASSWORD=", `postgres://acme:${ACME_PG_PASSWORD}@localhost:10132/acme?`} {
		if !strings.Contains(env, want) {
			t.Errorf(".example.env lacks %q", want)
		}
	}
	readme := read(".deploy/local/README.md")
	for _, want := range []string{"`acme-postgres`", "`acme-kratos` public API", "`acme-postgres-init` runs on every", "`acme-kratos-migrate` applies"} {
		if !strings.Contains(readme, want) {
			t.Errorf("the deploy README lacks %q", want)
		}
	}
}

func TestFlagsOverrideTheDatabaseAndServices(t *testing.T) {
	doc := infraDryRunJSON(t, "--name", "acme-backend", "--stacks", "go-srv", "--all-modules",
		"--db-name", "ledger", "--db-user", "ledger_rw", "--service", "postgres=acme-db", "--service", "kratos=acme-auth")
	want := map[string]string{
		"database": "ledger", "dbUser": "ledger_rw", "service.postgres": "acme-db", "service.kratos": "acme-auth",
		"service.postgres-init": "acme-backend-postgres-init", "envPrefix": "ACME_BACKEND",
	}
	for slot, value := range want {
		if doc.Names[slot] != value {
			t.Errorf("names[%s] = %q, want %q", slot, doc.Names[slot], value)
		}
	}
	wantSelection := `"database":{"name":"ledger","user":"ledger_rw"},"services":{"kratos":"acme-auth","postgres":"acme-db"}`
	resolvedServices := `"database":{"name":"ledger","user":"ledger_rw"},"services":{"kratos":"acme-auth","kratos-migrate":"acme-backend-kratos-migrate","postgres":"acme-db","postgres-init":"acme-backend-postgres-init"}`
	if !strings.Contains(string(doc.Selection), resolvedServices) {
		t.Errorf("selection %s lacks %s", doc.Selection, resolvedServices)
	}
	if !strings.Contains(string(doc.Recorded), wantSelection) {
		t.Errorf("recorded %s lacks %s", doc.Recorded, wantSelection)
	}
}

func TestAFileOverridesTheDatabaseAndServicesInYAMLAndJSON(t *testing.T) {
	yamlBody := "schema: 2\nproject:\n  name: acme-backend\nstacks: [go-srv]\nmodules: []\n" +
		"database:\n  name: ledger\n  user: ledger_rw\nservices:\n  postgres: acme-db\n"
	jsonBody := `{"schema":2,"project":{"name":"acme-backend"},"stacks":["go-srv"],"modules":[],` +
		`"database":{"name":"ledger","user":"ledger_rw"},"services":{"postgres":"acme-db"}}`
	for name, body := range map[string]string{"selection.yaml": yamlBody, "selection.json": jsonBody} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			doc := infraDryRunJSON(t, "-f", path)
			for slot, value := range map[string]string{"database": "ledger", "dbUser": "ledger_rw", "service.postgres": "acme-db"} {
				if doc.Names[slot] != value {
					t.Errorf("names[%s] = %q, want %q", slot, doc.Names[slot], value)
				}
			}
		})
	}
}

func TestAFlagReplacesOnlyItsOwnKeyOfTheFile(t *testing.T) {
	body := `{"schema":2,"project":{"name":"acme-backend"},"stacks":["go-srv"],"modules":["identity"],` +
		`"database":{"name":"ledger","user":"ledger_rw"},"services":{"postgres":"acme-db","kratos":"acme-auth"}}`
	path := filepath.Join(t.TempDir(), "selection.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := infraDryRunJSON(t, "-f", path, "--db-user", "ops", "--service", "postgres=acme-pg")
	want := map[string]string{
		"database": "ledger", "dbUser": "ops", "service.postgres": "acme-pg", "service.kratos": "acme-auth",
	}
	for slot, value := range want {
		if doc.Names[slot] != value {
			t.Errorf("names[%s] = %q, want %q", slot, doc.Names[slot], value)
		}
	}
	if string(doc.Recorded) == "" || !strings.Contains(string(doc.Recorded), `"database":{"name":"ledger","user":"ops"}`) {
		t.Errorf("recorded = %s", doc.Recorded)
	}
	// The input file is read once per run: a second run sees the same file values.
	again := infraDryRunJSON(t, "-f", path)
	if again.Names["dbUser"] != "ledger_rw" {
		t.Errorf("without the flag the file value must win, got %q", again.Names["dbUser"])
	}
}

func TestInfrastructureKeysUnderSchema1AreRefused(t *testing.T) {
	body := `{"schema":1,"project":{"name":"acme"},"stacks":["go-srv"],"modules":[],"database":{"name":"ledger"}}`
	path := filepath.Join(t.TempDir(), "selection.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := infraDryRun(t, "-f", path, "--format", "json")
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 1 || found[0]["message"] != "database needs schema 2" {
		t.Fatalf("exit %d issues %v", code, found)
	}
}

func TestTheDatabaseGrammarIsPGIdentWithTheUnderscoreExample(t *testing.T) {
	code, stdout, _ := infraDryRun(t, "--name", "acme", "--db-name", "Ledger-1", "--db-user", "1ledger", "--format", "json")
	found := issueArray(t, stdout)
	if code != exitIssues {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if len(found) != 2 {
		t.Fatalf("issues = %v", found)
	}
	for _, issue := range found {
		if issue["code"] != "NAME_INVALID" || issue["hint"] != "example: acme_api" || !strings.Contains(issue["message"], "underscores") {
			t.Errorf("issue = %v", issue)
		}
	}
}

func TestUnusedUnknownAndComponentServiceKeysAreAllReported(t *testing.T) {
	code, stdout, _ := infraDryRun(t, "--name", "acme", "--stacks", "go-srv", "--format", "json",
		"--service", "kratos=acme-auth", "--service", "kratsos=acme-auth", "--service", "go-srv=acme-auth")
	found := issueArray(t, stdout)
	if code != exitIssues {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	byMessage := map[string]map[string]string{}
	for _, issue := range found {
		byMessage[issue["message"]] = issue
	}
	notLive := `/services/kratos: no selected module delivers a "kratos" service (identity is not selected)`
	unknown := `/services/kratsos: no service key "kratsos"; keys: kratos, kratos-migrate, postgres, postgres-init`
	component := `/services/go-srv: no service key "go-srv"; keys: kratos, kratos-migrate, postgres, postgres-init`
	if len(found) != 3 {
		t.Fatalf("issues = %v", found)
	}
	for message, hint := range map[string]string{
		notLive: "", unknown: `did you mean "kratos"?`,
		component: "a component's service is its component name: --component go-srv=acme-auth",
	} {
		issue, has := byMessage[message]
		if !has {
			t.Errorf("no issue %q in %v", message, found)
			continue
		}
		if issue["code"] != "NAME_OVERRIDE_UNUSED" || issue["hint"] != hint {
			t.Errorf("issue = %v, want hint %q", issue, hint)
		}
	}
}

func TestADatabaseOverrideIsUsedWithoutIdentity(t *testing.T) {
	doc := infraDryRunJSON(t, "--name", "acme", "--stacks", "go-srv", "--db-name", "ledger")
	if doc.Names["database"] != "ledger" {
		t.Fatalf("names = %v", doc.Names)
	}
	if _, has := doc.Names["service.kratos"]; has {
		t.Error("kratos service is live without identity")
	}
}

func TestAServiceNameLongerThanTheLimitDependsOnTheLiveServices(t *testing.T) {
	name := strings.Repeat("a", 49)
	code, stdout, _ := infraDryRun(t, "--name", name, "--stacks", "go-srv", "--modules", "identity", "--format", "json")
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 1 || found[0]["code"] != "NAME_TOO_LONG" {
		t.Fatalf("exit %d issues %v", code, found)
	}
	wantMessage := `derived compose service "` + name + `-kratos-migrate" is 64 characters (max 63)`
	if found[0]["message"] != wantMessage || !strings.HasSuffix(found[0]["path"], "#/project/name") ||
		found[0]["hint"] != "shorten the name to 48 characters or fewer, or set services.kratos-migrate" {
		t.Errorf("issue = %v", found[0])
	}
	code, _, stderr := infraDryRun(t, "--name", name, "--stacks", "go-srv")
	if code != exitOK {
		t.Fatalf("a 49-byte name without identity must pass: exit %d: %s", code, stderr)
	}
	override := infraDryRunJSON(t, "--name", name, "--stacks", "go-srv", "--modules", "identity", "--service", "kratos-migrate=short-migrate")
	if override.Names["service.kratos-migrate"] != "short-migrate" {
		t.Errorf("names = %v", override.Names)
	}
}

func TestServiceConflictsNameEveryField(t *testing.T) {
	code, stdout, _ := infraDryRun(t, "--name", "acme", "--stacks", "go-srv", "--format", "json",
		"--service", "postgres=acme-db", "--component", "go-srv=acme-db")
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 2 {
		t.Fatalf("exit %d issues %v", code, found)
	}
	for _, issue := range found {
		if issue["code"] != "NAME_CONFLICT" || issue["message"] != `component go-srv and service postgres both resolve to "acme-db"` {
			t.Errorf("issue = %v", issue)
		}
	}
	paths := found[0]["path"] + " " + found[1]["path"]
	if !strings.Contains(paths, "flags#/components/0") || !strings.Contains(paths, "flags#/services/postgres") {
		t.Errorf("paths = %s", paths)
	}
}

func TestTwoServicesResolvingToOneNameConflict(t *testing.T) {
	code, stdout, _ := infraDryRun(t, "--name", "acme", "--stacks", "go-srv", "--format", "json",
		"--modules", "identity", "--service", "postgres=acme-db", "--service", "postgres-init=acme-db")
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 2 {
		t.Fatalf("exit %d issues %v", code, found)
	}
	for _, issue := range found {
		if issue["code"] != "NAME_CONFLICT" || issue["message"] != `service postgres and service postgres-init both resolve to "acme-db"` {
			t.Errorf("issue = %v", issue)
		}
	}
	paths := found[0]["path"] + " " + found[1]["path"]
	if !strings.Contains(paths, "flags#/services/postgres ") && !strings.HasSuffix(paths, "flags#/services/postgres") {
		t.Errorf("paths = %s", paths)
	}
	if !strings.Contains(paths, "flags#/services/postgres-init") {
		t.Errorf("paths = %s", paths)
	}
}

func TestADefaultServiceNameConflictPointsAtTheProjectName(t *testing.T) {
	code, stdout, _ := infraDryRun(t, "--name", "acme", "--stacks", "go-srv", "--format", "json",
		"--component", "go-srv=acme-postgres")
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 2 {
		t.Fatalf("exit %d issues %v", code, found)
	}
	paths := found[0]["path"] + " " + found[1]["path"]
	if !strings.Contains(paths, "flags#/components/0") || !strings.Contains(paths, "#/project/name") {
		t.Errorf("paths = %s", paths)
	}
}

func TestADatabaseMayEqualItsUser(t *testing.T) {
	doc := infraDryRunJSON(t, "--name", "acme", "--stacks", "go-srv", "--db-name", "shared", "--db-user", "shared")
	if doc.Names["database"] != "shared" || doc.Names["dbUser"] != "shared" {
		t.Fatalf("names = %v", doc.Names)
	}
}

func TestDryRunTextListsTheDatabaseAndServices(t *testing.T) {
	code, stdout, stderr := infraDryRun(t, "--name", "acme", "--stacks", "go-srv,ng-web", "--all-modules")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		"Database", "acme (user acme)", "Services",
		"acme-postgres, acme-postgres-init, acme-kratos-migrate, acme-kratos",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

func TestDryRunSelectionIsValidInputWithTheLiveServices(t *testing.T) {
	doc := infraDryRunJSON(t, "--name", "acme", "--stacks", "go-srv", "--modules", "identity")
	var resolved manifest.Selection
	if err := json.Unmarshal(doc.Selection, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Database == nil || resolved.Database.Name != "acme" || resolved.Database.User != "acme" {
		t.Fatalf("database = %+v", resolved.Database)
	}
	if len(resolved.Services) != 4 || resolved.Services["postgres"] != "acme-postgres" {
		t.Fatalf("services = %v", resolved.Services)
	}
	path := filepath.Join(t.TempDir(), "resolved.json")
	if err := os.WriteFile(path, doc.Selection, 0o600); err != nil {
		t.Fatal(err)
	}
	again := infraDryRunJSON(t, "-f", path)
	for slot, value := range doc.Names {
		if again.Names[slot] != value {
			t.Errorf("replay names[%s] = %q, want %q", slot, again.Names[slot], value)
		}
	}
}

func TestServiceFlagProblemsAreAllListed(t *testing.T) {
	code, _, stderr := execute(t, "generate", "--service", "postgres", "--service", "=x", "--service", "kratos=",
		"--service", "postgres=a", "--service", "postgres=b")
	if code != exitUsage {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		`--service: invalid value: "postgres" needs KEY=NAME`,
		`--service: invalid value: "=x" needs a key before =NAME`,
		`--service: invalid value: "kratos=" needs a name after =`,
		`--service postgres=b: service "postgres" is given twice`,
		"5 problems.",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestParseService(t *testing.T) {
	good := map[string]manifest.KeyValue{
		"postgres=acme-db": {Key: "postgres", Value: "acme-db"},
		"a=b=c":            {Key: "a", Value: "b=c"},
		"Bad Key=Not_Ok":   {Key: "Bad Key", Value: "Not_Ok"},
	}
	for value, want := range good {
		got, err := parseService(value)
		if err != nil || got != want {
			t.Errorf("parseService(%q) = %+v %v, want %+v", value, got, err, want)
		}
	}
	for _, value := range []string{"", "postgres", "=x", "postgres="} {
		if _, err := parseService(value); err == nil {
			t.Errorf("parseService(%q) accepted", value)
		}
	}
}

func FuzzServiceFlag(f *testing.F) {
	for _, seed := range []string{"postgres=acme-db", "", "=", "a", "a=", "=b", "a=b=c", "\x00=\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		pair, err := parseService(value)
		if err != nil {
			return
		}
		if pair.Key == "" || pair.Value == "" {
			t.Fatalf("accepted %q without a key and a name: %+v", value, pair)
		}
		if got := pair.Key + "=" + pair.Value; got != value {
			t.Fatalf("accepted %q does not round-trip: %q", value, got)
		}
	})
}

// randomProjectName returns a valid project name of 2 to 48 bytes.
func randomProjectName(random *rand.Rand) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	length := 2 + random.Intn(47)
	label := make([]byte, length)
	for i := range label {
		label[i] = letters[random.Intn(len(letters))]
		isInner := i > 0 && i < length-1
		if isInner && random.Intn(6) == 0 && label[i-1] != '-' {
			label[i] = '-'
		}
	}
	return string(label)
}

// Property: for any valid project name, every live service is <name>-<key>,
// the database is the snake-cased name and the delivered compose holds no
// reference service key as a service line.
func TestPropertyInfrastructureNamesFollowTheProject(t *testing.T) {
	root, catalogPath := realCatalog(t)
	random := rand.New(rand.NewSource(7))
	for i := 0; i < 8; i++ {
		name := randomProjectName(random)
		out := filepath.Join(t.TempDir(), "out")
		code, _, stderr := execute(t, "generate", out, "--root", root, "--catalog", catalogPath, "--name", name,
			"--go-module", "example.com/acme", "--stacks", "go-srv", "--all-modules")
		if code != exitOK {
			t.Fatalf("name %q: exit %d: %s", name, code, stderr)
		}
		data, err := os.ReadFile(filepath.Join(out, "initializer.lock.json"))
		if err != nil {
			t.Fatal(err)
		}
		var lock struct {
			Names map[string]string `json:"names"`
		}
		if err := json.Unmarshal(data, &lock); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"postgres", "postgres-init", "kratos-migrate", "kratos"} {
			if lock.Names["service."+key] != name+"-"+key {
				t.Errorf("name %q: service.%s = %q", name, key, lock.Names["service."+key])
			}
		}
		snake := strings.ReplaceAll(name, "-", "_")
		if lock.Names["database"] != snake || lock.Names["dbUser"] != snake {
			t.Errorf("name %q: database %q user %q", name, lock.Names["database"], lock.Names["dbUser"])
		}
		compose, err := os.ReadFile(filepath.Join(out, ".deploy", "local", "docker-compose.yml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"postgres", "postgres-init", "kratos-migrate", "kratos"} {
			if strings.Contains(string(compose), "\n  "+key+":\n") {
				t.Errorf("name %q: the compose file keeps the service line %q", name, key)
			}
		}
	}
}
