# Code Path Parity: full matrix

Passive reference, reached from root `AGENTS.md` "Code Path Parity". It holds the full cross-subproject parity rows; root keeps one summary bullet per group naming its files. Read the full row here before touching any file named in root's Code Path Parity section.

Each row states:

- **Files**: every copy or twin, by repo-relative path. No "and similar": a file not listed is not checked.
- **What duplicates**: the logic that is shared and the shape the duplication has (byte-identical copy, matching halves of a contract, same literal across layers).
- **Check**: for byte-identical copies, the command that recomputes the hash of every copy and the expected value; a hash nobody can recompute is not a baseline. For copies that have already diverged, say so and name how to establish the good version before propagating anything.
- **What breaks silently**: the failure a missed update produces, so the reader knows what to look for.

Format and examples: `.claude/references/code-path-parity-format.md`.

---

<!-- Add parity rows below this line. The skill never edits below. -->

| Group | Files | What duplicates | Check | What breaks silently |
|---|---|---|---|---|
| oasdiff pin | `mise.toml`, `.github/workflows/ci.yml` | The oasdiff version (`github:oasdiff/oasdiff` in `mise.toml`, `OASDIFF_VERSION` and its sha256 in `ci.yml`) | Both name the same version; the sha256 comment matches that release's linux_amd64 archive | Local and CI `gates contracts` classify breaking changes with different oasdiff versions |
| Envelope schemas | `specs/core/openapi.yaml`, `specs/identity/openapi.yaml`, `specs/parameters/openapi.yaml`, `specs/feature-flags/openapi.yaml` | The `ApiMeta`, `ApiIssue`, `ApiError`, `ApiEnvelopeError` schemas and the `XTraceId` header, copied byte for byte (no external `$ref`) | `for f in <the files>; do awk '/^    ApiMeta:/{p=1} /^    (ApiPage\|ApiErrorCode\|SuccessEnvelope):/{p=0} p' $f \| sha256sum; done` prints one hash for all files (same for the `XTraceId` block) | A client generated from one spec reads another module's envelope differently; oasdiff sees no break because each file is self-consistent |
| Flag key rule | `go-srv/internal/platform/featureflags/migrations/20261026170000_flg_create_overrides.sql`, `go-srv/internal/platform/featureflags/migrations/20261026170100_flg_create_tenant_overrides.sql`, `go-srv/internal/platform/featureflags/manifest.go`, `tooling/gates/internal/flagscheck/flagscheck.go` | The flag key rule `^[a-z][a-z0-9_]*$` with at most 64 characters (SQL `CHECK`, loader `keyPattern` and `maxKeyLength`, gate `keyPattern` and `maxKeyLength`) | `grep -n 'a-z0-9_\]\*\$' <the four files>` shows the same pattern and the same 64 limit in each | A key the gate accepts is refused by startup or by the database, or the reverse; the failure shows only at deploy |
