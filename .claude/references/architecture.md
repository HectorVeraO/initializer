# Architecture, workflow and conventions: full root text

Passive reference, reached from root `AGENTS.md` "Healthy Baseline Rule", "Library documentation lookup" and "Anti-degradation governance". It holds the full text behind condensed root sections and any section the owner moves out of root to stay within the line budget. Root stays the active rule; where the two disagree, fix both in the same edit.

---

## Healthy Baseline Rule

You begin work assuming the source branch is GREEN: tests pass, lint clean, build succeeds, type-check clean.

### When verification fails after your changes

1. **Failures are YOURS unless proven pre-existing.** No "pre-existing" claim without evidence.

2. **Acceptable evidence** (one of the following):
   - Captured baseline output before editing, with the failing case appearing identically.
   - An open issue or known-flake annotation in the repo.
   - Red CI status on the source branch (or latest integration merge) for the identical case (cite the run link).

3. **Without evidence, fix it.** It is yours.

### When evidence DOES prove pre-existence: STOP and ASK

You do NOT decide whether to fix a pre-existing failure. The developer does. Surface the failure using this format and await an explicit decision:

```
PRE-EXISTING FAILURE [n/N]
- Check: <test/lint/build name>
- Evidence: <baseline run | issue number | CI run link>
- Drift if fixed: <N> file(s) outside scope, <low|med|high> risk, <small|medium|separate-PR>
- Not investigated. Recommendation: <defer | investigate first | fix (only if trivial)>

Decision options:
  (a) fix now in this PR
  (d) defer; will file a follow-up issue
  (n) defer; mention in PR description only
  (i) investigate further before deciding

Default if no answer (headless mode): (n)
```

Rules for the surface:

- **Surface ASAP, before investigating.** Offer "investigate first" only when needed; the developer decides.
- **Default recommendation: defer.** Recommend "fix" only when the fix is trivially small in a file already being modified.
- **Default action when no answer: defer with PR note (n).** Never silently fix.
- **Apply same flow if YOUR change exposed a pre-existing bug** (a test now reaches a previously unreachable broken path, lint catches a long-existing violation, etc.). Treat it identically.

### Authorization marker (required when developer says "fix")

If the developer authorizes (a), the off-scope fix MUST carry an authorization marker in BOTH places:

**1. Commit message** with `chore(unscoped):` prefix, following the project commit message format (`unscoped` is a legal scope, not an exception):

```
chore(unscoped): Fix <the pre-existing failure>

Authorized during work on #<issue> to reach a healthy baseline.
Outside the issue's scope.

Refs: #<issue>
Co-Authored-By: <Model> <model noreply address>
```

The `Refs:` trailer names the issue the work belongs to; drop it when there is none.

The off-scope fix lives in its own commit, not bundled with the in-scope work.

**2. PR description** with a `## Pre-existing fixes` section:

```
## Pre-existing fixes
- <path>: <one-line description> (authorized during work on #<issue>)
```

The marker is a convention, not an automated gate: reviewers do not emit findings if it is omitted.

### Baseline capture strategy

Two valid strategies. Pick based on suite duration:

- **Lazy with stash** (preferred for slow suites): capture baseline only if verification fails after changes. Stash changes (`git stash -u`), run verification on the clean source branch, capture output, unstash (`git stash pop`), and compare.
- **Upfront** (preferred for fast suites, under ~30 seconds): run verification before editing, capture output, and compare after.

Either strategy provides acceptable evidence.

## Agent discipline

The full text lives in `.claude/references/agent-discipline.md`, copied verbatim from the scaffolding skill; root carries the short form. It is not duplicated here.

## Library documentation lookup

When you need authoritative, version-specific API docs for a library this repo uses, query the **context7** MCP, not memorized snippets.

- **Wired in**: see `.mcp.json` at the repo root. Claude Code prompts once for approval on the first session in this project; approve it and it stays connected. `.claude/settings.json` pre-allowlists the tool names, so individual lookups do not prompt.
- **Workflow**: call `resolve-library-id` to find the slug, then `query-docs` with the topic you care about. Older context7 versions name this tool `get-library-docs`.
- **Strong default**: if a change touches a library API and you have not opened the project's source for that area in this session, query context7 before writing the code.
- **When NOT to call it**: trivial questions, things already documented in this repo's docs, anything you can confirm by reading the project's own source.
- **Higher rate limits (optional)**: set `CONTEXT7_API_KEY` in your shell (or in `.claude/settings.local.json` env) for paid-tier limits.
- **Fallback: WebSearch on official docs, never blogs / SO / random gists.** If context7 returns "library not found" or the topic is too narrow for it, fall back to WebSearch scoped to the vendor's official documentation domain (`site:` operators). The official-doc table for this project's stack:

| Stack signal | Official doc domain(s) |
|---|---|
| go | `go.dev`, `pkg.go.dev` |
| node-angular | `angular.dev` |
| `vitest` in deps | `vitest.dev` |
| `@playwright/test` in deps | `playwright.dev` |
| postgres (compose image, resolved at run time) | `www.postgresql.org` |
| Ory Kratos (declared stack, resolved at run time) | `www.ory.com` |
| OpenTelemetry (declared stack, resolved at run time) | `opentelemetry.io` |
| always | `developer.mozilla.org`, `nodejs.org` |

  Cite the source URL whenever you base a code decision on it. **Stack Overflow, Medium, dev.to, random blogs, gists and old GitHub issues are not authoritative.** If the only "answer" you find is on those, surface that uncertainty to the developer before writing code.

## Anti-degradation governance

The root `AGENTS.md` budget is **<= 400 lines**, counted as `wc -l` counts them, and `node .claude/scripts/validate-artifacts.mjs` reports a WARN (`root-line-soft-budget`) above three quarters of it, a WARN (`root-line-tolerance`) above it up to the 10% tolerance (budget * 1.10, rounded down), and an ERROR (`root-line-budget`) past the tolerance. The budget is never above 500; a higher value is itself an ERROR (`root-line-budget-max`). Each subproject index (`<subproject>/AGENTS.md`) and each cookbook fragment (`<subproject>/cookbooks/*.md`) has a budget of **<= 120 lines**, reported as a WARN (`index-line-budget`, `cookbook-fragment-budget`).

- **Placement**: root holds project-wide rules and navigation: what each subproject is, where to read next, and rules that apply across subprojects. Anything specific to one directory lives in that subproject's index or cookbook. A subproject index routes each task to one fragment through its "Read before" table (`| Task | Fragment |`), kept outside AUTOGEN markers; every fragment is routed and every row names a fragment that exists. A fragment over budget is split by task, never trimmed.
- **Generated references are never hand-edited**: every file listed in `.claude/references/.generated.json` is emitted by the scaffolding skill, which records there the sha256 of what it wrote (computed with every CR byte removed, so an LF or CRLF checkout hashes the same). The validator WARNs `generated-reference-edited` when a listed file differs or is missing. Repo-specific facts go to an `AGENTS.md`; a wrong generic rule is fixed in the skill and regenerated.
- **Rule purity**: root and every reference declare active invariants only: schemas, closed vocabularies, regexes, failure criteria. No historical chronicles ("formerly X, now Y"), debate logs or conversational quotes. Transitions belong in git commits and PR descriptions; deliberations belong in ADRs/RFCs. Full rule: `.claude/references/agent-discipline.md`.
- **Author** (whoever edits root): keeps normative rules in root. Operational detail (runbooks, measured inventories, command catalogues, long examples) and history go to `.claude/references/`, leaving at most a one-line pointer in root. A new reference gets that inline pointer in the root section that uses it; only a reference no root section points at gets a row in root's "Passive references on demand" table, with the task that triggers reading it.
- **Hand edits go outside AUTOGEN markers**: a regeneration discards whatever is inside one (see the markers section below).
- **Validator** (`node .claude/scripts/validate-artifacts.mjs`): enforces balanced and unique AUTOGEN markers, no heading duplicated across a marker, the AI-generation disclosure in the commit and PR formats, and the root line budget, and warns on the soft root, index and fragment budgets, unrouted fragments and dangling routing rows, and edited generated files.
- **Owner** (the team member who owns the agent docs): approves every move between root and a reference. **Detail is never deleted to meet the budget: it moves**, verbatim, to the reference whose row matches the task that needs it. Where no reference fits, it goes below the "Moved from root" sentinel at the end of this file.

Where moved content goes:

| Content | Destination |
|---|---|
| Full text of a condensed root section | this file |
| Historical Lessons Learned entries | `.claude/references/troubleshooting.md` |
| Full Code Path Parity rows, with hash commands | `.claude/references/code-path-parity.md` |
| Anything else operational | this file, below the sentinel, under the root heading it came from |

### `AGENTIC-SETUP:AUTOGEN` markers: what they mean now

A matched pair of `AGENTIC-SETUP:AUTOGEN` HTML comments (one suffixed `START`, one `END`, both naming the same block) marks a region the scaffolding skill may regenerate, **discarding whatever is inside**.

The marker syntax is described here, not quoted: `validate-artifacts.mjs` and `pl-validate-agent-docs` count a literal example as a real marker. Read a real one in root `AGENTS.md` for the exact form.

- **Content that is a measured fact about this repo does not live inside a marker.** Root emits "Branch types", "Verification before push", "Cross-cutting patterns", "Scratchpad" and "Per-subproject guidance" without markers for that reason, and each subproject index emits its "Read before" routing table the same way. When you correct a block that is still wrapped and the correction is a measured fact, remove its two marker comments in the same edit.
- **Markers stay only where the content really is a template**: the overview, the policy blocks copied from the scaffolding skill's canonical formats, and the `*-header` blocks that hold nothing but a pointer to a format reference.
- **Both "Add ... below this line" sentinels are load-bearing** and stay: Lessons Learned entries and Code Path Parity rows go *below* them, outside any marker.
- **Opting out of regeneration**: a section whose heading is present but whose marker pair was removed is hand-owned; a re-run diffs it and rewrites only a sentence that contradicts a current generated rule, replacing it with that rule and reporting the rewrite. A section with neither heading nor markers is emitted again. Conditional sections are re-evaluated on every run.
- **A regeneration is not idempotent against these files.** If you run the scaffolding skill again, diff the result and keep the hand-authored blocks.

---

## Moved from root

<!-- Add entries below this line: moved sections, each under the root heading it came from. The skill only appends below (a new heading, skipped when it exists) and never edits existing entries. -->

## Platform rules

Full text of the root `AGENTS.md` "Platform rules". Root keeps the short form; this section is the reference.

### Trunk-based development

- One integration branch, `main`. Every merge to `main` must leave it releasable: on a red `main`, revert first, then fix.
- Deploy is not release. Deploying installs a build; releasing switches a capability on for a tenant or for everyone through configuration.
- The same build artifact is promoted from staging to production; no per-environment branches, no cherry-picks between environments.
- Most changes carry no flag:

| Change | Flag |
|---|---|
| Bugfix or refactor (`A -> A'`) | none; fix `A` with a regression test |
| Small complete change (one PR) | none |
| Inner layers of a new feature, unreachable from any route (Keystone Interface) | none |
| Replacing an active implementation (`A'` vs `B`) | one release flag |
| Multi-day change to a screen in use | one release flag |
| Gradual rollout to selected tenants | one release flag |
| Switching off a degraded external integration without a deploy | an ops flag |

Agent rules:

1. **No flag on bugfixes or refactors.** Modify `A` directly with its regression test.
2. **Inside-out construction (Keystone Interface).** When a task spans several PRs, persistence and domain slices merge first with their tests, disconnected from routes and screens, without flags. Only the final slice that exposes the route or screen adds a single flag, and only when a gradual release is needed.
3. **Branch by Abstraction (single seam).** When `A'` and `B` coexist, isolate them behind one interface with one routing point that reads the flag. Never scatter `if (flag)` through business methods. Never nest release flags. One active replacement per module: `C` waits until `B` fully replaced `A'` and `A'` is deleted.
4. **Additive evolution (Expand-First).** No PR removes or renames a database column, table, JSON property or event field that current code uses. Migrations follow Expand -> Dual-Write (only when `A'` and `B` alternate on the same data) -> Contract (a later release, after the old code is gone).
5. **Close with cleanup.** Every feature that needed a release flag ends with a cleanup PR that deletes the flag and the legacy implementation `A'`.

### Switch types

- **Release flags**: temporary (1 to 4 weeks), owned, with `expires_at`. Removed from the code once `B` serves every tenant.
- **Ops flags (kill-switches)**: permanent; switch off a degraded external integration or enable a degraded mode without a deploy.
- **Entitlements**: what a tenant's commercial plan includes. Never modelled as feature flags.

### `flags.yaml` and evaluation

- `flags.yaml` at the repo root declares every flag the code evaluates: `key` (snake_case, unique), `type: release | ops`, `default`, `expires_at` (release only), `description`.
- A release flag declares `default: false` and an `expires_at` at most 30 days after it is added. An expired release flag is removed, not silently extended.
- Precedence: tenant override `(key, tenantId) -> enabled`, then global override `key -> enabled`, then the manifest `default`. An unknown key evaluates to `false` (default-closed).
- Overrides live in their own tables (`flg_overrides`, `flg_tenant_overrides`); a flag definition never embeds lists of tenants.
- An override on a key that is not in `flags.yaml` is rejected with `404 FLAG_NOT_DEFINED_IN_MANIFEST`.
- Flag administration routes require a platform operator (`403 PLATFORM_OPERATOR_REQUIRED` otherwise). Tenant users only receive the resolved `{ [key]: boolean }` map for their active tenant, in the session responses.
- The backend enforces every flag; hiding a control in `ng-web` is never the only guard. A new flag is exercised switched on in staging before it is switched on for the first tenant.
- Queued work survives a flag change: payloads change additively or carry a `schema_version`.

### Contracts, SemVer and the catalog

- Each module has `specs/<module>/openapi.yaml` (or `asyncapi.yaml`) and `specs/<module>/tck/`. Contract and TCK share one SemVer tag, `spec-<module>-vX.Y.Z`: MAJOR for a breaking wire change, MINOR for a backward-compatible extension, PATCH for test or text fixes.
- A breaking wire change carries the `BREAKING CHANGE:` trailer in its commit. `gates contracts` (CI job `gates`) runs `oasdiff breaking --fail-on ERR --flatten-allof` on every `specs/<spec>/openapi.yaml` changed since `git merge-base <base-rev> HEAD` (a spec absent there is new and skipped). A breaking change fails `CONTRACT_BREAKING_UNMARKED` unless a commit touching `specs/<spec>/` carries a `BREAKING CHANGE:` footer (`feat!:` is not a marker), and `CONTRACT_BREAKING_NOT_MAJOR` unless `version` in `spec.manifest.json` has a higher MAJOR than at the merge base. While `versioning.json` has `preRelease: true`, `CONTRACT_BREAKING_NOT_MAJOR` is not reported; `CONTRACT_BREAKING_UNMARKED` is.
- Every spec and module carries a manifest `version`. `gates versions` (CI job `gates`) compares HEAD with `git merge-base <base-rev> HEAD`: a component whose files changed (spec folder, or the longest-prefix `files` entry of a module manifest; `exclude` entries and the composition roots `go-srv/cmd/server/modules.go`, `ng-web/src/app/platform/modules.ts` belong to no component) must carry a higher `version` (`VERSION_NOT_BUMPED`), no version may drop (`VERSION_DECREASED`), versions are `X.Y.Z` (`VERSION_INVALID`) and no entry is claimed by two manifests (`COMPONENT_PATH_OVERLAP`). While `preRelease` is `true`, `VERSION_NOT_BUMPED` and `VERSION_DECREASED` are not reported and every version other than `0.1.0` fails `VERSION_NOT_PRERELEASE`; `gates tags` prints nothing. `gates tags` lists `<component>-v<version>` names with no git tag; `.github/workflows/tags.yml` pushes them on `main`.
- Implementations are versioned separately from contracts and declare the contract version they implement (`implementsSpec`) in a module manifest. Stacks may move at different speeds; each is tested against the TCK tag it declares.
- The contract changes in the same PR as the code that implements it.

### Pre-release and the first release

- `versioning.json` at the repo root is `{ "schema": 1, "preRelease": <bool> }`; a missing file means `false`. `tooling/gates` reads it from the `--root` working tree in `gates versions`, `gates contracts`, `gates migrations` and `gates tags`; a malformed file exits 2.
- While `preRelease` is `true`: every manifest `version` and `implementsSpec` value is `0.1.0`, every range `^0.1.0`; no PR bumps a version; `VERSION_NOT_BUMPED`, `VERSION_DECREASED`, `CONTRACT_BREAKING_NOT_MAJOR`, `MIGRATION_MODIFIED` and `MIGRATION_OUT_OF_ORDER` are not reported; `gates tags` prints no tag name, so `tags.yml` pushes no tag. No component tag exists while `preRelease` is `true`: the old tags were deleted, and `gates tags` exits 1 with `TAG_CONTENT_MISMATCH` for a tag `<component>-v<version>` of a component's current version whose commit holds other content of that component (the check runs with `preRelease` on or off).
- The first release happens only when the repository owner names it, in one PR on its own:
  1. `versioning.json` becomes `{ "schema": 1, "preRelease": false }`.
  2. Every module and spec manifest `version`, every `info.version` in `specs/*/openapi.yaml` and every `implementsSpec` value becomes `1.0.0`; every `requiresCore` and `requiresModules` range becomes `^1.0.0`. Nothing else changes in that PR, no migration included.
  3. Before the merge, the release PR passes `gates tags` (CI job `gates` runs it): a remote tag named `<component>-v1.0.0` fails it, so the owner deletes any such tag first. A tag that already points at the component's content is not an error.
  4. After the merge, every `<component>-v1.0.0` tag points at the merge commit.
- From then on the bump, MAJOR and migration-immutability rules apply.

### Wire format

- Every `/api/v1/` response body is `ApiEnvelope<T>`: `{ "data": ..., "meta": { "traceId": "...", "timestamp": "<RFC 3339 UTC>" }, "error": null | { "code", "message", "issues": [] } }`.
- Invalid input on `POST`, `PUT` or `PATCH` collects every problem and answers one `422` with all of them in `error.issues[]`.
- Error codes are SCREAMING_SNAKE constants and part of the contract; renaming one is a breaking change.
- Lists paginate and filter through explicit query parameters (`page`, `size`, `sort`); no query language parser.

### Tenant isolation

- The active tenant is resolved from the stateless session token (human) or the API key (machine), never from a request body.
- Every operation by `:id` filters by the active tenant and answers `404` for another tenant's resource, so the response never reveals that it exists.
- An optional `tenantId` in an M2M payload accepts the tenant UUID or slug and must match the API key's tenant (`403 TENANT_MISMATCH`).
- Without the tenancy module every principal and every row is in the one global scope. With tenancy, a row whose `tenant_id` is NULL (a global API key) is operator-only.

### Modules and core ports

Backend rules (R1-R6), the same in every selection:

1. **Imports.** A module imports only core (`core/...`, never `core/internal`) and its own subtree (`go-srv/internal/platform/core/boundary_test.go` rule 2).
2. **Cooperation only through core ports.** A module publishes a port with `Builder.Provide*` in `Provide` and reads it from `Host` in `Mount`, `Start` or a handler. A module never branches on which other modules are installed. The `No*` default answers "absent" and fails closed (no tenant, access denied, not found), never a panic.
3. **No SQL crosses a module.** A module's SQL names only tables with its own prefixes (boundary test rule 6). Account data reaches tenancy only through `accounts.Directory` and `accounts.Provisioner`; tenant data reaches identity and api-keys only through `access.TenantAccess` and `access.TenantDirectory`.
4. **No cross-module foreign key.** A column that holds another module's id is a plain `uuid` with no `REFERENCES`.
5. **Spec paths never depend on the selection.** Every module mounts the same routes in every selection; the answers change.
6. **Check order across the session port is pinned.** Identity raises `ACCOUNT_LOCKED_OR_DISABLED` before it calls `TenantAccess`; tenancy raises `TENANT_ACCESS_DENIED`, `TENANT_SUSPENDED`, `MEMBERSHIP_REVOKED` in that order.

Hard edges, identical in both stacks: `tenancy -> identity`, `api-keys -> identity`, `parameters -> identity`, `feature-flags -> identity`. Identity requires nothing; `core` is implied. Table prefixes: `iam_` identity, `ten_` tenancy, `apk_` api-keys (`go-srv/cookbooks/data-migrations.md`).

Go core ports (`go-srv/internal/platform/core/`):

| Port | Methods | Default (fails closed) |
|---|---|---|
| `access.SessionResolver` | `Present`, `Resolve` | `access.NoSessions` |
| `access.APIKeyResolver` | `Present`, `Resolve` | `access.NoAPIKeys` |
| `access.TenantAccess` | `Select`, `Grant`, `Remember`, `Memberships` | `access.NoTenantAccess` |
| `access.TenantDirectory` | `TenantExists`, `Tenant` | `access.NoTenants` |
| `accounts.Directory` | `Get`, `ByEmail` | `accounts.NoDirectory` |
| `accounts.Provisioner` | `Provision`, `GrantsOperator` | `accounts.NoProvisioner` |
| `featureflag.Evaluator` | `Enabled`, `Snapshot` | `featureflag.Disabled{}` |

`access.Policy.TenantlessOperator` lets a platform operator with no active tenant pass a `MinRole` route; a session with an active tenant is still held to `MinRole`, and an API key never passes it. Scope levels of an API key scope: `access.ScopeTenant` (only a tenant-bound key may hold it) and `access.ScopeAny`.

The tenancy `Start` hook stops the process when `APP_BOOTSTRAP_TENANT_SLUG` is set, no tenant exists and no account has `APP_BOOTSTRAP_ADMIN_EMAIL`.

Web shell (`ng-web/src/app/platform/`): `core` holds the shell contract and the ports; each module folder (`identity`, `tenancy`, `api-keys`, `parameters`, `feature-flags`) exports a `WebModule`; `modules.ts` is the only generated file. A module imports core only through `@/platform/core` and never another module (`ng-web/eslint.config.js`); it reaches another module only through a port. The `WebModule` fields and the topbar slots are in `ng-web/cookbooks/new-feature.md` -> "Platform modules".

| Port (core barrel) | Provided by | Consumed by | Default |
|---|---|---|---|
| `ACCESS_PROVIDER` | identity (`SessionAccess`) | `requireAccess`, menu, topbar items, tenancy (switcher, no-tenant, accept, pages), api-keys page, parameters list | `OPEN_ACCESS` |
| `FLAG_SOURCE` | identity (`SessionAccess`, reads `SessionView.flags`) | `flagGuard`, the `flag` clause | `NO_FLAGS` |
| `TENANT_DIRECTORY` | tenancy (`TenantsApi`) | feature-flags flag-detail | `null` |
| `TENANT_SELECTION` | tenancy (`TenancySelection`) | `requireAccess`, menu, identity's session interceptor (403 navigation) | `null` (single-tenant console) |

At most one installed module binds a port, else `PLATFORM_DUPLICATE_PORT:<port>:<id>,<id>`.

### Copy-and-own and `initializer.lock.json`

- A generated project owns the code the generator copied into it and may change it; this repository never ships empty interfaces reserved for modules that do not exist yet.
- Module paths are stable: `go-srv/internal/platform/{core,identity,tenancy,apikeys,parameters,featureflags}/` (Go), `ng-web/src/app/platform/{core,identity,tenancy,api-keys,parameters,feature-flags}/` (Angular), and the module's migrations `<timestamp>_<prefix>_<name>.sql`.
- A generated project records, per module, the base version it copied and the SHA-256 hash of every delivered file in `initializer.lock.json`. An agent upgrading a module compares the project against that file, keeps the local changes, and runs the module's TCK.

### Trace correlation

- Every HTTP request and every asynchronous execution continues the incoming W3C `traceparent` context or opens a new one.
- Structured logs and spans carry `trace_id`, `span_id`, `tenant_id` and `actor_id` once the request context knows them.
- The `X-Trace-Id` response header and `meta.traceId` hold the same trace id, with or without an incoming `traceparent`.

### Toolchain and ports

- Go, Node, gitleaks, lefthook and oasdiff are pinned in root `mise.toml`. Run `mise install` once per clone; never rely on a globally installed version.
- Local host ports come from band `101xx` (10100-10199); the per-service map is `.deploy/local/README.md`.
