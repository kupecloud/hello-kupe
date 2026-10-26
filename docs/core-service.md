# Public core game

The always-on game uses `chart/values/mgmt/prod.yaml`: a single practice-mode
replica on the management cluster's core pool, at https://hello.kupe.cloud.
It has no database or tenant-node dependency. Scores are temporary.
The tenant deployment used for the website screenshots is separate.

## Publish an image without making a release

Run **Publish Image** manually against the game review branch with a unique
`demo-YYYYMMDD` tag. The publisher explicitly uses GitHub-hosted `ubuntu-latest`
runners and the existing cosign signing workflow. It does not tag a Git release.
The GHCR package must be public so the demo needs no pull credentials.

Pin the resulting `tag@sha256:...` in the management overlay before deploying.
Do not use a laptop-pushed unsigned image: production verifies the GitHub
Actions signing identity.

## Deployment

The ArgoCD repo owns this Application through `coreServices.charts`, scoped
only to `mgmt` destinations. Its management gateway values also allow the
`hello-kupe` namespace. Run the ArgoCD repo's normal production upgrade after
reviewing its local changes. The game chart itself creates no cluster, registry,
database, or access binding.

The chart permits only `/`, `/api`, `/_next/static`, and `/kupe-mark.svg` on its
public route. Health, readiness and metrics endpoints stay internal.
The homepage loads the real game in an iframe when its demo tab opens.
