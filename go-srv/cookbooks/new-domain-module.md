# Adding a domain module

Read before adding a vertical: the feature the application is for, as opposed to the catalog modules that every project installs. A vertical lives in `go-srv/internal/<vertical>/` and is the only kind of Go code this project writes itself.

## The seam

`go-srv/cmd/server/product.go` is project-owned. `go-srv/cmd/server/main.go` and `go-srv/cmd/server/modules.go` are generator-owned: re-generating the project replaces them, so an edit there is lost. Registering a vertical is one edit in `product.go`:

```go
import (
	"<module>/internal/dispatch"
	"<module>/internal/platform/core"
)

func productModules() []core.Module {
	return []core.Module{
		dispatch.Module(),
	}
}
```

`<module>` is the path on line 1 of `go-srv/go.mod`. `cmd/server/modules.go` appends `productModules()` to the catalog modules in that order, so a vertical starts after every platform module.

## Files of a vertical

| File | Holds |
|---|---|
| `module.go` | `Module() core.Module` and the `module` struct `Provide` fills |
| `migrations.go` | `//go:embed migrations/*.sql` and the `embed.FS` |
| `migrations/` | goose files, `<YYYYMMDDhhmmss>_<prefix>_<name>.sql` (`cookbooks/data-migrations.md`) |
| `errors.go` | the vertical's error codes as `api.Code` constants |
| `handlers.go` | `Mount`, the routes and the request decoding |
| `service.go` | validation and the rules; returns `*api.Error` for a caller mistake |
| `store.go` | the SQL on the vertical's tables |
| `module_test.go` | `package <vertical>_test`, through `testcore` (`cookbooks/testing-tck.md`) |

## Lifecycle

`core.Module` has three optional funcs and one embedded filesystem. Core runs them in this order, for every module in list order:

| Phase | Call | May | Must not |
|---|---|---|---|
| 1 | `Provide(ctx, *core.Builder)` | read config, build services, call `b.Scopes().Register`, publish a port with `b.Provide*` | do I/O, read another module's port |
| (between) | migrations | the embedded files are applied, after every `Provide` and before any `Mount` | |
| 2 | `Mount(ctx, *core.Host)` | register routes with `h.Handle`, add readiness checks | write to the database at startup |
| 3 | `Start(ctx, *core.Host)` | work that needs a mounted router and a migrated schema | block past its own work |

- `Provide` does no I/O because core reports every configuration problem at once before it opens the pool. `b.Pool()` is nil when `APP_DATABASE_URL` is missing or unparseable: return nil then, as the catalog modules do, and let the config error stop startup.
- `Host.Handle` panics outside `Mount`, on a duplicate pattern, on a `/health/` pattern, and on a `/api/v1` pattern without a method. A panic in a phase stops the process with the message naming the module and the pattern.
- A port is published once, in `Provide`, and never after. A second `Provide*` for the same port fails startup even if the error is dropped.

## Routes, policies, scopes

```go
func (m *module) mount(_ context.Context, h *core.Host) error {
	rt := &routes{service: m.service, writer: h.API()}
	member := access.Policy{Kind: access.KindSession, MinRole: reqctx.RoleViewer}
	h.Handle("GET /api/v1/dispatch", member, http.HandlerFunc(rt.list))
	h.Handle("POST /api/v1/dispatch", member, http.HandlerFunc(rt.create))
	return nil
}
```

- The policy is the only authorization: `KindSession`, `KindSessionAllowExpired`, `KindAPIKey`, `KindSessionOrAPIKey` or `KindNone`. Handlers repeat no credential check.
- `MinRole` requires an active tenant; a session principal also needs that role. An API key passes on its scope instead, because a key carries no role.
- `Policy.Scope` is required for `KindAPIKey` and `KindSessionOrAPIKey`, and the scope must be registered first, in `Provide`: `b.Scopes().Register("dispatch:read", "Read the dispatch queue", access.ScopeTenant)`. A scope is `<resource>:<verb>`, levels are `access.ScopeTenant` (a key bound to a tenant only) and `access.ScopeAny`. Registering after `Provide` fails startup.
- `PlatformOperator` needs `KindSession` and no role; `TenantlessOperator` needs a role and accepts a platform operator with no active tenant.
- Path values come from `r.PathValue("id")`; validate them before use.

## Envelope, errors, 422

- Every `/api/v1` response goes through `h.API()`: `writer.Data(w, r, http.StatusOK, view)` on success, `writer.Fail(w, r, err)` on failure. Never write a body directly: the writer sets `X-Trace-Id`, `meta.traceId` and `meta.timestamp`, and an unwrapped error becomes a logged 500.
- Validation accumulates. `api.DecodeBody` gives an `*api.Body`; `body.String(&problems, "name", api.Required)` records every problem instead of returning at the first, and one `problems.Err()` answers `422 VALIDATION_FAILED` with all of them in `error.issues[]`.
- A caller mistake that is not a field problem is an `*api.Error` from `errors.go`: `api.NewError(http.StatusConflict, CodeReferenceTaken, "A job with this reference is already queued")`. Status mapping: `401` missing or rejected credentials, `403` authenticated but not allowed, `404` absent or another tenant's resource, `409` state conflict, `422` validation.
- `api.Registry()` holds the core and catalog codes and is generator-owned. A vertical declares its own constants in `errors.go` and never adds to the registry; a test proves their shape and that no code collides:

```go
func TestCodesAreFree(t *testing.T) {
	codes := []api.CodeInfo{{Code: dispatch.CodeReferenceTaken, Status: http.StatusConflict}}
	for _, problem := range api.ValidateCodes(codes) {
		t.Error(problem)
	}
}
```

- Never put a SQL error, a path or a stack trace in `error.message`; log the cause and answer a generic 500.

## Tenancy

- The active tenant comes from the principal, never from the body: `reqctx.PrincipalFrom(r.Context())` and `reqctx.TenantFrom(r.Context())`.
- Every read and write by id filters by that tenant; a row of another tenant answers `404` through `api.NotFound()`, never `403`, so the response does not confirm that it exists.
- A unique constraint on tenant-owned data includes `tenant_id`. A row that may belong to no tenant carries a nullable `tenant_id`; NULL is the global scope, which only an operator reaches.
- A vertical that needs no tenant (a queue shared by the project) still takes the principal and records the actor.

## Migrations

- `migrations.go` embeds the folder; core applies it at startup under one advisory lock, one goose version table per module (`cor_migrations_<vertical>`).
- The prefix is derived from the file names, one per vertical: `<YYYYMMDDhhmmss>_<prefix>_<name>.sql`. It cannot be core's `cor`, a platform module's prefix, a reserved prefix, a prefix another vertical claims, or a token that already appears as an ordinary identifier outside the vertical.
- Rules, expand-first and the collision errors: `cookbooks/data-migrations.md`.

## Flags and parameters

- Flags: the evaluator is a core port and exists in every component, but the declarations, the loader and a real evaluator do not. `flags.yaml` at the project root, `APP_FLAGS_FILE` and the module that reads them ship only with the feature-flags module; without it `Host.FlagEvaluator()` is `featureflag.Disabled{}`, every key is false, and `cookbooks/tbd-feature-flags.md` and `flags.yaml` are not in the project at all. With the module installed: read the evaluator from the host and nothing else, `h.FlagEvaluator().Enabled(ctx, "dispatch_retry")`, so the code needs no branch on which modules are installed; declare the flag in `flags.yaml` and list the files in `APP_FLAGS_FILE` (`cookbooks/tbd-feature-flags.md`); evaluate a release flag once, at the boundary, with `featureflag.Route(eval, key, on, off)` around the route registration, never inside a service method.
- Parameters: `value, found, err := h.Parameters().Get(r.Context(), "dispatch.max_attempts")` resolves the active tenant's override, then the global value, with the tenant taken from the request context. `found == false` with a nil error is a missing definition: answer the documented default or a `404`, never a 500. An error means the store failed or the stored value does not decode; log it and answer a 500. Decode with the typed accessors (`value.String()`, `value.Number()`, `value.Bool()`, `value.Decode(&target)`) and handle `parameter.ErrTypeMismatch`.
- A build without the parameters module gets `parameter.NoReader{}`: every key is not found and no call fails. Never read the platform parameter tables directly.

## Import boundaries

`go test ./...` enforces these from `internal/platform/core/boundary_test.go`; a violation fails the build with `rule <n>: <import>`.

- Rule 1: core imports no vertical.
- Rule 2: a vertical imports only its own subtree and the core packages outside `core/internal`. Not a platform module, not another vertical (`internal/<other>`), not `core/internal/ctxkey`.
- Rule 3: `cmd/server/product.go` imports the core root and vertical roots only; every other file of `cmd/server` imports the core root and the module roots `modules.go` names.
- Rule 5: a core subpackage never imports the core root.
- Rule 6: no Go file of a vertical names a table whose prefix another module or vertical claims.
- Rule 7: only a `_test.go` file imports `internal/testkit`.

The Go module path appears in `go.mod` and in import specs, in an import group of its own.

## Checklist

- [ ] `internal/<vertical>/` holds `module.go`, `migrations.go`, `errors.go`, `handlers.go`, `service.go`, `store.go` and `module_test.go`.
- [ ] Registration is one edit in `cmd/server/product.go`; `main.go` and `modules.go` are untouched.
- [ ] Every `/api/v1` route is mounted in `Mount` with a method pattern and a policy.
- [ ] Every response goes through `h.API()`; a `422` carries every issue.
- [ ] The vertical's codes are constants in `errors.go`, checked with `api.ValidateCodes`.
- [ ] Every `:id` access filters by the active tenant and answers `404` across tenants.
- [ ] Migrations follow the prefix rule and are additive.
- [ ] The flag is declared in `flags.yaml`, evaluated once at the boundary; a parameter is read through `h.Parameters()` and a missing one is not a 500.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` pass.

## Pitfalls

1. A prefix that is also an ordinary word outside the vertical: a column named `dispatch_status` inside the vertical is fine, but `pay_status` in a platform module refuses the prefix `pay` at the vertical. The matcher cannot tell an identifier from a table name; choose another prefix rather than editing the other package.
2. A nil pool in `Provide`: reaching `b.Pool()` with no `APP_DATABASE_URL` gives nil, and a store built over it panics on the first query. Return nil and let the reported config problem stop startup.
3. A route registered in `modules.go` or from an `init()`: `Host.Handle` panics outside `Mount`, and `upgrade` overwrites `modules.go`.
4. A tenant read from the request body: the guard has already resolved the real one, and the two disagreeing is a cross-tenant leak. Read `reqctx.TenantFrom`.
5. A tenant-owned row counted by a query that filters the tenant in Go instead of in SQL: the count includes rows the caller may not see.
6. A second port provided for a port a catalog module already publishes, in a project that installs that module: startup fails with "already provided". A vertical consumes ports; it does not replace them.
7. A `_test.go` answer trusted as the TCK's: the TCK runs over HTTP against a live instance and is the contract; the Go test proves the internals.
