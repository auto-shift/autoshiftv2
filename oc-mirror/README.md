# AutoShift oc-mirror Container

This directory contains a containerized oc-mirror environment for AutoShift disconnected installations.

## Overview

The container packages oc-mirror v2 and OpenShift CLI with AutoShift integration, enabling users to:
- Generate ImageSetConfiguration from AutoShift values files
- Mirror OpenShift platform and operator images for disconnected environments
- Support air-gapped deployments with file-based mirroring
- Manage OpenShift clusters with version-matched oc client

## Quick Start

### Build Container

```bash
# Build from project root
podman build -f oc-mirror/Containerfile -t oc-mirror-autoshift:latest .

# Or with Docker
docker build -f oc-mirror/Containerfile -t oc-mirror-autoshift:latest .
```

### Generate ImageSet Configuration

```bash
# Version is automatically read from labels in values file
podman run --rm oc-mirror-autoshift:latest \
  bash -c "cd /workspace && ./scripts/generate-imageset-config.sh values.hub.yaml"
```

### Test with Dry Run

```bash
podman run --rm oc-mirror-autoshift:latest \
  bash -c "cd /workspace && \
    ./scripts/generate-imageset-config.sh values.hub.yaml && \
    oc-mirror -c imageset-config-hub.yaml file://mirror --v2 --dry-run"
```

### Mirror to Disk

```bash
# Create persistent volume for mirror data
podman volume create oc-mirror-data

# Mirror images to disk (version from values file)
podman run --rm -v oc-mirror-data:/workspace/mirror oc-mirror-autoshift:latest \
  bash -c "cd /workspace && \
    ./scripts/generate-imageset-config.sh values.hub.yaml && \
    oc-mirror -c imageset-config-hub.yaml file://mirror --v2"
```

## OpenShift Version Management

The container and script automatically read the OpenShift version from your AutoShift values file:

```yaml
# In autoshift/values.hub.yaml
hubClusterSets:
  hub:
    labels:
      openshift-version: '4.18.22'  # Full version with patch
managedClusterSets:
  managed:
    labels:
      openshift-version: '4.18.22'  # Can be different per cluster set
```

### How It Works

1. **Container Build**: 
   - Downloads latest stable oc-mirror binary (recommended for best compatibility)
   - Downloads oc client matching highest OpenShift version from labels
2. **Script Execution**: Reads OpenShift versions from all labels sections for ImageSet generation
3. **Multiple Version Support**: Uses min/max range for platform images, highest version for binaries
4. **Command Line Override**: Use `--openshift-version` flag to override values file setting

### Benefits

- **Latest Features**: Always uses the most recent stable oc-mirror with latest bug fixes
- **Backward Compatibility**: Latest oc-mirror works with all supported OpenShift versions
- **Multi-Version Support**: Handles different OpenShift versions per cluster set automatically
- **Smart Version Range**: Uses min/max versions for platform images, highest for binaries
- **Warning System**: Alerts when multiple versions detected with clear version information
- **Flexible Override**: Command line flag available when needed for testing

## Container Components

### Files

- **Containerfile** - Container definition with oc-mirror v2 and AutoShift integration
- **entrypoint.sh** - Container entrypoint handling authentication and ImageSet generation
- **README.md** - This documentation

### Base Image

- **Registry**: `registry.access.redhat.com/ubi9/ubi-minimal:latest`
- **oc-mirror version**: Latest stable (automatically downloaded for best compatibility)
- **oc client version**: Matches OpenShift version from values file (e.g., 4.18-stable)
- **User**: Non-root (1001:root) for security

### Environment Variables

- `HOME=/workspace` - Container home directory
- `XDG_CACHE_HOME=/workspace/cache` - oc-mirror v2 cache location
- `XDG_RUNTIME_DIR=/workspace` - oc-mirror v2 runtime directory

## Usage Patterns

### Interactive Mode

```bash
# Run container interactively
podman run -it --rm oc-mirror-autoshift:latest bash

# Inside container - version read automatically from values file
cd /workspace
./scripts/generate-imageset-config.sh values.hub.yaml
oc-mirror -c imageset-config-hub.yaml file://mirror --v2 --dry-run
```

### Batch Processing

```bash
# Process multiple configurations (each with their own openshift-version)
podman run --rm oc-mirror-autoshift:latest \
  bash -c "
    cd /workspace
    for config in values.hub.yaml values.sbx.yaml; do
      echo Processing \$config...
      ./scripts/generate-imageset-config.sh \$config
      oc-mirror -c imageset-config-*.yaml file://mirror --v2 --dry-run
    done
  "
```

### Custom Pull Secret

```bash
# Mount custom pull secret (version still read from values file)
podman run --rm \
  -v /path/to/your/pull-secret.txt:/workspace/pull-secret.txt \
  oc-mirror-autoshift:latest \
  bash -c "cd /workspace && ./scripts/generate-imageset-config.sh values.hub.yaml"
```

### Override OpenShift Version

```bash
# Override version from command line if needed (values file version is ignored)
podman run --rm oc-mirror-autoshift:latest \
  bash -c "cd /workspace && ./scripts/generate-imageset-config.sh values.hub.yaml --openshift-version 4.17.15"
```

## Build Context

The container must be built from the project root with `-f oc-mirror/Containerfile` because it needs access to:

```
autoshiftv2/
├── scripts/generate-imageset-config.sh    # ImageSet generation script
├── autoshift/values.hub.yaml              # AutoShift configuration
├── pull-secret.txt                        # OpenShift pull secret
└── oc-mirror/
    ├── Containerfile                       # Container definition
    ├── entrypoint.sh                       # Container entrypoint
    └── README.md                           # This file
```

## Supported Operators

The container automatically detects and mirrors these operators when enabled in AutoShift values:

- OpenShift GitOps (gitops)
- Advanced Cluster Management (acm) - always included
- MetalLB (metallb)
- OpenShift Data Foundation (odf)
- Advanced Cluster Security (acs)
- Developer Spaces (dev-spaces)
- Developer Hub (dev-hub)
- OpenShift Pipelines (pipelines)
- Trusted Artifact Signer (tas)
- Quay (quay)
- Loki (loki)
- OpenShift Logging (logging)
- Cluster Observability Operator (coo)
- Compliance Operator (compliance)

## Troubleshooting

### Build Issues

```bash
# Ensure building from project root
cd /path/to/autoshiftv2
podman build -f oc-mirror/Containerfile -t oc-mirror-autoshift .
```

### Permission Issues

```bash
# Container runs as user 1001, ensure volume permissions
podman volume create oc-mirror-data
podman run --rm -v oc-mirror-data:/data alpine chown -R 1001:0 /data
```

### Authentication Issues

```bash
# Verify pull secret format
cat pull-secret.txt | jq .

# Check authentication inside container
podman run -it --rm oc-mirror-autoshift bash
cat /workspace/containers/auth.json
```

## Related Documentation

- [AutoShift Documentation](../README.md)
- [OpenShift Disconnected Installation](https://docs.openshift.com/container-platform/latest/installing/disconnected_install/installing-mirroring-disconnected.html)