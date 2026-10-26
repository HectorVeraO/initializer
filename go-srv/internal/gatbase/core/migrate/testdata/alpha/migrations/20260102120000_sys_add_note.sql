-- +goose Up
ALTER TABLE sys_examples ADD COLUMN note text;
