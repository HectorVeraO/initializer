package versions

import (
	"strings"
	"testing"
)

func FuzzBuildComponent(f *testing.F) {
	f.Add(`{"module":"core","version":"0.1.0","files":["go-srv/",{"path":"a","target":"b"}],"exclude":["x/"]}`)
	f.Add(`{"files":[5,null,"..", "a\\b"]}`)
	f.Add(`[]`)
	f.Fuzz(func(t *testing.T, raw string) {
		for _, loc := range locations {
			component, usable, _ := buildComponent(loc, "dir", "p/"+loc.manifestName, []byte(raw))
			if !usable {
				continue
			}
			if !componentName.MatchString(strings.TrimPrefix(strings.TrimPrefix(component.Name, "spec-"), loc.stack+"-")) {
				t.Fatalf("unsafe name %q", component.Name)
			}
			for _, entry := range append(component.Paths, component.Exclude...) {
				if strings.Contains(entry, `\`) || strings.HasPrefix(entry, "/") || strings.Contains("/"+entry+"/", "/../") {
					t.Fatalf("bad entry %q", entry)
				}
			}
		}
	})
}
