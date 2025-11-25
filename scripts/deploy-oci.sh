#!/bin/bash
# AutoShift OCI Deployment Helper
# Simplifies deploying AutoShift from OCI registries with pre-built values files

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# Defaults
REGISTRY="${REGISTRY:-oci://quay.io/autoshift}"
VERSION="${VERSION:-}"
VALUES_FILE="${VALUES_FILE:-hub}"
NAMESPACE="${NAMESPACE:-openshift-gitops}"
RELEASE_NAME="${RELEASE_NAME:-autoshift}"
DRY_RUN="${DRY_RUN:-false}"
METHOD="${METHOD:-argocd}"
OCI_POLICIES="${OCI_POLICIES:-false}"
POLICIES_REGISTRY="${POLICIES_REGISTRY:-}"

usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "Deploy AutoShift from OCI registry with pre-built configurations"
    echo ""
    echo "Options:"
    echo "  --registry REGISTRY        OCI registry path (default: oci://quay.io/autoshift)"
    echo "  --version VERSION          Chart version to deploy [required]"
    echo "  --values VALUES            Pre-built values file to use:"
    echo "                               hub (default) - Standard hub cluster"
    echo "                               minimal - Minimal required configuration (GitOps + ACM only)"
    echo "                               sbx - Sandbox/dev environment"
    echo "                               hubofhubs - Hub of hubs configuration"
    echo "                               baremetal-sno - Baremetal single-node"
    echo "                               baremetal-compact - Baremetal compact cluster"
    echo "  --oci-policies             Deploy policies from OCI registry (instead of Git)"
    echo "  --policies-registry PATH   OCI path for policies (default: same as --registry with /policies)"
    echo "  --namespace NAMESPACE      Kubernetes namespace (default: openshift-gitops)"
    echo "  --name NAME                Release name (default: autoshift)"
    echo "  --method METHOD            Deployment method:"
    echo "                               argocd (default) - Create ArgoCD Application"
    echo "                               helm - Use Helm CLI directly"
    echo "  --dry-run                  Show what would be deployed"
    echo "  --help                     Show this help"
    echo ""
    echo "Environment Variables:"
    echo "  REGISTRY      OCI registry path"
    echo "  VERSION       Chart version"
    echo "  VALUES_FILE   Values file name"
    echo "  METHOD        Deployment method"
    echo ""
    echo "Examples:"
    echo "  # Deploy hub configuration with policies from Git"
    echo "  $0 --version 1.0.0"
    echo ""
    echo "  # Deploy hub configuration with policies from OCI"
    echo "  $0 --version 1.0.0 --oci-policies"
    echo ""
    echo "  # Deploy sandbox configuration with Helm"
    echo "  $0 --version 1.0.0 --values sbx --method helm"
    echo ""
    echo "  # Deploy from Quay with hub-of-hubs values and OCI policies"
    echo "  $0 --registry oci://quay.io/autoshift --version 1.0.0 --values hubofhubs --oci-policies"
    echo ""
    echo "  # Dry run to see what would be deployed"
    echo "  $0 --version 1.0.0 --oci-policies --dry-run"
    echo ""
    echo "  # Gradual rollout: deploy multiple versions to different clustersets"
    echo "  $0 --version 1.0.0 --name autoshift-stable --oci-policies"
    echo "  $0 --version 1.0.1 --name autoshift-canary --oci-policies"
    echo "  # See GRADUAL-ROLLOUT.md for complete guide"
}

log() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --version)
            VERSION="$2"
            shift 2
            ;;
        --values)
            VALUES_FILE="$2"
            shift 2
            ;;
        --namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        --name)
            RELEASE_NAME="$2"
            shift 2
            ;;
        --method)
            METHOD="$2"
            shift 2
            ;;
        --oci-policies)
            OCI_POLICIES=true
            shift
            ;;
        --policies-registry)
            POLICIES_REGISTRY="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --help)
            usage
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            ;;
    esac
done

# Validate
if [ -z "$VERSION" ]; then
    error "Version is required. Use --version or set VERSION environment variable"
fi

if [[ ! "$METHOD" =~ ^(argocd|helm)$ ]]; then
    error "Invalid method: $METHOD. Must be 'argocd' or 'helm'"
fi

# Map values file names to actual files
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
    baremetal-sno|sno)
        VALUES_FILE_PATH="values.hub.baremetal-sno.yaml"
        ;;
    baremetal-compact|compact)
        VALUES_FILE_PATH="values.hub.baremetal-compact.yaml"
        ;;
    *)
        error "Unknown values file: $VALUES_FILE. Use: hub, minimal, sbx, hubofhubs, baremetal-sno, or baremetal-compact"
        ;;
esac

# Set policies registry if OCI mode enabled
if [ "$OCI_POLICIES" = "true" ] && [ -z "$POLICIES_REGISTRY" ]; then
    # Default: use same registry with /policies suffix
    POLICIES_REGISTRY="${REGISTRY}/policies"
fi

log "AutoShift OCI Deployment"
log "========================"
log "Registry: $REGISTRY"
log "Version: $VERSION"
log "Values: $VALUES_FILE_PATH"
log "Method: $METHOD"
log "Namespace: $NAMESPACE"
log "Release: $RELEASE_NAME"
log "Dry Run: $DRY_RUN"
if [ "$OCI_POLICIES" = "true" ]; then
    log "OCI Policies: Enabled (from $POLICIES_REGISTRY)"
else
    log "OCI Policies: Disabled (using Git discovery)"
fi
echo ""

if [ "$METHOD" = "argocd" ]; then
    log "Creating ArgoCD Application..."

    # Check if oc is available
    command -v oc >/dev/null 2>&1 || error "oc CLI is required for ArgoCD deployment"

    # Check if logged in
    oc whoami >/dev/null 2>&1 || error "Not logged in to OpenShift cluster. Run: oc login"

    # Build OCI values override if enabled
    OCI_VALUES=""
    if [ "$OCI_POLICIES" = "true" ]; then
        OCI_VALUES="      values: |
        autoshiftOciRegistry: true
        autoshiftOciRepo: ${POLICIES_REGISTRY}
        autoshiftOciVersion: \"${VERSION}\""
    fi

    # Create Application manifest
    APP_MANIFEST=$(cat <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ${RELEASE_NAME}
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: ${REGISTRY}
    chart: autoshift
    targetRevision: "${VERSION}"
    helm:
      valueFiles:
        - ${VALUES_FILE_PATH}
${OCI_VALUES}
  destination:
    server: https://kubernetes.default.svc
    namespace: ${NAMESPACE}
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
EOF
)

    if [ "$DRY_RUN" = "true" ]; then
        warn "DRY RUN: Would create this Application:"
        echo ""
        echo "$APP_MANIFEST"
        echo ""
        log "To apply: $0 --version $VERSION --values $VALUES_FILE"
    else
        echo "$APP_MANIFEST" | oc apply -f -
        log "✓ ArgoCD Application created"
        echo ""
        log "Monitor deployment:"
        echo "  oc get application ${RELEASE_NAME} -n openshift-gitops -w"
        echo ""
        log "View in ArgoCD UI:"
        echo "  oc get route argocd-server -n openshift-gitops"
    fi

elif [ "$METHOD" = "helm" ]; then
    log "Deploying with Helm CLI..."

    # Check if helm is available
    command -v helm >/dev/null 2>&1 || error "helm is required for Helm deployment"

    # Pull chart to temp directory
    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    log "Pulling chart from registry..."
    cd "$TEMP_DIR"
    if ! helm pull "${REGISTRY}/autoshift" --version "$VERSION"; then
        error "Failed to pull chart. You may need to login: helm registry login <registry>"
    fi

    log "Extracting chart..."
    tar -xzf autoshift-${VERSION}.tgz

    if [ "$DRY_RUN" = "true" ]; then
        warn "DRY RUN: Would install with these values:"
        echo ""
        cat "autoshift/${VALUES_FILE_PATH}" | head -50
        echo ""
        echo "... (values file truncated)"
        echo ""
        log "To install: $0 --version $VERSION --values $VALUES_FILE --method helm"
    else
        log "Installing chart..."
        helm install "$RELEASE_NAME" ./autoshift \
            -f "autoshift/${VALUES_FILE_PATH}" \
            --namespace "$NAMESPACE" \
            --create-namespace

        log "✓ Helm release installed"
        echo ""
        log "Check status:"
        echo "  helm status ${RELEASE_NAME} -n ${NAMESPACE}"
        echo ""
        log "Verify deployment:"
        echo "  oc get applications -n ${NAMESPACE}"
        echo "  oc get policies -A"
    fi
fi

echo ""
log "Deployment complete!"
