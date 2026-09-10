# Hello Kupe

`hello-kupe` is the official Kupe Cloud quickstart app.

<!-- toc -->

* [Deploy it](#deploy-it)
  * [Or install the chart directly](#or-install-the-chart-directly)
* [Overview](#overview)
  * [Endpoints](#endpoints)
* [Autoscaling](#autoscaling)
* [Repo layout](#repo-layout)
* [Local development](#local-development)

<!-- Regenerate with "pre-commit run -a markdown-toc" -->

<!-- tocstop -->

## Deploy it

The fastest route needs nothing installed locally and no clone. Apply one
`Application` into the `argocd` namespace of your Kupe cluster, and the platform
deploys this repo for you.

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

The app serves a simple HTTP response, exposes health and metrics
endpoints, and emits
continuous background logs so the observability flow is visible immediately
after deploy.

### Endpoints

| Path | What it does |
| --- | --- |
| `/` | HTML page naming the tenant, pod, and namespace serving it |
| `/api/hello` | the same as JSON |
| `/api/work?ms=N` | burns roughly N milliseconds of CPU (default 50, capped at 1000) |
| `/healthz`, `/readyz` | probes |
| `/metrics` | Prometheus metrics, scraped automatically |

## Autoscaling

Turn it on and the chart adds a `HorizontalPodAutoscaler` and stops managing
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

* `cmd/hello-kupe` - the app
* `chart` - the Helm chart used by Argo CD and local installs

## Local development

For local code checks, use `make test`, `make gosec`, `make govulncheck`,
and `make helm-lint`.
