#!/bin/bash
# Update operator channels to the latest stable version-specific channels
# Usage: update-operator-channels.sh --pull-secret FILE [OPTIONS]
#
# This script auto-discovers operators from AutoShift values files,
# extracts the operator index image, and finds the latest version-specific
# channels for each operator.
#
# Requirements:
#   - oc CLI (for oc image extract)
#   - jq (for JSON parsing)
#   - Pull secret for registry.redhat.io

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors (enabled if either stdout or stderr is a terminal)
# Using $'...' syntax so escape sequences are actual escape characters
if [[ -t 1 ]] || [[ -t 2 ]]; then
    RED=$'\033[0;31m'
    GREEN=$'\033[0;32m'
    YELLOW=$'\033[1;33m'
    BLUE=$'\033[0;34m'
    CYAN=$'\033[0;36m'
    NC=$'\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    CYAN=''
    NC=''
fi

log() { echo -e "${BLUE}[INFO]${NC} $1" >&2; }
success() { echo -e "${GREEN}[OK]${NC} $1" >&2; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1" >&2; }
error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }

# Defaults
CATALOG="registry.redhat.io/redhat/redhat-operator-index:v4.18"
CACHE_DIR="$PROJECT_ROOT/.cache/catalog-cache"
DRY_RUN=false
CHECK_ONLY=false
PULL_SECRET=""

usage() {
    cat << EOF
Usage: $0 --pull-secret FILE [OPTIONS]

Update operator channels to the latest stable version-specific channels.

This script auto-discovers operators from your AutoShift values files and
updates them to the latest channels from the Red Hat operator catalog.

Required:
  --pull-secret FILE  Path to pull secret JSON file for registry.redhat.io

Options:
  --catalog CATALOG   Operator catalog image (default: $CATALOG)
  --dry-run           Show what would be updated without making changes
  --check             Check for updates and exit with code 1 if updates available
  --no-cache          Force re-download of catalog
  -h, --help          Show this help message

Examples:
  $0 --pull-secret pull-secret.json              # Update all operator channels
  $0 --pull-secret pull-secret.json --dry-run   # Preview changes without applying
  $0 --pull-secret pull-secret.json --check     # Check if updates are available (for CI)

EOF
    exit 0
}

# Parse arguments
NO_CACHE=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --pull-secret)
            PULL_SECRET="$2"
            shift 2
            ;;
        --catalog)
            CATALOG="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --check)
            CHECK_ONLY=true
            DRY_RUN=true
            shift
            ;;
        --no-cache)
            NO_CACHE=true
            shift
            ;;
        -h|--help)
            usage
            ;;
        *)
            error "Unknown option: $1"
            usage
            ;;
    esac
done

# Validate required pull-secret
if [[ -z "$PULL_SECRET" ]]; then
    error "Missing required --pull-secret option"
    echo "" >&2
    usage
fi

if [[ ! -f "$PULL_SECRET" ]]; then
    error "Pull secret file not found: $PULL_SECRET"
    exit 1
fi

# Export for oc image extract
export REGISTRY_AUTH_FILE="$PULL_SECRET"

# Check requirements
check_requirements() {
    local missing=()
    command -v oc >/dev/null 2>&1 || missing+=("oc")
    command -v jq >/dev/null 2>&1 || missing+=("jq")

    if [[ ${#missing[@]} -gt 0 ]]; then
        error "Missing required tools: ${missing[*]}"
        exit 1
    fi
}

# Extract catalog if not cached
ensure_catalog() {
    local catalog_hash
    catalog_hash=$(echo "$CATALOG" | md5sum | cut -d' ' -f1)
    local catalog_dir="$CACHE_DIR/$catalog_hash"

    if [[ "$NO_CACHE" == "true" ]] && [[ -d "$catalog_dir" ]]; then
        log "Removing cached catalog..."
        rm -rf "$catalog_dir"
    fi

    if [[ -d "$catalog_dir/configs" ]]; then
        log "Using cached catalog from $catalog_dir"
        echo "$catalog_dir"
        return 0
    fi

    log "Extracting catalog $CATALOG..."
    mkdir -p "$catalog_dir"

    local extract_output
    extract_output=$(oc image extract "$CATALOG" --path /:"$catalog_dir" --confirm --filter-by-os=linux/amd64 2>&1)
    local extract_rc=$?

    if [[ $extract_rc -ne 0 ]]; then
        extract_output=$(oc image extract "$CATALOG" --path /:"$catalog_dir" --confirm 2>&1)
        extract_rc=$?
    fi

    if [[ $extract_rc -ne 0 ]]; then
        error "Failed to extract catalog"
        echo "$extract_output" >&2
        rm -rf "$catalog_dir"
        exit 1
    fi

    local extracted_count
    extracted_count=$(find "$catalog_dir/configs" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l)
    if [[ $extracted_count -eq 0 ]]; then
        error "Catalog extraction produced no operator packages"
        echo "Extract output: $extract_output" >&2
        rm -rf "$catalog_dir"
        exit 1
    fi

    success "Catalog extracted to $catalog_dir ($extracted_count operators)"
    echo "$catalog_dir"
}

# Get all channels for an operator package
get_channels() {
    local catalog_dir="$1"
    local package="$2"
    local package_dir="$catalog_dir/configs/$package"

    if [[ ! -d "$package_dir" ]]; then
        return 1
    fi

    if [[ -f "$package_dir/catalog.json" ]]; then
        jq -r 'select(.schema == "olm.channel") | .name' "$package_dir/catalog.json" 2>/dev/null | sort -u
    elif [[ -f "$package_dir/channels.json" ]]; then
        sed 's/}{/}\n{/g' "$package_dir/channels.json" | jq -r '.name' 2>/dev/null | sort -u
    else
        return 1
    fi
}

# Determine the best channel for an operator
# Uses heuristics based on channel naming patterns
get_best_channel() {
    local channels="$1"
    local package="$2"
    local best=""

    # Try version-specific patterns based on package name hints
    case "$package" in
        *gitops*)
            best=$(echo "$channels" | grep -E '^gitops-[0-9]+\.[0-9]+$' | sort -Vr | head -1)
            ;;
        *pipelines*)
            best=$(echo "$channels" | grep -E '^pipelines-[0-9]+\.[0-9]+$' | sort -Vr | head -1)
            ;;
        advanced-cluster-management)
            best=$(echo "$channels" | grep -E '^release-[0-9]+\.[0-9]+$' | sort -Vr | head -1)
            ;;
        rhdh)
            best=$(echo "$channels" | grep -E '^fast-[0-9]+\.[0-9]+$' | sort -Vr | head -1)
            ;;
    esac

    # If no specific pattern matched, try generic patterns
    if [[ -z "$best" ]]; then
        # Try stable-X.Y (most common for Red Hat operators)
        best=$(echo "$channels" | grep -E '^stable-[0-9]+\.[0-9]+$' | sort -Vr | head -1)
    fi

    if [[ -z "$best" ]]; then
        # Try generic stable
        best=$(echo "$channels" | grep -E '^stable$' | head -1)
    fi

    if [[ -z "$best" ]]; then
        # Fall back to latest
        best=$(echo "$channels" | grep -E '^latest$' | head -1)
    fi

    if [[ -z "$best" ]]; then
        # Last resort: first available channel
        best=$(echo "$channels" | head -1)
    fi

    echo "$best"
}

# ============================================================================
# AUTO-DISCOVERY: Find operators from values files
# ============================================================================

# Normalize label to canonical form (avoid duplicates like coo/cluster-observability)
normalize_label() {
    local label="$1"
    case "$label" in
        cluster-observability) echo "coo" ;;
        virt) echo "virtualization" ;;
        *) echo "$label" ;;
    esac
}

# Discover operators by scanning values files for *-channel: patterns
discover_operators() {
    local labels=""

    # Scan autoshift/values.*.yaml files
    for file in "$PROJECT_ROOT"/autoshift/values.*.yaml; do
        [[ -f "$file" ]] || continue
        # Find patterns like "gitops-channel:" and extract "gitops"
        local found
        found=$(grep -oE '^[[:space:]]*[a-z][-a-z0-9]*-channel:' "$file" 2>/dev/null | \
                sed 's/-channel:.*//' | tr -d ' ' | sort -u)
        labels="$labels $found"
    done

    # Scan policy values.yaml files for operators with channel: field
    for file in "$PROJECT_ROOT"/policies/*/values.yaml; do
        [[ -f "$file" ]] || continue
        # Check if file has a channel: field (indicates it's an operator)
        if grep -qE '^[[:space:]]+channel:' "$file" 2>/dev/null; then
            # Try to get the operator label from directory name
            local dir_name
            dir_name=$(basename "$(dirname "$file")")
            # Map common directory names to labels
            case "$dir_name" in
                openshift-gitops) labels="$labels gitops" ;;
                openshift-pipelines) labels="$labels pipelines" ;;
                advanced-cluster-management) labels="$labels acm" ;;
                advanced-cluster-security) labels="$labels acs" ;;
                openshift-data-foundation) labels="$labels odf" ;;
                developer-hub) labels="$labels dev-hub" ;;
                openshift-compliance-operator) labels="$labels compliance" ;;
                cluster-observabilty) labels="$labels coo" ;;  # Note: typo in dir name
                trusted-artifact-signer) labels="$labels tas" ;;
                openshift-virtualization) labels="$labels virtualization" ;;
                *) labels="$labels $dir_name" ;;
            esac
        fi
    done

    # Normalize labels and deduplicate
    local normalized=""
    for label in $labels; do
        normalized="$normalized $(normalize_label "$label")"
    done

    echo "$normalized" | tr ' ' '\n' | grep -v '^$' | sort -u | tr '\n' ' '
}

# Get package name for a label - tries multiple sources
get_package_for_label() {
    local label="$1"
    local package=""

    # First, try to find from policy values.yaml (most accurate)
    for file in "$PROJECT_ROOT"/policies/*/values.yaml; do
        [[ -f "$file" ]] || continue

        # Check if this policy dir matches the label
        local dir_name
        dir_name=$(basename "$(dirname "$file")")

        local matches=false
        case "$label" in
            gitops) [[ "$dir_name" == "openshift-gitops" ]] && matches=true ;;
            pipelines) [[ "$dir_name" == "openshift-pipelines" ]] && matches=true ;;
            acm) [[ "$dir_name" == "advanced-cluster-management" ]] && matches=true ;;
            acs) [[ "$dir_name" == "advanced-cluster-security" ]] && matches=true ;;
            odf) [[ "$dir_name" == "openshift-data-foundation" ]] && matches=true ;;
            dev-hub) [[ "$dir_name" == "developer-hub" ]] && matches=true ;;
            dev-spaces) [[ "$dir_name" == "dev-spaces" ]] && matches=true ;;
            compliance) [[ "$dir_name" == "openshift-compliance-operator" ]] && matches=true ;;
            coo) [[ "$dir_name" == "cluster-observabilty" ]] && matches=true ;;
            tas) [[ "$dir_name" == "trusted-artifact-signer" ]] && matches=true ;;
            virtualization) [[ "$dir_name" == "openshift-virtualization" ]] && matches=true ;;
            *) [[ "$dir_name" == "$label" ]] && matches=true ;;
        esac

        if $matches; then
            # Look for name: field in the values.yaml
            package=$(grep -E '^[[:space:]]+name:' "$file" 2>/dev/null | head -1 | sed 's/.*name:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
            if [[ -n "$package" ]]; then
                echo "$package"
                return 0
            fi
        fi
    done

    # Second, try to find subscription-name from autoshift values files
    for file in "$PROJECT_ROOT"/autoshift/values.*.yaml; do
        [[ -f "$file" ]] || continue
        package=$(grep -E "^[[:space:]]*${label}-subscription-name:" "$file" 2>/dev/null | head -1 | \
                  sed 's/.*subscription-name:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
        if [[ -n "$package" ]]; then
            echo "$package"
            return 0
        fi
    done

    # Fallback: use a known mapping for common operators
    case "$label" in
        gitops) echo "openshift-gitops-operator" ;;
        acm) echo "advanced-cluster-management" ;;
        metallb) echo "metallb-operator" ;;
        odf) echo "odf-operator" ;;
        acs) echo "rhacs-operator" ;;
        dev-spaces) echo "devspaces" ;;
        dev-hub) echo "rhdh" ;;
        pipelines) echo "openshift-pipelines-operator-rh" ;;
        tas) echo "rhtas-operator" ;;
        quay) echo "quay-operator" ;;
        loki) echo "loki-operator" ;;
        logging) echo "cluster-logging" ;;
        coo) echo "cluster-observability-operator" ;;
        compliance) echo "compliance-operator" ;;
        local-storage) echo "local-storage-operator" ;;
        lvm) echo "lvms-operator" ;;
        nmstate) echo "kubernetes-nmstate-operator" ;;
        virtualization) echo "kubevirt-hyperconverged" ;;
        *) echo "$label-operator" ;;  # Best guess
    esac
}

# Get label name for a package (reverse lookup)
get_label_for_package() {
    local package="$1"

    # Try to find from autoshift values files
    for file in "$PROJECT_ROOT"/autoshift/values.*.yaml; do
        [[ -f "$file" ]] || continue
        local label
        label=$(grep -B1 "subscription-name:[[:space:]]*['\"]\\?${package}['\"]\\?" "$file" 2>/dev/null | \
                grep -oE '^[[:space:]]*[a-z][-a-z0-9]*-subscription-name:' | \
                sed 's/-subscription-name:.*//' | tr -d ' ' | head -1)
        if [[ -n "$label" ]]; then
            echo "$label"
            return 0
        fi
    done

    # Fallback mapping
    case "$package" in
        openshift-gitops-operator) echo "gitops" ;;
        advanced-cluster-management) echo "acm" ;;
        metallb-operator) echo "metallb" ;;
        odf-operator) echo "odf" ;;
        rhacs-operator) echo "acs" ;;
        devspaces) echo "dev-spaces" ;;
        rhdh) echo "dev-hub" ;;
        openshift-pipelines-operator-rh) echo "pipelines" ;;
        rhtas-operator) echo "tas" ;;
        quay-operator) echo "quay" ;;
        loki-operator) echo "loki" ;;
        cluster-logging) echo "logging" ;;
        cluster-observability-operator) echo "coo" ;;
        compliance-operator) echo "compliance" ;;
        local-storage-operator) echo "local-storage" ;;
        lvms-operator) echo "lvm" ;;
        kubernetes-nmstate-operator) echo "nmstate" ;;
        kubevirt-hyperconverged) echo "virtualization" ;;
        *) echo "${package%-operator}" ;;  # Strip -operator suffix
    esac
}

# Get policy values.yaml file for a label
get_policy_file_for_label() {
    local label="$1"

    case "$label" in
        gitops) echo "$PROJECT_ROOT/policies/openshift-gitops/values.yaml" ;;
        pipelines) echo "$PROJECT_ROOT/policies/openshift-pipelines/values.yaml" ;;
        acm) echo "$PROJECT_ROOT/policies/advanced-cluster-management/values.yaml" ;;
        acs) echo "$PROJECT_ROOT/policies/advanced-cluster-security/values.yaml" ;;
        odf) echo "$PROJECT_ROOT/policies/openshift-data-foundation/values.yaml" ;;
        dev-hub) echo "$PROJECT_ROOT/policies/developer-hub/values.yaml" ;;
        dev-spaces) echo "$PROJECT_ROOT/policies/dev-spaces/values.yaml" ;;
        compliance) echo "$PROJECT_ROOT/policies/openshift-compliance-operator/values.yaml" ;;
        coo) echo "$PROJECT_ROOT/policies/cluster-observabilty/values.yaml" ;;
        tas) echo "$PROJECT_ROOT/policies/trusted-artifact-signer/values.yaml" ;;
        virtualization) echo "$PROJECT_ROOT/policies/openshift-virtualization/values.yaml" ;;
        loki) echo "$PROJECT_ROOT/policies/loki/values.yaml" ;;
        logging) echo "$PROJECT_ROOT/policies/logging/values.yaml" ;;
        quay) echo "$PROJECT_ROOT/policies/quay/values.yaml" ;;
        metallb) echo "$PROJECT_ROOT/policies/metallb/values.yaml" ;;
        nmstate) echo "$PROJECT_ROOT/policies/nmstate/values.yaml" ;;
        lvm) echo "$PROJECT_ROOT/policies/lvm/values.yaml" ;;
        local-storage) echo "$PROJECT_ROOT/policies/local-storage/values.yaml" ;;
        *) echo "$PROJECT_ROOT/policies/$label/values.yaml" ;;
    esac
}

# ============================================================================
# UPDATE FUNCTIONS
# ============================================================================

# Update a channel in autoshift values files
update_autoshift_channel() {
    local label="$1"
    local new_channel="$2"
    local updated=0

    for file in "$PROJECT_ROOT"/autoshift/values.*.yaml; do
        [[ -f "$file" ]] || continue

        if grep -q "${label}-channel:" "$file" 2>/dev/null; then
            if $DRY_RUN; then
                local current
                current=$(grep -E "^[[:space:]]*${label}-channel:" "$file" | head -1 | \
                          sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
                if [[ -n "$current" ]] && [[ "$current" != "$new_channel" ]]; then
                    echo "  Would update $file: ${label}-channel: $current -> $new_channel"
                    updated=1
                fi
            else
                # Ensure exactly one space after colon for proper YAML formatting
                # The .* matches the old value to replace it entirely
                sed -i.bak "s/\(${label}-channel:\)[[:space:]]*.*/\1 ${new_channel}/" "$file"
                rm -f "$file.bak"
                updated=1
            fi
        fi
    done

    return $updated
}

# Update policy helm chart values.yaml
update_policy_channel() {
    local label="$1"
    local new_channel="$2"
    local updated=0

    local file
    file=$(get_policy_file_for_label "$label")

    if [[ -f "$file" ]] && grep -qE "^[[:space:]]+channel:" "$file" 2>/dev/null; then
        if $DRY_RUN; then
            local current
            current=$(grep -E "^[[:space:]]+channel:" "$file" | head -1 | \
                      sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
            if [[ -n "$current" ]] && [[ "$current" != "$new_channel" ]]; then
                echo "  Would update $file: channel: $current -> $new_channel"
                updated=1
            fi
        else
            # Ensure exactly one space after colon for proper YAML formatting
            # The .* matches the old value to replace it entirely
            sed -i.bak "s/^\([[:space:]]*channel:\)[[:space:]]*.*/\1 ${new_channel}/" "$file"
            rm -f "$file.bak"
            updated=1
        fi
    fi

    return $updated
}

# Get current channel for a label
get_current_channel() {
    local label="$1"
    local current=""

    # Check autoshift values files first
    for file in "$PROJECT_ROOT"/autoshift/values.*.yaml; do
        [[ -f "$file" ]] || continue
        current=$(grep -E "^[[:space:]]*${label}-channel:" "$file" 2>/dev/null | head -1 | \
                  sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
        if [[ -n "$current" ]]; then
            echo "$current"
            return 0
        fi
    done

    # Check policy values file
    local policy_file
    policy_file=$(get_policy_file_for_label "$label")
    if [[ -f "$policy_file" ]]; then
        current=$(grep -E "^[[:space:]]+channel:" "$policy_file" 2>/dev/null | head -1 | \
                  sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
        if [[ -n "$current" ]]; then
            echo "$current"
            return 0
        fi
    fi

    echo ""
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    check_requirements

    log "AutoShift Operator Channel Updater (Auto-Discovery)"
    echo ""

    # Discover operators from values files
    log "Discovering operators from values files..."
    local discovered_labels
    discovered_labels=$(discover_operators)
    local label_count
    label_count=$(echo "$discovered_labels" | wc -w | tr -d ' ')
    success "Found $label_count operators: $discovered_labels"
    echo ""

    # Extract or use cached catalog
    local catalog_dir
    catalog_dir=$(ensure_catalog)
    echo ""

    # Track updates
    local updates_available=0
    local updates_made=0

    log "Checking operator channels..."
    echo ""

    printf "%-35s %-20s %-20s %s\n" "OPERATOR" "CURRENT" "LATEST" "STATUS"
    printf "%s\n" "$(printf '=%.0s' {1..90})"

    for label in $discovered_labels; do
        # Get package name for this label
        local package
        package=$(get_package_for_label "$label")

        if [[ -z "$package" ]]; then
            printf "%-35s %-20s %-20s %s\n" "$label" "-" "-" "${YELLOW}no package mapping${NC}"
            continue
        fi

        # Get channels for this package from catalog
        local channels
        channels=$(get_channels "$catalog_dir" "$package")
        if [[ -z "$channels" ]]; then
            printf "%-35s %-20s %-20s %s\n" "$package" "-" "-" "${YELLOW}not in catalog${NC}"
            continue
        fi

        # Get the best channel
        local best_channel
        best_channel=$(get_best_channel "$channels" "$package")
        if [[ -z "$best_channel" ]]; then
            printf "%-35s %-20s %-20s %s\n" "$package" "-" "-" "${YELLOW}no suitable channel${NC}"
            continue
        fi

        # Get current channel
        local current_channel
        current_channel=$(get_current_channel "$label")
        [[ -z "$current_channel" ]] && current_channel="-"

        # Compare and update
        if [[ "$current_channel" == "$best_channel" ]]; then
            printf "%-35s %-20s %-20s %s\n" "$package" "$current_channel" "$best_channel" "${GREEN}up to date${NC}"
        elif [[ "$current_channel" == "-" ]]; then
            printf "%-35s %-20s %-20s %s\n" "$package" "$current_channel" "$best_channel" "${CYAN}not configured${NC}"
        else
            printf "%-35s %-20s %-20s %s\n" "$package" "$current_channel" "$best_channel" "${YELLOW}update available${NC}"
            updates_available=1

            if ! $CHECK_ONLY; then
                # || true prevents set -e from exiting (return 1 means "updated")
                update_autoshift_channel "$label" "$best_channel" || true
                update_policy_channel "$label" "$best_channel" || true
                if ! $DRY_RUN; then
                    ((updates_made++)) || true
                fi
            fi
        fi
    done

    echo ""

    if $CHECK_ONLY; then
        if [[ $updates_available -eq 1 ]]; then
            warn "Updates are available. Run without --check to apply."
            exit 1
        else
            success "All operator channels are up to date."
            exit 0
        fi
    elif $DRY_RUN; then
        if [[ $updates_available -eq 1 ]]; then
            echo ""
            warn "Dry run mode - no changes made. Run without --dry-run to apply updates."
        else
            success "All operator channels are up to date."
        fi
    else
        if [[ $updates_made -gt 0 ]]; then
            success "Updated $updates_made operator channel(s)."
        else
            success "All operator channels are up to date."
        fi
    fi
}

main "$@"
