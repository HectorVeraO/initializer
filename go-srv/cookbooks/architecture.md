# go-srv -- Architecture and module layout

Read before adding or changing a module, route, handler or error response. Canonical examples: `go-srv/internal/platform/core/health` (mounting, handler, black-box test) and `go-srv/internal/platform/core/coretest` (a module under test).

## Layout

| Path | Holds |
|---|---|
| `cmd/server/main.go` | Fixed: `os.Exit(core.Main(installedModules()))`. `cmd/server/modules.go` is generated and lists the installed modules. No business logic. |
| `internal/platform/core/` | The always-installed foundation: module seam (`Module`, `Builder`, `Host`, `Build`, `Run`), request chain, `api`, `access`, `reqctx`, `featureflag`, `config`, `telemetry`, `db`, `migrate`, `health`, `clock`, `coretest`. |
| `internal/platform/<module>/` | One package tree per catalog module (`identity`, `tenancy`, `apikeys`, `parameters`, `featureflags`). |
| `specs/<module>/openapi.yaml` (repo root) | The module's HTTP contract. The Go code implements it; it never defines it. |

- Import rules, enforced by `internal/platform/core/boundary_test.go`: a package under `core/` imports no other `internal/platform/<x>`; a module imports from this Go module only `core/...` and its own subtree; `cmd/server` imports only `core` and the module root packages named in `modules.go`; core subpackages never import the `core` root (`core/coretest` is the one exception); the Go module path appears only in `go.mod` and import specs, in an import group of its own; no Go file of a module names a table whose prefix another module claims (rule 6).
- Ports are declared in core only: `access.SessionResolver`, `access.APIKeyResolver`, `access.TenantAccess`, `access.TenantDirectory`, `accounts.Directory`, `accounts.Provisioner`, `featureflag.Evaluator`. A module publishes one with `Builder.Provide*` in `Provide`, once, and reads it from `Host` in `Mount`, `Start` or a handler. Each has a fail-closed default (`access.NoTenantAccess`, `accounts.NoDirectory`, `featureflag.Disabled{}`, ...); a module never branches on which other modules are installed. Full list: `.claude/references/architecture.md` -> "Modules and core ports".
- One module owns its routes, its handlers, its persistence prefix and its tests. Nothing outside the module reads its tables.
- Modules talk to each other only through core ports, never through another module's package or tables; a column that holds another module's id has no foreign key. Account data reaches tenancy through `accounts.Directory` and `accounts.Provisioner`; tenant data reaches identity and api-keys through `access.TenantAccess` and `access.TenantDirectory`.
- Module paths are stable: the generator copies `internal/platform/<module>/` into generated projects at the same path, and each records its version in `initializer.lock.json` (copy-and-own). Do not move a module directory.
- No package-level mutable state. Dependencies (logger, pool, clock, config) arrive as constructor parameters.
- No reserved empty interfaces for modules that do not exist yet.

## Routing

- Stdlib `net/http` `ServeMux` method patterns, registered only through `Host.Handle(pattern, policy, handler)` in a module's `Mount`: `h.Handle("GET /api/v1/parameters", auth.Policy{Kind: auth.KindSession}, handler)`. Every `/api/v1/` pattern carries a method; `/health/` belongs to core.
- Each module exposes `Module() core.Module` (no package-level registry), listed in `cmd/server/modules.go`. `Register(mux, ...)` no longer exists; the policy replaces hand-written auth checks. `core/health` is mounted by core itself.
- Path parameters come from `r.PathValue("id")`; validate them before use.
- Versioned API routes start with the path prefix /api/v1; probes start with /health.

## Response envelope (`ApiEnvelope<T>`)

Every /api/v1 response body is one JSON object:

```json
{
  "data": {},
  "meta": { "traceId": "4bf92f3577b34da6a3ce929d0e0e4736", "timestamp": "2026-01-01T00:00:00Z" },
  "error": null
}
```

- Success: `data` set, `error` null. Failure: `data` null, `error` set as `{ "code": "<CODE>", "message": "<text>", "issues": [] }`.
- `meta.traceId` equals the `X-Trace-Id` response header (see `cookbooks/testing-tck.md` and root `AGENTS.md` "Platform rules").
- `meta.timestamp` is RFC 3339 UTC ending in `Z`.
- Probes (/health/*) do not use the envelope.

## Errors

- Error codes are SCREAMING_SNAKE constants declared in the module that raises them (`TENANT_MISMATCH`, `LAST_TENANT_ADMIN`, `SYSTEM_PARAMETER_PROTECTED`). A code is part of the contract: renaming one is a breaking change.
- Validation is accumulated: a `POST`, `PUT` or `PATCH` with invalid data collects every problem and answers `422` with all of them in `error.issues[]` (`{ "field": "<json path>", "code": "<CODE>", "message": "<text>" }`). Never stop at the first invalid field.
- Status mapping: `401` credentials missing or rejected, `403` authenticated but not allowed (with its code), `404` not found or not visible to the active tenant, `409` state conflict, `422` validation.
- The registry of codes lives in `internal/platform/core/api/codes.go`; add a code there with its contract change. Body decoding answers `413 PAYLOAD_TOO_LARGE`, `415 UNSUPPORTED_MEDIA_TYPE` and `422` with every issue (`api.DecodeBody`).
- Never return an internal error message, SQL error or stack trace in `error.message`. Log it with the trace id; answer a generic `500`.

## Tenant scoping

- The active tenant comes from the request context, resolved from the stateless session token or from the API key. Handlers never read a tenant id from the body to decide access.
- Every lookup by `:id` filters by the active tenant. A resource of another tenant answers `404`, never `403`: the response must not reveal that it exists.
- An optional `tenantId` in an M2M payload accepts the tenant UUID or slug and must match the API key's tenant, else `403 TENANT_MISMATCH`.
- The tenancy `Start` hook stops the process when `APP_BOOTSTRAP_TENANT_SLUG` is set, no tenant exists and no account has `APP_BOOTSTRAP_ADMIN_EMAIL`. Without tenancy every principal and row is in the one global scope; with it, a NULL `tenant_id` (a global API key) is operator-only.

## Implementation checklist

- [ ] The contract change is in `specs/<module>/openapi.yaml` first, in the same PR.
- [ ] Routes are registered through `Host.Handle` with an `auth.Policy` and use method patterns.
- [ ] Every /api/v1 response uses the envelope; validation errors are accumulated `422`.
- [ ] Every `:id` access is scoped to the active tenant (`404` across tenants).
- [ ] Tables use the module prefix (`cookbooks/data-migrations.md`).
- [ ] Tests cover the behavior through HTTP (`cookbooks/testing-tck.md`).
