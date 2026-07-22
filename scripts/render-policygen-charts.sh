#!/usr/bin/env bash
# Render each PolicyGenerator policy dir into a plain Helm chart at release time, so the OCI release is
# consumable with STOCK HELM (no policy-generator CMP sidecar) — backwards compatible with non-PG
# customers. The chart carries the PG-rendered manifests raw in files/ and substitutes the deploy-time
# ${...} tokens via `.Files.Get | replace` (NOT Helm templating — so the ACM {{hub}} templates in the
# manifests pass through untouched). Uses the same autoshift.policyValuesObject keys as the existing
# Helm charts, so it deploys identically.
#
# Usage: KUSTOMIZE=... KUSTOMIZE_PLUGIN_HOME=... CHARTS_DIR=... VERSION=... render-policygen-charts.sh <pg-dir>...
set -euo pipefail

KUSTOMIZE="${KUSTOMIZE:?KUSTOMIZE (path to kustomize binary) is required}"
export KUSTOMIZE_PLUGIN_HOME="${KUSTOMIZE_PLUGIN_HOME:?KUSTOMIZE_PLUGIN_HOME is required}"
HELM="${HELM:-helm}"
CHARTS_DIR="${CHARTS_DIR:-.helm-charts}"
VERSION="${VERSION:?VERSION is required}"

OUT="$CHARTS_DIR/policies"
mkdir -p "$OUT"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# ai-accelerator source for the openshift-ai / gpu-operator hybrid policies. These tokens live in a
# kustomize resource URL, so they must be substituted to concrete values BEFORE `kustomize build` fetches
# (unlike ${POLICY_NAMESPACE}/${REMEDIATION}/${CLUSTER_SET_SUFFIX}, which pass PG untouched for the shim).
AI_ACCELERATOR_REPO="${AI_ACCELERATOR_REPO:-github.com/redhat-ai-services/ai-accelerator}"
AI_ACCELERATOR_REF="${AI_ACCELERATOR_REF:-main}"
AI_ACCELERATOR_OVERLAY="${AI_ACCELERATOR_OVERLAY:-instance-3.x/base}"

# Absurd, valid, unique duration sentinels for the ONLY tokens PolicyGenerator validates
# (evaluationInterval). ${POLICY_NAMESPACE}/${REMEDIATION}/${CLUSTER_SET_SUFFIX} pass PG untouched;
# the ai-accelerator tokens are substituted to real values here (they steer the kustomize fetch).
sedtok() {
  sed -e 's|${EVAL_COMPLIANT}|1000000h|g' -e 's|${EVAL_NONCOMPLIANT}|2000000h|g' \
      -e "s|\${AI_ACCELERATOR_REPO}|${AI_ACCELERATOR_REPO}|g" \
      -e "s|\${AI_ACCELERATOR_REF}|${AI_ACCELERATOR_REF}|g" \
      -e "s|\${AI_ACCELERATOR_OVERLAY}|${AI_ACCELERATOR_OVERLAY}|g"
}
unsedtok() { sed -e 's|1000000h|${EVAL_COMPLIANT}|g' -e 's|2000000h|${EVAL_NONCOMPLIANT}|g'; }

for dir in "$@"; do
  name="$(basename "$dir")"
  src="$work/$name-src"
  cp -r "$dir" "$src"

  # 1. EVAL tokens -> sentinel durations so PG's duration validation passes.
  find "$src" -name '*.yaml' -type f | while read -r f; do
    sedtok < "$f" > "$f.tmp" && mv "$f.tmp" "$f"
  done

  # 2. render via PolicyGenerator.
  rendered="$work/$name.yaml"
  "$KUSTOMIZE" build --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone "$src" > "$rendered"

  # 3. sentinels -> EVAL tokens (safe: absurd/unique, no re-serialization -> byte-identical to CMP).
  unsedtok < "$rendered" > "$rendered.tmp" && mv "$rendered.tmp" "$rendered"

  # 4. scaffold the thin chart.
  chart="$work/$name-chart"
  mkdir -p "$chart/templates" "$chart/files"
  cp "$rendered" "$chart/files/rendered.yaml"
  cat > "$chart/Chart.yaml" <<EOF
apiVersion: v2
name: $name
version: $VERSION
description: AutoShift policy '$name' (rendered from PolicyGenerator at release time; deploys with stock Helm)
EOF
  cat > "$chart/values.yaml" <<'EOF'
# Consumed via the ApplicationSet's autoshift.policyValuesObject, same as the existing Helm charts.
policy_namespace: policies-autoshift
clusterSetSuffix: ""
autoshift:
  dryRun: false
  evaluationInterval:
    compliant: 10m
    noncompliant: 30s
EOF
  cat > "$chart/templates/policies.yaml" <<'EOF'
{{- .Files.Get "files/rendered.yaml"
    | replace "${POLICY_NAMESPACE}"   .Values.policy_namespace
    | replace "${REMEDIATION}"        (ternary "inform" "enforce" ((.Values.autoshift).dryRun | default false))
    | replace "${EVAL_COMPLIANT}"     (((.Values.autoshift).evaluationInterval).compliant | default "10m")
    | replace "${EVAL_NONCOMPLIANT}"  (((.Values.autoshift).evaluationInterval).noncompliant | default "30s")
    | replace "${CLUSTER_SET_SUFFIX}" (.Values.clusterSetSuffix | default "") }}
EOF

  # 5. package.
  "$HELM" package "$chart" -d "$OUT" >/dev/null
  echo "  - $name"
done
