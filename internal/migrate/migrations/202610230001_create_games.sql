-- +goose Up
CREATE TABLE kupe_games (
    id text PRIMARY KEY,
    state jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX kupe_games_expiry ON kupe_games (expires_at)
    WHERE COALESCE(state->>'name', '') = '';
CREATE INDEX kupe_games_leaderboard ON kupe_games
    (((state->>'moves')::integer), ((state->>'duration_ms')::bigint), id)
    WHERE COALESCE(state->>'name', '') <> '';

-- The API reads/writes application tables but cannot modify migration history.
REVOKE ALL ON TABLE goose_db_version FROM app_readwrite;

-- +goose Down
DROP TABLE kupe_games;
