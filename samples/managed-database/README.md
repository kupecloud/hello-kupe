# Managed database sample

A small todo app that keeps its data in a Kupe managed PostgreSQL database. It
shows the parts most applications need: a schema migration, an API that reads
and writes rows, and a frontend that never sees a database credential.

| Component | Namespace | What it is | Database access |
| --- | --- | --- | --- |
| `todo-migrate` | `todo-migrate` | Job that applies the SQL migrations before each install and upgrade | owner role (`app_owner`): can change the schema |
| `todo-api` | `api` | Go API: `/api/todos` plus `/healthz` and `/readyz` | app role (`app_user`): reads and writes rows only |
| `todo-web` | `web` | React frontend, served as static files | none |

```text
browser ── https://todo.<cluster>.<tenant>.clusters.kupe.cloud
  ├── /api/*  HTTPRoute (api) ── todo-api ──── TLS, app role ────┐
  └── /*      HTTPRoute (web) ── todo-web (static files)          ├── todo-db
todo-migrate Job (todo-migrate) ───────────── TLS, owner role ───┘
```

Both routes use the same hostname. The gateway sends `/api` to the API and
everything else to the frontend, so the browser calls the API on its own origin
and the frontend needs no API address and no CORS.

The API has no authentication. Anyone who can reach the hostname can read,
change and delete every todo, so remove the sample when you have finished with
it (see [Clean up](#clean-up)) or keep it private, as described under
[API](#api).

## Contents

* `cmd/todo-api`, `internal/` - the API and the migration runner (one binary, one image)
* `internal/migrate/migrations` - SQL migrations, embedded in the binary
* `cmd/todo-web`, `web/` - the frontend and the small Go server that serves it
* `chart/` - Helm chart for all three components
* `terraform/` - the cluster, the database and both bindings
* `verify/db-probe.yaml` - a Pod that checks whether a namespace can reach the database
* `scripts/local-smoke.sh` - the whole flow in Docker, without a Kupe cluster

## What you need

* A Kupe tenant whose plan includes managed databases.
* `kubectl` access to the cluster, `helm`, and `docker` (or another image builder).
* Either OpenTofu or Terraform with a `kupecloud/kupe` provider release that
  includes `kupe_database`, or the `kupe` CLI.

## 1. Create the database

With Terraform or OpenTofu, `terraform/main.tf` creates a cluster named `todo`,
a database named `todo-db`, and both bindings:

```bash
cd terraform
export KUPE_API_KEY=<api key>
tofu init
tofu apply -var tenant=<tenant>
```

To use a cluster you already have, delete the `kupe_cluster` resource and set
`cluster` in both bindings to its name.

The database has `deletion_protection = true`, so the platform refuses to delete
it, and `lifecycle { prevent_destroy = true }`, so Terraform refuses to plan the
delete in the first place. [Clean up](#clean-up) shows how to remove it.

With the CLI instead:

```bash
kupe database create todo-db --version 18 --cpu 250m --memory 512Mi --storage 5Gi
```

`--wait` is on by default, so the command returns once the database is
`Running`. The PostgreSQL database inside is named `todo_db`: each `-` in the
Kupe name becomes `_`.

## 2. Add the bindings

Each role gets its own namespace. The owner binding targets `todo-migrate`,
where only the migration Job runs, and the app binding targets `api`. Create
`web` yourself:

```bash
kubectl create namespace web
```

Do not create `api` or `todo-migrate`: a binding creates the namespace it
targets when it delivers its Secret.

Terraform has already added the bindings. With the CLI:

```bash
kupe database binding add todo-db --cluster <cluster> --namespace todo-migrate --role owner
kupe database binding add todo-db --cluster <cluster> --namespace api --role app
```

Each binding delivers a Secret named `{database}-{role}` and a Service named
after the database into its namespace:

```bash
kubectl -n api get service todo-db
kubectl -n api get secret todo-db-app
kubectl -n todo-migrate get service todo-db
kubectl -n todo-migrate get secret todo-db-owner
```

List the keys without printing any values:

```bash
kubectl -n api get secret todo-db-app \
  -o go-template='{{range $k, $v := .data}}{{$k}}{{"\n"}}{{end}}'
```

The keys follow [servicebinding.io](https://servicebinding.io/): `type`,
`provider`, `host`, `port`, `database`, `username`, `password`, `sslmode`,
`ca.crt` and `uri`. `host` names the Service in the binding's own namespace:
`todo-db.api.svc.cluster.local` for the app binding,
`todo-db.todo-migrate.svc.cluster.local` for the owner binding. `uri` holds the
same details as one connection string, with `sslrootcert` set to
`/bindings/<secret name>/ca.crt`. That is where the chart mounts each Secret
(`SERVICE_BINDING_ROOT=/bindings`), so the path is correct whichever form an
application reads. This sample reads the separate keys.

Nothing is created in `web`. That namespace has no binding.

## 3. Build the images

Build from this directory and push to a registry your cluster can pull from:

```bash
docker build -f Dockerfile.api -t <registry>/todo-api:0.1.0 .
docker build -f Dockerfile.web -t <registry>/todo-web:0.1.0 .
docker push <registry>/todo-api:0.1.0
docker push <registry>/todo-web:0.1.0
```

`todo-api` is one image for both the API and the migration: the Job runs it with
`migrate`, the Deployment with `serve`. The migrations are compiled into the
binary, so a Job always applies the schema that version of the API expects.

The chart renders `repository:tag`, so a tag of the form
`0.1.0@sha256:<digest>` pins the exact build you pushed.

## 4. Deploy

One Helm release installs all three components, in this order:

```bash
helm upgrade --install todo ./chart \
  --namespace api \
  --set tenant=<tenant> \
  --set cluster=<cluster> \
  --set api.image.repository=<registry>/todo-api \
  --set api.image.tag=0.1.0 \
  --set web.image.repository=<registry>/todo-web \
  --set web.image.tag=0.1.0
```

If you changed the binding Secret names, set `database.ownerSecretName` and
`database.appSecretName` to match. If the owner binding targets another
namespace, set `migrations.namespace` to it.

### Migrations

The `todo-migrate` Job is a Helm `pre-install,pre-upgrade` hook, so it runs and
must succeed before the API is rolled out. Argo CD runs the same hook as a
`PreSync` hook, on every sync. The Job and its ServiceAccount are created in
`todo-migrate`, not in the release namespace.

It mounts `todo-db-owner` at `/bindings/todo-db-owner` and applies
`internal/migrate/migrations` with [goose](https://github.com/pressly/goose).
Running it again is safe. Applied versions are recorded in `goose_db_version`
and skipped, each migration runs in its own transaction, and an advisory lock
stops two Jobs applying the same migration at once.

```bash
kubectl -n todo-migrate logs job/todo-migrate
```

A first run logs `"msg":"schema up to date","version":1,"applied":1`; every run
after that logs `"applied":0`.

Kupe sets default privileges on every database so the app role can read and
write rows in any table the owner role creates. `todos` needs exactly that, so
the migration has no `GRANT`. The same default privileges would also let the
app role write to `goose_db_version` and make a later run skip a migration, so
the first migration revokes every privilege the app role has on that table.

### API

`todo-api` mounts `todo-db-app` at `/bindings/todo-db-app` and reads one file
per key. It sets `sslmode=verify-full` with `sslrootcert` pointing at the
mounted `ca.crt`: the server certificate must be signed by that CA and carry the
host name in `host`. It refuses to start if the Secret asks for any other
`sslmode`.

The API does not connect at startup. `/readyz` pings the database and
`/healthz` does not, so a database outage takes pods out of the Service without
restarting them. Only `/api` is routed publicly.

```bash
kubectl -n api rollout status deploy/todo-api
```

To keep the API off the internet, install with `--set httpRoute.enabled=false`
and reach it through a port-forward:

```bash
kubectl -n api port-forward svc/todo-api 8080:80
```

### Frontend

`todo-web` serves the built React app with a Content-Security-Policy that only
allows its own origin. Its pod mounts no Secret, has no database settings, and
has no ServiceAccount token.

```bash
kubectl -n web rollout status deploy/todo-web
```

### With Argo CD

The same chart deploys from Git. Fill in `destination.name` as described in the
[top-level README](../../README.md#deploy-it):

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: todo
  namespace: argocd
spec:
  project: <tenant>
  source:
    repoURL: https://github.com/kupecloud/hello-kupe.git
    targetRevision: main
    path: samples/managed-database/chart
    helm:
      releaseName: todo
      values: |
        tenant: <tenant>
        cluster: <cluster>
        api:
          image:
            repository: <registry>/todo-api
            tag: "0.1.0"
        web:
          image:
            repository: <registry>/todo-web
            tag: "0.1.0"
  destination:
    name: <tenant>--<cluster>-<suffix>
    namespace: api
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

## 5. Verify

Open `https://todo.<cluster>.<tenant>.clusters.kupe.cloud` and add a todo, or
use the API directly:

```bash
HOST=https://todo.<cluster>.<tenant>.clusters.kupe.cloud
curl -s -X POST "$HOST/api/todos" -H 'Content-Type: application/json' -d '{"title":"try it"}'
curl -s "$HOST/api/todos"
```

Check that `web` has no binding Secret:

```bash
kubectl -n web get secrets
```

Check the network path, with `api` as the control:

```bash
kubectl -n api apply -f verify/db-probe.yaml
kubectl -n web apply -f verify/db-probe.yaml
kubectl -n api get pod db-probe   # Completed: the database answered
kubectl -n web get pod db-probe   # Error: no connection within 5 seconds
kubectl -n api delete pod db-probe
kubectl -n web delete pod db-probe
```

## Isolation

What the platform guarantees for this layout:

* **Each role's Secret reaches one namespace.** A binding delivers its Secret
  into the one namespace it names: the owner role's into `todo-migrate`, the
  app role's into `api`. `web` has no binding, so it has no Secret to mount, and
  Kubernetes RBAC never grants a database login.
* **The network path is opened for bound namespaces only.** Kupe allows
  connections to the database port from pods in bound namespaces of the bound
  cluster, and drops everything else. The rules live outside your cluster, so
  nothing inside it can widen them: pods in `web`, other namespaces and other
  clusters cannot connect.
* **TLS is required.** The database rejects connections without TLS, and the
  sample verifies the server certificate and host name against the CA in the
  binding.
* **Two roles, neither a superuser.** `app_user` can read and write rows in
  tables the owner creates. It cannot create, alter or drop tables:
  `CREATE TABLE` fails with `permission denied for schema public`.

What it does not separate:

* **Pods in `api` share the app role.** Any workload in `api` can mount
  `todo-db-app` and reach the database. Keep unrelated workloads out of that
  namespace. The owner role stays out of `api` because its binding targets
  `todo-migrate`.
* **Pods in `todo-migrate` share the owner role.** Run nothing there except the
  migration Job.
* **Removed access.** Deleting a binding removes the Secret and the network path,
  not knowledge of the password. Rotate the role's credentials afterwards.

## Rotating credentials

```bash
kupe database rotate-credentials todo-db --role app
```

The old password stops working immediately and the bound Secrets are updated.
The API needs no restart: it reads `password` from the mounted Secret each
time it opens a database connection, so new connections use the new password
once the kubelet has refreshed the mounted file. Until then, new connections
fail. PostgreSQL does not end a session when its role's password changes, so
connections that are already open keep working; the API closes each one about
15 minutes after it was opened and replaces it.

The CA in `ca.crt` is read only when the API starts. Restart it after the CA
changes:

```bash
kubectl -n api rollout restart deploy/todo-api
```

The migration Job reads its Secret when it starts, so the next run after an
owner rotation uses the new password.

## Clean up

With Helm and the CLI:

```bash
helm uninstall todo --namespace api
kupe database binding list todo-db
kupe database binding delete todo-db <app binding name>
kupe database binding delete todo-db <owner binding name>
kubectl delete namespace web api todo-migrate
kupe database update todo-db --deletion-protection=false
kupe database delete todo-db
```

`helm uninstall` leaves the last `todo-migrate` Job behind, because Helm does
not track hook resources; deleting the namespace removes it.

With Terraform or OpenTofu, uninstall the chart, then remove the `lifecycle`
block from `kupe_database.todo`, set `deletion_protection = false`, run
`tofu apply`, and then `tofu destroy`. That destroys the `todo` cluster as well
as the database and both bindings. If you pointed the bindings at a cluster you
already had, delete the `web`, `api` and `todo-migrate` namespaces afterwards.

Deleting a database removes it immediately. Its storage is kept for 7 days for
support-assisted recovery, then permanently deleted. There is no self-service
restore in v1.

## Local development

```bash
make test       # Go unit tests
make web-test   # frontend type-check and tests
make helm-lint
make smoke      # the full flow in Docker
```

`make smoke` runs `scripts/local-smoke.sh`. It starts PostgreSQL 18 with a
self-signed CA, sets up the same roles, grants and TLS-only `pg_hba.conf` as a
Kupe database, then runs the migration as the owner role (twice), the API as the
app role, and CRUD through the API. It checks that the app role cannot create a
table or use `goose_db_version`, that a plaintext connection is refused, that
the API keeps working after the app role's password is rotated without being
restarted, and that it reports not ready when the server certificate does not
name its host or was signed by another CA. It removes every container, network and image
it created when it exits. The PostgreSQL image is pinned by digest; set
`PG_IMAGE` to pull it from a mirror.

For frontend work, run the API locally on port 8080 and `npm run dev` in `web/`:
Vite forwards `/api` to it.
