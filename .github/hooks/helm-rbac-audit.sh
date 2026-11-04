#!/usr/bin/env bash
#
# helm-rbac-audit.sh <release-name> <chart-path>
#                    [--allow "<ClusterRole name> <reason>"]... [helm template args...]
#
# The RBAC secrets audit of kupecloud/github-workflows helm-rbac-audit.yaml as
# a pre-commit hook, so the PR gates job runs it (kupecloud/github-workflows#14,
# kupecloud/kupe-tests#65). Canonical copy: github-workflows
# scripts/hooks/helm-rbac-audit.sh. Chart repositories vendor it
# byte-identical; change it there, never in a copy.
#
# Fails when a rendered ClusterRole grants access to core secrets (exactly
# "secrets" or "*") and is not named in an --allow entry with a reason. An
# --allow entry that names no such ClusterRole fails too, so the list cannot
# go stale. Arguments after the options go to helm template.
#
# The Render and Audit blocks below are the workflow's `run:` scripts,
# verbatim; scripts/test_helm_rbac_audit.py fails if they drift. An empty
# render (a wrong chart path or values that switch everything off) fails
# rather than reading as clean. An empty --allow argument is refused.
#
# Requires: helm, yq (v4) on PATH.
set -euo pipefail

usage() {
  echo "usage: $0 <release-name> <chart-path> [--allow \"<ClusterRole name> <reason>\"]... [helm template args...]" >&2
  exit 2
}

[ "$#" -ge 2 ] || usage
RELEASE_NAME="$1"
CHART_PATH="$2"
shift 2
ALLOW=""
while [ "$#" -gt 0 ] && [ "$1" = "--allow" ]; do
  [ "$#" -ge 2 ] && [ -n "${2//[[:space:]]/}" ] || usage
  ALLOW+="$2"$'\n'
  shift 2
done
TEMPLATE_ARGS=""
for arg in "$@"; do
  TEMPLATE_ARGS+="$(printf '%q' "$arg") "
done

RUNNER_TEMP="$(mktemp -d)"
trap 'rm -rf "$RUNNER_TEMP"' EXIT
export RUNNER_TEMP RELEASE_NAME CHART_PATH ALLOW TEMPLATE_ARGS

# --- helm-rbac-audit.yaml "Render chart" step, verbatim ---
# args is assigned by the eval inside it.
# shellcheck disable=SC2154
(
set -euo pipefail
# template-args has always been shell words (quotes and $VARS
# included), so it is parsed into an argument list here exactly as
# it was when the input was pasted into the command line.
eval "args=($TEMPLATE_ARGS)"
# The +-guard keeps bash 3.2 (macOS, where the hook copy runs) from
# failing `set -u` on an empty array.
helm template "$RELEASE_NAME" "$CHART_PATH" ${args[@]+"${args[@]}"} \
  < /dev/null > "$RUNNER_TEMP/rendered.yaml"

)

# --- helm-rbac-audit.yaml "Audit cluster-wide secrets access" step, verbatim ---
(
set -euo pipefail
tab=$'\t'
bad=0

# An empty render (a wrong chart path, or values that switch
# everything off) fails rather than reading as clean.
if [ -z "$(yq -N 'select(.kind != null) | .kind' "$RUNNER_TEMP/rendered.yaml")" ]; then
  echo "::error::The chart rendered no objects; check the chart path and values files."
  exit 1
fi

# --- allow list: <ClusterRole name> <reason> ---
allowed="$RUNNER_TEMP/rbac-allow.tsv"
: > "$allowed"
while IFS= read -r line || [ -n "$line" ]; do
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  case "$line" in
    '' | '#'*) continue ;;
  esac
  name="" reason=""
  read -r name reason <<< "$line"
  if [ -z "$reason" ]; then
    echo "::error::Malformed allow entry '$line'. Expected '<ClusterRole name> <reason>'; the reason is required."
    bad=1
    continue
  fi
  if cut -f1 "$allowed" | grep -qxF -- "$name"; then
    echo "::error::Duplicate allow entry for ClusterRole $name."
    bad=1
    continue
  fi
  printf '%s\t%s\n' "$name" "$reason" >> "$allowed"
done <<< "$ALLOW"

# Detect ClusterRoles whose rules grant access to core secrets.
# Core Secrets live only in API group "", so a rule matches only
# when its apiGroups contain "" or "*"; a rule with no apiGroups
# counts as a match so a malformed rule fails closed. A rule on
# "*" resources in another group (kyverno.io/*) is not a match.
# Uses yq -N to suppress null documents and an inline select so
# the match is evaluated per-document — avoids the cross-document
# variable leak that the `$n` pattern is susceptible to.
# Match resources exactly: `contains(["secrets"])` substring-matches
# (false positives on externalsecrets/secretstores/...) and never
# matches the wildcard, so `resources: ["*"]` bypassed the gate.
# yq's `==` glob-matches strings, so the wildcard needs a regex.
# No `|| true`: this is a blocking security gate, so a yq error
# (parse failure on an odd rendered doc, an expression regression
# after a yq bump, a missing rendered.yaml) must FAIL the job,
# not swallow the error into an empty result that reads as "clean".
# `yq -N` already emits nothing with exit 0 when no document matches,
# so the empty case does not need `|| true`.
# shellcheck disable=SC2016
offenders=$(yq -N \
  'select(.kind == "ClusterRole")
  | select([.rules[]? | select(.resources != null)
      | select((.apiGroups // [""]) | any_c(. == "" or test("^\*$")))
      | .resources[] | select(. == "secrets" or test("^\*$"))] | length > 0)
  | .metadata.name' \
  "$RUNNER_TEMP/rendered.yaml" | sort -u)

# An allowed ClusterRole passes with its reason in the log; every
# other offender fails. An entry that names no offender is stale.
failed=""
while IFS= read -r name; do
  [ -n "$name" ] || continue
  reason="$(awk -F'\t' -v n="$name" '$1 == n { print $2; exit }' "$allowed")"
  if [ -n "$reason" ]; then
    echo "Allowed: ClusterRole $name reads secrets cluster-wide: $reason"
  else
    failed="$failed$name"$'\n'
  fi
done <<< "$offenders"
while IFS="$tab" read -r name reason; do
  if ! printf '%s\n' "$offenders" | grep -qxF -- "$name"; then
    echo "::error::Allow entry '$name' names no ClusterRole with cluster-wide secrets access in the render; remove it."
    bad=1
  fi
done < "$allowed"

if [ -n "$failed" ]; then
  echo "::error::Rendered chart contains ClusterRoles with cluster-wide secrets access:"
  printf '%s' "$failed"
  echo ""
  echo "Cluster-wide secrets read is a blast-radius amplifier. Prefer"
  echo "narrowing the rule to specific namespaces (a Role, not a"
  echo "ClusterRole) or to named resources. If the cluster-wide grant is"
  echo "required by design, name the ClusterRole in the allow list with"
  echo "the reason."
  exit 1
fi
if [ "$bad" -ne 0 ]; then
  exit 1
fi
echo "OK: no cluster-wide secrets access in rendered ClusterRoles, or only allowed ones"

)
