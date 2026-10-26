# AGENTS.md - go-srv

<!-- AGENTIC-SETUP:AUTOGEN:overview START -->
**Purpose**: Go stack of the catalog: the Go HTTP API and BFF implementation of each Gatbase module (identity, tenancy, API keys, parameters, feature flags, OpenTelemetry) that the generator copies into Go projects.

- **Stack**: Go 1.26 / stdlib `net/http` / PostgreSQL 18
- **Entry point**: `cmd/server/main.go`
- **Cross-cutting rules**: see root `AGENTS.md`
<!-- AGENTIC-SETUP:AUTOGEN:overview END -->

## Read before

Read the fragment routed to your task before building here; this `AGENTS.md` is the index and the fragments under `cookbooks/` are the source of truth for patterns.

| Task | Fragment |
|---|---|
| Adding or changing a module, route, handler or error response | `cookbooks/architecture.md` |
| Creating or changing a table, column, index or migration | `cookbooks/data-migrations.md` |
| Adding, evaluating or removing a feature flag; splitting a feature into PRs | `cookbooks/tbd-feature-flags.md` |
| Writing or changing tests, or a TCK scenario | `cookbooks/testing-tck.md` |

## Commands

<!-- AGENTIC-SETUP:AUTOGEN:commands START -->
| Action | Command |
|---|---|
| Develop | `go run ./cmd/server` |
| Lint | `go vet ./...` |
| Build | `go build ./...` |
<!-- AGENTIC-SETUP:AUTOGEN:commands END -->

## Verification

<!-- TODO(hvera): add targeted-test commands for go test. -->
<!-- AGENTIC-SETUP:AUTOGEN:test-mode START -->
Test runner: **go test** (`go test ./...`); no dedicated stub.

Before declaring a change complete:

- **Tests**: `go test ./...`
- **Lint**: `go vet ./...`
- **Build**: `go build ./...`
- **Read the full modified file**, not just the diff.
- **Manual smoke (boot the service)**: start it in the background (`go run ./cmd/server`, then `GET http://localhost:10100/health/live`), hit the health route, then stop it. Describe the manual scenario in the PR description.
- **Review the diff**: run `/pl-review-working-tree` before pushing - it reviews the uncommitted diff for correctness AND for violations of this AGENTS.md cookbook.

The **Healthy Baseline Rule** (root `AGENTS.md`) applies to lint / build / type-check too: a failure after your changes is yours unless evidence proves it pre-existing. If proven pre-existing, STOP and ASK - do not silently fix.

When a test suite is added, re-run the scaffold to replace this section.
<!-- AGENTIC-SETUP:AUTOGEN:test-mode END -->


## Lessons Learned

<!-- AGENTIC-SETUP:AUTOGEN:lessons-learned-header START -->
Subproject-specific incidents. Format: see `.claude/references/lessons-learned-format.md`.
<!-- AGENTIC-SETUP:AUTOGEN:lessons-learned-header END -->

<!-- Add entries below this line. -->

## Code Path Parity

<!-- AGENTIC-SETUP:AUTOGEN:code-path-parity-header START -->
Files inside this subproject that must stay in sync. Format: see `.claude/references/code-path-parity-format.md`.
<!-- AGENTIC-SETUP:AUTOGEN:code-path-parity-header END -->

<!-- Add parity rows below this line. -->
