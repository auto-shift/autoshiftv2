#!/bin/bash
# AutoShift Configuration Policy Generator
# Generates standardized configuration policies for AutoShift
#
# Usage: ./scripts/generate-policy.sh <policy-name> [options]
# Example: ./scripts/generate-policy.sh my-config --dir policies/stable/my-component --target both
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
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEMPLATE_DIR="$SCRIPT_DIR/templates"

# Render a PolicyGenerator dir the same way the repo-server CMP / CI does: substitute ONLY the
# per-deployment ${...} tokens into a throwaway copy (never mutate the source), then run
# kustomize + the PolicyGenerator plugin. Returns 0 on success, 1 on render failure, 2 if the
# toolchain is not installed.
pg_render() {
    local dir="$1"
    local kbin plugin_home
    if [[ -x "$PROJECT_ROOT/.tools/kustomize" ]]; then
        kbin="$PROJECT_ROOT/.tools/kustomize"
        plugin_home="$PROJECT_ROOT/.tools/kustomize-plugin"
    elif command -v kustomize >/dev/null 2>&1; then
        kbin="kustomize"
        plugin_home="${KUSTOMIZE_PLUGIN_HOME:-}"
    else
        return 2
    fi

    local tmp
    tmp="$(mktemp -d)"
    cp -R "$dir"/. "$tmp"/
    local f
    while IFS= read -r f; do
        sed -e 's/\${POLICY_NAMESPACE}/policies-autoshift/g' \
            -e 's/\${REMEDIATION}/enforce/g' \
            -e 's/\${EVAL_COMPLIANT}/2h/g' \
            -e 's/\${EVAL_NONCOMPLIANT}/45s/g' \
            -e 's/\${CLUSTER_SET_SUFFIX}//g' \
            "$f" > "$f.sub" && mv "$f.sub" "$f"
    done < <(find "$tmp" -name '*.yaml')

    KUSTOMIZE_PLUGIN_HOME="$plugin_home" "$kbin" build \
        --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone "$tmp" >/dev/null 2>&1
    local rc=$?
    rm -rf "$tmp"
    return $rc
}

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
    echo "  $0 my-config --dir policies/stable/my-component --target both"
    echo "  $0 dns-config --dir policies/stable/openshift-dns --target hub"
    echo "  $0 my-config --dir policies/stable/test --target spoke --dependency lvm-operator-install"
    echo "  $0 my-config --dir policies/stable/my-component --target both --add-to-autoshift"
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
            ADD_TO_AUTOSHIFT=true  # --values-files implies --add-to-autoshift
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
    done < <(find policies/stable policies/certified policies/community -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort)
    echo "  $i) Create new directory under policies/stable/"
    echo ""
    read -rp "Choice [1-$i]: " choice

    if [[ "$choice" -eq "$i" ]] 2>/dev/null; then
        read -rp "New directory name (under policies/stable/): " new_dir
        if [[ -z "$new_dir" ]]; then
            log_error "Directory name is required"
            exit 1
        fi
        POLICY_DIR="policies/stable/$new_dir"
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

# Interactive prompt for add-to-autoshift (only when stdin is a TTY;
# non-interactive callers skip silently and must pass --add-to-autoshift explicitly)
if [[ "$ADD_TO_AUTOSHIFT" == "false" && "$TARGET" != "all" && -t 0 ]]; then
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

# Check if a manifest for this policy already exists
if [[ -f "$POLICY_DIR/manifests/${POLICY_NAME}.yaml" ]]; then
    log_error "Manifest $POLICY_DIR/manifests/${POLICY_NAME}.yaml already exists"
    exit 1
fi

# Existing dir must be a PolicyGenerator dir (AutoShift policies use PolicyGenerator, not Helm)
if [[ "$IS_NEW_DIR" == "false" && ! -f "$POLICY_DIR/policy-generator-config.yaml" ]]; then
    log_error "$POLICY_DIR exists but has no policy-generator-config.yaml"
    if [[ -f "$POLICY_DIR/Chart.yaml" ]]; then
        echo "This looks like a legacy Helm chart. AutoShift policies now use PolicyGenerator —"
        echo "migrate this directory first, or pass --dir with a new directory name."
    fi
    exit 1
fi

# Build the predicate matchExpressions for the given target. The PG placements carry NO
# spec.clusterSets — scoping comes from the ManagedClusterSetBindings the top autoshift chart
# creates in the policy namespace, filtered by these label predicates:
#   hub   -> autoshift.io/self-managed Exists   (hub-only marker; managed clusters never carry it)
#   spoke -> self-managed DoesNotExist + autoshift.io/<label> In [true]
#   both  -> autoshift.io/<label> In [true]      (hub + managed)
#   all   -> no predicate (every bound cluster)
build_match_expressions() {
    case "$TARGET" in
        hub)
            echo "            - key: 'autoshift.io/self-managed'"
            echo "              operator: Exists"
            ;;
        spoke)
            echo "            - key: 'autoshift.io/self-managed'"
            echo "              operator: DoesNotExist"
            echo "            - key: 'autoshift.io/${LABEL}'"
            echo "              operator: In"
            echo "              values:"
            echo "                - 'true'"
            ;;
        both)
            echo "            - key: 'autoshift.io/${LABEL}'"
            echo "              operator: In"
            echo "              values:"
            echo "                - 'true'"
            ;;
    esac
}

# Write a complete Placement file for the given target to stdout.
# $1 = placement metadata.name
build_placement() {
    local placement_name="$1"
    cat <<EOF
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: ${placement_name}
  namespace: \${POLICY_NAMESPACE}
spec:
EOF
    if [[ "$TARGET" != "all" ]]; then
        echo "  # No spec.clusterSets: selects across all ManagedClusterSetBindings in this namespace,"
        echo "  # filtered by the predicate below."
        echo "  predicates:"
        echo "    - requiredClusterSelector:"
        echo "        labelSelector:"
        echo "          matchExpressions:"
        build_match_expressions
    else
        echo "  # No predicate: selects every cluster bound to this namespace's ManagedClusterSetBindings."
    fi
    cat <<'EOF'
  tolerations:
    - key: cluster.open-cluster-management.io/unreachable
      operator: Exists
    - key: cluster.open-cluster-management.io/unavailable
      operator: Exists
EOF
}

# Build the PolicyGenerator dependencies block for a policies[] entry (4-space indent).
# Emits nothing when there are no dependencies.
build_dependency_block() {
    if [[ ${#DEPENDENCIES[@]} -eq 0 ]]; then
        return
    fi
    echo "    dependencies:"
    for dep in "${DEPENDENCIES[@]}"; do
        echo "      - name: policy-${dep}"
    done
}

# --- AutoShift values file integration ---

# Find the last label line number in a section/clusterset (to append at bottom).
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

# Check if the label already exists in a section.
# Uses awk for proper section tracking — previous grep-based approach with
# fixed -A windows broke on large values files where the labels block spans
# hundreds of lines past the clusterset key.
check_label_exists() {
    local file_path="$1"
    local section_type="$2"
    local clusterset="$3"
    local is_commented="$4"
    local label_key="$5"

    if [[ "$is_commented" == "true" ]]; then
        awk -v sec="$section_type" -v cs="$clusterset" -v lbl="$label_key" '
            {
                stripped = $0
                sub(/[[:space:]]+#.*$/, "", stripped)
                sub(/[[:space:]]+$/, "", stripped)
            }
            stripped == "# " sec ":" { found_section=1; next }
            found_section && stripped == "#   " cs ":" { found_clusterset=1; next }
            found_clusterset && /^#     labels:/ { in_labels=1; next }
            in_labels && $0 ~ ("^#       " lbl ":") { found=1; exit }
            /^[a-zA-Z]/ { if (in_labels) exit; found_section=0; found_clusterset=0 }
            END { if (found) exit 0; exit 1 }
        ' "$file_path"
    else
        awk -v sec="$section_type" -v cs="$clusterset" -v lbl="$label_key" '
            {
                stripped = $0
                sub(/[[:space:]]+#.*$/, "", stripped)
                sub(/[[:space:]]+$/, "", stripped)
            }
            stripped == sec ":" { found_section=1; next }
            found_section && stripped == "  " cs ":" { found_clusterset=1; next }
            found_clusterset && /^    labels:/ { in_labels=1; next }
            in_labels && $0 ~ ("^      " lbl ":") { found=1; exit }
            /^[a-zA-Z]/ { if (in_labels) exit; found_section=0; found_clusterset=0 }
            END { if (found) exit 0; exit 1 }
        ' "$file_path"
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
        return 1
    fi

    local labels_line
    labels_line=$(find_labels_line "$file_path" "$section_type" "$clusterset" "$is_commented")

    if [[ -z "$labels_line" ]]; then
        log_warning "Could not find labels: line for $section_type/$clusterset, skipping"
        return 1
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
    local _project_root="$(cd "$SCRIPT_DIR/.." && pwd)"
    mkdir -p "$_project_root/.tmp"
    temp_file="$_project_root/.tmp/policy-gen.$$.tmp"
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

    # Only count sections that were actually updated (add_label_to_section returns
    # non-zero when it skipped, e.g. labels-line not found or duplicate).

    # Add to managedClusterSets
    if [[ "$is_example" == "true" || "$TARGET" == "spoke" || "$TARGET" == "both" ]]; then
        if grep -q "^managedClusterSets:" "$file_path"; then
            local managed_clustersets
            managed_clustersets=$(awk '/^managedClusterSets:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^  [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^  /, ""); print}' "$file_path")
            while IFS= read -r clusterset; do
                [[ -z "$clusterset" ]] && continue
                if add_label_to_section "$file_path" "managedClusterSets" "$clusterset" false "$LABEL"; then
                    sections_found="$sections_found managedClusterSets/$clusterset"
                fi
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
                if add_label_to_section "$file_path" "hubClusterSets" "$clusterset" false "$LABEL"; then
                    sections_found="$sections_found hubClusterSets/$clusterset"
                fi
            done <<< "$hub_clustersets"
        fi
    fi

    # Process clusters sections (commented or active)
    if grep -q "^clusters:" "$file_path"; then
        local active_clusters
        active_clusters=$(awk '/^clusters:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^  [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^  /, ""); print}' "$file_path")
        while IFS= read -r cluster; do
            [[ -z "$cluster" ]] && continue
            if add_label_to_section "$file_path" "clusters" "$cluster" false "$LABEL"; then
                sections_found="$sections_found clusters/$cluster"
            fi
        done <<< "$active_clusters"
    fi

    if grep -q "^# clusters:" "$file_path"; then
        local commented_clusters
        commented_clusters=$(awk '/^# clusters:/{flag=1; next} /^[a-zA-Z]/{flag=0} flag && /^#   [a-zA-Z][^:]*:/{gsub(/:.*/, ""); gsub(/^#   /, ""); print}' "$file_path")
        while IFS= read -r cluster; do
            [[ -z "$cluster" ]] && continue
            if add_label_to_section "$file_path" "clusters" "$cluster" true "$LABEL"; then
                sections_found="$sections_found clusters/$cluster(commented)"
            fi
        done <<< "$commented_clusters"
    fi

    if [[ -z "$sections_found" ]]; then
        log_warning "No matching sections found in $(basename "$file_path")"
        return 1
    fi
    log_step "Added '$LABEL' label to:$sections_found"
    return 0
}

# Add labels to AutoShift values files
add_to_autoshift_values() {
    log_step "Adding labels to AutoShift values files..."

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
        # Search clustersets/ AND parent values/ for single-file setups
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

    local updated_count=0
    for values_file in "${values_files_to_update[@]}"; do
        local file_path="autoshift/$values_file"
        if [[ ! -f "$file_path" ]]; then
            log_warning "File $file_path not found, skipping"
            continue
        fi
        log_step "Processing $values_file..."
        if add_labels_to_file "$file_path"; then
            log_success "Updated $values_file"
            updated_count=$((updated_count + 1))
        else
            log_warning "No changes made to $values_file"
        fi
    done

    log_success "Labels added to $updated_count of ${#values_files_to_update[@]} values file(s)"
}

# Main generation
echo ""
echo -e "${GREEN}🚀 Generating configuration policy '${POLICY_NAME}'...${NC}"
echo ""

# Emit the bare placeholder manifest for this policy (PG wraps it into a ConfigurationPolicy).
write_manifest() {
    mkdir -p "$POLICY_DIR/manifests"
    sed -e "s/{{POLICY_NAME}}/$POLICY_NAME/g" \
        -e "s/{{LABEL}}/$LABEL/g" \
        "$TEMPLATE_DIR/manifest-config.yaml.template" > "$POLICY_DIR/manifests/${POLICY_NAME}.yaml"
    log_success "Created manifests/${POLICY_NAME}.yaml (bare placeholder — replace with your resource)"
}

MANIFEST_PATH="manifests/${POLICY_NAME}.yaml"

if [[ "$IS_NEW_DIR" == "true" ]]; then
    log_step "Creating new PolicyGenerator directory: $POLICY_DIR"
    mkdir -p "$POLICY_DIR/manifests"

    # kustomization.yaml (static entrypoint)
    cp "$TEMPLATE_DIR/kustomization.yaml.template" "$POLICY_DIR/kustomization.yaml"
    log_success "Created kustomization.yaml"

    # placement.yaml (default placement for the dir)
    build_placement "placement-policy-${POLICY_NAME}" > "$POLICY_DIR/placement.yaml"
    log_success "Created placement.yaml (target: $TARGET)"

    # policy-generator-config.yaml — line-based marker replacement for the dependency block
    log_step "Generating policy-generator-config.yaml"
    DEP_BLOCK="$(build_dependency_block)"
    {
        while IFS= read -r line; do
            case "$line" in
                *'{{DEPENDENCY_BLOCK}}'*)
                    [[ -n "$DEP_BLOCK" ]] && echo "$DEP_BLOCK"
                    ;;
                *)
                    line="${line//\{\{DIR_BASENAME\}\}/$DIR_BASENAME}"
                    line="${line//\{\{POLICY_NAME\}\}/$POLICY_NAME}"
                    echo "$line"
                    ;;
            esac
        done < "$TEMPLATE_DIR/pg-config.yaml.template"
    } > "$POLICY_DIR/policy-generator-config.yaml"
    log_success "Created policy-generator-config.yaml"

    # bare manifest
    write_manifest
else
    log_step "Appending policy to existing PolicyGenerator directory: $POLICY_DIR"

    # A bare `path: manifests` wildcard on an existing policy would also sweep the new flat file.
    if grep -qE '^[[:space:]]*-[[:space:]]*path:[[:space:]]*manifests[[:space:]]*$' "$POLICY_DIR/policy-generator-config.yaml"; then
        log_warning "An existing policy uses 'path: manifests' (dir wildcard); it may also pick up the new"
        log_warning "manifest. Review policy-generator-config.yaml and use explicit file paths if so."
    fi

    # bare manifest
    write_manifest

    # per-policy placement (the dir's default placement.yaml has its own predicate)
    build_placement "placement-policy-${POLICY_NAME}" > "$POLICY_DIR/placement-${POLICY_NAME}.yaml"
    log_success "Created placement-${POLICY_NAME}.yaml (target: $TARGET)"

    # Append a policies[] entry (assumes policies: is the last top-level key — true for all
    # AutoShift PG configs).
    log_step "Appending policies[] entry to policy-generator-config.yaml"
    {
        echo "  - name: policy-${POLICY_NAME}"
        echo "    placement:"
        echo "      placementPath: placement-${POLICY_NAME}.yaml"
        build_dependency_block
        echo "    manifests:"
        echo "      - path: ${MANIFEST_PATH}"
    } >> "$POLICY_DIR/policy-generator-config.yaml"
    log_success "Appended policy-${POLICY_NAME} to policy-generator-config.yaml"
fi

OUTPUT_FILE="$POLICY_DIR/manifests/${POLICY_NAME}.yaml"

# Validate with a PolicyGenerator render (same as the CMP / CI)
log_step "Validating generated policy (PolicyGenerator render)..."
pg_render "$POLICY_DIR"
rc=$?
if [[ $rc -eq 0 ]]; then
    log_success "Policy validation passed (kustomize + PolicyGenerator)"
elif [[ $rc -eq 2 ]]; then
    log_warning "kustomize/PolicyGenerator not found in .tools/ — skipping render validation."
    log_warning "Install it with: make install-policy-generator"
else
    log_error "Generated policy fails PolicyGenerator render"
    echo "Run: KUSTOMIZE_PLUGIN_HOME=\$PWD/.tools/kustomize-plugin .tools/kustomize build --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone $POLICY_DIR"
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
echo "   - Replace the placeholder ConfigMap with your actual bare resource (no ConfigurationPolicy wrapper)"
echo "   - For hub templates / loops / conditionals, use a bare 'object-templates-raw:' manifest instead"
echo "2. Test locally: KUSTOMIZE_PLUGIN_HOME=\$PWD/.tools/kustomize-plugin .tools/kustomize build --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone $POLICY_DIR"
echo "   Full validation: cd tools && go test -tags integration -count=1 ./internal/resolver/..."
if [[ "$TARGET" != "all" ]]; then
    if [[ "$ADD_TO_AUTOSHIFT" == "true" ]]; then
        echo "3. Labels already added to values files via --add-to-autoshift"
    else
        echo "3. Add 'autoshift.io/${LABEL}: true' to your values files (or re-run with --add-to-autoshift)"
        echo "   Also declare 'autoshift.io/${LABEL}' in autoshift/values/clustersets/_example.yaml (label-contract CI)"
    fi
fi
echo ""
