-- Runs as the database owner role. Kupe sets default privileges on every
-- managed database so the readwrite role can read and write rows in any table
-- the owner creates. That suits todos, so it needs no GRANT.
--
-- The same default privileges cover goose_db_version, the table goose creates
-- as the owner to record applied migrations. The API never needs it, and a
-- readwrite role that could write to it could make a later run skip a
-- migration, so the readwrite role loses every privilege on it. The readonly
-- role keeps SELECT on it, which changes nothing.

-- +goose Up
CREATE TABLE IF NOT EXISTS todos (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title      text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    done       boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

REVOKE ALL ON TABLE goose_db_version FROM app_readwrite;

-- +goose Down
DROP TABLE IF EXISTS todos;
