#!/usr/bin/env bash
#
# helm-limits-audit.sh <release-name> <chart-path> [--values "<file>..."]...
#                      [--allow "<entry>"]... [-- <helm template args>...]
#
# The resource requests and limits audit of kupecloud/github-workflows
# helm-limits-audit.yaml as a pre-commit hook, so the PR gates job runs it
# without a job of its own (kupecloud/kupe-tests#64). Canonical copy:
# github-workflows scripts/hooks/helm-limits-audit.sh. Chart repositories
# vendor it byte-identical; change it there, never in a copy.
#
#   --values  one render: whitespace-separated values files, layered in order
#             (the workflow's `values-files` line). Repeat for each render.
#             None renders the chart once with its defaults.
#   --allow   one exception, "<Kind>/<workload>/<container> <field> <reason>"
#             (the workflow's `allow` line). Repeatable.
#   --        everything after it goes to every helm template call
#             (the workflow's `template-args`).
#
# The Render and Audit blocks below are the workflow's `run:` scripts,
# verbatim; scripts/test_helm_limits_audit.py fails if they drift. A render
# with no container to audit fails, so a wrong chart path or a values file
# that switches everything off cannot pass as clean. An empty --values or
# --allow argument is refused.
#
# Requires: helm, yq (v4) on PATH.
set -euo pipefail

usage() {
  echo "usage: $0 <release-name> <chart-path> [--values \"<file>...\"]... [--allow \"<entry>\"]... [-- <helm template args>...]" >&2
  exit 2
}

[ "$#" -ge 2 ] || usage
RELEASE_NAME="$1"
CHART_PATH="$2"
shift 2
VALUES_FILES=""
ALLOW=""
TEMPLATE_ARGS=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --values)
      [ "$#" -ge 2 ] && [ -n "${2//[[:space:]]/}" ] || usage
      VALUES_FILES+="$2"$'\n'
      shift 2
      ;;
    --allow)
      [ "$#" -ge 2 ] && [ -n "${2//[[:space:]]/}" ] || usage
      ALLOW+="$2"$'\n'
      shift 2
      ;;
    --)
      shift
      for arg in "$@"; do
        TEMPLATE_ARGS+="$(printf '%q' "$arg") "
      done
      break
      ;;
    *) usage ;;
  esac
done

RUNNER_TEMP="$(mktemp -d)"
trap 'rm -rf "$RUNNER_TEMP"' EXIT
export RUNNER_TEMP RELEASE_NAME CHART_PATH VALUES_FILES ALLOW TEMPLATE_ARGS

# --- helm-limits-audit.yaml "Render chart" step, verbatim ---
# args is assigned by the eval inside it.
# shellcheck disable=SC2154
(
set -euo pipefail
out="$RUNNER_TEMP/limits-audit"
rm -rf "$out"
mkdir -p "$out"
# template-args is shell words, parsed the same way as in the other
# helm audit workflows.
eval "args=($TEMPLATE_ARGS)"
n=0

# render <label> [values file...]: one helm template into $out/<n>.yaml.
render() {
  local label="$1" f
  shift
  local vargs=()
  for f in "$@"; do
    vargs+=(-f "$f")
  done
  n=$((n + 1))
  printf '%s\n' "$label" > "$out/$n.label"
  echo "Rendering $n: $label"
  helm template "$RELEASE_NAME" "$CHART_PATH" \
    ${args[@]+"${args[@]}"} ${vargs[@]+"${vargs[@]}"} \
    < /dev/null > "$out/$n.yaml"
}

while IFS= read -r line || [ -n "$line" ]; do
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  case "$line" in
    '' | '#'*) continue ;;
  esac
  read -r -a files <<< "$line"
  render "$line" "${files[@]}"
done <<< "$VALUES_FILES"

if [ "$n" -eq 0 ]; then
  render "chart defaults and template-args"
fi

)

# --- helm-limits-audit.yaml "Audit resource requests and limits" step, verbatim ---
(
set -euo pipefail
out="$RUNNER_TEMP/limits-audit"
tab=$'\t'
kinds_re='^(Deployment|StatefulSet|DaemonSet|Job|CronJob)$'
fields_re='^(requests\.cpu|requests\.memory|limits\.cpu|limits\.memory|\*)$'
bad=0

# --- allow list: <Kind>/<workload>/<container> <field> <reason> ---
: > "$out/allow.tsv"
: > "$out/used.tsv"
while IFS= read -r line || [ -n "$line" ]; do
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  case "$line" in
    '' | '#'*) continue ;;
  esac
  target="" field="" reason=""
  read -r target field reason <<< "$line"
  if ! [[ "$target" =~ ^[^/]+/[^/]+/[^/]+$ ]] \
    || ! [[ "${target%%/*}" =~ $kinds_re ]] \
    || ! [[ "$field" =~ $fields_re ]] \
    || [ -z "$reason" ]; then
    echo "::error::Malformed allow entry '$line'. Expected '<Kind>/<workload>/<container> <field> <reason>': Kind is Deployment, StatefulSet, DaemonSet, Job or CronJob; field is requests.cpu, requests.memory, limits.cpu, limits.memory or *; the reason is required."
    bad=1
    continue
  fi
  if cut -f1,2 "$out/allow.tsv" | grep -qxF -- "$target$tab$field"; then
    echo "::error::Duplicate allow entry for $target $field."
    bad=1
    continue
  fi
  # A * entry and a field entry for the same container overlap: the
  # first one wins every match, so reject the pair rather than
  # report the shadowed entry as stale.
  if [ "$field" = "*" ]; then
    overlap="$(awk -F'\t' -v t="$target" '$1 == t { print $2; exit }' "$out/allow.tsv")"
    if [ -n "$overlap" ]; then
      echo "::error::Allow entry '$target *' overlaps '$target $overlap'; keep one."
      bad=1
      continue
    fi
  elif cut -f1,2 "$out/allow.tsv" | grep -qxF -- "$target$tab*"; then
    echo "::error::Allow entry '$target $field' overlaps '$target *'; keep one."
    bad=1
    continue
  fi
  printf '%s\t%s\t%s\n' "$target" "$field" "$reason" >> "$out/allow.tsv"
done <<< "$ALLOW"

# --- per render: one line per container, <target> <type> <missing> ---
# A field is missing when it is absent, null, empty or a zero
# quantity in any form ("0", "0m", "0Mi", ".0", "0e3", "+0"): none of
# those is an explicit ceiling.
# CronJob pod specs sit under .spec.jobTemplate; every other kind
# here keeps them at .spec.template.spec. initContainers include
# native sidecars (restartPolicy: Always), which run for the pod's
# whole life. No `|| true`: a yq error must fail the gate, never
# read as an empty, clean result.
# shellcheck disable=SC2016
expr='select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "DaemonSet"
      or .kind == "Job" or .kind == "CronJob")
  | .kind as $k | (.metadata.name // "") as $n
  | ((select($k == "CronJob") | .spec.jobTemplate.spec.template.spec),
     (select($k != "CronJob") | .spec.template.spec)) as $p
  | ((($p.initContainers // []) | .[] | {"t": "initContainer", "c": .}),
     (($p.containers // []) | .[] | {"t": "container", "c": .}))
  | .t as $t | (.c.name // "") as $cn
  | (.c.resources // {}) as $r | ($r.requests // {}) as $rq | ($r.limits // {}) as $l
  | [({"f": "requests.cpu", "v": ($rq.cpu // "")},
     {"f": "requests.memory", "v": ($rq.memory // "")},
     {"f": "limits.cpu", "v": ($l.cpu // "")},
     {"f": "limits.memory", "v": ($l.memory // "")})
     | select((.v | tostring) | test("^([+-]?(0+(\.0*)?|\.0+)([eE][+-]?[0-9]+|[a-zA-Z]*))?$")) | .f] as $miss
  | $k + "/" + $n + "/" + $cn + "\t" + $t + "\t" + ($miss | join(","))'

: > "$out/fail.tsv"
: > "$out/exempt.tsv"
: > "$out/empty.tsv"
renders=0
containers=0
i=1
while [ -f "$out/$i.yaml" ]; do
  label="$(cat "$out/$i.label")"
  yq -N "$expr" "$out/$i.yaml" > "$out/$i.containers"
  renders=$((renders + 1))
  # A render with nothing to audit (a wrong chart path, or values
  # that switch every workload off) fails rather than reading as
  # clean.
  if [ ! -s "$out/$i.containers" ]; then
    printf '%s\n' "$label" >> "$out/empty.tsv"
    echo "::error::Render '$label' has no Deployment, StatefulSet, DaemonSet, Job or CronJob container to audit; check the chart path and values files."
    bad=1
  fi
  while IFS="$tab" read -r target type missing; do
    containers=$((containers + 1))
    [ -n "$missing" ] || continue
    IFS=, read -r -a fields <<< "$missing"
    for field in "${fields[@]}"; do
      reason=""
      while IFS="$tab" read -r a_target a_field a_reason; do
        if [ "$a_target" = "$target" ] && { [ "$a_field" = "$field" ] || [ "$a_field" = "*" ]; }; then
          reason="$a_reason"
          printf '%s\t%s\n' "$a_target" "$a_field" >> "$out/used.tsv"
          break
        fi
      done < "$out/allow.tsv"
      if [ -n "$reason" ]; then
        printf '%s\t%s\t%s\t%s\t%s\n' "$label" "$target" "$type" "$field" "$reason" >> "$out/exempt.tsv"
      else
        printf '%s\t%s\t%s\t%s\n' "$label" "$target" "$type" "$field" >> "$out/fail.tsv"
        echo "::error::$target ($type) has no resources.$field (render: $label)"
      fi
    done
  done < "$out/$i.containers"
  i=$((i + 1))
done

if [ "$renders" -eq 0 ]; then
  echo "::error::No rendered chart found in $out; the render step did not run."
  exit 1
fi

: > "$out/stale.tsv"
while IFS="$tab" read -r a_target a_field a_reason; do
  if ! grep -qxF -- "$a_target$tab$a_field" "$out/used.tsv"; then
    printf '%s\t%s\t%s\n' "$a_target" "$a_field" "$a_reason" >> "$out/stale.tsv"
    echo "::error::Allow entry '$a_target $a_field' matches nothing in any render; remove it."
    bad=1
  fi
done < "$out/allow.tsv"

{
  echo "Audited $containers container(s) across $renders render(s)."
  if [ -s "$out/fail.tsv" ]; then
    echo ""
    echo "**Missing** (set the field, or add an \`allow\` entry with a reason):"
    echo ""
    echo "| Render | Workload/container | Type | Missing |"
    echo "| --- | --- | --- | --- |"
    while IFS="$tab" read -r label target type field; do
      echo "| \`$label\` | \`$target\` | $type | \`resources.$field\` |"
    done < "$out/fail.tsv"
  fi
  if [ -s "$out/exempt.tsv" ]; then
    echo ""
    echo "**Allowed exceptions:**"
    echo ""
    echo "| Render | Workload/container | Type | Field | Reason |"
    echo "| --- | --- | --- | --- | --- |"
    while IFS="$tab" read -r label target type field reason; do
      # A | in a reason would split the table row.
      reason="${reason//|/\\|}"
      echo "| \`$label\` | \`$target\` | $type | \`resources.$field\` | $reason |"
    done < "$out/exempt.tsv"
  fi
  if [ -s "$out/empty.tsv" ]; then
    echo ""
    echo "**Empty renders** (no container to audit):"
    echo ""
    while IFS= read -r label; do
      echo "- \`$label\`"
    done < "$out/empty.tsv"
  fi
  if [ -s "$out/stale.tsv" ]; then
    echo ""
    echo "**Stale allow entries** (match nothing; remove them):"
    echo ""
    while IFS="$tab" read -r a_target a_field a_reason; do
      echo "- \`$a_target $a_field\`"
    done < "$out/stale.tsv"
  fi
} > "$out/summary.md"
cat "$out/summary.md"

if [ -s "$out/fail.tsv" ] || [ "$bad" -ne 0 ]; then
  exit 1
fi
echo "OK: every container sets requests.cpu, requests.memory, limits.cpu and limits.memory, or is allowed"

)
