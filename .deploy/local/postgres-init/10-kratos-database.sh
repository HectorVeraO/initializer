#!/usr/bin/env bash
# Creates the kratos role and database on the shared server; safe to re-run.
set -euo pipefail

psql -v ON_ERROR_STOP=1 --set kratos_password="${KRATOS_PG_PASSWORD}" <<'SQL'
SELECT 'CREATE ROLE kratos LOGIN'
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'kratos')\gexec
ALTER ROLE kratos PASSWORD :'kratos_password';
SELECT 'CREATE DATABASE kratos OWNER kratos'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'kratos')\gexec
SQL
