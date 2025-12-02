#!/bin/bash
# Sync bootstrap chart values from policy chart values
# This ensures bootstrap installs with the same defaults that policies will enforce
# Usage: ./sync-bootstrap-values.sh

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Check for yq
if ! command -v yq &> /dev/null; then
    echo "yq is required but not installed. Install with: brew install yq"
    exit 1
fi

log "Syncing bootstrap values from policy charts..."
echo ""

# =============================================================================
# Sync OpenShift GitOps values
# =============================================================================
log "Syncing openshift-gitops values..."

GITOPS_POLICY_VALUES="$REPO_ROOT/policies/openshift-gitops/values.yaml"
GITOPS_BOOTSTRAP_VALUES="$REPO_ROOT/openshift-gitops/values.yaml"

if [ ! -f "$GITOPS_POLICY_VALUES" ]; then
    warn "Policy values not found: $GITOPS_POLICY_VALUES"
    exit 1
fi

# Generate bootstrap values from policy values
cat > "$GITOPS_BOOTSTRAP_VALUES" << 'EOF'
# =============================================================================
# AUTO-GENERATED - DO NOT EDIT DIRECTLY
# =============================================================================
# This file is generated from policies/openshift-gitops/values.yaml
# To modify defaults, edit the policy chart values and run:
#   make sync-values
# =============================================================================

# Bootstrap-specific settings
ignoreHelmHooks: false

# Job image for waiting for CRD
# Use internal registry for disconnected environments
image: image-registry.openshift-image-registry.svc:5000/openshift/cli:latest

# Git repository secrets (optional)
secrets: []
# EXAMPLE:
# secrets:
#   - name: git-auth
#     username: 'user'
#     password: 'pass1234'
#     sshPrivateKey: ''

# =============================================================================
# Values synced from policies/openshift-gitops/values.yaml
# =============================================================================
EOF

echo "" >> "$GITOPS_BOOTSTRAP_VALUES"
echo "gitops:" >> "$GITOPS_BOOTSTRAP_VALUES"
yq eval '.gitops' "$GITOPS_POLICY_VALUES" | sed 's/^/  /' >> "$GITOPS_BOOTSTRAP_VALUES"

# Also sync disableDefaultArgoCD at root level (used by subscription template)
DISABLE_DEFAULT=$(yq eval '.gitops.disableDefaultArgoCD' "$GITOPS_POLICY_VALUES")
echo "" >> "$GITOPS_BOOTSTRAP_VALUES"
echo "# Disable default ArgoCD instance (we create our own)" >> "$GITOPS_BOOTSTRAP_VALUES"
echo "disableDefaultArgoCD: $DISABLE_DEFAULT" >> "$GITOPS_BOOTSTRAP_VALUES"

success "Synced openshift-gitops values"

# =============================================================================
# Sync Advanced Cluster Management values
# =============================================================================
log "Syncing advanced-cluster-management values..."

ACM_POLICY_VALUES="$REPO_ROOT/policies/advanced-cluster-management/values.yaml"
ACM_BOOTSTRAP_VALUES="$REPO_ROOT/advanced-cluster-management/values.yaml"

if [ ! -f "$ACM_POLICY_VALUES" ]; then
    warn "Policy values not found: $ACM_POLICY_VALUES"
    exit 1
fi

cat > "$ACM_BOOTSTRAP_VALUES" << 'EOF'
# =============================================================================
# AUTO-GENERATED - DO NOT EDIT DIRECTLY
# =============================================================================
# This file is generated from policies/advanced-cluster-management/values.yaml
# To modify defaults, edit the policy chart values and run:
#   make sync-values
# =============================================================================

# Bootstrap-specific settings
ignoreHelmHooks: false

# Job image for waiting for CRD
# Use internal registry for disconnected environments
image: image-registry.openshift-image-registry.svc:5000/openshift/cli:latest

# =============================================================================
# Values synced from policies/advanced-cluster-management/values.yaml
# =============================================================================

EOF

# Add acm section with installPlanApproval injected
yq eval '.acm.installPlanApproval = "Automatic"' "$ACM_POLICY_VALUES" | yq eval '{"acm": .acm}' - >> "$ACM_BOOTSTRAP_VALUES"

success "Synced advanced-cluster-management values"

echo ""
log "========================================="
success "Bootstrap values synced from policy charts"
log "========================================="
echo ""
echo "Updated files:"
echo "  - openshift-gitops/values.yaml"
echo "  - advanced-cluster-management/values.yaml"
echo ""
echo "These files are now in sync with their respective policy charts."
echo "The policies will manage these operators after initial bootstrap."
