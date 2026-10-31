package catalog

import "testing"

const sharedPostgres = `{"name": "postgres", "offset": 32, "stride": 10}`

func TestASharedPortNeedsNoStrideAndIsRecordedShared(t *testing.T) {
	repo := portsRepo(t)
	edit(t, repo, coreManifest, sharedPostgres, `{"name": "postgres", "offset": 32, "shared": true}`)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	found := false
	for _, stack := range built.Stacks {
		for _, module := range stack.Modules {
			for _, port := range module.Ports {
				if port.Name == "postgres" {
					found = port.Shared && port.Stride == 0
				}
			}
		}
	}
	if !found {
		t.Error("the catalog does not record the shared postgres port")
	}
	encoded, err := Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	if _, decodeIssues := Decode(encoded); len(decodeIssues) != 0 {
		t.Fatalf("a catalog with a shared port does not decode: %v", decodeIssues)
	}
}

func TestASharedPortWithAStrideIsRefused(t *testing.T) {
	repo := portsRepo(t)
	edit(t, repo, coreManifest, sharedPostgres, `{"name": "postgres", "offset": 32, "shared": true, "stride": 10}`)
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues, "PORT_STRIDE_INVALID")
}

func TestAPortWithoutAStrideIsRefusedUnlessItIsShared(t *testing.T) {
	repo := portsRepo(t)
	edit(t, repo, coreManifest, sharedPostgres, `{"name": "postgres", "offset": 32}`)
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues, "PORT_STRIDE_INVALID")
}

// A token of a shared port means the same host port for every instance, so it
// may sit outside every run; a token of a per-component port may not.
func TestASharedPortTokenMaySitOutsideEveryRun(t *testing.T) {
	outside := "name: fixture-app\n\nservices: {}\n10132\n"
	repo := portsRepo(t)
	put(repo, composeFile, outside)
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues, "PORT_UNSCOPED")

	shared := portsRepo(t)
	edit(t, shared, coreManifest, sharedPostgres, `{"name": "postgres", "offset": 32, "shared": true}`)
	put(shared, composeFile, outside)
	_, issues = buildRepo(t, shared)
	assertCodes(t, issues)
}
