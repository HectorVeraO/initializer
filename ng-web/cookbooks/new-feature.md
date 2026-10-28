# ng-web -- Adding a feature

Read before adding or changing a feature, component, service or route. The Sakai demos under `src/app/pages/uikit` show every Optimus UI component in use: copy from them. The shell (`src/app/layout`) and the demos (`src/app/pages`, `src/app/dev`) are the template's UI documentation: never delete them.

The file map lives in `AGENTS.md` -> "File map for a new feature"; this fragment defines the conventions for each file.

## Feature conventions

- **Layout**: one folder per feature, `src/app/<feature>/`, with `<feature>.routes.ts` (default export or named `Routes`), loaded lazily from `src/app.routes.ts` with `loadChildren`. Platform modules live in `src/app/platform/<module>/`.
- **Components**: standalone, `inject()` for dependencies, `ChangeDetectionStrategy.OnPush` for new components, selector prefix `app` or `p`. Templates use `@if` / `@for` (with `track`), never `*ngIf` / `*ngFor`.
- **State**: signals (`signal`, `computed`, `input`, `output`); RxJS only for HTTP and event streams. Convert once at the boundary.
- **UI**: import Optimus UI components one by one from `@openng/optimus-ui/<component>` (for example `@openng/optimus-ui/table`). Style with Tailwind 4 utilities and the theme tokens (`bg-surface-0`, `text-primary`); no hardcoded colors. Reuse `core/ui` (`page-header`, `data-table`, `list-state`, `form-section`, `confirm-delete`) before writing a new widget.
- **Text**: every string goes through Transloco (`transloco` pipe or `TranslocoService`), keys added to BOTH `src/assets/i18n/es.json` and `en.json` (a spec fails when their keys differ). Spanish is neutral or Chilean, never voseo. Platform module strings go to `src/app/platform/<module>/i18n/{en,es}.json`, not the root files.
- **HTTP**: services use `HttpClient`; paths start with the `/api/v1` prefix. The response is `ApiEnvelope<T>` (`core/envelope`): read `data`. Failures: `core/api/api-error` reads `error.code` and the `422` `error.issues[]`; `SubmitErrors` keeps them for a form; `IssueDisplayService` shows them. Pass `SKIP_ERROR_TOAST` when the view shows its own error state. Query params for lists are `page`, `size`, `sort` (`buildParams`).
- **Forms**: reactive forms. A `422` shows every issue at once, next to its field.
- **Config**: deployment values come from `RuntimeConfigService` (`src/assets/config/runtime-config.json`), never from constants.
- **Style**: Prettier (`printWidth` 250, single quotes, no trailing commas); ESLint runs with `--max-warnings=0`. A blank line follows every `const` / `let` block and precedes every `return`.

## Common pitfalls

1. Subscriptions that outlive the component: use `takeUntilDestroyed()` or the `async`/signal path.
2. Mutating an input object.
3. A string missing in one language: the parity spec fails.
4. Hardcoded API URLs or hosts.
5. `document.querySelector` for component state: use `Renderer2`, `ElementRef` or signals.
6. A feature that imports another feature's folder: share through `core/` only.
7. Editing `dev.routes.ts` / `dev.menu.ts` without the matching `*.prod.ts` file (production builds swap them).

## Implementation checklist

- [ ] Feature folder, `<feature>.routes.ts` and the lazy route exist
- [ ] Components are standalone and every string has a key in `es.json` and `en.json`
- [ ] HTTP goes through a service; errors use the envelope helpers
- [ ] Specs cover the observable behavior (`cookbooks/testing.md`)
- [ ] `npm run lint`, `npm run format:check`, `npm run build` and `npm test -- --watch=false` pass
