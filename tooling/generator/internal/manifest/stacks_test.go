package manifest_test

import (
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

func TestStacksReturnsACopy(t *testing.T) {
	stacks := manifest.Stacks()
	if len(stacks) != 2 || stacks[0].ID != "go-srv" || stacks[1].ID != "ng-web" {
		t.Fatalf("stacks = %+v", stacks)
	}
	stacks[0].ID = "mutated"
	stacks[0].Root = "mutated/"
	info, ok := manifest.LookupStack("go-srv")
	if !ok || info.Root != "go-srv/" {
		t.Fatalf("lookup after mutation = %+v %v", info, ok)
	}
	if _, ok := manifest.LookupStack("mutated"); ok {
		t.Fatal("mutation reached the table")
	}
}

func TestLookupStackRows(t *testing.T) {
	goSrv, _ := manifest.LookupStack("go-srv")
	if goSrv.Role != manifest.RoleBackend || !goSrv.GoModule || goSrv.Suffix != "srv" {
		t.Errorf("go-srv = %+v", goSrv)
	}
	ngWeb, _ := manifest.LookupStack("ng-web")
	if ngWeb.Role != manifest.RoleFrontend || ngWeb.GoModule || ngWeb.ModulesRoot != "src/app/platform/" {
		t.Errorf("ng-web = %+v", ngWeb)
	}
	if _, ok := manifest.LookupStack("rust-srv"); ok {
		t.Error("unknown stack found")
	}
}
