package plan_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// slotOf returns a slot of the component's instance.
func slotOf(t *testing.T, p plan.Plan, component, slot string) string {
	t.Helper()
	value, found := byName(t, p, component).Slots[slot]
	if !found {
		t.Fatalf("%s has no slot %s", component, slot)
	}
	return value
}

func TestBackendsShareTheServerNamesAndEachHasItsOwnDatabaseAndKratos(t *testing.T) {
	p := mustResolve(t, repoCatalog(t), modulesSelection(3, 0, "identity"))
	for _, name := range []string{"b1", "b2", "b3"} {
		for slot, want := range map[string]string{
			"service.postgres": "acme-postgres", "service.postgres-init": "acme-postgres-init",
			"serverUser": "postgres", "serverDatabase": "postgres",
			"database": name, "dbUser": name, "kratosDatabase": name + "_kratos", "kratosUser": name + "_kratos",
			"service.kratos": name + "-kratos", "service.kratos-migrate": name + "-kratos-migrate",
		} {
			if got := slotOf(t, p, name, slot); got != want {
				t.Errorf("%s: %s = %q, want %q", name, slot, got, want)
			}
		}
		services := byName(t, p, name).Services
		if services["postgres"] != "acme-postgres" || services["postgres-init"] != "acme-postgres-init" {
			t.Errorf("%s services = %v", name, services)
		}
	}
}

func TestASingleBackendIsItsOwnServerAndKeepsTheKratosNames(t *testing.T) {
	p := mustResolve(t, repoCatalog(t), modulesSelection(1, 0, "identity"))
	// A single project derives the database and user from the project name.
	for slot, want := range map[string]string{
		"serverUser": "acme", "serverDatabase": "acme", "database": "acme", "dbUser": "acme",
		"kratosDatabase": "kratos", "kratosUser": "kratos", "service.postgres": "acme-postgres",
	} {
		if got := slotOf(t, p, "b1", slot); got != want {
			t.Errorf("%s = %q, want %q", slot, got, want)
		}
	}
}

func TestTheProjectNamesTheSharedServicesAndEachBackendItsKratosServices(t *testing.T) {
	sel := modulesSelection(2, 0, "identity")
	sel.Services = map[string]string{"postgres": "acme-db", "postgres-init": "acme-db-init"}
	sel.Components[1].Services = map[string]string{"kratos": "orders-id"}
	p := mustResolve(t, repoCatalog(t), sel)
	for _, name := range []string{"b1", "b2"} {
		if got := slotOf(t, p, name, "service.postgres"); got != "acme-db" {
			t.Errorf("%s postgres service = %q", name, got)
		}
		if got := slotOf(t, p, name, "service.postgres-init"); got != "acme-db-init" {
			t.Errorf("%s init service = %q", name, got)
		}
	}
	if got := slotOf(t, p, "b2", "service.kratos"); got != "orders-id" {
		t.Errorf("b2 kratos service = %q", got)
	}
	if got := slotOf(t, p, "b1", "service.kratos"); got != "b1-kratos" {
		t.Errorf("b1 kratos service = %q", got)
	}
}

func TestASharedServiceSetOnOneBackendIsRefused(t *testing.T) {
	for _, key := range []string{"postgres", "postgres-init"} {
		sel := modulesSelection(2, 0, "identity")
		sel.Components[1].Services = map[string]string{key: "orders-db"}
		_, issues := plan.Resolve(repoCatalog(t), sel)
		if len(issues) != 1 || issues[0].Code != plan.CodeNameOverrideUnused ||
			issues[0].Path != "selection#/components/1/services/"+key || !strings.Contains(issues[0].Hint, "--service "+key+"=") {
			t.Errorf("%s: issues = %+v", key, issues)
		}
	}
}

func codesAndPaths(issues []string) string {
	sort.Strings(issues)
	return strings.Join(issues, "; ")
}

func conflictsOf(t *testing.T, sel manifest.Selection) string {
	t.Helper()
	_, issues := plan.Resolve(repoCatalog(t), sel)
	var found []string
	for _, issue := range issues {
		found = append(found, issue.Code+" "+issue.Path)
	}
	return codesAndPaths(found)
}

func TestTwoBackendsMayNotShareADatabaseOrAUserOnTheServer(t *testing.T) {
	sel := modulesSelection(2, 0)
	sel.Components[0].Database = &manifest.Database{Name: "orders"}
	sel.Components[1].Database = &manifest.Database{Name: "orders"}
	want := "NAME_CONFLICT selection#/components/0/database/name; NAME_CONFLICT selection#/components/1/database/name"
	if got := conflictsOf(t, sel); got != want {
		t.Errorf("databases: %s", got)
	}
	users := modulesSelection(2, 0)
	users.Components[0].Database = &manifest.Database{User: "owner"}
	users.Components[1].Database = &manifest.Database{User: "owner"}
	want = "NAME_CONFLICT selection#/components/0/database/user; NAME_CONFLICT selection#/components/1/database/user"
	if got := conflictsOf(t, users); got != want {
		t.Errorf("users: %s", got)
	}
}

func TestABackendMayNotTakeTheServersOwnName(t *testing.T) {
	sel := modulesSelection(2, 0)
	sel.Components[1].Database = &manifest.Database{Name: "postgres", User: "postgres"}
	want := "NAME_CONFLICT selection#/components/1/database/name; NAME_CONFLICT selection#/components/1/database/user"
	if got := conflictsOf(t, sel); got != want {
		t.Errorf("got %s", got)
	}
}

func TestAKratosDatabaseMayNotTakeABackendsName(t *testing.T) {
	sel := modulesSelection(2, 0, "identity")
	// b2 is named like the Kratos database of b1.
	sel.Components[1].Database = &manifest.Database{Name: "b1_kratos", User: "b1_kratos"}
	want := "NAME_CONFLICT selection#/components/0/name; NAME_CONFLICT selection#/components/0/name; " +
		"NAME_CONFLICT selection#/components/1/database/name; NAME_CONFLICT selection#/components/1/database/user"
	if got := conflictsOf(t, sel); got != want {
		t.Errorf("got %s", got)
	}
}

func TestADerivedKratosNamePastTheIdentifierLimitIsTooLong(t *testing.T) {
	sel := modulesSelection(2, 0, "identity")
	sel.Components[1].Database = &manifest.Database{Name: strings.Repeat("d", 60), User: strings.Repeat("u", 60)}
	_, issues := plan.Resolve(repoCatalog(t), sel)
	var found []string
	for _, issue := range issues {
		found = append(found, issue.Code+" "+issue.Path)
	}
	want := "NAME_TOO_LONG selection#/components/1/database/name; NAME_TOO_LONG selection#/components/1/database/user"
	if got := codesAndPaths(found); got != want {
		t.Errorf("got %s (issues %+v)", got, issues)
	}
	for _, issue := range issues {
		if !strings.Contains(issue.Message, "Kratos database") {
			t.Errorf("message = %q", issue.Message)
		}
	}
}

func TestEveryBackendPlansADistinctDatabaseAndUserUpToTheLimit(t *testing.T) {
	cat := repoCatalog(t)
	for _, n := range []int{2, 9, 32} {
		p := mustResolve(t, cat, modulesSelection(n, 0, "identity"))
		databases, users := map[string]string{}, map[string]string{}
		for i := 1; i <= n; i++ {
			name := fmt.Sprintf("b%d", i)
			for _, slot := range []string{"database", "kratosDatabase"} {
				value := slotOf(t, p, name, slot)
				if owner, taken := databases[value]; taken {
					t.Errorf("%d backends: %s of %s repeats %s", n, slot, name, owner)
				}
				databases[value] = slot + " of " + name
			}
			for _, slot := range []string{"dbUser", "kratosUser"} {
				value := slotOf(t, p, name, slot)
				if owner, taken := users[value]; taken {
					t.Errorf("%d backends: %s of %s repeats %s", n, slot, name, owner)
				}
				users[value] = slot + " of " + name
			}
		}
	}
}
