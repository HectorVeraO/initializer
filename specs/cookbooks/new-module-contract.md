# Adding a module contract and its TCK

Read before adding a spec: the contract of a catalog module or of a product vertical, and the
black-box suite that certifies it. A vertical's spec is where its behaviour is written down as a
promise, not as a description of the implementation.

## Layout

One folder per vertical, `specs/<vertical>/`, holding three things:

| Path | Holds |
|---|---|
| `openapi.yaml` | the HTTP contract: paths, operations, schemas, the error codes |
| `spec.manifest.json` | how the suite discovers the spec and what it needs installed |
| `tck/` | the Playwright scenarios; a cooperation scenario under `tck/with-<name>/` |

The folder name equals the `spec` id. Two verticals cannot share a folder or a tag.

## `openapi.yaml`

- OpenAPI 3.1, one document per vertical, at the project root's `specs/` folder next to the
  component.
- Every `/api/v1` response is the envelope: `data`, `meta.traceId`, `meta.timestamp`, `error`. The schemas `ApiMeta`, `ApiIssue`, `ApiError` and `ApiEnvelopeError` and the header `XTraceId` are byte-identical in every spec of the catalog; a vertical copies them and never edits the shape.
- Every response declares `X-Trace-Id` as the header `XTraceId`; its value equals `meta.traceId` of the body.
- A `422` documents `VALIDATION_FAILED` and the issue codes its fields can raise.
- An error code is part of the contract. Adding one is a compatible change; renaming or removing
  one is breaking.
- The contract changes in the same pull request as the code, and the code implements the document;
  it never defines it.

## `spec.manifest.json`

```json
{
  "schema": 1,
  "spec": "dispatch",
  "version": "0.1.0",
  "tckTags": ["@dispatch"],
  "tckEnv": {
    "APP_FLAGS_FILE": "flags.yaml,specs/dispatch/tck/fixtures/flags.yaml",
    "APP_BOOTSTRAP_ADMIN_EMAIL": "admin@example.com",
    "APP_BOOTSTRAP_ADMIN_PASSWORD": "change-me-local-only"
  },
  "cooperatesWith": ["@identity"]
}
```

- `schema`, `spec`, `version`, `tckTags` and `tckEnv` are required; `cooperatesWith` is optional.
- `version` is the contract version, `MAJOR.MINOR.PATCH`. While the project is pre-release (every manifest at `0.1.0`), no version is bumped at all, not even for a breaking wire change: the wire may still change freely. After the first release, a breaking wire change is a major bump.
- `tckTags`: one tag per spec, `^@[a-z][a-z0-9-]*$`, the spec's own tag. A scenario carries its own
  tag, plus the tag of each cooperating spec whose routes it calls.
- `tckEnv`: the environment the instance under test must be started with for the suite to pass. A
  path value is relative to the project root. `APP_FLAGS_FILE` takes one path or a comma-separated
  list, read in order and merged, so a fixture flag sits beside the project's own `flags.yaml`:
  `flags.yaml,specs/<vertical>/tck/fixtures/flags.yaml`. The list is the same value the component
  runs with; a key declared twice takes the definition of the last file. Declaring one key with two
  different values in two specs is an error, so a shared value (an email, a password) is declared in
  one place only.
- `cooperatesWith`: the tags of the other specs whose routes this spec's cooperation scenarios call.
  Sorted, no duplicates, never one of its own tags.

## How the suite finds the folder

`specs/scripts/tck-select.mjs` reads `<specs>/<folder>/spec.manifest.json` for every folder, sorted
by folder name, and takes `spec` (falling back to the folder name), `tckTags`, `tckEnv` and
`cooperatesWith`. No list of specs exists anywhere else: a folder with a manifest is installed, and
the generator copies a spec folder only into a project that installs its module.

- `specs/scripts/run-tck.mjs` collects the tags of every manifest, narrows them to `--tags` when
  given, and plans the Playwright passes: one pass for every tag except `@feature-flags`, then a
  second single-worker pass for `@feature-flags` alone, because those scenarios change what the
  instance evaluates.
- A tag of a spec the component does not install is inverted, so its scenarios never run there.
- `specs/scripts/check-tags.mjs` lists the tests with Playwright and reports `TCK_SPEC_MANIFEST_MISSING`,
  `TCK_TEST_UNTAGGED` and `TCK_TAG_FOREIGN` before anything runs.
- `specs/scripts/tck-env.mjs` merges every `tckEnv` into one exported environment for a manual run,
  and reports two specs that set the same key to different values.

## Scenarios

- Imports resolve in every selection that installs the spec: `@playwright/test`, Node built-ins,
  `specs/tck-support/index.ts`, `specs/tck-support/envelope.ts`, the actor helpers the spec's module
  requires, and files inside `specs/<vertical>/tck/`.
- Tags are test-level and stated in full in the test's own `{ tag: [...] }`, own tag first:
  `{ tag: ['@dispatch', '@identity'] }`. No describe-level tags.
- A scenario that calls a cooperating spec's routes lives in `specs/<vertical>/tck/with-<name>/`,
  carries the cooperation tag on every test, and imports that spec's helper only there.
- Given / When / Then from the contract; one scenario per invariant.
- Test data is synthetic: `tenant-a`, `user@example.com`, generated UUIDs. No real personal data,
  no token shaped like a provider key.
- The suite is black-box over HTTP: no knowledge of the schema, the database or the language.

## Running it

Once per clone, then against a running component:

```bash
npm --prefix specs ci
TCK_BASE_URL=<origin> npm --prefix specs run tck
TCK_BASE_URL=<origin> npm --prefix specs run check:tags
```

`<origin>` is the component's public origin, the one `.deploy/local/README.md` lists. Both commands
also need the variables the manifests declare in `tckEnv`: the runner reads them from the manifests
for a manual run, and the instance under test must be started with them.

## Checklist

- [ ] `specs/<vertical>/openapi.yaml` describes every route, the envelope and every error code.
- [ ] `spec.manifest.json` has the folder's id, one own tag, the `tckEnv` the instance needs and the
      cooperating tags it calls.
- [ ] A scenario per invariant, tags on every test, own tag first.
- [ ] A cooperation scenario sits under `tck/with-<name>/` and its spec declares `@<name>`.
- [ ] `npm --prefix specs run check:tags` is clean.
