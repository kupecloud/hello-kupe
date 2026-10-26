# From signup to your first game

Create a tenant, export its API key, provision infrastructure, then deploy the
current game from this checkout. Terraform/OpenTofu owns the cluster, managed
PostgreSQL database, database bindings, managed registry, cluster pull binding
and optional build token. Helm owns the app, public route and migration Job.

## 1. Create your tenant and API key

Create a tenant at [kupe.cloud/signup](https://kupe.cloud/signup), verify your
email and sign in to the [console](https://console.kupe.cloud). In **Settings →
API Keys**, create an **admin** API key for this tenant. The build-token resource
requires admin access; the tenant's plan must allow clusters, managed databases
and a registry.

Install Git, Node.js 22+, Docker with Buildx, Helm, kubectl, the Kupe CLI and
[OpenTofu](https://opentofu.org/docs/intro/install/) or Terraform. Docker must be
running and support builds for Linux AMD64 and ARM64. Install the current Kupe
CLI using the [signup guide](https://docs.kupe.cloud/get-started/sign-up/).
This API-key workflow does not require `kupe auth login`.

```bash
git clone https://github.com/kupecloud/hello-kupe.git
cd hello-kupe
read -s 'KUPE_API_KEY?Demo tenant API key: '; echo
export KUPE_API_KEY
```

The input command above is for zsh (the default on macOS). In bash, use
`read -rsp 'Demo tenant API key: ' KUPE_API_KEY; echo`, then export it.
The key stays out of shell history and is never passed as a command argument.
If you have `KUPE_TOKEN` set for the provider, unset it for this workflow.

## 2. Prepare and review infrastructure

```bash
make cloud-init TENANT=your-tenant
make cloud-plan
```

`cloud-init` creates ignored `infra/terraform.tfvars` only if it does not already
exist, then initializes OpenTofu. The fresh-demo settings create:

- Cluster `demo`, with 2 CPU, 4 GiB memory and 20 GiB storage.
- PostgreSQL 18 database `hello-kupe-db`, with 250m CPU, 512 MiB memory and 5 GiB storage.
- Readwrite binding in `hello-kupe` and owner binding in `hello-kupe-migrate`.
- The tenant's managed registry and a cluster binding for image pulls.
- A seven-day registry push token for building the app.

Review `infra/terraform.tfvars` and the plan before applying. Do not change the
names of an existing deployment to adopt these fresh-demo names. The underlying
variable defaults still preserve `todo` / `todo-db` for earlier users.

A tenant has one registry. If it already exists and this configuration should
manage it, import it **before planning/applying**:

```bash
tofu -chdir=infra import 'kupe_registry.app[0]' registry
```

Use a fresh demo tenant if you want the simplest path without shared-resource
ownership. See [infra/README.md](../infra/README.md) for existing state migration,
registry ownership and deletion protection.

## 3. Provision

```bash
make cloud-apply
```

Review the displayed plan and type `yes` to create the resources. The provider
waits for provisioning; the deployment workflow also waits for the cluster and
database Secrets. Resource sizes are explicit in the plan; check your tenant's
current plan and pricing before applying.

The build token is a sensitive output but is stored in local Terraform state.
Keep that state private; use an encrypted backend with access controls for a
shared environment. Database passwords never pass through Terraform. Do not
commit state, variable files, build credentials or kubeconfig.

## 4. Deploy the current game

```bash
make cloud-deploy
```

This command reads the applied Terraform outputs and:

1. Generates `.tmp/cloud/<tenant>/<cluster>/kubeconfig`, readable only by you.
2. Waits for the app and migration database Secrets.
3. Signs Docker in with the build token, builds this checkout for Linux AMD64
   and ARM64, and pushes a uniquely tagged image to the managed registry.
4. Writes `.tmp/cloud/<tenant>/<cluster>/values.json` with the image digest,
   database Secret names, namespaces and two app replicas.
5. Runs `helm upgrade --install`, including the migration hook, and waits for
   the app rollout. It prints the public game URL when complete.

Your default Kubernetes context is unchanged. All kubectl and Helm calls name
both the generated kubeconfig and the Terraform-derived context explicitly.
The kubeconfig embeds the API key: it expires/revokes with that key. Run
`make cloud-kubeconfig` after replacing your exported key, or run
`make cloud-deploy` to regenerate it and deploy again.

The serving app receives only the readwrite binding. Schema migration runs in
its own namespace with the owner binding, before the app starts. The Next.js
frontend, Go backend and migrations all use the same container image.

The fresh-demo URL is:

```text
https://hello-kupe.demo.<tenant>.clusters.kupe.cloud
```

Public DNS and TLS may take a little longer after the pods become ready.
`cloud-deploy` verifies the Kubernetes rollout; open the printed URL to verify
public access. The scoreboard should say **Kept safe in managed PostgreSQL**.
Play a round and save your name to check the database path end to end.

Check the workload at any time:

```bash
make cloud-status
```

Re-run `make cloud-deploy` after code changes. It builds a new image, pins its
digest and reruns pending migrations. If the build token expires, run
`make cloud-apply` to refresh infrastructure/token state first. For an existing
setup without a build token, set `enable_build_token = true` before applying.

To use Terraform instead of OpenTofu, append `TF=terraform` to each Make command
and use `terraform` for direct commands. Stick with one tool for a given state.

## 5. Inspect or extend the deployment

For manual commands, point at the generated file explicitly:

```bash
export KUBECONFIG="$PWD/.tmp/cloud/your-tenant/demo/kubeconfig"
kubectl -n hello-kupe get pods
kubectl -n hello-kupe logs deployment/hello-kupe
kubectl -n hello-kupe rollout restart deployment/hello-kupe
kubectl -n hello-kupe rollout status deployment/hello-kupe
```

Refresh the game: your saved score remains after the restart. Both app replicas
share PostgreSQL, so either can continue the same game. Unfinished games expire
after 30 minutes; submitted scores remain until the operator removes them.

Use Helm for deployment rather than `kubectl apply` on a directory. The chart
uses pre-install/pre-upgrade migration hooks, which plain kubectl does not run
as Helm hooks. The generated values file is also suitable for reviewing the
configuration or moving it into GitOps; see the [Argo CD instructions](../README.md#deploy-with-gitops).

More things to try:

- **Custom domain:** use the game's top banner to find the guide, then configure
  DNS and `httpRoute.hostname` through your Helm/GitOps values.
- **Observability:** play and save a score, then inspect real requests in logs
  and Grafana. The chart enables scraping of `/metrics`; there are no dummy errors.
- **Scaling:** the demo starts with two app replicas. Enable autoscaling through
  Helm/GitOps and use `/api/work?ms=50` as described in the
  [main README](../README.md#autoscaling).

The display names are public and unverified. Anyone with the URL can play and
submit a result. The server validates moves and calculates scores, but this is
an educational demo without accounts, moderation or bot prevention.

## 6. Clean up

Using the kubeconfig from the previous section:

```bash
helm uninstall hello-kupe --namespace hello-kupe
kubectl -n hello-kupe-migrate delete job hello-kupe-migrate --ignore-not-found
```

Follow [infrastructure cleanup](../infra/README.md#clean-up) to disable deletion
protection and destroy the infrastructure. This deletes the cluster and database;
if the registry is included, its images are permanently deleted too. Keep a
shared registry outside this demo's lifecycle if other applications use it.
