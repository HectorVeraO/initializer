# AGENTS.md - ng-web

<!-- AGENTIC-SETUP:AUTOGEN:overview START -->
**Purpose**: Angular stack of the module catalog: the console screens of each platform module (login through the BFF session, tenant switch, module administration) that the generator copies into Angular projects.

- **Stack**: Node.js 24.22.1 / Angular 22.2 / TypeScript
- **UI libraries**: Optimus UI 2.0.2 (`@openng/optimus-ui`, a PrimeNG fork), Tailwind CSS 4, Transloco. The Sakai demos under `src/app/pages/uikit` are the component reference: copy their markup before inventing one.
- **Cross-cutting rules**: see root `AGENTS.md`
<!-- AGENTIC-SETUP:AUTOGEN:overview END -->

## Read before

Read the fragment routed to your task before building here; this `AGENTS.md` is the index and the fragments under `cookbooks/` are the source of truth for patterns.

| Task | Fragment |
|---|---|
| Adding or changing a feature, component, service or route | `cookbooks/new-feature.md` |
| Writing or changing tests | `cookbooks/testing.md` |

## Commands

<!-- AGENTIC-SETUP:AUTOGEN:commands START -->
| Action | Command |
|---|---|
| Develop (port 10120) | `npm start` |
| Lint | `npm run lint` |
| Format check | `npm run format:check` |
| Build | `npm run build` |

Available environment configurations: `production`, `development`.
<!-- AGENTIC-SETUP:AUTOGEN:commands END -->

## File map for a new feature

<!-- AGENTIC-SETUP:AUTOGEN:file-map START -->
| # | File | Purpose |
|---|---|---|
| 1 | `src/app/<feature>/<feature>.component.ts` | Component class |
| 2 | `src/app/<feature>/<feature>.component.html` | Template |
| 3 | `src/app/<feature>/<feature>.component.scss` | Styles |
| 4 | `src/app/<feature>/<feature>.service.ts` | Service for HTTP / state |
| 5 | `src/app/<feature>/<feature>-routing.module.ts` | Routes (lazy-loaded preferred) |
| 6 | `src/app/<feature>/<feature>.module.ts` | Feature module |

Conventions for each file: `cookbooks/new-feature.md`, "Feature conventions".
<!-- AGENTIC-SETUP:AUTOGEN:file-map END -->

### Layout of this Angular version

Angular 22, standalone components only, no NgModules. Files are named `<name>.component.ts` / `.html` / `.scss` (the Sakai layout and demos use `<name>.ts`), routes live in `<feature>.routes.ts`, and the `@/` alias points to `src/app`. The Sakai shell sits in `src/app/layout`, shell pages in `src/app/pages`, developer demos in `src/app/dev` (replaced by empty files in production builds), generic code in `src/app/core`, and each feature in its own folder `src/app/<feature>/`, lazy-loaded from `src/app.routes.ts`. The platform tree `src/app/platform/` holds `core` (the shell contract and its ports) and one folder per module (`identity`, `tenancy`, `api-keys`, `parameters`, `feature-flags`); `modules.ts` there is the only generated file. Module fields, ports and import rules: `cookbooks/new-feature.md` -> "Platform modules". Follow this layout where the file map above names `*.module.ts` or `*-routing.module.ts` files: use `<feature>.routes.ts` instead.

## Verification

<!-- AGENTIC-SETUP:AUTOGEN:test-mode START -->
Test runner: **Vitest**. Discipline rules: `.claude/references/test-rules.md`.

### Commands

| Action | Command |
|---|---|
| All tests | `npm test -- --watch=false` |
| Single file | `npm test -- --watch=false --include src/app/home/home.component.spec.ts` |
| Pattern match | `npm test -- --watch=false --filter '<pattern>'` |

The full suite runs in CI, or before pushing where no CI runs it (root `AGENTS.md` "Verification before push"). Locally, run only the tests relevant to your change.

### Which tests to run when you change what

| If you changed... | Run these tests |
|---|---|
| `src/app/<feature>/<name>.ts` | `npm test -- --watch=false --include src/app/<feature>/<name>.spec.ts` |
| several files of `src/app/<feature>/` | `npm test -- --watch=false --include src/app/<feature>` |
| `src/app.config.ts`, `src/app.routes.ts` | `npm test -- --watch=false` |

### Verification before push

1. Run the targeted tests above
2. `npm run build`
3. `/pl-review-working-tree` on the uncommitted diff (correctness + cookbook compliance)

Test fixtures and baseline capture: `cookbooks/testing.md` -> "Testing".
<!-- AGENTIC-SETUP:AUTOGEN:test-mode END -->

## Common pitfalls (summary)

<!-- AGENTIC-SETUP:AUTOGEN:common-pitfalls START -->
- Every subscription is cleaned up on destroy, and inputs are never mutated.
- API URLs come from environment config, never hardcoded.

Full list with consequences: `cookbooks/new-feature.md`, "Common pitfalls".
<!-- AGENTIC-SETUP:AUTOGEN:common-pitfalls END -->

## Lessons Learned

<!-- AGENTIC-SETUP:AUTOGEN:lessons-learned-header START -->
Format: see `.claude/references/lessons-learned-format.md`.
<!-- AGENTIC-SETUP:AUTOGEN:lessons-learned-header END -->

<!-- Add entries below this line. -->

## Code Path Parity

<!-- AGENTIC-SETUP:AUTOGEN:code-path-parity-header START -->
Format: see `.claude/references/code-path-parity-format.md`.
<!-- AGENTIC-SETUP:AUTOGEN:code-path-parity-header END -->

<!-- Add parity rows below this line. -->
