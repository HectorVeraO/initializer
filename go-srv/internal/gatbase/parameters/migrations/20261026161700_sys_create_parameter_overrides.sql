-- +goose Up
CREATE TABLE sys_parameter_overrides (
    tenant_id      uuid NOT NULL,
    parameter_key  text NOT NULL REFERENCES sys_parameters (key) ON DELETE CASCADE,
    value          jsonb NOT NULL CHECK (jsonb_typeof(value) <> 'null'),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid NULL,
    PRIMARY KEY (tenant_id, parameter_key)
);
CREATE INDEX sys_parameter_overrides_key_idx ON sys_parameter_overrides (parameter_key);
