# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

AutoShiftv2 is an Infrastructure-as-Code (IaC) framework for managing OpenShift Platform Plus components using Red Hat Advanced Cluster Management (RHACM) and OpenShift GitOps. It provides declarative management of day-2 operations across hub and managed OpenShift clusters.

## Architecture

The framework consists of three main architectural layers:

1. **Hub Cluster**: Hosts RHACM and OpenShift GitOps that manage all other clusters
2. **GitOps Layer**: Uses ArgoCD ApplicationSets to deploy policies across cluster sets
3. **Policy Layer**: Helm charts in `policies/` directory that define OpenShift Platform Plus components

### Key Components

- **autoshift/**: Main Helm chart that creates ArgoCD ApplicationSet to deploy all policies
- **policies/**: Individual Helm charts for each OpenShift Platform Plus component (ACM, ACS, ODF, etc.)
- **openshift-gitops/**: Bootstrap chart for initial GitOps installation
- **advanced-cluster-management/**: Bootstrap chart for initial ACM installation

## Common Commands

### Installation Commands
```bash
# Install OpenShift GitOps
helm upgrade --install openshift-gitops openshift-gitops -f policies/openshift-gitops/values.yaml

# Install Advanced Cluster Management
helm upgrade --install advanced-cluster-management advanced-cluster-management -f policies/advanced-cluster-management/values.yaml

# Install AutoShift (creates ApplicationSet that deploys all policies)
export APP_NAME="autoshift"
export REPO_URL="https://github.com/auto-shift/autoshiftv2.git"
export TARGET_REVISION="main"
export VALUES_FILE="values.hub.yaml"
export ARGO_PROJECT="default"
export GITOPS_NAMESPACE="openshift-gitops"
cat << EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: $APP_NAME
  namespace: $GITOPS_NAMESPACE
spec:
  destination:
    namespace: ''
    server: https://kubernetes.default.svc
  source:
    path: autoshift
    repoURL: $REPO_URL
    targetRevision: $TARGET_REVISION
    helm:
      valueFiles:
        - $VALUES_FILE
      values: |-
        autoshiftGitRepo: $REPO_URL
        autoshiftGitBranchTag: $TARGET_REVISION
  sources: []
  project: $ARGO_PROJECT
  syncPolicy:
    automated:
      prune: false
      selfHeal: true
EOF
```

### Verification Commands
```bash
# Check OpenShift GitOps pods
oc get pods -n openshift-gitops

# Check ACM installation status
oc get mch -A -w

# Check ArgoCD instance
oc get argocd -A

# Check cluster sets
oc get managedclustersets
```

## Configuration System

AutoShiftv2 uses a three-tiered configuration system with precedence: **cluster labels > clusterset labels > helm values**

### Key Configuration Files
- `autoshift/values.hub.yaml`: Default hub cluster configuration
- `autoshift/values.hub.baremetal-sno.yaml`: Single Node OpenShift configuration
- `autoshift/values.sbx.yaml`: Sandbox environment configuration
- `policies/*/values.yaml`: Individual policy default values

### Feature Flags
Components are enabled/disabled using boolean labels like:
- `metallb: 'true'` - Enable MetalLB
- `acs: 'true'` - Enable Advanced Cluster Security
- `odf: 'true'` - Enable OpenShift Data Foundation
- `logging: 'true'` - Enable OpenShift Logging

## Policy Structure

Each policy in `policies/` follows this pattern:
- **Chart.yaml**: Helm chart metadata
- **values.yaml**: Default configuration values
- **templates/**: Kubernetes manifests with ACM Policy wrappers
- **files/**: Static configuration files (e.g., MetalLB configs)

### Policy Template Pattern
Policies use ACM Policy resources with Placement and PlacementBinding to target specific cluster sets:
- Policy: Defines the desired state
- Placement: Selects target clusters using cluster sets
- PlacementBinding: Links Policy to Placement

## Cluster Management

### Cluster Sets
- **Hub Cluster Sets**: Defined in `hubClusterSets` section, manage hub cluster components
- **Managed Cluster Sets**: Defined in `managedClusterSets` section, manage spoke cluster components
- **Individual Clusters**: Override settings in `clusters` section

### Label System
Uses `autoshift.io/` prefixed labels for feature enablement:
- `autoshift.io/metallb: 'true'`
- `autoshift.io/acs-channel: 'stable'`
- `autoshift.io/odf: 'true'`

## Development Workflow

When modifying policies:
1. Update the appropriate policy's `values.yaml` and templates
2. Test changes on development cluster sets first
3. Policies auto-sync via ArgoCD when changes are committed
4. Monitor ACM governance dashboard for policy compliance

## Important Notes

- All policies use Helm templating with Go template syntax
- ArgoCD manages the GitOps workflow, not direct kubectl/oc commands
- Cluster labels override all other configuration sources
- Policy changes propagate automatically through GitOps sync
- Use `oc get policies -A` to check policy status across clusters