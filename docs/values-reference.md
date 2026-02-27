# AutoShift Values Reference

## Values File Architecture

AutoShift uses a **composable values file** pattern. Instead of a single monolithic values file, configuration is split into focused files under `autoshift/values/` that you combine in your ArgoCD Application:

```
autoshift/values/
  global.yaml                        # Shared config: git repo, branch, dryRun
  clustersets/
    _example-hub.yaml                # Reference: ALL hub clusterset options
    _example-managed.yaml            # Reference: ALL managed clusterset options
    hub.yaml                         # Hub clusterset — full enterprise profile
    hub-minimal.yaml                 # Hub clusterset — minimal (GitOps + ACM only)
    hub-baremetal-sno.yaml           # Hub clusterset — baremetal single-node
    hub-baremetal-compact.yaml       # Hub clusterset — baremetal compact (3 node)
    hubofhubs.yaml                   # Hub-of-hubs clusterset + selfManagedHubSet override
    hub1.yaml                        # Spoke hub (managed by hub-of-hubs)
    hub2.yaml                        # Spoke hub (managed by hub-of-hubs)
    managed.yaml                     # Managed spoke clusterset — full enterprise
    sbx.yaml                         # Managed spoke clusterset — sandbox
  clusters/
    _example.yaml                    # Reference: ALL per-cluster override options
```

### How Composition Works

Helm **deep-merges** multiple `-f` value files in order. Each clusterset file defines a unique key (e.g., `hubClusterSets.hub`, `managedClusterSets.managed`), so they combine without conflict:

```yaml
# hub.yaml defines hubClusterSets.hub
# managed.yaml defines managedClusterSets.managed
# Result: both .hubClusterSets.hub and .managedClusterSets.managed exist
```

For scalar values like `selfManagedHubSet`, later files override earlier ones (last-file-wins). This lets profile files like `hubofhubs.yaml` override defaults from `global.yaml`.

### Precedence

Labels follow this override precedence (highest to lowest):

1. **Per-cluster overrides** (`values/clusters/my-cluster.yaml`)
2. **Clusterset labels** (`values/clustersets/hub.yaml`)
3. **Helm chart defaults** (`values.yaml`)

### Creating Custom Profiles

Copy an `_example-*.yaml` file and uncomment/modify the labels you need:

```bash
# Create a custom hub profile
cp autoshift/values/clustersets/_example-hub.yaml autoshift/values/clustersets/my-hub.yaml

# Create a custom managed profile
cp autoshift/values/clustersets/_example-managed.yaml autoshift/values/clustersets/my-managed.yaml

# Create per-cluster overrides
cp autoshift/values/clusters/_example.yaml autoshift/values/clusters/my-cluster.yaml
```

See `autoshift/README.md` for detailed chart documentation.

## Cluster Labels

Values can be set on a per cluster and clusterset level to decide what features of AutoShift will be applied to each cluster. If a value is defined in helm values, a clusterset label and a cluster label precedence will be **cluster > clusterset > helm** values where helm values is the least.

Helm chart defaults (`values.yaml`) live in each policy's Helm chart under the `policies/` directory (e.g., `policies/openshift-gitops/values.yaml`, `policies/advanced-cluster-security/values.yaml`). These defaults can be overridden by clusterset labels in your `autoshift/values/clustersets/` files, and further overridden by per-cluster labels in `autoshift/values/clusters/`.

## Operator Version Control

AutoShift v2 provides comprehensive version control for all managed operators through cluster labels. This feature allows you to pin operators to specific versions while maintaining automatic upgrade capabilities when desired.

### Version Control Behavior

When you specify a version for an operator:
- **Manual Install Plan Approval**: The operator subscription is automatically set to manual approval mode
- **Version Pinning**: ACM will only approve install plans for the exact CSV (ClusterServiceVersion) specified
- **Controlled Upgrades**: Operators will not automatically upgrade beyond the specified version

When no version is specified:
- **Automatic Upgrades**: Operators use automatic install plan approval and follow normal upgrade paths
- **Channel-based Updates**: Operators receive updates based on their configured channel (stable, latest, etc.)

### Setting Operator Versions

Operator versions are controlled through the AutoShift values files (e.g., `autoshift/values/clustersets/hub.yaml`, `autoshift/values/clustersets/sbx.yaml`, etc.) using cluster labels with the pattern `autoshift.io/OPERATOR_NAME-version`:

```yaml
# Example: Pin Advanced Cluster Security to specific version and channel
hubClusterSets:
  hub:
    labels:
      acs: 'true'
      acs-channel: 'stable'
      acs-version: 'rhacs-operator.v4.6.1'

# Example: Pin OpenShift Pipelines to specific version and channel
managedClusterSets:
  managed:
    labels:
      pipelines: 'true'
      pipelines-channel: 'pipelines-1.18'
      pipelines-version: 'openshift-pipelines-operator-rh.v1.18.1'

# Example: Remove version pinning (enables automatic upgrades)
# Simply remove or comment out the version label
# acs-version: 'rhacs-operator.v4.6.1'
```

Labels can also be set at the individual cluster level in the `clusters:` section to override cluster set defaults.

### Available Version Labels

Every managed operator supports version control via its respective label:

| Operator                    | Version Label           | Example CSV                                        |
| --------------------------- | ----------------------- | -------------------------------------------------- |
| Advanced Cluster Management | `acm-version`           | `advanced-cluster-management.v2.14.0`              |
| Advanced Cluster Security   | `acs-version`           | `rhacs-operator.v4.6.1`                            |
| OpenShift GitOps            | `gitops-version`        | `openshift-gitops-operator.v1.18.0`                |
| OpenShift Pipelines         | `pipelines-version`     | `openshift-pipelines-operator-rh.v1.18.1`          |
| OpenShift Data Foundation   | `odf-version`           | `odf-operator.v4.18.11-rhodf`                      |
| MetalLB                     | `metallb-version`       | `metallb-operator.v4.18.0-202509240837`            |
| Quay                        | `quay-version`          | `quay-operator.v3.15.0`                            |
| Developer Hub               | `dev-hub-version`       | `rhdh.v1.5.0`                                      |
| Developer Spaces            | `dev-spaces-version`    | `devspaces.v3.21.0`                                |
| Trusted Artifact Signer     | `tas-version`           | `rhtas-operator.v1.2.0`                            |
| Loki                        | `loki-version`          | `loki-operator.v6.3.0`                             |
| OpenShift Logging           | `logging-version`       | `cluster-logging.v6.3.0`                           |
| Cluster Observability       | `coo-version`           | `cluster-observability-operator.v0.4.0`            |
| Compliance Operator         | `compliance-version`    | `compliance-operator.v1.8.0`                       |
| LVM Storage                 | `lvm-version`           | `lvms-operator.v4.18.0-202410091522`               |
| Local Storage               | `local-storage-version` | `local-storage-operator.v4.18.0-202410091522`      |
| NMState                     | `nmstate-version`       | `kubernetes-nmstate-operator.v4.18.0-202410091522` |
| OpenShift Virtualization    | `virt-version`          | `kubevirt-hyperconverged.v4.18.0`                  |

### Finding Available CSV Versions

To find available CSV versions for operators, use the OpenShift CLI:

```bash
# List available CSV versions for an operator
oc get packagemanifests rhacs-operator -o jsonpath='{.status.channels[*].currentCSV}'

# Get all available versions in a channel
oc get packagemanifests openshift-pipelines-operator-rh -o yaml | grep currentCSV
```

> **Note**: Version control removes the need for install-plan-approval labels, as version specification automatically handles install plan management through ACM governance.

---

## Policy Label Reference

### Advanced Cluster Manager

> [!WARNING]
> Hub Clusters Only

| Variable                    | Type      | Default Value             | Notes |
|-----------------------------|-----------|---------------------------|-------|
| `self-managed`              | bool      | `true` or `false`         |       |
| `acm-enable-provisioning`   | bool      | `false`                   | Configures ACM to provision clusters |
| `acm-provisioning-storage-class` | string |                         | (optional) name of StorageClass to use if non default is desired |
| `acm-provisioning-database-size` | string | `10Gi`                  | DatabaseStorage defines the spec of the PersistentVolumeClaim to be
created for the database's filesystem. Minimum 10GiB is recommended. |
| `acm-provisioning-filesystem-storage-size` | string | `100Gi`       | FileSystemStorage defines the spec of the PersistentVolumeClaim to be
created for the assisted-service's filesystem (logs, etc). Minimum 100GiB recommended |
| `acm-provisioning-image-storage-size` | string | `50Gi`             | ImageStorage defines the spec of the PersistentVolumeClaim to be
created for each replica of the image service. 2GiB per OSImage entry is required. |
| `acm-channel`               | string    | `release-2.14`            |       |
| `acm-version`               | string    | (optional)                | Specific CSV version for controlled upgrades |
| `acm-source`                | string    | `redhat-operators`        |       |
| `acm-source-namespace`      | string    | `openshift-marketplace`   |       |
| `acm-availability-config`   | string    | `Basic` or `High`         |       |
| `acm-observability`         | bool      | `true` or `false`         | this will enable observability utilizing a noobaa bucket for acm. ODF will have to be enabled as well |
| `acm-search-storage`        | bool      | `true` or `false`         | Enable persistent storage for ACM Search (recommended for production) |
| `acm-search-storage-class`  | string    | `ocs-storagecluster-ceph-rbd` | Storage class for Search database |
| `acm-search-storage-size`   | string    | `100Gi`                   | Storage size for Search database. Sizing: Small (<50 clusters): 20Gi, Medium (50-200): 50Gi, Large (200-500): 100Gi, Very Large (500+): 200Gi+ |

### Cluster Labels

Manages the automated cluster labeling system that applies `autoshift.io/` prefixed labels to clusters and cluster sets. This policy automatically propagates labels from cluster sets to individual clusters and manages the label hierarchy.

### MetalLB

| Variable                            | Type              | Default Value             | Notes |
|-------------------------------------|-------------------|---------------------------|-------|
| `metallb`                           | bool              | `true` or `false`         | If not set MetalLB will not be managed |
| `metallb-source`                    | string            | redhat-operators          |  |
| `metallb-source-namespace`          | string            | openshift-marketplace     |  |
| `metallb-version`                   | string            | (optional)                | Specific CSV version for controlled upgrades |
| `metallb-channel`                   | string            | stable                    |  |
| `metallb-quota`                     | bool              | `false`                   | Enable resource quotas for MetalLB namespace |
| `metallb-quota-cpu`                 | int               | `2`                       | Number of cpu for Resource Quota on namespace |
| `metallb-quota-memory`              | string            | 2Gi                       | Amount of memory for Resource Quota on namespace (example: 2Gi or 512Mi) |
| `metallb-ippool-1`                  | string            |                           | Name of config file for IP Pool (copy this value if more than one, increasing number each time) |
| `metallb-l2-1`                      | string            |                           | Name of config file for L2 Advertisement (copy this value if more than one, increasing number each time) |
| `metallb-bgp-1`                     | string            |                           | Name of config file for BGP Advertisement (copy this value if more than one, increasing number each time) |
| `metallb-peer-1`                    | string            |                           | Name of config file for BGP Peer (copy this value if more than one, increasing number each time) |

### OpenShift GitOps

Manages the OpenShift GitOps operator installation and systems ArgoCD instance. This policy ensures the GitOps operator is installed and creates the main ArgoCD instance used by AutoShift to declaratively manage all cluster configurations.

| Variable                        | Type      | Default Value             | Notes |
|---------------------------------|-----------|---------------------------|-------|
| `gitops`                        | bool      |                           | If not set to `true`, OpenShift GitOps will not be managed |
| `gitops-channel`                | string    | `latest`                  | Operator channel for GitOps updates |
| `gitops-version`                | string    | (optional)                | Specific CSV version for controlled upgrades |
| `gitops-source`                 | string    | `redhat-operators`        | Operator catalog source |
| `gitops-source-namespace`       | string    | `openshift-marketplace`   | Namespace for operator catalog |
| `gitops-cluster-ca-bundle`      | bool      | `false`                   | Inject cluster trusted CA bundle into ArgoCD repo server |


### Master Nodes

Single Node OpenShift clusters as well as Compact Clusters have to rely on their master nodes to handle workloads. You may have to increase the number of pods per node in these resource constrained environments.

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `master-nodes`                    | bool              | `false`                   |       |
| `master-max-pods`                 | int               | `250`                     | The number of maximum pods per node. Up to 2500 supported dependent on hardware |

### Machine Health Checks

Automated node health monitoring and remediation.

| Variable                          | Type   | Default | Notes |
|-----------------------------------|--------|---------|-------|
| `machine-health-checks`           | bool   |         | Enable the MachineHealthCheck policy |
| `machine-health-checks-worker`    | bool   |         | Enable MHC for all worker MachineSets (timeout=300s, maxUnhealthy=40%) |
| `machine-health-checks-infra`     | bool   |         | Enable MHC for all infra MachineSets (timeout=300s, maxUnhealthy=40%) |
| `machine-health-checks-storage`   | bool   |         | Enable MHC for storage MachineSets (timeout=600s, maxUnhealthy=1) |

**Notes:**
- Storage nodes are identified by `cluster.ocs.openshift.io/openshift-storage` label (same as ODF)
- Storage uses longer timeouts and maxUnhealthy=1 to allow Ceph recovery
- Never create MachineHealthChecks for control plane nodes

### Infra Nodes

| Variable                            | Type              | Default Value             | Notes |
|-------------------------------------|-------------------|---------------------------|-------|
| `infra-nodes`                       | int               |                           | Number of infra nodes. If not set infra nodes are not managed, if 0 infra nodes will be deleted |
| `infra-nodes-provider`              | string            |                           | Provider type - 'aws', 'vmware', or 'test' |
| `infra-nodes-instance-type`         | string            |                           | AWS instance type |
| `infra-nodes-numcpu`                | int               |                           | Number of cpu per infra node |
| `infra-nodes-memory-mib`            | int               |                           | Memory mib per infra node |
| `infra-nodes-numcores-per-socket`   | int               |                           | Number of CPU Cores per socket |
| `infra-nodes-zone-[number]`         | string            |                           | Availability zone (e.g., infra-nodes-zone-1: 'us-east-2a') |



### Worker Nodes

| Variable                            | Type              | Default Value             | Notes |
|-------------------------------------|-------------------|---------------------------|-------|
| `worker-nodes`                      | int               |                           | Number of worker nodes min if autoscale. If not set worker nodes are not managed, if blank worker nodes will be deleted |
| `worker-nodes-numcpu`               | int               |                           | Number of cpu per worker node |
| `worker-nodes-memory-mib`           | int               |                           | Memory mib per worker node |
| `worker-nodes-numcores-per-socket`  | int               |                           | Number of CPU Cores per socket |
| `worker-nodes-zones`                | <list<String>>    |                           | List of availability zones |

### Storage Nodes

| Variable                            | Type           | Default Value | Notes |
| ----------------------------------- | -------------- | ------------- | ----- |
| `storage-nodes`                     | int            |               | Number of storage nodes min if autoscale. If not set storage nodes are not managed, if blank storage nodes will be deleted. Local Storage Operator will be installed if Storage Nodes are enabled |
| `storage-nodes-numcpu`              | int            |               | Number of cpu per storage node  |
| `storage-nodes-memory-mib`          | int            |               | Memory mib per storage node |
| `storage-nodes-numcores-per-socket` | int            |               | Number of CPU Cores per socket |
| `storage-nodes-zone-[number]`       | string         |               | Availability zone (e.g., storage-nodes-zone-1: 'us-east-2a') |
| `storage-nodes-instance-type`       | string         |               | Instance type for cloud provider |
| `storage-nodes-provider`            | string         |               | Provider type; valid choices: aws, vmware, baremetal |
| `storage-nodes-node-[iterator]`     | <list<String>> |               | List of node names to apply storage label to. Used for baremetal where MachineSets aren't used. |

### Advanced Cluster Security

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `acs`                             | bool              |                           | If not set Advanced Cluster Security will not be managed |
| `acs-egress-connectivity`         | string            | `Online`                  | Options are `Online` or `Offline`, use `Offline` if disconnected |
| `acs-channel`                     | string            | `stable`                  |       |
| `acs-version`                     | string            | (optional)                | Specific CSV version for controlled upgrades |
| `acs-source`                      | string            | `redhat-operators`        |       |
| `acs-source-namespace`            | string            | `openshift-marketplace`   |       |
| `acs-scanner-v4`                  | string            | `Enabled`                 | Scanner V4 component state (`Enabled` or `Disabled`) |
| `acs-monitoring`                  | bool              | `true`                    | Enable OpenShift monitoring integration for Central and SecuredCluster |
| `acs-vm-scanning`                 | bool              |                           | Enable VM scanning (Developer Preview, opt-in) |
| `acs-admission-control`           | bool              |                           | Enable admission control enforcement on SecuredCluster (opt-in, can block deployments) |
| `acs-network-policies`            | string            |                           | Network policy generation (`Enabled` or `Disabled`), only set when explicit control needed |
| `acs-auth-provider`               | string            | `openshift`               | Auth provider type (`openshift`). Hub only. Configures declarative RBAC |
| `acs-auth-min-role`               | string            | `None`                    | Minimum role for authenticated users. Hub only |
| `acs-auth-admin-group`            | string            | `cluster-admins`          | Group mapped to Admin role. Hub only |
| `acs-default-policies`            | bool              |                           | Deploy baseline SecurityPolicy CRDs (no privilege escalation, no root, no shell). Hub only |

### Developer Spaces

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `dev-spaces`                          | bool              |                           | If not set Developer Spaces will not be managed |
| `dev-spaces-channel`                  | string            | `stable`                  |       |
| `dev-spaces-version`                  | string            | (optional)                | Specific CSV version for controlled upgrades |
| `dev-spaces-source`                   | string            | `redhat-operators`        |       |
| `dev-spaces-source-namespace`         | string            | `openshift-marketplace`   |       |

### Developer Hub

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `dev-hub`                         | bool              |                           | If not set Developer Hub will not be managed |
| `dev-hub-channel`                 | string            | `fast`                    |       |
| `dev-hub-version`                 | string            | (optional)                | Specific CSV version for controlled upgrades |
| `dev-hub-source`                  | string            | `redhat-operators`        |       |
| `dev-hub-source-namespace`        | string            | `openshift-marketplace`   |       |

### OpenShift Pipelines

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `pipelines`                       | bool              |                           | If not set OpenShift Pipelines will not be managed |
| `pipelines-channel`               | string            | `latest`                  |       |
| `pipelines-version`               | string            | (optional)                | Specific CSV version for controlled upgrades |
| `pipelines-source`                | string            | `redhat-operators`        |       |
| `pipelines-source-namespace`      | string            | `openshift-marketplace`   |       |

### Trusted Artifact Signer

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `tas`                             | bool              |                           | If not set Trusted Artifact Signer will not be managed |
| `tas-channel`                     | string            | `stable`                  |       |
| `tas-version`                     | string            | (optional)                | Specific CSV version for controlled upgrades |
| `tas-source`                      | string            | `redhat-operators`        |       |
| `tas-source-namespace`            | string            | `openshift-marketplace`   |       |

### Quay

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `quay`                            | bool              |                           | If not set Quay will not be managed |
| `quay-channel`                    | string            | `stable-3.13`             |       |
| `quay-version`                    | string            | (optional)                | Specific CSV version for controlled upgrades |
| `quay-source`                     | string            | `redhat-operators`        |       |
| `quay-source-namespace`           | string            | `openshift-marketplace`   |       |

### OpenShift Virtualization

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `virt`                            | bool              |                           | If not set OpenShift Virtualization will not be managed |
| `virt-channel`                    | string            | `stable`                  | KubeVirt-based virtualization platform for running VMs on OpenShift |
| `virt-version`                    | string            | (optional)                | Specific CSV version for controlled upgrades |
| `virt-source`                     | string            | `redhat-operators`        |       |
| `virt-source-namespace`           | string            | `openshift-marketplace`   |       |

### Developer OpenShift Gitops

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `gitops-dev`                          | bool              |                           | If not set Developer OpenShift Gitops intances will not be managed |
| `gitops-dev-team-{INSERT_TEAM_NAME}`  | string        |                           | Team that can deploy onto cluster from dev team gitops. Must match a team in the `gitops-dev` helm chart values file |

### Loki

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `loki`                            | bool              |                           | If not set Loki will not be managed. Dependent on ODF Multi Object Gateway |
| `loki-channel`                    | string            | `stable-6.2`              |       |
| `loki-version`                    | string            | (optional)                | Specific CSV version for controlled upgrades |
| `loki-source`                     | string            | `redhat-operators`        |       |
| `loki-source-namespace`           | string            | `openshift-marketplace`   |       |
| `loki-size`                       | string            | `1x.extra-small`          |       |
| `loki-storageclass`               | string            | `gp3-csi`                 |       |
| `loki-lokistack-name`             | string            | `logging-lokistack`       |       |

### OpenShift Logging

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `logging`                         | bool              |                           | If not set OpenShift Logging will not be managed, Dependent on Loki and COO |
| `logging-channel`                 | string            | `stable-6.2`              |       |
| `logging-version`                 | string            | (optional)                | Specific CSV version for controlled upgrades |
| `logging-source`                  | string            | `redhat-operators`        |       |
| `logging-source-namespace`        | string            | `openshift-marketplace`   |       |

### Cluster Observability Operator

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `coo`                             | bool              |                           | If not set Cluster Observability Operator will not be managed |
| `coo-channel`                     | string            | `stable`                  |       |
| `coo-version`                     | string            | (optional)                | Specific CSV version for controlled upgrades |
| `coo-source`                      | string            | `redhat-operators`        |       |
| `coo-source-namespace`            | string            | `openshift-marketplace`   |       |

### Compliance Operator STIG Apply

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `compliance`                          | bool              |                           | If not set Compliance Operator will not be managed. Helm chart config map must be set with profiles and remediations |
| `compliance-auto-remediate`           | bool              | `true`                    |       |
| `compliance-storage-class`            | string            |                           | StorageClass for compliance scan raw results. Use when default StorageClass isn't available on master nodes (e.g., Ceph RBD) |
| `compliance-subscription-name`        | string            | `compliance-operator`     |       |
| `compliance-version`                  | string            | (optional)                | Specific CSV version for controlled upgrades |
| `compliance-source`                   | string            | `redhat-operators`        |       |
| `compliance-source-namespace`         | string            | `openshift-marketplace`   |       |
| `compliance-channel`                  | string            | `stable`                  |       |

### LVM Operator

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `lvm`                                 | bool              | `false`                   | If not set the LVM Operator will not be managed |
| `lvm-channel`                         | string            | `stable-4.18`             | Operator channel |
| `lvm-version`                         | string            | (optional)                | Specific CSV version for controlled upgrades |
| `lvm-source`                          | string            | `redhat-operators`        | Operator catalog source |
| `lvm-source-namespace`                | string            | `openshift-marketplace`   | Catalog namespace |
| `lvm-default`                         | bool              | `true`                    | Sets the lvm-operator as the default Storage Class |
| `lvm-fstype`                          | string            | `xfs`                     | Options `xfs` `ext4` |
| `lvm-size-percent`                    | int               | `90`                      | Percentage of the Volume Group to use for the thinpool |
| `lvm-overprovision-ratio`             | int               | `10`                      |       |

### Local Storage Operator

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `local-storage`                       | bool              |                           | if not set to true, local storage will not be managed or deployed. |
| `local-storage-channel`               | string            | `stable`                  | Operator channel |
| `local-storage-version`               | string            | (optional)                | Specific CSV version for controlled upgrades |
| `local-storage-source`                | string            | `redhat-operators`        | Operator catalog source |
| `local-storage-source-namespace`      | string            | `openshift-marketplace`   | Catalog namespace |

### Ansible Automation Platform

| Variable                         | Type      | Default Value              | Notes |
|----------------------------------|-----------|----------------------------|-------|
| `aap`                            | bool      | `true` or `false`          |  |
| `aap-channel`                    | string    | `stable-2.5`             |  |
| `aap-install-plan-approval`      | string    | `Automatic`                |  |
| `aap-source`                     | string    | `redhat-operators`         |  |
| `aap-hub-disabled`               | bool      | `true` or `false`          | 'false' will include Hub content storage in your deployment, 'true' will omit.       |
| `aap-file-storage`               | bool      | `true` or `false`          | 'false' will use file storage for Hub content storage in your deployment, 'true' will omit. |
| `aap-file_storage_storage_class` | string    | `ocs-storagecluster-cephfs`| you will set the storage class for your file storage, defaults to ODF. you must have a RWX capable storage class if using anything else. |
| `aap-file_storage_size`          | bool      | `10G`                      | set the pvc claim size for your file storage.  |
| `aap-s3-storage`                 | bool      | `true` or `false`          | 'false' will use ODF NooBa for Hub content storage in your deployment, 'true' will omit. |
| `aap-eda-disabled`               | bool      | `true` or `false`          | 'false' will include EDA in your deployment, 'true' will omit. |
| `aap-lightspeed-disabled`        | bool      | `true` or `false`          | 'false' will include Ansible Lightspeed in your deployment, 'true' will omit. |
| `aap-version`                    | bool      | `aap-operator.v2.6.0-0.1762261205`          | Specific CSV version for controlled upgrades  |
| `aap-custom-cabundle`            | bool      | `true` or `false`          | 'true' will inject cluster CA Bundle into AAP CRD |
| `aap-cabundle-name`              | string    | `user-ca-bundle`           |  name of the secret to be created for CA Bundle injection |

### OpenShift Data Foundation

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `odf`                             | bool              |                           | If not set OpenShift Data Foundation will not be managed. if Storage Nodes are enable will deploy ODF on local storage/ storage nodes |
| `odf-multi-cloud-gateway`         | string            |                           | values `standalone` or `standard`. Install ODF with only noobaa object gateway or full odf |
| `odf-noobaa-pvpool`                | bool              |                           | if not set noobaa will be deployed with default settings. Recommended don't set for cloud providers. Use pv pool for storage |
| `odf-noobaa-store-size`            | string            |                           | example `500Gi`. if pvpool set. Size of noobaa backing store |
| `odf-noobaa-store-num-volumes`     | string            |                           | example `1`. if pvpool set. number of volumes |
| `odf-ocs-storage-class-name`      | string            |                           | if not using local-storage, storage class to use for ocs |
| `odf-ocs-storage-size`            | string            |                           | storage size per nvme |
| `odf-ocs-storage-count`           | string            |                           | number of replica sets of nvme drives, note total amount will count * replicas |
| `odf-ocs-storage-replicas`        | string            |                           | replicas, `3` is recommended; if using flexibleScaling use `1` |
| `odf-ocs-flexible-scaling`        | bool              | `false`*                  | Sets failure domain to host and evenly spreads OSDs over hosts. Defaults to true on baremetal with a number of storage nodes that isn't a multiple of 3 |
| `odf-resource-profile`            | string            | `balanced`                | `lean`: suitable for clusters with limited resources, `balanced`: suitable for most use cases, `performance`: suitable for clusters with high amount of resources |
| `odf-default-storageclass`        | string            | `ocs-storagecluster-ceph-rbd` | Sets specified storage class as default and all others as non-default |
| `odf-csi-all-nodes`              | bool              | `false`                   | `true` runs CSI plugins on all nodes (masters, infra, storage) allowing PVCs on non-storage nodes. `false` restricts CSI plugins to storage-labeled nodes only |
| `odf-channel`                     | string            | `stable-4.20`             |       |
| `odf-version`                     | string            | (optional)                | Specific CSV version for controlled upgrades |
| `odf-source`                      | string            | `redhat-operators`        |       |
| `odf-source-namespace`            | string            | `openshift-marketplace`   |       |
| `odf-default-storageclass`        | string            | `ocs-storagecluster-ceph-rbd` | Sets specified storage class as default and all others as non-default |

### OpenShift Internal Registry
| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `imageregistry`                   | bool              | `false`                   | If not set OpenShift Internal Image Registry will not be managed |
| `imageregistry-management-state`  | string            | `Managed`                 | Can be set to `Managed` and `Unmanaged`, though only `Managed` is supported |
| `imageregistry-replicas`          | int               |                           | Need at least `2`, as well as read write many storage or object/s3 storage in order support HA and Rolling Updates |
| `imageregistry-storage-type`      | string            |                           | Supported `s3` or `pvc`, s3 only supports Nooba |
| `imageregistry-s3-region`         | string            |                           | If type is `s3` you can specify a region |
| `imageregistry-pvc-access-mode`   | string            |                           | Example `ReadWriteMany` |
| `imageregistry-pvc-storage`       | string            | `100Gi`                   | PVC size (default: '100Gi') |
| `imageregistry-pvc-storage-class` | string            |                           | Example `ocs-storagecluster-ceph-rbd` |
| `imageregistry-pvc-volume-mode`   | string            | `Filesystem`              | Example `Block` or `Filesystem` |
| `imageregistry-rollout-strategy`  | string            | `Recreate`                | Example `RollingUpdate` if at least 2 or `Recreate` if only 1 |

### OpenShift DNS

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `dns-tolerations`                 | bool              |                           | If set, applies DNS operator tolerations for specialized node configurations |
| `dns-node-placement`              | string            |                           | Node placement configuration for DNS pods |

### Kubernetes NMState Operator

The Kubernetes NMState Operator declaratively configures Red Hat CoreOS network settings including bonds, VLANs, static routes, and DNS. Network configuration can be defined entirely through labels without requiring configuration files.

See [policies/nmstate/README.md](../policies/nmstate/README.md) for detailed documentation and examples.

#### Operator Configuration

| Variable                        | Type           | Default Value         | Notes                                                                             |
| ------------------------------- | -------------- | --------------------- | --------------------------------------------------------------------------------- |
| `nmstate`                       | bool           | `false`               | Enable/disable the NMState Operator                                               |
| `nmstate-channel`               | string         | `stable`              | Operator channel                                                                  |
| `nmstate-version`               | string         | (optional)            | Specific CSV version for controlled upgrades                                      |
| `nmstate-source`                | string         | `redhat-operators`    | Operator catalog source                                                           |
| `nmstate-source-namespace`      | string         | `openshift-marketplace` | Catalog namespace                                                               |

#### Bond Interface Configuration

| Variable                                  | Type     | Default Value  | Notes                                                      |
| ----------------------------------------- | -------- | -------------- | ---------------------------------------------------------- |
| `nmstate-bond-{N}`                        | string   |                | Bond interface name (e.g., `bond0`)                        |
| `nmstate-bond-{N}-mode`                   | string   |                | `802.3ad`, `active-backup`, `balance-rr`, etc.             |
| `nmstate-bond-{N}-port-{M}`               | string   |                | Port interface name (e.g., `eno1`)                         |
| `nmstate-bond-{N}-mtu`                    | string   |                | MTU size (e.g., `9000`)                                    |
| `nmstate-bond-{N}-mac`                    | string   |                | MAC address with dots (e.g., `aa.bb.cc.dd.ee.ff`)          |
| `nmstate-bond-{N}-miimon`                 | string   |                | MII monitoring interval in ms                              |
| `nmstate-bond-{N}-ipv4`                   | string   |                | `dhcp`, `static`, or `disabled`                            |
| `nmstate-bond-{N}-ipv4-address-{M}`       | string   |                | Static IP address (e.g., `192.168.1.10`)                   |
| `nmstate-bond-{N}-ipv4-address-{M}-cidr`  | string   |                | CIDR prefix length (e.g., `24`)                            |
| `nmstate-bond-{N}-ipv6`                   | string   |                | `dhcp`, `autoconf`, `static`, or `disabled`                |

#### VLAN Interface Configuration

| Variable                                  | Type     | Default Value  | Notes                                                      |
| ----------------------------------------- | -------- | -------------- | ---------------------------------------------------------- |
| `nmstate-vlan-{N}`                        | string   |                | VLAN interface name (e.g., `bond0.100`)                    |
| `nmstate-vlan-{N}-id`                     | string   |                | VLAN ID (e.g., `100`)                                      |
| `nmstate-vlan-{N}-base`                   | string   |                | Base interface (e.g., `bond0`)                             |
| `nmstate-vlan-{N}-mtu`                    | string   |                | MTU size                                                   |
| `nmstate-vlan-{N}-ipv4`                   | string   |                | `dhcp`, `static`, or `disabled`                            |
| `nmstate-vlan-{N}-ipv4-address-{M}`       | string   |                | Static IP address                                          |
| `nmstate-vlan-{N}-ipv4-address-{M}-cidr`  | string   |                | CIDR prefix length                                         |

#### Ethernet Interface Configuration

| Variable                                  | Type     | Default Value  | Notes                                                      |
| ----------------------------------------- | -------- | -------------- | ---------------------------------------------------------- |
| `nmstate-ethernet-{N}`                    | string   |                | Interface name (e.g., `eno1`)                              |
| `nmstate-ethernet-{N}-mac`                | string   |                | MAC address with dots                                      |
| `nmstate-ethernet-{N}-mtu`                | string   |                | MTU size                                                   |
| `nmstate-ethernet-{N}-state`              | string   | `up`           | `up` or `down`                                             |

#### Static Route Configuration

| Variable                                  | Type     | Default Value  | Notes                                                      |
| ----------------------------------------- | -------- | -------------- | ---------------------------------------------------------- |
| `nmstate-route-{N}-dest`                  | string   |                | Destination network (e.g., `10.0.0.0`)                     |
| `nmstate-route-{N}-cidr`                  | string   |                | CIDR prefix length (e.g., `8`)                             |
| `nmstate-route-{N}-gateway`               | string   |                | Next hop address (e.g., `192.168.1.1`)                     |
| `nmstate-route-{N}-interface`             | string   |                | Outgoing interface (e.g., `bond0`)                         |
| `nmstate-route-{N}-metric`                | string   |                | Route metric (optional)                                    |

#### DNS Configuration

| Variable                                  | Type     | Default Value  | Notes                                                      |
| ----------------------------------------- | -------- | -------------- | ---------------------------------------------------------- |
| `nmstate-dns-server-{N}`                  | string   |                | DNS server IP (e.g., `8.8.8.8`)                            |
| `nmstate-dns-search-{N}`                  | string   |                | Search domain (e.g., `example.com`)                        |

#### Node Selector Configuration

| Variable                                  | Type     | Default Value  | Notes                                                      |
| ----------------------------------------- | -------- | -------------- | ---------------------------------------------------------- |
| `nmstate-nodeselector-{N}-prefix`         | string   |                | Node label prefix (e.g., `node-role.kubernetes.io`) - optional |
| `nmstate-nodeselector-{N}-name`           | string   |                | Node label name (e.g., `worker`)                           |
| `nmstate-nodeselector-{N}-value`          | string   |                | Node label value (empty string for exists)                 |

#### OVS Bridge Configuration (for UDN)

| Variable                                       | Type     | Default Value  | Notes                                                      |
| ---------------------------------------------- | -------- | -------------- | ---------------------------------------------------------- |
| `nmstate-ovs-bridge-{N}`                       | string   |                | OVS bridge name (e.g., `ovs-br1`)                          |
| `nmstate-ovs-bridge-{N}-port-{M}`              | string   |                | Port interface (e.g., `eth1`, `bond0`)                     |
| `nmstate-ovs-bridge-{N}-stp`                   | string   | `false`        | Spanning tree protocol                                     |
| `nmstate-ovs-bridge-{N}-allow-extra-patch-ports` | string | `true`         | Allow OVN patch ports                                      |
| `nmstate-ovs-bridge-{N}-mcast-snooping`        | string   |                | Multicast snooping (optional)                              |

#### OVN Bridge Mapping Configuration (for UDN Localnet)

| Variable                                  | Type     | Default Value  | Notes                                                      |
| ----------------------------------------- | -------- | -------------- | ---------------------------------------------------------- |
| `nmstate-ovn-mapping-{N}-localnet`        | string   |                | Localnet network name for UDN (e.g., `localnet1`)          |
| `nmstate-ovn-mapping-{N}-bridge`          | string   |                | OVS bridge to map (e.g., `ovs-br1`)                        |

#### Host-Specific Configuration

For per-node configurations (different IPs per host), use the `nmstate-host-{H}` prefix. Each host gets its own NNCP.

| Variable                                       | Type     | Default Value  | Notes                                                      |
| ---------------------------------------------- | -------- | -------------- | ---------------------------------------------------------- |
| `nmstate-host-{H}-hostname`                    | string   |                | Target node hostname (e.g., `worker-0.ocp.example.com`)    |
| `nmstate-host-{H}-bond-{N}`                    | string   |                | Bond interface name                                        |
| `nmstate-host-{H}-bond-{N}-ipv4-address-{M}`   | string   |                | Static IP for this host                                    |

All interface types support host-specific prefixes: `nmstate-host-{H}-bond-*`, `nmstate-host-{H}-vlan-*`, `nmstate-host-{H}-ovs-bridge-*`, etc.

#### Legacy File-Based Configuration (Deprecated)

| Variable                        | Type           | Default Value         | Notes                                                                             |
| ------------------------------- | -------------- | --------------------- | --------------------------------------------------------------------------------- |
| `nmstate-nncp-{name}`           | string         |                       | Filename of NMState config in `policies/nmstate/files/`                           |

#### NMState Example: Bond with DHCP

```yaml
nmstate: 'true'
nmstate-bond-1: 'bond0'
nmstate-bond-1-mode: '802.3ad'
nmstate-bond-1-port-1: 'eno1'
nmstate-bond-1-port-2: 'eno2'
nmstate-bond-1-ipv4: 'dhcp'
nmstate-bond-1-ipv6: 'disabled'
```

#### NMState Example: OVS Bridge with OVN Mapping for UDN

```yaml
nmstate: 'true'
# Create bond for physical NICs
nmstate-bond-1: 'bond1'
nmstate-bond-1-mode: '802.3ad'
nmstate-bond-1-port-1: 'eno3'
nmstate-bond-1-port-2: 'eno4'
nmstate-bond-1-ipv4: 'disabled'
# Create OVS bridge with bond as port
nmstate-ovs-bridge-1: 'ovs-br1'
nmstate-ovs-bridge-1-port-1: 'bond1'
# Map OVS bridge to OVN localnet for UDN
nmstate-ovn-mapping-1-localnet: 'localnet1'
nmstate-ovn-mapping-1-bridge: 'ovs-br1'
```

#### NMState Example: Per-Host Static IPs

```yaml
nmstate: 'true'
# Host 1 - worker-0 with 192.168.1.10
nmstate-host-1-hostname: 'worker-0.ocp.example.com'
nmstate-host-1-bond-1: 'bond0'
nmstate-host-1-bond-1-mode: '802.3ad'
nmstate-host-1-bond-1-port-1: 'eno1'
nmstate-host-1-bond-1-port-2: 'eno2'
nmstate-host-1-bond-1-ipv4: 'static'
nmstate-host-1-bond-1-ipv4-address-1: '192.168.1.10'
nmstate-host-1-bond-1-ipv4-address-1-cidr: '24'
# Host 2 - worker-1 with 192.168.1.11
nmstate-host-2-hostname: 'worker-1.ocp.example.com'
nmstate-host-2-bond-1: 'bond0'
nmstate-host-2-bond-1-mode: '802.3ad'
nmstate-host-2-bond-1-port-1: 'eno1'
nmstate-host-2-bond-1-port-2: 'eno2'
nmstate-host-2-bond-1-ipv4: 'static'
nmstate-host-2-bond-1-ipv4-address-1: '192.168.1.11'
nmstate-host-2-bond-1-ipv4-address-1-cidr: '24'
```

### Manual Remediations

Provides manual fixes and configurations that cannot be automated through operators, including managing allowed image registries for enhanced security.

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `manual-remediations`             | bool              |                           | If not set Manual Remediations will not be managed |
| `allowed-registries`              | <list<String>>    |                           | List of allowed container image registries. Controls which registries can be used for pulling images |
