#!/bin/bash
# Render a PolicyGenerator policy directory the way the ArgoCD repo-server CMP does.
#
# Usage: scripts/pg-render.sh <policy-dir>
#   e.g. scripts/pg-render.sh policies/stable/cert-manager
#
# Running `kustomize build` on a policy directly does not work, for three reasons that are each
# invisible until you hit them:
#
#   1. Manifests carry ${POLICY_NAMESPACE}, ${REMEDIATION}, ${EVAL_COMPLIANT},
#      ${EVAL_NONCOMPLIANT} and ${CLUSTER_SET_SUFFIX} — placeholders the CMP substitutes at sync
#      time. PolicyGenerator validates evaluationInterval as a duration or "watch" and rejects the
#      literal token.
#   2. A policy that renders a shared chart uses `chartHome: ../../../../components/`, so the
#      policy has to be staged at its repo-relative path with components/ alongside it.
#   3. That nested build is configured by POLICY_GEN_* environment variables, not by the outer
#      --enable-* flags.
#
# Exit codes: 0 rendered, 1 render failed, 2 kustomize/PolicyGenerator not installed.
#
# Values match the e2e harness (tools/internal/resolver/pipeline.go) so a local render matches what
# CI validates. Override any of them by exporting the same name before calling.
#
# NOTE: scripts/render-policygen-charts.sh deliberately does NOT use this helper. It substitutes
# reversible sentinel durations because it has to restore the ${...} tokens into the released
# chart; real values here would be baked in permanently.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

POLICY_NAMESPACE="${POLICY_NAMESPACE:-policies-autoshift}"
REMEDIATION="${REMEDIATION:-enforce}"
EVAL_COMPLIANT="${EVAL_COMPLIANT:-watch}"
EVAL_NONCOMPLIANT="${EVAL_NONCOMPLIANT:-watch}"
CLUSTER_SET_SUFFIX="${CLUSTER_SET_SUFFIX:-}"

pg_render() {
    local dir="$1"
    local kbin plugin_home
    if [[ -x "$PROJECT_ROOT/.tools/kustomize" ]]; then
        kbin="$PROJECT_ROOT/.tools/kustomize"
        plugin_home="$PROJECT_ROOT/.tools/kustomize-plugin"
    elif command -v kustomize >/dev/null 2>&1; then
        kbin="kustomize"
        plugin_home="${KUSTOMIZE_PLUGIN_HOME:-}"
    else
        return 2
    fi

    local tmp rel rc
    tmp="$(mktemp -d)"
    rel="${dir#"$PROJECT_ROOT"/}"; rel="${rel#./}"; rel="${rel%/}"
    mkdir -p "$tmp/$(dirname "$rel")"
    cp -R "$PROJECT_ROOT/$rel" "$tmp/$rel"
    [[ -d "$PROJECT_ROOT/components" ]] && cp -R "$PROJECT_ROOT/components" "$tmp/components"

    # Anchored ${...} patterns only. Hub-template vars use $base with no braces, so they are safe.
    local f
    while IFS= read -r f; do
        sed -e "s|\${POLICY_NAMESPACE}|${POLICY_NAMESPACE}|g" \
            -e "s|\${REMEDIATION}|${REMEDIATION}|g" \
            -e "s|\${EVAL_COMPLIANT}|${EVAL_COMPLIANT}|g" \
            -e "s|\${EVAL_NONCOMPLIANT}|${EVAL_NONCOMPLIANT}|g" \
            -e "s|\${CLUSTER_SET_SUFFIX}|${CLUSTER_SET_SUFFIX}|g" \
            "$f" > "$f.sub" && mv "$f.sub" "$f"
    done < <(find "$tmp/$rel" -name '*.yaml')

    KUSTOMIZE_PLUGIN_HOME="$plugin_home" \
    POLICY_GEN_ENABLE_HELM=true POLICY_GEN_DISABLE_LOAD_RESTRICTORS=true \
        "$kbin" build --enable-alpha-plugins --enable-helm \
        --load-restrictor LoadRestrictionsNone "$tmp/$rel"
    rc=$?
    rm -rf "$tmp"
    return $rc
}

# Only run as a CLI when executed directly; sourcing exposes pg_render() to the generators.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    # Only when run directly — sourcing must not change the caller's shell options.
    set -uo pipefail
    if [[ $# -ne 1 || "$1" == "--help" || "$1" == "-h" ]]; then
        sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
        exit 1
    fi
    [[ -d "$1" ]] || { echo "error: not a directory: $1" >&2; exit 1; }
    pg_render "$1"
    rc=$?
    [[ $rc -eq 2 ]] && echo "error: kustomize/PolicyGenerator not found — run \`make install-policy-generator\`" >&2
    exit $rc
fi
