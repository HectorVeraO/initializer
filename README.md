# initializer

initializer is a deterministic project generator in the spirit of Spring Initializr: pick a tech stack and the features you need, and it assembles a compilable project with only those modules and no dead code. Its catalog, Gatbase, holds modular, decoupled, composable capabilities (identity with Ory Kratos and a stateless BFF session, flat multi-tenancy, M2M API keys, typed parameters, trunk-based feature flags, OpenTelemetry), each defined by a language-agnostic contract and certified by a black-box TCK. Go (`go-srv`) and Angular (`ng-web`) are the first stack implementations; a generated project owns the copied code and records each module's base version in `gatbase.lock.json`.

## Status

Pre-alpha. The first catalog modules (`core`, `core-auth`, `parameters`, `feature-flags`) are on `main` for both stacks, and the generator writes go-srv and ng-web projects that pass their module TCKs in CI.

Terms (TCK, catalog, selection, active tenant and the rest): `GLOSSARY.md`.

## Layout

| Path | Holds |
|---|---|
| `specs/<module>/` | A module's contract (`openapi.yaml`) and its black-box TCK. |
| `go-srv/` | Go stack: one package tree per module under `internal/gatbase/<module>/`. |
| `ng-web/` | Angular stack: one folder per module under `src/app/gatbase/<module>/`. |
| `initializer-e2e/` | Playwright UI suite, independent of the frontend stack. |
| `tooling/generator/` | The `initializer` CLI: catalog build, project generation and verification. |
| `tooling/gates/` | Repo checks for `flags.yaml`, migrations, contracts and versions. |
| `.deploy/local/` | Local Docker Compose stack and port map. |

## Develop

1. Install the pinned toolchain: `mise install`.
2. Enable the pre-commit leak gate: `lefthook install`.
3. Start local infra: see `.deploy/local/README.md`.

Contributor and agent rules: `AGENTS.md`. Agent setup: `ONBOARDING.md`.
