#!/bin/bash
# Generate installation artifacts for OCI release
# Usage: ./generate-install-artifacts.sh VERSION REGISTRY REGISTRY_NAMESPACE ARTIFACTS_DIR

set -e

VERSION="$1"
REGISTRY="$2"
REGISTRY_NAMESPACE="$3"
ARTIFACTS_DIR="$4"

if [ -z "$VERSION" ] || [ -z "$REGISTRY" ] || [ -z "$REGISTRY_NAMESPACE" ] || [ -z "$ARTIFACTS_DIR" ]; then
    echo "Usage: $0 VERSION REGISTRY REGISTRY_NAMESPACE ARTIFACTS_DIR"
    exit 1
fi

mkdir -p "$ARTIFACTS_DIR"

echo "Generating installation artifacts for version $VERSION..."

# Generate bootstrap installation script
cat > "$ARTIFACTS_DIR/install-bootstrap.sh" << 'INSTALL_EOF'
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

INSTALL_EOF

cat >> "$ARTIFACTS_DIR/install-bootstrap.sh" << INSTALL_VARS
VERSION="${VERSION}"
REGISTRY="${REGISTRY}"
REGISTRY_NAMESPACE="${REGISTRY_NAMESPACE}"
OCI_REPO="oci://\${REGISTRY}/\${REGISTRY_NAMESPACE}"
OCI_BOOTSTRAP_REPO="oci://\${REGISTRY}/\${REGISTRY_NAMESPACE}/bootstrap"
OCI_REGISTRY="\${REGISTRY}/\${REGISTRY_NAMESPACE}"
INSTALL_VARS

cat >> "$ARTIFACTS_DIR/install-bootstrap.sh" << 'INSTALL_EOF'

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
INSTALL_EOF

chmod +x "$ARTIFACTS_DIR/install-bootstrap.sh"

# Generate AutoShift installation script
cat > "$ARTIFACTS_DIR/install-autoshift.sh" << 'AUTOSHIFT_EOF'
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

usage() {
    echo "Usage: $0 [OPTIONS] [VALUES_FILE]"
    echo ""
    echo "Arguments:"
    echo "  VALUES_FILE    Values profile to use: hub, minimal, sbx, hubofhubs (default: hub)"
    echo ""
    echo "Options:"
    echo "  --versioned    Enable versioned ClusterSets for gradual rollout"
    echo "                 - Application name includes version (e.g., autoshift-0-0-1)"
    echo "                 - ClusterSet names include version suffix (e.g., hub-0-0-1)"
    echo "                 - Allows multiple versions to run side-by-side"
    echo "  --dry-run      Enable dry run mode (policies report but don't enforce)"
    echo "  --name NAME    Custom application name (default: autoshift or autoshift-VERSION)"
    echo "  --gitops-namespace NS  GitOps namespace (default: openshift-gitops)"
    echo "  -h, --help     Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 hub                                    # Standard deployment"
    echo "  $0 --versioned hub                        # Versioned deployment for gradual rollout"
    echo "  $0 --dry-run hub                          # Dry run mode"
    echo "  $0 --versioned --dry-run                  # Versioned + dry run"
    echo "  $0 --gitops-namespace custom-gitops hub   # Custom GitOps namespace"
    exit 0
}

AUTOSHIFT_EOF

cat >> "$ARTIFACTS_DIR/install-autoshift.sh" << AUTOSHIFT_VARS
VERSION="${VERSION}"
REGISTRY="${REGISTRY}"
REGISTRY_NAMESPACE="${REGISTRY_NAMESPACE}"
OCI_REPO="oci://\${REGISTRY}/\${REGISTRY_NAMESPACE}"
OCI_REGISTRY="\${REGISTRY}/\${REGISTRY_NAMESPACE}"
AUTOSHIFT_VARS

cat >> "$ARTIFACTS_DIR/install-autoshift.sh" << 'AUTOSHIFT_EOF'

# Parse arguments
VERSIONED=false
DRY_RUN=false
CUSTOM_NAME=""
VALUES_FILE="hub"
GITOPS_NAMESPACE="openshift-gitops"

while [[ $# -gt 0 ]]; do
    case $1 in
        --versioned)
            VERSIONED=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --name)
            CUSTOM_NAME="$2"
            shift 2
            ;;
        --gitops-namespace)
            GITOPS_NAMESPACE="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        -*)
            error "Unknown option: $1"
            ;;
        *)
            VALUES_FILE="$1"
            shift
            ;;
    esac
done

# Sanitize version for DNS-compatible names (dots -> dashes)
VERSION_SUFFIX=$(echo "${VERSION}" | tr '.' '-' | tr '/' '-' | tr '[:upper:]' '[:lower:]')

# Determine application name
if [ -n "$CUSTOM_NAME" ]; then
    APP_NAME="$CUSTOM_NAME"
elif [ "$VERSIONED" = true ]; then
    APP_NAME="autoshift-${VERSION_SUFFIX}"
else
    APP_NAME="autoshift"
fi

log "AutoShift Installation"
log "======================"
log "Version: ${VERSION}"
log "Registry: ${OCI_REPO}"
log "Values: ${VALUES_FILE}"
log "Application: ${APP_NAME}"
[ "$VERSIONED" = true ] && log "Mode: Versioned ClusterSets (gradual rollout)"
[ "$DRY_RUN" = true ] && log "Mode: Dry Run (policies won't enforce)"
echo ""

# Check prerequisites
command -v oc >/dev/null 2>&1 || error "oc CLI is required"

# Check cluster connection
oc whoami >/dev/null 2>&1 || error "Not logged in to OpenShift. Run: oc login"

# Map values file names to composable values files
case "$VALUES_FILE" in
    hub)
        VALUES_FILE_PATHS=("values/global.yaml" "values/clustersets/hub.yaml" "values/clustersets/managed.yaml")
        ;;
    minimal|min)
        VALUES_FILE_PATHS=("values/global.yaml" "values/clustersets/hub-minimal.yaml")
        ;;
    sbx|sandbox)
        VALUES_FILE_PATHS=("values/global.yaml" "values/clustersets/sbx.yaml")
        ;;
    hubofhubs|hoh)
        VALUES_FILE_PATHS=("values/global.yaml" "values/clustersets/hubofhubs.yaml" "values/clustersets/hub1.yaml" "values/clustersets/hub2.yaml")
        ;;
    *)
        error "Unknown values file: $VALUES_FILE. Use: hub, minimal, sbx, or hubofhubs"
        ;;
esac

# Build values override
VALUES_OVERRIDE="# Enable OCI registry mode for ApplicationSet
        autoshiftOciRegistry: true
        autoshiftOciRepo: ${OCI_REPO}/policies
        autoshiftOciVersion: \"${VERSION}\"
        gitopsNamespace: ${GITOPS_NAMESPACE}"

if [ "$VERSIONED" = true ]; then
    VALUES_OVERRIDE="${VALUES_OVERRIDE}
        # Enable versioned ClusterSets for gradual rollout
        versionedClusterSets: true"
fi

if [ "$DRY_RUN" = true ]; then
    VALUES_OVERRIDE="${VALUES_OVERRIDE}
        # Dry run mode - policies report but don't enforce
        autoshift:
          dryRun: true"
fi

# Build valueFiles YAML entries
VALUEFILES_YAML=""
for f in "${VALUES_FILE_PATHS[@]}"; do
    VALUEFILES_YAML="${VALUEFILES_YAML}        - ${f}
"
done

log "Creating ArgoCD Application for AutoShift..."

cat <<EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ${APP_NAME}
  namespace: ${GITOPS_NAMESPACE}
spec:
  project: default
  source:
    repoURL: ${OCI_REGISTRY}
    chart: autoshift
    targetRevision: "${VERSION}"
    helm:
      valueFiles:
${VALUEFILES_YAML}      values: |
        ${VALUES_OVERRIDE}
  destination:
    server: https://kubernetes.default.svc
    namespace: ${GITOPS_NAMESPACE}
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
oc get application ${APP_NAME} -n ${GITOPS_NAMESPACE}

echo ""
log "========================================="
log "AutoShift installation initiated!"
log "========================================="
echo ""
log "Monitor deployment:"
echo "  oc get application ${APP_NAME} -n ${GITOPS_NAMESPACE} -w"
echo "  oc get applicationset -n ${GITOPS_NAMESPACE}"
echo "  oc get applications -n ${GITOPS_NAMESPACE} | grep ${APP_NAME}"
echo ""
log "View policies:"
echo "  oc get policies -A"

if [ "$VERSIONED" = true ]; then
    echo ""
    log "Versioned ClusterSets created:"
    echo "  Hub ClusterSet: hub-${VERSION_SUFFIX}"
    echo "  Managed ClusterSet: managed-${VERSION_SUFFIX}"
    echo ""
    log "Assign clusters to this version:"
    echo "  oc label managedcluster <cluster-name> cluster.open-cluster-management.io/clusterset=hub-${VERSION_SUFFIX} --overwrite"
fi

echo ""
log "Access ArgoCD UI:"
echo "  oc get route argocd-server -n ${GITOPS_NAMESPACE}"
AUTOSHIFT_EOF

chmod +x "$ARTIFACTS_DIR/install-autoshift.sh"

# Generate comprehensive installation guide
cat > "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'
# AutoShift Installation Guide
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
**Version:** ${VERSION}
**Registry:** oci://${REGISTRY}/${REGISTRY_NAMESPACE}

GUIDE_VERSION

cat >> "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'
## Overview

AutoShift provides a complete Infrastructure-as-Code solution for OpenShift using:
- **OpenShift GitOps** (ArgoCD) - For declarative application deployment
- **Red Hat Advanced Cluster Management** - For policy-based cluster management
- **AutoShift Policies** - Pre-configured ACM policies for Day 2 operations

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Phase 1: Bootstrap (Helm direct install)              │
│  ├─ OpenShift GitOps Operator                          │
│  └─ Advanced Cluster Management Operator               │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│  Phase 2: Deploy AutoShift (via ArgoCD Application)    │
│  └─ AutoShift Chart → ApplicationSet                   │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│  Phase 3: Policy Deployment (via ApplicationSet)       │
│  ├─ ACM Policy Charts from OCI Registry                │
│  ├─ policies/openshift-gitops (takes over GitOps)      │
│  └─ policies/advanced-cluster-management (takes over)  │
└─────────────────────────────────────────────────────────┘
```

## Prerequisites

- OpenShift cluster (4.14+) with cluster-admin access
- `oc` CLI installed and logged in
- `helm` CLI installed (3.14+)
- Access to OCI registry (if private, configure authentication)

## Installation Methods

### Quick Start: Automated Installation

Use the provided scripts for a streamlined installation:

```bash
# Step 1: Install bootstrap operators (GitOps + ACM)
./install-bootstrap.sh

# Step 2: Install AutoShift (deploys policies)
./install-autoshift.sh hub  # Use 'hub' values file

# Optional flags:
#   --versioned    Enable versioned ClusterSets for gradual rollout
#   --dry-run      Test policies without enforcement
#   --help         Show all options
```

### Manual Installation

#### Step 1: Configure OCI Registry Authentication (if private)

```bash
# Example for GitHub Container Registry
helm registry login ghcr.io -u YOUR_USERNAME -p YOUR_TOKEN

# Example for Quay
helm registry login quay.io -u YOUR_USERNAME -p YOUR_TOKEN
```

#### Step 2: Install Bootstrap Charts

**Install OpenShift GitOps:**

```bash
helm upgrade --install openshift-gitops \
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
  oci://${REGISTRY}/${REGISTRY_NAMESPACE}/bootstrap/openshift-gitops \\
  --version ${VERSION} \\
GUIDE_VERSION

cat >> "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'
  --create-namespace \
  --wait \
  --timeout 10m

# Verify installation
oc get pods -n openshift-gitops
oc wait --for=condition=ready pod -l app.kubernetes.io/name=openshift-gitops-server \
  -n openshift-gitops --timeout=300s
```

**Install Advanced Cluster Management:**

```bash
helm upgrade --install advanced-cluster-management \
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
  oci://${REGISTRY}/${REGISTRY_NAMESPACE}/bootstrap/advanced-cluster-management \\
  --version ${VERSION} \\
GUIDE_VERSION

cat >> "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'
  --create-namespace \
  --wait \
  --timeout 15m

# Verify installation (may take 10+ minutes)
oc get multiclusterhub -n open-cluster-management
oc wait --for=condition=Complete multiclusterhub multiclusterhub \
  -n open-cluster-management --timeout=900s
```

#### Step 3: Deploy AutoShift

Create an ArgoCD Application to deploy AutoShift:

```bash
cat <<EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  project: default
  source:
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
    repoURL: ${REGISTRY}/${REGISTRY_NAMESPACE}
    chart: autoshift
    targetRevision: "${VERSION}"
GUIDE_VERSION

cat >> "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'
    helm:
      valueFiles:
        - values/global.yaml
        - values/clustersets/hub.yaml          # Or other clusterset profile
        - values/clustersets/managed.yaml      # Add managed spoke clusters
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
```

#### Step 4: Verify Deployment

```bash
# Check AutoShift Application
oc get application autoshift -n openshift-gitops

# Check ApplicationSet (deploying policy charts)
oc get applicationset -n openshift-gitops

# Check individual policy Applications
oc get applications -n openshift-gitops | grep autoshift

# Verify ACM policies are created
oc get policies -A
```

## Configuration

### Values File Architecture

The AutoShift chart uses a composable values directory structure. Compose your
deployment by combining multiple values files:

- **values/global.yaml** - Shared configuration (git repo, branch, settings)
- **values/clustersets/hub.yaml** - Standard hub cluster labels
- **values/clustersets/hub-minimal.yaml** - Minimal hub (GitOps + ACM only)
- **values/clustersets/managed.yaml** - Managed spoke cluster labels
- **values/clustersets/sbx.yaml** - Sandbox environment labels
- **values/clustersets/hubofhubs.yaml** - Hub-of-hubs configuration
- **values/clustersets/hub-baremetal-sno.yaml** - Baremetal single-node hub
- **values/clustersets/hub-baremetal-compact.yaml** - Baremetal compact hub

### Deploying from OCI Registry (Recommended)

When deploying from an OCI registry, use your existing values file and override the OCI settings:

\`\`\`bash
cat <<EOFAPP | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  project: default
  source:
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
    repoURL: ${REGISTRY}/${REGISTRY_NAMESPACE}
    chart: autoshift
    targetRevision: "${VERSION}"
GUIDE_VERSION

cat >> "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'
    helm:
      valueFiles:
        - values/global.yaml
        - values/clustersets/hub.yaml          # Or other clusterset profile
        - values/clustersets/managed.yaml      # Add managed spoke clusters
      values: |
        # Enable OCI mode for policy deployment
        autoshiftOciRegistry: true
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
        autoshiftOciRepo: oci://${REGISTRY}/${REGISTRY_NAMESPACE}/policies
        autoshiftOciVersion: "${VERSION}"
GUIDE_VERSION

cat >> "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
EOFAPP
\`\`\`

### Customizing Configuration

You can customize the deployment by providing your own values:

```bash
# Create custom values
cat > my-values.yaml <<EOF
autoshift:
  dryRun: false

hubClusterSets:
  hub:
    labels:
      self-managed: 'true'
      openshift-version: '4.18.28'
      acs: 'true'
      acs-channel: 'stable'
      odf: 'true'
      odf-channel: 'stable-4.18'
EOF

# Deploy with custom values
cat <<EOFAPP | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  project: default
  source:
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
    repoURL: ${REGISTRY}/${REGISTRY_NAMESPACE}
    chart: autoshift
    targetRevision: "${VERSION}"
GUIDE_VERSION

cat >> "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'
    helm:
      valuesObject: |
        # Paste contents of my-values.yaml here
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
EOFAPP
```

## Gradual Rollout with Multiple Versions

AutoShift supports running multiple versions side-by-side for gradual rollout:

```bash
# Deploy first version with versioned ClusterSets
./install-autoshift.sh --versioned hub

# This creates:
# - Application: autoshift-X-Y-Z (e.g., autoshift-1-0-0)
# - Hub ClusterSet: hub-X-Y-Z (e.g., hub-1-0-0)
# - Managed ClusterSet: managed-X-Y-Z (e.g., managed-1-0-0)

# Assign clusters to this version
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub-1-0-0 --overwrite
oc label managedcluster spoke-1 cluster.open-cluster-management.io/clusterset=managed-1-0-0 --overwrite
```

When a new version is released, deploy it alongside:

```bash
# Deploy new version (after updating scripts from new release)
./install-autoshift.sh --versioned hub

# Migrate clusters one at a time
oc label managedcluster spoke-1 cluster.open-cluster-management.io/clusterset=managed-2-0-0 --overwrite

# After validation, migrate remaining clusters
# Then delete old version
oc delete application autoshift-1-0-0 -n openshift-gitops
```

See [Gradual Rollout Guide](https://github.com/auto-shift/autoshiftv2/blob/main/docs/gradual-rollout.md) for detailed instructions.

## Testing with Dry Run Mode

Test policy changes without enforcement:

```bash
# Deploy in dry run mode
./install-autoshift.sh --dry-run hub

# Policies will report compliance status but won't make changes
oc get policies -A

# Switch to enforcement
./install-autoshift.sh hub  # Re-run without --dry-run
```

## Testing with Release Candidates

For testing in non-production environments, use release candidate versions:

```bash
# Test with RC version
./install-bootstrap.sh
./install-autoshift.sh hub

# After validation, upgrade to production release
```

## Policy Management

AutoShift deploys ACM policies that manage various OpenShift components:

- **Infrastructure**: infra-nodes, worker-nodes, storage-nodes
- **Operators**: ACS, ODF, Logging, Loki, GitOps, Pipelines, etc.
- **Configuration**: DNS, Image Registry, Compliance, etc.

### Policy Takeover

After AutoShift is deployed:
1. `policies/openshift-gitops` policy takes over management of the GitOps operator
2. `policies/advanced-cluster-management` policy takes over management of ACM

This allows ACM to manage its own upgrades and configuration via GitOps.

## Troubleshooting

### GitOps Installation Issues

```bash
# Check operator status
oc get csv -n openshift-gitops-operator

# Check pod status
oc get pods -n openshift-gitops

# View operator logs
oc logs -n openshift-gitops-operator -l control-plane=controller-manager
```

### ACM Installation Issues

```bash
# Check MultiClusterHub status
oc get mch -n open-cluster-management

# Check operator status
oc get csv -n open-cluster-management

# View MCH details
oc describe mch multiclusterhub -n open-cluster-management
```

### AutoShift Application Issues

```bash
# Check Application sync status
oc get application autoshift -n openshift-gitops -o yaml

# Check ApplicationSet status
oc get applicationset -n openshift-gitops -o yaml

# View ArgoCD UI
oc get route argocd-server -n openshift-gitops
```

### Policy Issues

```bash
# Check policy compliance
oc get policies -A

# View specific policy details
oc describe policy <policy-name> -n <namespace>

# Check policy status
oc get policy <policy-name> -n <namespace> -o yaml
```

## Upgrading

To upgrade to a new version:

```bash
# Upgrade bootstrap charts
helm upgrade openshift-gitops \
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
  oci://${REGISTRY}/${REGISTRY_NAMESPACE}/bootstrap/openshift-gitops \\
  --version <NEW_VERSION>

helm upgrade advanced-cluster-management \\
  oci://${REGISTRY}/${REGISTRY_NAMESPACE}/bootstrap/advanced-cluster-management \\
  --version <NEW_VERSION>
GUIDE_VERSION

cat >> "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'

# Upgrade AutoShift Application
oc patch application autoshift -n openshift-gitops \
  --type=merge \
  -p '{"spec":{"source":{"targetRevision":"<NEW_VERSION>"}}}'
```

## Air-Gapped / Disconnected Environments

For disconnected environments:

1. Mirror all charts to internal registry:
```bash
# Pull charts
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
helm pull oci://${REGISTRY}/${REGISTRY_NAMESPACE}/bootstrap/openshift-gitops --version ${VERSION}
helm pull oci://${REGISTRY}/${REGISTRY_NAMESPACE}/bootstrap/advanced-cluster-management --version ${VERSION}
helm pull oci://${REGISTRY}/${REGISTRY_NAMESPACE}/autoshift --version ${VERSION}
GUIDE_VERSION

cat >> "$ARTIFACTS_DIR/INSTALL.md" << 'GUIDE_EOF'

# Push to internal registry
helm push openshift-gitops-*.tgz oci://harbor.internal.com/autoshift
helm push advanced-cluster-management-*.tgz oci://harbor.internal.com/autoshift
helm push autoshift-*.tgz oci://harbor.internal.com/autoshift
```

2. Update values to use internal registry in the AutoShift Application

## Support

- Documentation: https://github.com/auto-shift/autoshiftv2
- Issues: https://github.com/auto-shift/autoshiftv2/issues
GUIDE_EOF

cat >> "$ARTIFACTS_DIR/INSTALL.md" << GUIDE_VERSION
- Release Notes: https://github.com/auto-shift/autoshiftv2/releases/tag/v${VERSION}
GUIDE_VERSION

# Generate charts list
cat > "$ARTIFACTS_DIR/charts.txt" << CHARTS_EOF
# AutoShift ${VERSION} - Published Charts

## Bootstrap Charts
CHARTS_EOF

cat >> "$ARTIFACTS_DIR/charts.txt" << CHARTS_VERSION
oci://${REGISTRY}/${REGISTRY_NAMESPACE}/bootstrap/openshift-gitops:${VERSION}
oci://${REGISTRY}/${REGISTRY_NAMESPACE}/bootstrap/advanced-cluster-management:${VERSION}
CHARTS_VERSION

cat >> "$ARTIFACTS_DIR/charts.txt" << CHARTS_EOF

## Main Chart
CHARTS_EOF

cat >> "$ARTIFACTS_DIR/charts.txt" << CHARTS_VERSION
oci://${REGISTRY}/${REGISTRY_NAMESPACE}/autoshift:${VERSION}
CHARTS_VERSION

cat >> "$ARTIFACTS_DIR/charts.txt" << CHARTS_EOF

## Policy Charts
CHARTS_EOF

find policies -maxdepth 2 -name Chart.yaml | while read -r chart_file; do
    policy_dir=$(dirname "$chart_file")
    policy_name=$(basename "$policy_dir")
    echo "oci://${REGISTRY}/${REGISTRY_NAMESPACE}/policies/${policy_name}:${VERSION}" >> "$ARTIFACTS_DIR/charts.txt"
done

echo "✓ Installation artifacts generated in $ARTIFACTS_DIR/"
