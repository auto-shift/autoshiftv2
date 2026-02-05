#!/bin/bash
# AutoShift Bootstrap Installation Script
# Installs OpenShift GitOps and Advanced Cluster Management from OCI registry

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

VERSION="0.0.1"
REGISTRY="quay.io"
REGISTRY_NAMESPACE="autoshift"
OCI_REPO="oci://${REGISTRY}/${REGISTRY_NAMESPACE}"
OCI_BOOTSTRAP_REPO="oci://${REGISTRY}/${REGISTRY_NAMESPACE}/bootstrap"

log "AutoShift Bootstrap Installation"
log "================================="
log "Version: ${VERSION}"
log "Registry: ${OCI_REPO}"
echo ""

# Check prerequisites
command -v oc >/dev/null 2>&1 || error "oc CLI is required"
command -v helm >/dev/null 2>&1 || error "helm is required"

# Check cluster connection
oc whoami >/dev/null 2>&1 || error "Not logged in to OpenShift. Run: oc login"

log "Installing OpenShift GitOps..."
helm upgrade --install openshift-gitops ${OCI_BOOTSTRAP_REPO}/openshift-gitops \
    --version ${VERSION} \
    --create-namespace \
    --wait \
    --timeout 10m

log "✓ OpenShift GitOps installed"
echo ""

log "Installing Advanced Cluster Management..."
helm upgrade --install advanced-cluster-management ${OCI_BOOTSTRAP_REPO}/advanced-cluster-management \
    --version ${VERSION} \
    --create-namespace \
    --wait \
    --timeout 15m

log "✓ Advanced Cluster Management installed"
echo ""

log "Waiting for ACM MultiClusterHub to be ready (this may take 10+ minutes)..."
oc wait --for=condition=Complete multiclusterhub multiclusterhub \
    -n open-cluster-management --timeout=900s 2>/dev/null || \
    warn "MultiClusterHub readiness check timed out - check status manually with: oc get mch -n open-cluster-management"

echo ""
log "========================================="
log "Bootstrap installation complete!"
log "========================================="
echo ""
log "Next steps:"
echo "  1. Verify GitOps: oc get pods -n openshift-gitops"
echo "  2. Verify ACM: oc get mch -n open-cluster-management"
echo "  3. Install AutoShift: ./install-autoshift.sh"
