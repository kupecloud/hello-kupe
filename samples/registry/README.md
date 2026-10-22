# Registry sample

Build the hello-kupe image, push it to your tenant's Kupe registry, and run it
on a connected cluster with no pull Secret. The image is the one at the root of
this repository, and the chart is the root `chart/`.

```text
docker build ── push ──> registry.kupe.cloud/<tenant>/hello-kupe:1.0.0
                                   │
connected cluster: every pod pulls ┘  (no imagePullSecrets in the chart)
```

A cluster connection lets every pod in the cluster pull from the registry. The
pull credential never enters the cluster: Kupe adds it to each pod on the host
when the pod is created. See
[Connect a cluster](https://docs.kupe.cloud/registry/connect-a-cluster/).

## Contents

* `terraform/` - the registry, the cluster connection and a push access token
* `ci/github-actions.yaml` - a GitHub Actions workflow that builds and pushes on every push to `main`
* `ci/gitlab-ci.yml` - the same as a GitLab CI job

## What you need

* A Kupe tenant and an existing cluster in phase `Running`.
* The `kupe` CLI, signed in with `kupe auth login` as a tenant `admin`.
* Docker with BuildKit, `helm` and `kubectl` access to the cluster.
* For the Terraform route, OpenTofu or Terraform with a `kupecloud/kupe`
  provider release that has `kupe_registry`.

The examples use the tenant `acme` and the cluster `prod`. Use your own names.

## 1. Turn on the registry and connect the cluster

With the CLI:

```bash
kupe registry create
kupe registry connect prod
```

`kupe registry create` waits until the registry is running, and
`kupe registry connect` until the connection is ready.

Or with Terraform or OpenTofu, `terraform/main.tf` creates the registry, the
connection and a push access token for CI:

```bash
cd terraform
export KUPE_API_KEY=<api key>
tofu init
tofu apply -var tenant=acme -var cluster_name=prod
```

The registry is created with deletion protection on. [Clean up](#6-clean-up)
says how to turn it off before deleting it.

## 2. Build and push the image

From the repository root, sign Docker in and push:

```bash
kupe registry login
docker build --platform linux/amd64 -t registry.kupe.cloud/acme/hello-kupe:1.0.0 .
docker push registry.kupe.cloud/acme/hello-kupe:1.0.0
```

Kupe's nodes are `linux/amd64`, so `--platform linux/amd64` matters on an Apple
silicon Mac. The login lasts 12 hours.

Read the digest the registry stored:

```bash
DIGEST=$(kupe registry image get hello-kupe:1.0.0 -o json | jq -r .digest)
echo "$DIGEST"
```

## 3. Deploy it

```bash
helm upgrade --install hello-kupe ./chart \
  --namespace hello-kupe \
  --create-namespace \
  --set tenant=acme \
  --set cluster=prod \
  --set image.repository=registry.kupe.cloud/acme/hello-kupe \
  --set image.tag="1.0.0@$DIGEST"
```

The chart renders `repository:tag`, so a tag of the form `1.0.0@sha256:...`
runs exactly the build you pushed, even if someone pushes `1.0.0` again. The
chart sets no `imagePullSecrets`.

## 4. Check it

```bash
kubectl -n hello-kupe get pods
kubectl -n hello-kupe get pod -l app.kubernetes.io/name=hello-kupe \
  -o jsonpath='{.items[0].status.containerStatuses[0].imageID}{"\n"}'
curl "https://hello-kupe.prod.acme.clusters.kupe.cloud/api/hello"
```

The pod is `Running`, its image ID ends with the digest you read in step 2, and
the route answers with the tenant, pod and namespace that served it.

If the pod is in `ImagePullBackOff`, check the connection with
`kupe registry connection get prod`, and see
[Troubleshooting](https://docs.kupe.cloud/registry/troubleshooting/).

## 5. Push from CI

CI signs in with an access token instead of `kupe registry login`. Create a
push token:

```bash
kupe registry token create --name github-actions --access push -o json
```

Store the `username` and the `secret` in your CI system, then copy
`ci/github-actions.yaml` to `.github/workflows/` in your repository, or
`ci/gitlab-ci.yml` to `.gitlab-ci.yml`. Each file says which variables and
secrets it reads. The Terraform route already created a token: read it with
`tofu output ci_username` and `tofu output -raw ci_secret`.

[Access tokens and CI](https://docs.kupe.cloud/registry/access-tokens/)
explains token access, expiry and revocation.

## 6. Clean up

```bash
helm uninstall hello-kupe --namespace hello-kupe
kupe registry repo delete hello-kupe
kupe registry disconnect prod
```

`repo delete` removes every image and tag in the repository, and asks you to
type its name.

To delete the registry as well, with every image in it, turn deletion
protection off first:

```bash
kupe registry update --deletion-protection=false
kupe registry delete
```

With Terraform, set `deletion_protection = false` on `kupe_registry`, run
`tofu apply`, then `tofu destroy`.
