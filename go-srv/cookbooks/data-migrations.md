# go-srv -- Data migrations and table prefixes

Read before creating or changing a table, column, index or migration file. The full data-change policy is `.claude/references/data-changes.md`; this fragment holds the Gatbase-specific rules.

## Table naming

Every table matches `^[a-z]{2,4}_[a-z0-9_]+$`: a 2 to 4 letter module prefix, an underscore, a snake_case name.

- Valid: `sys_parameters`, `ten_memberships`, `iam_api_keys`.
- Invalid: `parameters` (no prefix), `parameter_definitions` (prefix longer than 4 letters), `Sys_Parameters` (uppercase).

## Prefix registry

The registry the lint reads is each module's `go-srv/internal/gatbase/<module>/module.manifest.json` `tablePrefixes` (2 to 4 lowercase letters, no underscore). The table below is the human index: `cor_`, `ten_` and `iam_` are enforced once their module ships a manifest that claims them; until then the lint does not stop another manifest from claiming them, so check this table in review.

| Prefix | Module | Tables |
|---|---|---|
| `cor_` | Core migration runner: goose version tables | `cor_migrations_<module>` (one goose version table per module) |
| `ten_` | Tenancy and memberships (core) | `ten_tenants`, `ten_memberships`, `ten_invitations` |
| `iam_` | Identity and API keys (core) | `iam_accounts`, `iam_api_keys` |
| `sys_` | Parameters | `sys_parameters`, `sys_parameter_overrides` |
| `flg_` | Feature flags | `flg_overrides`, `flg_tenant_overrides` |

Reserved for planned catalog modules; `gates migrations` rejects a manifest that claims one (list: `tooling/gates/internal/migrationlint/reserved-prefixes.json`): `aud_` (audit log), `pln_` (plans and entitlements), `mtr_` (metering), `bil_` (billing), `mkc_` (maker-checker), `jbr_` (support ticket bridge), `stg_` (file storage).

- A new module claims a new prefix in its `module.manifest.json` and adds a row here in the same PR as its first migration. No two manifests claim the same prefix.
- A generated project registers its own prefixes in its own copy of this table; it never reuses a core or reserved prefix.

## Migration files

- Run `go -C tooling/gates run ./cmd/gates migrations --root ../.. [--base-rev <rev>]` (CI job `gates` runs it with the PR base). Output ends with `<n> migration files checked`. It checks every `.sql` file under `go-srv/` except those inside a `testdata/` directory (test fixtures).
- Files live in `go-srv/internal/gatbase/<module>/migrations/`, one file per change, named `^(\d{14})_([a-z]{2,4})_([a-z0-9]+(?:_[a-z0-9]+)*)\.sql$`: `<YYYYMMDDhhmmss>_<prefix>_<name>.sql` (for example `20260101120000_sys_create_parameters.sql`). The prefix is in the owning module's `tablePrefixes`; timestamps are unique across the repo.
- Every file has the line `-- +goose Up`. Wrap PL/pgSQL bodies in `-- +goose StatementBegin` / `-- +goose StatementEnd`. No `-- +goose Down` (Expand-first), no `-- +goose NO TRANSACTION`.
- Tables are unquoted, in schema `public` (qualifier optional), and carry the file's module prefix, also in `REFERENCES`, `ALTER TABLE`, `CREATE INDEX ... ON` and `DROP TABLE`; no cross-module foreign keys.
- A Contract step (`DROP TABLE`, `DROP COLUMN`, `RENAME COLUMN`, `RENAME TO`, `ALTER COLUMN ... TYPE`, `ALTER COLUMN ... SET NOT NULL`) carries the line `-- gatbase:contract-step <reason>` directly above the statement.
- A new file sorts after the newest file of its module on the base revision (its tip, not the fork point); a file that exists at the base is never edited, renamed or deleted.
- Files are applied by the migration tool chosen in slice 1.1. Until that tool exists, no migration file is committed.
- A merged migration is immutable. Fix a mistake with a new migration.
- A migration touches only its own module's tables.

## Expand, Dual-Write, Contract

Schema changes are additive first, because migrations run on deploy even when the new code path is switched off:

1. **Expand**: add tables, or add columns that are `NULL` or carry a `DEFAULT`. Never drop or rename anything the running code reads or writes.
2. **Dual-Write** (only when old and new code alternate on the same data): write both shapes until the old reader is gone.
3. **Contract**: drop the old column or table in a later release, after the code that used it has been deleted.

Rules:

- No PR drops or renames a column, table, JSON property or event field that current code uses.
- No migration rewrites a large table in one statement or holds a long lock; batch it.
- Every new foreign key and every column used in a tenant-scoped lookup has an index.
- Tenant-owned tables carry a non-null `tenant_id` and every tenant-scoped unique constraint includes it.

## Checklist

- [ ] Table names match the regex and use the module's registered prefix.
- [ ] The migration is additive (Expand) unless it is a planned Contract step for code already removed.
- [ ] File name follows `<timestamp>_<prefix>_<name>.sql`.
- [ ] An integration test exercises the new schema against a real PostgreSQL (`cookbooks/testing-tck.md`).
