# AutoShift Release Process

This document describes how to create and publish AutoShift releases to OCI-compliant registries.

## Overview

AutoShift releases consist of multiple Helm charts:
- **2 bootstrap charts**: `openshift-gitops`, `advanced-cluster-management`
- **1 main chart**: `autoshift` (ApplicationSet)
- **Policy charts**: ACM policies for Day 2 operations (one per `policies/` subdirectory)

All charts are version-synchronized and published to an OCI registry for deployment without Git dependencies.

## Prerequisites

### Required Tools

```bash
# Helm 3.14+
helm version

# yq (YAML processor) 4.x
yq --version

# Git
git version

# Access to OCI registry
helm registry login <registry>
```

###Install yq

```bash
# macOS
brew install yq

# Linux
wget https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 -O /usr/local/bin/yq
chmod +x /usr/local/bin/yq

# Check installation
yq --version
```

## Release Workflow

### 1. Prepare Release

```bash
# View available Make targets
make help

# Discover charts (informational)
make discover

# Validate prerequisites
make validate
```

### 2. Create Release

**For Testing (Release Candidate):**
```bash
# Uses default registry: quay.io/autoshift
make release VERSION=1.0.0-rc.1

# Or override with custom registry
make release VERSION=1.0.0-rc.1 REGISTRY=ghcr.io REGISTRY_NAMESPACE=myorg/autoshift
```

**For Production:**
```bash
# Uses default registry: quay.io/autoshift
make release VERSION=1.0.0

# Or override with custom registry
make release VERSION=1.0.0 REGISTRY=ghcr.io REGISTRY_NAMESPACE=myorg/autoshift
```

**Dry Run (package without pushing):**
```bash
make release VERSION=1.0.0 DRY_RUN=true
```

### 3. What Happens During Release

The `make release` command:

1. **Validates** - Checks tools and version format
2. **Updates versions** - Sets all charts to the same version
3. **Generates policy list** - Creates `policy-list.txt` with discovered policies
4. **Packages charts** - Creates `.tgz` files for all charts (includes policy-list.txt)
5. **Pushes to OCI** - Uploads charts to registry
6. **Generates artifacts** - Creates bootstrap installation scripts and documentation

### 4. Tag and Release

```bash
# Create and push git tag
git add .
git commit -m "Release v1.0.0"
git tag v1.0.0
git push origin v1.0.0

# Create GitHub/GitLab release
# Upload artifacts from release-artifacts/ directory
```

## Makefile Targets

```bash
make help                  # Show available targets
make discover              # List all discoverable charts
make validate              # Check required tools
make validate-version      # Validate VERSION format
make clean                 # Remove build artifacts
make update-versions       # Update all chart versions
make generate-policy-list  # Generate policy-list.txt for OCI mode
make package-charts        # Package all Helm charts
make push-charts           # Push charts to OCI registry
make generate-artifacts    # Generate bootstrap installation scripts
make release               # Full release process
make package-only          # Package without version updates
```

## OCI Registry Structure

Charts are organized in a namespaced structure to avoid naming collisions:

```
oci://registry/namespace/
├── bootstrap/
│   ├── openshift-gitops
│   └── advanced-cluster-management
├── autoshift
└── policies/
    ├── openshift-gitops
    ├── advanced-cluster-management
    ├── advanced-cluster-security
    ├── openshift-data-foundation
    └── ... (24 more policy charts)
```

## Zero Git Dependency

Released charts are **completely self-contained**:

✅ Bootstrap charts come from OCI (`bootstrap/` namespace)
✅ AutoShift chart comes from OCI (root namespace)
✅ Policy list embedded in AutoShift chart
✅ Policy charts come from OCI (`policies/` namespace)
✅ No Git repository access required at runtime

**Deployment workflow:**
```bash
# 1. Bootstrap (from OCI bootstrap namespace)
helm install openshift-gitops oci://quay.io/autoshift/bootstrap/openshift-gitops:1.0.0
helm install advanced-cluster-management oci://quay.io/autoshift/bootstrap/advanced-cluster-management:1.0.0

# 2. Deploy AutoShift (from OCI via ArgoCD)
oc apply -f autoshift-application.yaml  # Points to OCI

# 3. ApplicationSet deploys policies (from OCI policies namespace)
# All policy charts pulled from oci://quay.io/autoshift/policies/
```

## OCI Deployment

When deploying from OCI, use your existing values file and override the OCI-specific settings:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  source:
    repoURL: oci://quay.io/autoshift
    chart: autoshift
    targetRevision: "1.0.0"
    helm:
      valueFiles:
        - values/global.yaml
        - values/clustersets/hub.yaml  # Or other clusterset files
        - values/clustersets/managed.yaml
      values: |
        # Enable OCI mode
        autoshiftOciRegistry: true
        autoshiftOciRepo: oci://quay.io/autoshift/policies
        autoshiftOciVersion: "1.0.0"
```

This approach allows you to:
- Keep cluster-specific configuration in your existing values files
- Switch between Git and OCI mode easily
- Override only what's needed for OCI deployment

## Gradual Rollouts

AutoShift supports deploying multiple versions side-by-side for gradual rollouts using ACM ClusterSets:

- Deploy `autoshift-stable` managing `hub-stable` clusterset (v0.0.1)
- Deploy `autoshift-canary` managing `hub-canary` clusterset (v0.0.2)
- Gradually move clusters between clustersets to migrate

See [gradual-rollout.md](gradual-rollout.md) for complete guide.

## Support

- **Documentation**: [README.md](README.md)
- **Gradual Rollouts**: [gradual-rollout.md](gradual-rollout.md)
- **Issues**: https://github.com/auto-shift/autoshiftv2/issues
