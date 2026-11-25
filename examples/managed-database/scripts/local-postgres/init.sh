#!/usr/bin/env bash
# Runs once, as the postgres user, when the local smoke-test container
# initialises. It recreates what a Kupe managed database looks like from the
# inside: database todo_db owned by app_owner, the app_readwrite and
# app_readonly roles, the same bootstrap grants, and a pg_hba.conf that only
# accepts the three roles over TLS.
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v owner_password="$OWNER_PASSWORD" <<'SQL'
CREATE ROLE app_owner LOGIN PASSWORD :'owner_password';
CREATE DATABASE todo_db OWNER app_owner;
SQL

# The bootstrap SQL Kupe runs in every new database, with {db} = todo_db. The
# platform's own roles (streaming_replica, cnpg_metrics_exporter) and their
# grants are left out: nothing here replicates or scrapes metrics.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname todo_db <<'SQL'
CREATE ROLE app_readwrite NOLOGIN NOINHERIT;
CREATE ROLE app_readonly NOLOGIN NOINHERIT;
REVOKE EXECUTE ON FUNCTION pg_catalog.lo_create(oid), pg_catalog.lo_creat(integer), pg_catalog.lo_from_bytea(oid, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pg_catalog.lo_create(oid), pg_catalog.lo_creat(integer), pg_catalog.lo_from_bytea(oid, bytea) TO app_owner, app_readwrite;
REVOKE ALL ON DATABASE todo_db FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE todo_db TO app_readwrite;
GRANT CONNECT, TEMPORARY ON DATABASE todo_db TO app_readonly;
REVOKE CONNECT ON DATABASE postgres FROM PUBLIC;
REVOKE CONNECT ON DATABASE template1 FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO app_readwrite;
GRANT USAGE ON SCHEMA public TO app_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_readwrite;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT USAGE, SELECT ON SEQUENCES TO app_readwrite;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT USAGE ON SCHEMAS TO app_readwrite;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT SELECT ON TABLES TO app_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT SELECT ON SEQUENCES TO app_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT USAGE ON SCHEMAS TO app_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT EXECUTE ON FUNCTIONS TO app_readwrite;
SQL

# Kupe's role management then makes app_readwrite and app_readonly login roles.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v readwrite_password="$READWRITE_PASSWORD" \
  -v readonly_password="$READONLY_PASSWORD" <<'SQL'
ALTER ROLE app_readwrite LOGIN PASSWORD :'readwrite_password';
ALTER ROLE app_readonly LOGIN PASSWORD :'readonly_password';
SQL

# TLS only, and only the three application roles on the application database.
# The local line lets `docker exec ... psql -U postgres` work for debugging.
cat >"$PGDATA/pg_hba.conf" <<'HBA'
local   all      postgres                                    trust
hostssl todo_db  app_owner,app_readwrite,app_readonly  all   scram-sha-256
hostnossl all    all                                   all   reject
host    all      all                                   all   reject
HBA
