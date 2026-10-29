# Generate a project

`initializer` is a command-line tool that creates a project from the module catalog of this repository: you pick stacks and modules, it copies only those, and `initializer.lock.json` records what it delivered.

## 1. Set up

```bash
git clone <repo-url> initializer
cd initializer
mise trust          # plus `mise trust .config/mise.toml` if mise reports an untrusted config
mise install        # installs the pinned tools, then builds tooling/generator/bin/initializer
initializer --help
```

Inside the clone, mise puts `tooling/generator/bin` on the `PATH`. To use the command from any folder, add one line to your own global mise config:

```toml
[env]
_.path = ["<clone>/tooling/generator/bin"]
```

The binary finds its sources by walking up from its own location, so it needs nothing else. After `git pull`, rebuild with `mise run cli`; without mise, run `go -C tooling/generator build -o bin/ ./cmd/initializer`. When the sources changed since the build, every command prints a `GENERATOR_STALE` warning on stderr with that hint.

## 2. Discover

```bash
initializer module ls
```

```text
MODULE          STACKS          REQUIRES   SPEC                  VERSION
core            go-srv,ng-web   -          core 0.1.0            0.1.0   (always installed)
api-keys        go-srv,ng-web   identity   api-keys 0.1.0        0.1.0
feature-flags   go-srv,ng-web   identity   feature-flags 0.1.0   0.1.0
identity        go-srv,ng-web   -          identity 0.1.0        0.1.0
parameters      go-srv,ng-web   identity   parameters 0.1.0      0.1.0
tenancy         go-srv,ng-web   identity   tenancy 0.1.0         0.1.0
```

```bash
initializer stack ls
```

```text
STACK    ROLE       SUFFIX   GO MODULE   MODULES
go-srv   backend    srv      yes         6
ng-web   frontend   web      no          6
```

| Module | Adds |
|---|---|
| `core` | Guard, envelope, telemetry, migrations, ports. Always installed. |
| `identity` | Kratos login, BFF session, accounts. |
| `tenancy` | Tenants, memberships, invitations. |
| `api-keys` | `X-API-Key` machine credentials. |
| `parameters` | Typed parameters with tenant overrides. |
| `feature-flags` | `flags.yaml` flags with overrides. |

A module you select pulls in the modules it requires: `tenancy` alone installs `identity` too. `core` is always installed.

## 3. Generate with flags

```bash
initializer generate acme --stacks go-srv,ng-web --modules identity,parameters
```

```text
Project acme generated in ./acme (533 files).
Next: cd acme && mise run up
```

stderr adds one `Sources:` line and a `note:` for each default it chose:

```text
note: project name "acme" comes from the folder; pass --name to set it
note: Go module defaults to example.com/acme; pass --go-module to set yours
```

Defaults when a flag is absent:

| Value | Default |
|---|---|
| Folder | the `DIR` argument, else `./<name>` |
| Project name | the folder's name, else `demo` |
| Stacks | the catalog's backend stack (`go-srv`) |
| Modules | none beyond `core` and what the chosen modules require; `--all-modules` installs every module |
| Go module | `example.com/<name>`; pass `--go-module` for yours |

The folder must be absent or empty; otherwise nothing is written and the command answers `OUTPUT_NOT_EMPTY`:

```text
error: ./acme: output directory is not empty [OUTPUT_NOT_EMPTY]
  hint: choose another folder: initializer generate <dir>
1 problem; nothing was written.
```

## 4. Generate from a file

A file holds the same choices as the flags. YAML:

```yaml
schema: 2
project:
  name: acme
  goModule: example.com/acme
stacks: [go-srv, ng-web]
modules: [identity, tenancy]
```

and its JSON twin:

```json
{
  "schema": 2,
  "project": { "name": "acme", "goModule": "example.com/acme" },
  "stacks": ["go-srv", "ng-web"],
  "modules": ["identity", "tenancy"]
}
```

```bash
initializer generate -f acme.yaml
```

The extension (`.yaml`, `.yml`, `.json`) picks the decoder; `-f -` reads standard input. Flags win over the file, the file wins over defaults:

```bash
initializer generate acme-103 -f acme.yaml --port-band 103
```

`--dry-run` prints the resolved plan and writes nothing:

```bash
initializer generate acme-dry -f acme.yaml --dry-run
```

```text
Project acme in ./acme-dry (dry run, nothing written)
COMPONENT   STACK    FOLDER      MODULES
acme-srv    go-srv   acme-srv/   core, identity, tenancy
acme-web    ng-web   acme-web/   core, identity, tenancy
Go module   example.com/acme
Database    acme (user acme)
Services    acme-postgres, acme-postgres-init, acme-kratos-migrate, acme-kratos
Ports       band 101
556 files
```

To turn a flag line into a starter file, keep the `selection` member of the dry run's JSON document:

```bash
initializer generate starter --stacks go-srv --modules parameters --dry-run --format json | jq .selection > starter.json
initializer generate starter -f starter.json
```

Every generated project carries the resolved choices as `initializer.selection.json` (with the port band when you set one), so `-f <project>/initializer.selection.json` regenerates it.

## 5. Names, database, services, display name, port band

| Flag | Sets | Default |
|---|---|---|
| `--name NAME` | Project name | the folder's name, else `demo` |
| `--display-name TEXT` | Brand text of the web console | the project name |
| `--component STACK=NAME` | Folder of that stack's component | `<name>-<suffix>` (`acme-srv`, `acme-web`) |
| `--db-name NAME`, `--db-user NAME` | Database and its user | `<name>` with `-` replaced by `_` |
| `--service KEY=NAME` | Rename one compose service | `<name>-<key>` |
| `--port-band NNN` | Host ports `1NNxx`, from 100 to 654 | `101` |

```bash
initializer generate shop --name shop --stacks go-srv,ng-web --display-name "Acme Shop" \
  --db-name shop_db --db-user shop_app --service postgres=shop-pg \
  --component go-srv=shop-api --port-band 103 --dry-run
```

```text
Project shop in ./shop (dry run, nothing written)
COMPONENT   STACK    FOLDER      MODULES
shop-api    go-srv   shop-api/   core
shop-web    ng-web   shop-web/   core
Go module   example.com/shop (default)
Database    shop_db (user shop_app)
Services    shop-pg
Ports       band 103
397 files
```

Names are lowercase letters, digits and inner hyphens, 2 to 63 characters, starting with a letter. Anything else answers `NAME_INVALID` with an example.

## 6. Components

Each stack produces one component folder. Without `--component` the folder is `<name>-<suffix>`. Two components of the same stack are not supported yet: a second `--component go-srv=...` answers `COMPONENT_MULTI_UNSUPPORTED` and nothing is written.

When a file lists components, a `--component STACK=NAME` flag must name one of them exactly; it never renames. For a file that lists `acme-core` for `go-srv`:

```bash
initializer generate m4 -f comp.yaml --component go-srv=acme-core
initializer generate m3 -f comp.yaml --component go-srv=other
```

The second line answers `COMPONENT_MERGE_AMBIGUOUS` (exit 1, nothing generated):

```text
error: --component go-srv=other: --component go-srv=other names no go-srv component of comp.yaml (acme-core) [COMPONENT_MERGE_AMBIGUOUS]
  hint: To add, remove or rename a component, edit comp.yaml
1 problem; nothing was written.
```

Add, remove or rename a component by editing the file.

## 7. Check

```bash
initializer verify acme
```

```text
No differences.
```

`verify` compares the files with the hashes in `initializer.lock.json`; `DIR` defaults to the working directory. After you edit a delivered file it lists the difference and exits 1 (`MODIFIED`, `UNTRACKED` or missing files). It does not compile the project. Build the backend from its folder, which the lock records (`acme-srv/` by default):

```bash
go -C acme/acme-srv build ./...
```

## 8. Run

Running a generated project (database, Kratos, console) is described in its own `README.md`, section `## Run`: `mise trust`, `mise install`, `mise run up`.

## 9. Try many configurations

`tooling/generator/testdata/ci/*.json` holds the selections CI covers. Generate and verify each into its own folder:

```bash
mkdir -p try
for f in tooling/generator/testdata/ci/*.json; do
  n=$(basename "$f" .json)
  initializer generate "try/$n" -f "$f" > "try/$n.log" 2>&1 \
  && initializer verify "try/$n" >> "try/$n.log" 2>&1 \
  || echo "FAIL $n"
done
```

It prints nothing when every selection passes; each log is `try/<name>.log`.

## 10. Output and errors

- `--format json` prints one JSON document: on stdout for exit 0 and 1, on stderr for exit 2 and 3 (stdout stays empty).
- `--log json` writes structured log events to stderr.
- `Sources:`, `note:`, `warning:` and `error:` lines go to stderr; stdout carries the result only.

| Exit | Meaning |
|---|---|
| 0 | Success: results, help, `version`, `verify` with no differences |
| 1 | The values are wrong: `SELECTION_INVALID`, `NAME_*`, `COMPONENT_*`, `MODULE_UNKNOWN`, `OUTPUT_NOT_EMPTY`, `verify` differences |
| 2 | Change the command or the environment: command-line problems, `SOURCE_ROOT_NOT_FOUND`, `SOURCE_ROOT_INVALID`, `INPUT_FORMAT_UNKNOWN` |
| 3 | I/O or internal failure: `INPUT_UNREADABLE`, `OUTPUT_UNWRITABLE`, `VERIFY_UNREADABLE` |

Codes a person meets:

| Code | Meaning and fix |
|---|---|
| `MODULE_UNKNOWN` | The module id is not in the catalog; run `initializer module ls`. |
| `OUTPUT_NOT_EMPTY` | The folder already holds files; choose another one. |
| `NAME_INVALID` | A name breaks the name rule of section 5. |
| `COMPONENT_MULTI_UNSUPPORTED` | A stack has more than one component; use one. |
| `COMPONENT_MERGE_AMBIGUOUS` | A `--component` flag names no component of the file; edit the file. |
| `SOURCE_ROOT_NOT_FOUND` | No clone found from the working directory or the binary; run inside a clone, pass `--root <clone>`, or set `INITIALIZER_ROOT`. |
| `REQUIRES_STACK_MISMATCH` | A catalog build error: a module names different required modules in its two stacks. |
| `GENERATOR_STALE` | A warning: the sources changed since the build; run `mise run cli`. |

A flag the CLI used to have answers exit 2 with its replacement:

```text
error: --out: was removed
  hint: pass the folder as an argument: initializer generate <dir>
```

`--selection` becomes `-f <file>`, and `--project` becomes the `DIR` argument of `verify`.

`initializer catalog build --out <file>` is for CI and module authors, who need the catalog as a file; people who generate projects never build it.
