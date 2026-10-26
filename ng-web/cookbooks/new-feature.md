# ng-web -- Adding a feature

Reference for agents building in this Angular app. Read before implementing any feature, component, service, or route. Canonical example: TODO(hvera): name the feature new work should copy.

The file map lives in `AGENTS.md` -> "File map for a new feature"; these fragments define the conventions for each file.

## Feature conventions

- Standalone components are preferred for new code where the project has migrated. Otherwise stay consistent with the existing module pattern.
- Forms: reactive forms (`FormBuilder`, `FormGroup`) over template-driven.
- HTTP: services use `HttpClient`; never call fetch directly.
- State: signals where Angular version supports it; otherwise RxJS subjects.

## Common pitfalls

1. Missing `OnDestroy` cleanup: RxJS subscriptions leak.
2. Mutating an input object: breaks `OnPush` change detection.
3. Hardcoded API URLs: must come from environment configuration.
4. Direct DOM manipulation via `document.querySelector`: use `Renderer2` or `ElementRef`.
5. Forgetting `trackBy` on `*ngFor` for large lists: triggers a full re-render on every change.

## Implementation checklist

- [ ] Every file in the `AGENTS.md` file map exists for the new feature in the project's component style ("Feature conventions")
- [ ] Forms are reactive; HTTP routes through a service using `HttpClient`
- [ ] Subscriptions are cleaned up on destroy; no input object is mutated
- [ ] API URLs come from environment configuration
