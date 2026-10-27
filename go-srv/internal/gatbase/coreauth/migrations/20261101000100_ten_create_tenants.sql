-- +goose Up
CREATE TABLE ten_tenants (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  slug        text        NOT NULL,
  name        text        NOT NULL,
  status      text        NOT NULL DEFAULT 'active',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ten_tenants_status_check CHECK (status IN ('active', 'suspended')),
  CONSTRAINT ten_tenants_slug_format CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$'),
  CONSTRAINT ten_tenants_name_length CHECK (char_length(name) BETWEEN 1 AND 100)
);
CREATE UNIQUE INDEX ten_tenants_slug_key ON ten_tenants (slug);
