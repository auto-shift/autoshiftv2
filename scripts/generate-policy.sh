#!/bin/bash
# AutoShift Configuration Policy Generator
# Generates standardized configuration policies for AutoShift
#
# Usage: ./scripts/generate-policy.sh <policy-name> [options]
# Example: ./scripts/generate-policy.sh my-config --dir policies/my-component --target both
# Example: ./scripts/generate-policy.sh  (interactive mode)

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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_DIR="$SCRIPT_DIR/templates"

# Parse arguments
POLICY_NAME=""
POLICY_DIR=""
TARGET=""
LABEL=""
DEPENDENCIES=()
ADD_TO_AUTOSHIFT=false
VALUES_FILES=""

usage() {
    echo "Usage: $0 [policy-name] [options]"
    echo ""
    echo "Generates a configuration policy template for AutoShift."
    echo "Missing required values will be prompted interactively."
    echo ""
    echo "Arguments:"
    echo "  policy-name              Kebab-case name for the policy (positional, or prompted)"
    echo ""
    echo "Options:"
    echo "  --dir DIR                Policy directory - existing or new (default: prompted)"
    echo "  --target TARGET          Placement target: hub, spoke, both, all (default: prompted)"
    echo "  --label LABEL            Label predicate key without autoshift.io/ prefix"
    echo "                           (default: directory basename; ignored for hub/all targets)"
    echo "  --dependency POLICY      Policy dependency name (repeatable)"
    echo "  --add-to-autoshift       Add enable label to AutoShift values files (spoke/both targets)"
    echo "  --values-files FILES     Comma-separated list of values files to update (e.g., 'hub,sbx')"
    echo "  --help                   Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 my-config --dir policies/my-component --target both"
    echo "  $0 dns-config --dir policies/openshift-dns --target hub"
    echo "  $0 my-config --dir policies/test --target spoke --dependency lvm-operator-install"
    echo "  $0 my-config --dir policies/my-component --target both --add-to-autoshift"
    echo "  $0   # fully interactive"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --dir)
            POLICY_DIR="$2"
            shift 2
            ;;
        --target)
            TARGET="$2"
            shift 2
            ;;
        --label)
            LABEL="$2"
            shift 2
            ;;
        --dependency)
            DEPENDENCIES+=("$2")
            shift 2
            ;;
        --add-to-autoshift)
            ADD_TO_AUTOSHIFT=true
            shift
            ;;
        --values-files)
            VALUES_FILES="$2"
            shift 2
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
            if [[ -z "$POLICY_NAME" ]]; then
                POLICY_NAME="$1"
            else
                echo -e "${RED}Error: Too many positional arguments${NC}"
                usage
                exit 1
            fi
            shift
            ;;
    esac
done

# Interactive prompts for missing values
if [[ -z "$POLICY_NAME" ]]; then
    echo -e "${BLUE}AutoShift Configuration Policy Generator${NC}"
    echo ""
    read -rp "Policy name (kebab-case): " POLICY_NAME
    if [[ -z "$POLICY_NAME" ]]; then
        log_error "Policy name is required"
        exit 1
    fi
fi

# Validate policy name format
if [[ ! "$POLICY_NAME" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]]; then
    log_error "Policy name must be lowercase alphanumeric with hyphens only (no leading/trailing hyphens)"
    echo "Examples: my-config, dns-tolerations, set-max-pods"
    exit 1
fi

# Interactive directory selection
if [[ -z "$POLICY_DIR" ]]; then
    echo ""
    echo -e "${BLUE}Select policy directory:${NC}"
    dirs=()
    i=1
    while IFS= read -r dir; do
        dirs+=("$dir")
        echo "  $i) $dir"
        i=$((i + 1))
    done < <(find policies -mindepth 1 -maxdepth 1 -type d | sort)
    echo "  $i) Create new directory"
    echo ""
    read -rp "Choice [1-$i]: " choice

    if [[ "$choice" -eq "$i" ]] 2>/dev/null; then
        read -rp "New directory name (under policies/): " new_dir
        if [[ -z "$new_dir" ]]; then
            log_error "Directory name is required"
            exit 1
        fi
        POLICY_DIR="policies/$new_dir"
    elif [[ "$choice" -ge 1 && "$choice" -lt "$i" ]] 2>/dev/null; then
        POLICY_DIR="${dirs[$((choice - 1))]}"
    else
        log_error "Invalid choice"
        exit 1
    fi
fi

# Interactive target selection
if [[ -z "$TARGET" ]]; then
    echo ""
    echo -e "${BLUE}Select placement target:${NC}"
    echo "  1) hub    - Hub clusters only"
    echo "  2) spoke  - Managed/spoke clusters only (with label selector)"
    echo "  3) both   - Hub + managed clusters (with label selector)"
    echo "  4) all    - All clusters (no label selector)"
    echo ""
    read -rp "Choice [1-4]: " target_choice
    case "$target_choice" in
        1) TARGET="hub" ;;
        2) TARGET="spoke" ;;
        3) TARGET="both" ;;
        4) TARGET="all" ;;
        *)
            log_error "Invalid choice"
            exit 1
            ;;
    esac
fi

# Validate target
case "$TARGET" in
    hub|spoke|both|all) ;;
    *)
        log_error "Invalid target: $TARGET (must be hub, spoke, both, or all)"
        exit 1
        ;;
esac

# Interactive prompt for add-to-autoshift
if [[ "$ADD_TO_AUTOSHIFT" == "false" && "$TARGET" != "all" ]]; then
    echo ""
    read -rp "Add label to AutoShift values files? [y/N]: " add_choice
    if [[ "$add_choice" =~ ^[Yy]$ ]]; then
        ADD_TO_AUTOSHIFT=true
    fi
fi

# Derive label from directory basename if not set
DIR_BASENAME="$(basename "$POLICY_DIR")"
if [[ -z "$LABEL" ]]; then
    LABEL="$DIR_BASENAME"
fi

# Check template directory
if [[ ! -d "$TEMPLATE_DIR" ]]; then
    log_error "Template directory $TEMPLATE_DIR not found"
    echo "Run this script from the AutoShift repository root"
    exit 1
fi

# Determine if this is a new or existing directory
IS_NEW_DIR=false
if [[ ! -d "$POLICY_DIR" ]]; then
    IS_NEW_DIR=true
fi

# Check if template file already exists
if [[ -f "$POLICY_DIR/templates/policy-${POLICY_NAME}.yaml" ]]; then
    log_error "Template file $POLICY_DIR/templates/policy-${POLICY_NAME}.yaml already exists"
    exit 1
fi

# Build placement clusterSets block
build_clustersets() {
    local cs=""
    case "$TARGET" in
        hub)
            cs='  {{- range $clusterSet, $value := .Values.hubClusterSets }}
    - {{ $clusterSet }}
  {{- end }}'
            ;;
        spoke)
            cs='  {{- range $clusterSet, $value := .Values.managedClusterSets }}
    - {{ $clusterSet }}
  {{- end }}'
            ;;
        both|all)
            cs='  {{- range $clusterSet, $value := .Values.managedClusterSets }}
    - {{ $clusterSet }}
  {{- end }}
  {{- range $clusterSet, $value := .Values.hubClusterSets }}
    - {{ $clusterSet }}
  {{- end }}'
            ;;
    esac
    echo "$cs"
}

# Build placement predicates block
build_predicates() {
    local pred=""
    case "$TARGET" in
        spoke|both)
            pred="  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchExpressions:
            - key: 'autoshift.io/${LABEL}'
              operator: In
              values:
              - 'true'
  tolerations:
    - key: cluster.open-cluster-management.io/unreachable
      operator: Exists
    - key: cluster.open-cluster-management.io/unavailable
      operator: Exists"
            ;;
        hub|all)
            pred="  tolerations:
    - key: cluster.open-cluster-management.io/unreachable
      operator: Exists
    - key: cluster.open-cluster-management.io/unavailable
      operator: Exists"
            ;;
    esac
    echo "$pred"
}

# Build dependency block
build_dependency_block() {
    if [[ ${#DEPENDENCIES[@]} -eq 0 ]]; then
        return
    fi
    echo "  dependencies:"
    for dep in "${DEPENDENCIES[@]}"; do
        echo "    - name: policy-${dep}"
        echo "      namespace: {{ .Values.policy_namespace }}"
        echo "      apiVersion: policy.open-cluster-management.io/v1"
        echo "      compliance: Compliant"
        echo "      kind: Policy"
    done
}

# --- AutoShift values file integration ---

# Find the last label line number in a section/clusterset (to append at bottom)
find_labels_line() {
    local file_path="$1"
    local section_type="$2"
    local clusterset="$3"
    local is_commented="$4"

    if [[ "$is_commented" == "true" ]]; then
        awk "
            /^# $section_type:/ { found_section=1; next }
            found_section && /^#   $clusterset:/ { found_clusterset=1; next }
            found_clusterset && /^#     labels:/ { in_labels=1; last=NR; next }
            in_labels && /^#       / { last=NR; next }
            in_labels && /^#$/ { last=NR; next }
            in_labels { print last; exit }
            /^[a-zA-Z]/ { if (in_labels) { print last; exit } found_section=0; found_clusterset=0 }
            END { if (in_labels) print last }
        " "$file_path"
    else
        awk "
            /^$section_type:/ { found_section=1; next }
            found_section && /^  $clusterset:/ { found_clusterset=1; next }
            found_clusterset && /^    labels:/ { in_labels=1; last=NR; next }
            in_labels && /^      / { last=NR; next }
            in_labels && /^$/ { last=NR; next }
            in_labels { print last; exit }
            /^[a-zA-Z]/ { if (in_labels) { print last; exit } found_section=0; found_clusterset=0 }
            END { if (in_labels) print last }
        " "$file_path"
    fi
}

# Check if the label already exists in a section
check_label_exists() {
    local file_path="$1"
    local section_type="$2"
    local clusterset="$3"
    local is_commented="$4"
    local label_key="$5"

    if [[ "$is_commented" == "true" ]]; then
        grep -A 50 "^# $section_type:" "$file_path" | \
        grep -A 30 "^#   $clusterset:" | \
        grep -q "^#       $label_key:"
    else
        grep -A 50 "^$section_type:" "$file_path" | \
        grep -A 30 "^  $clusterset:" | \
        grep -q "^      $label_key:"
    fi
}

# Add the enable label to a specific section/clusterset
add_label_to_section() {
    local file_path="$1"
    local section_type="$2"
    local clusterset="$3"
    local is_commented="$4"
    local label_key="$5"

    if check_label_exists "$file_path" "$section_type" "$clusterset" "$is_commented" "$label_key"; then
        log_warning "Label '$label_key' already exists in $section_type/$clusterset, skipping"
        return 0
    fi

    local labels_line
    labels_line=$(find_labels_line "$file_path" "$section_type" "$clusterset" "$is_commented")

    if [[ -z "$labels_line" ]]; then
        log_warning "Could not find labels: line for $section_type/$clusterset, skipping"
        return 0
    fi

    # Determine comment style: example files use banner, others use ###
    local basename_file
    basename_file=$(basename "$file_path")
    local is_example=false
    if [[ "$basename_file" == _example* ]]; then
        is_example=true
    fi

    # Convert label key to title case for banner header
    local label_title
    label_title=$(echo "$label_key" | sed 's/-/ /g' | awk '{for(i=1;i<=NF;i++) $i=toupper(substr($i,1,1)) substr($i,2)}1')

    local label_content
    if [[ "$is_commented" == "true" ]]; then
        label_content="#       ### $label_key
#       $label_key: 'true'"
    elif [[ "$is_example" == "true" ]]; then
        label_content="
      # =======================================================================
      # $label_title
      # =======================================================================
      $label_key: 'false'"
    else
        label_content="      ### $label_key
      $label_key: 'true'"
    fi

    local temp_file
    temp_file=$(mktemp)
    head -n "$labels_line" "$file_path" > "$temp_file"
    echo "$label_content" >> "$temp_file"
    tail -n +$((labels_line + 1)) "$file_path" >> "$temp_file"
    mv "$temp_file" "$file_path"
}

# Process all sections in a values file based on target
add_labels_to_file() {
    local file_path="$1"
    local sections_found=""

    # Example files get the label in all sections regardless of target
    local is_example=false
    local basename_file
    basename_file=$(basename "$file_path")
    if [[ "$basename_file" == _example* ]]; then
        is_example=true
    fi

    # Add to managedClusterSets
    if [[ "$is_example" == "true" || "$TARGET" == "spoke" || "$TARGET" == "both" ]]; then
        if grep -q "^managedClusterSets:" "$file_path"; then
            local managed_clustersets
            managed_clustersets=$(awk '/^managedClusterSets:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^  [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^  /, ""); print}' "$file_path")
            while IFS= read -r clusterset; do
                [[ -z "$clusterset" ]] && continue
                add_label_to_section "$file_path" "managedClusterSets" "$clusterset" false "$LABEL"
                sections_found="$sections_found managedClusterSets/$clusterset"
            done <<< "$managed_clustersets"
        fi
    fi

    # Add to hubClusterSets
    if [[ "$is_example" == "true" || "$TARGET" == "hub" || "$TARGET" == "both" ]]; then
        if grep -q "^hubClusterSets:" "$file_path"; then
            local hub_clustersets
            hub_clustersets=$(awk '/^hubClusterSets:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^  [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^  /, ""); print}' "$file_path")
            while IFS= read -r clusterset; do
                [[ -z "$clusterset" ]] && continue
                add_label_to_section "$file_path" "hubClusterSets" "$clusterset" false "$LABEL"
                sections_found="$sections_found hubClusterSets/$clusterset"
            done <<< "$hub_clustersets"
        fi
    fi

    # Process clusters sections (commented or active)
    if grep -q "^clusters:" "$file_path"; then
        local active_clusters
        active_clusters=$(awk '/^clusters:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^  [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^  /, ""); print}' "$file_path")
        while IFS= read -r cluster; do
            [[ -z "$cluster" ]] && continue
            add_label_to_section "$file_path" "clusters" "$cluster" false "$LABEL"
            sections_found="$sections_found clusters/$cluster"
        done <<< "$active_clusters"
    fi

    if grep -q "^# clusters:" "$file_path"; then
        local commented_clusters
        commented_clusters=$(awk '/^# clusters:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^#   [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^#   /, ""); print}' "$file_path")
        while IFS= read -r cluster; do
            [[ -z "$cluster" ]] && continue
            add_label_to_section "$file_path" "clusters" "$cluster" true "$LABEL"
            sections_found="$sections_found clusters/$cluster(commented)"
        done <<< "$commented_clusters"
    fi

    if [[ -z "$sections_found" ]]; then
        log_warning "No matching sections found in $(basename "$file_path")"
    else
        log_step "Added '$LABEL' label to:$sections_found"
    fi
}

# Add labels to AutoShift values files
add_to_autoshift_values() {
    log_step "Adding labels to AutoShift values files..."

    local values_files_to_update=()
    if [[ -n "$VALUES_FILES" ]]; then
        # CLI flag: parse comma-separated list
        IFS=',' read -ra file_list <<< "$VALUES_FILES"
        for file_prefix in "${file_list[@]}"; do
            file_prefix=$(echo "$file_prefix" | xargs)
            if [[ -f "autoshift/values/clustersets/$file_prefix.yaml" ]]; then
                values_files_to_update+=("values/clustersets/$file_prefix.yaml")
            else
                log_warning "Values file autoshift/values/clustersets/$file_prefix.yaml not found, skipping"
            fi
        done
    else
        # Interactive: let user select which values files to update
        local available_files=()
        while IFS= read -r -d '' file; do
            local rel_path="${file#autoshift/}"
            available_files+=("$rel_path")
        done < <(find autoshift/values/clustersets -name "*.yaml" -not -name "_*" -print0 2>/dev/null | sort -z)

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
                # All files
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

    # Always include example files
    while IFS= read -r -d '' file; do
        local rel_path="${file#autoshift/}"
        values_files_to_update+=("$rel_path")
    done < <(find autoshift/values/clustersets autoshift/values/clusters -name "_example*.yaml" -print0 2>/dev/null)

    if [[ ${#values_files_to_update[@]} -eq 0 ]]; then
        log_error "No valid values files found to update"
        return 1
    fi

    for values_file in "${values_files_to_update[@]}"; do
        local file_path="autoshift/$values_file"
        if [[ ! -f "$file_path" ]]; then
            log_warning "File $file_path not found, skipping"
            continue
        fi
        log_step "Processing $values_file..."
        add_labels_to_file "$file_path"
        log_success "Updated $values_file"
    done

    log_success "Labels added to ${#values_files_to_update[@]} values file(s)"
}

# Main generation
echo ""
echo -e "${GREEN}🚀 Generating configuration policy '${POLICY_NAME}'...${NC}"
echo ""

# Create directory structure
if [[ "$IS_NEW_DIR" == "true" ]]; then
    log_step "Creating new chart directory: $POLICY_DIR"
    mkdir -p "$POLICY_DIR/templates"

    # Generate Chart.yaml using existing template
    log_step "Generating Chart.yaml"
    sed -e "s/{{COMPONENT_NAME}}/$DIR_BASENAME/g" \
        "$TEMPLATE_DIR/Chart.yaml.template" > "$POLICY_DIR/Chart.yaml"
    log_success "Created Chart.yaml"

    # Generate minimal values.yaml
    log_step "Generating values.yaml"
    cp "$TEMPLATE_DIR/values-minimal.yaml.template" "$POLICY_DIR/values.yaml"
    log_success "Created values.yaml"
else
    log_step "Using existing directory: $POLICY_DIR"
    mkdir -p "$POLICY_DIR/templates"
fi

# Write substitution blocks to temp files for reliable multi-line replacement
TMPDIR_SUB=$(mktemp -d)
build_clustersets > "$TMPDIR_SUB/clustersets"
build_predicates > "$TMPDIR_SUB/predicates"
build_dependency_block > "$TMPDIR_SUB/dependency"

if [[ "$TARGET" == "hub" ]]; then
    echo '{{- if .Values.hubClusterSets }}' > "$TMPDIR_SUB/hub_start"
    echo '{{- end }}' > "$TMPDIR_SUB/hub_end"
else
    : > "$TMPDIR_SUB/hub_start"
    : > "$TMPDIR_SUB/hub_end"
fi

# Generate policy template
log_step "Generating policy template"

OUTPUT_FILE="$POLICY_DIR/templates/policy-${POLICY_NAME}.yaml"

# Read the template line by line, replacing markers with file contents
{
    while IFS= read -r line; do
        case "$line" in
            *'{{HUB_WRAP_START}}'*)
                if [[ -s "$TMPDIR_SUB/hub_start" ]]; then
                    cat "$TMPDIR_SUB/hub_start"
                fi
                ;;
            *'{{HUB_WRAP_END}}'*)
                if [[ -s "$TMPDIR_SUB/hub_end" ]]; then
                    cat "$TMPDIR_SUB/hub_end"
                fi
                ;;
            *'{{PLACEMENT_CLUSTERSETS}}'*)
                cat "$TMPDIR_SUB/clustersets"
                ;;
            *'{{PLACEMENT_PREDICATES}}'*)
                cat "$TMPDIR_SUB/predicates"
                ;;
            *'{{DEPENDENCY_BLOCK}}'*)
                if [[ -s "$TMPDIR_SUB/dependency" ]]; then
                    cat "$TMPDIR_SUB/dependency"
                fi
                ;;
            *'{{POLICY_NAME}}'*)
                echo "${line//\{\{POLICY_NAME\}\}/$POLICY_NAME}"
                ;;
            *)
                echo "$line"
                ;;
        esac
    done < "$TEMPLATE_DIR/policy-config.yaml.template"
} > "$OUTPUT_FILE"

rm -rf "$TMPDIR_SUB"
log_success "Created policy-${POLICY_NAME}.yaml"

# Validate with helm template
log_step "Validating generated policy..."
if helm template "$POLICY_DIR" >/dev/null 2>&1; then
    log_success "Policy validation passed (helm template)"
else
    log_error "Generated policy fails helm template validation"
    echo "Run: helm template $POLICY_DIR"
    exit 1
fi

# Add labels to values files if requested
if [[ "$ADD_TO_AUTOSHIFT" == "true" ]]; then
    if [[ "$TARGET" == "all" ]]; then
        log_warning "--add-to-autoshift skipped: 'all' target applies to every cluster without labels"
    else
        echo ""
        if add_to_autoshift_values; then
            log_success "Label '$LABEL' integrated with AutoShift values files"
        else
            log_warning "Failed to add labels to values files"
        fi
    fi
fi

echo ""
echo -e "${GREEN}🎉 Policy generation completed successfully!${NC}"
echo ""

# Show summary
echo -e "${BLUE}📋 Summary:${NC}"
echo "  Policy:    policy-${POLICY_NAME}"
echo "  Directory: $POLICY_DIR/"
echo "  Target:    $TARGET"
if [[ "$TARGET" != "all" ]]; then
    echo "  Label:     autoshift.io/${LABEL}"
fi
if [[ ${#DEPENDENCIES[@]} -gt 0 ]]; then
    echo "  Depends:   ${DEPENDENCIES[*]}"
fi
echo ""

# Show next steps
echo -e "${BLUE}📋 Next Steps:${NC}"
echo "1. Edit $OUTPUT_FILE"
echo "   - Replace the placeholder ConfigMap with your actual resource definition"
echo "   - Adjust severity and complianceType as needed"
echo "2. Test locally: helm template $POLICY_DIR/"
if [[ "$TARGET" != "all" ]]; then
    if [[ "$ADD_TO_AUTOSHIFT" == "true" ]]; then
        echo "3. Labels already added to values files via --add-to-autoshift"
    else
        echo "3. Add 'autoshift.io/${LABEL}: true' to your values files (or re-run with --add-to-autoshift)"
    fi
fi
echo ""
