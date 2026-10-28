-- +goose Up
CREATE TABLE sys_parameters (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    name text NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS sys_parameters_name_uq ON ONLY public.sys_parameters (tenant_id, name);
