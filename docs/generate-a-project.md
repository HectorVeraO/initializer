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
Several components of one stack: repeat --component STACK=NAME (see 'initializer help generate').
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
Project acme generated in ./acme (543 files).
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
| Project name | the folder's name; none when `DIR`, `--name` and `-f` are all absent |
| Stacks | the catalog's backend stack (`go-srv`) |
| Modules | none beyond `core` and what the chosen modules require; `--all-modules` installs every module |
| Go module | `example.com/<name>`; pass `--go-module` for yours |

A project needs a name from `DIR`, `--name` or the input file. With none of them the command writes nothing and exits 2:

```text
error: needs a project name or a folder [NAME_REQUIRED]
  hint: name the project: initializer generate <name>  (or --name <name>, or -f <file>)
1 problem.
```

An input file without a project name, with no `DIR` or `--name`, answers `NAME_REQUIRED` as an issue (exit 1, nothing written).

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
566 files
```

To turn a flag line into a starter file, keep the `selection` member of the dry run's JSON document:

```bash
initializer generate starter --stacks go-srv --modules parameters --dry-run --format json | jq .selection > starter.json
initializer generate starter -f starter.json
```

The `selection` member is valid `-f` input for every project, also one with several backends: each backend entry then holds its own modules, Go module, database and services, and each frontend entry its `backend`.

Every generated project carries its choices as `initializer.selection.json` (with the port band when you set one), so `-f <project>/initializer.selection.json` regenerates it. The file records the project name, also when it came from the folder; the Go module, folders, database and services you did not set are derived from the name again. `--name` on the replay renames the project and every derived name, and a frontend bound to the derived backend follows it.

## 5. Names, database, services, display name, port band

| Flag | Sets | Default |
|---|---|---|
| `--name NAME` | Project name | the folder's name; required without `DIR` or `-f` |
| `--display-name TEXT` | Brand text of the web console | the project name |
| `--component STACK[=NAME][,KEY=VALUE...]` | Add or override a component of STACK (section 6) | one component per stack, named `<name>-<suffix>` (`acme-srv`, `acme-web`) |
| `--db-name NAME`, `--db-user NAME` | Database and its user | `<name>` with `-` replaced by `_` |
| `--service KEY=NAME` | Rename one compose service; with several backends only `postgres` and `postgres-init` (section 6) | `<name>-<key>` |
| `--port-band NNN` | Host ports `1NNxx`, from 100 to 654 | `101` |

```bash
initializer generate shop --name shop --stacks go-srv,ng-web --display-name "Acme Shop" \
  --db-name shop_db --db-user shop_app --service postgres=shop-pg \
  --component go-srv=shop-api --port-band 103 --dry-run
```

```text
note: Go module defaults to example.com/shop; pass --go-module to set yours
Project shop in ./shop (dry run, nothing written)
COMPONENT   STACK    FOLDER      MODULES
shop-api    go-srv   shop-api/   core
shop-web    ng-web   shop-web/   core
Go module   example.com/shop (default)
Database    shop_db (user shop_app)
Services    shop-pg
Ports       band 103
407 files
```

Names are lowercase letters, digits and inner hyphens, 2 to 63 characters, starting with a letter. Anything else answers `NAME_INVALID` with an example.

With the identity module, Kratos keeps the fixed role and database `kratos` and the password key `KRATOS_PG_PASSWORD` in a single-component project. The project name `kratos` (its prefix `KRATOS` makes the app's password key that fixed key), `--db-name kratos` and `--db-user kratos` answer `NAME_RESERVED` with an example; a project without identity keeps every name. A several-backend project derives the Kratos names (`<database>_kratos`), so only the project name is reserved there.

A frontend component's name is also its Angular project id: the project key and the `dist/<name>` output folder in `angular.json`, the `name` in `package.json` and `package-lock.json`, and the build output folder in the `Dockerfile`.

### Running several local stacks

The generated `.deploy/local/docker-compose.yml` is named `<project name>-<port band>` (`acme-101`, `acme-103`). Docker Compose keys containers and volumes (the Postgres data) by that name, so:

- Two projects keep separate containers and volumes when their project name or their port band differs: `mise run up`, `down` and `down -v` in one never touch the other. To run both at the same time their port bands must differ too, because the host ports come from the band.
- Two projects with the same name and the same band share one compose project: the same containers and the same Postgres volume, and their host ports clash. Give each its own name (`--name`) or its own band (`--port-band`).

```bash
initializer generate acme-a -f acme.yaml
initializer generate acme-b -f acme.yaml --port-band 103
```

Both folders hold the project `acme`; the first runs as compose project `acme-101`, the second as `acme-103`.

## 6. Components

A component is one copy of one stack, in the folder named after it. Without `--component`, each stack produces one component named `<name>-<suffix>`. A project can hold several components of one stack. Each backend is an instance with its own origin, ports, database and user on the project's one Postgres server and, with identity, its own Kratos (with its own database and user) and session key; a frontend binds to exactly one backend, and a backend serves at most one frontend. How many backends fit is set by the host ports (see "How many backends fit").

`--component STACK[=NAME][,KEY=VALUE...]` is repeatable. With several components of one stack, each needs a `NAME`. Keys:

| Key | Sets | For |
|---|---|---|
| `modules` | Modules the component starts from, ids joined by `+`; required modules are added | backends |
| `go-module` | Go module path | backends |
| `db-name`, `db-user` | Database and its user | backends |
| `service.<key>` | Rename one compose service of the component (`kratos`, `kratos-migrate`); the Postgres services are the project's, see below | backends |
| `backend` | Name of the backend the frontend binds to | frontends |

`go-module` may extend the repository's own Go module path (`<repository module>-fork`, `<repository module>/v2`). A path that is still the repository's own, next to components with another module, answers `GO_MODULE_PATH_LEAK` (exit 1, nothing written).

A frontend installs what its backend installs, so it takes no `modules`. A top-level `--modules` seeds every backend that sets no `modules` of its own. With several backends, set `--db-name` and `--db-user` per component.

### Two backends and a console

The first backend installs identity only; the second installs every module; the console binds to the second. As flags:

```bash
initializer generate acme-backend --stacks go-srv,ng-web --go-module example.com/acme/backend \
  --modules identity \
  --component go-srv=acme-backend-srv-first \
  --component go-srv=acme-backend-srv-second,modules=api-keys+feature-flags+identity+parameters+tenancy \
  --component ng-web=acme-backend-web,backend=acme-backend-srv-second --dry-run
```

or as a YAML file (`multi.yaml`), run with `initializer generate acme-backend -f multi.yaml --dry-run`:

```yaml
schema: 2
project:
  name: acme-backend
  goModule: example.com/acme/backend
stacks: [go-srv, ng-web]
modules: [identity]
components:
  - stack: go-srv
    name: acme-backend-srv-first
  - stack: go-srv
    name: acme-backend-srv-second
    modules: [api-keys, feature-flags, identity, parameters, tenancy]
  - stack: ng-web
    name: acme-backend-web
    backend: acme-backend-srv-second
```

Both print the same plan (`INDEX` counts components of one stack; `PORTS` are the host ports of the instance):

```text
Project acme-backend in ./acme-backend (dry run, nothing written)
COMPONENT                 STACK    INDEX   BACKEND                   GO MODULE                                          DATABASE                                          PORTS                                                                MODULES
acme-backend-srv-first    go-srv   0                                 example.com/acme/backend/acme-backend-srv-first    acme_backend_srv_first/acme_backend_srv_first     srv 10100, postgres 10132, kratos-public 10133, kratos-admin 10134   core, identity
acme-backend-srv-second   go-srv   1                                 example.com/acme/backend/acme-backend-srv-second   acme_backend_srv_second/acme_backend_srv_second   srv 10101, postgres 10132, kratos-public 10135, kratos-admin 10136   core, identity, api-keys, feature-flags, parameters, tenancy
acme-backend-web          ng-web   0       acme-backend-srv-second                                                                                                        web 10120                                                            core, identity, api-keys, feature-flags, parameters, tenancy
810 files
```

The command also prints `note: go modules derived from example.com/acme/backend; pass go-module= per component to change them` on stderr (section 10). Without `--dry-run` the command writes the project (810 files) and `initializer verify acme-backend` answers `No differences.`

### One Postgres server for all backends

A project with several backends runs one Postgres server, one volume and one host port (`10132` on band 101), whatever the number of backends:

| What | Value |
|---|---|
| Server service | `<name>-postgres` (`--service postgres=NAME`), superuser `postgres` |
| Init service | `<name>-postgres-init` (`--service postgres-init=NAME`): a one-shot that runs on every `up` and creates, idempotently, the database and user of every backend and, with identity, of every Kratos. Backends and Kratos start after it |
| Backend database and user | `--db-name` and `--db-user` per component; default `<component>` with `-` replaced by `_` |
| Kratos database and user | `<database>_kratos` and `<user>_kratos` of its backend |
| Passwords | `<NAME>_PG_PASSWORD` (the name in upper case, `-` as `_`) for the superuser and every backend user; `KRATOS_PG_PASSWORD` for every Kratos user. Local-only defaults in `docker-compose.yml`, overridden through the environment or `.deploy/local/.env` |
| Connection string of a backend | `postgres://<user>:${<NAME>_PG_PASSWORD}@<server>:5432/<database>`: inside the compose network the server is `<name>-postgres`; on the host it is `localhost:10132` |

The `postgres` and `postgres-init` services are the project's, so `--service postgres=NAME` applies to all backends and `service.postgres` on one component answers `NAME_OVERRIDE_UNUSED`. Two databases, or two users, that resolve to the same name (the server's own `postgres` included) answer `NAME_CONFLICT`; set `db-name` and `db-user` on one of them. A single-backend project keeps one Postgres whose superuser is the backend's own user.

### How many backends fit

There is no fixed count: the host ports of a band (`1NN00-1NN98`) decide. Each port is at an offset of the band: a backend takes `00+i` (its HTTP port, `i` counting backends from 0), a frontend `20+i`, the shared Postgres `32`, and a backend with identity `33+2i` and `34+2i` for Kratos. With every module, the band holds:

| Shape | Backends |
|---|---|
| No console | 32 (offsets `00`-`31`, up to the Postgres) |
| One console | 20 (the 21st backend would use the console's offset `20`) |
| A console per backend | 12 (consoles at `20`-`31`, up to the Postgres) |

Past that, the command answers `PORT_BAND_EXHAUSTED`, names the port and its component, and states how many components fit with the same modules. A band holds one project of this size; projects that should run side by side use different bands.

In a multi-component project:

- Per-component files live in `.deploy/local/<component>/`: `.example.env` (local-only defaults, read by compose and the host-dev task) and, with identity, `kratos/`. Put overrides in `.deploy/local/<component>/.env` (optional, gitignored, same keys as `.example.env`); compose passes it to the container and `mise run dev:<component>` reads it. `.deploy/local/.env` is read only by compose interpolation and reaches every instance.
- The host-dev tasks are `dev:<component>` (`mise run dev:acme-srv-first`, `mise run dev:acme-web`); the generated READMEs and `.example.env` files name them.
- Each Kratos reads its public origin in Docker mode from `APP_PUBLIC_BASE_URL_<COMPONENT>` in `.deploy/local/.env` (the backend's name in upper case, `-` as `_`; default: the instance's origin). To move an instance, set that variable and `APP_PUBLIC_BASE_URL` in the instance's `.env` to the same value.
- A single-component project keeps `.example.env` and `kratos/` directly in `.deploy/local/`, and overrides go in `.deploy/local/.env`.
- On `localhost` one browser profile is signed in to one identity instance at a time, because cookies are per host, not per port: signing in to the second backend overwrites the first's cookies. Use a second browser profile or a private window per instance.
- `mise run tck:<component>` runs the TCK against one backend; the generated `README.md` lists the tags and the `.env` keys each needs.

When a file lists components, a `--component STACK=NAME` flag must name one of them exactly; it never renames, and its keys override that component. For a file that lists `acme-core` for `go-srv`:

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

Add, remove or rename a component by editing the file. Two examples of the new codes:

```bash
initializer generate x1 --name acme --stacks go-srv --component go-srv --component go-srv --dry-run
```

```text
error: --component go-srv: stack go-srv has 2 components; name each one [COMPONENT_NAME_REQUIRED]
  hint: initializer generate x1 --name acme --stacks go-srv --component go-srv=acme-srv-first --component go-srv=acme-srv-second --dry-run
1 problem; nothing was written.
```

```bash
initializer generate x3 --name acme --stacks go-srv,ng-web --component go-srv=acme-one \
  --component go-srv=acme-two --component ng-web=acme-web,backend=nope --dry-run
```

```text
error: --component ng-web=acme-web,backend=nope: "nope" names no backend component [COMPONENT_BACKEND_UNKNOWN]
  hint: backend components: acme-one, acme-two
1 problem; nothing was written.
```

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
- With `--format json`, the `Sources:`, `note:` and `warning:` lines are printed only when the exit code is 0 or 1; an exit 2 or 3 leaves the one JSON document alone on stderr.

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
| `COMPONENT_NAME_REQUIRED` | A stack has several components and one has no name; the error's hint (`suggestedNames` in JSON) lists names to use. |
| `COMPONENT_BACKEND_REQUIRED` | A frontend does not say which of several backends it binds to; set `backend=`. |
| `COMPONENT_BACKEND_UNKNOWN` | `backend=` names no backend component; use one the hint lists. |
| `COMPONENT_BACKEND_TAKEN` | Two frontends bind to one backend; a backend serves at most one. |
| `COMPONENT_FIELD_NOT_ALLOWED` | A key does not apply to that component (for example `modules` on a frontend). |
| `NAME_CONFLICT` | Two names resolve to one: components, services, or databases and users on the shared Postgres server; set one of them. |
| `COMPONENT_AMBIGUOUS` | A top-level `--db-name` or `--db-user` would be inherited by several backends; set it per component. |
| `MODULES_UNUSED` | Top-level `--modules` while every backend sets its own; drop one of them. |
| `PORT_BAND_EXHAUSTED` | The components' host ports do not fit the band (offsets 0-98); the hint says how many fit, use fewer components. |
| `NAME_RESERVED` | A component name equals a folder of the project (for example `specs`), or, with identity, the project name is `kratos` (also `--db-name` and `--db-user` in a single-component project), the fixed Kratos name; choose another. |
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
