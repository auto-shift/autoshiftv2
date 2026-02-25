#!/bin/bash
# Get operator dependencies from the operator catalog index
# Usage: get-operator-dependencies.sh [--catalog CATALOG] [--operators PKG1,PKG2,...] [--all]
#
# This script extracts the operator index image and parses the catalog
# to find olm.package.required dependencies for operators.
#
# Requirements:
#   - oc CLI (for oc image extract)
#   - jq (for JSON parsing)
#   - Pull secret configured (~/.docker/config.json or REGISTRY_AUTH_FILE)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors (disabled if not a terminal)
if [[ -t 2 ]]; then
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

log() { echo -e "${BLUE}[INFO]${NC} $1" >&2; }
success() { echo -e "${GREEN}[OK]${NC} $1" >&2; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1" >&2; }
error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }

# Defaults
CATALOG="registry.redhat.io/redhat/redhat-operator-index:v4.18"
OPERATORS=""
SHOW_ALL=false
OUTPUT_FORMAT="text"
CACHE_DIR="$PROJECT_ROOT/.cache/catalog-cache"
RECURSIVE=true
KNOWN_DEPS_FILE="$SCRIPT_DIR/known-dependencies.json"

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Extract operator dependencies from an operator catalog index.

This script requires:
  - oc CLI installed
  - jq installed
  - Valid pull secret for registry.redhat.io (in ~/.docker/config.json or REGISTRY_AUTH_FILE)

Options:
  --catalog CATALOG      Catalog image (default: $CATALOG)
  --operators PKG1,PKG2  Comma-separated list of operators to check
  --all                  Show all operators with dependencies
  --no-recursive         Disable recursive resolution (recursive is default)
  --json                 Output in JSON format
  --cache-dir DIR        Directory to cache extracted catalog (default: $CACHE_DIR)
  --help                 Show this help

The script also reads known-dependencies.json for dependencies not declared
in the catalog (e.g., odf-operator -> odf-dependencies).

Examples:
  $0 --operators odf-operator --recursive --json
  $0 --operators devspaces,odf-operator
  $0 --all --json
  $0 --catalog registry.redhat.io/redhat/redhat-operator-index:v4.17 --all

Environment Variables:
  REGISTRY_AUTH_FILE     Path to pull secret file (alternative to ~/.docker/config.json)

EOF
}

# Check dependencies
check_dependencies() {
    local missing=()

    if ! command -v oc &> /dev/null; then
        missing+=("oc")
    fi

    if ! command -v jq &> /dev/null; then
        missing+=("jq")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        error "Missing required tools: ${missing[*]}"
        echo "Install them and try again." >&2
        exit 1
    fi
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --catalog)
            CATALOG="$2"
            shift 2
            ;;
        --operators)
            OPERATORS="$2"
            shift 2
            ;;
        --all)
            SHOW_ALL=true
            shift
            ;;
        --no-recursive)
            RECURSIVE=false
            shift
            ;;
        --json)
            OUTPUT_FORMAT="json"
            shift
            ;;
        --cache-dir)
            CACHE_DIR="$2"
            shift 2
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Validate
if [[ "$SHOW_ALL" == "false" && -z "$OPERATORS" ]]; then
    error "Either --operators or --all is required"
    usage
    exit 1
fi

# Check dependencies
check_dependencies

# Create cache directory
mkdir -p "$CACHE_DIR"

# Generate cache key from catalog image
if command -v md5sum &> /dev/null; then
    CACHE_KEY=$(echo "$CATALOG" | md5sum | cut -d' ' -f1)
elif command -v md5 &> /dev/null; then
    CACHE_KEY=$(echo "$CATALOG" | md5)
else
    # Fallback: simple hash
    CACHE_KEY=$(echo "$CATALOG" | cksum | cut -d' ' -f1)
fi
CATALOG_DIR="$CACHE_DIR/$CACHE_KEY"

# Extract catalog if not cached
if [[ ! -d "$CATALOG_DIR/configs" ]]; then
    log "Extracting catalog from $CATALOG..."
    mkdir -p "$CATALOG_DIR"

    # Extract full image and access configs directory
    # Note: extracting /configs directly doesn't work reliably, so we extract root
    # Use --filter-by-os to handle multi-arch manifests (catalog images are linux/amd64 only)
    if ! oc image extract "$CATALOG" --path /:"$CATALOG_DIR" --confirm --filter-by-os=linux/amd64 2>/dev/null; then
        error "Failed to extract catalog. Check:"
        error "  - Pull secret is configured (~/.docker/config.json or REGISTRY_AUTH_FILE)"
        error "  - Registry access to $CATALOG"
        error "  - oc CLI is authenticated"
        exit 1
    fi

    if [[ ! -d "$CATALOG_DIR/configs" ]]; then
        error "Extracted image does not contain /configs directory"
        exit 1
    fi

    log "Catalog extracted to $CATALOG_DIR ($(ls "$CATALOG_DIR/configs" | wc -l | tr -d ' ') packages)"
else
    log "Using cached catalog from $CATALOG_DIR"
fi

# Function to get dependencies for a package from catalog
get_catalog_deps() {
    local pkg="$1"
    local pkg_dir="$CATALOG_DIR/configs/$pkg"

    if [[ ! -d "$pkg_dir" ]]; then
        return
    fi

    local deps=""

    # Check bundles directory for the latest bundle
    if [[ -d "$pkg_dir/bundles" ]]; then
        local latest_bundle=$(ls "$pkg_dir/bundles/"*.json 2>/dev/null | sort -V | tail -1)
        if [[ -n "$latest_bundle" && -f "$latest_bundle" ]]; then
            deps=$(jq -r '.properties[]? | select(.type == "olm.package.required") | .value.packageName' "$latest_bundle" 2>/dev/null | sort -u | tr '\n' ',' | sed 's/,$//')
        fi
    fi

    # Fallback to old catalog.json format
    if [[ -z "$deps" && -f "$pkg_dir/catalog.json" ]]; then
        deps=$(jq -r '.properties[]? | select(.type == "olm.package.required") | .value.packageName' "$pkg_dir/catalog.json" 2>/dev/null | sort -u | tr '\n' ',' | sed 's/,$//')
    fi

    echo "$deps"
}

# Function to get known dependencies from JSON file
get_known_deps() {
    local pkg="$1"

    if [[ -f "$KNOWN_DEPS_FILE" ]]; then
        local deps=$(jq -r --arg pkg "$pkg" '.[$pkg] // [] | join(",")' "$KNOWN_DEPS_FILE" 2>/dev/null)
        echo "$deps"
    fi
}

# Function to merge and dedupe dependencies
merge_deps() {
    local deps1="$1"
    local deps2="$2"

    local all_deps=""
    [[ -n "$deps1" ]] && all_deps="$deps1"
    [[ -n "$deps2" ]] && all_deps="${all_deps:+$all_deps,}$deps2"

    # Dedupe
    echo "$all_deps" | tr ',' '\n' | sort -u | tr '\n' ',' | sed 's/,$//'
}

# Function to get all dependencies (catalog + known)
get_deps() {
    local pkg="$1"

    local catalog_deps=$(get_catalog_deps "$pkg")
    local known_deps=$(get_known_deps "$pkg")

    merge_deps "$catalog_deps" "$known_deps"
}

# Collect results using temp file (avoid associative arrays for bash 3.x compatibility)
RESULTS_FILE=$(mktemp)
VISITED_FILE=$(mktemp)
trap "rm -f $RESULTS_FILE $VISITED_FILE" EXIT

# Function to recursively resolve dependencies
resolve_recursive() {
    local pkg="$1"

    # Skip if already visited
    if grep -q "^${pkg}$" "$VISITED_FILE" 2>/dev/null; then
        return
    fi
    echo "$pkg" >> "$VISITED_FILE"

    local deps=$(get_deps "$pkg")

    if [[ -n "$deps" ]]; then
        echo "$pkg|$deps" >> "$RESULTS_FILE"

        # Recurse into each dependency
        if [[ "$RECURSIVE" == "true" ]]; then
            IFS=',' read -ra DEP_ARRAY <<< "$deps"
            for dep in "${DEP_ARRAY[@]}"; do
                dep=$(echo "$dep" | xargs)  # trim whitespace
                resolve_recursive "$dep"
            done
        fi
    else
        echo "$pkg|" >> "$RESULTS_FILE"
    fi
}

if [[ "$SHOW_ALL" == "true" ]]; then
    # Check all operators
    for dir in "$CATALOG_DIR/configs"/*/; do
        pkg=$(basename "$dir")
        deps=$(get_deps "$pkg")
        if [[ -n "$deps" ]]; then
            echo "$pkg|$deps" >> "$RESULTS_FILE"
        fi
    done
else
    # Check specific operators (with recursive resolution)
    IFS=',' read -ra PKGS <<< "$OPERATORS"
    for pkg in "${PKGS[@]}"; do
        pkg=$(echo "$pkg" | xargs)  # trim whitespace
        resolve_recursive "$pkg"
    done
fi

# Output results
if [[ "$OUTPUT_FORMAT" == "json" ]]; then
    # Build JSON object
    echo "{"
    # Use awk to handle comma placement properly
    sort "$RESULTS_FILE" | awk -F'|' '
        NR > 1 { printf ",\n" }
        {
            pkg = $1
            deps = $2
            if (deps == "") {
                printf "  \"%s\": []", pkg
            } else {
                # Convert comma-separated deps to JSON array
                n = split(deps, arr, ",")
                printf "  \"%s\": [", pkg
                for (i = 1; i <= n; i++) {
                    if (i > 1) printf ", "
                    printf "\"%s\"", arr[i]
                }
                printf "]"
            }
        }
        END { printf "\n" }
    '
    echo "}"
else
    # Text format
    sort "$RESULTS_FILE" | while IFS='|' read -r pkg deps; do
        if [[ -n "$deps" ]]; then
            echo "$pkg: $deps"
        else
            echo "$pkg: (no dependencies)"
        fi
    done
fi
