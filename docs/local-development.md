# Local development

Use Go matching `go.mod`, Node.js 22.15 or later and npm. Docker is needed only
for PostgreSQL and image checks.

## Practice mode

```bash
make dev
```

Open `http://localhost:3000`. This installs frontend dependencies when needed,
builds and starts the Go backend on port 8080, and starts Next.js with frontend
hot reload. Ctrl+C stops both servers. Restart `make dev` after Go code changes.

Scores in practice mode are in memory, expire with their sessions, and disappear
on restart. To choose different ports:

```bash
make dev PORT=8081 WEB_PORT=3001
```

The frontend proxy and allowed development origin follow those ports automatically.
The command also accepts `DATABASE_URL` or mounted database bindings through the
environment. It does not create cloud resources or start a database.

For a production-style static frontend served directly by Go, use
`make web-install` followed by `make run`, then open `http://localhost:8080`.
Production uses a static export, so no Next.js server is needed.

## PostgreSQL mode

Start the local database and apply migrations as the owner:

```bash
make db-up
DATABASE_URL='postgres://app_owner:local-owner@127.0.0.1:15432/hello_kupe?sslmode=disable' make migrate
```

Start the serving process with the restricted application role:

```bash
DATABASE_URL='postgres://app_readwrite:local-readwrite@127.0.0.1:15432/hello_kupe?sslmode=disable' make dev
```

The plain-text connection and fixed development passwords are for the local
Docker database only. On Kupe, set `DATABASE_BINDING` and mount its Secret under
`SERVICE_BINDING_ROOT` instead; bindings require TLS with hostname and CA
verification. New connections read the mounted password again after rotation.

Stop the database with `docker compose down`. Its named volume persists; use
`docker compose down --volumes` only when intentionally discarding local scores.

## Kubernetes with kind

To test the container, Helm chart, database bindings and migrations together,
install Docker Desktop, kind, kubectl, Helm, OpenSSL and Node.js, then run:

```bash
make kind-dev
```

Open `http://localhost:18080`. This builds the current image and deploys it to
the isolated `hello-kupe-local` cluster with:

- PostgreSQL 18 on a persistent volume.
- Two application replicas sharing sessions and scores.
- A migration Job in `hello-kupe-migrate` using the owner role.
- A readwrite binding only in the application namespace, `hello-kupe`.
- TLS connections with CA and hostname verification.

Random local passwords, certificates and keys stay under ignored `.tmp/kind/`.
Keep that directory while retaining the database. The isolated kubeconfig is
`.tmp/kind-kubeconfig`; commands leave your default Kubernetes context alone.
The local database is configured by the script; Kupe's managed provisioning
remains in `infra/` for deployment on Kupe.

Ctrl+C stops port forwarding and leaves the cluster and database running.
To rebuild and redeploy after changes, use `make kind-up`. Then reconnect with
`make kind-forward`; forwarding automatically reconnects after app pod restarts.
Use `make kind-status` to inspect local pods and storage.

App rollouts and database pod restarts retain the scoreboard. To remove the
entire local cluster **and its database data**, run `make kind-down`.

The development server at `http://localhost:3000` is independent of this
deployment and retains the database configuration supplied to `make dev`.

## Verification

```bash
make test
make web-test
make web-build
make helm-lint
tofu -chdir=infra init -backend=false
make infra-check
```

To include PostgreSQL integration tests against the migrated local database:

```bash
TEST_DATABASE_URL='postgres://app_readwrite:local-readwrite@127.0.0.1:15432/hello_kupe?sslmode=disable' \
  GOCACHE="$PWD/.tmp/go-build" go test -race ./...
```

These check transaction locking across pools, persistence after reconnecting,
rollback and that the runtime role cannot create tables or touch migration
history. Unit tests cover hidden cards, matching, advancing past mismatches,
timing, malformed requests, score submission and cross-origin writes. CI also builds the frontend
and container and validates the chart with and without a database.
