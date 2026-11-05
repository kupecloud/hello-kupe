#!/usr/bin/env bash
#
# report-reader-render.sh — render checks for the optional
# VulnerabilityReport reader (chart/templates/vulnerability-report-reader.yaml,
# hello-kupe#27). Fails if any of these stops holding:
#
#   1. The chart-default render has no reader Role or RoleBinding.
#   2. The prod render has exactly one Role and one RoleBinding, in the
#      release namespace, granting only get/list on
#      vulnerabilityreports.aquasecurity.github.io, bound only to
#      kupe-system/kupe-runner-onb-mission-observer.
#   3. Enabling it without a ServiceAccount name or namespace fails the render.
#
# Usage: .github/hooks/report-reader-render.sh   (from anywhere; needs helm, yq v4, jq)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
for bin in helm yq jq; do
  command -v "$bin" >/dev/null || { echo "missing required tool: $bin" >&2; exit 2; }
done

CHART="$ROOT/chart"
REL_NS=hello-kupe
PROD_VALUES=(-f "$ROOT/chart/values/mgmt/prod.yaml")
DEFAULT_VALUES=(--set tenant=example-tenant --set cluster=example)
NAME=vulnerability-report-reader

fail() { echo "FAIL: $*" >&2; exit 1; }

render() {
  helm template hello-kupe "$CHART" --namespace "$REL_NS" "$@" \
    | yq -o=json -I=0 'select(.kind == "Role" or .kind == "RoleBinding") | select(.metadata.name == "'"$NAME"'")' \
    | jq -s '.'
}

[ "$(render "${DEFAULT_VALUES[@]}" | jq 'length')" = 0 ] || fail "chart-default render has reader objects"

prod_json="$(render "${PROD_VALUES[@]}")"
[ "$(jq 'length' <<<"$prod_json")" = 2 ] || fail "prod render: want one Role and one RoleBinding"
jq -e --arg ns "$REL_NS" 'all(.[]; .metadata.namespace == $ns)' <<<"$prod_json" >/dev/null \
  || fail "reader objects must be in $REL_NS"
jq -e '[.[] | select(.kind == "Role") | .rules]
  == [[{"apiGroups": ["aquasecurity.github.io"], "resources": ["vulnerabilityreports"], "verbs": ["get", "list"]}]]' \
  <<<"$prod_json" >/dev/null || fail "Role must grant only get/list on vulnerabilityreports"
jq -e '[.[] | select(.kind == "RoleBinding") | {roleRef, subjects}]
  == [{"roleRef": {"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "vulnerability-report-reader"},
       "subjects": [{"kind": "ServiceAccount", "name": "kupe-runner-onb-mission-observer", "namespace": "kupe-system"}]}]' \
  <<<"$prod_json" >/dev/null || fail "RoleBinding must bind only the configured ServiceAccount"

for missing in name namespace; do
  if out="$(helm template hello-kupe "$CHART" --namespace "$REL_NS" "${PROD_VALUES[@]}" \
    --set "vulnerabilityReportReader.serviceAccount.$missing=" 2>&1)"; then
    fail "enabled without serviceAccount.$missing must fail the render"
  fi
  grep -q 'are required when enabled' <<<"$out" \
    || fail "unexpected render error without serviceAccount.$missing: $out"
done

echo "report reader render checks passed"
