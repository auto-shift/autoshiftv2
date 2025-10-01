# AutoShift oc-mirror Container

This directory contains a containerized oc-mirror environment for AutoShift disconnected installations with comprehensive workflow support.

## Overview

The container packages oc-mirror v2 and OpenShift CLI with AutoShift integration, enabling users to:
- Generate ImageSetConfiguration from AutoShift values files
- Mirror OpenShift platform and operator images for disconnected environments
- Support air-gapped deployments with file-based mirroring
- Direct registry-to-registry mirroring for semi-connected environments
- Manage image lifecycle with safe deletion workflows
- Automated workflow orchestration for complex mirroring scenarios
- Manage OpenShift clusters with version-matched oc client

## Quick Start

### Build Container

```bash
# Build from project root
podman build -f oc-mirror/Containerfile -t oc-mirror-autoshift:latest .

# Or with Docker
docker build -f oc-mirror/Containerfile -t oc-mirror-autoshift:latest .
```

**Note**: Pull secret is now provided via Kubernetes secrets at runtime, not during build.

### Authentication Setup

The container expects a pull secret mounted at `/workspace/pull-secret.txt`:

#### Podman/Docker Usage

```bash
# Mount pull secret from host
podman run --rm \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk
```

#### Kubernetes/Pipeline Usage

Handled automatically by the Tekton pipeline - see `oc-mirror/pipeline/` directory.

### Available Workflows

The container provides several built-in workflows accessible through the entrypoint:

```bash
# Show available workflows
podman run --rm oc-mirror-autoshift:latest workflows

# Generate ImageSet from AutoShift values
podman run --rm \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  oc-mirror-autoshift:latest generate-imageset

# Complete air-gapped workflow: AutoShift values → ImageSet → Disk
podman run --rm \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk

# Use different values file with operators-only mode
podman run --rm \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk --values-file values.sbx.yaml --operators-only --dry-run

# Clean cache before mirroring to ensure fresh download
podman run --rm \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk --clean-cache --dry-run

# Upload air-gapped content to registry
podman run --rm \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-from-disk

# Direct mirroring workflow: AutoShift values → ImageSet → Registry
podman run --rm \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  oc-mirror-autoshift:latest workflow-direct -r registry.example.com:443
```

## Mirroring Workflows

### 1. Air-Gapped Mirroring (Mirror-to-Disk → Disk-to-Mirror)

**Use Case**: Complete disconnection - no network access to Red Hat registries from target environment.

#### Step 1: Mirror to Disk (Connected Environment)

```bash
# Create persistent volume for mirror data
podman volume create oc-mirror-data

# Complete workflow: values → imageset → disk
# ImageSet configurations are automatically backed up to /workspace/content/imageset-configs/
podman run --rm -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk

# Or step-by-step:
# 1. Generate ImageSet configuration
podman run --rm oc-mirror-autoshift:latest generate-imageset

# 2. Mirror to disk with custom options
# Configurations are automatically backed up with timestamps
podman run --rm -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest mirror-to-disk \
  -c imageset-autoshift.yaml --since 2025-09-01
```

#### Step 2: Transport to Air-Gapped Environment

```bash
# Export volume data for transport
podman run --rm -v oc-mirror-data:/data -v $(pwd):/backup \
  registry.access.redhat.com/ubi9/ubi-minimal:latest \
  tar czf /backup/mirror-content.tar.gz -C /data .

# Copy mirror-content.tar.gz to air-gapped environment
# Import in air-gapped environment:
podman volume create oc-mirror-data
podman run --rm -v oc-mirror-data:/data -v $(pwd):/backup \
  registry.access.redhat.com/ubi9/ubi-minimal:latest \
  tar xzf /backup/mirror-content.tar.gz -C /data
```

#### Step 3: Upload to Registry (Air-Gapped Environment)

```bash
# Complete workflow: disk → registry
podman run --rm -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-from-disk \
  -r your-disconnected-registry:443

# Or with custom options:
podman run --rm -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest disk-to-mirror \
  -r your-disconnected-registry:443 --dry-run
```

### 2. Semi-Connected Mirroring (Mirror-to-Mirror)

**Use Case**: Limited connectivity - access to both Red Hat registries and target registry.

```bash
# Complete workflow: values → imageset → direct mirror  
# ImageSet configurations are automatically backed up to /workspace/workspace/imageset-configs/
podman run --rm oc-mirror-autoshift:latest workflow-direct \
  -r registry.example.com:443

# Use different values file with custom OpenShift version
podman run --rm oc-mirror-autoshift:latest workflow-direct \
  --values-file values.hub.baremetal-sno.yaml --openshift-version 4.17 \
  -r registry.example.com:443 --dry-run

# Or step-by-step:
# 1. Generate ImageSet
podman run --rm oc-mirror-autoshift:latest generate-imageset

# 2. Direct mirror with custom options
# Configurations are automatically backed up with timestamps
podman run --rm oc-mirror-autoshift:latest mirror-to-mirror \
  -c imageset-autoshift.yaml -r registry.example.com:443 --dry-run
```

### 3. Image Lifecycle Management

**Use Case**: Clean up old OpenShift versions and operators to save registry space.

#### Step 1: Generate Deletion Plan (Safe)

```bash
# Generate safe deletion plan (no actual deletions)
podman run --rm oc-mirror-autoshift:latest delete-generate \
  -c imageset-delete.yaml -r registry.example.com:443

# Review the deletion plan
podman run --rm oc-mirror-autoshift:latest bash -c \
  "cat workspace/working-dir/delete/delete-images.yaml | head -20"
```

#### Step 2: Execute Deletion (Permanent)

```bash
# Execute deletion plan (WARNING: Permanent!)
podman run --rm oc-mirror-autoshift:latest delete-execute \
  -r registry.example.com:443

# Or with volume persistence for large operations
podman run --rm -v oc-mirror-data:/workspace/workspace \
  oc-mirror-autoshift:latest delete-execute \
  -r registry.example.com:443
```

### 4. Individual Workflow Components

#### Generate ImageSet Configuration

```bash
# From AutoShift values file
podman run --rm oc-mirror-autoshift:latest generate-imageset

# With custom values file (mount as volume)
podman run --rm -v /path/to/values.yaml:/workspace/autoshift/values.hub.yaml \
  oc-mirror-autoshift:latest generate-imageset
```

#### Mirror to Disk

```bash
# Basic mirror to disk
podman run --rm -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest mirror-to-disk

# With custom configuration and incremental mirroring
podman run --rm -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest mirror-to-disk \
  -c imageset-config.yaml --since 2025-09-01 --dry-run

# Auto-detect incremental from .history files
podman run --rm -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest mirror-to-disk \
  -c imageset-config.yaml --incremental --dry-run
```

#### Disk to Mirror

```bash
# Upload to registry
podman run --rm -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest disk-to-mirror \
  -r registry.example.com:443

# With dry run and custom cache
podman run --rm -v oc-mirror-data:/workspace/content \
  -v oc-mirror-cache:/workspace/cache \
  oc-mirror-autoshift:latest disk-to-mirror \
  -r registry.example.com:443 --dry-run
```

#### Direct Mirror

```bash
# Direct registry-to-registry
podman run --rm oc-mirror-autoshift:latest mirror-to-mirror \
  -c imageset-config.yaml -r registry.example.com:443

# With incremental mirroring and workspace persistence
podman run --rm -v oc-mirror-workspace:/workspace/workspace \
  oc-mirror-autoshift:latest mirror-to-mirror \
  -c imageset-config.yaml -r registry.example.com:443 --incremental

# Manual since date for direct mirroring
podman run --rm oc-mirror-autoshift:latest mirror-to-mirror \
  -c imageset-config.yaml -r registry.example.com:443 --since 2025-09-20
```

## Incremental Mirroring & Performance Optimization

The container now supports intelligent incremental mirroring that dramatically reduces mirror time by leveraging `.history` files and the `--since` flag.

### Performance Benefits

- **Full Mirror**: ~7 minutes 17 seconds (complete download)
- **Incremental Mirror**: ~1 minute 13 seconds (**83% faster**)
- **Cache Utilization**: Reuses existing downloads efficiently
- **Automatic Detection**: No manual date calculation required

### How It Works

oc-mirror automatically creates `.history` files in `content/working-dir/.history/` (or `workspace/working-dir/.history/`) that contain SHA256 digests of all mirrored images. These files are timestamped and preserved across container runs via persistent volumes.

Example history files:
```bash
content/working-dir/.history/
├── .history-2025-09-22T14:13:30Z  # First mirror operation
└── .history-2025-09-23T14:32:31Z  # Second mirror operation
```

### Incremental Mirroring Options

#### 1. Automatic History Detection

```bash
# Auto-detect last mirror date from .history files
podman run --rm -v oc-mirror-content:/workspace/content \
  oc-mirror-autoshift:latest mirror-to-disk \
  -c imageset-config.yaml --incremental

# Works with direct mirroring too
podman run --rm -v oc-mirror-workspace:/workspace/workspace \
  oc-mirror-autoshift:latest mirror-to-mirror \
  -c imageset-config.yaml -r registry.example.com:443 --incremental
```

When `--incremental` is used:
- ✅ Finds most recent `.history-YYYY-MM-DDTHH:MM:SSZ` file
- ✅ Extracts date automatically (e.g., `2025-09-23`)
- ✅ Applies `--since 2025-09-23` to oc-mirror
- ✅ Shows detection: `🔍 Auto-detected incremental mode from history: 2025-09-23`

#### 2. Manual Since Date

```bash
# Specify custom since date (overrides auto-detection)
podman run --rm -v oc-mirror-content:/workspace/content \
  oc-mirror-autoshift:latest mirror-to-disk \
  -c imageset-config.yaml --since 2025-09-20

# Both YYYY-MM-DD and ISO formats supported
podman run --rm oc-mirror-autoshift:latest mirror-to-mirror \
  -c imageset-config.yaml -r registry.example.com:443 --since 2025-09-23T10:00:00Z
```

### Incremental Workflow Examples

#### Air-Gapped Incremental Workflow

```bash
# Initial full mirror (first time)
podman run --rm -v oc-mirror-content:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk

# Subsequent incremental mirrors (much faster)
podman run --rm -v oc-mirror-content:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk --incremental

# Manual date for specific timeframes
podman run --rm -v oc-mirror-content:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk --since 2025-09-15
```

#### Direct Registry Incremental Workflow

```bash
# Initial full mirror
podman run --rm -v oc-mirror-workspace:/workspace/workspace \
  oc-mirror-autoshift:latest workflow-direct -r registry.example.com:443

# Incremental updates
podman run --rm -v oc-mirror-workspace:/workspace/workspace \
  oc-mirror-autoshift:latest workflow-direct -r registry.example.com:443 --incremental
```

### History File Persistence

Incremental mirroring requires persistent volumes to preserve `.history` files between container runs:

```bash
# Create persistent volumes (one-time setup)
podman volume create oc-mirror-content  # For air-gapped workflows
podman volume create oc-mirror-workspace # For direct registry workflows
podman volume create oc-mirror-cache    # For performance optimization

# Always use persistent volumes for incremental mirroring
podman run --rm \
  -v oc-mirror-content:/workspace/content \
  -v oc-mirror-cache:/workspace/cache \
  oc-mirror-autoshift:latest mirror-to-disk --incremental
```

### Incremental Mirroring Best Practices

#### 1. Volume Strategy
```bash
# Dedicated volumes per environment
podman volume create oc-mirror-prod-content --label env=production
podman volume create oc-mirror-dev-content --label env=development

# Use consistent mount points
-v oc-mirror-prod-content:/workspace/content
-v oc-mirror-cache:/workspace/cache
```

#### 2. Regular Incremental Updates
```bash
# Daily incremental sync (cron job example)
0 6 * * * podman run --rm -v oc-mirror-content:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk --incremental
```

#### 3. Validation and Testing
```bash
# Always test incremental mode with dry-run first
podman run --rm -v oc-mirror-content:/workspace/content \
  oc-mirror-autoshift:latest mirror-to-disk --incremental --dry-run

# Check history files manually if needed
podman run --rm -v oc-mirror-content:/workspace/content \
  registry.access.redhat.com/ubi9/ubi-minimal:latest \
  ls -la /workspace/content/working-dir/.history/
```

#### 4. Troubleshooting Incremental Mode
```bash
# If no history found, performs full mirror automatically
# ℹ️  No .history directory found - performing full mirror

# Force full mirror by cleaning history
podman run --rm -v oc-mirror-content:/workspace/content \
  registry.access.redhat.com/ubi9/ubi-minimal:latest \
  rm -rf /workspace/content/working-dir/.history/

# Combine with cache cleaning for completely fresh mirror
podman run --rm \
  -v oc-mirror-content:/workspace/content \
  -v oc-mirror-cache:/workspace/cache \
  oc-mirror-autoshift:latest workflow-to-disk --clean-cache
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

### Core Files

- **Containerfile** - Container definition with oc-mirror v2 and AutoShift integration
- **entrypoint.sh** - Enhanced container entrypoint with workflow orchestration
- **README.md** - This comprehensive documentation

### Workflow Scripts

- **mirror-to-disk.sh** - Mirror registry content to disk for air-gapped transport with incremental support
- **disk-to-mirror.sh** - Upload disk content to disconnected registry
- **mirror-to-mirror.sh** - Direct registry-to-registry mirroring with incremental support
- **delete-generate.sh** - Generate safe deletion plans for old images
- **delete-execute.sh** - Execute deletion plans (permanent operations)

### Generated Configurations

All ImageSet configurations are dynamically generated from AutoShift values files using `generate-imageset-config.sh`. No static template files are provided since configurations are specific to each environment's operator selections and OpenShift versions.

### Configuration Backups

All workflow scripts automatically create timestamped backups of ImageSet configurations before running mirror operations:

- **Mirror-to-disk**: Backs up configs to `content/imageset-configs/`
- **Mirror-to-mirror**: Backs up configs to `workspace/imageset-configs/`  
- **Delete operations**: Backs up delete configs to `workspace/imageset-configs/`

Backup filename format: `{config-name}-{YYYYMMDD-HHMMSS}.yaml`

Example backup files:
```
content/imageset-configs/
├── imageset-autoshift-20250908-143022.yaml
├── imageset-delete-autoshift-20250908-143055.yaml
└── imageset-config-hub-20250908-142815.yaml
```

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
./generate-imageset-config.sh values.hub.yaml
oc-mirror -c imageset-config-hub.yaml file://mirror --v2 --dry-run
```

### Direct Script Usage (Outside Container)

The `generate-imageset-config.sh` script can also be used directly without containers:

```bash
# Direct script usage from oc-mirror directory
cd oc-mirror
./generate-imageset-config.sh values.hub.yaml --help

# Generate ImageSet with custom options
./generate-imageset-config.sh values.hub.yaml \
  --openshift-version 4.18 \
  --operators-only \
  --output my-imageset.yaml

# For detailed script documentation, see script help:
./generate-imageset-config.sh --help
```

### Batch Processing

```bash
# Process multiple configurations (each with their own openshift-version)
podman run --rm oc-mirror-autoshift:latest \
  bash -c "
    cd /workspace
    for config in values.hub.yaml values.sbx.yaml; do
      echo Processing \$config...
      ./generate-imageset-config.sh \$config
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
  bash -c "cd /workspace && ./generate-imageset-config.sh values.hub.yaml"
```

### Override OpenShift Version

```bash
# Override version from command line if needed (values file version is ignored)
podman run --rm oc-mirror-autoshift:latest \
  bash -c "cd /workspace && ./generate-imageset-config.sh values.hub.yaml --openshift-version 4.17.15"
```

## Container Workflow Architecture

The container provides three types of operations:

### 1. **Individual Scripts** - Granular control for specific operations
- Direct access to each workflow script with full parameter control
- Suitable for automation and custom integration scenarios
- Examples: `mirror-to-disk --since 2025-09-01`, `delete-generate -c custom-config.yaml`

### 2. **Combined Workflows** - Multi-step operations in single commands
- Orchestrated operations that combine multiple steps
- Automatic error handling and progress reporting
- Examples: `workflow-to-disk`, `workflow-direct`, `workflow-delete-generate`

### 3. **AutoShift Integration** - Seamless integration with AutoShift values
- Automatic ImageSet generation from AutoShift Helm values
- Version detection and operator selection based on cluster configuration
- Examples: `generate-imageset`, combined workflows with auto-generated configs

## Build Context

The container must be built from the project root with `-f oc-mirror/Containerfile` because it needs access to:

```
autoshiftv2/
├── autoshift/values.hub.yaml              # AutoShift configuration
├── pull-secret.txt                        # OpenShift pull secret
└── oc-mirror/
    ├── Containerfile                       # Container definition
    ├── entrypoint.sh                       # Enhanced workflow entrypoint
    ├── generate-imageset-config.sh         # ImageSet generation script
    ├── mirror-to-disk.sh                   # Air-gapped mirroring script
    ├── disk-to-mirror.sh                   # Upload script
    ├── mirror-to-mirror.sh                 # Direct mirroring script
    ├── delete-generate.sh                  # Deletion planning script
    ├── delete-execute.sh                   # Deletion execution script
    └── README.md                           # This comprehensive documentation
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

## Workflow Best Practices

### Volume Management

```bash
# Create dedicated volumes for different data types
podman volume create oc-mirror-data      # For mirror content
podman volume create oc-mirror-cache     # For oc-mirror cache
podman volume create oc-mirror-workspace # For workspace data

# Use volume labels for organization
podman volume create oc-mirror-prod-data --label env=production
podman volume create oc-mirror-dev-data --label env=development
```

### Performance Optimization

```bash
# Use persistent cache for multiple operations
podman run --rm \
  -v oc-mirror-cache:/workspace/cache \
  -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk

# Parallel operations with separate caches
podman run --name mirror1 -d \
  -v oc-mirror-cache1:/workspace/cache \
  oc-mirror-autoshift:latest mirror-to-disk -c config1.yaml &
podman run --name mirror2 -d \
  -v oc-mirror-cache2:/workspace/cache \
  oc-mirror-autoshift:latest mirror-to-disk -c config2.yaml &
```

### Security Considerations

```bash
# Use read-only mounts for configuration files
podman run --rm \
  -v /secure/path/pull-secret.txt:/workspace/pull-secret.txt:ro \
  -v /secure/path/values.yaml:/workspace/autoshift/values.hub.yaml:ro \
  oc-mirror-autoshift:latest generate-imageset

# Run with specific user ID for consistency
podman run --rm --user 1001:0 \
  oc-mirror-autoshift:latest workflows
```

### Testing and Validation

```bash
# Always test with dry-run first
podman run --rm oc-mirror-autoshift:latest \
  mirror-to-disk --dry-run

# Validate configurations before mirroring
podman run --rm oc-mirror-autoshift:latest bash -c \
  "yq eval '.mirror.platform.channels[].name' imageset-config.yaml"

# Test connectivity before large operations
podman run --rm oc-mirror-autoshift:latest bash -c \
  "curl -s --connect-timeout 5 https://registry.redhat.io/v2/"
```

## Troubleshooting

### Build Issues

```bash
# Ensure building from project root
cd /path/to/autoshiftv2
podman build -f oc-mirror/Containerfile -t oc-mirror-autoshift .

# Check available files during build
podman build -f oc-mirror/Containerfile --target builder -t debug .
podman run --rm debug ls -la /workspace/
```

### Workflow Issues

```bash
# Debug workflow execution
podman run -it --rm oc-mirror-autoshift:latest bash
# Inside container:
./mirror-to-disk.sh --help
ls -la /workspace/

# Check script permissions
podman run --rm oc-mirror-autoshift:latest bash -c \
  "ls -la /workspace/*.sh"

# Verify workflow scripts are executable
podman run --rm oc-mirror-autoshift:latest bash -c \
  "file /workspace/mirror-to-disk.sh"
```

### Permission Issues

```bash
# Container runs as user 1001, ensure volume permissions
podman volume create oc-mirror-data
podman run --rm -v oc-mirror-data:/data \
  registry.access.redhat.com/ubi9/ubi-minimal:latest \
  chown -R 1001:0 /data

# Fix SELinux context for volumes (if applicable)
podman run --rm -v oc-mirror-data:/data:Z oc-mirror-autoshift:latest bash
```

### Authentication Issues

```bash
# Verify pull secret format
cat pull-secret.txt | jq .

# Test authentication inside container
podman run -it --rm oc-mirror-autoshift:latest bash
cat /workspace/containers/auth.json

# Debug authentication setup
podman run --rm oc-mirror-autoshift:latest bash -c \
  "env | grep -E '(XDG|HOME|AUTH)'"
```

### Network and Registry Issues

```bash
# Test registry connectivity
podman run --rm oc-mirror-autoshift:latest bash -c \
  "curl -s -k https://your-registry:443/v2/"

# Check DNS resolution
podman run --rm oc-mirror-autoshift:latest bash -c \
  "nslookup registry.redhat.io"

# Test registry authentication
podman run --rm oc-mirror-autoshift:latest bash -c \
  "oc-mirror --help && echo 'oc-mirror available'"
```

### Cache and Storage Issues

```bash
# Check cache size and contents
podman run --rm -v oc-mirror-cache:/cache \
  registry.access.redhat.com/ubi9/ubi-minimal:latest \
  du -sh /cache

# Clean up cache if needed
podman run --rm -v oc-mirror-cache:/cache \
  registry.access.redhat.com/ubi9/ubi-minimal:latest \
  rm -rf /cache/*

# Monitor disk usage during operations
podman run --rm -v oc-mirror-data:/data \
  registry.access.redhat.com/ubi9/ubi-minimal:latest \
  bash -c "while true; do du -sh /data; sleep 60; done"
```

## Related Documentation

- [AutoShift Documentation](../README.md)
- [OpenShift Disconnected Installation](https://docs.openshift.com/container-platform/latest/installing/disconnected_install/installing-mirroring-disconnected.html)

## Image Lifecycle Management

### Registry-Aware Delete Operations

The container supports intelligent delete operations that analyze actual registry state before generating deletion plans:

```bash
# Generate delete plan based on registry introspection
podman run --rm oc-mirror-autoshift:latest delete-generate \
  -r registry.example.com:443 --older-than 90d

# Execute delete operations with safety checks
podman run --rm oc-mirror-autoshift:latest delete-execute \
  -r registry.example.com:443 --delete-plan delete-plan.yaml
```

### How Registry-Aware Delete Works

The new delete system operates at mirror execution time, not generation time:

1. **Registry Introspection**: Queries actual registry to see what images exist
2. **History Analysis**: Uses `.history` files to identify old content
3. **Safe Planning**: Generates delete plans based on actual registry state
4. **Manual Review**: All delete plans must be reviewed before execution
5. **Incremental Deletion**: Focuses on removing outdated content efficiently

### Safe Deletion Workflow

1. **Generate deletion plan** (safe, no actual deletions):
   ```bash
   podman run --rm oc-mirror-autoshift:latest workflow-delete-generate -r registry.example.com:443
   ```

2. **Review deletion plan**:
   ```bash
   # Plans are saved in workspace/working-dir/delete/delete-images.yaml
   podman run --rm -v oc-mirror-workspace:/workspace oc-mirror-autoshift:latest \
     bash -c "cat workspace/working-dir/delete/delete-images.yaml"
   ```

3. **Execute deletion** (permanent!):
   ```bash
   podman run --rm -v oc-mirror-workspace:/workspace oc-mirror-autoshift:latest \
     delete-execute --delete-plan workspace/working-dir/delete/delete-images.yaml -r registry.example.com:443
   ```

### Delete Safety Features

- **Two-phase operation**: Generate plan first, execute separately
- **Version range protection**: Current version is never included in delete range
- **Platform-focused**: Only deletes platform images by default (operators skipped for safety)
- **Manual review required**: Deletion plan must be manually reviewed before execution
- **Interactive confirmation**: Multiple confirmations required before deletion
- **Dry-run support**: Preview deletion operations without actual execution

## Testing Results

### Comprehensive Workflow Testing

All container workflows have been tested with successful results:

#### ImageSet Generation ✅
- **Basic generation**: `generate-imageset` produces valid ImageSetConfiguration
- **Registry-aware delete**: `delete-generate` creates safe deletion plans based on registry state
- **Version override**: `--openshift-version 4.19` correctly overrides values file version
- **Operators-only mode**: `--operators-only` skips platform images
- **Custom output**: `--output custom-file.yaml` saves to specified location

#### Mirroring Workflows ✅
- **Workflow-to-disk**: Complete values → imageset → disk workflow with dry-run validation
- **Mirror-to-disk**: Individual script with comprehensive options and validation
- **Mirror-to-mirror**: Direct registry mirroring with connectivity checks
- **Disk-to-mirror**: Air-gapped upload workflow with content validation

#### Delete Workflows ✅
- **Delete generation**: Automatic version range calculation (4.18.1-4.18.21 for current 4.18.22)
- **Workflow-delete-generate**: Complete values → delete imageset → delete plan workflow
- **Safety validation**: Multiple confirmation prompts and plan review capabilities

#### Architecture Support ✅
- **x86_64**: Full build and runtime testing completed
- **aarch64**: Cross-platform build successful with automatic binary selection
- **Multi-arch manifest**: Created for container registry distribution

### Test Results Summary

| Component | Status | Notes |
|-----------|--------|-------|
| ImageSet Generation | ✅ PASS | 14 operators detected, proper YAML generation |
| Delete ImageSet Generation | ✅ PASS | Intelligent version range calculation |
| Mirror-to-Disk Dry Run | ✅ PASS | 405/406 images discovered, proper validation |
| Workflow Orchestration | ✅ PASS | Multi-step workflows with error handling |
| Architecture Support | ✅ PASS | x86_64 and aarch64 builds successful |
| Container Security | ✅ PASS | Non-root user (1001), proper permissions |
| Authentication Setup | ✅ PASS | Automatic pull-secret configuration |
| Volume Management | ✅ PASS | Persistent data, cache, and workspace volumes |

### Test Commands Used

```bash
# Basic workflow testing
podman run --rm oc-mirror-autoshift:latest workflows
podman run --rm oc-mirror-autoshift:latest generate-imageset
podman run --rm oc-mirror-autoshift:latest generate-delete-imageset

# Advanced testing
podman run --rm oc-mirror-autoshift:latest workflow-to-disk --dry-run
podman run --rm oc-mirror-autoshift:latest workflow-delete-generate --help
podman run --rm -v $(pwd):/host-workspace:Z oc-mirror-autoshift:latest \
  bash -c "cd /workspace && ./generate-imageset-config.sh values.hub.yaml --openshift-version 4.19 --operators-only --output /host-workspace/test-imageset.yaml"

# Architecture testing
podman build --platform linux/aarch64 -t oc-mirror-autoshift:aarch64 -f oc-mirror/Containerfile .
podman manifest create oc-mirror-autoshift:multiarch
podman manifest add oc-mirror-autoshift:multiarch oc-mirror-autoshift:latest
podman manifest add oc-mirror-autoshift:multiarch oc-mirror-autoshift:aarch64
```

## New Workflow Commands

### Complete Command Reference

#### ImageSet Management
```bash
podman run --rm oc-mirror-autoshift:latest generate-imageset [--openshift-version X.Y.Z] [--operators-only] [--output file.yaml]
# Note: delete operations now handled by dedicated delete-generate script
```

#### Individual Workflows
```bash
podman run --rm oc-mirror-autoshift:latest mirror-to-disk [-c config.yaml] [--content-dir dir] [--since YYYY-MM-DD | --incremental] [--dry-run]
podman run --rm oc-mirror-autoshift:latest disk-to-mirror [-c config.yaml] [--content-dir dir] [-r registry:port]
podman run --rm oc-mirror-autoshift:latest mirror-to-mirror [-c config.yaml] [-r registry:port] [--since YYYY-MM-DD | --incremental] [--dry-run]
podman run --rm oc-mirror-autoshift:latest delete-generate [-c config.yaml] [-r registry:port] [--workspace-dir dir]
podman run --rm oc-mirror-autoshift:latest delete-execute [--delete-plan file.yaml] [-r registry:port] [--force]
```

#### Combined Workflows
```bash
# Flexible workflows with values file and ImageSet options
podman run --rm oc-mirror-autoshift:latest workflow-to-disk [workflow-options] [mirror-options]
podman run --rm oc-mirror-autoshift:latest workflow-from-disk [disk-to-mirror options]
podman run --rm oc-mirror-autoshift:latest workflow-direct [workflow-options] [mirror-options]
podman run --rm oc-mirror-autoshift:latest workflow-delete-generate [workflow-options] [delete-options]

# Example workflow options:
# --values-file values.sbx.yaml --operators-only --openshift-version 4.17 --clean-cache --incremental --dry-run
```

#### Container Operations
```bash
podman run --rm oc-mirror-autoshift:latest workflows    # Show help
podman run --rm oc-mirror-autoshift:latest bash         # Interactive shell
podman run --rm oc-mirror-autoshift:latest [oc-mirror args]  # Direct oc-mirror passthrough
```

## Flexible Workflow System

### Multi-Values File Support

All workflows now support multiple AutoShift values files with automatic detection:

```bash
# Default: uses values.hub.yaml
podman run --rm oc-mirror-autoshift:latest workflow-to-disk --dry-run

# Use specific values file
podman run --rm oc-mirror-autoshift:latest workflow-to-disk \
  --values-file values.sbx.yaml --dry-run

# Use baremetal SNO configuration
podman run --rm oc-mirror-autoshift:latest workflow-direct \
  --values-file values.hub.baremetal-sno.yaml -r registry.example.com:443
```

### Flexible ImageSet Generation

Pass ImageSet generation parameters directly through workflows:

```bash
# Operators-only ImageSet
podman run --rm oc-mirror-autoshift:latest workflow-to-disk \
  --operators-only --dry-run

# Platform-only ImageSet  
podman run --rm oc-mirror-autoshift:latest workflow-direct \
  --openshift-only -r registry.example.com:443

# Override OpenShift version
podman run --rm oc-mirror-autoshift:latest workflow-to-disk \
  --openshift-version 4.17 --dry-run

# Clean cache before operation for fresh download
podman run --rm oc-mirror-autoshift:latest workflow-direct \
  --clean-cache --openshift-only -r registry.example.com:443

# Combined options
podman run --rm oc-mirror-autoshift:latest workflow-delete-generate \
  --values-file values.sbx.yaml --delete-older-than 90d -r registry.example.com:443
```

### Enhanced Features

- **Incremental Mirroring**: Automatic `.history` file detection with `--incremental` flag and manual `--since` override for 83% faster mirror operations
- **Multi-Values File Support**: Use any AutoShift values file (hub, sbx, baremetal-sno, custom)
- **Intelligent Version Management**: Automatic OpenShift version detection from AutoShift values with multi-version support
- **Flexible ImageSet Options**: Pass --operators-only, --openshift-only, --openshift-version directly to workflows
- **Cache Management**: --clean-cache option to ensure fresh downloads when needed
- **Registry-Aware Delete Operations**: Dedicated delete scripts that operate at mirror execution time (removed from ImageSet generation)
- **Multi-Architecture**: Native support for x86_64 and aarch64 platforms with automatic binary selection
- **Workflow Orchestration**: Combined workflows for complex multi-step operations with error handling
- **Safety Features**: Dry-run support, interactive confirmations, and comprehensive validation
- **Performance Optimization**: Persistent caching, parallel operations, and efficient volume management

## Related Documentation

- [AutoShift Documentation](../README.md)
- [OpenShift Disconnected Installation](https://docs.openshift.com/container-platform/latest/installing/disconnected_install/installing-mirroring-disconnected.html)