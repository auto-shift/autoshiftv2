#!/bin/bash
# AutoShift ImageSetConfiguration Generator for oc-mirror
# Generates ImageSetConfiguration YAML from AutoShift values files for disconnected environments
#
# Detects and includes 14 standard AutoShift operators:
# - gitops (openshift-gitops-operator)
# - acm (advanced-cluster-management)  
# - metallb (metallb-operator)
# - odf (odf-operator)
# - acs (rhacs-operator)
# - dev-spaces (devspaces)
# - dev-hub (rhdh)
# - pipelines (openshift-pipelines-operator-rh)
# - tas (trusted-artifact-signer-operator)
# - quay (quay-operator)
# - loki (loki-operator)
# - logging (cluster-logging)
# - coo (cluster-observability-operator)
# - compliance (compliance-operator)
#
# Usage: ./scripts/generate-imageset-config.sh <values-files> [options]
# Example: ./scripts/generate-imageset-config.sh values.hub.yaml
# Example: ./scripts/generate-imageset-config.sh values.hub.yaml,values.sbx.yaml --openshift-version 4.18
# Example: ./scripts/generate-imageset-config.sh values.hub.yaml --openshift-version 4.18.22
# Example: ./scripts/generate-imageset-config.sh values.hub.yaml --openshift-version 4.18 --min-version 4.18.15 --max-version 4.18.25
# Example: ./scripts/generate-imageset-config.sh values.hub.baremetal-sno.yaml --operators-only

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
VALUES_FILES=""
VALUES_FILES_ARRAY=()
OPENSHIFT_VERSION=""  # Will be read from values file or command line
MIN_VERSION=""
MAX_VERSION=""
OUTPUT_FILE=""
INCLUDE_OPENSHIFT=true
INCLUDE_OPERATORS=true

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
        local major_minor=$(echo "$version" | cut -d. -f1-2)
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
    echo "Automatically detects up to 14 standard operators based on enabled flags."
    echo "Supports multiple values files with channel merging for operators with different channels."
    echo ""
    echo "Arguments:"
    echo "  values-files          AutoShift values file(s) to process. Can be:"
    echo "                        - Single file: values.hub.yaml"
    echo "                        - Multiple files: values.hub.yaml,values.sbx.yaml"
    echo "                        - Available files: values.hub.yaml, values.sbx.yaml,"
    echo "                          values.hub.baremetal-sno.yaml, values.huhofhubs.yaml"
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
    echo "  --help                         Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 values.hub.yaml"
    echo "  $0 values.hub.yaml,values.sbx.yaml --openshift-version 4.17"
    echo "  $0 values.hub.yaml --openshift-version 4.18.22"
    echo "  $0 values.hub.yaml --openshift-version 4.18 --min-version 4.18.15 --max-version 4.18.25"
    echo "  $0 values.hub.baremetal-sno.yaml --operators-only"
    echo "  $0 values.hub.yaml --output my-imageset.yaml"
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

# Extract OpenShift version from values file
extract_openshift_version() {
    local values_file="$1"
    
    # Extract openshift-version from the values file and clean it
    local version
    version=$(grep -E "^[[:space:]]*openshift-version:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"' | sed 's/[^0-9.]//g')
    
    if [[ -n "$version" ]]; then
        echo "$version"
    else
        echo ""  # No version found
    fi
}

# Parse comma-separated values files
IFS=',' read -ra VALUES_FILES_ARRAY <<< "$VALUES_FILES"

# Validate all input files exist
for values_file in "${VALUES_FILES_ARRAY[@]}"; do
    # Strip leading/trailing whitespace
    values_file=$(echo "$values_file" | xargs)
    
    # Construct full path
    input_file="autoshift/$values_file"
    
    if [[ ! -f "$input_file" ]]; then
        echo -e "${RED}Error: Values file $input_file not found${NC}"
        exit 1
    fi
done

# Read OpenShift version from values file if not specified on command line
if [[ -z "$OPENSHIFT_VERSION" ]]; then
    # Extract from first values file
    first_values_file=$(echo "${VALUES_FILES_ARRAY[0]}" | xargs)
    OPENSHIFT_VERSION=$(extract_openshift_version "autoshift/$first_values_file")
    
    if [[ -z "$OPENSHIFT_VERSION" ]]; then
        echo -e "${RED}Error: No OpenShift version found in values file and none specified with --openshift-version${NC}"
        echo -e "${RED}Please add 'openshift-version: 'X.Y.Z'' to your values file or use --openshift-version flag${NC}"
        exit 1
    else
        log_step "Using OpenShift version from values file: $OPENSHIFT_VERSION"
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
    if [[ ${#VALUES_FILES_ARRAY[@]} -eq 1 ]]; then
        # Single file: extract base name
        base_name=$(basename "${VALUES_FILES_ARRAY[0]}" .yaml)
        base_name=${base_name#values.}  # Remove 'values.' prefix
        OUTPUT_FILE="imageset-config-$base_name.yaml"
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
        OUTPUT_FILE="imageset-config-$combined_name.yaml"
    fi
fi

# Get operator subscription name from labels
get_operator_subscription_name() {
    local label_name="$1"
    local values_file="$2"
    
    # First try to get explicit subscription name from labels
    local subscription_name
    subscription_name=$(grep -E "^[[:space:]]*$label_name-subscription-name:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"')
    
    # If found, return it
    if [[ -n "$subscription_name" ]]; then
        echo "$subscription_name"
        return
    fi
    
    # Skip non-operator labels that don't have subscription names
    case "$label_name" in
        imageregistry|gitops-dev|acm-observability|metallb-quota|self-managed) echo "" ;;
        *) echo "$label_name" ;;  # Fallback to label name if no explicit subscription name
    esac
}

# Extract enabled operators from values file
extract_operators() {
    local values_file="$1"
    local operators=()
    
    # Parse YAML to find enabled operators
    # Look for patterns like: operator-name: 'true' followed by operator-name-channel, operator-name-source
    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ -z "$(echo "$line" | xargs)" ]] && continue
        
        # Look for enabled operator (value: 'true') - handle inline comments
        if echo "$line" | grep -qE "^[[:space:]]*[a-z0-9-]+:[[:space:]]*['\"]?true['\"]?"; then
            local label_name=$(echo "$line" | sed -E 's/^[[:space:]]*([a-z0-9-]+):[[:space:]]*['\''"]?true['\''"]?.*/\1/')
            
            # Skip non-operator labels
            case "$label_name" in
                self-managed|acm-observability|metallb-quota|gitops-dev-team-*|nmstate-nncp-*) continue ;;
                *-numcpu|*-memory-mib|*-numcores-per-socket|*-zone-*|*-region|*-instance-type) continue ;;
                *-provider|*-default|*-fstype|*-size-percent|*-overprovision-ratio) continue ;;
                *-management-state|*-replicas|*-storage-type|*-s3-region|*-access-mode) continue ;;
                *-size|*-storage-class|*-volume-mode|*-rollout-strategy|*-availability-config) continue ;;
                *-multi-cloud-gateway|*-nooba-*|*-ocs-*|*-resource-profile) continue ;;
            esac
            
            # Get operator subscription name from labels
            local operator_name
            operator_name=$(get_operator_subscription_name "$label_name" "$values_file")
            
            # Skip if no operator name found (like imageregistry, gitops-dev)
            [[ -z "$operator_name" ]] && continue
            
            # Extract operator details from entire file (not just next 10 lines)
            local channel source source_namespace install_plan_approval
            channel=$(grep -E "^[[:space:]]*$label_name-channel:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"')
            source=$(grep -E "^[[:space:]]*$label_name-source:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"')
            source_namespace=$(grep -E "^[[:space:]]*$label_name-source-namespace:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"')
            
            # Only add if we have the required fields and avoid duplicates
            if [[ -n "$channel" && -n "$source" && -n "$source_namespace" ]]; then
                echo "$operator_name|$channel|$source|$source_namespace"
            fi
        fi
    done < "$values_file"
}

# Extract ACM configuration (always required)
extract_acm_operator() {
    local values_file="$1"
    
    # Extract ACM configuration - it's always required for AutoShift
    local subscription_name channel source source_namespace
    subscription_name=$(grep -E "^[[:space:]]*acm-subscription-name:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"')
    channel=$(grep -E "^[[:space:]]*acm-channel:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"')
    source=$(grep -E "^[[:space:]]*acm-source:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"')
    source_namespace=$(grep -E "^[[:space:]]*acm-source-namespace:" "$values_file" | head -1 | sed 's/.*:[[:space:]]*//' | tr -d "'" | tr -d '"')
    
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
        input_file="autoshift/$values_file"
        
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
    
    # Start YAML file - v2 format doesn't use apiVersion or metadata
    cat > "$output_file" << EOF
# AutoShift Generated ImageSetConfiguration
# Values files: $values_files_label  
# Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
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
            while IFS= read -r line; do
                operators+=("$line")
            done < <(extract_and_log_operators "autoshift/${values_files_array[0]}" "true")
        else
            # Multiple files: use new multi-file function
            while IFS= read -r line; do
                operators+=("$line")
            done < <(extract_and_log_operators_multi "${values_files_array[@]}")
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
            
            # Generate redhat-operators catalog if we have operators
            if [[ -n "$redhat_operators" ]]; then
                # Extract major.minor from OPENSHIFT_VERSION for catalog versioning
                local catalog_version
                if [[ "$OPENSHIFT_VERSION" =~ ^([0-9]+\.[0-9]+) ]]; then
                    catalog_version="${BASH_REMATCH[1]}"
                else
                    catalog_version="$OPENSHIFT_VERSION"
                fi
                echo "  - catalog: registry.redhat.io/redhat/redhat-operator-index:v$catalog_version" >> "$output_file"
                echo "    packages:" >> "$output_file"
                
                for package_info in $redhat_operators; do
                    [[ -z "$package_info" ]] && continue
                    IFS='|' read -r name channels <<< "$package_info"
                    echo "    - name: $name" >> "$output_file"
                    echo "      channels:" >> "$output_file"
                    
                    # Handle multiple channels (comma-separated)
                    IFS=',' read -ra channels_array <<< "$channels"
                    for channel in "${channels_array[@]}"; do
                        channel=$(echo "$channel" | xargs)  # Strip whitespace
                        echo "      - name: $channel" >> "$output_file"
                    done
                done
                log_step "Added catalog: redhat-operator-index"
            fi
            
            # Generate community-operators catalog if we have operators
            if [[ -n "$community_operators" ]]; then
                # Extract major.minor from OPENSHIFT_VERSION for catalog versioning
                local catalog_version
                if [[ "$OPENSHIFT_VERSION" =~ ^([0-9]+\.[0-9]+) ]]; then
                    catalog_version="${BASH_REMATCH[1]}"
                else
                    catalog_version="$OPENSHIFT_VERSION"
                fi
                echo "  - catalog: registry.redhat.io/redhat/community-operator-index:v$catalog_version" >> "$output_file"
                echo "    packages:" >> "$output_file"
                
                for package_info in $community_operators; do
                    [[ -z "$package_info" ]] && continue
                    IFS='|' read -r name channels <<< "$package_info"
                    echo "    - name: $name" >> "$output_file"
                    echo "      channels:" >> "$output_file"
                    
                    # Handle multiple channels (comma-separated)
                    IFS=',' read -ra channels_array <<< "$channels"
                    for channel in "${channels_array[@]}"; do
                        channel=$(echo "$channel" | xargs)  # Strip whitespace
                        echo "      - name: $channel" >> "$output_file"
                    done
                done
                log_step "Added catalog: community-operator-index"
            fi
            
            # Generate certified-operators catalog if we have operators
            if [[ -n "$certified_operators" ]]; then
                # Extract major.minor from OPENSHIFT_VERSION for catalog versioning
                local catalog_version
                if [[ "$OPENSHIFT_VERSION" =~ ^([0-9]+\.[0-9]+) ]]; then
                    catalog_version="${BASH_REMATCH[1]}"
                else
                    catalog_version="$OPENSHIFT_VERSION"
                fi
                echo "  - catalog: registry.redhat.io/redhat/certified-operator-index:v$catalog_version" >> "$output_file"
                echo "    packages:" >> "$output_file"
                
                for package_info in $certified_operators; do
                    [[ -z "$package_info" ]] && continue
                    IFS='|' read -r name channels <<< "$package_info"
                    echo "    - name: $name" >> "$output_file"
                    echo "      channels:" >> "$output_file"
                    
                    # Handle multiple channels (comma-separated)
                    IFS=',' read -ra channels_array <<< "$channels"
                    for channel in "${channels_array[@]}"; do
                        channel=$(echo "$channel" | xargs)  # Strip whitespace
                        echo "      - name: $channel" >> "$output_file"
                    done
                done
                log_step "Added catalog: certified-operator-index"
            fi
            
            log_success "Added ${#operators[@]} operators to ImageSetConfiguration"
        fi
    else
        echo "  operators: []" >> "$output_file"
        log_step "Skipped operators (openshift-only mode)"
    fi

    # Add additional images section (empty by default)
    cat >> "$output_file" << EOF
  additionalImages: []
  helm: {}
EOF

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
    echo -e "   ${YELLOW}oc apply -f mirror/working-dir/cluster-resources/catalogSource-*.yaml${NC}"
    echo -e "   ${YELLOW}oc apply -f mirror/working-dir/cluster-resources/imageContentSourcePolicy.yaml${NC}"
    echo ""
    echo "5. Deploy AutoShift with mirrored content:"
    if [[ ${#VALUES_FILES_ARRAY[@]} -eq 1 ]]; then
        echo -e "   ${YELLOW}helm upgrade --install autoshift autoshift/ -f autoshift/${VALUES_FILES_ARRAY[0]}${NC}"
    else
        echo -e "   ${YELLOW}helm upgrade --install autoshift autoshift/ -f autoshift/${VALUES_FILES_ARRAY[0]} # (primary values file)${NC}"
    fi
    echo ""
    echo -e "${BLUE}📖 For more information, see: https://docs.openshift.com/container-platform/latest/installing/disconnected_install/installing-mirroring-disconnected.html${NC}"
}

# Main execution
main() {
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
        echo "📊 Source values file: autoshift/${VALUES_FILES_ARRAY[0]}"
    else
        echo "📊 Source values files (${#VALUES_FILES_ARRAY[@]}): autoshift/${VALUES_FILES_ARRAY[0]}"
        for ((i=1; i<${#VALUES_FILES_ARRAY[@]}; i++)); do
            echo "                           autoshift/${VALUES_FILES_ARRAY[i]}"
        done
    fi
    echo "🏗️  OpenShift version: $OPENSHIFT_VERSION"
    echo "🔧 Include OpenShift: $INCLUDE_OPENSHIFT"
    echo "📦 Include Operators: $INCLUDE_OPERATORS"
    
    show_usage_instructions "$OUTPUT_FILE"
}

# Run main function
main "$@"