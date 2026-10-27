-- +goose Up
-- CREATE TABLE commented (id int);
/* DROP TABLE hidden; /* nested CREATE TABLE inner (id int); */ DROP TABLE still_hidden; */
CREATE TABLE sys_ok (note text DEFAULT 'DROP TABLE nope; CREATE TABLE bad (id int)');
-- +goose StatementBegin
CREATE FUNCTION sys_fn() RETURNS void AS $$
BEGIN
    DROP TABLE secret_one;
    CREATE TABLE other_x (id int);
END;
$$ LANGUAGE plpgsql;
CREATE FUNCTION sys_fn2() RETURNS void AS $body$
BEGIN
    ALTER TABLE zzz_y DROP COLUMN c; SELECT $$ x $$;
END;
$body$ LANGUAGE plpgsql;
-- +goose StatementEnd
