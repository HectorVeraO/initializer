-- +goose Up
CREATE TABLE ten_memberships (
  id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id         uuid        NOT NULL REFERENCES ten_tenants (id),
  account_id        uuid        NOT NULL,
  role              text        NOT NULL,
  status            text        NOT NULL DEFAULT 'active',
  last_selected_at  timestamptz NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ten_memberships_role_check CHECK (role IN ('admin', 'member', 'viewer')),
  CONSTRAINT ten_memberships_status_check CHECK (status IN ('active', 'suspended', 'revoked'))
);
CREATE UNIQUE INDEX ten_memberships_tenant_account_key ON ten_memberships (tenant_id, account_id);
CREATE INDEX ten_memberships_account_idx ON ten_memberships (account_id);
CREATE INDEX ten_memberships_active_admins_idx ON ten_memberships (tenant_id)
  WHERE role = 'admin' AND status = 'active';
