# go-srv -- Trunk-based development and feature flags

Read before adding, evaluating or removing a feature flag, or before splitting a feature into several PRs. The flags module itself is built in slice 1.2.

## When a flag is allowed

| Change | Flag |
|---|---|
| Bugfix or refactor (`A -> A'`) | No. Fix `A` directly with a regression test. |
| Small complete change (one PR) | No. |
| Inner layers of a new feature (schema, domain, services) not yet reachable from any route | No. Unreachable code is invisible (Keystone Interface). |
| Replacing an active implementation (`A'` vs `B`) | Yes, one release flag. |
| Multi-day change to a screen in use | Yes, one release flag. |
| Gradual rollout to selected tenants | Yes, one release flag. |
| Cutting off a degraded external integration without a deploy | Yes, an ops flag (permanent). |

Commercial entitlements (what a plan includes) are never feature flags.

## `flags.yaml` (repo root)

The declarative catalog. Every flag the code evaluates is declared here.

```yaml
flags:
  - key: new_settings_ui
    type: release
    default: false
    expires_at: "2026-02-01"
    description: "Replaces the settings screen"
  - key: example_outbound_enabled
    type: ops
    default: true
    description: "Kill-switch for outbound calls to an external provider"
```

- `key`: snake_case, unique.
- `type: release`: `default: false` and an `expires_at` date at most 30 days after the flag is added. An expired release flag must be removed, not extended silently.
- `type: ops`: permanent; may default to `true`; no `expires_at`.
- Overrides on a key that is not in `flags.yaml` are rejected (`404 FLAG_NOT_DEFINED_IN_MANIFEST`).

## Evaluation

Precedence, first match wins:

1. Tenant override `(key, tenantId) -> enabled`.
2. Global override `key -> enabled`.
3. The `default` in `flags.yaml`.

An unknown key evaluates to `false` (default-closed).

The port is `core/featureflag` (`Evaluator.Enabled(ctx, key)`, `Snapshot`, `Route`); the `feature-flags` module implements it and a project without that module gets `featureflag.Disabled{}`. Use only the port, read from `Host.FlagEvaluator()`; never read flag tables directly.

## Single seam (Branch by Abstraction)

- Evaluate a release flag once, at the boundary: the route registration or one router function. Never inside business methods.
- `A'` and `B` live in separate types behind one interface; the router picks one per request from the flag. A fix to `A'` edits only `A'`.
- No nested release flags (a flag checked inside code guarded by another flag).
- WIP limit: one active replacement per module. `C` cannot start until `B` fully replaced `A'` and `A'` is deleted.
- The backend enforces the flag; hiding a button in `ng-web` is never the only guard.
- Queued work created under one flag state stays readable under the other: payloads change additively or carry a `schema_version`.

## Splitting a feature into PRs

1. Persistence and domain layers merge first, with tests, unreachable from routes, without a flag.
2. The last PR connects the route or screen and adds the single release flag when a gradual release is needed.
3. Every feature that added a release flag ends with a cleanup PR that deletes the flag from `flags.yaml`, its evaluation, and the old implementation `A'`.

## Checklist

- [ ] No flag on a bugfix, refactor or single complete change.
- [ ] The flag is declared in `flags.yaml` with the right `type`, `default` and `expires_at`.
- [ ] Evaluated once at the boundary; no nested release flags.
- [ ] A cleanup task exists for every release flag.
- [ ] Tests cover both flag states through HTTP.
