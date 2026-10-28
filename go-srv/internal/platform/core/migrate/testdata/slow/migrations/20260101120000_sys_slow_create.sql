-- +goose Up
SELECT pg_sleep(2);
CREATE TABLE sys_slow (id int PRIMARY KEY);
