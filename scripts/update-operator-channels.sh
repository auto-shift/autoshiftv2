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
CATALOG=""
CATALOG_OVERRIDE=false
CACHE_DIR="$PROJECT_ROOT/.cache/catalog-cache"
DRY_RUN=false
CHECK_ONLY=false
PULL_SECRET=""
CATALOG_VERSION=""
VALUES_FILES=()  # User-specified values files (--values-file)
UPDATE_ALL=false  # Update channels in all values files, not just scanned ones

# Temp file for operator mappings (portable alternative to associative arrays)
MAPPINGS_FILE=""
# Temp file for catalog dir lookups (source -> catalog_dir)
CATALOG_DIRS_FILE=""

cleanup() {
    [[ -n "$MAPPINGS_FILE" ]] && rm -f "$MAPPINGS_FILE"
    [[ -n "$CATALOG_DIRS_FILE" ]] && rm -f "$CATALOG_DIRS_FILE"
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
  --values-file FILE  Only scan this values file (can be repeated for multiple files)
  --update-all        Update channels in all values files, not just scanned ones
  --catalog CATALOG   Operator catalog image (overrides auto-detection from openshift-version)
  --dry-run           Show what would be updated without making changes
  --check             Check for updates and exit with code 1 if updates available
  --no-cache          Force re-download of catalog
  -h, --help          Show this help message

The catalog version is auto-detected from the 'openshift-version' label in your
values files (e.g., openshift-version: '4.20.12' -> v4.20 catalog). Use --catalog
to override this.

Examples:
  $0 --pull-secret pull-secret.json              # Update all (auto-detects catalog version)
  $0 --pull-secret pull-secret.json --values-file autoshift/values/clustersets/hub.yaml
  $0 --pull-secret pull-secret.json --values-file autoshift/values/clustersets/hub.yaml --update-all
  $0 --pull-secret pull-secret.json --dry-run   # Preview changes without applying
  $0 --pull-secret pull-secret.json --check     # Check if updates are available (for CI)
  $0 --pull-secret pull-secret.json --catalog registry.redhat.io/redhat/redhat-operator-index:v4.18

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
            CATALOG_OVERRIDE=true
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
        --values-file)
            VALUES_FILES+=("$2")
            shift 2
            ;;
        --update-all)
            UPDATE_ALL=true
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

# Validate and resolve --values-file paths
for i in "${!VALUES_FILES[@]}"; do
    vf="${VALUES_FILES[$i]}"
    # Resolve relative paths against PROJECT_ROOT
    if [[ "$vf" != /* ]]; then
        vf="$PROJECT_ROOT/$vf"
    fi
    if [[ ! -f "$vf" ]]; then
        error "Values file not found: ${VALUES_FILES[$i]}"
        exit 1
    fi
    VALUES_FILES[$i]="$vf"
done

# Helper: returns the list of values files to scan
# Uses --values-file entries if specified, otherwise discovers all clustersets
get_values_files() {
    if [[ ${#VALUES_FILES[@]} -gt 0 ]]; then
        printf '%s\n' "${VALUES_FILES[@]}"
    else
        for f in "$PROJECT_ROOT"/autoshift/values/clustersets/*.yaml; do
            [[ -f "$f" ]] || continue
            [[ "$(basename "$f")" == _* ]] && continue
            echo "$f"
        done
    fi
}

# Helper: returns the list of values files to write updates to
# With --update-all, writes to all clustersets + example files regardless of --values-file
get_update_files() {
    if [[ "$UPDATE_ALL" == "true" ]]; then
        for f in "$PROJECT_ROOT"/autoshift/values/clustersets/*.yaml "$PROJECT_ROOT"/autoshift/values/clusters/_example*.yaml; do
            [[ -f "$f" ]] || continue
            echo "$f"
        done
    else
        get_values_files
    fi
}

# Export for oc image extract
export REGISTRY_AUTH_FILE="$PULL_SECRET"

# Auto-detect catalog version from openshift-version in values files
if [[ "$CATALOG_OVERRIDE" == "false" ]]; then
    OCP_VERSION=""
    while IFS= read -r values_file; do
        ver=$(grep -E "^[[:space:]]*openshift-version:" "$values_file" 2>/dev/null | head -1 | \
              sed "s/.*:[[:space:]]*//" | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
        if [[ -n "$ver" ]]; then
            # Use the highest version found across all files
            if [[ -z "$OCP_VERSION" ]]; then
                OCP_VERSION="$ver"
            else
                OCP_VERSION=$(printf '%s\n%s\n' "$OCP_VERSION" "$ver" | sort -V | tail -1)
            fi
        fi
    done < <(get_values_files)

    if [[ -z "$OCP_VERSION" ]]; then
        error "No openshift-version found in values files"
        error "Set 'openshift-version' in your clusterset values file or use --catalog to specify the catalog image"
        exit 1
    fi

    # Extract major.minor (e.g., 4.20.12 -> 4.20)
    CATALOG_VERSION=$(echo "$OCP_VERSION" | grep -oE '^[0-9]+\.[0-9]+')
    if [[ -z "$CATALOG_VERSION" ]]; then
        error "Could not parse major.minor from openshift-version: $OCP_VERSION"
        exit 1
    fi

    log "Auto-detected OpenShift version $OCP_VERSION -> catalog v${CATALOG_VERSION}"
else
    # Extract version from --catalog override (e.g., .../redhat-operator-index:v4.18 -> 4.18)
    CATALOG_VERSION=$(echo "$CATALOG" | grep -oE 'v[0-9]+\.[0-9]+' | sed 's/^v//')
fi

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
# Format: label|package|policy_dir|source
# ============================================================================

# Map a CatalogSource name to a registry catalog index image
# Strips mirror suffixes (e.g., redhat-operators-mirror -> redhat-operators)
# Returns the full catalog image URL
source_to_catalog_image() {
    local source="$1"
    local version="$2"

    # Strip common mirror suffixes (e.g., redhat-operators-mirror -> redhat-operators)
    local base_source
    base_source=$(echo "$source" | sed 's/-mirror$//')

    case "$base_source" in
        redhat-operators)
            echo "registry.redhat.io/redhat/redhat-operator-index:v${version}"
            ;;
        certified-operators)
            echo "registry.redhat.io/redhat/certified-operator-index:v${version}"
            ;;
        community-operators)
            echo "registry.redhat.io/redhat/community-operator-index:v${version}"
            ;;
        *)
            # Unknown source - return empty (caller should warn)
            return 1
            ;;
    esac
}

# Build operator mappings dynamically from values files
build_operator_mappings() {
    MAPPINGS_FILE=$(mktemp)

    while IFS= read -r values_file; do
        # Find all *-subscription-name: entries (skip commented lines)
        grep -v '^\s*#' "$values_file" 2>/dev/null | \
        grep -oE '[a-z][-a-z0-9]*-subscription-name:[[:space:]]*[^[:space:]]+' 2>/dev/null | \
        while IFS= read -r line; do
            # Extract label and package
            local label package policy_dir="" source=""
            label=$(echo "$line" | sed 's/-subscription-name:.*//')
            package=$(echo "$line" | sed 's/.*-subscription-name:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r')

            [[ -z "$label" || -z "$package" ]] && continue

            # Extract source for this operator (e.g., redhat-operators)
            # Match only uncommented lines, strip inline comments and quotes
            source=$(grep -E "^[[:space:]]+${label}-source:" "$values_file" 2>/dev/null | grep -v '^\s*#' | head -1 | \
                     sed 's/.*:[[:space:]]*//' | sed 's/#.*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
            # Default to redhat-operators if not specified
            [[ -z "$source" ]] && source="redhat-operators"

            # Find policy directory by searching for name: {package} in policies/*/values.yaml
            for policy_values in "$PROJECT_ROOT"/policies/*/values.yaml; do
                [[ -f "$policy_values" ]] || continue
                if grep -qE "^[[:space:]]+name:[[:space:]]*['\"]?${package}['\"]?" "$policy_values" 2>/dev/null; then
                    policy_dir=$(dirname "$policy_values")
                    break
                fi
            done

            # Store mapping: label|package|policy_dir|source
            echo "${label}|${package}|${policy_dir}|${source}" >> "$MAPPINGS_FILE"
        done
    done < <(get_values_files)

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

# Get source for a label (dynamic lookup)
get_source_for_label() {
    local label="$1"
    [[ -f "$MAPPINGS_FILE" ]] || return
    grep "^${label}|" "$MAPPINGS_FILE" 2>/dev/null | head -1 | cut -d'|' -f4
}

# Get all unique sources across all operators
get_unique_sources() {
    [[ -f "$MAPPINGS_FILE" ]] || return
    cut -d'|' -f4 "$MAPPINGS_FILE" | sort -u
}

# ============================================================================
# CATALOG FUNCTIONS
# ============================================================================

# Extract catalog if not cached
# Usage: ensure_catalog <catalog_image>
ensure_catalog() {
    local catalog_image="$1"
    local catalog_hash

    if command -v md5sum &> /dev/null; then
        catalog_hash=$(echo "$catalog_image" | md5sum | cut -d' ' -f1)
    elif command -v md5 &> /dev/null; then
        catalog_hash=$(echo "$catalog_image" | md5)
    else
        catalog_hash=$(echo "$catalog_image" | cksum | cut -d' ' -f1)
    fi

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

    log "Extracting catalog $catalog_image..."
    mkdir -p "$catalog_dir"

    local extract_output
    extract_output=$(oc image extract "$catalog_image" --path /:"$catalog_dir" --confirm --filter-by-os=linux/amd64 2>&1)
    local extract_rc=$?

    if [[ $extract_rc -ne 0 ]]; then
        extract_output=$(oc image extract "$catalog_image" --path /:"$catalog_dir" --confirm 2>&1)
        extract_rc=$?
    fi

    if [[ $extract_rc -ne 0 ]]; then
        error "Failed to extract catalog: $catalog_image"
        echo "$extract_output" >&2
        rm -rf "$catalog_dir"
        return 1
    fi

    local extracted_count
    extracted_count=$(find "$catalog_dir/configs" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l)
    if [[ $extracted_count -eq 0 ]]; then
        error "Catalog extraction produced no operator packages"
        echo "Extract output: $extract_output" >&2
        rm -rf "$catalog_dir"
        return 1
    fi

    success "Catalog extracted to $catalog_dir ($(echo $extracted_count | xargs) operators)"
    echo "$catalog_dir"
}

# Ensure all needed catalogs are extracted based on operator sources
# Populates CATALOG_DIRS_FILE with source|catalog_dir mappings
ensure_all_catalogs() {
    CATALOG_DIRS_FILE=$(mktemp)

    if [[ "$CATALOG_OVERRIDE" == "true" ]]; then
        # User specified a single catalog - use it for everything
        local catalog_dir
        catalog_dir=$(ensure_catalog "$CATALOG")
        if [[ -z "$catalog_dir" ]]; then
            error "Failed to extract catalog: $CATALOG"
            exit 1
        fi
        # Map all sources to this single catalog
        for source in $(get_unique_sources); do
            echo "${source}|${catalog_dir}" >> "$CATALOG_DIRS_FILE"
        done
        return 0
    fi

    # Auto mode: extract the right catalog for each unique source
    local unique_sources
    unique_sources=$(get_unique_sources)

    for source in $unique_sources; do
        local catalog_image
        catalog_image=$(source_to_catalog_image "$source" "$CATALOG_VERSION" 2>/dev/null) || true
        if [[ -z "$catalog_image" ]]; then
            warn "Unknown operator source '$source' - cannot map to a catalog index. Skipping operators from this source."
            continue
        fi

        local catalog_dir
        catalog_dir=$(ensure_catalog "$catalog_image") || true
        if [[ -z "$catalog_dir" ]]; then
            warn "Failed to extract catalog for source '$source' ($catalog_image). Skipping."
            continue
        fi

        echo "${source}|${catalog_dir}" >> "$CATALOG_DIRS_FILE"
    done
}

# Get the catalog directory for a given source
get_catalog_dir_for_source() {
    local source="$1"
    [[ -f "$CATALOG_DIRS_FILE" ]] || return
    grep "^${source}|" "$CATALOG_DIRS_FILE" 2>/dev/null | head -1 | cut -d'|' -f2
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
    if [[ "$channel1" =~ -v?([0-9]+\.[0-9]+)$ ]]; then
        ver1="${BASH_REMATCH[1]}"
    fi
    if [[ "$channel2" =~ -v?([0-9]+\.[0-9]+)$ ]]; then
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

    while IFS= read -r file; do
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
    done < <(get_update_files)

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
    while IFS= read -r file; do
        current=$(grep -E "^[[:space:]]*${label}-channel:" "$file" 2>/dev/null | head -1 | \
                  sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
        if [[ -n "$current" ]]; then
            echo "$current"
            return 0
        fi
    done < <(get_values_files)

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

    # Extract or use cached catalogs (one per unique source)
    ensure_all_catalogs
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

        # Get the correct catalog for this operator's source
        local source catalog_dir
        source=$(get_source_for_label "$label")
        catalog_dir=$(get_catalog_dir_for_source "$source")

        if [[ -z "$catalog_dir" ]]; then
            printf "%-35s %-20s %-20s %s\n" "$package" "-" "-" "${YELLOW}no catalog for source: ${source}${NC}"
            continue
        fi

        # Get channels for this package from catalog
        local channels
        channels=$(get_channels "$catalog_dir" "$package" || true)
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
