#!/bin/bash
# Update operator channels to the latest stable version-specific channels
# Usage: update-operator-channels.sh --pull-secret FILE [OPTIONS]
#
# This script auto-discovers operators from AutoShift values files,
# extracts the operator index image, and finds the latest version-specific
# channels for each operator.
#
# OPERATOR DETECTION REQUIREMENTS:
# This script dynamically discovers operators by scanning for '{operator}-subscription-name'
# entries in your values files. For each operator to be detected, you MUST define:
#
#   {operator}-subscription-name: 'package'   # OLM package name (REQUIRED for detection)
#   {operator}-channel: 'channel'             # Current operator channel
#
# The subscription-name label is the canonical key that links labels to OLM packages.
# Without it, the operator will NOT be discovered or updated by this script.
#
# Requirements:
#   - oc CLI (for oc image extract)
#   - jq (for JSON parsing)
#   - Pull secret for registry.redhat.io

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors (enabled if either stdout or stderr is a terminal)
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

# Temp file for operator mappings (portable alternative to associative arrays)
MAPPINGS_FILE=""

cleanup() {
    [[ -n "$MAPPINGS_FILE" ]] && rm -f "$MAPPINGS_FILE"
}
trap cleanup EXIT

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

# ============================================================================
# DYNAMIC OPERATOR MAPPINGS
# Uses subscription-name as the canonical key - no hardcoded mappings!
# Mappings stored in temp file for bash 3.x compatibility
# Format: label|package|policy_dir
# ============================================================================

# Build operator mappings dynamically from values files
build_operator_mappings() {
    MAPPINGS_FILE=$(mktemp)

    for values_file in "$PROJECT_ROOT"/autoshift/values.*.yaml; do
        [[ -f "$values_file" ]] || continue

        # Find all *-subscription-name: entries
        grep -oE '[a-z][-a-z0-9]*-subscription-name:[[:space:]]*[^[:space:]]+' "$values_file" 2>/dev/null | \
        while IFS= read -r line; do
            # Extract label and package
            local label package policy_dir=""
            label=$(echo "$line" | sed 's/-subscription-name:.*//')
            package=$(echo "$line" | sed 's/.*-subscription-name:[[:space:]]*//' | tr -d "'" | tr -d '"')

            [[ -z "$label" || -z "$package" ]] && continue

            # Find policy directory by searching for name: {package} in policies/*/values.yaml
            for policy_values in "$PROJECT_ROOT"/policies/*/values.yaml; do
                [[ -f "$policy_values" ]] || continue
                if grep -qE "^[[:space:]]+name:[[:space:]]*['\"]?${package}['\"]?" "$policy_values" 2>/dev/null; then
                    policy_dir=$(dirname "$policy_values")
                    break
                fi
            done

            # Store mapping: label|package|policy_dir
            echo "${label}|${package}|${policy_dir}" >> "$MAPPINGS_FILE"
        done
    done

    # Deduplicate
    if [[ -f "$MAPPINGS_FILE" ]]; then
        sort -u "$MAPPINGS_FILE" -o "$MAPPINGS_FILE"
    fi
}

# Get all discovered operator labels
get_discovered_labels() {
    [[ -f "$MAPPINGS_FILE" ]] || return
    cut -d'|' -f1 "$MAPPINGS_FILE" | sort -u | tr '\n' ' '
}

# Get package name for a label (dynamic lookup)
get_package_for_label() {
    local label="$1"
    [[ -f "$MAPPINGS_FILE" ]] || return
    grep "^${label}|" "$MAPPINGS_FILE" 2>/dev/null | head -1 | cut -d'|' -f2
}

# Get label for a package (dynamic lookup)
get_label_for_package() {
    local package="$1"
    [[ -f "$MAPPINGS_FILE" ]] || return
    grep "|${package}|" "$MAPPINGS_FILE" 2>/dev/null | head -1 | cut -d'|' -f1
}

# Get policy values.yaml file for a label (dynamic lookup)
get_policy_file_for_label() {
    local label="$1"
    [[ -f "$MAPPINGS_FILE" ]] || return
    local policy_dir
    policy_dir=$(grep "^${label}|" "$MAPPINGS_FILE" 2>/dev/null | head -1 | cut -d'|' -f3)
    if [[ -n "$policy_dir" ]]; then
        echo "$policy_dir/values.yaml"
    fi
}

# ============================================================================
# CATALOG FUNCTIONS
# ============================================================================

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

    success "Catalog extracted to $catalog_dir ($(echo $extracted_count | xargs) operators)"
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

    local channels=""

    # Method 1: catalog.json with olm.channel entries
    if [[ -f "$package_dir/catalog.json" ]]; then
        channels=$(jq -r 'select(.schema == "olm.channel") | .name' "$package_dir/catalog.json" 2>/dev/null)
    fi

    # Method 2: Standalone channel JSON files (stable-3.16.json, etc.)
    # These may contain newer channels not in catalog.json
    local standalone_channels
    standalone_channels=$(ls "$package_dir"/*.json 2>/dev/null | xargs -n1 basename 2>/dev/null | \
        grep -E '^(stable|fast|latest|release)-' | sed 's/\.json$//' || true)
    if [[ -n "$standalone_channels" ]]; then
        channels=$(printf '%s\n%s' "$channels" "$standalone_channels")
    fi

    # Method 3: channels.json file
    if [[ -f "$package_dir/channels.json" ]]; then
        local json_channels
        json_channels=$(sed 's/}{/}\n{/g' "$package_dir/channels.json" | jq -r '.name' 2>/dev/null)
        channels=$(printf '%s\n%s' "$channels" "$json_channels")
    fi

    # Method 4: channels/ subdirectory
    if [[ -d "$package_dir/channels" ]]; then
        local dir_channels
        dir_channels=$(ls "$package_dir/channels"/*.json 2>/dev/null | xargs -n1 basename | sed 's/\.json$//' || true)
        channels=$(printf '%s\n%s' "$channels" "$dir_channels")
    fi

    # Dedupe and return
    echo "$channels" | grep -v '^$' | sort -u
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

# Compare two channels and determine if the second is newer
# Returns 0 if channel2 > channel1, 1 otherwise
is_newer_channel() {
    local channel1="$1"
    local channel2="$2"

    # If they're the same, not newer
    [[ "$channel1" == "$channel2" ]] && return 1

    # Extract version numbers from channels like "stable-3.15" or "release-2.14"
    local ver1="" ver2=""

    # Try to extract version from channel name
    if [[ "$channel1" =~ -([0-9]+\.[0-9]+)$ ]]; then
        ver1="${BASH_REMATCH[1]}"
    fi
    if [[ "$channel2" =~ -([0-9]+\.[0-9]+)$ ]]; then
        ver2="${BASH_REMATCH[1]}"
    fi

    # If both have versions, compare them
    if [[ -n "$ver1" && -n "$ver2" ]]; then
        # Use sort -V to compare versions
        local newer
        newer=$(printf '%s\n%s\n' "$ver1" "$ver2" | sort -V | tail -1)
        if [[ "$newer" == "$ver2" && "$ver1" != "$ver2" ]]; then
            return 0  # channel2 is newer
        else
            return 1  # channel1 is newer or same
        fi
    fi

    # If only one has a version, can't reliably compare
    # If neither has version (e.g., "stable" vs "latest"), can't compare
    return 1
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

    if [[ -n "$file" ]] && [[ -f "$file" ]] && grep -qE "^[[:space:]]+channel:" "$file" 2>/dev/null; then
        if $DRY_RUN; then
            local current
            current=$(grep -E "^[[:space:]]+channel:" "$file" | head -1 | \
                      sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
            if [[ -n "$current" ]] && [[ "$current" != "$new_channel" ]]; then
                echo "  Would update $file: channel: $current -> $new_channel"
                updated=1
            fi
        else
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
    if [[ -n "$policy_file" ]] && [[ -f "$policy_file" ]]; then
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

    log "AutoShift Operator Channel Updater (Dynamic Discovery)"
    echo ""

    # Build operator mappings from values files (no hardcoding!)
    log "Building operator mappings from subscription-name entries..."
    build_operator_mappings

    local discovered_labels
    discovered_labels=$(get_discovered_labels)
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
        elif is_newer_channel "$current_channel" "$best_channel"; then
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
        else
            # best_channel is older or can't compare - current is newer or same, don't downgrade
            printf "%-35s %-20s %-20s %s\n" "$package" "$current_channel" "$best_channel" "${GREEN}up to date${NC} (catalog has older)"
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
