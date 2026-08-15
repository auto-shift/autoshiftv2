#!/bin/bash
# AutoShift Operator Policy Updater (PolicyGenerator)
# Re-renders every operator's bare OperatorPolicy manifest (manifests/operator.yaml) from the
# shared template, preserving each operator's own params. Use it to propagate a template-wide
# change (e.g. a new OperatorPolicy field) across all operators, then review with `git diff`.
#
# It regenerates ONLY the OperatorPolicy manifest — not the Namespace, placement, or
# policy-generator-config.yaml. Structural drift in an operator manifest is reset to the template
# (that is the point); real per-operator values (subscription/namespace/channel/source) are
# extracted from the existing manifest and preserved.
#
# Usage: ./scripts/update-operator-policies.sh [options]
# Example: ./scripts/update-operator-policies.sh                    # Regenerate all
# Example: ./scripts/update-operator-policies.sh --operator kiali   # Regenerate only kiali

set -e

# Colors for output
if [[ -t 1 ]] || [[ -t 2 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEMPLATE_FILE="$SCRIPT_DIR/templates/manifest-operator.yaml.template"
POLICIES_DIR="$REPO_ROOT/policies"

# Options
SPECIFIC_OPERATOR=""
VERBOSE=false

usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "Re-renders operator OperatorPolicy manifests (manifests/operator.yaml) from the template."
    echo "After running, use 'git diff' to review changes and 'git checkout' to discard."
    echo ""
    echo "Options:"
    echo "  --operator NAME        Only regenerate a specific operator (dir name, e.g., kiali)"
    echo "  --verbose              Show extracted params per operator"
    echo "  --help                 Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                           # Regenerate all"
    echo "  $0 --operator tempo          # Regenerate only tempo"
    echo "  $0 --verbose                 # Show extraction details"
    echo ""
    echo "After running:"
    echo "  git diff                     # Review what changed"
    echo "  git checkout -- policies/    # Discard all changes"
    echo "  git add -p                   # Selectively stage changes"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --operator)
            SPECIFIC_OPERATOR="$2"
            shift 2
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        --help)
            usage
            exit 0
            ;;
        *)
            echo -e "${RED}Error: Unknown option $1${NC}"
            usage
            exit 1
            ;;
    esac
done

# Helper functions
log_info() {
    echo -e "${BLUE}$1${NC}"
}

log_success() {
    echo -e "${GREEN}$1${NC}"
}

log_verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "   $1"
    fi
}

log_warning() {
    echo -e "${YELLOW}$1${NC}"
}

log_error() {
    echo -e "${RED}$1${NC}"
}

# Extract a `| default "<value>"` from the hub-template line whose label key ends in <suffix>.
# Uses @ as the sed delimiter so the go-template pipe (|) stays literal.
extract_default_for() {
    local file="$1" suffix="$2"
    sed -n "s@.*${suffix}\" | default \"\([^\"]*\)\".*@\1@p" "$file" | head -1
}

# Extract the 7 template params + namespace-scoped flag from an existing operator manifest.
# Emits: component_name|label_prefix|namespace|subscription_name|source|source_namespace|channel|scoped
extract_operator_params() {
    local file="$1"

    # component_name from `name: install-operator-<component>`
    local component_name
    component_name=$(sed -n 's@.*name: install-operator-\([a-zA-Z0-9-]*\).*@\1@p' "$file" | head -1)

    # label_prefix from `autoshift.io/<prefix>-channel` (may differ from component_name, e.g. virt)
    local label_prefix
    label_prefix=$(sed -n 's@.*autoshift.io/\(.*\)-channel".*@\1@p' "$file" | head -1)
    [[ -z "$label_prefix" ]] && label_prefix="$component_name"

    # namespace = the operatorGroup namespace (first `    namespace:` line, 4-space indent)
    local namespace
    namespace=$(grep -m1 '^    namespace:' "$file" | awk '{print $2}')

    # defaults baked into the hub templates
    local subscription_name source source_namespace channel
    subscription_name=$(extract_default_for "$file" "-subscription-name")
    source=$(extract_default_for "$file" "-source")
    source_namespace=$(extract_default_for "$file" "-source-namespace")
    channel=$(extract_default_for "$file" "-channel")

    # namespace-scoped if the operatorGroup carries targetNamespaces
    local scoped="false"
    grep -q '^    targetNamespaces:' "$file" && scoped="true"

    echo "${component_name}|${label_prefix}|${namespace}|${subscription_name}|${source}|${source_namespace}|${channel}|${scoped}"
}

# Re-render an operator manifest from the template with the extracted params.
regenerate_operator() {
    local out_file="$1" cn="$2" lp="$3" ns="$4" sub="$5" src="$6" srcns="$7" ch="$8" scoped="$9"

    log_verbose "component=$cn label_prefix=$lp namespace=$ns"
    log_verbose "subscription=$sub source=$src source-namespace=$srcns channel=$ch scoped=$scoped"

    # awk (not sed) so the template's Go vars ($base, etc.) and ${REMEDIATION} token pass through.
    # NB: `sub` is a reserved awk function name, so the subscription var is `subn`.
    awk -v cn="$cn" -v lp="$lp" -v ns="$ns" -v subn="$sub" -v src="$src" -v srcns="$srcns" -v ch="$ch" '{
        gsub(/\{\{COMPONENT_NAME\}\}/, cn)
        gsub(/\{\{LABEL_PREFIX\}\}/, lp)
        gsub(/\{\{NAMESPACE\}\}/, ns)
        gsub(/\{\{SUBSCRIPTION_NAME\}\}/, subn)
        gsub(/\{\{SOURCE_NAMESPACE\}\}/, srcns)
        gsub(/\{\{SOURCE\}\}/, src)
        gsub(/\{\{CHANNEL\}\}/, ch)
        print
    }' "$TEMPLATE_FILE" > "$out_file"

    # Re-inject targetNamespaces into the operatorGroup for namespace-scoped operators.
    if [[ "$scoped" == "true" ]]; then
        awk -v ns="$ns" '
            !done && /^    namespace: / {
                print; print "    targetNamespaces:"; print "      - " ns; done=1; next
            }
            { print }
        ' "$out_file" > "$out_file.tmp" && mv "$out_file.tmp" "$out_file"
    fi
}

# Render a PolicyGenerator dir the way the CMP/CI does (token-subst a temp copy, then kustomize+PG).
# Returns 0 ok, 1 render failure, 2 toolchain missing.
pg_render() {
    local dir="$1" kbin plugin_home
    if [[ -x "$REPO_ROOT/.tools/kustomize" ]]; then
        kbin="$REPO_ROOT/.tools/kustomize"; plugin_home="$REPO_ROOT/.tools/kustomize-plugin"
    elif command -v kustomize >/dev/null 2>&1; then
        kbin="kustomize"; plugin_home="${KUSTOMIZE_PLUGIN_HOME:-}"
    else
        return 2
    fi
    local tmp; tmp="$(mktemp -d)"
    cp -R "$dir"/. "$tmp"/
    local f
    while IFS= read -r f; do
        sed -e 's/\${POLICY_NAMESPACE}/policies-autoshift/g' \
            -e 's/\${REMEDIATION}/enforce/g' \
            -e 's/\${EVAL_COMPLIANT}/2h/g' \
            -e 's/\${EVAL_NONCOMPLIANT}/45s/g' \
            -e 's/\${CLUSTER_SET_SUFFIX}//g' \
            "$f" > "$f.sub" && mv "$f.sub" "$f"
    done < <(find "$tmp" -name '*.yaml')
    KUSTOMIZE_PLUGIN_HOME="$plugin_home" "$kbin" build \
        --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone "$tmp" >/dev/null 2>&1
    local rc=$?; rm -rf "$tmp"; return $rc
}

# Main function
main() {
    local total=0 regenerated=0 failed=0 render_warned=0

    log_info "Re-rendering operator manifests from $(basename "$TEMPLATE_FILE")..."
    echo ""

    # Find bare OperatorPolicy manifests anywhere under a policy's manifests/ dir.
    while IFS= read -r policy_file; do
        [[ -f "$policy_file" ]] || continue
        grep -q '^kind: OperatorPolicy' "$policy_file" 2>/dev/null || continue

        local policy_dir operator_dir
        policy_dir="${policy_file%%/manifests/*}"
        operator_dir=$(basename "$policy_dir")

        if [[ -n "$SPECIFIC_OPERATOR" && "$operator_dir" != "$SPECIFIC_OPERATOR" ]]; then
            continue
        fi

        total=$((total + 1))

        local info cn lp ns sub src srcns ch scoped
        info=$(extract_operator_params "$policy_file")
        IFS='|' read -r cn lp ns sub src srcns ch scoped <<< "$info"

        # Require the fields the template needs; skip (don't clobber) if extraction was incomplete.
        if [[ -z "$cn" || -z "$lp" || -z "$ns" || -z "$sub" || -z "$src" || -z "$srcns" || -z "$ch" ]]; then
            log_error "  $operator_dir - could not extract all params (non-standard manifest), skipping"
            log_verbose "extracted: cn=$cn lp=$lp ns=$ns sub=$sub src=$src srcns=$srcns ch=$ch"
            failed=$((failed + 1))
            continue
        fi

        regenerate_operator "$policy_file" "$cn" "$lp" "$ns" "$sub" "$src" "$srcns" "$ch" "$scoped"

        # Validate the dir still renders through PolicyGenerator.
        pg_render "$policy_dir"
        local rc=$?
        if [[ $rc -eq 0 ]]; then
            log_success "  $operator_dir"
        elif [[ $rc -eq 2 ]]; then
            log_success "  $operator_dir (render not validated — .tools/ missing)"
            render_warned=1
        else
            log_warning "  $operator_dir - regenerated but PolicyGenerator render FAILED (review git diff)"
        fi
        regenerated=$((regenerated + 1))
    done < <(find "$POLICIES_DIR" -path '*/manifests/*' -name '*.yaml' | sort)

    echo ""
    log_info "Summary: $regenerated/$total operator manifests re-rendered"
    if [[ $render_warned -eq 1 ]]; then
        log_warning "Some dirs were not render-validated. Install the toolchain: make install-policy-generator"
    fi
    if [[ $failed -gt 0 ]]; then
        log_error "Skipped (incomplete extraction): $failed — review these manually"
    fi

    echo ""
    echo "Next steps:"
    echo "  git diff                     # Review what changed"
    echo "  git checkout -- policies/    # Discard all changes"
    echo "  git add -p                   # Selectively stage changes"
}

# Run
main
