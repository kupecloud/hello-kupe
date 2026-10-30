#!/usr/bin/env bash
# Usage: helm-automount-audit.sh <chart-path> <release-name> [helm template args...]
#
# Report the rendered workloads that mount a ServiceAccount token (their pod
# spec does not set automountServiceAccountToken: false). Same render and the
# same yq expression as the old helm-automount-audit.yaml workflow
# (kupecloud/github-workflows#14 moved it here). Like that job it REPORTS and
# does not fail on what it finds; a render or yq error does fail it, so it
# never prints a false all-clear. Needs helm and yq.
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

# shellcheck disable=SC2016
automounts=$(yq -N \
  'select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "DaemonSet"
      or .kind == "Job"
      or .kind == "CronJob" or .kind == "Pod")
  | {"name": .metadata.name,
     "automount": (
        (select(.kind == "Pod") | .spec.automountServiceAccountToken),
        (select(.kind == "CronJob") | .spec.jobTemplate.spec.template.spec.automountServiceAccountToken),
        (select(.kind != "Pod" and .kind != "CronJob") | .spec.template.spec.automountServiceAccountToken)
     )}
  | select(.automount != false)
  | .name' \
  "$rendered" | sort -u)

if [ -z "$automounts" ]; then
  echo "All workloads explicitly disable automountServiceAccountToken."
else
  echo "The following workloads mount SA tokens (review whether each is intentional):"
  echo "$automounts"
fi
