# Hello Kupe infrastructure

This directory owns the app's Terraform/OpenTofu infrastructure. Application
workloads are deployed separately with Helm or Argo CD.

| File | Resources or configuration |
| --- | --- |
| `versions.tf`, `provider.tf` | Kupe provider and authentication |
| `variables.tf`, `terraform.tfvars.example` | Tenant and optional settings |
| `cluster.tf` | Kubernetes cluster, named `todo` by default |
| `database.tf` | Managed PostgreSQL 18 database, named `todo-db` |
| `bindings.tf` | Owner, readwrite and optional readonly database bindings |
| `registry.tf` | Optional managed registry and cluster pull binding |
| `build-token.tf` | Optional seven-day build token (`enable_build_token`) |
| `outputs.tf` | Deployment settings, database host and binding Secret names |

The owner binding targets `hello-kupe-migrate` for schema migrations. The readwrite
binding targets `hello-kupe` for the Go backend. The Next.js static frontend runs
in the same image; it never receives database credentials. Set `enable_reports = true` to add readonly access in `todo-reports`. Bindings create their target
namespaces and deliver credentials as Secrets; passwords do not pass through
Terraform. Override `app_namespace` and `migration_namespace` together with the
Helm release namespace and `migrations.namespace` if needed.

## Quick path for a fresh demo tenant

From the repository root, export `KUPE_API_KEY`, then run:

```bash
make cloud-init TENANT=your-tenant
make cloud-plan
make cloud-apply
make cloud-deploy
```

`cloud-apply` asks for Terraform's confirmation. `cloud-deploy` builds and pushes
this checkout and deploys with Helm, including migrations. It generates a private
repo-local kubeconfig and values file under `.tmp/cloud/`; your default Kubernetes
context stays unchanged. See the [full walkthrough](../docs/deploy-from-scratch.md)
for prerequisites, API-key input, existing registries and verification.

The generated fresh-demo variable file uses cluster `demo`, database
`hello-kupe-db`, managed registry and a seven-day build token. It preserves an
existing variable file. Bare variable defaults retain `todo` / `todo-db` for
compatibility with earlier deployments; keep names stable when reusing state.

## Provision manually

You need a Kupe tenant with managed databases enabled, an API key permitted to
create the resources, and Terraform or OpenTofu. The build-token option requires an admin API key. The configuration uses Kupe provider 1.11 or later within version 1; it has
been validated with 1.11.2, recorded in the dependency lock file.

From the repository root:

```bash
cd infra
cp terraform.tfvars.example terraform.tfvars
# Set tenant in terraform.tfvars.
export KUPE_API_KEY=<api-key>
tofu init
tofu plan
tofu apply
```

Terraform users can replace `tofu` with `terraform`. The checked-in
`.terraform.lock.hcl` keeps provider selection repeatable; use `tofu init -upgrade`
when deliberately upgrading it. Local variable files and state are ignored by Git.

The cluster requests 2 CPU, 4 GiB memory and 20 GiB storage. The database requests
250m CPU, 512 MiB memory and 5 GiB storage. Review the infrastructure plan and
your tenant's current plan/pricing before applying.

## Managed registry

Set `enable_registry = true` before applying to provision the tenant registry
and bind the cluster for image pulls. The `registry_endpoint` output gives the
image path prefix to use when building and pushing the app images. Push using
a separate CLI or CI credential; application namespaces receive no push access.

For API-key-driven builds, set `enable_build_token = true` and apply. The build
secret is stored in Terraform state, so keep state private and encrypted for
shared use. `make cloud-deploy` sends it to Docker through standard input.
Human users can instead use `kupe registry login` after `kupe auth login`; that
login command does not accept API keys.

If you previously copied `push-token.tf.example` to `push-token.tf`, remove the
old copied file and set `enable_build_token = true`. The checked-in moved block
preserves the existing token's state address; review the plan before applying.

A tenant has only one registry. If yours already exists and should be managed
here, enable the option and import it before planning:

```bash
tofu import 'kupe_registry.app[0]' registry
```

A registry is shared infrastructure: review its other users before changing or
destroying it. Leave the option off if it is managed elsewhere.

## Deploy the app

Follow the [signup-to-game walkthrough](../docs/deploy-from-scratch.md) to build,
push and deploy the main chart with `database.enabled=true`. Terraform owns the
cluster, database, registry and bindings; the chart owns the workloads and routes.

For the retained todo example, set `app_namespace = "todo-api"` and
`migration_namespace = "todo-migrate"`, then follow its
[walkthrough](../samples/managed-database/README.md).

## Moving an existing setup

This configuration replaces `samples/managed-database/terraform/`. Resource
addresses and cluster/database names are unchanged. The namespace defaults now
target the main game. Set `app_namespace = "todo-api"` and
`migration_namespace = "todo-migrate"` to preserve existing todo bindings. If you previously applied it, move
your local `terraform.tfstate`, any `terraform.tfstate.backup`, variable files and
`.terraform.lock.hcl` from that directory into `infra/` before initializing.
If you configured a remote backend, carry its configuration over instead.
Run `tofu plan` and confirm it proposes no unexpected changes before applying.
Do not initialize an empty state and recreate an existing deployment.

## Clean up

Uninstall the application chart first. The database has both Kupe deletion
protection and Terraform's `prevent_destroy` enabled. To intentionally remove it:

1. Remove the `lifecycle` block from `database.tf`.
2. Set `deletion_protection = false` in that file.
3. Run `tofu apply` to disable platform deletion protection.
4. If the registry is enabled, also remove its `lifecycle` block and set
   `deletion_protection = false` in `registry.tf`, then run `tofu apply`.
5. Run `tofu destroy` to remove the bindings, database, cluster and optional registry.

Deleting the registry permanently removes every image it contains.

Destroying this configuration also destroys the cluster. See the app
[cleanup instructions](../samples/managed-database/README.md#clean-up) for details
about migration Jobs and namespaces when retaining an existing cluster.
