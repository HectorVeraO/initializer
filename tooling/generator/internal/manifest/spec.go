package manifest

import (
	"fmt"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/semver"
)

// Spec is a decoded spec.manifest.json.
type Spec struct {
	Spec    string
	Version semver.Version
	TCKTags []string
	TCKEnv  map[string]string
}

var specKeys = []string{"schema", "spec", "version", "tckTags", "tckEnv"}

// DecodeSpec decodes and validates a spec manifest. path is the manifest file
// path used in issue paths; folder is the name of the spec folder, which must
// equal the manifest's spec id.
func DecodeSpec(path, folder string, data []byte) (Spec, []issue.Issue) {
	d := newDecoder(path, CodeManifestInvalid)
	var spec Spec
	root, ok := d.syntax(data)
	if !ok {
		return spec, d.result()
	}
	members, ok := d.object(root, "", specKeys)
	if !ok {
		return spec, d.result()
	}
	d.schema(members)
	if raw, ok := d.required(members, "", "spec"); ok {
		if id, ok := d.str(raw, "/spec"); ok {
			spec.Spec = id
			if !validID(id) {
				d.add("/spec", "must be a spec id of at most 31 characters")
			} else if id != folder {
				d.add("/spec", "must equal the name of the spec folder")
			}
		}
	}
	if raw, ok := d.required(members, "", "version"); ok {
		spec.Version, _ = d.version(raw, "/version")
	}
	if raw, ok := d.required(members, "", "tckTags"); ok {
		spec.TCKTags = d.tckTags(raw)
	}
	if raw, ok := d.required(members, "", "tckEnv"); ok {
		spec.TCKEnv = d.tckEnv(raw)
	}
	return spec, d.result()
}

func (d *decoder) tckTags(raw []byte) []string {
	values, valid, ok := d.strings(raw, "/tckTags")
	if !ok {
		return nil
	}
	tags := []string{}
	for i, value := range values {
		if !valid[i] {
			continue
		}
		if !tckTagTable[value] {
			d.add(fmt.Sprintf("/tckTags/%d", i), "must be one of the known TCK tags")
			valid[i] = false
			continue
		}
		tags = append(tags, value)
	}
	d.checkOrder("/tckTags", values, valid)
	return tags
}

func (d *decoder) tckEnv(raw []byte) map[string]string {
	members, ok := d.object(raw, "/tckEnv", nil)
	if !ok {
		return nil
	}
	env := map[string]string{}
	for key, value := range members {
		pointer := "/tckEnv" + issue.Pointer(key)
		if !envKeyPattern.MatchString(key) {
			d.add(pointer, "key must match ^[A-Z][A-Z0-9_]*$")
			continue
		}
		if text, ok := d.str(value, pointer); ok {
			env[key] = text
		}
	}
	return env
}
