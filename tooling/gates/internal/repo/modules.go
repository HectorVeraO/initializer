package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// GatbaseDir is the repo-relative directory that holds one directory per module.
const GatbaseDir = "go-srv/internal/gatbase"

// ManifestName is the file that registers a module.
const ManifestName = "module.manifest.json"

// ModuleDir is one directory under go-srv/internal/gatbase and what its
// module.manifest.json says. Only the fields the gates read are decoded.
type ModuleDir struct {
	// Dir is the directory name under GatbaseDir.
	Dir string
	// ManifestPath is repo-relative with forward slashes.
	ManifestPath string
	// HasManifest is false when the file does not exist.
	HasManifest bool
	// ManifestError is set when the file exists but is not usable JSON of the expected shape.
	ManifestError string
	// Module is the catalog id from the manifest.
	Module string
	// TablePrefixes is the manifest's tablePrefixes, verbatim.
	TablePrefixes []string
}

type manifestFields struct {
	Module        string   `json:"module"`
	TablePrefixes []string `json:"tablePrefixes"`
}

// DiscoverModules lists the directories under <root>/go-srv/internal/gatbase,
// sorted by name, with their manifests decoded. A missing gatbase directory
// yields no modules.
func DiscoverModules(root string) ([]ModuleDir, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(GatbaseDir)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", GatbaseDir, err)
	}
	var modules []ModuleDir
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		module := ModuleDir{
			Dir:          entry.Name(),
			ManifestPath: path.Join(GatbaseDir, entry.Name(), ManifestName),
		}
		raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(module.ManifestPath)))
		switch {
		case os.IsNotExist(readErr):
		case readErr != nil:
			module.HasManifest = true
			module.ManifestError = readErr.Error()
		default:
			module.HasManifest = true
			var fields manifestFields
			decodeErr := json.Unmarshal(raw, &fields)
			if decodeErr != nil {
				module.ManifestError = decodeErr.Error()
			} else {
				module.Module = fields.Module
				module.TablePrefixes = fields.TablePrefixes
			}
		}
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Dir < modules[j].Dir })
	return modules, nil
}
