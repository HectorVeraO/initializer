-- +goose Up
CREATE TABLE sys_parameters (
    key                 text PRIMARY KEY
                        CHECK (char_length(key) <= 128
                               AND key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'),
    value_type          text NOT NULL
                        CHECK (value_type IN ('string', 'number', 'boolean', 'json')),
    default_value       jsonb NOT NULL CHECK (jsonb_typeof(default_value) <> 'null'),
    min_value           double precision NULL,
    max_value           double precision NULL,
    allowed_values      jsonb NULL
                        CHECK (allowed_values IS NULL OR jsonb_typeof(allowed_values) = 'array'),
    description         text NOT NULL DEFAULT '',
    is_system_required  boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    updated_by          uuid NULL,
    CONSTRAINT sys_parameters_range CHECK (min_value IS NULL OR max_value IS NULL OR min_value <= max_value)
);
