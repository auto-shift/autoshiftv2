#!/bin/bash
# AutoShift Operator Policy Updater
# Regenerates operator policies from template, then use git diff to review changes
#
# Usage: ./scripts/update-operator-policies.sh [options]
# Example: ./scripts/update-operator-policies.sh                    # Regenerate all policies
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
TEMPLATE_FILE="$SCRIPT_DIR/templates/policy-operator-install.yaml.template"
POLICIES_DIR="$REPO_ROOT/policies"

# Options
SPECIFIC_OPERATOR=""
VERBOSE=false

usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "Regenerates operator installation policies from the template."
    echo "After running, use 'git diff' to review changes and 'git checkout' to discard."
    echo ""
    echo "Options:"
    echo "  --operator NAME        Only regenerate a specific operator (e.g., kiali)"
    echo "  --verbose              Show detailed output"
    echo "  --help                 Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                           # Regenerate all policies"
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

log_error() {
    echo -e "${RED}$1${NC}"
}

# Extract component info from an existing policy file
extract_component_info() {
    local policy_file="$1"
    local component_name=""
    local component_camel=""
    local label_prefix=""
    local filename=""

    # Extract from filename
    filename=$(basename "$policy_file")
    component_name=$(echo "$filename" | sed 's/policy-//' | sed 's/-operator-install\.yaml//')

    # Extract camelCase from .Values.CAMEL.namespace pattern
    component_camel=$(grep -oE '\.Values\.[a-zA-Z0-9]+\.namespace' "$policy_file" 2>/dev/null | \
                      head -1 | \
                      sed 's/\.Values\.//' | \
                      sed 's/\.namespace//')

    # If still no camelCase, derive from component name
    if [[ -z "$component_camel" ]]; then
        component_camel=$(echo "$component_name" | perl -pe 's/-([a-z])/\u$1/g')
    fi

    # Extract label prefix from autoshift.io/XXX-channel pattern
    # This captures the actual label prefix used (e.g., "virt" not "virtualization")
    label_prefix=$(grep -oE 'autoshift\.io/[a-zA-Z0-9-]+-channel' "$policy_file" 2>/dev/null | \
                   head -1 | \
                   sed 's/autoshift\.io\///' | \
                   sed 's/-channel//')

    # If no label prefix found, fall back to component name
    if [[ -z "$label_prefix" ]]; then
        label_prefix="$component_name"
    fi

    echo "$component_name|$component_camel|$label_prefix"
}

# Generate policy from template
regenerate_policy() {
    local policy_file="$1"
    local component_name="$2"
    local component_camel="$3"
    local label_prefix="$4"

    log_verbose "component_name: $component_name"
    log_verbose "component_camel: $component_camel"
    log_verbose "label_prefix: $label_prefix"

    sed -e "s/{{COMPONENT_NAME}}/$component_name/g" \
        -e "s/{{COMPONENT_CAMEL}}/$component_camel/g" \
        -e "s/{{LABEL_PREFIX}}/$label_prefix/g" \
        "$TEMPLATE_FILE" > "$policy_file"
}

# Main function
main() {
    local total=0
    local regenerated=0
    local failed=0

    log_info "Regenerating operator policies from template..."
    echo ""

    # Find all operator install policies
    while IFS= read -r -d '' policy_file; do
        local operator_dir=""
        operator_dir=$(basename "$(dirname "$(dirname "$policy_file")")")

        # Skip if specific operator requested and this isn't it
        if [[ -n "$SPECIFIC_OPERATOR" && "$operator_dir" != "$SPECIFIC_OPERATOR" ]]; then
            continue
        fi

        ((total++))

        local info=""
        local component_name=""
        local component_camel=""
        local label_prefix=""
        info=$(extract_component_info "$policy_file")
        component_name=$(echo "$info" | cut -d'|' -f1)
        component_camel=$(echo "$info" | cut -d'|' -f2)
        label_prefix=$(echo "$info" | cut -d'|' -f3)

        if [[ -n "$component_name" && -n "$component_camel" && -n "$label_prefix" ]]; then
            regenerate_policy "$policy_file" "$component_name" "$component_camel" "$label_prefix"
            log_success "  $operator_dir"
            ((regenerated++))
        else
            log_error "  $operator_dir - could not extract component info"
            ((failed++))
        fi

    done < <(find "$POLICIES_DIR" -name "policy-*-operator-install.yaml" -print0 2>/dev/null)

    # Summary
    echo ""
    log_info "Summary: $regenerated/$total policies regenerated"

    if [[ $failed -gt 0 ]]; then
        log_error "Errors: $failed"
        exit 1
    fi

    echo ""
    echo "Next steps:"
    echo "  git diff                     # Review what changed"
    echo "  git checkout -- policies/    # Discard all changes"
    echo "  git add -p                   # Selectively stage changes"
}

# Run
main
