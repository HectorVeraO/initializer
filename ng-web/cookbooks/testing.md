# ng-web -- Testing

Test philosophy: `.claude/references/test-rules.md`. Test observable behavior (rendered text, emitted values, HTTP requests, navigation), never internals.

## Setup

- Runner: Vitest through `ng test` (builder `@angular/build:unit-test`, jsdom). `describe`, `it`, `expect` and `vi` are globals (`tsconfig.spec.json` types).
- Specs sit next to the code: `<name>.spec.ts`. They import through the `@/` alias.
- Translations: `TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })` with `es` imported from `src/assets/i18n/es.json`. Assert the Spanish text.
- Shared helpers: `src/app/core/testing/ui-testing.ts` (`configureFeatureTest`, `render`, `renderRoute`, `settle`, `byTestId`, `textOf`, `typeInto`, `expectCall`, `expectNoCall`, `stubMatchMedia`, `stubCanvas`). Reuse them; add a helper there when two specs need it.
- Select elements with `data-testid`, not CSS classes of the UI library.

## Boundaries

- Mock only at boundaries: HTTP (`provideHttpClient()` + `provideHttpClientTesting()`, then `HttpTestingController`), `MessageService` for toasts, the router through `provideRouter([...])`, the clock and randomness.
- Never spy on the unit under test or on its internal helpers. A test that fails when internals change but behavior does not is a change detector: delete it.
- jsdom lacks `matchMedia` and canvas: call `stubMatchMedia()` before rendering overlays and `stubCanvas()` before charts.
- Call `HttpTestingController.verify()` in `afterEach` for specs that issue requests.

## Data

- Synthetic only: `user@example.com`, `tenant-a`, generated ids. Never a real person's data or a copy of a production payload.
- API failures use the envelope: `{ data: null, meta: { traceId, timestamp }, error: { code, message, issues: [{ field, code, message }] } }`.

## What to cover

- A component: the text and controls it renders for each state (loading, empty, error, data) and what a click or input does.
- A service: the request it sends (method, URL, params) and what it returns for a success and for each failure.
- A guard or interceptor: the navigation or side effect for each status/code.
- New translation keys need no spec: `src/app/core/i18n/i18n-parity.spec.ts` compares `es.json` and `en.json`.

## Commands and baseline

- All: `npm test -- --watch=false`. One file: `npm test -- --watch=false --include <path>`.
- Baseline capture (Healthy Baseline Rule, root `AGENTS.md`): when a test fails after your change, run the suite on a `git stash -u` baseline and on your change, and diff both outputs under `.me/<slug>/logs/` (recipe: `.claude/references/test-rules.md` rule 7). If proven pre-existing, STOP and ASK.
- Core-only shared fixtures go in `src/testing/fixtures` (`provideFakeAccess`, `http-testing`, `api-envelope`), never in production folders. A fixture only one module's specs use lives in that module's `testing/` folder: it ships with the specs, and core-owned code cannot import a module.
