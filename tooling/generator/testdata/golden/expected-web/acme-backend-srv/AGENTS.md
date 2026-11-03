# AGENTS.md - acme-backend-srv

The Go backend component of this project: the always-installed `core`, the catalog modules the
generator delivered, and the product verticals this project writes itself. Code, comments and commit
messages are in English.

## Layout

| Path | Holds |
|---|---|
| `cmd/server/main.go` | Fixed: `os.Exit(core.Main(installedModules()))`. No business logic. |
| `cmd/server/modules.go` | Generator-owned: the catalog modules, in catalog order. Re-generating the project replaces it. |
| `cmd/server/product.go` | Project-owned: `productModules()`, the verticals this project adds. |
| `internal/platform/core/` | The foundation: module seam, request chain, `api`, `access`, `reqctx`, `featureflag`, `parameter`, `config`, `telemetry`, `db`, `migrate`, `health`, `clock`. |
| `internal/platform/<module>/` | One catalog module (identity, parameters). Copy-and-own: the project may edit it, and a module path never moves. |
| `internal/<vertical>/` | A product vertical: its module, `migrations/`, handlers, service, store and codes. |
| `internal/testkit/` | `coretest` and `testpg`. Only a `_test.go` file imports it. |

## Commands

Run every command from this folder.

| Action | Command |
|---|---|
| Build | `go build ./...` |
| Vet | `go vet ./...` |
| Test | `go test ./...` |
| Run | `go run ./cmd/server` |

`go run` needs `APP_DATABASE_URL`. `../.deploy/local/.example.env` lists that value and every other one the component reads; `../README.md` and `../.deploy/local/README.md` show the local stack and its ports. A database test needs `APP_TEST_DATABASE_URL` and skips without it; never report a skipped test as passed.

## Import rules

`go test ./...` enforces them in `internal/platform/core/boundary_test.go`:

- A package under `internal/platform/core/` imports no other module and no vertical.
- A platform module imports only `core/...` and its own subtree.
- A vertical imports only non-internal `core/...` packages and its own subtree: no platform module,
  no sibling vertical.
- `cmd/server/product.go` imports the core root and vertical roots only; every other file of
  `cmd/server` imports the core root and the module roots `modules.go` names.
- No file of a package imports `internal/testkit`; a `_test.go` file may.
- The Go module path on line 1 of `go.mod` appears in `go.mod` and in import specs only, in an
  import group of its own.

## Wire format

- Every `/api/v1` response is the envelope `{ data, meta: { traceId, timestamp }, error }`. Write it
  through `Host.API()`: `writer.Data` for success, `writer.Fail` for failure. `X-Trace-Id` equals
  `meta.traceId`.
- Invalid input answers one `422 VALIDATION_FAILED` with every problem in `error.issues[]`; validate
  everything, then answer once (`api.Problems`).
- Error codes are SCREAMING_SNAKE and part of the contract. `internal/platform/core/api/codes.go`
  holds the core and catalog codes; a vertical declares its own in its `errors.go`.
- A missing, malformed or foreign `:id` answers `404`: never reveal that another tenant's resource
  exists.

## Tables and migrations

- A module embeds `migrations/*.sql` (`//go:embed`) and core applies them at startup, one goose
  version table per module.
- A migration file is `<YYYYMMDDhhmmss>_<prefix>_<name>.sql`. A platform module claims its prefixes
  in its `module.manifest.json`; a vertical derives its single prefix from its file names.
- `cor`, a platform prefix, a reserved prefix and a prefix already used elsewhere are refused, and
  `go test ./...` reports it. Additive changes first: no change drops or renames something the
  running code reads.
- Full rules: `cookbooks/data-migrations.md`.

## Ports

- Parameters: `Host.Parameters()` (`parameter.Reader`) with the tenant taken from the request context. Without a module that provides the port it is `parameter.NoReader{}`: every key not found, no call fails, and a missing key is not a `500`.
- Tenancy: `reqctx.PrincipalFrom` and `reqctx.TenantFrom`. The active tenant comes from the session token or the API key, never from the body.
- Adding a vertical: `cookbooks/new-domain-module.md`. Between services, a core port is the only coupling.
