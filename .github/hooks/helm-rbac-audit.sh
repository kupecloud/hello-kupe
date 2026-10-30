#!/usr/bin/env bash
# Usage: helm-rbac-audit.sh <chart-path> <release-name> [helm template args...]
#
# Fail when the rendered chart has a ClusterRole that grants access to core
# secrets (resources "secrets" or "*"). Same render and the same yq
# expression as the old helm-rbac-audit.yaml workflow
# (kupecloud/github-workflows#14 moved it here). Needs helm and yq.
set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <chart-path> <release-name> [helm template args...]" >&2
  exit 2
fi
chart="$1"
release="$2"
shift 2

rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT
helm template "$release" "$chart" "$@" > "$rendered"

# Exact resource match: `contains(["secrets"])` substring-matches
# externalsecrets/secretstores and never matches "*". yq's `==` glob-matches,
# so the wildcard needs a regex. No `|| true`: a yq error must fail the gate.
# shellcheck disable=SC2016
offenders=$(yq -N \
  'select(.kind == "ClusterRole")
  | select([.rules[]? | select(.resources != null) | .resources[]
      | select(. == "secrets" or test("^\*$"))] | length > 0)
  | .metadata.name' \
  "$rendered" | sort -u)

if [ -n "$offenders" ]; then
  echo "Rendered chart contains ClusterRoles with cluster-wide secrets access:" >&2
  echo "$offenders" >&2
  echo "" >&2
  echo "Cluster-wide secrets read is a blast-radius amplifier. Narrow the rule to" >&2
  echo "specific namespaces (a Role, not a ClusterRole) or to named resources." >&2
  exit 1
fi
echo "OK: no cluster-wide secrets access in rendered ClusterRoles"
