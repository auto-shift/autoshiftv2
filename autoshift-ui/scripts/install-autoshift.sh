#!/bin/bash
# AutoShift Installation Script
# Deploys AutoShift via ArgoCD Application

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

# Default values file
VALUES_FILE="${1:-hub}"

log "AutoShift Installation"
log "======================"
log "Version: ${VERSION}"
log "Registry: ${OCI_REPO}"
log "Values: ${VALUES_FILE}"
echo ""

# Check prerequisites
command -v oc >/dev/null 2>&1 || error "oc CLI is required"

# Check cluster connection
oc whoami >/dev/null 2>&1 || error "Not logged in to OpenShift. Run: oc login"

# Map values file names
case "$VALUES_FILE" in
    hub)
        VALUES_FILE_PATH="values.hub.yaml"
        ;;
    minimal|min)
        VALUES_FILE_PATH="values.minimal.yaml"
        ;;
    sbx|sandbox)
        VALUES_FILE_PATH="values.sbx.yaml"
        ;;
    hubofhubs|hoh)
        VALUES_FILE_PATH="values.hubofhubs.yaml"
        ;;
    *)
        error "Unknown values file: $VALUES_FILE. Use: hub, minimal, sbx, or hubofhubs"
        ;;
esac

log "Creating ArgoCD Application for AutoShift..."

cat <<EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  project: default
  source:
    path: .
    repoURL: ${OCI_REPO}/autoshift
    targetRevision: "${VERSION}"
    helm:
      valueFiles:
        - ${VALUES_FILE_PATH}
      values: |
        # Enable OCI registry mode for ApplicationSet
        autoshiftOciRegistry: true
        autoshiftOciRepo: ${OCI_REPO}/policies
        autoshiftOciVersion: "${VERSION}"
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
EOF

log "✓ AutoShift Application created"
echo ""

log "Monitoring sync status..."
sleep 5
oc get application autoshift -n openshift-gitops

echo ""
log "========================================="
log "AutoShift installation initiated!"
log "========================================="
echo ""
log "Monitor deployment:"
echo "  oc get application autoshift -n openshift-gitops -w"
echo "  oc get applicationset -n openshift-gitops"
echo "  oc get applications -n openshift-gitops | grep autoshift"
echo ""
log "View policies:"
echo "  oc get policies -A"
echo ""
log "Access ArgoCD UI:"
echo "  oc get route argocd-server -n openshift-gitops"
