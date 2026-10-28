-- +goose Up
DROP TABLE sys_a;
ALTER TABLE sys_b DROP COLUMN c;
ALTER TABLE sys_b RENAME COLUMN c TO d;
ALTER TABLE sys_b RENAME e TO f;
ALTER TABLE sys_b RENAME TO sys_c;
ALTER TABLE sys_b ALTER COLUMN c TYPE bigint;
ALTER TABLE sys_b ALTER COLUMN c SET DATA TYPE bigint;
ALTER TABLE sys_b ALTER COLUMN c SET NOT NULL;
-- initializer:contract-step the old column has no readers
DROP TABLE sys_silenced;
-- initializer:contract-step

DROP TABLE sys_blank_reason_and_gap;
-- initializer:contract-step reason applies to the next statement only
ALTER TABLE sys_b ADD COLUMN ok int; DROP TABLE sys_same_line;
ALTER TABLE sys_b ADD COLUMN fine int;
ALTER TABLE sys_b RENAME CONSTRAINT x TO y;
