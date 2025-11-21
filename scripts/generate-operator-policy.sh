#!/bin/bash
# AutoShift Operator Policy Generator
# Generates standardized operator installation policies for AutoShift
#
# Usage: ./scripts/generate-operator-policy.sh <component-name> <operator-name> [options]
# Example: ./scripts/generate-operator-policy.sh cert-manager cert-manager
# Example: ./scripts/generate-operator-policy.sh metallb metallb-operator --source community-operators

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
DEFAULT_SOURCE="redhat-operators"
DEFAULT_SOURCE_NAMESPACE="openshift-marketplace"
DEFAULT_CHANNEL="stable"
DEFAULT_VERSION=""  # Optional version pinning

# Parse arguments
COMPONENT_NAME=""
OPERATOR_NAME=""
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
    echo "  --values-files FILES      Comma-separated list of values files to update (e.g., 'hub,sbx')"
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
POLICY_DIR="policies/${COMPONENT_NAME}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_DIR="$SCRIPT_DIR/templates"

# Convert kebab-case to camelCase for values.yaml
COMPONENT_CAMEL=$(echo "$COMPONENT_NAME" | perl -pe 's/-([a-z])/\u$1/g')

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
    
    # Check YAML syntax for non-template files only
    for yaml_file in "$POLICY_DIR"/*.yaml; do
        if [[ -f "$yaml_file" ]] && ! python3 -c "import yaml; yaml.safe_load(open('$yaml_file'))" 2>/dev/null; then
            log_error "Invalid YAML syntax in $yaml_file"
            return 1
        fi
    done
    
    # Template files are validated via helm template above
    
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
        sed -i '' "s/  # targetNamespaces: # Optional: specify target namespaces for namespace-scoped operators/  targetNamespaces: # Target namespaces for namespace-scoped operators/" "$POLICY_DIR/values.yaml"
        sed -i '' "s/  #   - $NAMESPACE/    - $NAMESPACE/" "$POLICY_DIR/values.yaml"
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
    echo "To add this policy to AutoShift, edit autoshift/templates/applicationset.yaml and add:"
    echo ""
    echo -e "${YELLOW}    - name: $COMPONENT_NAME"
    echo "      path: policies/$COMPONENT_NAME"
    echo "      helm:"
    echo "        valueFiles:"
    echo -e "        - values.yaml${NC}"
    echo ""
    echo "Then deploy AutoShift to make the policy available across your clusters."
    echo ""
}

# Add labels to AutoShift values files
add_to_autoshift_values() {
    log_step "Adding labels to AutoShift values files..."
    
    # Determine which values files to update
    local values_files_to_update=()
    if [[ -z "$VALUES_FILES" ]]; then
        # Default: update all values files
        while IFS= read -r -d '' file; do
            values_files_to_update+=("$(basename "$file")")
        done < <(find autoshift -name "values*.yaml" -print0 2>/dev/null)
    else
        # Parse comma-separated list
        IFS=',' read -ra file_list <<< "$VALUES_FILES"
        for file_prefix in "${file_list[@]}"; do
            file_prefix=$(echo "$file_prefix" | xargs)  # trim whitespace
            if [[ -f "autoshift/values.$file_prefix.yaml" ]]; then
                values_files_to_update+=("values.$file_prefix.yaml")
            else
                log_warning "Values file autoshift/values.$file_prefix.yaml not found, skipping"
            fi
        done
    fi
    
    if [[ ${#values_files_to_update[@]} -eq 0 ]]; then
        log_error "No valid values files found to update"
        return 1
    fi
    
    # Add labels to each values file
    for values_file in "${values_files_to_update[@]}"; do
        local file_path="autoshift/$values_file"
        
        if [[ ! -f "$file_path" ]]; then
            log_warning "File $file_path not found, skipping"
            continue
        fi
        
        log_step "Adding labels to $values_file..."
        
        # Process all sections in the file
        add_labels_to_all_sections "$file_path"
        
        log_success "Updated $values_file"
    done
    
    log_success "Labels added to ${#values_files_to_update[@]} values file(s)"
}


# Add labels to all sections in the values file
add_labels_to_all_sections() {
    local file_path="$1"
    
    # Find all sections and their clustersets
    local sections_found=""
    
    # Check for hubClusterSets
    if grep -q "^hubClusterSets:" "$file_path"; then
        local hub_clustersets=$(awk '/^hubClusterSets:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^  [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^  /, ""); print}' "$file_path")
        while IFS= read -r clusterset; do
            [[ -z "$clusterset" ]] && continue
            add_labels_to_section "$file_path" "hubClusterSets" "$clusterset" false
            sections_found="$sections_found hubClusterSets/$clusterset"
        done <<< "$hub_clustersets"
    fi
    
    # Check for managedClusterSets  
    if grep -q "^managedClusterSets:" "$file_path"; then
        local managed_clustersets=$(awk '/^managedClusterSets:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^  [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^  /, ""); print}' "$file_path")
        while IFS= read -r clusterset; do
            [[ -z "$clusterset" ]] && continue
            add_labels_to_section "$file_path" "managedClusterSets" "$clusterset" false
            sections_found="$sections_found managedClusterSets/$clusterset"
        done <<< "$managed_clustersets"
    fi
    
    # Check for clusters (commented or active)
    if grep -q "^clusters:" "$file_path"; then
        # Active clusters section
        local active_clusters=$(awk '/^clusters:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^  [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^  /, ""); print}' "$file_path")
        while IFS= read -r cluster; do
            [[ -z "$cluster" ]] && continue
            add_labels_to_section "$file_path" "clusters" "$cluster" false
            sections_found="$sections_found clusters/$cluster"
        done <<< "$active_clusters"
    fi
    
    if grep -q "^# clusters:" "$file_path"; then
        # Commented clusters section
        local commented_clusters=$(awk '/^# clusters:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^#   [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^#   /, ""); print}' "$file_path")
        while IFS= read -r cluster; do
            [[ -z "$cluster" ]] && continue
            add_labels_to_section "$file_path" "clusters" "$cluster" true
            sections_found="$sections_found clusters/$cluster(commented)"
        done <<< "$commented_clusters"
    fi
    
    if [[ -z "$sections_found" ]]; then
        log_warning "No suitable sections found in $(basename "$file_path")"
    else
        log_step "Added $COMPONENT_NAME labels to:$sections_found"
    fi
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
        return 0
    fi
    
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
    local labels_line=$(find_labels_line "$file_path" "$section_type" "$clusterset" "$is_commented")
    
    if [[ -n "$labels_line" ]]; then
        # Insert the labels after the labels: line
        local temp_file=$(mktemp)
        
        # Copy everything up to the labels line
        head -n "$labels_line" "$file_path" > "$temp_file"
        
        # Add the new labels
        echo "$labels_content" >> "$temp_file"
        
        # Add everything after the labels line
        tail -n +$((labels_line + 1)) "$file_path" >> "$temp_file"
        
        # Replace the original file
        mv "$temp_file" "$file_path"
    else
        log_warning "Could not find labels: line for $section_type/$clusterset, skipping"
    fi
}

# Check if component already exists in a section
check_component_exists() {
    local file_path="$1"
    local section_type="$2"
    local clusterset="$3"
    local is_commented="$4"
    
    if [[ "$is_commented" == "true" ]]; then
        # Check in commented section - use grep for simpler detection
        grep -A 50 "^# $section_type:" "$file_path" | \
        grep -A 30 "^#   $clusterset:" | \
        grep -q "^#       $COMPONENT_NAME:"
    else
        # Check in active section - use grep for simpler detection  
        grep -A 50 "^$section_type:" "$file_path" | \
        grep -A 30 "^  $clusterset:" | \
        grep -q "^      $COMPONENT_NAME:"
    fi
}

# Find the line number of the labels: line for a specific section
find_labels_line() {
    local file_path="$1"
    local section_type="$2"
    local clusterset="$3"
    local is_commented="$4"
    
    if [[ "$is_commented" == "true" ]]; then
        # Find in commented section
        awk "
            /^# $section_type:/ { found_section=1; next }
            found_section && /^#   $clusterset:/ { found_clusterset=1; next }
            found_clusterset && /^#     labels:/ { print NR; exit }
            /^[a-zA-Z]/ { found_section=0; found_clusterset=0 }
        " "$file_path"
    else
        # Find in active section
        awk "
            /^$section_type:/ { found_section=1; next }
            found_section && /^  $clusterset:/ { found_clusterset=1; next }
            found_clusterset && /^    labels:/ { print NR; exit }
            /^[a-zA-Z]/ { found_section=0; found_clusterset=0 }
        " "$file_path"
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
        echo "2. Test locally: ${YELLOW}helm template $POLICY_DIR/${NC}"
        echo "3. Customize values.yaml if needed"
        echo "4. Add to AutoShift ApplicationSet (see below)"
        echo "5. Add operator-specific configuration policies"
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