# Hello Kupe

`hello-kupe` is the official Kupe Cloud quickstart app: a Next.js memory game
served by Go, with a scoreboard backed by managed PostgreSQL.

Match six pairs to discover custom domains, databases, registries, autoscaling,
observability and GitOps. Each match unlocks a short Kupe lesson. Finish the board,
enter a display name and save your result. The server controls card reveals,
counts moves and calculates time; the browser cannot submit an arbitrary score.

**Start here:** [From signup to your first game](docs/deploy-from-scratch.md).
The walkthrough covers infrastructure, building an image, deployment, persistence,
custom domains and cleanup. [The implementation plan](docs/game-plan.md) describes
the design and verification scope.

<!-- toc -->

* [Deploy it](#deploy-it)
  * [Fresh tenant: provision and deploy](#fresh-tenant-provision-and-deploy)
  * [Deploy with GitOps](#deploy-with-gitops)
  * [Or install the chart directly](#or-install-the-chart-directly)
* [Overview](#overview)
  * [Endpoints](#endpoints)
* [Autoscaling](#autoscaling)
* [Repo layout](#repo-layout)
* [Local development](#local-development)

<!-- Regenerate with "pre-commit run -a markdown-toc" -->

<!-- tocstop -->

## Deploy it

### Fresh tenant: provision and deploy

Create a tenant at [kupe.cloud/signup](https://kupe.cloud/signup), then create an
**admin API key** in **Settings → API Keys** in the Kupe console. Install Node.js
22+, Docker with Buildx, the Kupe CLI, kubectl, Helm and OpenTofu (or Terraform).
Docker must be running.

From a checkout of this repository, export the key locally as `KUPE_API_KEY`.
Then run:

```bash
make cloud-init TENANT=your-tenant
make cloud-plan
make cloud-apply
make cloud-deploy
```

`cloud-init` creates `infra/terraform.tfvars` for a fresh demo and initializes
OpenTofu. It preserves an existing variable file. Review the plan before
confirming `cloud-apply`: it provisions the cluster, managed PostgreSQL,
owner/readwrite database bindings, managed registry, cluster pull binding and a
seven-day build token.

`cloud-deploy` builds this checkout, pushes the image and installs the chart
with two app replicas and persistent scores. It runs schema migrations before
the app starts and prints your public URL:

```text
https://hello-kupe.demo.<tenant>.clusters.kupe.cloud
```

The generated kubeconfig and Helm values live under
`.tmp/cloud/<tenant>/<cluster>/`, outside Git. The kubeconfig is readable only by
you, embeds the API key and expires/revokes with it. Deployment commands target
that file explicitly; your default Kubernetes context is unchanged.

```bash
make cloud-kubeconfig # Generate or refresh just the repo-local kubeconfig
make cloud-status     # Show pods, service, route and public URL
```

Re-run `make cloud-deploy` after code changes. Use `TF=terraform` on each Make
command if you prefer Terraform. The build token is stored in Terraform state;
keep state private. An existing tenant registry must be imported before apply if
this configuration will manage it.

Follow [From signup to your first game](docs/deploy-from-scratch.md) for secure
API-key input, prerequisites, existing registries, token renewal, persistence
checks and cleanup. Use **Helm** for this route: plain `kubectl apply` does not
run the chart's pre-deployment migration hooks.

### Deploy with GitOps

For an existing Kupe cluster, apply an Argo CD `Application` and let the platform
deploy the chart from Git. The default chart image follows the published release;
the fresh-tenant workflow above builds the game from your current checkout.

```yaml
# hello-kupe.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: hello-kupe
  namespace: argocd
spec:
  project: <tenant>
  source:
    repoURL: https://github.com/kupecloud/hello-kupe.git
    targetRevision: main
    path: chart
    helm:
      releaseName: hello-kupe
      values: |
        tenant: <tenant>
        cluster: <cluster>
  destination:
    name: <tenant>--<cluster>-<suffix>
    namespace: hello-kupe
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

```bash
kubectl apply -f hello-kupe.yaml
```

**`destination.name` is the one value you cannot type from memory.** The `<suffix>`
is derived from your cluster's internal ID. Open Argo CD (the grid icon in the Kupe
console, or `argocd.kupe.cloud`), go to **Settings → Clusters**, and copy the name
exactly. Only your own clusters are listed, so anything you see there is safe to
copy.

By default the chart runs a single-replica practice game with temporary scores.
For persistent results, provision [infra/](infra/README.md), use namespace
`hello-kupe`, and add these Helm values to the Application above:

```yaml
database:
  enabled: true
  readwriteSecretName: <database-name>-readwrite
  ownerSecretName: <database-name>-owner
migrations:
  namespace: hello-kupe-migrate
```

Use the Secret names from your Terraform outputs and the image repository/tag
from the generated Helm values when deploying this checkout through GitOps.
The bindings must be ready before Argo CD syncs. The owner binding belongs only
in the migration namespace; the application uses the readwrite binding. To try
unreleased code, build and override the image as described in the walkthrough.

Then open the app:

```text
https://hello-kupe.<cluster>.<tenant>.clusters.kupe.cloud
```

Two things about this route that surprise people:

- **`kubectl get application -n argocd` shows a blank status forever.** The
  `Application` is exported up to the platform's Argo CD, and the status is not
  copied back down. Check `kubectl get deploy,pods -n hello-kupe` instead, or look
  at the app in Argo CD.
- **`spec.project` is overwritten** with your tenant name whatever you put in it.
  That is the platform pinning the application to your own project.

Full walkthrough, including how to find your tenant and cluster names:
[Deploy your first app](https://docs.kupe.cloud/get-started/deploy-an-app/).

### Or install the chart directly

Useful when you are changing the chart itself. This one does need the repo cloned,
because the chart is not published to a registry:

```bash
helm upgrade --install hello-kupe ./chart \
  --namespace hello-kupe \
  --create-namespace \
  --set tenant=<tenant> \
  --set cluster=<cluster>
```

`tenant` and `cluster` only build the hostname and the page content; set `domain`
too if your platform is not on `kupe.cloud`. For a second copy in the same tenant,
override the hostname:

```bash
--set httpRoute.hostname=my-app.example.com
```

## Overview

It is intentionally small, it showcases the main platform paths that new users need
to focus on day one:

* **ArgoCD** deploys it from Git as a Helm chart
* **Gateway API** exposes it with an `HTTPRoute`
* **Grafana Loki** gets structured JSON logs automatically
* **Grafana Metrics** scrape `/metrics` automatically from pod annotations
* **Horizontal autoscaling** scales it out on CPU, using the metrics-server
  your tenant cluster already proxies

The app serves the game and API under the same hostname, with health and metrics
endpoints. In PostgreSQL mode, sessions and submitted results survive restarts
and are shared by replicas. Unfinished sessions expire after 30 minutes.
Submitted display names and scores are public; names are unverified and the demo
has no accounts or bot prevention. Structured logs record actual requests and service lifecycle events;
no synthetic background requests or errors are generated.

### Endpoints

| Path | What it does |
| --- | --- |
| `/` | Next.js matching game, feature hints and leaderboard |
| `/api/features` | Kupe capabilities and learning links |
| `POST /api/games` | Create a shuffled game with a private deck |
| `GET /api/games/{id}` | Resume an unexpired game |
| `POST /api/games/{id}/flip` | Reveal a card with `{ "index": 0 }` |
| `POST /api/games/{id}/score` | Save a completed game with `{ "name": "Player" }` |
| `/api/leaderboard` | Top ten results, ranked by moves then completion time |
| `/api/hello` | Service, tenant and pod information as JSON |
| `/api/work?ms=N` | burns roughly N milliseconds of CPU (default 50, capped at 1000) |
| `/healthz`, `/readyz` | probes |
| `/metrics` | Prometheus metrics, scraped automatically |

## Autoscaling

Enable `database.enabled=true` first: practice mode supports one replica.
Turn autoscaling on and the chart adds a `HorizontalPodAutoscaler` and stops managing
the replica count:

```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
resources:
  requests:
    cpu: 250m      # utilisation is measured against this
```

Nothing else needs installing. Each Kupe tenant cluster proxies the platform's
metrics-server, so `kubectl top pods` and a CPU-based HPA work out of the box.

To see it scale, drive `/api/work`, which is the only endpoint that costs
meaningful CPU. The rest are a JSON marshal each, so no achievable request rate
would move CPU utilisation:

```bash
# ~3 requests/sec per pod is enough to pass a 70% target at 250m
# Quote the URL: in zsh the `?` is a glob character and the command dies before it runs.
hey -z 5m -q 4 -c 20 "https://hello-kupe.<cluster>.<tenant>.clusters.kupe.cloud/api/work?ms=50"
kubectl get hpa hello-kupe -w
```

Two things to know when running it under Argo CD. The chart omits the
Deployment's `replicas` field entirely while autoscaling is enabled, because a
rendered `replicas: 1` plus `selfHeal: true` would revert every scale event
within seconds and quietly pin the app at one replica. And your plan's CPU pool
is the real ceiling: the HPA will stop adding replicas once the namespace
quota is reached, whatever `maxReplicas` says.

## Repo layout

* `web` - Next.js frontend, statically exported and served by Go
* `cmd/hello-kupe`, `internal/game` - the API and server-controlled game rules
* `internal/binding`, `internal/migrate` - database bindings and schema migrations
* `chart` - the Helm chart used by Argo CD and local installs
* `infra` - [Terraform/OpenTofu infrastructure](infra/README.md): cluster, managed
  PostgreSQL database and database bindings
* `samples/managed-database` - a Go API, migration Job and React frontend using
  a Kupe managed PostgreSQL database
* `samples/registry` - build this app's image, push it to your tenant's Kupe
  registry and run it on a connected cluster with no pull Secret

## Local development

For the complete app with PostgreSQL in local Kubernetes, run `make kind-dev`
and open `http://localhost:18080`. This uses an isolated kind cluster, two app
replicas and the same Helm chart and database-binding layout as Kupe.
See [local development](docs/local-development.md#kubernetes-with-kind) for
requirements, rebuilds and cleanup.

See [local development](docs/local-development.md) for practice mode, local
PostgreSQL, hot reload and verification. Quick start:

```bash
make dev
```

Open `http://localhost:3000`. Frontend dependencies are installed automatically;
Ctrl+C stops both servers. Run `make test`, `make web-test`, `make web-build`
and `make helm-lint` for the core checks. `make gosec` and `make govulncheck`
run the Go security checks.
