#!/usr/bin/env bash
# Runs once, as the postgres user, when the local smoke-test container
# initialises. It recreates what a Kupe managed database looks like from the
# inside: database todo_db owned by app_owner, the app_user role, the same
# bootstrap grants, and a pg_hba.conf that only accepts the two roles over TLS.
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v owner_password="$OWNER_PASSWORD" <<'SQL'
CREATE ROLE app_owner LOGIN PASSWORD :'owner_password';
CREATE DATABASE todo_db OWNER app_owner;
SQL

# The bootstrap SQL Kupe runs in every new database, with {db} = todo_db.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname todo_db <<'SQL'
CREATE ROLE app_user NOLOGIN NOINHERIT;
REVOKE ALL ON DATABASE todo_db FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE todo_db TO app_user;
REVOKE CONNECT ON DATABASE postgres FROM PUBLIC;
REVOKE CONNECT ON DATABASE template1 FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO app_user;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_user;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT USAGE, SELECT ON SEQUENCES TO app_user;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT USAGE ON SCHEMAS TO app_user;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT EXECUTE ON FUNCTIONS TO app_user;
SQL

# Kupe's role management then makes app_user a login role.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v app_password="$APP_PASSWORD" <<'SQL'
ALTER ROLE app_user LOGIN PASSWORD :'app_password';
SQL

# TLS only, and only the two application roles on the application database.
# The local line lets `docker exec ... psql -U postgres` work for debugging.
cat >"$PGDATA/pg_hba.conf" <<'HBA'
local   all      postgres                       trust
hostssl todo_db  app_owner,app_user  all        scram-sha-256
hostnossl all    all                 all        reject
host    all      all                 all        reject
HBA
