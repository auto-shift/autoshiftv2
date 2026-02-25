# AutoShift Release & OCI Guide

This guide covers how AutoShift releases are created, how OCI mode works, and how to manage OCI-based deployments. For step-by-step installation, see the [Quick Start Guide](quickstart.md).

## How OCI Mode Works

AutoShift has two deployment modes controlled by the `autoshiftOciRegistry` value:

### Git Mode (Default)
```
AutoShift Chart (from Git)
    |
ApplicationSet with Git Generator
    |
Auto-discovers policies/ directories in Git
    |
Deploys each policy as an ArgoCD Application
```

### OCI Mode (`autoshiftOciRegistry: true`)
```
AutoShift Chart (from OCI)
    |
ApplicationSet with List Generator
    |
Reads policy-list.txt (generated at release time)
    |
Deploys each policy chart from OCI registry
```

The OCI chart includes a `files/policy-list.txt` that is **automatically generated during the release process** by discovering all policies in the `policies/` directory. No manual maintenance is required.

### Key Differences

| Aspect | Git Mode | OCI Mode |
|--------|----------|----------|
| **Generator** | `git:` - auto-discovers `policies/*` | `list:` - reads `policy-list.txt` |
| **Source** | Git repository path | OCI chart reference |
| **Version** | Git branch/tag | Chart version (pinned) |
| **Dynamic** | Auto-discovers new policies | Fixed to released policies |
| **Use Case** | Development, customization | Production, version-pinned |

## OCI Registry Structure

When a release is created with `make release`, charts are published to the registry in this structure:

```
quay.io/autoshift/
├── bootstrap/
│   ├── openshift-gitops          # Bootstrap chart for GitOps operator
│   └── advanced-cluster-management  # Bootstrap chart for ACM operator
├── autoshift                      # Main chart (ApplicationSet)
└── policies/
    ├── openshift-gitops           # Policy chart (takes over GitOps)
    ├── advanced-cluster-management # Policy chart (takes over ACM)
    ├── advanced-cluster-security
    └── ... (additional policy charts)
```

All charts share the same version number, set during the release process.

## OCI Configuration Values

When deploying from OCI, three values control the behavior. These are set automatically by the `install-autoshift.sh` script generated during the release:

```yaml
# Enable OCI mode (boolean, not a URL)
autoshiftOciRegistry: true

# OCI path where policy charts are published
autoshiftOciRepo: oci://quay.io/autoshift/policies

```

## Overview

AutoShift releases consist of multiple Helm charts:
- **2 bootstrap charts**: `openshift-gitops`, `advanced-cluster-management`
- **1 main chart**: `autoshift` (ApplicationSet)
- **Policy charts**: ACM policies for Day 2 operations (one per `policies/` subdirectory)

All charts are version-synchronized and published to an OCI registry. Released charts are completely self-contained with no Git repository access required at runtime.

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

### Install yq

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
make release VERSION=X.Y.Z-rc.1

# Or override with custom registry
make release VERSION=X.Y.Z-rc.1 REGISTRY=ghcr.io REGISTRY_NAMESPACE=myorg/autoshift
```

**For Production:**
```bash
# Uses default registry: quay.io/autoshift
make release VERSION=X.Y.Z

# Or override with custom registry
make release VERSION=X.Y.Z REGISTRY=ghcr.io REGISTRY_NAMESPACE=myorg/autoshift
```

**Dry Run (package without pushing):**
```bash
make release VERSION=X.Y.Z DRY_RUN=true
```

### 3. What Happens During Release

The `make release` command:

1. **Validates** - Checks tools and version format
2. **Updates versions** - Sets all charts to the same version
3. **Generates policy list** - Creates `policy-list.txt` with discovered policies
4. **Packages charts** - Creates `.tgz` files for all charts (includes policy-list.txt)
5. **Pushes to OCI** - Uploads charts to registry and tags as `latest`
6. **Generates artifacts** - Creates bootstrap installation scripts (`install-bootstrap.sh`, `install-autoshift.sh`)

### 4. Tag and Release

```bash
# Create and push git tag
git add .
git commit -m "Release vX.Y.Z"
git tag vX.Y.Z
git push origin vX.Y.Z

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

## Private Registry Authentication

If publishing to or deploying from a private OCI registry, configure credentials for ArgoCD:

```bash
# Create secret for OCI registry authentication
oc create secret docker-registry autoshift-oci-creds \
  --docker-server=quay.io \
  --docker-username=YOUR_USERNAME \
  --docker-password=YOUR_TOKEN \
  -n openshift-gitops

# Link to ArgoCD repo server (pulls Helm charts)
oc patch serviceaccount argocd-repo-server \
  -n openshift-gitops \
  --type='json' \
  -p='[{"op":"add","path":"/imagePullSecrets/-","value":{"name":"autoshift-oci-creds"}}]'

# Link to ApplicationSet controller (generates Applications)
oc patch serviceaccount argocd-applicationset-controller \
  -n openshift-gitops \
  --type='json' \
  -p='[{"op":"add","path":"/imagePullSecrets/-","value":{"name":"autoshift-oci-creds"}}]'

# Restart ArgoCD components to pick up credentials
oc rollout restart deployment/argocd-repo-server -n openshift-gitops
oc rollout restart deployment/argocd-applicationset-controller -n openshift-gitops
```

## Custom CA Certificate

If your OCI registry uses a custom CA certificate (e.g., private registry with self-signed certs), ArgoCD's repo server needs access to the CA bundle to pull charts.

AutoShift handles this automatically when you set the `gitops-cluster-ca-bundle` label to `'true'` on a clusterset or cluster in your values files:

```yaml
hubClusterSets:
  hub:
    labels:
      gitops-cluster-ca-bundle: 'true'
```

Alternatively, you can enable it globally via the Helm value in `policies/openshift-gitops/values.yaml`:

```yaml
gitops:
  repo:
    cluster_ca_bundle: true
```

The cluster label takes precedence over the Helm value when both are set.

When enabled, AutoShift:
1. Creates a `user-ca-bundle` ConfigMap in the ArgoCD namespace with the `config.openshift.io/inject-trusted-cabundle: "true"` label
2. OpenShift automatically populates that ConfigMap with the cluster's trusted CA certificates
3. Mounts the CA bundle into the ArgoCD repo server at `/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem`

> [!NOTE]
> Your custom CA must already be added to the cluster's trust store via the [cluster-wide proxy](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/networking/configuring-a-custom-pki) for the injection to include it.

## Disconnected / Air-Gapped Environments

For disconnected environments, mirror the released charts to an internal registry:

```bash
VERSION="X.Y.Z"  # Replace with desired version

# Pull all charts from source registry
helm pull oci://quay.io/autoshift/autoshift --version ${VERSION}
helm pull oci://quay.io/autoshift/bootstrap/openshift-gitops --version ${VERSION}
helm pull oci://quay.io/autoshift/bootstrap/advanced-cluster-management --version ${VERSION}

# Pull policy charts (use the policy-list.txt from the release)
for policy in $(cat policy-list.txt); do
  helm pull oci://quay.io/autoshift/policies/$policy --version ${VERSION}
done

# Push to internal registry
for chart in *.tgz; do
  helm push $chart oci://harbor.internal.com/autoshift
done
```

Update the OCI values to point to your internal registry:

```yaml
autoshiftOciRegistry: true
autoshiftOciRepo: oci://harbor.internal.com/autoshift/policies
```

AutoShift also provides an ImageSet generator for mirroring operator images. See `scripts/generate-imageset-config.sh` for details.

## Version Management

### Upgrading

To upgrade or pin to a specific version, set `targetRevision` on the ArgoCD Application:

```bash
oc patch application autoshift -n openshift-gitops \
  --type=merge \
  -p '{"spec":{"source":{"targetRevision":"X.Y.Z"}}}'
```


### Gradual Rollouts

AutoShift supports deploying multiple versions side-by-side using ACM ClusterSets. See the [Gradual Rollout Guide](gradual-rollout.md) for details.

## Exclude Policies

You can exclude specific policies from deployment in both Git and OCI modes:

```yaml
excludePolicies:
  - infra-nodes
  - worker-nodes
```

## Troubleshooting

### ArgoCD can't pull charts from OCI registry

```bash
# Check if secret exists
oc get secret autoshift-oci-creds -n openshift-gitops

# Verify secret is linked to service accounts
oc get sa argocd-repo-server -n openshift-gitops -o yaml | grep -A2 imagePullSecrets
oc get sa argocd-applicationset-controller -n openshift-gitops -o yaml | grep -A2 imagePullSecrets

# Test credentials manually
helm registry login quay.io -u USERNAME -p TOKEN
helm pull oci://quay.io/autoshift/autoshift
```

### ApplicationSet not creating policy Applications

```bash
# Check ApplicationSet status
oc get applicationset -n openshift-gitops -o yaml

# Check ApplicationSet controller logs
oc logs -n openshift-gitops deployment/argocd-applicationset-controller --tail=100

# Verify OCI mode is enabled
oc get application autoshift -n openshift-gitops -o yaml | grep autoshiftOciRegistry
```

### Policy charts not found in registry

```bash
# Pull specific chart to test (omit --version to pull latest)
helm pull oci://quay.io/autoshift/policies/advanced-cluster-security

# Verify all charts are the same version
oc get applications -n openshift-gitops -o custom-columns=NAME:.metadata.name,REVISION:.spec.source.targetRevision | grep autoshift
```

## Support

- [Quick Start Guide](quickstart.md)
- [Gradual Rollout Guide](gradual-rollout.md)
- [Main README](../README.md)
