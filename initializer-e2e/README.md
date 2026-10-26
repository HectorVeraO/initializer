# initializer-e2e

Playwright end-to-end suite for **ng-web (Angular 22)**. This is the **functional base** — config, fixtures, Page Object Model, env loader, and CI are wired and green, but it ships **no real tests yet**. Developers (and agents) add tests here; a web change ships its happy-path e2e (root `AGENTS.md` -> "E2E for web changes"; `.claude/references/test-rules.md` rule 12).

## Setup

```bash
npm install
npm run install:browsers      # Chromium
cp .example.env .env          # then fill the base URL + credentials
```

## Commands

| Action | Command |
|---|---|
| All tests | `npm test` |
| Smoke (`@smoke`) | `npm run test:smoke` |
| E2E (`@e2e`) | `npm run test:e2e` |
| Headed / UI / Debug | `npm run test:headed` / `test:ui` / `test:debug` |
| Record selectors | `npm run codegen` |
| Type check / Lint | `npm run typecheck` / `npm run lint` |
| Open report | `npm run report` |

## Conventions

- **POM**: selectors + intent methods live in `src/pages/`. Specs import `{ test, expect }` from `src/fixtures/test` (never from `@playwright/test` directly).
- **Tags**: exactly one primary per test (`@smoke` / `@e2e` / `@crud` / `@file`) + free annotations (`@issue-<N>`, `@flaky`, `@manual`). `@manual` is excluded from every run by the config `grepInvert`; `--grep @manual` cannot lift it. Run one on purpose with `INIT_TEST_GREP_INVERT= npx playwright test <file>` (bash) or `$env:INIT_TEST_GREP_INVERT='none'` (PowerShell).
- **Selectors**: stable `id` > ARIA role/text > `data-testid`. No fragile XPath/CSS.
- **Waits**: web-first first — `expect(locator).toBeVisible()`, `waitForURL`, `waitForResponse`, `expect.poll`. A fixed `waitForTimeout` is allowed up to 120000 ms and only with a comment on the same line or the line above saying why (a hook enforces it). No `waitForLoadState('networkidle')`.
- **Manual contexts**: never `browser.newContext()` bare. Use `newRecordedContext(browser)` and close with `closeAndAttachVideo(context, '<name>')` (`src/utils/recorded-context.ts`), or the context records nothing and its video never reaches the report.
- **Video**: viewport and video size both come from `INIT_VIEWPORT_WIDTH` / `_HEIGHT` (default 1600x900). Local runs capture everything (`.example.env` sets `on`); CI keeps the failure-only defaults.
- **Optional config**: a spec that needs optional env guards itself with an `assert<Area>Env()` from `src/env.ts` and calls `test.skip(...)` when it fails, instead of failing.
- **Env-driven**: URLs/credentials/timeouts from `.env`. Never hardcode.

## First test

Replace `tests/smoke/example.spec.ts` (currently skipped) with a real happy-path smoke once you know the app's selectors. Use `npm run codegen` to discover them.
