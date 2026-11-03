# Trunk-based development and feature flags

Read before adding, evaluating or removing a feature flag, or before splitting a feature into
several pull requests.

## When a flag is allowed

| Change | Flag |
|---|---|
| Bugfix or refactor (`A -> A'`) | No. Fix `A` directly with a regression test. |
| Small complete change (one pull request) | No. |
| Inner layers of a new feature (schema, domain, services) not yet reachable from any route | No. Unreachable code is invisible (Keystone Interface). |
| Replacing an active implementation (`A'` vs `B`) | Yes, one release flag. |
| Multi-day change to a screen in use | Yes, one release flag. |
| Gradual rollout to selected tenants | Yes, one release flag. |
| Cutting off a degraded external integration without a deploy | Yes, an ops flag (permanent). |

Commercial entitlements (what a plan includes) are never feature flags.

## `flags.yaml`

The declarative catalog, at the project root beside `initializer.lock.json`. Every flag the code
evaluates is declared here.

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

- `key`: snake_case, `^[a-z][a-z0-9_]*$`, at most 64 characters, unique.
- `type: release`: `default: false` and an `expires_at` date at most 30 days after the flag is
  added. An expired release flag is removed, never extended silently.
- `type: ops`: permanent; may default to `true`; no `expires_at`.
- `description` is required.
- An override on a key that is not in `flags.yaml` is rejected
  (`404 FLAG_NOT_DEFINED_IN_MANIFEST`).

## Evaluation

Precedence, first match wins:

1. Tenant override `(key, tenantId) -> enabled`.
2. Global override `key -> enabled`.
3. The `default` in `flags.yaml`.

An unknown key evaluates to `false` (default-closed), and a store error resolves to the manifest
default, so a release flag fails closed.

The port is `core/featureflag` (`Evaluator.Enabled(ctx, key)`, `Snapshot`); the feature-flags module
implements it and a project without that module gets `featureflag.Disabled{}`. Use only the port,
read from `Host.FlagEvaluator()`; never read the flag tables directly.

Two evaluation points exist:

- `featureflag.Route(eval, key, on, off)` wraps a route registration and evaluates once per request.
- `Evaluator.Enabled` once per job, at the job boundary.

Nothing else evaluates a flag: not a service method, not a store, not a template.

## `APP_FLAGS_FILE`

A comma-separated list of manifest paths, read in order and merged. A key declared in more than one
file takes the definition of the last file; a blank segment is a startup error; a missing or invalid
file fails startup.

- The project's own `flags.yaml` is the first path.
- A spec that needs a fixture flag adds it as another segment; its `spec.manifest.json` `tckEnv`
  names the same list (`specs/cookbooks/new-module-contract.md`).
- Relative segments are relative to the directory the component runs from; the project README shows
  the composed value.

## Single seam (Branch by Abstraction)

- Evaluate a release flag once, at the boundary: the route registration or one router function.
  Never inside a business method.
- `A'` and `B` live in separate types behind one interface; the router picks one per request from
  the flag. A fix to `A'` edits only `A'`.
- No nested release flags (a flag checked inside code guarded by another flag).
- WIP limit: one active replacement per module. `C` cannot start until `B` fully replaced `A'` and
  `A'` is deleted.
- The backend enforces the flag; hiding a control in the console is never the only guard.
- Queued work created under one flag state stays readable under the other: payloads change
  additively or carry a schema version.

## A product flag

A vertical declares its own flags; it never reuses a key of the catalog.

1. Add the row to the project's `flags.yaml`, with `type`, `default`, `description` and, for a
   release flag, `expires_at`.
2. Evaluate it at one boundary with `featureflag.Route` or one `Enabled` call per job.
3. Add its file to `APP_FLAGS_FILE` if it lives in a second manifest; keep one row per key.
4. Prove both states over HTTP in the vertical's own test, and in the spec's TCK when the flag
   changes a route the contract describes.
5. Write the cleanup task in the same pull request: the release flag's `expires_at` is the deadline.

## Splitting a feature into pull requests

1. Persistence and domain layers merge first, with tests, unreachable from routes, without a flag.
2. The last pull request connects the route or screen and adds the single release flag when a gradual release is needed.
3. Every feature that added a release flag ends with a cleanup pull request that deletes the flag from `flags.yaml`, its evaluation, the old implementation `A'`, and its tests. It also removes the key from any TCK fixture that declared it. A key the code still evaluates after its row is deleted evaluates to `false`; that is the safety net, not the cleanup.

## Checklist

- [ ] No flag on a bugfix, refactor or single complete change.
- [ ] The flag is declared in `flags.yaml` with the right `type`, `default` and `expires_at`.
- [ ] `APP_FLAGS_FILE` lists every manifest that declares one of the project's keys.
- [ ] Evaluated once at the boundary; no nested release flags.
- [ ] Both flag states are covered through HTTP.
- [ ] A cleanup task exists for every release flag.
