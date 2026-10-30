#!/usr/bin/env bash
# The Go tests the old `Unit Tests` and `Game / postgres` jobs ran
# (kupecloud/github-workflows#14 moved them here).
#
# With MIGRATE_DATABASE_URL set (the owner role), the migrations are applied
# twice first, to check they are repeatable. With TEST_DATABASE_URL set (the
# read-write role), the PostgreSQL tests run instead of skipping. CI sets both
# against a postgres service; locally, `make db-up` and then:
#   MIGRATE_DATABASE_URL=postgres://app_owner:local-owner@127.0.0.1:15432/hello_kupe?sslmode=disable \
#   TEST_DATABASE_URL=postgres://app_readwrite:local-readwrite@127.0.0.1:15432/hello_kupe?sslmode=disable \
#   pre-commit run go-test --all-files --hook-stage manual
set -euo pipefail

# In CI the PostgreSQL tests must run: a missing URL would make them skip and
# the gate would pass without them.
if [ "${CI:-}" = "true" ] && { [ -z "${TEST_DATABASE_URL:-}" ] || [ -z "${MIGRATE_DATABASE_URL:-}" ]; }; then
  echo "CI must set MIGRATE_DATABASE_URL and TEST_DATABASE_URL so the PostgreSQL tests run" >&2
  exit 1
fi

if [ -n "${MIGRATE_DATABASE_URL:-}" ]; then
  DATABASE_URL="$MIGRATE_DATABASE_URL" go run ./cmd/hello-kupe migrate
  DATABASE_URL="$MIGRATE_DATABASE_URL" go run ./cmd/hello-kupe migrate
fi

go test -race -timeout 120s ./...
