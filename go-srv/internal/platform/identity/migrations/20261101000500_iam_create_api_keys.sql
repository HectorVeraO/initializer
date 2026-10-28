-- +goose Up
CREATE TABLE iam_api_keys (
  id                     uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id              uuid        NULL,
  name                   text        NOT NULL,
  key_prefix             text        NOT NULL,
  secret_hash            bytea       NOT NULL,
  hash_scheme            text        NOT NULL DEFAULT 'sha256',
  scopes                 text[]      NOT NULL,
  expires_at             timestamptz NULL,
  revoked_at             timestamptz NULL,
  created_by_account_id  uuid        NOT NULL,
  created_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT iam_api_keys_prefix_format CHECK (key_prefix ~ '^ak_live_[a-z2-7]{16}$'),
  CONSTRAINT iam_api_keys_name_length CHECK (char_length(name) BETWEEN 1 AND 100),
  CONSTRAINT iam_api_keys_scopes_nonempty CHECK (cardinality(scopes) > 0)
);
CREATE UNIQUE INDEX iam_api_keys_prefix_key ON iam_api_keys (key_prefix);
CREATE INDEX iam_api_keys_tenant_created_idx ON iam_api_keys (tenant_id, created_at);
CREATE INDEX iam_api_keys_created_by_idx ON iam_api_keys (created_by_account_id);
