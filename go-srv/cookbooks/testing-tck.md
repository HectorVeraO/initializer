# Testing and the TCK

Read before writing or changing tests in this component.

## Test layers

| Layer | Where | Runs with |
|---|---|---|
| Unit and contract tests | `internal/platform/<module>/*_test.go` and `internal/<vertical>/*_test.go`, package `<name>_test` | `go test ./...` |
| Integration tests (database) | the same package, against a real PostgreSQL | `go test ./...` with the local stack up |
| TCK (black-box contract suite) | `specs/<module>/tck/` and `specs/<vertical>/tck/` in the project's `specs/` folder | against a running instance |

## Go tests

- Test-support code lives in `internal/testkit/` (`coretest` for module tests, `testpg` for
  databases), outside the platform tree. Only a `_test.go` file imports it; boundary rule 7 fails a
  file of the package that does.
- Test through the public surface: run the module through
  `coretest.New(t, []core.Module{yourpkg.Module()}, coretest.Options{...})`, which runs
  `core.Build` and returns the full handler (chain, envelope, policies, fallback), serve requests
  with `net/http/httptest`, and assert status codes, headers and decoded `api.Envelope[T]`.
  `internal/testkit/coretest/coretest_test.go` is the reference.
- `coretest.Options` supplies the boundary fakes: `SessionResolver` (`coretest.Session(principal)`:
  cookie `test_session` resolves to the principal, header `X-XSRF-TOKEN: ok` makes the CSRF check
  pass), `APIKeyResolver`, `TenantDirectory`, `FlagEvaluator`, `Parameters` (the `parameter.Reader`
  seam; no fake ships with it, so a test that needs a value supplies its own reader in its own
  `_test.go`), `Clock` (default fixed `2026-01-01T00:00:00Z`), `Config` (merged over the defaults,
  which include `APP_PUBLIC_BASE_URL`) and `Logger` (receives every record with `trace_id`,
  `span_id`, `tenant_id`, `actor_id`).
- Use the external test package (`package <name>_test`) so tests see only exported API.
- One behavior per test function; the name states it (`TestSwitchTenantRejectsNonMember`).
- Assert the full error contract on failures: status, `error.code` and, for `422`, every expected
  entry in `error.issues[]`.
- Cross-tenant cases are mandatory for every `:id` route: a resource of `tenant-b` read with a
  `tenant-a` session answers `404`.
- Table-driven tests for validation matrices.

## Integration tests (PostgreSQL)

- Set `APP_TEST_DATABASE_URL` to an admin connection on the compose service that runs PostgreSQL:
  the host port and the user are the ones `.deploy/local/README.md` and `.deploy/local/.env` list,
  for example `postgres://user:password@localhost:10132/postgres?sslmode=disable`.
- Tests never use that URL's own database: `coretest.Options{Database: true}` creates an `it_*`
  database on the server, applies the modules' migrations and drops it on cleanup; the next
  database test also drops `it_*` leftovers of killed runs that are older than an hour and have no
  connection.
- Without `Database: true` the pool rejects every query, so a module that touches `Pool` fails.
- Each test owns its database and never depends on another test's rows.
- A test skips with a clear message when `APP_TEST_DATABASE_URL` is unset; never report a skipped
  integration test as passed.
- Never point a test at a non-local database.

## TCK

- The TCK certifies a running instance from outside, over HTTP only, with no knowledge of Go, the
  schema or the database.
- Each scenario is tagged with its spec: `@core`, `@identity`, `@tenancy`, `@parameters`,
  `@feature-flags`, `@otel`, and the tag of a product vertical. The runner selects tags by the
  specs installed in the instance.
- Run it from the project root against an instance started with `APP_DATABASE_URL` and the
  variables the spec manifests declare in `tckEnv`, with `TCK_BASE_URL` set to the component's
  public origin:

  ```bash
  TCK_BASE_URL=http://localhost:10100 npm --prefix specs run tck
  ```

- The TCK of a module or vertical lives next to its contract in `specs/<name>/tck/` and shares the
  contract's version.
- A scenario may call routes of: its own spec; the specs of the modules its module requires,
  transitively; the specs named in its own spec's `cooperatesWith`. It carries its own spec's tag,
  plus the tag of each cooperating spec whose routes it calls, and it runs only when every one of
  its tags is installed. The runner inverts the uninstalled cooperating tags; `check:tags` accepts
  a co-tag only for a declared cooperating spec and reports any other foreign or unknown tag as
  `TCK_TAG_FOREIGN`.
- Authoring rules for `specs/<s>/tck/`:
  - Imports resolve in every selection that installs `<s>`: only `@playwright/test`, Node built-ins,
    `specs/tck-support/index.ts` and `envelope.ts`, `specs/tck-support/actors/**` when `<s>`'s
    module requires identity, and files inside `specs/<s>/tck/` (Playwright loads every matching
    file before `--grep` filters, and `npm --prefix specs run typecheck` compiles them all).
  - An actor helper that calls the routes of one module ships with that module; identity owns the
    rest of `specs/tck-support/actors/`. A project ships a helper only with its module, and a
    helper never imports another module's helper (module helper -> `actors/index.ts`, never the
    reverse).
  - A scenario file that imports a cooperating module's helper lives in `specs/<s>/tck/with-<name>/`
    (for example `specs/parameters/tck/with-tenancy/`), where `@<name>` is a tag in `<s>`'s
    `cooperatesWith`. Files outside the folder import no cooperating helper. Every test of such a
    file carries the cooperation tag.
  - Tags are test-level: every test states its full list in its own `{ tag: [...] }`, own tag first
    (`{ tag: ['@parameters', '@tenancy'] }`); no describe-level tags.
  - A `beforeAll`/`beforeEach` that calls a cooperating spec's routes lives in a file whose every
    test carries that cooperation tag.
  - Only tenancy's invitations create a second account.
  - The bootstrap operator's tenant depends on the selection: a scenario not co-tagged `@tenancy`
    asserts neither state.
- Scenarios are written as Given / When / Then from the spec; one scenario per invariant.
- Layout of a vertical's spec and how the runner discovers it: `specs/cookbooks/new-module-contract.md`.

## Test data

- Synthetic only: `tenant-a`, `tenant-b`, `acme`, `user@example.com`, `Jane Doe`, generated UUIDs.
- No real names, emails, tokens, production payloads or exports.
- Secrets in tests are obviously fake (`change-me`, `test-secret`) and never shaped like a real
  provider key.

## Checklist

- [ ] New behavior has an HTTP-level test; a fixed bug has a regression test that fails without the
      fix.
- [ ] Cross-tenant and `422` accumulation cases are covered where they apply.
- [ ] No internal mocks; boundary fakes only.
- [ ] `go vet ./...` and `go test ./...` pass from the component folder.
