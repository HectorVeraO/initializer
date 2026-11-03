# Data migrations and table prefixes

Read before creating or changing a table, column, index or migration file. The schema belongs to
the module or vertical that writes it; no other package reads its tables.

## Table naming

Every table matches `^[a-z]{2,4}_[a-z0-9_]+$`: a 2 to 4 letter prefix, an underscore, a snake_case
name.

- Valid: `sys_parameters`, `ten_memberships`, `apk_keys`.
- Invalid: `parameters` (no prefix), `parameter_definitions` (prefix longer than 4 letters),
  `Sys_Parameters` (uppercase).

## Where the prefix comes from

A platform module claims its prefixes in
`go-srv/internal/platform/<module>/module.manifest.json` -> `tablePrefixes` (2 to 4 lowercase
letters, no underscore). A vertical claims its prefix through its migration file names: every file
in `go-srv/internal/<vertical>/migrations/` is named `<YYYYMMDDhhmmss>_<prefix>_<name>.sql`, and one
vertical yields exactly one prefix. A vertical carries no `module.manifest.json`; it is never
upgraded from the catalog.

Two consequences, both checked:

- A vertical's migration files must yield exactly one prefix: two distinct prefixes are refused, and a file whose name carries none is refused by the file-name rule.
- A vertical that names another package's table fails too, even with no migrations of its own.

## The check

`go test ./...` runs it; there is no separate command. Boundary rule 6 derives the prefix from the
migration file names and refuses, naming the directory:

| Message | Means |
|---|---|
| `claims 2 table prefixes (ord, pay)` | two prefixes among the vertical's migration files |
| `prefix "ten" is already claimed by platform/tenancy` | a platform module owns it |
| `prefix "ten" is already claimed by <other vertical>` | a sibling vertical owns it |
| `prefix "cor" belongs to core` | core's migration ledger |
| `prefix "aud" is reserved` | a reserved area, list below |
| `prefix "pay" already appears as an identifier outside this vertical, choose another prefix` | the prefix is also an ordinary word elsewhere |
| `prefix "payments" must be 2 to 4 lowercase letters` | the shape |
| `name must be <timestamp>_<prefix>_<name>.sql` | a migration file name, or a stray file, in `migrations/` |
| `<file>:<line> names <table>, owned by <owner>` | a Go file names another package's table |

Reserved for areas outside the catalog: `aud` (audit log), `pln` (plans and entitlements), `mtr`
(metering), `bil` (billing), `mkc` (maker-checker), `jbr` (support ticket bridge), `stg` (file
storage).

The identifier refusal exists because the matcher cannot tell a table name from an ordinary
identifier: a platform module that uses `pay_status` as a column or a variable refuses the prefix
`pay` at the vertical, and the fix is another prefix, not an edit to the other package.

## Migration files

- One file per change, named `^(\d{14})_([a-z]{2,4})_([a-z0-9]+(?:_[a-z0-9]+)*)\.sql`:
  `<YYYYMMDDhhmmss>_<prefix>_<name>.sql` (for example `20260101120000_sys_create_parameters.sql`).
  Timestamps are unique across the component.
- Every file starts with `-- +goose Up`. Wrap a PL/pgSQL body in `-- +goose StatementBegin` and
  `-- +goose StatementEnd`. No `-- +goose Down` (expand-first), no `-- +goose NO TRANSACTION`.
- Tables are unquoted, in schema `public` (the qualifier is optional), and carry the owner's prefix
  also in `REFERENCES`, `ALTER TABLE`, `CREATE INDEX ... ON` and `DROP TABLE`. No foreign key across
  modules or verticals.
- A Contract step (`DROP TABLE`, `DROP COLUMN`, `RENAME COLUMN`, `RENAME TO`,
  `ALTER COLUMN ... TYPE`, `ALTER COLUMN ... SET NOT NULL`) carries the line
  `-- initializer:contract-step <reason>` directly above the statement.
- A module embeds its files with `//go:embed migrations/*.sql` and passes the embedded FS as
  `core.Module.Migrations`. Core applies every set at startup, after all `Provide` calls and before
  any `Mount`, in module list order, under one Postgres advisory lock, so concurrent instances wait
  instead of racing.
- Each module has its own goose version table `cor_migrations_<module>` (`-` becomes `_`:
  `cor_migrations_feature_flags`). There is no `-- +goose Down`; a failing file fails startup.
- A migration that has reached a shared environment is immutable: fix a mistake with a new file.
- A migration touches only its own tables.
- Test a new schema against a real PostgreSQL through `testcore.New(t, modules,
  testcore.Options{Database: true})` (`cookbooks/testing-tck.md`).

## Expand, Dual-Write, Contract

Schema changes are additive first, because migrations run on deploy even when the new code path is
switched off:

1. **Expand**: add tables, or add columns that are `NULL` or carry a `DEFAULT`. Never drop or rename
   anything the running code reads or writes.
2. **Dual-Write** (only when old and new code alternate on the same data): write both shapes until
   the old reader is gone.
3. **Contract**: drop the old column or table in a later release, after the code that used it has
   been deleted.

Rules:

- No change drops or renames a column, table, JSON property or event field that current code uses.
- No migration rewrites a large table in one statement or holds a long lock; batch it.
- Every new foreign key and every column used in a tenant-scoped lookup has an index.
- Tenant-owned tables carry a non-null `tenant_id` and every tenant-scoped unique constraint
  includes it. A table whose rows may belong to no tenant (`apk_keys`) carries a nullable
  `tenant_id`; NULL means the global scope.

## Checklist

- [ ] Table names match the regex and use the owner's declared prefix.
- [ ] Every migration of the vertical shares one prefix, and no other package claims it.
- [ ] The migration is additive (Expand) unless it is a planned Contract step for code already
      removed.
- [ ] File name follows `<timestamp>_<prefix>_<name>.sql`.
- [ ] `go test ./...` passes; an integration test exercises the new schema against a real
      PostgreSQL (`cookbooks/testing-tck.md`).
