#!/usr/bin/env bash
# The old `Samples / registry sample chart render` job
# (kupecloud/github-workflows#14 moved it here): the chart renders a registry
# image reference and no imagePullSecrets, because a connected cluster needs
# none. Needs helm.
set -euo pipefail

rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT
helm template hello-kupe ./chart \
  --set tenant=acme --set cluster=prod \
  --set image.repository=registry.kupe.cloud/acme/hello-kupe \
  --set image.tag=1.0.0 > "$rendered"
if ! grep -q 'image: "registry.kupe.cloud/acme/hello-kupe:1.0.0"' "$rendered"; then
  echo "the chart does not render the registry image reference" >&2
  exit 1
fi
if grep -n imagePullSecrets "$rendered"; then
  echo "the chart renders imagePullSecrets; a connected cluster needs none" >&2
  exit 1
fi
echo "OK: registry image rendered without imagePullSecrets"
