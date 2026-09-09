# Hello Kupe

`hello-kupe` is the official Kupe Cloud quickstart app.

<!-- toc -->

* [Overview](#overview)
  * [Endpoints](#endpoints)
* [Autoscaling](#autoscaling)
* [Repo layout](#repo-layout)
* [Local chart install](#local-chart-install)
* [Argo CD example](#argo-cd-example)

<!-- Regenerate with "pre-commit run -a markdown-toc" -->

<!-- tocstop -->

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
| `/` | HTML page naming the tenant, cluster and pod serving it |
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
hey -z 5m -q 4 -c 20 https://hello-kupe.<cluster>.<tenant>.clusters.kupe.cloud/api/work?ms=50
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

## Local chart install

```bash
helm upgrade --install hello-kupe ./chart \
  --namespace hello-kupe \
  --create-namespace \
  --set tenant=<tenant> \
  --set cluster=<cluster>
```

By default the chart creates an `HTTPRoute` for:

`hello-kupe.<cluster>.<tenant>.clusters.kupe.cloud`

If you want a different hostname, set:

```bash
--set httpRoute.hostname=my-app.example.com
```

This is useful when you want multiple demo deployments inside the same tenant.

For local code checks, use `make test`, `make gosec`, `make govulncheck`,
and `make helm-lint`.

## Argo CD example

```yaml
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
    name: <tenant>-<cluster-slug>
    namespace: hello-kupe
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```
