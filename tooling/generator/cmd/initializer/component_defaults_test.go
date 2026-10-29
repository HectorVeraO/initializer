package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// folderNames returns "stack=name" for every component of a generate document.
func folderNames(t *testing.T, stdout string) []string {
	t.Helper()
	var doc generateDocument
	decodeDocument(t, stdout, &doc)
	var out []string
	for _, component := range doc.Components {
		out = append(out, component.Stack+"="+component.Name)
	}
	return out
}

func TestDefaultFoldersFollowTheNameAndOverridesWin(t *testing.T) {
	long := strings.Repeat("a", 59)
	cases := []struct {
		name        string
		project     string
		extra       []string
		wantFolders string
	}{
		{"default", "acme", nil, "go-srv=acme-srv,ng-web=acme-web"},
		{"two-byte name", "ab", nil, "go-srv=ab-srv,ng-web=ab-web"},
		{"flag override", "acme", []string{"--component", "go-srv=acme-api"}, "go-srv=acme-api,ng-web=acme-web"},
		{"59 bytes", long, nil, "go-srv=" + long + "-srv,ng-web=" + long + "-web"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			args := e.args("--name", tc.project, "--go-module", "example.com/acme", "--stacks", "go-srv,ng-web", "--all-modules")
			code, stdout, stderr := executeJSON(t, append(args, tc.extra...)...)
			if code != exitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if got := strings.Join(folderNames(t, stdout), ","); got != tc.wantFolders {
				t.Fatalf("components = %s, want %s", got, tc.wantFolders)
			}
			files := tree(t, e.out)
			for _, entry := range strings.Split(tc.wantFolders, ",") {
				stack, folder, _ := strings.Cut(entry, "=")
				marker := "package.json"
				if stack == "go-srv" {
					marker = "go.mod"
				}
				if _, found := files[folder+"/"+marker]; !found {
					t.Errorf("folder %s was not delivered", folder)
				}
			}
			for path := range files {
				if strings.HasPrefix(path, "go-srv/") || strings.HasPrefix(path, "ng-web/") {
					t.Errorf("%s stayed under a stack id", path)
				}
			}
		})
	}
}

func TestAFileEntryNamesAComponentAndAFlagNamesTheOther(t *testing.T) {
	body := `{"schema":2,"project":{"name":"acme","goModule":"example.com/acme"},"stacks":["go-srv","ng-web"],"components":[{"stack":"go-srv","name":"acme-api"}]}`
	file := writeSelection(t, body)
	cases := map[string]struct {
		extra []string
		want  string
	}{
		"file entry":               {nil, "go-srv=acme-api ng-web=acme-web"},
		"flag for the other stack": {[]string{"--component", "ng-web=acme-ui"}, "go-srv=acme-api ng-web=acme-ui"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			code, stdout, stderr := executeJSON(t, append(e.args("-f", file), tc.extra...)...)
			if code != exitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if got := strings.Join(folderNames(t, stdout), " "); got != tc.want {
				t.Fatalf("components = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestReplayingARecordedSelectionRederivesTheDefaults(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	first := []string{"first", "--name", "acme", "--stacks", "go-srv,ng-web", "--modules", "parameters"}
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs(first...)...); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	recorded := filepath.Join(wd, "first", "initializer.selection.json")
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", recorded, "--name", "other", "second")...); code != exitOK {
		t.Fatalf("replay exit %d: %s", code, stderr)
	}
	second := filepath.Join(wd, "second")
	if got := folderOf(t, second, "go-srv"); got != "other-srv" {
		t.Errorf("backend folder = %q, want other-srv", got)
	}
	if got := folderOf(t, second, "ng-web"); got != "other-web" {
		t.Errorf("frontend folder = %q, want other-web", got)
	}
}

func TestADerivedFolderOverTheLimitIsRefusedAtTheName(t *testing.T) {
	e := newEnv(t)
	name := strings.Repeat("a", 60)
	args := e.args("--name", name, "--go-module", "example.com/acme", "--stacks", "go-srv,ng-web", "--all-modules")
	code, stdout, _ := executeJSON(t, args...)
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 2 {
		t.Fatalf("exit %d issues %v", code, found)
	}
	for _, got := range found {
		if got["code"] != "NAME_TOO_LONG" || !strings.HasSuffix(got["path"], "#/project/name") {
			t.Errorf("issue %v", got)
		}
	}
	if entries, err := os.ReadDir(e.out); err == nil && len(entries) > 0 {
		t.Errorf("nothing may be written, found %d entries", len(entries))
	}
}

func TestAnOverrideEqualToTheOtherDefaultConflicts(t *testing.T) {
	e := newEnv(t)
	args := e.args("--name", "acme", "--go-module", "example.com/acme", "--stacks", "go-srv,ng-web", "--component", "ng-web=acme-srv")
	code, stdout, _ := executeJSON(t, args...)
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 2 {
		t.Fatalf("exit %d issues %v", code, found)
	}
	for _, got := range found {
		if got["code"] != "NAME_CONFLICT" {
			t.Errorf("issue %v", got)
		}
	}
}

func TestASchema1FileGetsTheProjectNamedDefaults(t *testing.T) {
	e := newEnv(t)
	body := `{"schema":1,"project":{"name":"acme","goModule":"example.com/acme"},"stacks":["go-srv"],"modules":[]}`
	code, stdout, stderr := executeJSON(t, e.args("-f", writeSelection(t, body))...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := strings.Join(folderNames(t, stdout), ","); got != "go-srv=acme-srv" {
		t.Fatalf("components = %s", got)
	}
}
