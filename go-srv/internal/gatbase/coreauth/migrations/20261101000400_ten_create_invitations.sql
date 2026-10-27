-- +goose Up
CREATE TABLE ten_invitations (
  id                      uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id               uuid        NOT NULL REFERENCES ten_tenants (id),
  email                   text        NOT NULL,
  role                    text        NOT NULL,
  token_hash              bytea       NOT NULL,
  expires_at              timestamptz NOT NULL,
  created_by_account_id   uuid        NOT NULL REFERENCES iam_accounts (id),
  accepted_at             timestamptz NULL,
  accepted_by_account_id  uuid        NULL REFERENCES iam_accounts (id),
  revoked_at              timestamptz NULL,
  created_at              timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ten_invitations_role_check CHECK (role IN ('admin', 'member', 'viewer')),
  CONSTRAINT ten_invitations_email_lower CHECK (email = lower(email)),
  CONSTRAINT ten_invitations_token_hash_len CHECK (octet_length(token_hash) = 32),
  CONSTRAINT ten_invitations_single_outcome CHECK (accepted_at IS NULL OR revoked_at IS NULL)
);
CREATE UNIQUE INDEX ten_invitations_token_hash_key ON ten_invitations (token_hash);
CREATE UNIQUE INDEX ten_invitations_one_pending_key ON ten_invitations (tenant_id, email)
  WHERE accepted_at IS NULL AND revoked_at IS NULL;
CREATE INDEX ten_invitations_tenant_created_idx ON ten_invitations (tenant_id, created_at);
CREATE INDEX ten_invitations_created_by_idx ON ten_invitations (created_by_account_id);
CREATE INDEX ten_invitations_accepted_by_idx ON ten_invitations (accepted_by_account_id);
