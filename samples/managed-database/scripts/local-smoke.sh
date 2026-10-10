#!/usr/bin/env bash
# End-to-end check without a Kupe cluster: PostgreSQL 18 with TLS and the
# Kupe role layout in Docker, the migration as the owner role, the API as the
# app role, then CRUD through the API, checks that the app role cannot change
# the schema or the migration history, that the API survives a password
# rotation without a restart, and that it refuses a server certificate for
# the wrong host or from the wrong CA. Everything it starts is removed on exit.
#
# Usage: scripts/local-smoke.sh   (from samples/managed-database)
# Needs: docker, openssl, curl.
set -euo pipefail

cd "$(dirname "$0")/.."

# Always a fresh directory this script owns: cleanup deletes it.
WORKDIR="$(mktemp -d)"
RUN_ID="todo-smoke-$$"
NET="$RUN_ID"
PG="$RUN_ID-pg"
API="$RUN_ID-api"
WEB="$RUN_ID-web"
BAD_HOST_API="$RUN_ID-api-bad-host"
BAD_CA_API="$RUN_ID-api-bad-ca"
API_IMAGE="todo-api:$RUN_ID"
WEB_IMAGE="todo-web:$RUN_ID"
# Each binding's host is the database's name in the namespace it targets:
# the app role is bound into api, the owner role into todo-migrate. The
# server certificate carries both, as a Kupe database's does.
APP_HOST="todo-db.api.svc.cluster.local"
OWNER_HOST="todo-db.todo-migrate.svc.cluster.local"
# Resolves on the test network but is not in the server certificate.
UNLISTED_HOST="todo-db.web.svc.cluster.local"
API_PORT="${API_PORT:-18080}"
WEB_PORT="${WEB_PORT:-18081}"
# Any PostgreSQL 18 image with the official entrypoint, e.g. a registry
# mirror. The default is pinned by digest.
PG_IMAGE="${PG_IMAGE:-postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336}"

cleanup() {
  docker rm -f "$API" "$BAD_HOST_API" "$BAD_CA_API" "$WEB" "$PG" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
  docker image rm "$API_IMAGE" "$WEB_IMAGE" >/dev/null 2>&1 || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

pass() { printf 'PASS  %s\n' "$*"; }
fail() { printf 'FAIL  %s\n' "$*" >&2; exit 1; }

echo "== certificates"
new_ca() { # name
  openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$WORKDIR/$1.key"
  openssl req -x509 -new -key "$WORKDIR/$1.key" -days 1 -subj "/CN=todo-smoke-$1" \
    -out "$WORKDIR/$1.crt"
}
new_ca ca
# A second CA that did not sign the server certificate.
new_ca other-ca
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$WORKDIR/server.key"
openssl req -new -key "$WORKDIR/server.key" -subj "/CN=$APP_HOST" -out "$WORKDIR/server.csr"
printf 'subjectAltName=DNS:%s,DNS:%s\n' "$APP_HOST" "$OWNER_HOST" >"$WORKDIR/san.ext"
openssl x509 -req -in "$WORKDIR/server.csr" -CA "$WORKDIR/ca.crt" -CAkey "$WORKDIR/ca.key" \
  -CAcreateserial -days 1 -extfile "$WORKDIR/san.ext" -out "$WORKDIR/server.crt"
mkdir -p "$WORKDIR/certs" && cp "$WORKDIR/server.crt" "$WORKDIR/server.key" "$WORKDIR/certs/"

OWNER_PASSWORD="$(openssl rand -hex 32)"
APP_PASSWORD="$(openssl rand -hex 32)"

# One file per key, as a mounted binding Secret presents them, mounted where
# the chart mounts them: /bindings/<secret name>. The kubelet's ..data
# symlink layout is covered by the internal/binding tests instead.
write_binding() { # dir host username password ca-file
  mkdir -p "$1"
  printf 'postgresql' >"$1/type"
  printf 'kupe' >"$1/provider"
  printf '%s' "$2" >"$1/host"
  printf '5432' >"$1/port"
  printf 'todo_db' >"$1/database"
  printf '%s' "$3" >"$1/username"
  printf '%s' "$4" >"$1/password"
  printf 'verify-full' >"$1/sslmode"
  cp "$5" "$1/ca.crt"
  chmod 0755 "$1" && chmod 0644 "$1"/*
}
write_binding "$WORKDIR/owner" "$OWNER_HOST" app_owner "$OWNER_PASSWORD" "$WORKDIR/ca.crt"
write_binding "$WORKDIR/app" "$APP_HOST" app_user "$APP_PASSWORD" "$WORKDIR/ca.crt"
write_binding "$WORKDIR/app-bad-host" "$UNLISTED_HOST" app_user "$APP_PASSWORD" "$WORKDIR/ca.crt"
write_binding "$WORKDIR/app-bad-ca" "$APP_HOST" app_user "$APP_PASSWORD" "$WORKDIR/other-ca.crt"

echo "== images"
docker build -q -f Dockerfile.api -t "$API_IMAGE" . >/dev/null
docker build -q -f Dockerfile.web -t "$WEB_IMAGE" . >/dev/null

echo "== postgres"
docker network create "$NET" >/dev/null
docker run -d --name "$PG" --network "$NET" \
  --network-alias "$APP_HOST" --network-alias "$OWNER_HOST" --network-alias "$UNLISTED_HOST" \
  -e POSTGRES_PASSWORD="$(openssl rand -hex 16)" \
  -e OWNER_PASSWORD="$OWNER_PASSWORD" -e APP_PASSWORD="$APP_PASSWORD" \
  -v "$PWD/scripts/local-postgres/init.sh:/docker-entrypoint-initdb.d/init.sh:ro" \
  -v "$WORKDIR/certs:/certs:ro" \
  --entrypoint bash "$PG_IMAGE" -c '
    install -o postgres -g postgres -m 0600 /certs/server.key /var/lib/postgresql/server.key
    install -o postgres -g postgres -m 0644 /certs/server.crt /var/lib/postgresql/server.crt
    exec docker-entrypoint.sh postgres -c ssl=on \
      -c ssl_cert_file=/var/lib/postgresql/server.crt \
      -c ssl_key_file=/var/lib/postgresql/server.key' >/dev/null

# The init-time server listens on the socket only, so a TCP check passes
# only once the final, TLS-enabled server is up.
for _ in $(seq 1 60); do
  docker exec "$PG" pg_isready -q -h 127.0.0.1 && break
  sleep 1
done
docker exec "$PG" pg_isready -q -h 127.0.0.1 || { docker logs "$PG"; fail "postgres did not start"; }

# psql as a role, over TLS with full verification against the test CA.
psql_as() { # binding-dir sql
  docker run --rm --network "$NET" -v "$1:/b:ro" -e PGPASSWORD="$(cat "$1/password")" "$PG_IMAGE" \
    psql -v ON_ERROR_STOP=1 -qtA \
    "host=$(cat "$1/host") dbname=todo_db user=$(cat "$1/username") sslmode=verify-full sslrootcert=/b/ca.crt" \
    -c "$2"
}

echo "== migration (owner role)"
docker run --rm --network "$NET" -v "$WORKDIR/owner:/bindings/todo-db-owner:ro" -e DATABASE_BINDING=todo-db-owner "$API_IMAGE" migrate \
  | tee "$WORKDIR/migrate1.log" | grep -q '"msg":"schema up to date","version":1,"applied":1' \
  || { cat "$WORKDIR/migrate1.log"; fail "first migration run"; }
pass "migration applied version 1"
docker run --rm --network "$NET" -v "$WORKDIR/owner:/bindings/todo-db-owner:ro" -e DATABASE_BINDING=todo-db-owner "$API_IMAGE" migrate \
  | tee "$WORKDIR/migrate2.log" | grep -q '"msg":"schema up to date","version":1,"applied":0' \
  || { cat "$WORKDIR/migrate2.log"; fail "second migration run"; }
pass "migration re-run applied nothing"

echo "== api (app role)"
docker run -d --name "$API" --network "$NET" -p "127.0.0.1:$API_PORT:8080" \
  -v "$WORKDIR/app:/bindings/todo-db-app:ro" -e DATABASE_BINDING=todo-db-app "$API_IMAGE" >/dev/null
BASE="http://127.0.0.1:$API_PORT"
for _ in $(seq 1 30); do
  [ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/readyz")" = 200 ] && break
  sleep 1
done
[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/readyz")" = 200 ] || { docker logs "$API"; fail "readyz"; }
pass "readyz 200 (database reachable over verify-full TLS)"

expect() { # description want-status method path [body]
  local out code
  out="$(curl -s -w '\n%{http_code}' -X "$3" -H 'Content-Type: application/json' ${5:+--data "$5"} "$BASE$4")"
  code="${out##*$'\n'}"
  BODY="${out%$'\n'*}"
  BODY="${BODY%$'\n'}"
  [ "$code" = "$2" ] || fail "$1: $3 $4 = $code, want $2 ($BODY)"
  pass "$1: $3 $4 = $code $BODY"
}
expect "create" 201 POST /api/todos '{"title":"run the migration"}'
ID="$(printf '%s' "$BODY" | sed -E 's/.*"id":([0-9]+).*/\1/')"
expect "list" 200 GET /api/todos
printf '%s' "$BODY" | grep -q '"title":"run the migration"' || fail "list does not contain the new todo"
expect "update" 200 PATCH "/api/todos/$ID" '{"done":true}'
printf '%s' "$BODY" | grep -q '"done":true' || fail "update did not set done"
expect "read" 200 GET "/api/todos/$ID"
expect "delete" 204 DELETE "/api/todos/$ID"
expect "read deleted" 404 GET "/api/todos/$ID"

echo "== privileges"
[ "$(psql_as "$WORKDIR/app" 'SELECT count(*) FROM todos')" = 0 ] || fail "app role cannot read todos"
pass "app role can read todos"
if out="$(psql_as "$WORKDIR/app" 'CREATE TABLE app_cannot_do_this (id int)' 2>&1)"; then
  fail "app role created a table"
fi
printf '%s' "$out" | grep -q 'permission denied for schema public' || fail "unexpected error: $out"
pass "app role CREATE TABLE refused: $(printf '%s' "$out" | grep -o 'permission denied for schema public')"
psql_as "$WORKDIR/owner" 'CREATE TABLE owner_can_do_this (id int); DROP TABLE owner_can_do_this' >/dev/null \
  || fail "owner role could not create a table"
pass "owner role can create and drop a table"
for privilege in SELECT INSERT; do
  got="$(psql_as "$WORKDIR/app" "SELECT has_table_privilege('app_user', 'goose_db_version', '$privilege')")"
  [ "$got" = f ] || fail "app role has $privilege on goose_db_version ($got)"
  pass "app role has no $privilege on goose_db_version"
done
if out="$(docker run --rm --network "$NET" -e PGPASSWORD="$APP_PASSWORD" "$PG_IMAGE" \
  psql "host=$APP_HOST dbname=todo_db user=app_user sslmode=disable" -c 'SELECT 1' 2>&1)"; then
  fail "plaintext connection accepted"
fi
printf '%s' "$out" | grep -q 'no encryption' || fail "unexpected error: $out"
pass "plaintext connection refused by pg_hba"

echo "== password rotation"
# Rotate the app role's password the way Kupe does (the old one stops working
# at once), update the mounted binding file, and close the API's sessions so
# it has to log in again. It must carry on without a restart.
API_STARTED="$(docker inspect -f '{{.State.StartedAt}}' "$API")"
NEW_APP_PASSWORD="$(openssl rand -hex 32)"
printf "ALTER ROLE app_user PASSWORD :'pw';\n" |
  docker exec -i "$PG" psql -v ON_ERROR_STOP=1 -q -U postgres -d postgres -v pw="$NEW_APP_PASSWORD"
printf '%s' "$NEW_APP_PASSWORD" >"$WORKDIR/app/password"
closed="$(docker exec "$PG" psql -v ON_ERROR_STOP=1 -qtA -U postgres -d postgres \
  -c "SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE usename = 'app_user'")"
[ "$closed" -ge 1 ] || fail "the API had no open session to close"
for _ in $(seq 1 10); do
  [ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/readyz")" = 200 ] && break
  sleep 1
done
expect "list after rotation" 200 GET /api/todos
[ "$(docker inspect -f '{{.State.StartedAt}}' "$API")" = "$API_STARTED" ] || fail "the API restarted during rotation"
pass "API reconnected with the rotated password, no restart"

echo "== tls verification"
# The API must refuse a server it cannot verify. It still starts (it does not
# connect until asked), so /healthz answers and /readyz must report 503 with
# the certificate error in the log. Three samples, so a slow start cannot pass
# as a refusal.
expect_tls_refused() { # container binding-dir log-pattern description
  local port code
  docker run -d --name "$1" --network "$NET" -p 127.0.0.1::8080 \
    -v "$2:/bindings/todo-db-app:ro" -e DATABASE_BINDING=todo-db-app "$API_IMAGE" >/dev/null
  port="$(docker port "$1" 8080/tcp | head -n1)"
  port="${port##*:}"
  for _ in $(seq 1 30); do
    curl -sf -o /dev/null "http://127.0.0.1:$port/healthz" && break
    sleep 1
  done
  for _ in 1 2 3; do
    code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/readyz")"
    [ "$code" = 503 ] || { docker logs "$1"; fail "$4: readyz = $code, want 503"; }
  done
  docker logs "$1" 2>&1 | grep -q "$3" || { docker logs "$1"; fail "$4: no '$3' in the API log"; }
  pass "$4: readyz 503, log has '$3'"
}
expect_tls_refused "$BAD_HOST_API" "$WORKDIR/app-bad-host" "not $UNLISTED_HOST" "host not in the server certificate"
expect_tls_refused "$BAD_CA_API" "$WORKDIR/app-bad-ca" "certificate signed by unknown authority" "server certificate from another CA"

echo "== web"
docker run -d --name "$WEB" -p "127.0.0.1:$WEB_PORT:8080" "$WEB_IMAGE" >/dev/null
for _ in $(seq 1 15); do
  curl -sf -o /dev/null "http://127.0.0.1:$WEB_PORT/healthz" && break
  sleep 1
done
curl -sfi "http://127.0.0.1:$WEB_PORT/" >"$WORKDIR/web.txt" || fail "web index"
grep -qi "^content-security-policy: default-src 'self'" "$WORKDIR/web.txt" || fail "web CSP header missing"
grep -q '<div id="root">' "$WORKDIR/web.txt" || fail "web index body"
pass "web serves the built frontend with its CSP"
[ -z "$(docker inspect -f '{{range .Mounts}}{{.Destination}}{{end}}' "$WEB")" ] || fail "web has a mount"
pass "web container has no volume mounts"

echo "All checks passed."
