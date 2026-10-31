package names_test

import (
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

func TestTheServerAndKratosNamesOfASingleProjectAreTheBackendsAndKratos(t *testing.T) {
	got := names.Resolved{Project: "acme", Database: "shop", DBUser: "shop_owner", Components: map[string]string{"go-srv": "acme-srv"}}.Slots()
	want := map[string]string{
		"serverDatabase": "shop", "serverUser": "shop_owner", "kratosDatabase": "kratos", "kratosUser": "kratos",
	}
	for slot, value := range want {
		if got[slot] != value {
			t.Errorf("%s = %q, want %q", slot, got[slot], value)
		}
	}
}

func TestTheServerOfAMultiProjectIsPostgresAndEachKratosIsNamedAfterItsBackend(t *testing.T) {
	got := names.Resolved{
		Project: "acme", Database: "shop", DBUser: "shop_owner", Multi: true,
		Components: map[string]string{"go-srv": "acme-srv-first"},
	}.Slots()
	want := map[string]string{
		"serverDatabase": "postgres", "serverUser": "postgres", "kratosDatabase": "shop_kratos", "kratosUser": "shop_owner_kratos",
	}
	for slot, value := range want {
		if got[slot] != value {
			t.Errorf("%s = %q, want %q", slot, got[slot], value)
		}
	}
}

func TestWithoutADatabaseThereAreNoKratosNames(t *testing.T) {
	got := names.Resolved{Project: "acme", Components: map[string]string{"go-srv": "acme-srv"}}.Slots()
	for _, slot := range []string{"serverDatabase", "serverUser", "kratosDatabase", "kratosUser"} {
		if value, has := got[slot]; has {
			t.Errorf("%s = %q without a database", slot, value)
		}
	}
}

func TestTheServerAndKratosSlotsAreDerivedAndNeverRecorded(t *testing.T) {
	for _, slot := range []string{"serverDatabase", "serverUser", "kratosDatabase", "kratosUser"} {
		if !names.IsSlot(slot, []string{"go-srv"}) {
			t.Errorf("%s is not a slot", slot)
		}
		if names.IsRecorded(slot) {
			t.Errorf("%s is recorded", slot)
		}
	}
}

func TestOnlyTheServerAndTheSharedServicesAreProjectLevel(t *testing.T) {
	for slot, want := range map[string]bool{
		"serverDatabase": true, "serverUser": true, "service.postgres": true, "service.postgres-init": true,
		"project": true, "envPrefix": true,
		"service.kratos": false, "service.kratos-migrate": false, "database": false, "dbUser": false,
		"kratosDatabase": false, "kratosUser": false,
	} {
		if got := names.IsProjectLevel(slot); got != want {
			t.Errorf("IsProjectLevel(%s) = %v, want %v", slot, got, want)
		}
	}
	if !names.IsSharedService("postgres") || !names.IsSharedService("postgres-init") || names.IsSharedService("kratos") {
		t.Error("the shared services are postgres and postgres-init")
	}
}
