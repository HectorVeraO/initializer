# go-srv -- Architecture and module layout

Read before adding or changing a module, route, handler or error response. Canonical example today: `go-srv/internal/gatbase/health` (registration, handler, black-box test).

## Layout

| Path | Holds |
|---|---|
| `cmd/server/main.go` | Process wiring only: logger, config from env, `http.ServeMux`, module registration, graceful shutdown. No business logic. |
| `internal/gatbase/<module>/` | One package tree per Gatbase module (`health`, `tenancy`, `iam`, `parameters`, `flags`, ...). |
| `specs/<module>/openapi.yaml` (repo root) | The module's HTTP contract. The Go code implements it; it never defines it. |

- One module owns its routes, its handlers, its persistence prefix and its tests. Nothing outside the module reads its tables.
- Modules talk to each other through exported Go functions or interfaces of the other module's package, never through its tables.
- Module paths are stable: the generator copies `internal/gatbase/<module>/` into generated projects at the same path, and each records its version in `gatbase.lock.json` (copy-and-own). Do not move a module directory.
- No package-level mutable state. Dependencies (logger, pool, clock, config) arrive as constructor parameters.
- No reserved empty interfaces for modules that do not exist yet.

## Routing

- Stdlib `net/http` `ServeMux` with method patterns: `mux.HandleFunc("GET /api/v1/parameters", h.list)`.
- Each module exposes one `Register(mux *http.ServeMux, ...)` entry point, called from `cmd/server/main.go`. It panics on a nil mux (programmer error), as `health.Register` does.
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
- `meta.traceId` equals the `X-Trace-Id` response header (see `cookbooks/testing-tck.md` and root `AGENTS.md` "Gatbase rules").
- `meta.timestamp` is RFC 3339 UTC ending in `Z`.
- Probes (/health/*) do not use the envelope.

## Errors

- Error codes are SCREAMING_SNAKE constants declared in the module that raises them (`TENANT_MISMATCH`, `LAST_TENANT_ADMIN`, `SYSTEM_PARAMETER_PROTECTED`). A code is part of the contract: renaming one is a breaking change.
- Validation is accumulated: a `POST`, `PUT` or `PATCH` with invalid data collects every problem and answers `422` with all of them in `error.issues[]` (`{ "field": "<json path>", "code": "<CODE>", "message": "<text>" }`). Never stop at the first invalid field.
- Status mapping: `401` credentials missing or rejected, `403` authenticated but not allowed (with its code), `404` not found or not visible to the active tenant, `409` state conflict, `422` validation.
- Never return an internal error message, SQL error or stack trace in `error.message`. Log it with the trace id; answer a generic `500`.

## Tenant scoping

- The active tenant comes from the request context, resolved from the stateless session token or from the API key. Handlers never read a tenant id from the body to decide access.
- Every lookup by `:id` filters by the active tenant. A resource of another tenant answers `404`, never `403`: the response must not reveal that it exists.
- An optional `tenantId` in an M2M payload accepts the tenant UUID or slug and must match the API key's tenant, else `403 TENANT_MISMATCH`.

## Implementation checklist

- [ ] The contract change is in `specs/<module>/openapi.yaml` first, in the same PR.
- [ ] Routes are registered through the module's `Register` and use method patterns.
- [ ] Every /api/v1 response uses the envelope; validation errors are accumulated `422`.
- [ ] Every `:id` access is scoped to the active tenant (`404` across tenants).
- [ ] Tables use the module prefix (`cookbooks/data-migrations.md`).
- [ ] Tests cover the behavior through HTTP (`cookbooks/testing-tck.md`).
