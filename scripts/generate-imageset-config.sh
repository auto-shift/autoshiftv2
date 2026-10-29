#!/bin/bash
# AutoShift ImageSetConfiguration Generator for oc-mirror
# Generates ImageSetConfiguration YAML from AutoShift values files for disconnected environments
#
# OPERATOR DETECTION REQUIREMENTS:
# This script dynamically discovers operators by scanning for '{operator}-subscription-name'
# entries in your values files. For each operator to be detected, you MUST define:
#
#   {operator}: 'true'                        # Enable the operator
#   {operator}-subscription-name: 'package'   # OLM package name (REQUIRED for detection)
#   {operator}-channel: 'channel'             # Operator channel
#   {operator}-source: 'redhat-operators'     # Catalog source
#   {operator}-source-namespace: 'openshift-marketplace'
#
# The subscription-name label is the canonical key that links labels to OLM packages.
# Without it, the operator will NOT be included in the generated ImageSetConfiguration.
#
# No hardcoded operator lists - new operators are auto-discovered when added to AutoShift!
#
# Usage: ./generate-imageset-config.sh <values-files> [options]
# Example: ./generate-imageset-config.sh values/clustersets/hub.yaml
# Example: ./generate-imageset-config.sh values/clustersets/hub.yaml,values/clustersets/sbx.yaml --openshift-version 4.20
# Example: ./generate-imageset-config.sh values/clustersets/hub.yaml --openshift-version 4.20.29
# Example: ./generate-imageset-config.sh values/clustersets/hub.yaml --openshift-version 4.20 --min-version 4.20.5 --max-version 4.20.29
# Example: ./generate-imageset-config.sh values/clustersets/hub-baremetal-sno.yaml --operators-only

set -e

# Colors for output (enabled if either stdout or stderr is a terminal)
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

# Default values
VALUES_FILES=""
VALUES_FILES_ARRAY=()
OPENSHIFT_VERSION=""  # Will be read from values file or command line
MIN_VERSION=""
MAX_VERSION=""
OUTPUT_FILE=""
INCLUDE_OPENSHIFT=true
INCLUDE_OPERATORS=true
INCLUDE_AUTOSHIFT_CHARTS=false
ALL_OPERATORS=false
AUTOSHIFT_REGISTRY=""
AUTOSHIFT_VERSION=""
DEPENDENCIES_FILE=""

# Script directory for relative paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CATALOG_CACHE_DIR="$PROJECT_ROOT/.cache/catalog-cache"
GET_DEPS_SCRIPT="$SCRIPT_DIR/get-operator-dependencies.sh"

# Local temp directory (avoids /tmp issues on Windows/Git Bash)
LOCAL_TMP="$PROJECT_ROOT/.tmp"
mkdir -p "$LOCAL_TMP"

# Temp file for operator mappings (portable alternative to associative arrays)
MAPPINGS_FILE=""
# Marker extract_operators touches from its subshell when an enabled operator is incomplete
EXTRACT_ERRORS_FILE="$LOCAL_TMP/extract-errors.$$"

cleanup_temp_files() {
    [[ -n "$MAPPINGS_FILE" ]] && rm -f "$MAPPINGS_FILE" || true
    rm -f "$EXTRACT_ERRORS_FILE"
    # Only cleanup DEPENDENCIES_FILE if it was auto-generated (not passed via --dependencies-file)
    [[ -n "$DEPENDENCIES_FILE" && "$DEPENDENCIES_FILE" == "$LOCAL_TMP"/* ]] && rm -f "$DEPENDENCIES_FILE" || true
}
trap cleanup_temp_files EXIT

# ============================================================================
# DYNAMIC OPERATOR MAPPINGS
# Uses subscription-name as the canonical key - no hardcoded mappings!
# Mappings stored in temp file for bash 3.x compatibility
# Format: label|package|policy_dir
# ============================================================================

# Build operator mappings dynamically from values files
build_operator_mappings() {
    MAPPINGS_FILE="$LOCAL_TMP/operator-mappings.$$"

    for values_file in "$PROJECT_ROOT"/autoshift/values/clustersets/*.yaml; do
        [[ -f "$values_file" ]] || continue
        # Skip example files
        [[ "$(basename "$values_file")" == _* ]] && continue

        # Find all *-subscription-name: entries
        grep -oE '[a-z][-a-z0-9]*-subscription-name:[[:space:]]*[^[:space:]]+' "$values_file" 2>/dev/null | \
        while IFS= read -r line; do
            # Extract label and package
            local label package policy_dir=""
            label=$(echo "$line" | sed 's/-subscription-name:.*//')
            package=$(echo "$line" | sed 's/.*-subscription-name:[[:space:]]*//' | tr -d "'" | tr -d '"')

            [[ -z "$label" || -z "$package" ]] && continue

            # Find policy directory by searching for name: {package} in policies values files
            for policy_values in "$PROJECT_ROOT"/policies/stable/*/values.yaml "$PROJECT_ROOT"/policies/certified/*/values.yaml "$PROJECT_ROOT"/policies/community/*/values.yaml; do
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

# Resolve dependencies for all operators
# This calls get-operator-dependencies.sh and stores the result
resolve_all_dependencies() {
    local operators_list="$1"
    local catalog_version="$2"

    if [[ -z "$operators_list" ]]; then
        return
    fi

    # Without dependencies the imageset lacks operators OLM must install alongside (multicluster-engine
    # for ACM, the ODF stack), so a mirror built from it cannot install them. Fail rather than continue.
    if [[ ! -x "$GET_DEPS_SCRIPT" ]]; then
        log_error "get-operator-dependencies.sh not found or not executable"
        return 1
    fi

    # Detect Windows/Git Bash — dependency resolution won't work
    if [[ "$OSTYPE" == "msys" || "$OSTYPE" == "cygwin" ]]; then
        log_error "Windows/Git Bash detected — cannot resolve dependencies (requires oc image extract with symlinks)"
        log_error "Use --dependencies-file scripts/operator-dependencies.json instead"
        log_error "Generate the file on Mac/Linux/WSL2: ./scripts/get-operator-dependencies.sh --json > scripts/operator-dependencies.json"
        return 1
    fi

    log_step "Resolving operator dependencies..."

    # Create temp file for dependencies
    DEPENDENCIES_FILE="$LOCAL_TMP/dependencies.$$.json"
    local deps_log="$LOCAL_TMP/dependencies.$$.log"

    # Call get-operator-dependencies.sh with all operators
    local catalog_image="registry.redhat.io/redhat/redhat-operator-index:v${catalog_version}"

    if "$GET_DEPS_SCRIPT" --catalog "$catalog_image" --operators "$operators_list" --json > "$DEPENDENCIES_FILE" 2>"$deps_log"; then
        local dep_count=$(jq 'to_entries | map(select(.value | length > 0)) | length' "$DEPENDENCIES_FILE" 2>/dev/null || echo "0")
        log_success "Resolved dependencies for $dep_count operators with dependencies"
        rm -f "$deps_log"
    else
        log_error "Failed to resolve operator dependencies from $catalog_image:"
        cat "$deps_log" >&2
        rm -f "$DEPENDENCIES_FILE" "$deps_log"
        DEPENDENCIES_FILE=""
        return 1
    fi
}

# Extract a catalog into the cache so its channels can be checked. The dependency resolution above
# only covers redhat-operator-index; certified and community operators are checked the same way.
cache_catalog() {
    local catalog_image="$1"
    local operators_list="$2"
    local deps_log="$LOCAL_TMP/catalog-cache.$$.log"

    [[ -n "$(get_catalog_cache_configs "$catalog_image")" ]] && return 0
    [[ "$OSTYPE" == "msys" || "$OSTYPE" == "cygwin" ]] && return 0

    log_step "Caching $catalog_image to check its channels..."
    if ! "$GET_DEPS_SCRIPT" --catalog "$catalog_image" --operators "$operators_list" --json > /dev/null 2>"$deps_log"; then
        log_error "Failed to extract $catalog_image:"
        cat "$deps_log" >&2
        rm -f "$deps_log"
        return 1
    fi
    rm -f "$deps_log"
}

# Cache a certified or community catalog and check its channels. Skipped with --dependencies-file,
# which is the offline path; validate_catalog_channels then warns that nothing was checked.
check_other_catalog() {
    local packages="$1"
    local catalog_image="$2"
    if [[ "$DEPENDENCIES_FILE" == "$LOCAL_TMP"/* ]]; then
        local names
        names=$(for p in $packages; do echo "${p%%|*}"; done | paste -sd, -)
        cache_catalog "$catalog_image" "$names" || return 1
    fi
    validate_catalog_channels "$packages" "$catalog_image"
}

# Cached config directory for a catalog image (empty if not cached)
get_catalog_cache_configs() {
    local catalog_image="$1"
    local cache_key=""

    if command -v md5sum &> /dev/null; then
        cache_key=$(echo "$catalog_image" | md5sum | cut -d' ' -f1)
    elif command -v md5 &> /dev/null; then
        cache_key=$(echo "$catalog_image" | md5)
    else
        cache_key=$(echo "$catalog_image" | cksum | cut -d' ' -f1)
    fi

    local configs_dir="$CATALOG_CACHE_DIR/$cache_key/configs"
    [[ -d "$configs_dir" ]] && echo "$configs_dir"
    return 0
}

# Channel names an operator has in the cached catalog, one per line. FBC packages are laid out as
# catalog.json, catalog.yaml, or split files, so read every non-bundle file.
get_catalog_channels() {
    local pkg_dir="$1"
    find "$pkg_dir" -type f -name '*.json' -not -path '*/bundles*' -exec cat {} + 2>/dev/null \
        | jq -r 'select(.schema == "olm.channel") | .name' 2>/dev/null
    find "$pkg_dir" -type f \( -name '*.yaml' -o -name '*.yml' \) -not -path '*/bundles*' -exec cat {} + 2>/dev/null \
        | awk '/^---/ { if (s == "olm.channel" && n != "") print n; s = ""; n = ""; next }
               /^schema:/ { s = $2 } /^name:/ { n = $2 }
               END { if (s == "olm.channel" && n != "") print n }' | tr -d "\"'"
}

# Fail when a requested channel is not in the catalog. oc-mirror 4.22 drops an unknown channel
# without a warning, so the values file keeps a channel no mirrored Subscription can resolve.
# Skipped when the catalog is not cached.
#
# Sets VALID_PACKAGES to the packages to write. With --all-operators a package the catalog does not
# carry is left out with a warning: a vendor that has not published for this OpenShift version should
# not block a release, and nothing could be mirrored for it anyway. SKIPPED_PACKAGES records them.
VALID_PACKAGES=""
SKIPPED_PACKAGES=""
validate_catalog_channels() {
    local packages="$1"
    local catalog_image="$2"
    local catalog_label="${catalog_image##*/}"
    local configs_dir
    configs_dir=$(get_catalog_cache_configs "$catalog_image")
    VALID_PACKAGES="$packages"

    if [[ -z "$configs_dir" ]]; then
        log_warning "$catalog_label is not cached; channels not checked against the catalog"
        return 0
    fi

    VALID_PACKAGES=""
    local invalid=0 package_info name channels channel available
    for package_info in $packages; do
        [[ -z "$package_info" ]] && continue
        IFS='|' read -r name channels <<< "$package_info"
        if [[ ! -d "$configs_dir/$name" ]]; then
            if [[ "$ALL_OPERATORS" == "true" ]]; then
                log_warning "Package $name is not in $catalog_label; left out of the imageset"
                SKIPPED_PACKAGES+="$name ($catalog_label) "
            else
                log_error "Package $name is not in $catalog_label"
                invalid=1
            fi
            continue
        fi
        VALID_PACKAGES+="$package_info "
        available=$(get_catalog_channels "$configs_dir/$name" | sort -u)
        IFS=',' read -ra channels_array <<< "$channels"
        for channel in "${channels_array[@]}"; do
            channel=$(echo "$channel" | xargs)
            [[ -z "$channel" ]] && continue
            if ! grep -qxF "$channel" <<< "$available"; then
                log_error "Channel $channel of $name is not in $catalog_label. Available: $(echo $available)"
                invalid=1
            fi
        done
    done

    if [[ $invalid -ne 0 ]]; then
        log_error "Fix the <operator>-channel labels in the values files, then run again"
        return 1
    fi
}

# Get default channel for an operator from the catalog cache
get_default_channel() {
    local operator="$1"
    local catalog_version="$2"
    local default_channel=""

    local configs_dir
    configs_dir=$(get_catalog_cache_configs "registry.redhat.io/redhat/redhat-operator-index:v${catalog_version}")
    local pkg_dir="$configs_dir/$operator"

    if [[ -z "$configs_dir" || ! -d "$pkg_dir" ]]; then
        echo ""
        return
    fi

    # Check for package.json (split structure)
    if [[ -f "$pkg_dir/package.json" ]]; then
        default_channel=$(jq -r '.defaultChannel // empty' "$pkg_dir/package.json" 2>/dev/null)
    # Check for catalog.json (combined structure)
    elif [[ -f "$pkg_dir/catalog.json" ]]; then
        default_channel=$(jq -r 'select(.schema == "olm.package") | .defaultChannel // empty' "$pkg_dir/catalog.json" 2>/dev/null)
    # Check for catalog.yaml (newer catalog format)
    elif [[ -f "$pkg_dir/catalog.yaml" ]]; then
        default_channel=$(grep '^defaultChannel:' "$pkg_dir/catalog.yaml" 2>/dev/null | head -1 | awk '{print $2}' | tr -d '"' || true)
    fi

    echo "$default_channel"
}

# Version parsing function
parse_openshift_version() {
    local version="$1"
    local custom_min="$2"
    local custom_max="$3"
    local channel_name=""
    local min_version=""
    local max_version=""
    
    # Check if version has patch (e.g., 4.18.22)
    if [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        # Full version with patch - extract major.minor
        local major_minor
        major_minor=$(echo "$version" | cut -d. -f1-2)
        channel_name="stable-$major_minor"
        min_version="$version"
        max_version="$version"
    elif [[ "$version" =~ ^[0-9]+\.[0-9]+$ ]]; then
        # Major.minor only
        channel_name="stable-$version"
        min_version="$version.0"
        max_version="$version.999"
    else
        echo -e "${RED}❌ Invalid OpenShift version format in parse function: $version${NC}" >&2
        echo -e "${RED}❌ Expected format: X.Y (e.g., 4.18) or X.Y.Z (e.g., 4.18.22)${NC}" >&2
        exit 1
    fi
    
    # Override with custom min/max if provided
    [[ -n "$custom_min" ]] && min_version="$custom_min"
    [[ -n "$custom_max" ]] && max_version="$custom_max"
    
    echo "$channel_name|$min_version|$max_version"
}

usage() {
    echo "Usage: $0 <values-files> [options]"
    echo ""
    echo "Generates ImageSetConfiguration for oc-mirror from AutoShift values files."
    echo "Automatically discovers all enabled operators dynamically - no hardcoded lists!"
    echo "Supports multiple values files with channel merging for operators with different channels."
    echo ""
    echo "Arguments:"
    echo "  values-files          AutoShift values file(s) to process. Can be:"
    echo "                        - Single file: values/clustersets/hub.yaml"
    echo "                        - Multiple files: values/clustersets/hub.yaml,values/clustersets/sbx.yaml"
    echo "                        - Available files in autoshift/values/clustersets/:"
    echo "                          hub.yaml, managed.yaml, sbx.yaml, hubofhubs.yaml,"
    echo "                          hub-baremetal-sno.yaml, hub-baremetal-compact.yaml"
    echo ""
    echo "Options:"
    echo "  --openshift-version VERSION    OpenShift version to mirror (default: $OPENSHIFT_VERSION)"
    echo "                                 Supports formats: X.Y (e.g., 4.18) or X.Y.Z (e.g., 4.18.22)"
    echo "                                 X.Y format: channel=stable-X.Y, minVersion=X.Y.0, maxVersion=X.Y.999"
    echo "                                 X.Y.Z format: channel=stable-X.Y, minVersion=X.Y.Z, maxVersion=X.Y.Z"
    echo "  --min-version VERSION          Override minimum version (e.g., 4.18.15)"
    echo "  --max-version VERSION          Override maximum version (e.g., 4.18.25)"
    echo "  --output FILE                  Output file path (default: imageset-config-<combined-name>.yaml)"
    echo "  --operators-only               Only include operators, skip OpenShift platform"
    echo "  --openshift-only               Only include OpenShift platform, skip operators"
    echo "  --include-autoshift-charts     Include AutoShift Helm charts from OCI registry"
    echo "  --autoshift-registry URL       AutoShift OCI registry (default: oci://quay.io/autoshift)"
    echo "  --autoshift-version VERSION    AutoShift chart version to mirror"
    echo "  --input FILE                   Input values file (alternative to positional argument)"
    echo "  --dependencies-file FILE       JSON file with operator dependencies (from get-operator-dependencies.sh)"
    echo "  --all-operators                Mirror every operator the values files declare with"
    echo "                                 <label>-subscription-name, enabled or not. Use with the"
    echo "                                 _example*.yaml files, the label contract CI enforces, for a release"
    echo "  --help                         Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 values/clustersets/hub.yaml"
    echo "  $0 values/clustersets/hub.yaml,values/clustersets/sbx.yaml --openshift-version 4.20"
    echo "  $0 values/clustersets/hub.yaml --openshift-version 4.20.29"
    echo "  $0 values/clustersets/hub.yaml --openshift-version 4.20 --min-version 4.20.5 --max-version 4.20.29"
    echo "  $0 values/clustersets/hub-baremetal-sno.yaml --operators-only"
    echo "  $0 values/clustersets/hub.yaml --output my-imageset.yaml"
    echo ""
    echo "Channel Merging:"
    echo "  When multiple files specify different channels for the same operator,"
    echo "  both channels will be included in the ImageSetConfiguration."
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --openshift-version)
            OPENSHIFT_VERSION="$2"
            shift 2
            ;;
        --min-version)
            MIN_VERSION="$2"
            shift 2
            ;;
        --max-version)
            MAX_VERSION="$2"
            shift 2
            ;;
        --output)
            OUTPUT_FILE="$2"
            shift 2
            ;;
        --operators-only)
            INCLUDE_OPENSHIFT=false
            shift
            ;;
        --openshift-only)
            INCLUDE_OPERATORS=false
            shift
            ;;
        --include-autoshift-charts)
            INCLUDE_AUTOSHIFT_CHARTS=true
            shift
            ;;
        --autoshift-registry)
            AUTOSHIFT_REGISTRY="$2"
            INCLUDE_AUTOSHIFT_CHARTS=true
            shift 2
            ;;
        --autoshift-version)
            AUTOSHIFT_VERSION="$2"
            INCLUDE_AUTOSHIFT_CHARTS=true
            shift 2
            ;;
        --input)
            VALUES_FILES="$2"
            shift 2
            ;;
        --dependencies-file)
            DEPENDENCIES_FILE="$2"
            shift 2
            ;;
        --all-operators)
            ALL_OPERATORS=true
            shift
            ;;
        --help)
            usage
            exit 0
            ;;
        -*)
            echo -e "${RED}Error: Unknown option $1${NC}"
            usage
            exit 1
            ;;
        *)
            if [[ -z "$VALUES_FILES" ]]; then
                VALUES_FILES="$1"
            else
                echo -e "${RED}Error: Too many positional arguments${NC}"
                usage
                exit 1
            fi
            shift
            ;;
    esac
done

# Validate required arguments
if [[ -z "$VALUES_FILES" ]]; then
    echo -e "${RED}Error: Values files are required${NC}"
    usage
    exit 1
fi

# Helper functions
log_step() {
    echo -e "${BLUE}🔧 $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Extract major.minor version for catalog versioning (e.g., 4.18.22 -> 4.18)
get_catalog_version() {
    if [[ "$OPENSHIFT_VERSION" =~ ^([0-9]+\.[0-9]+) ]]; then
        echo "${BASH_REMATCH[1]}"
    else
        echo "$OPENSHIFT_VERSION"
    fi
}

# Extract OpenShift versions from all labels in values files
extract_openshift_versions() {
    local values_file="$1"
    local versions=()
    local in_labels=false
    local current_section=""
    
    # Read the YAML file and extract all openshift-version values from labels sections
    while IFS= read -r line; do
        # Check if we're entering a labels section
        if [[ "$line" =~ ^[[:space:]]*labels:[[:space:]]*$ ]]; then
            in_labels=true
            continue
        fi
        
        # Check if we're exiting labels section (new section starts)
        if [[ "$line" =~ ^[[:space:]]*[a-zA-Z][a-zA-Z0-9_-]*:[[:space:]]*$ ]] && [[ "$in_labels" == true ]]; then
            in_labels=false
            continue
        fi
        
        # Check if we're exiting labels section (cluster section ends)
        if [[ "$line" =~ ^[a-zA-Z][a-zA-Z0-9_-]*:[[:space:]]*$ ]]; then
            in_labels=false
            continue
        fi
        
        # Extract openshift-version if we're in a labels section
        if [[ "$in_labels" == true ]] && [[ "$line" =~ ^[[:space:]]*openshift-version:[[:space:]]*[\'\"]*([0-9]+\.[0-9]+\.[0-9]+)[\'\"]*[[:space:]]*$ ]]; then
            local version="${BASH_REMATCH[1]}"
            if [[ -n "$version" ]]; then
                versions+=("$version")
            fi
        fi
    done < "$values_file"
    
    # Return unique versions
    printf '%s\n' "${versions[@]}" | sort -u
}

# Get min and max versions from a list of versions
get_version_range() {
    local versions=("$@")
    if [[ ${#versions[@]} -eq 0 ]]; then
        echo "" ""
        return
    fi
    
    # Sort versions and get min/max using safer array handling
    local sorted_string
    sorted_string=$(printf '%s\n' "${versions[@]}" | sort -V | tr '\n' ' ')
    local sorted_versions=($sorted_string)
    local min_version="${sorted_versions[0]}"
    local max_version="${sorted_versions[${#sorted_versions[@]}-1]}"
    
    echo "$min_version" "$max_version"
}

# Parse comma-separated values files
IFS=',' read -ra VALUES_FILES_ARRAY <<< "$VALUES_FILES"

# Validate all input files exist
for values_file in "${VALUES_FILES_ARRAY[@]}"; do
    # Strip leading/trailing whitespace
    values_file=$(echo "$values_file" | xargs)

    # Construct full path - check multiple locations
    if [[ "$values_file" == /* ]]; then
        # Absolute path
        input_file="$values_file"
    elif [[ -f "$values_file" ]]; then
        # File exists at given relative path
        input_file="$values_file"
    elif [[ -f "autoshift/$values_file" ]]; then
        # File exists in autoshift/ directory
        input_file="autoshift/$values_file"
    else
        input_file="$values_file"
    fi

    if [[ ! -f "$input_file" ]]; then
        echo -e "${RED}Error: Values file $input_file not found${NC}"
        exit 1
    fi
done

# Read OpenShift versions from values files if not specified on command line
if [[ -z "$OPENSHIFT_VERSION" ]]; then
    # Collect all versions from all values files
    all_versions=()
    for values_file in "${VALUES_FILES_ARRAY[@]}"; do
        values_file=$(echo "$values_file" | xargs)
        if [[ "$values_file" == /* ]]; then
            input_file="$values_file"
        elif [[ -f "$values_file" ]]; then
            input_file="$values_file"
        elif [[ -f "autoshift/$values_file" ]]; then
            input_file="autoshift/$values_file"
        else
            input_file="$values_file"
        fi

        # Extract versions from this file
        while IFS= read -r version; do
            if [[ -n "$version" ]]; then
                all_versions+=("$version")
            fi
        done < <(extract_openshift_versions "$input_file")
    done
    
    if [[ ${#all_versions[@]} -eq 0 ]]; then
        echo -e "${RED}Error: No OpenShift version found in any values file and none specified with --openshift-version${NC}"
        echo -e "${RED}Please add 'openshift-version: 'X.Y.Z'' to labels section in your values file or use --openshift-version flag${NC}"
        exit 1
    fi
    
    # Get unique versions
    unique_versions=($(printf '%s\n' "${all_versions[@]}" | sort -u))
    
    if [[ ${#unique_versions[@]} -eq 1 ]]; then
        OPENSHIFT_VERSION="${unique_versions[0]}"
        log_step "Using OpenShift version from labels: $OPENSHIFT_VERSION"
    else
        # Multiple versions detected - get min/max for platform, use highest for binary downloads
        read min_version max_version < <(get_version_range "${unique_versions[@]}")
        OPENSHIFT_VERSION="$max_version"  # Use highest version for binary downloads
        
        echo -e "${YELLOW}⚠️  Multiple OpenShift versions detected: ${unique_versions[*]}${NC}"
        echo -e "${YELLOW}⚠️  Using version range: min=$min_version, max=$max_version for platform images${NC}"
        echo -e "${YELLOW}⚠️  Using highest version ($OPENSHIFT_VERSION) for binary downloads${NC}"
        
        # Set min/max if not already specified
        if [[ -z "$MIN_VERSION" ]]; then
            MIN_VERSION="$min_version"
        fi
        if [[ -z "$MAX_VERSION" ]]; then
            MAX_VERSION="$max_version"
        fi
    fi
fi

# Validate OpenShift version format if we're including platform
if [[ "$INCLUDE_OPENSHIFT" == "true" ]]; then
    # Check for X.Y.Z format first, then X.Y format
    if [[ "$OPENSHIFT_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || [[ "$OPENSHIFT_VERSION" =~ ^[0-9]+\.[0-9]+$ ]]; then
        log_step "OpenShift version format validated: $OPENSHIFT_VERSION"
    else
        echo -e "${RED}❌ Invalid OpenShift version format: $OPENSHIFT_VERSION${NC}"
        echo -e "${RED}❌ Expected format: X.Y (e.g., 4.18) or X.Y.Z (e.g., 4.18.22)${NC}"
        exit 1
    fi
    
    # Validate min version format if provided
    if [[ -n "$MIN_VERSION" ]] && ! [[ "$MIN_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo -e "${RED}❌ Invalid minimum version format: $MIN_VERSION${NC}"
        echo -e "${RED}❌ Expected format: X.Y.Z (e.g., 4.18.15)${NC}"
        exit 1
    fi
    
    # Validate max version format if provided
    if [[ -n "$MAX_VERSION" ]] && ! [[ "$MAX_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo -e "${RED}❌ Invalid maximum version format: $MAX_VERSION${NC}"
        echo -e "${RED}❌ Expected format: X.Y.Z (e.g., 4.18.25)${NC}"
        exit 1
    fi
fi

# Set output file if not specified
if [[ -z "$OUTPUT_FILE" ]]; then
    # Create output filename from input files
    filename_prefix="imageset-config"
    
    if [[ ${#VALUES_FILES_ARRAY[@]} -eq 1 ]]; then
        # Single file: extract base name
        base_name=$(basename "${VALUES_FILES_ARRAY[0]}" .yaml)
        base_name=${base_name#values.}  # Remove 'values.' prefix
        OUTPUT_FILE="$filename_prefix-$base_name.yaml"
    else
        # Multiple files: create combined name
        combined_name=""
        for values_file in "${VALUES_FILES_ARRAY[@]}"; do
            values_file=$(echo "$values_file" | xargs)
            base_name=$(basename "$values_file" .yaml)
            base_name=${base_name#values.}  # Remove 'values.' prefix
            if [[ -z "$combined_name" ]]; then
                combined_name="$base_name"
            else
                combined_name="$combined_name-$base_name"
            fi
        done
        OUTPUT_FILE="$filename_prefix-$combined_name.yaml"
    fi
fi

# Get policy values.yaml file path for a label (dynamic lookup)
get_policy_file_for_label() {
    local label="$1"
    [[ -f "$MAPPINGS_FILE" ]] || return
    local policy_dir
    policy_dir=$(grep "^${label}|" "$MAPPINGS_FILE" 2>/dev/null | head -1 | cut -d'|' -f3)
    if [[ -n "$policy_dir" ]]; then
        echo "$policy_dir/values.yaml"
    else
        # Fallback to direct directory match
        echo "$PROJECT_ROOT/policies/stable/$label/values.yaml"
    fi
}

# Get operator subscription name from labels using dynamic discovery
# Returns empty string for non-operators (labels without {label}-subscription-name entry)
get_operator_subscription_name() {
    local label_name="$1"
    local values_file="$2"
    local package=""

    # First try to get explicit subscription name from the autoshift values file
    local subscription_name
    subscription_name=$(grep -E "^[[:space:]]*$label_name-subscription-name:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r')

    if [[ -n "$subscription_name" ]]; then
        echo "$subscription_name"
        return 0
    fi

    # Second, try from mappings file (built from all subscription-name entries)
    if [[ -f "$MAPPINGS_FILE" ]]; then
        package=$(grep "^${label_name}|" "$MAPPINGS_FILE" 2>/dev/null | head -1 | cut -d'|' -f2)
        if [[ -n "$package" ]]; then
            echo "$package"
            return 0
        fi
    fi

    # Third, try to find from policy values.yaml
    local policy_file
    policy_file=$(get_policy_file_for_label "$label_name")

    if [[ -n "$policy_file" ]] && [[ -f "$policy_file" ]]; then
        # Look for name: field in the values.yaml
        package=$(grep -E '^\s+name:' "$policy_file" 2>/dev/null | head -1 | \
                  sed 's/.*name:\s*//' | tr -d "'" | tr -d '"' | tr -d '\r' | xargs)
        if [[ -n "$package" ]]; then
            echo "$package"
            return 0
        fi
    fi

    # No mapping found - return empty (caller will skip this operator)
    echo ""
}

# Extract enabled operators from values file
# Uses dynamic discovery - only labels with {label}-subscription-name: are considered operators
extract_operators() {
    local values_file="$1"
    local operators=()

    # Parse YAML to find enabled operators
    # Look for patterns like: operator-name: 'true' followed by operator-name-channel, operator-name-source
    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ -z "$(echo "$line" | xargs)" ]] && continue

        # Look for enabled operator (value: 'true') - handle inline comments. With --all-operators every
        # declared operator counts, so match its subscription-name label instead of the enable flag.
        local label_name=""
        if [[ "$ALL_OPERATORS" == "true" ]]; then
            if echo "$line" | grep -qE "^[[:space:]]*[a-z0-9-]+-subscription-name:"; then
                label_name=$(echo "$line" | sed -E 's/^[[:space:]]*([a-z0-9-]+)-subscription-name:.*/\1/')
            fi
        elif echo "$line" | grep -qE "^[[:space:]]*[a-z0-9-]+:[[:space:]]*['\"]?true['\"]?"; then
            label_name=$(echo "$line" | sed -E 's/^[[:space:]]*([a-z0-9-]+):[[:space:]]*['\''"]?true['\''"]?.*/\1/')
        fi
        if [[ -n "$label_name" ]]; then

            # Get operator subscription name - returns empty for non-operators
            # This is the ONLY filter needed: if no subscription-name mapping exists, it's not an operator
            local operator_name
            operator_name=$(get_operator_subscription_name "$label_name" "$values_file")

            # Skip if no operator name found (not an operator)
            [[ -z "$operator_name" ]] && continue

            # Extract operator details from entire file (not just next 10 lines)
            local channel source source_namespace install_plan_approval
            channel=$(grep -E "^[[:space:]]*$label_name-channel:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r')
            source=$(grep -E "^[[:space:]]*$label_name-source:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r')
            source_namespace=$(grep -E "^[[:space:]]*$label_name-source-namespace:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r')

            # A missing field used to drop the operator without a word. The policy's own default would
            # apply on the cluster, but this script cannot read it, so name the gap and fail.
            if [[ -n "$channel" && -n "$source" && -n "$source_namespace" ]]; then
                echo "$operator_name|$channel|$source|$source_namespace"
            else
                log_error "$(basename "$values_file"): $label_name is enabled but $label_name-channel, -source or -source-namespace is not set" >&2
                touch "$EXTRACT_ERRORS_FILE"
            fi
        fi
    done < "$values_file"
}

# Extract ACM configuration (always required)
extract_acm_operator() {
    local values_file="$1"
    
    # Extract ACM configuration - it's always required for AutoShift
    local subscription_name channel source source_namespace
    subscription_name=$(grep -E "^[[:space:]]*acm-subscription-name:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r')
    channel=$(grep -E "^[[:space:]]*acm-channel:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r')
    source=$(grep -E "^[[:space:]]*acm-source:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r')
    source_namespace=$(grep -E "^[[:space:]]*acm-source-namespace:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | tr -d '\r')
    
    # Use default subscription name if not specified
    [[ -z "$subscription_name" ]] && subscription_name="advanced-cluster-management"
    
    # Only add if we have the required fields
    if [[ -n "$channel" && -n "$source" && -n "$source_namespace" ]]; then
        echo "$subscription_name|$channel|$source|$source_namespace"
        log_step "Found ACM operator (always required): $subscription_name (channel: $channel, source: $source)" >&2
    fi
}

# Extract and log operators from multiple files with channel merging
extract_and_log_operators_multi() {
    local values_files_array=("$@")
    local include_acm="true"  # Always include ACM when processing operators
    
    # Use associative arrays to track operators and their channels
    local operator_names=""
    local operator_channels=""
    local operator_sources=""
    local operator_source_namespaces=""
    
    # Process each values file
    for values_file in "${values_files_array[@]}"; do
        values_file=$(echo "$values_file" | xargs)
        if [[ "$values_file" == /* ]]; then
            input_file="$values_file"
        elif [[ -f "$values_file" ]]; then
            input_file="$values_file"
        elif [[ -f "autoshift/$values_file" ]]; then
            input_file="autoshift/$values_file"
        else
            input_file="$values_file"
        fi

        log_step "Processing file: $values_file" >&2
        
        # Include ACM first only if requested (when processing operators)
        if [[ "$include_acm" == "true" ]]; then
            local acm_info
            acm_info=$(extract_acm_operator "$input_file")
            if [[ -n "$acm_info" ]]; then
                IFS='|' read -r name channel source source_namespace <<< "$acm_info"
                
                # Add operator if not seen before, or add channel if operator exists with different channel
                if [[ "$operator_names" != *"|$name|"* ]]; then
                    operator_names+="|$name|"
                    operator_channels+="|$name:$channel|"
                    operator_sources+="|$name:$source|"
                    operator_source_namespaces+="|$name:$source_namespace|"
                    log_step "Found ACM operator (always required): $name (channel: $channel, source: $source)" >&2
                else
                    # Check if this channel is already included for this operator
                    if [[ "$operator_channels" != *"|$name:$channel|"* ]]; then
                        operator_channels+="|$name:$channel|"
                        log_step "Added additional channel for ACM: $name (channel: $channel)" >&2
                    fi
                fi
                include_acm="false"  # Only include ACM once
            fi
        fi
        
        # Then add other enabled operators
        while IFS= read -r operator_info; do
            [[ -z "$operator_info" ]] && continue
            
            IFS='|' read -r name channel source source_namespace <<< "$operator_info"
            
            # Add operator if not seen before, or add channel if operator exists with different channel
            if [[ "$operator_names" != *"|$name|"* ]]; then
                operator_names+="|$name|"
                operator_channels+="|$name:$channel|"
                operator_sources+="|$name:$source|"
                operator_source_namespaces+="|$name:$source_namespace|"
                log_step "Found enabled operator: $name (channel: $channel, source: $source)" >&2
            else
                # Check if this channel is already included for this operator
                if [[ "$operator_channels" != *"|$name:$channel|"* ]]; then
                    operator_channels+="|$name:$channel|"
                    log_step "Added additional channel for operator: $name (channel: $channel)" >&2
                fi
            fi
        done < <(extract_operators "$input_file")
    done
    
    # Convert back to the expected format, handling multiple channels per operator
    local operators=()
    
    # Split operator names and build final operator list
    IFS='|' read -ra names_array <<< "$operator_names"
    for name in "${names_array[@]}"; do
        [[ -z "$name" ]] && continue
        
        # Get all channels for this operator
        local channels_for_operator=""
        while IFS= read -r channel_entry; do
            if [[ "$channel_entry" =~ ^$name:(.+)$ ]]; then
                local channel="${BASH_REMATCH[1]}"
                if [[ -z "$channels_for_operator" ]]; then
                    channels_for_operator="$channel"
                else
                    channels_for_operator="$channels_for_operator,$channel"
                fi
            fi
        done <<< "$(echo "$operator_channels" | tr '|' '\n')"
        
        # Get source and source_namespace (use first one found)
        local source=""
        local source_namespace=""
        while IFS= read -r source_entry; do
            if [[ "$source_entry" =~ ^$name:(.+)$ ]]; then
                source="${BASH_REMATCH[1]}"
                break
            fi
        done <<< "$(echo "$operator_sources" | tr '|' '\n')"
        
        while IFS= read -r source_ns_entry; do
            if [[ "$source_ns_entry" =~ ^$name:(.+)$ ]]; then
                source_namespace="${BASH_REMATCH[1]}"
                break
            fi
        done <<< "$(echo "$operator_source_namespaces" | tr '|' '\n')"
        
        # Add to operators array
        operators+=("$name|$channels_for_operator|$source|$source_namespace")
    done
    
    printf '%s\n' "${operators[@]}"
}

# Extract and log operators (with deduplication) - single file version
extract_and_log_operators() {
    local values_file="$1"
    local include_acm="$2"  # New parameter to control ACM inclusion
    local operators=()
    local seen_operators=""
    
    # Include ACM first only if requested (when processing operators)
    if [[ "$include_acm" == "true" ]]; then
        local acm_info
        acm_info=$(extract_acm_operator "$values_file")
        if [[ -n "$acm_info" ]]; then
            operators+=("$acm_info")
            IFS='|' read -r name channel source source_namespace <<< "$acm_info"
            seen_operators+="$name|$channel "
        fi
    fi
    
    # Then add other enabled operators
    while IFS= read -r operator_info; do
        [[ -z "$operator_info" ]] && continue
        
        IFS='|' read -r name channel source source_namespace <<< "$operator_info"
        local operator_key="$name|$channel"
        
        # Skip if we've already seen this operator with same channel (including ACM)
        if [[ " $seen_operators " == *" $operator_key "* ]]; then
            continue
        fi
        
        operators+=("$operator_info")
        seen_operators+="$operator_key "
        
        log_step "Found enabled operator: $name (channel: $channel, source: $source)" >&2
    done < <(extract_operators "$values_file")
    
    printf '%s\n' "${operators[@]}"
}

# Generate ImageSetConfiguration YAML
generate_imageset_config() {
    local output_file="$1"
    shift
    local values_files_array=("$@")
    
    # Create name from files
    local config_name=""
    local values_files_label=""
    for values_file in "${values_files_array[@]}"; do
        values_file=$(echo "$values_file" | xargs)
        base_name=$(basename "$values_file" .yaml)
        base_name=${base_name#values.}  # Remove 'values.' prefix
        if [[ -z "$config_name" ]]; then
            config_name="$base_name"
            values_files_label="$values_file"
        else
            config_name="$config_name-$base_name"
            values_files_label="$values_files_label,$values_file"
        fi
    done
    
    log_step "Generating ImageSetConfiguration from ${#values_files_array[@]} file(s): ${values_files_label}..."

    # Start YAML file
    cat > "$output_file" << EOF
# AutoShift Generated ImageSetConfiguration
# Values files: $values_files_label
# Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
apiVersion: mirror.openshift.io/v2alpha1
kind: ImageSetConfiguration
archiveSize: 4
mirror:
  platform:
EOF

    # Add OpenShift platform if requested
    if [[ "$INCLUDE_OPENSHIFT" == "true" ]]; then
        # Parse OpenShift version to get channel name and min/max versions
        local version_info
        version_info=$(parse_openshift_version "$OPENSHIFT_VERSION" "$MIN_VERSION" "$MAX_VERSION")
        IFS='|' read -r channel_name min_version max_version <<< "$version_info"
        
        cat >> "$output_file" << EOF
    channels:
    - name: $channel_name
      minVersion: $min_version
      maxVersion: $max_version
    graph: true
EOF
        if [[ -n "$MIN_VERSION" || -n "$MAX_VERSION" ]]; then
            log_step "Added OpenShift platform: $channel_name (min: $min_version, max: $max_version) [custom range]"
        else
            log_step "Added OpenShift platform: $channel_name (min: $min_version, max: $max_version)"
        fi
    else
        echo "    channels: []" >> "$output_file"
        log_step "Skipped OpenShift platform (operators-only mode)"
    fi

    # Add operators if requested
    if [[ "$INCLUDE_OPERATORS" == "true" ]]; then
        echo "  operators:" >> "$output_file"

        # Extract operators from multiple values files (include ACM when processing operators)
        local operators=()
        if [[ ${#values_files_array[@]} -eq 1 ]]; then
            # Single file: use original function
            # Resolve the file path (same logic as elsewhere in the script)
            local single_file="${values_files_array[0]}"
            single_file=$(echo "$single_file" | xargs)
            if [[ "$single_file" == /* ]]; then
                : # absolute path, use as-is
            elif [[ -f "$single_file" ]]; then
                : # file exists at relative path, use as-is
            elif [[ -f "autoshift/$single_file" ]]; then
                single_file="autoshift/$single_file"
            fi
            while IFS= read -r line; do
                operators+=("$line")
            done < <(extract_and_log_operators "$single_file" "true")
        else
            # Multiple files: use new multi-file function
            while IFS= read -r line; do
                operators+=("$line")
            done < <(extract_and_log_operators_multi "${values_files_array[@]}")
        fi

        if [[ -e "$EXTRACT_ERRORS_FILE" ]]; then
            rm -f "$output_file"
            exit 1
        fi

        # Automatically resolve dependencies for all operators
        if [[ ${#operators[@]} -gt 0 && -z "$DEPENDENCIES_FILE" ]]; then
            # Collect all operator package names
            local all_operator_names=""
            for operator_info in "${operators[@]}"; do
                IFS='|' read -r name channels source source_namespace <<< "$operator_info"
                if [[ -z "$all_operator_names" ]]; then
                    all_operator_names="$name"
                else
                    all_operator_names="$all_operator_names,$name"
                fi
            done
            # Resolve dependencies (this populates DEPENDENCIES_FILE)
            resolve_all_dependencies "$all_operator_names" "$(get_catalog_version)" || { rm -f "$output_file"; exit 1; }
        fi

        if [[ ${#operators[@]} -eq 0 ]]; then
            echo "  - catalog: registry.redhat.io/redhat/redhat-operator-index:v$OPENSHIFT_VERSION" >> "$output_file"
            echo "    packages: []" >> "$output_file"
            log_warning "No enabled operators found in values files"
        else
            # Group operators by catalog source with channel merging support
            local redhat_operators=""
            local community_operators=""
            local certified_operators=""
            
            for operator_info in "${operators[@]}"; do
                IFS='|' read -r name channels source source_namespace <<< "$operator_info"

                # Strip whitespace and carriage returns from source
                source=$(echo "$source" | tr -d '\r' | xargs)

                # Group by source, preserving all channels
                case "$source" in
                    redhat-operators)
                        redhat_operators+="$name|$channels "
                        ;;
                    community-operators)
                        community_operators+="$name|$channels "
                        ;;
                    certified-operators)
                        certified_operators+="$name|$channels "
                        ;;
                    *)
                        redhat_operators+="$name|$channels "
                        log_warning "Unknown operator source '$source' for $name, using redhat-operators catalog"
                        ;;
                esac
            done

            # Expand operators with dependencies if dependencies file is provided
            if [[ -n "$DEPENDENCIES_FILE" && -f "$DEPENDENCIES_FILE" ]]; then
                log_step "Loading operator dependencies from $DEPENDENCIES_FILE"
                local added_deps=0

                # Get list of existing operators (to avoid duplicates)
                local existing_operators=""
                for package_info in $redhat_operators; do
                    [[ -z "$package_info" ]] && continue
                    IFS='|' read -r name channels <<< "$package_info"
                    existing_operators+=" $name "
                done

                # The dependencies JSON contains ALL transitive dependencies as keys
                # Add all packages from the JSON that aren't already in our list
                local all_dep_packages
                all_dep_packages=$(jq -r 'keys[]' "$DEPENDENCIES_FILE" 2>/dev/null)
                local catalog_ver=$(get_catalog_version)

                for dep in $all_dep_packages; do
                    [[ -z "$dep" ]] && continue
                    # Only add if not already in list
                    if [[ ! "$existing_operators" =~ \ $dep\  ]]; then
                        # Find which operator required this dependency (for logging)
                        local required_by
                        required_by=$(jq -r --arg dep "$dep" 'to_entries[] | select(.value | index($dep)) | .key' "$DEPENDENCIES_FILE" 2>/dev/null | head -1)
                        [[ -z "$required_by" ]] && required_by="transitive"

                        # Look up the actual default channel from catalog
                        local dep_channel
                        dep_channel=$(get_default_channel "$dep" "$catalog_ver")
                        [[ -z "$dep_channel" ]] && dep_channel="stable"

                        # Check if this dependency is already in certified or community operators
                        # to avoid adding it to the wrong catalog
                        if [[ "$certified_operators" =~ "$dep|" ]]; then
                            log_step "Skipped dependency $dep — already in certified catalog"
                        elif [[ "$community_operators" =~ "$dep|" ]]; then
                            log_step "Skipped dependency $dep — already in community catalog"
                        else
                            redhat_operators+="$dep|$dep_channel "
                            log_step "Added dependency: $dep (required by $required_by, channel: $dep_channel)"
                        fi
                        existing_operators+=" $dep "
                        added_deps=$((added_deps + 1))
                    fi
                done

                if [[ $added_deps -gt 0 ]]; then
                    log_success "Added $added_deps operator dependencies"
                fi
            fi

            # Generate redhat-operators catalog if we have operators
            if [[ -n "$redhat_operators" ]]; then
                local catalog_ver=$(get_catalog_version)
                validate_catalog_channels "$redhat_operators" "registry.redhat.io/redhat/redhat-operator-index:v${catalog_ver}" || { rm -f "$output_file"; exit 1; }
                if [[ -n "${VALID_PACKAGES// /}" ]]; then
                    echo "  - catalog: registry.redhat.io/redhat/redhat-operator-index:v${catalog_ver}" >> "$output_file"
                    echo "    packages:" >> "$output_file"

                    for package_info in $VALID_PACKAGES; do
                        [[ -z "$package_info" ]] && continue
                        IFS='|' read -r name channels <<< "$package_info"
                        echo "    - name: $name" >> "$output_file"

                        # Set defaultChannel to the first specified channel
                        IFS=',' read -ra channels_array <<< "$channels"
                        local first_channel=$(echo "${channels_array[0]}" | xargs)
                        if [[ -n "$first_channel" ]]; then
                            echo "      defaultChannel: $first_channel" >> "$output_file"
                        fi

                        echo "      channels:" >> "$output_file"
                        for channel in "${channels_array[@]}"; do
                            channel=$(echo "$channel" | xargs)
                            [[ -z "$channel" ]] && continue
                            echo "      - name: $channel" >> "$output_file"
                        done
                    done
                fi
                log_step "Added catalog: redhat-operator-index"
            fi
            
            # Generate community-operators catalog if we have operators
            if [[ -n "$community_operators" ]]; then
                local catalog_ver=$(get_catalog_version)
                check_other_catalog "$community_operators" "registry.redhat.io/redhat/community-operator-index:v${catalog_ver}" || { rm -f "$output_file"; exit 1; }
                if [[ -n "${VALID_PACKAGES// /}" ]]; then
                    echo "  - catalog: registry.redhat.io/redhat/community-operator-index:v${catalog_ver}" >> "$output_file"
                    echo "    packages:" >> "$output_file"

                    for package_info in $VALID_PACKAGES; do
                        [[ -z "$package_info" ]] && continue
                        IFS='|' read -r name channels <<< "$package_info"
                        echo "    - name: $name" >> "$output_file"

                        # Set defaultChannel to the first specified channel
                        IFS=',' read -ra channels_array <<< "$channels"
                        local first_channel=$(echo "${channels_array[0]}" | xargs)
                        if [[ -n "$first_channel" ]]; then
                            echo "      defaultChannel: $first_channel" >> "$output_file"
                        fi

                        echo "      channels:" >> "$output_file"
                        for channel in "${channels_array[@]}"; do
                            channel=$(echo "$channel" | xargs)
                            [[ -z "$channel" ]] && continue
                            echo "      - name: $channel" >> "$output_file"
                        done
                    done
                fi
                log_step "Added catalog: community-operator-index"
            fi
            
            # Generate certified-operators catalog if we have operators
            if [[ -n "$certified_operators" ]]; then
                local catalog_ver=$(get_catalog_version)
                check_other_catalog "$certified_operators" "registry.redhat.io/redhat/certified-operator-index:v${catalog_ver}" || { rm -f "$output_file"; exit 1; }
                if [[ -n "${VALID_PACKAGES// /}" ]]; then
                    echo "  - catalog: registry.redhat.io/redhat/certified-operator-index:v${catalog_ver}" >> "$output_file"
                    echo "    packages:" >> "$output_file"

                    for package_info in $VALID_PACKAGES; do
                        [[ -z "$package_info" ]] && continue
                        IFS='|' read -r name channels <<< "$package_info"
                        echo "    - name: $name" >> "$output_file"

                        # Set defaultChannel to the first specified channel
                        IFS=',' read -ra channels_array <<< "$channels"
                        local first_channel=$(echo "${channels_array[0]}" | xargs)
                        if [[ -n "$first_channel" ]]; then
                            echo "      defaultChannel: $first_channel" >> "$output_file"
                        fi

                        echo "      channels:" >> "$output_file"
                        for channel in "${channels_array[@]}"; do
                            channel=$(echo "$channel" | xargs)
                            [[ -z "$channel" ]] && continue
                            echo "      - name: $channel" >> "$output_file"
                        done
                    done
                fi
                log_step "Added catalog: certified-operator-index"
            fi
            
            if [[ -n "$SKIPPED_PACKAGES" ]]; then
                echo "  # Declared but not in the catalog for this OpenShift version, so not mirrored: $SKIPPED_PACKAGES" >> "$output_file"
                log_warning "Not mirrored, absent from the catalog: $SKIPPED_PACKAGES"
            fi
            log_success "Added ${#operators[@]} operators to ImageSetConfiguration"
        fi
    else
        echo "  operators: []" >> "$output_file"
        log_step "Skipped operators (openshift-only mode)"
    fi

    # Collect known additional images for enabled operators
    local KNOWN_IMAGES_FILE="$SCRIPT_DIR/known-additional-images.json"
    local known_images=()
    if [[ -f "$KNOWN_IMAGES_FILE" ]]; then
        for op in "${operators[@]}"; do
            local op_name="${op%%|*}"
            # An operator the catalog does not carry was left out, so its runtime images are no use
            [[ " $SKIPPED_PACKAGES" == *" $op_name ("* ]] && continue
            local imgs
            imgs=$(jq -r --arg pkg "$op_name" '.[$pkg] // [] | .[]' "$KNOWN_IMAGES_FILE" 2>/dev/null)
            while IFS= read -r img; do
                [[ -z "$img" ]] && continue
                known_images+=("$img")
            done <<< "$imgs"
        done
    fi

    # Non-operator (Helm chart) components: their images aren't in any OLM catalog, so the operator scan
    # above misses them. Mirror their known images when the component is enabled ('<component>: true' in a
    # clusterset values file). Keyed the same as operators in known-additional-images.json.
    if [[ -f "$KNOWN_IMAGES_FILE" ]]; then
        local CHART_COMPONENTS=("vault")
        for comp in "${CHART_COMPONENTS[@]}"; do
            local comp_enabled="$ALL_OPERATORS"
            for values_file in "$PROJECT_ROOT"/autoshift/values/clustersets/*.yaml; do
                [[ -f "$values_file" ]] || continue
                [[ "$(basename "$values_file")" == _* ]] && continue
                if grep -qE "^[[:space:]]*${comp}:[[:space:]]*['\"]?true['\"]?[[:space:]]*\$" "$values_file" 2>/dev/null; then
                    comp_enabled=true; break
                fi
            done
            [[ "$comp_enabled" != true ]] && continue
            local comp_imgs
            comp_imgs=$(jq -r --arg pkg "$comp" '.[$pkg] // [] | .[]' "$KNOWN_IMAGES_FILE" 2>/dev/null)
            while IFS= read -r img; do
                [[ -z "$img" ]] && continue
                known_images+=("$img")
            done <<< "$comp_imgs"
        done
    fi

    # Add AutoShift OCI Helm charts to additionalImages if requested
    # OCI Helm charts are stored as OCI artifacts, so they must be mirrored via additionalImages, not helm section
    if [[ "$INCLUDE_AUTOSHIFT_CHARTS" == "true" ]]; then
        # Set defaults if not specified
        [[ -z "$AUTOSHIFT_REGISTRY" ]] && AUTOSHIFT_REGISTRY="quay.io/autoshift"
        # Strip oci:// prefix if present for additionalImages format
        AUTOSHIFT_REGISTRY="${AUTOSHIFT_REGISTRY#oci://}"

        # Get version from Chart.yaml if not specified
        if [[ -z "$AUTOSHIFT_VERSION" ]]; then
            if [[ -f "autoshift/Chart.yaml" ]]; then
                AUTOSHIFT_VERSION=$(grep "^version:" autoshift/Chart.yaml | sed 's/version:[[:space:]]*//')
            fi
        fi

        if [[ -z "$AUTOSHIFT_VERSION" ]]; then
            log_warning "AutoShift version not specified and could not be determined from Chart.yaml"
            if [[ ${#known_images[@]} -gt 0 ]]; then
                echo "  additionalImages:" >> "$output_file"
            else
                echo "  additionalImages: []" >> "$output_file"
            fi
        else
            log_step "Adding AutoShift OCI Helm charts from $AUTOSHIFT_REGISTRY (version: $AUTOSHIFT_VERSION)"

            # Helm charts (Chart.yaml) and PolicyGenerator dirs (policy-generator-config.yaml) both
            # publish to <registry>/policies/<name>, so mirror both. Chart.yaml-only misses the PG policies.
            local policy_charts=()
            for chart_dir in policies/stable/*/ policies/certified/*/ policies/community/*/; do
                if [[ -f "${chart_dir}Chart.yaml" || -f "${chart_dir}policy-generator-config.yaml" ]]; then
                    policy_charts+=($(basename "$chart_dir"))
                fi
            done

            # Generate additionalImages section with OCI Helm charts
            echo "  additionalImages:" >> "$output_file"
            echo "    # AutoShift OCI Helm Charts" >> "$output_file"
            echo "    # Main chart" >> "$output_file"
            echo "    - name: $AUTOSHIFT_REGISTRY/autoshift:$AUTOSHIFT_VERSION" >> "$output_file"

            echo "    # Bootstrap charts" >> "$output_file"
            echo "    - name: $AUTOSHIFT_REGISTRY/bootstrap/openshift-gitops:$AUTOSHIFT_VERSION" >> "$output_file"
            echo "    - name: $AUTOSHIFT_REGISTRY/bootstrap/advanced-cluster-management:$AUTOSHIFT_VERSION" >> "$output_file"

            echo "    # Policies (Helm charts + PolicyGenerator OCI artifacts)" >> "$output_file"
            for chart_name in "${policy_charts[@]}"; do
                echo "    - name: $AUTOSHIFT_REGISTRY/policies/$chart_name:$AUTOSHIFT_VERSION" >> "$output_file"
            done

            # The autoshift-console policy runs this image tagged for the cluster's OpenShift minor.
            echo "    # AutoShift console plugin" >> "$output_file"
            echo "    - name: $AUTOSHIFT_REGISTRY/autoshift-console-plugin:ocp$(get_catalog_version)" >> "$output_file"

            log_success "Added ${#policy_charts[@]} policies + 3 core charts + console plugin to additionalImages"
        fi
    else
        if [[ ${#known_images[@]} -gt 0 ]]; then
            echo "  additionalImages:" >> "$output_file"
        else
            echo "  additionalImages: []" >> "$output_file"
        fi
    fi

    # Append known additional images for enabled operators
    if [[ ${#known_images[@]} -gt 0 ]]; then
        echo "    # Operator runtime images not declared in catalog relatedImages" >> "$output_file"
        for img in "${known_images[@]}"; do
            echo "    - name: $img" >> "$output_file"
        done
        log_success "Added ${#known_images[@]} known additional images"
    fi

    # helm section not used - OCI charts go in additionalImages
    echo "  helm: {}" >> "$output_file"

    log_success "Generated ImageSetConfiguration: $output_file"
}

# Show usage instructions
show_usage_instructions() {
    local output_file="$1"
    
    echo ""
    echo -e "${BLUE}📋 Usage Instructions:${NC}"
    echo ""
    echo "1. Review the generated configuration:"
    echo -e "   ${YELLOW}cat $output_file${NC}"
    echo ""
    echo "2. Mirror images to disk (recommended for air-gapped):"
    echo -e "   ${YELLOW}oc-mirror -c $output_file file://mirror --v2${NC}"
    echo ""
    echo "3. Or mirror directly to registry:"
    echo -e "   ${YELLOW}oc-mirror -c $output_file docker://your-registry.example.com/mirror --v2${NC}"
    echo ""
    echo "4. Apply mirrored content to disconnected cluster:"
    echo -e "   ${YELLOW}oc apply -f mirror/working-dir/cluster-resources/cs-*.yaml${NC}"
    echo -e "   ${YELLOW}oc apply -f mirror/working-dir/cluster-resources/idms-oc-mirror.yaml${NC}"
    echo -e "   ${YELLOW}oc apply -f mirror/working-dir/cluster-resources/itms-oc-mirror.yaml${NC}"
    echo ""
    echo "5. Deploy AutoShift with mirrored content:"
    if [[ ${#VALUES_FILES_ARRAY[@]} -eq 1 ]]; then
        echo -e "   ${YELLOW}helm upgrade --install autoshift autoshift/ -f ${VALUES_FILES_ARRAY[0]}${NC}"
    else
        echo -e "   ${YELLOW}helm upgrade --install autoshift autoshift/ -f ${VALUES_FILES_ARRAY[0]} # (primary values file)${NC}"
    fi
    echo ""
    echo -e "${BLUE}📖 For more information, see: https://docs.openshift.com/container-platform/latest/installing/disconnected_install/installing-mirroring-disconnected.html${NC}"
}

# Main execution
main() {
    # Build dynamic operator mappings from subscription-name entries
    build_operator_mappings

    if [[ ${#VALUES_FILES_ARRAY[@]} -eq 1 ]]; then
        echo -e "${GREEN}🚀 Generating ImageSetConfiguration for AutoShift ${VALUES_FILES_ARRAY[0]} environment...${NC}"
    else
        echo -e "${GREEN}🚀 Generating ImageSetConfiguration for AutoShift multi-file environment (${#VALUES_FILES_ARRAY[@]} files)...${NC}"
    fi
    echo ""

    generate_imageset_config "$OUTPUT_FILE" "${VALUES_FILES_ARRAY[@]}"
    
    echo ""
    echo -e "${GREEN}🎉 ImageSetConfiguration generation completed!${NC}"
    echo ""
    echo "📄 Generated file: $OUTPUT_FILE"
    if [[ ${#VALUES_FILES_ARRAY[@]} -eq 1 ]]; then
        echo "📊 Source values file: ${VALUES_FILES_ARRAY[0]}"
    else
        echo "📊 Source values files (${#VALUES_FILES_ARRAY[@]}): ${VALUES_FILES_ARRAY[0]}"
        for ((i=1; i<${#VALUES_FILES_ARRAY[@]}; i++)); do
            echo "                           ${VALUES_FILES_ARRAY[i]}"
        done
    fi
    echo "🏗️  OpenShift version: $OPENSHIFT_VERSION"
    echo "🔧 Include OpenShift: $INCLUDE_OPENSHIFT"
    echo "📦 Include Operators: $INCLUDE_OPERATORS"
    
    show_usage_instructions "$OUTPUT_FILE"
}

# Run main function
main "$@"