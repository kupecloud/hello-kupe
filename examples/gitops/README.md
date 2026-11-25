# GitOps example

Run Hello Kupe on three clusters, `dev`, `staging` and `prod`, and move a
release from one to the next with a pull request. Managed Argo CD does the
deploying. Nobody runs `kubectl apply` or `helm upgrade` after the first setup.

```text
environments/dev.yaml      ── Application hello-kupe-dev      ── dev cluster
environments/staging.yaml  ── Application hello-kupe-staging  ── staging cluster
environments/prod.yaml     ── Application hello-kupe-prod     ── prod cluster
```

Each environment has one small file that says which version it runs. Each
cluster has one Argo CD `Application` that reads the chart at the root of this
repository together with that file. Changing a version in Git is the
deployment.

## Contents

* `environments/` - one Helm values file per environment, holding the image tag
* `applications/` - one Argo CD `Application` per cluster

## What you need

* A Kupe tenant with three clusters named `dev`, `staging` and `prod`. One
  cluster is enough to try the pattern: use only the `dev` files.
* `kubectl` connected to each cluster
  ([Cluster access](https://docs.kupe.cloud/platform/cluster-access/)).
* Your own copy of this repository, so that you can merge pull requests into it.
  Fork it, then change `repoURL` in the three files under `applications/` to
  your fork.

## Set it up

1. Open Argo CD from the console's app switcher, or at
   <https://argocd.kupe.cloud>, and go to **Settings**, **Clusters**. Copy the
   **NAME** of each cluster. It looks like `<tenant>--dev-<suffix>`. Do not
   build the name yourself: the suffix is different for every cluster.
2. In each file under `applications/`, replace `<tenant>` with your tenant name
   and the destination name with the one you copied.
3. Apply each `Application` to its own cluster:

   ```bash
   kubectl --kubeconfig ./dev-kupe.kubeconfig apply -f applications/dev.yaml
   kubectl --kubeconfig ./staging-kupe.kubeconfig apply -f applications/staging.yaml
   kubectl --kubeconfig ./prod-kupe.kubeconfig apply -f applications/prod.yaml
   ```

4. Watch the three applications turn **Synced** and **Healthy** in Argo CD, then
   open the game on each cluster:

   ```text
   https://hello-kupe.<cluster>.<tenant>.clusters.kupe.cloud
   ```

`kubectl get application -n argocd` shows no status on a Kupe cluster. Look in
Argo CD, or run `kubectl get deploy,pods -n hello-kupe`.

## Promote a release

Say `dev` runs `1.9.2`, and `staging` and `prod` still run `1.9.1`.

1. Change one line in `environments/staging.yaml`. The tag carries the image
   digest, so copy both from the release:

   ```diff
    image:
   -  tag: "1.9.1@sha256:853e6a7141b82430e93c8298a5fc31016165d97b028c07940afdfdd8fd4d3d7a"
   +  tag: "1.9.2@sha256:454c8109f69e1856cba8fb169f80f45b7b60aeb6aea0237dbbe52a11f3b60bed"
   ```

2. Open a pull request, let your checks run against `dev`, and merge it.
3. Argo CD sees the commit and syncs the `staging` cluster. Auto sync is on, so
   there is nothing else to do.
4. When `staging` looks right, make the same one-line change in
   `environments/prod.yaml`.

To roll back, revert the commit. Argo CD puts the previous version back.

## Getting a new version into dev

Argo CD deploys what is in Git. It does not watch your registry for new images.
When your pipeline builds a release, have its last step commit the new tag and
digest to `environments/dev.yaml`, or open a pull request that does. From there
the release moves forward the same way as every other change.

## Things to know

* **One `Application` per cluster.** `ApplicationSet` is not available on Kupe.
* **Self-heal is on.** A change made by hand in a cluster is put back to match
  Git. Make the change in Git.
* **Deleting an `Application` leaves the workload running.** Delete the
  `hello-kupe` namespace as well if you want the game gone.
* **The tag includes the digest.** A Kupe cluster only runs a
  `ghcr.io/kupecloud` image that is pinned by digest, so each environment file
  holds `<version>@sha256:<digest>`. The chart's default value in
  `chart/values.yaml` carries the newest release's. Pinning your own images the
  same way means a tag that is moved later cannot change what runs.
* **This runs the game in practice mode.** Scores are not kept. To add a
  database, see the [managed database example](../managed-database/) and the
  database values in the [root README](../../README.md#deploy-with-gitops).

## Clean up

```bash
kubectl --kubeconfig ./dev-kupe.kubeconfig delete -f applications/dev.yaml
kubectl --kubeconfig ./dev-kupe.kubeconfig delete namespace hello-kupe
```

Repeat for `staging` and `prod`.
