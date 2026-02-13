# AutoShift Helm Chart

This Helm chart deploys AutoShift via an ArgoCD ApplicationSet that uses RHACM (Red Hat Advanced Cluster Management) to manage OpenShift cluster infrastructure through GitOps policies.

## Chart Overview

| Field | Value |
|-------|-------|
| Chart Name | `autoshift` |
| Type | `application` |
| Version | `0.0.1` |

The chart generates an ArgoCD `ApplicationSet` that dynamically creates RHACM governance policies based on cluster labels. Clusters are organized into **ClusterSets** (hub, managed, sandbox, etc.) and each cluster inherits labels from its ClusterSet, with optional per-cluster overrides.

## Values Directory Structure

Values are split into composable files that you combine via Helm's `-f` flag or ArgoCD `valueFiles`:

```
values/
  global.yaml                        # Shared config (always include first)
  clustersets/
    _example-hub.yaml                # Reference: ALL hub options (copy to create your own)
    _example-managed.yaml            # Reference: ALL managed options
    hub.yaml                         # Hub — full enterprise profile
    hub-minimal.yaml                 # Hub — minimal (GitOps + ACM only)
    hub-baremetal-sno.yaml           # Hub — baremetal single-node OpenShift
    hub-baremetal-compact.yaml       # Hub — baremetal compact (3-node)
    hubofhubs.yaml                   # Hub-of-hubs + selfManagedHubSet override
    hub1.yaml                        # Spoke hub managed by hub-of-hubs
    hub2.yaml                        # Spoke hub managed by hub-of-hubs
    managed.yaml                     # Managed spoke — full enterprise
    sbx.yaml                         # Managed spoke — sandbox
  clusters/
    _example.yaml                    # Reference: ALL per-cluster override options
```

### File Types

| Type | Directory | Key Pattern | Purpose |
|------|-----------|-------------|---------|
| Global config | `values/` | Top-level keys | Git repo, branch, dryRun, selfManagedHubSet |
| Hub clustersets | `values/clustersets/` | `hubClusterSets.<name>` | Hub cluster configuration with ACM |
| Managed clustersets | `values/clustersets/` | `managedClusterSets.<name>` | Spoke cluster configuration (no ACM) |
| Cluster overrides | `values/clusters/` | `clusters.<name>` | Per-cluster label overrides |

## How Helm Deep Merge Works

Helm merges multiple value files in order. Each file adds to or overrides the merged result:

```yaml
# File 1: global.yaml sets selfManagedHubSet: hub
# File 2: hub.yaml adds hubClusterSets.hub with all labels
# File 3: managed.yaml adds managedClusterSets.managed with all labels
# Result: all three sections exist in the merged values
```

**Key behaviors:**
- **Maps deep-merge**: `hubClusterSets.hub` from one file and `hubClusterSets.hub1` from another combine into a single `hubClusterSets` map
- **Scalars use last-file-wins**: `selfManagedHubSet: hubofhubs` in a later file overrides `selfManagedHubSet: hub` from `global.yaml`
- **Lists are replaced, not appended**: `excludePolicies` in a later file replaces the entire list

## Available Profiles

### Hub Profiles

| File | Description |
|------|-------------|
| `hub.yaml` | Full enterprise hub with all operators available. ACM, GitOps, ODF, ACS, logging, compliance, and more. |
| `hub-minimal.yaml` | Minimal hub with only GitOps and ACM. Good starting point to add features incrementally. |
| `hub-baremetal-sno.yaml` | Single-node OpenShift on baremetal. Excludes infra/worker node policies, enables LVM and master node config. |
| `hub-baremetal-compact.yaml` | 3-node compact cluster on baremetal. Excludes infra/worker node policies, enables local storage and ODF with flexible scaling. |
| `hubofhubs.yaml` | Hub-of-hubs configuration. Overrides `selfManagedHubSet` to `hubofhubs`. Use with `hub1.yaml`/`hub2.yaml` for spoke hubs. |
| `hub1.yaml` / `hub2.yaml` | Spoke hub clustersets managed by a hub-of-hubs. |

### Managed (Spoke) Profiles

| File | Description |
|------|-------------|
| `managed.yaml` | Full enterprise managed spoke cluster. All operators available for spoke clusters. |
| `sbx.yaml` | Sandbox spoke cluster. Subset of operators enabled for development/testing. |

## Composition Examples

### Standard Hub + Managed Spokes

```yaml
# ArgoCD Application valueFiles:
valueFiles:
  - values/global.yaml
  - values/clustersets/hub.yaml
  - values/clustersets/managed.yaml
```

### Minimal Hub Only

```yaml
valueFiles:
  - values/global.yaml
  - values/clustersets/hub-minimal.yaml
```

### Baremetal SNO + Managed Spokes

```yaml
valueFiles:
  - values/global.yaml
  - values/clustersets/hub-baremetal-sno.yaml
  - values/clustersets/managed.yaml
```

### Sandbox Spoke Only

```yaml
valueFiles:
  - values/global.yaml
  - values/clustersets/sbx.yaml
```

### Hub of Hubs with Spoke Hubs

```yaml
valueFiles:
  - values/global.yaml
  - values/clustersets/hubofhubs.yaml
  - values/clustersets/hub1.yaml
  - values/clustersets/hub2.yaml
```

### Hub + Managed + Per-Cluster Overrides

```yaml
valueFiles:
  - values/global.yaml
  - values/clustersets/hub.yaml
  - values/clustersets/managed.yaml
  - values/clusters/my-cluster.yaml
```

## Global Configuration

Defined in `values/global.yaml` and always loaded first:

| Key | Default | Description |
|-----|---------|-------------|
| `autoshift.dryRun` | `false` | Deploy policies in inform-only mode (no enforcement) |
| `autoshiftGitRepo` | `https://github.com/auto-shift/autoshiftv2.git` | Git repository URL for policy sources |
| `autoshiftGitBranchTag` | `main` | Git branch or tag to track |
| `selfManagedHubSet` | `hub` | Name of the clusterset that contains the hub itself |
| `versionedClusterSets` | `false` | Append version/branch suffix to clusterset names for gradual rollout |
| `excludePolicies` | `[]` | List of policy folder names to exclude from all clusters |

## Label Precedence

Labels are applied in this order (highest priority first):

1. **Per-cluster labels** (`clusters.<name>.labels`) — override everything for a specific cluster
2. **ClusterSet labels** (`hubClusterSets.<name>.labels` or `managedClusterSets.<name>.labels`) — apply to all clusters in the set
3. **Helm defaults** (`values.yaml`) — chart-level defaults

## Operator Label Pattern

Each operator follows a consistent label pattern:

```yaml
# Enable the operator
<operator>: 'true'

# Subscription configuration (required when enabled)
<operator>-subscription-name: '<package-name>'
<operator>-channel: '<channel>'
<operator>-source: 'redhat-operators'
<operator>-source-namespace: 'openshift-marketplace'

# Version pinning (optional — sets manual install plan approval)
# <operator>-version: '<csv-version>'
```

When a version is specified, the operator subscription is set to manual install plan approval and ACM only approves the exact CSV specified. When no version is set, the operator follows automatic upgrades via its channel.

## Creating Custom Profiles

1. Copy the appropriate example file:
   ```bash
   cp values/clustersets/_example-hub.yaml values/clustersets/my-hub.yaml
   cp values/clustersets/_example-managed.yaml values/clustersets/my-managed.yaml
   cp values/clusters/_example.yaml values/clusters/my-cluster.yaml
   ```

2. Edit the copy — uncomment and set the labels you need

3. Reference it in your ArgoCD Application:
   ```yaml
   valueFiles:
     - values/global.yaml
     - values/clustersets/my-hub.yaml
     - values/clustersets/my-managed.yaml
     - values/clusters/my-cluster.yaml
   ```

## Validation

Verify your values render correctly with `helm template`:

```bash
# Full hub + managed
helm template autoshift -f values/global.yaml -f values/clustersets/hub.yaml -f values/clustersets/managed.yaml

# Lint check
helm lint autoshift -f values/global.yaml -f values/clustersets/hub.yaml -f values/clustersets/managed.yaml
```
