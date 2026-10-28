-- +goose Up
CREATE TABLE flg_overrides (
    flag_key    text PRIMARY KEY
                CHECK (char_length(flag_key) <= 64 AND flag_key ~ '^[a-z][a-z0-9_]*$'),
    enabled     boolean NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid NULL
);
