#!/bin/bash
# AutoShift Operator Policy Generator
# Generates standardized operator installation policies for AutoShift
#
# Usage: ./scripts/generate-operator-policy.sh <component-name> <operator-name> [options]
# Example: ./scripts/generate-operator-policy.sh cert-manager cert-manager
# Example: ./scripts/generate-operator-policy.sh metallb metallb-operator --source community-operators

set -e

# Colors for output (enabled if stdout or stderr is a terminal)
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
DEFAULT_SOURCE="redhat-operators"
DEFAULT_SOURCE_NAMESPACE="openshift-marketplace"
DEFAULT_CHANNEL="stable"
DEFAULT_VERSION=""  # Optional version pinning

# Parse arguments
COMPONENT_NAME=""
SUBSCRIPTION_NAME=""  # Required field
SOURCE="$DEFAULT_SOURCE"
SOURCE_NAMESPACE="$DEFAULT_SOURCE_NAMESPACE"
CHANNEL=""  # Required field (no default)
VERSION="$DEFAULT_VERSION"
TARGET_NAMESPACE=""  # Required field (no default)
NAMESPACE_SCOPED=false
ADD_TO_AUTOSHIFT=false
SHOW_INTEGRATION=false
VALUES_FILES=""  # Empty means all values files

usage() {
    echo "Usage: $0 <component-name> <subscription-name> --channel <channel> --namespace <namespace> [options]"
    echo ""
    echo "Arguments:"
    echo "  component-name     Kebab-case name for the AutoShift policy (e.g., cert-manager)"
    echo "  subscription-name  Operator subscription name (e.g., cert-manager-operator)"
    echo ""
    echo "Required Options:"
    echo "  --channel CHANNEL         Operator channel (e.g., stable, fast, candidate)"
    echo "  --namespace NAMESPACE     Target namespace for operator installation"
    echo ""
    echo "Optional:"
    echo "  --source SOURCE           Operator catalog source (default: $DEFAULT_SOURCE)"
    echo "  --source-namespace NS     Source namespace (default: $DEFAULT_SOURCE_NAMESPACE)"
    echo "  --version VERSION         Pin to specific operator version (CSV name, optional)"
    echo "  --namespace-scoped        Add targetNamespaces for namespace-scoped operators"
    echo "  --add-to-autoshift        Add labels to AutoShift values files (default: all files)"
    echo "  --values-files FILES      Comma-separated list of values files to update; bare name (e.g., 'hub,sbx')
                            looks in autoshift/values/clustersets/; or pass a relative path
                            (e.g., 'autoshift/values/mysite.yaml') for non-standard layouts"
    echo "  --show-integration        Show manual integration instructions"
    echo "  --help                    Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 cert-manager cert-manager-operator --channel stable --namespace cert-manager"
    echo "  $0 metallb metallb-operator --channel stable --namespace metallb-system --source community-operators --version metallb-operator.v0.14.8"
    echo "  $0 compliance compliance-operator --channel stable --namespace openshift-compliance --namespace-scoped"
    echo "  $0 sealed-secrets sealed-secrets-operator --channel stable --namespace sealed-secrets --add-to-autoshift"
    echo "  $0 cert-manager cert-manager-operator --channel stable --namespace cert-manager --version cert-manager.v1.14.4 --add-to-autoshift"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --source)
            SOURCE="$2"
            shift 2
            ;;
        --source-namespace)
            SOURCE_NAMESPACE="$2"
            shift 2
            ;;
        --channel)
            CHANNEL="$2"
            shift 2
            ;;
        --version)
            VERSION="$2"
            shift 2
            ;;
        --namespace)
            TARGET_NAMESPACE="$2"
            shift 2
            ;;
        --namespace-scoped)
            NAMESPACE_SCOPED=true
            shift
            ;;
        --add-to-autoshift)
            ADD_TO_AUTOSHIFT=true
            shift
            ;;
        --values-files)
            VALUES_FILES="$2"
            ADD_TO_AUTOSHIFT=true  # --values-files implies --add-to-autoshift
            shift 2
            ;;
        --show-integration)
            SHOW_INTEGRATION=true
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
            if [[ -z "$COMPONENT_NAME" ]]; then
                COMPONENT_NAME="$1"
            elif [[ -z "$SUBSCRIPTION_NAME" ]]; then
                SUBSCRIPTION_NAME="$1"
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
if [[ -z "$COMPONENT_NAME" || -z "$SUBSCRIPTION_NAME" ]]; then
    echo -e "${RED}Error: Component name and subscription name are required${NC}"
    usage
    exit 1
fi

if [[ -z "$CHANNEL" ]]; then
    echo -e "${RED}Error: Channel is required. Use --channel <channel-name>${NC}"
    usage
    exit 1
fi

if [[ -z "$TARGET_NAMESPACE" ]]; then
    echo -e "${RED}Error: Namespace is required. Use --namespace <namespace-name>${NC}"
    usage
    exit 1
fi

# Validate component name format
if [[ ! "$COMPONENT_NAME" =~ ^[a-z0-9-]+$ ]]; then
    echo -e "${RED}Error: Component name must be lowercase alphanumeric with hyphens only${NC}"
    echo "Examples: cert-manager, metallb, sealed-secrets"
    exit 1
fi

# Set derived values
NAMESPACE="$TARGET_NAMESPACE"  # Use required TARGET_NAMESPACE

# Place policy in subdirectory based on catalog source
case "$SOURCE" in
    certified-operators) POLICY_SUBDIR="policies/certified" ;;
    community-operators) POLICY_SUBDIR="policies/community" ;;
    *)                   POLICY_SUBDIR="policies/stable" ;;
esac
POLICY_DIR="${POLICY_SUBDIR}/${COMPONENT_NAME}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEMPLATE_DIR="$SCRIPT_DIR/templates"

# Convert kebab-case to camelCase for values.yaml
COMPONENT_CAMEL=$(echo "$COMPONENT_NAME" | awk -F'-' '{for(i=1;i<=NF;i++){if(i==1){printf "%s",$i}else{printf "%s%s",toupper(substr($i,1,1)),substr($i,2)}}}')

# Validation checks
if [[ -d "$POLICY_DIR" ]]; then
    echo -e "${RED}Error: Policy directory $POLICY_DIR already exists${NC}"
    echo "Remove it first or choose a different component name"
    exit 1
fi

if [[ ! -d "$TEMPLATE_DIR" ]]; then
    echo -e "${RED}Error: Template directory $TEMPLATE_DIR not found${NC}"
    echo "Run this script from the AutoShift repository root"
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

# Portable in-place sed (works on both macOS and Linux)
sed_inplace() {
    local pattern="$1"
    local file="$2"
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "$pattern" "$file"
    else
        sed -i "$pattern" "$file"
    fi
}

# Template substitution function
substitute_template() {
    local template_file="$1"
    local output_file="$2"
    
    sed -e "s/{{COMPONENT_NAME}}/$COMPONENT_NAME/g" \
        -e "s/{{SUBSCRIPTION_NAME}}/$SUBSCRIPTION_NAME/g" \
        -e "s/{{NAMESPACE}}/$NAMESPACE/g" \
        -e "s/{{SOURCE}}/$SOURCE/g" \
        -e "s/{{SOURCE_NAMESPACE}}/$SOURCE_NAMESPACE/g" \
        -e "s/{{CHANNEL}}/$CHANNEL/g" \
        -e "s/{{VERSION}}/$VERSION/g" \
        -e "s/{{COMPONENT_CAMEL}}/$COMPONENT_CAMEL/g" \
        -e "s/{{LABEL_PREFIX}}/$COMPONENT_NAME/g" \
        "$template_file" > "$output_file"
}

# Validation function
validate_generated_policy() {
    log_step "Validating generated policy..."
    
    # Test helm template rendering
    if ! helm template "$POLICY_DIR" >/dev/null 2>&1; then
        log_error "Generated policy fails helm template validation"
        echo "Run: helm template $POLICY_DIR"
        return 1
    fi
    
    # Check for proper hub escaping
    if ! grep -q '{{ "{{hub" }}' "$POLICY_DIR/templates"/*.yaml; then
        log_warning "No hub functions found - this is unusual for AutoShift policies"
    fi
    
    # Additional YAML syntax check (non-template files only, advisory)
    # helm template above already validates the full chart; this is a secondary check
    if command -v yq >/dev/null 2>&1; then
        for yaml_file in "$POLICY_DIR"/*.yaml; do
            if [[ -f "$yaml_file" ]] && ! yq eval '.' "$yaml_file" >/dev/null 2>&1; then
                log_warning "YAML syntax issue detected in $yaml_file (helm template passed)"
            fi
        done
    fi

    log_success "Policy validation passed"
    return 0
}

# Main generation function
generate_policy() {
    echo -e "${GREEN}🚀 Generating AutoShift policy for $COMPONENT_NAME...${NC}"
    echo ""
    
    # Create directory structure
    log_step "Creating directory structure"
    mkdir -p "$POLICY_DIR/templates"
    log_success "Created $POLICY_DIR/"
    
    # Generate Chart.yaml
    log_step "Generating Chart.yaml"
    substitute_template "$TEMPLATE_DIR/Chart.yaml.template" "$POLICY_DIR/Chart.yaml"
    log_success "Created Chart.yaml"
    
    # Generate values.yaml
    log_step "Generating values.yaml"
    substitute_template "$TEMPLATE_DIR/values.yaml.template" "$POLICY_DIR/values.yaml"
    
    # Enable targetNamespaces if namespace-scoped flag is set
    if [[ "$NAMESPACE_SCOPED" == "true" ]]; then
        # Use | as delimiter to avoid conflicts with / in comments
        sed_inplace "s|  # targetNamespaces: # Optional: specify target namespaces for namespace-scoped operators|  targetNamespaces: # Target namespaces for namespace-scoped operators|" "$POLICY_DIR/values.yaml"
        sed_inplace "s|  #   - $NAMESPACE|    - $NAMESPACE|" "$POLICY_DIR/values.yaml"
    fi
    
    log_success "Created values.yaml"
    
    # Generate operator install policy
    log_step "Generating operator installation policy"
    substitute_template "$TEMPLATE_DIR/policy-operator-install.yaml.template" \
                       "$POLICY_DIR/templates/policy-$COMPONENT_NAME-operator-install.yaml"
    log_success "Created policy-$COMPONENT_NAME-operator-install.yaml"
    
    # Generate README.md
    log_step "Generating README.md with configuration guidance"
    substitute_template "$TEMPLATE_DIR/README.md.template" "$POLICY_DIR/README.md"
    log_success "Created README.md"
    
    echo ""
}

# Show integration instructions
show_integration_instructions() {
    echo -e "${BLUE}📋 AutoShift Integration Instructions:${NC}"
    echo ""
    echo "The policy has been created in policies/stable/$COMPONENT_NAME/."
    echo "The ApplicationSet auto-discovers policies under policies/stable/*, so no"
    echo "manual registration is needed. Commit and push to deploy."
    echo ""
}

# Add labels to AutoShift values files
add_to_autoshift_values() {
    log_step "Adding labels to AutoShift values files..."

    # Determine which values files to update
    local values_files_to_update=()
    if [[ -n "$VALUES_FILES" ]]; then
        # CLI flag: accepts bare names (looked up in clustersets/) or relative paths
        # Examples: 'hub,sbx'  OR  'autoshift/values/mysite.yaml'
        IFS=',' read -ra file_list <<< "$VALUES_FILES"
        for entry in "${file_list[@]}"; do
            entry=$(echo "$entry" | xargs)  # trim whitespace
            local resolved=""
            if [[ "$entry" == *.yaml || "$entry" == */* ]]; then
                # Treat as a path (relative to repo root)
                [[ "$entry" != autoshift/* ]] && entry="autoshift/$entry"
                resolved="$entry"
            else
                # Treat as a bare name under clustersets/
                resolved="autoshift/values/clustersets/$entry.yaml"
            fi
            if [[ -f "$resolved" ]]; then
                values_files_to_update+=("${resolved#autoshift/}")
            else
                log_warning "Values file $resolved not found, skipping"
            fi
        done
    else
        # Interactive: let user select which values files to update
        # Search clustersets/ AND the parent values/ directory for single-file setups
        # Use newline-based find (no -print0/-z) for Git Bash compatibility
        local available_files=()
        while IFS= read -r file; do
            [[ -z "$file" ]] && continue
            # hub-minimal.yaml is intentionally restricted to gitops + acm only
            [[ "$file" == *"hub-minimal.yaml" ]] && continue
            available_files+=("${file#autoshift/}")
        done < <(find autoshift/values -name "*.yaml" -not -name "_*" 2>/dev/null | sort)

        if [[ ${#available_files[@]} -gt 0 ]]; then
            echo ""
            echo -e "${BLUE}Select values files to update (example files are always included):${NC}"
            local idx=1
            for f in "${available_files[@]}"; do
                echo "  $idx) $f"
                idx=$((idx + 1))
            done
            echo "  $idx) All of the above"
            echo ""
            read -rp "Choice (comma-separated, e.g. 1,3) [${idx}]: " files_choice
            files_choice="${files_choice:-$idx}"

            if [[ "$files_choice" -eq "$idx" ]] 2>/dev/null; then
                values_files_to_update=("${available_files[@]}")
            else
                IFS=',' read -ra chosen <<< "$files_choice"
                for c in "${chosen[@]}"; do
                    c=$(echo "$c" | xargs)
                    if [[ "$c" -ge 1 && "$c" -lt "$idx" ]] 2>/dev/null; then
                        values_files_to_update+=("${available_files[$((c - 1))]}")
                    else
                        log_warning "Invalid choice '$c', skipping"
                    fi
                done
            fi
        fi
    fi

    # Always include example files that have a labels: section
    while IFS= read -r file; do
        [[ -z "$file" ]] && continue
        grep -q "^    labels:" "$file" || continue
        values_files_to_update+=("${file#autoshift/}")
    done < <(find autoshift/values -name "_example*.yaml" 2>/dev/null | sort)

    if [[ ${#values_files_to_update[@]} -eq 0 ]]; then
        log_error "No valid values files found to update"
        return 1
    fi

    # Add labels to each values file
    local updated_count=0
    for values_file in "${values_files_to_update[@]}"; do
        local file_path="autoshift/$values_file"

        if [[ ! -f "$file_path" ]]; then
            log_warning "File $file_path not found, skipping"
            continue
        fi

        log_step "Adding labels to $values_file..."
        if add_labels_to_all_sections "$file_path"; then
            log_success "Updated $values_file"
            updated_count=$((updated_count + 1))
        else
            log_warning "No changes made to $values_file"
        fi
    done

    log_success "Labels added to $updated_count of ${#values_files_to_update[@]} values file(s)"
}


# Add labels to all sections in the values file
# Captures all section names BEFORE modifying the file to avoid read/write races
add_labels_to_all_sections() {
    local file_path="$1"
    local sections_found=""

    # Pre-capture all section names before any file modifications
    local hub_clustersets=() managed_clustersets=() active_clusters=() commented_clusters=()

    if grep -q "^hubClusterSets:" "$file_path"; then
        while IFS= read -r cs; do
            [[ -n "$cs" ]] && hub_clustersets+=("$cs")
        done < <(awk '/^hubClusterSets:/{f=1;next} /^[a-zA-Z]/{f=0} f && /^  [a-zA-Z][^:]*:/{gsub(/:.*$/,""); gsub(/^  /,""); print}' "$file_path")
    fi

    if grep -q "^managedClusterSets:" "$file_path"; then
        while IFS= read -r cs; do
            [[ -n "$cs" ]] && managed_clustersets+=("$cs")
        done < <(awk '/^managedClusterSets:/{f=1;next} /^[a-zA-Z]/{f=0} f && /^  [a-zA-Z][^:]*:/{gsub(/:.*$/,""); gsub(/^  /,""); print}' "$file_path")
    fi

    if grep -q "^clusters:" "$file_path"; then
        while IFS= read -r cl; do
            [[ -n "$cl" ]] && active_clusters+=("$cl")
        done < <(awk '/^clusters:/{f=1;next} /^[a-zA-Z]/{f=0} f && /^  [a-zA-Z][^:]*:/{gsub(/:.*$/,""); gsub(/^  /,""); print}' "$file_path")
    fi

    if grep -q "^# clusters:" "$file_path"; then
        while IFS= read -r cl; do
            [[ -n "$cl" ]] && commented_clusters+=("$cl")
        done < <(awk '/^# clusters:/{f=1;next} /^[a-zA-Z]/{f=0} f && /^#   [a-zA-Z][^:]*:/{gsub(/:.*$/,""); gsub(/^#   /,""); print}' "$file_path")
    fi

    # Now apply modifications using pre-captured arrays.
    # Only count sections that were actually updated (add_labels_to_section
    # returns non-zero when it skipped, e.g. labels-line not found or duplicate).
    for cs in "${hub_clustersets[@]}"; do
        if add_labels_to_section "$file_path" "hubClusterSets" "$cs" false; then
            sections_found="$sections_found hubClusterSets/$cs"
        fi
    done
    for cs in "${managed_clustersets[@]}"; do
        if add_labels_to_section "$file_path" "managedClusterSets" "$cs" false; then
            sections_found="$sections_found managedClusterSets/$cs"
        fi
    done
    for cl in "${active_clusters[@]}"; do
        if add_labels_to_section "$file_path" "clusters" "$cl" false; then
            sections_found="$sections_found clusters/$cl"
        fi
    done
    for cl in "${commented_clusters[@]}"; do
        if add_labels_to_section "$file_path" "clusters" "$cl" true; then
            sections_found="$sections_found clusters/$cl(commented)"
        fi
    done

    if [[ -z "$sections_found" ]]; then
        log_warning "No suitable sections found in $(basename "$file_path")"
        return 1
    fi
    log_step "Added $COMPONENT_NAME labels to:$sections_found"
    return 0
}

# Add labels to a specific section/clusterset combination
add_labels_to_section() {
    local file_path="$1"
    local section_type="$2"  # hubClusterSets, managedClusterSets, clusters
    local clusterset="$3"    # hub, managed, nonprod, etc.
    local is_commented="$4"  # true/false
    
    # Check if component already exists in this section
    if check_component_exists "$file_path" "$section_type" "$clusterset" "$is_commented"; then
        log_warning "Labels for $COMPONENT_NAME already exist in $section_type/$clusterset $([ "$is_commented" == "true" ] && echo "(commented)"), skipping"
        return 1
    fi
    
    # Determine comment style: example files use banner, others use ###
    local basename_file
    basename_file=$(basename "$file_path")
    local is_example=false
    if [[ "$basename_file" == _example* ]]; then
        is_example=true
    fi

    # Convert component name to title case for banner header
    local component_title
    component_title=$(echo "$COMPONENT_NAME" | sed 's/-/ /g' | awk '{for(i=1;i<=NF;i++) $i=toupper(substr($i,1,1)) substr($i,2)}1')

    # Create labels with proper indentation and commenting
    local labels_content
    local version_line=""
    if [[ -n "$VERSION" ]]; then
        version_line="      $COMPONENT_NAME-version: '$VERSION'"
        if [[ "$is_commented" == "true" ]]; then
            version_line="#       $COMPONENT_NAME-version: '$VERSION'"
        fi
    fi

    if [[ "$is_commented" == "true" ]]; then
        labels_content="#       ### $COMPONENT_NAME
#       $COMPONENT_NAME: 'true'
#       $COMPONENT_NAME-subscription-name: $SUBSCRIPTION_NAME
#       $COMPONENT_NAME-channel: $CHANNEL
#       $COMPONENT_NAME-source: $SOURCE
#       $COMPONENT_NAME-source-namespace: $SOURCE_NAMESPACE"
        if [[ -n "$version_line" ]]; then
            labels_content="$labels_content
$version_line"
        fi
    elif [[ "$is_example" == "true" ]]; then
        labels_content="
      # =======================================================================
      # $component_title
      # =======================================================================
      $COMPONENT_NAME: 'false'
      $COMPONENT_NAME-subscription-name: $SUBSCRIPTION_NAME
      $COMPONENT_NAME-channel: $CHANNEL
      $COMPONENT_NAME-source: $SOURCE
      $COMPONENT_NAME-source-namespace: $SOURCE_NAMESPACE"
        if [[ -n "$version_line" ]]; then
            labels_content="$labels_content
$version_line"
        fi
    else
        labels_content="      ### $COMPONENT_NAME
      $COMPONENT_NAME: 'true'
      $COMPONENT_NAME-subscription-name: $SUBSCRIPTION_NAME
      $COMPONENT_NAME-channel: $CHANNEL
      $COMPONENT_NAME-source: $SOURCE
      $COMPONENT_NAME-source-namespace: $SOURCE_NAMESPACE"
        if [[ -n "$version_line" ]]; then
            labels_content="$labels_content
$version_line"
        fi
    fi
    
    # Find the labels line for this section
    local labels_line
    labels_line=$(find_labels_line "$file_path" "$section_type" "$clusterset" "$is_commented")

    if [[ -n "$labels_line" ]]; then
        # Insert the labels after the labels: line
        local temp_file
        mkdir -p "$PROJECT_ROOT/.tmp"
        temp_file="$PROJECT_ROOT/.tmp/policy-gen.$$.tmp"

        # Copy everything up to the labels line
        head -n "$labels_line" "$file_path" > "$temp_file"

        # Add the new labels
        echo "$labels_content" >> "$temp_file"

        # Add everything after the labels line
        tail -n +$((labels_line + 1)) "$file_path" >> "$temp_file"

        # Replace the original file
        mv "$temp_file" "$file_path"
        return 0
    else
        log_warning "Could not find labels: line for $section_type/$clusterset, skipping"
        return 1
    fi
}

# Check if component already exists in a section.
# Uses awk for proper section tracking — previous grep-based approach with
# fixed -A windows broke on large values files where the labels block spans
# hundreds of lines past the clusterset key.
check_component_exists() {
    local file_path="$1"
    local section_type="$2"
    local clusterset="$3"
    local is_commented="$4"

    if [[ "$is_commented" == "true" ]]; then
        awk -v sec="$section_type" -v cs="$clusterset" -v comp="$COMPONENT_NAME" '
            {
                stripped = $0
                sub(/[[:space:]]+#.*$/, "", stripped)
                sub(/[[:space:]]+$/, "", stripped)
            }
            stripped == "# " sec ":" { found_section=1; next }
            found_section && stripped == "#   " cs ":" { found_clusterset=1; next }
            found_clusterset && /^#     labels:/ { in_labels=1; next }
            in_labels && $0 ~ ("^#       " comp ":") { found=1; exit }
            /^[a-zA-Z]/ { if (in_labels) exit; found_section=0; found_clusterset=0 }
            END { if (found) exit 0; exit 1 }
        ' "$file_path"
    else
        awk -v sec="$section_type" -v cs="$clusterset" -v comp="$COMPONENT_NAME" '
            {
                stripped = $0
                sub(/[[:space:]]+#.*$/, "", stripped)
                sub(/[[:space:]]+$/, "", stripped)
            }
            stripped == sec ":" { found_section=1; next }
            found_section && stripped == "  " cs ":" { found_clusterset=1; next }
            found_clusterset && /^    labels:/ { in_labels=1; next }
            in_labels && $0 ~ ("^      " comp ":") { found=1; exit }
            /^[a-zA-Z]/ { if (in_labels) exit; found_section=0; found_clusterset=0 }
            END { if (found) exit 0; exit 1 }
        ' "$file_path"
    fi
}

# Find the last label line number in a section/clusterset (to append at bottom)
# Passes section/clusterset as awk variables (-v) to avoid regex metacharacter issues.
# Strips trailing comments/whitespace from section-header lines before matching so
# keys like `  hub:    # <-- change me` still match.
find_labels_line() {
    local file_path="$1"
    local section_type="$2"
    local clusterset="$3"
    local is_commented="$4"

    if [[ "$is_commented" == "true" ]]; then
        awk -v sec="$section_type" -v cs="$clusterset" '
            {
                stripped = $0
                sub(/[[:space:]]+#.*$/, "", stripped)
                sub(/[[:space:]]+$/, "", stripped)
            }
            stripped == "# " sec ":" { found_section=1; next }
            found_section && stripped == "#   " cs ":" { found_clusterset=1; next }
            found_clusterset && /^#     labels:/ { in_labels=1; last=NR; next }
            in_labels && /^#       / { last=NR; next }
            in_labels && /^#$/ { last=NR; next }
            in_labels { print last; in_labels=0; exit }
            /^[a-zA-Z]/ { if (in_labels) { print last; exit } found_section=0; found_clusterset=0 }
            END { if (in_labels) print last }
        ' "$file_path"
    else
        awk -v sec="$section_type" -v cs="$clusterset" '
            {
                stripped = $0
                sub(/[[:space:]]+#.*$/, "", stripped)
                sub(/[[:space:]]+$/, "", stripped)
            }
            stripped == sec ":" { found_section=1; next }
            found_section && stripped == "  " cs ":" { found_clusterset=1; next }
            found_clusterset && /^    labels:/ { in_labels=1; last=NR; next }
            in_labels && /^      / { last=NR; next }
            in_labels && /^$/ { last=NR; next }
            in_labels { print last; in_labels=0; exit }
            /^[a-zA-Z]/ { if (in_labels) { print last; exit } found_section=0; found_clusterset=0 }
            END { if (in_labels) print last }
        ' "$file_path"
    fi
}

# Main execution
main() {
    generate_policy
    
    # Validate the generated policy
    if validate_generated_policy; then
        echo -e "${GREEN}🎉 Policy generation completed successfully!${NC}"
        echo ""
        
        # Show next steps
        echo -e "${BLUE}📋 Next Steps:${NC}"
        echo "1. Review generated files in $POLICY_DIR/"
        echo -e "2. Test locally: ${YELLOW}helm template $POLICY_DIR/${NC}"
        echo "3. Customize values.yaml if needed"
        echo "4. Add operator-specific configuration policies"
        echo "5. Commit and push — ApplicationSet auto-discovers $POLICY_SUBDIR/*"
        echo ""
        echo -e "${BLUE}📖 See $POLICY_DIR/README.md for detailed configuration guidance${NC}"
        echo ""
        
        # Add to AutoShift values files if requested
        if [[ "$ADD_TO_AUTOSHIFT" == "true" ]]; then
            echo ""
            if add_to_autoshift_values; then
                log_success "Policy integrated with AutoShift values files"
                echo ""
                echo -e "${BLUE}🚀 Integration Complete!${NC}"
                echo "Your policy is now enabled in AutoShift. Deploy AutoShift to apply changes."
            else
                log_warning "Failed to add labels to values files"
                echo ""
                echo -e "${BLUE}📋 Manual Integration Required:${NC}"
                show_integration_instructions
            fi
        fi
        
        # Show integration instructions
        if [[ "$SHOW_INTEGRATION" == "true" ]]; then
            show_integration_instructions
        fi
        
    else
        log_error "Policy generation failed validation"
        exit 1
    fi
}

# Run main function
main "$@"