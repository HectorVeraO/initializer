-- +goose Up
CREATE TABLE flg_tenant_overrides (
    tenant_id   uuid NOT NULL,
    flag_key    text NOT NULL
                CHECK (char_length(flag_key) <= 64 AND flag_key ~ '^[a-z][a-z0-9_]*$'),
    enabled     boolean NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid NULL,
    PRIMARY KEY (tenant_id, flag_key)
);
CREATE INDEX flg_tenant_overrides_key_idx ON flg_tenant_overrides (flag_key);
