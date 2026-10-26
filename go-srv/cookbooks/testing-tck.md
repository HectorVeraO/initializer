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

- Test through the public surface: build the module's handlers with `Register` on a fresh `http.ServeMux`, serve them with `net/http/httptest`, and assert status codes, headers and decoded JSON. `internal/gatbase/health/health_test.go` is the reference.
- Use the external test package (`package <module>_test`) so tests see only exported API.
- No mocks of the module's own collaborators (repositories, services). Fakes only at real boundaries: external HTTP providers (an `httptest.Server` playing the provider), the clock, randomness.
- One behavior per test function; the name states it (`TestSwitchTenantRejectsNonMember`).
- Assert the full error contract on failures: status, `error.code` and, for `422`, every expected entry in `error.issues[]`.
- Cross-tenant cases are mandatory for every `:id` route: a resource of `tenant-b` read with a `tenant-a` session answers `404`.
- Table-driven tests for validation matrices.

## Integration tests (PostgreSQL)

- Run against the compose service `postgres` (host port `10132`, see `.deploy/local/README.md`), never against a shared database.
- Each test creates its own tenant and data, and never depends on another test's rows.
- Skip with `t.Skip` and a clear message when the database is not reachable; never report a skipped integration test as passed.
- Never point a test at a non-local database.

## TCK

- The TCK certifies a running instance from outside, over HTTP only, with no knowledge of Go, the schema or the database.
- Each scenario is tagged with its module: `@core-auth`, `@parameters`, `@feature-flags`, `@otel`. The runner selects tags by the modules installed in the instance.
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
