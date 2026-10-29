package manifest_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

func infraFile(t *testing.T, schema int, extra map[string]any) []byte {
	t.Helper()
	return mutateFixture(t, "selection.valid.json", func(m map[string]any) {
		m["schema"] = schema
		for key, value := range extra {
			m[key] = value
		}
	})
}

func TestDecodeReadsTheDatabaseAndServices(t *testing.T) {
	data := infraFile(t, 2, map[string]any{
		"database": map[string]any{"name": "ledger", "user": "ledger_rw"},
		"services": map[string]any{"postgres": "acme-db"},
	})
	selection, issues := decodeFile(t, selectionPath, data)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	if selection.Database == nil || *selection.Database != (manifest.Database{Name: "ledger", User: "ledger_rw"}) {
		t.Errorf("database = %+v", selection.Database)
	}
	if !reflect.DeepEqual(selection.Services, map[string]string{"postgres": "acme-db"}) {
		t.Errorf("services = %v", selection.Services)
	}
}

func TestDecodeRefusesInfrastructureKeysAtSchema1(t *testing.T) {
	data := infraFile(t, 1, map[string]any{"database": map[string]any{"name": "ledger"}, "services": map[string]any{"postgres": "acme-db"}})
	_, issues := decodeFile(t, selectionPath, data)
	if len(issues) != 2 {
		t.Fatalf("issues: %v", issues)
	}
	messages := []string{}
	for _, found := range issues {
		messages = append(messages, found.Message)
	}
	sort.Strings(messages)
	want := []string{"database needs schema 2", "services needs schema 2"}
	if !reflect.DeepEqual(messages, want) {
		t.Errorf("messages = %v, want %v", messages, want)
	}
}

func TestDecodeChecksTheInfrastructureGrammars(t *testing.T) {
	data := infraFile(t, 2, map[string]any{
		"database": map[string]any{"name": "Ledger", "user": "1user"},
		"services": map[string]any{"postgres": "Acme_DB"},
	})
	_, issues := decodeFile(t, selectionPath, data)
	if len(issues) != 3 {
		t.Fatalf("issues: %v", issues)
	}
	for _, found := range issues {
		if found.Code != manifest.CodeNameInvalid {
			t.Errorf("issue = %+v", found)
		}
	}
}

func TestMergeReplacesOneKeyAndLeavesTheFileInputUntouched(t *testing.T) {
	data := infraFile(t, 2, map[string]any{
		"database": map[string]any{"name": "ledger", "user": "ledger_rw"},
		"services": map[string]any{"postgres": "acme-db", "kratos": "acme-auth"},
	})
	file, issues := decodeInputFile(selectionPath, data)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	flags, flagIssues := manifest.FlagsInput(manifest.Flags{
		DBUser: "ops", Has: manifest.FlagPresence{DBUser: true},
		Services: []manifest.KeyValue{{Key: "postgres", Value: "acme-pg"}},
	})
	if len(flagIssues) != 0 {
		t.Fatalf("flag issues: %v", flagIssues)
	}
	merged, mergeIssues := manifest.Merge(&file, flags)
	if len(mergeIssues) != 0 {
		t.Fatalf("merge issues: %v", mergeIssues)
	}
	if *merged.Selection.Database != (manifest.Database{Name: "ledger", User: "ops"}) {
		t.Errorf("database = %+v", merged.Selection.Database)
	}
	wantServices := map[string]string{"postgres": "acme-pg", "kratos": "acme-auth"}
	if !reflect.DeepEqual(merged.Selection.Services, wantServices) {
		t.Errorf("services = %v", merged.Selection.Services)
	}
	if file.Selection.Database.User != "ledger_rw" || file.Selection.Services["postgres"] != "acme-db" {
		t.Errorf("the merge changed the file input: %+v %v", file.Selection.Database, file.Selection.Services)
	}
	if origin := merged.Origins["/database/user"]; origin.Arg != "--db-user" {
		t.Errorf("origin = %+v", origin)
	}
}

func TestFlagsInputPromotesToSchema2AndRecordsAsGiven(t *testing.T) {
	flags, issues := manifest.FlagsInput(manifest.Flags{
		Name: "acme", Has: manifest.FlagPresence{Name: true, Stacks: true},
		Stacks:   []string{"go-srv"},
		Services: []manifest.KeyValue{{Key: "postgres", Value: "acme-db"}},
	})
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	merged, _ := manifest.Merge(nil, flags)
	recorded := manifest.RecordedSelection(merged, []string{"core"})
	if recorded.Schema != 2 || recorded.Database != nil || recorded.Services["postgres"] != "acme-db" {
		t.Fatalf("recorded = %+v", recorded)
	}
	plain := manifest.RecordedSelection(mustFlagsMerged(t), []string{"core"})
	if plain.Schema != 1 || plain.Database != nil || plain.Services != nil {
		t.Fatalf("a run without infrastructure flags must record schema 1: %+v", plain)
	}
}

func mustFlagsMerged(t *testing.T) manifest.Input {
	t.Helper()
	flags, issues := manifest.FlagsInput(manifest.Flags{
		Name: "acme", Has: manifest.FlagPresence{Name: true, Stacks: true}, Stacks: []string{"go-srv"},
	})
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	merged, _ := manifest.Merge(nil, flags)
	return merged
}
