-- +goose Up
CREATE TABLE iam_accounts (
  id                     uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  kratos_identity_id     uuid        NOT NULL,
  email                  text        NOT NULL,
  full_name              text        NULL,
  status                 text        NOT NULL DEFAULT 'active',
  is_platform_operator   boolean     NOT NULL DEFAULT false,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT iam_accounts_status_check CHECK (status IN ('active', 'locked', 'disabled')),
  CONSTRAINT iam_accounts_email_lower CHECK (email = lower(email))
);
CREATE UNIQUE INDEX iam_accounts_kratos_identity_key ON iam_accounts (kratos_identity_id);
CREATE INDEX iam_accounts_email_idx ON iam_accounts (email);
