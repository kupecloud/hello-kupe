# Hello Kupe

`hello-kupe` is the official Kupe Cloud quickstart app.

<!-- toc -->

* [Overview](#overview)
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

The app serves a simple HTTP response, exposes health and metrics
endpoints, and emits
continuous background logs so the observability flow is visible immediately
after deploy.

## Repo layout

* `cmd/hello-kupe` - the app
* `chart/hello-kupe` - the Helm chart used by Argo CD and local
  installs

## Local chart install

```bash
helm upgrade --install hello-kupe ./chart/hello-kupe \
  --namespace hello-kupe \
  --create-namespace \
  --set tenant=<tenant>
```

By default the chart creates an `HTTPRoute` for:

`hello-kupe.<tenant>.kupe.cloud`

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
    path: chart/hello-kupe
    helm:
      releaseName: hello-kupe
      values: |
        tenant: <tenant>
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
