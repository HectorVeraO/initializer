# go-srv -- Testing and the TCK

Read before writing or changing tests in `go-srv`. Project-wide test discipline: `.claude/references/test-rules.md`.

## Test layers

| Layer | Where | Runs with |
|---|---|---|
| Unit and contract tests | `internal/gatbase/<module>/*_test.go`, package `<module>_test` | `go test ./...` |
| Integration tests (database) | same package, against a real PostgreSQL | `go test ./...` with the local stack up |
| TCK (black-box contract suite) | `specs/<module>/tck/` (repo root) | against a running instance |
| UI end-to-end | `initializer-e2e/` (Playwright) | `npm test` in that suite |

## Go tests

- Test through the public surface: run the module through `coretest.New(t, []core.Module{yourpkg.Module()}, coretest.Options{...})`, which runs `core.Build` and returns the full handler (chain, envelope, policies, fallback), serve requests with `net/http/httptest`, and assert status codes, headers and decoded `api.Envelope[T]`. `internal/gatbase/core/coretest/coretest_test.go` is the reference.
- `coretest.Options` supplies the boundary fakes: `SessionResolver` (`coretest.Session(principal)`: cookie `test_session` resolves to the principal, header `X-XSRF-TOKEN: ok` makes the CSRF check pass), `APIKeyResolver`, `TenantDirectory`, `FlagEvaluator`, `Clock` (default fixed 2026-01-01T00:00:00Z), `Config` (merged over `GATBASE_PUBLIC_BASE_URL=http://localhost:10120`) and `Logger` (receives every record with `trace_id`, `span_id`, `tenant_id`, `actor_id`).
- Use the external test package (`package <module>_test`) so tests see only exported API.
- No mocks of the module's own collaborators (repositories, services). Fakes only at real boundaries: external HTTP providers (an `httptest.Server` playing the provider), the clock, randomness.
- One behavior per test function; the name states it (`TestSwitchTenantRejectsNonMember`).
- Assert the full error contract on failures: status, `error.code` and, for `422`, every expected entry in `error.issues[]`.
- Cross-tenant cases are mandatory for every `:id` route: a resource of `tenant-b` read with a `tenant-a` session answers `404`.
- Table-driven tests for validation matrices.

## Integration tests (PostgreSQL)

- Set `GATBASE_TEST_DATABASE_URL` to an admin connection on the compose service `postgres` (host port `10132`, see `.deploy/local/README.md`; the password is in `.deploy/local/.env`). Tests never use that URL's own database: `coretest.Options{Database: true}` creates an `it_*` database on the server, applies the modules' migrations and drops it on cleanup; the next database test also drops `it_*` leftovers of killed runs that are older than an hour and have no connection.
- Without `Database: true` the pool rejects every query, so a module that touches `Pool` fails.
- Each test owns its database and never depends on another test's rows.
- A test skips with a clear message when `GATBASE_TEST_DATABASE_URL` is unset (the `go (go-srv)` CI job sets it); never report a skipped integration test as passed.
- Never point a test at a non-local database.

## TCK

- The TCK certifies a running instance from outside, over HTTP only, with no knowledge of Go, the schema or the database.
- Each scenario is tagged with its module: `@core`, `@core-auth`, `@parameters`, `@feature-flags`, `@otel` (five tags with `@core`; `@core` and `@otel` run against an instance with no module installed). The runner selects tags by the modules installed in the instance.
- Run it from the repo root against an instance started with `GATBASE_DATABASE_URL` and the output of `node specs/scripts/tck-env.mjs` exported: `TCK_BASE_URL=http://localhost:10100 npm --prefix specs run tck`. CI job `tck` does exactly this.
- The TCK of a module lives next to its contract in `specs/<module>/tck/` and shares the contract's version tag (`spec-<module>-vX.Y.Z`).
- Scenarios are written as Given / When / Then from the module spec; one scenario per invariant.
- The TCK is separate from the Playwright UI suite in `initializer-e2e/`. Do not put API contract checks in the UI suite.

## Test data

- Synthetic only: `tenant-a`, `tenant-b`, `acme`, `user@example.com`, `Jane Doe`, generated UUIDs.
- No real names, emails, tokens, production payloads or exports.
- Secrets in tests are obviously fake (`change-me`, `test-secret`) and never shaped like a real provider key.

## Checklist

- [ ] New behavior has an HTTP-level test; a fixed bug has a regression test that fails without the fix.
- [ ] Cross-tenant and `422` accumulation cases are covered where they apply.
- [ ] No internal mocks; boundary fakes only.
- [ ] `go vet ./...` and `go test ./...` pass from `go-srv/`.
