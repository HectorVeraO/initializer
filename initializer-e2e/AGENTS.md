# AGENTS.md - initializer-e2e

<!-- AGENTIC-SETUP:AUTOGEN:overview START -->
**Purpose**: Playwright end-to-end suite for `ng-web`: user flows tagged `@smoke` / `@e2e`, run against a running dev server or a deployed environment.

- **Stack**: Playwright / TypeScript
- **Entry point**: `playwright.config.ts`
- **Cross-cutting rules**: see root `AGENTS.md`
<!-- AGENTIC-SETUP:AUTOGEN:overview END -->

## Commands

<!-- AGENTIC-SETUP:AUTOGEN:commands START -->
| Action | Command |
|---|---|
| Lint | `npm run lint` |
<!-- AGENTIC-SETUP:AUTOGEN:commands END -->

## Verification

<!-- AGENTIC-SETUP:AUTOGEN:test-mode-e2e START -->
E2E: **Playwright**. Discipline rules: `.claude/references/test-rules.md`.

### Commands

| Action | Command |
|---|---|
| All E2E projects | `npm test` |
| Single project | `npx playwright test --project=chromium` |
| Install browsers | `npx playwright install chromium` |

Available Playwright projects: `chromium`.

Test philosophy: `.claude/references/test-rules.md`.

### Rules specific to E2E

- **Waits: web-first first.** Use web-first assertions (`expect(locator).toBeVisible()`, `expect(locator).toHaveText(...)`), `waitForURL`, `waitForResponse`, or `expect.poll`. A fixed wait (`waitForTimeout`, a `new Promise(... setTimeout ...)` sleep) is allowed up to 120000 ms and only with a comment on the same line or the line immediately above stating why no condition can be awaited. The `block-protected-paths` hook blocks any other fixed wait in `initializer-e2e/`, and the comment must be part of the same edit. Avoid `waitForLoadState('networkidle')`; use a web-first assertion.
- **Never call `browser.newContext()` bare**: it inherits neither `use.viewport` nor `use.video`, and its `.webm` is never attached to the report.
- **Optional configuration skips, it does not fail.** A spec requiring optional env calls `test.skip(...)` when keys are missing.
- **Recorded contexts**: open a manual context with `newRecordedContext(browser)` and close it with `closeAndAttachVideo(context, '<name>')` (`src/utils/recorded-context.ts`).
- **Viewport and video size share one value**, read once from `INIT_VIEWPORT_WIDTH` / `INIT_VIEWPORT_HEIGHT` (default 1600x900) and reapplied after the `devices[...]` spread in `playwright.config.ts`. Change the env vars, never one side alone.
- **`@manual` is excluded by the config `grepInvert`**, which `--grep` / `--grep-invert` cannot lift. To run one on purpose: `INIT_TEST_GREP_INVERT= npx playwright test <file>` (bash) or `$env:INIT_TEST_GREP_INVERT='none'` then the command (PowerShell deletes a variable set to `''`). Never leave the variable set in `.env`.
- **Local env**: `cp .example.env .env`, then fill it. Commit `.example.env` when you add a key; never commit `.env`.
- **Env guards**: the missing-keys check is the matching `assert<Area>Env()` from `src/env.ts`. Check for an existing guard before reading a bare `process.env` value; add a new one on `checkEnvKeys()` when none fits.
- **Evidence pauses are mandatory whenever the suite has a pause helper.** Find it before writing a spec: `src/utils/evidence.ts` (scaffold evidence mode) or a repo-local equivalent (grep the suite for `evidencePause`). If one exists, every new or changed spec calls it right after the assertion that proves each user-visible state (form filled, success message, persisted state after reload, validation error); it is a no-op on normal runs, so it costs nothing in CI. With `src/utils/evidence.ts`, use `fillWithEvidence` / `clickWithEvidence` for pacing and `captureStep` / `captureElement` for named, report-attached screenshots. Pacing never goes through a new fixed wait in the spec.
- **Tests must FAIL on the integration branch without your fix and PASS with it.** Verify by reverting your code change and watching the test break.
- **Test pages live in the suite's fixtures directory**, not in `examples/` or production directories.
- **No reliance on production data**. Each test sets up its own state.

### When to add an E2E test

- New user-visible workflow
- Fix to a workflow that broke (regression test)
- Cross-environment behavior (SSR vs CSR, dev vs prod)

### Happy-path e2e is required for web changes

A change to a web frontend ships its happy-path e2e here: new for a new flow, updated for a changed one. The happy path covers the primary success route a user takes through the changed flow. If a web change is delivered without one, the developer records why in the PR description (and commit). See `test-rules.md` rule 12.

Baseline capture (Healthy Baseline Rule, root `AGENTS.md`): only when something fails, run `npm test` (one project: `npx playwright test --project=chromium`) on a `git stash -u` baseline and on your change, and diff both outputs under `.me/<slug>/logs/` (recipe: `.claude/references/test-rules.md` rule 7). Rerun a single failure once before treating it as real. If proven pre-existing, STOP and ASK.
<!-- AGENTIC-SETUP:AUTOGEN:test-mode-e2e END -->

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
