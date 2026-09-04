# AutoShift values reference

## Values file architecture

AutoShift uses a **composable values file** pattern. Instead of a single monolithic values file, configuration is split into focused files under `autoshift/values/` that you combine in your ArgoCD Application:

```
autoshift/values/
  global.yaml                        # Shared config: git repo, branch, dryRun
  clustersets/
    _example.yaml                    # Reference: ALL clusterset options (hub and managed)
    hub.yaml                         # Hub clusterset — full enterprise profile
    hub-minimal.yaml                 # Hub clusterset — minimal (GitOps + ACM only)
    hub-baremetal-sno.yaml           # Hub clusterset — baremetal single-node
    hub-baremetal-compact.yaml       # Hub clusterset — baremetal compact (3 node)
    hubofhubs.yaml                   # Hub-of-hubs clusterset
    hub1.yaml                        # Spoke hub (managed by hub-of-hubs)
    hub2.yaml                        # Spoke hub (managed by hub-of-hubs)
    managed.yaml                     # Managed spoke clusterset — full enterprise
    sbx.yaml                         # Managed spoke clusterset — sandbox
  clusters/
    _example.yaml                    # Reference: ALL per-cluster override options
```

### How composition works

Helm **deep-merges** multiple `-f` value files in order. Each clusterset file defines a unique key (e.g., `hubClusterSets.hub`, `managedClusterSets.managed`), so they combine without conflict:

```yaml
# hub.yaml defines hubClusterSets.hub
# managed.yaml defines managedClusterSets.managed
# Result: both .hubClusterSets.hub and .managedClusterSets.managed exist
```

### Precedence

Labels follow this override precedence (highest to lowest):

1. **Per-cluster overrides** (`values/clusters/my-cluster.yaml`)
2. **Clusterset labels** (`values/clustersets/hub.yaml`)
3. **Helm chart defaults** (`values.yaml`)

### Creating custom profiles

Copy `_example.yaml` and make two edits (top-level key and clusterset name):

```bash
# Create a custom hub profile
cp autoshift/values/clustersets/_example.yaml autoshift/values/clustersets/my-hub.yaml
# Keep hubClusterSets, change "hub" to your clusterset name

# Create a custom managed profile
cp autoshift/values/clustersets/_example.yaml autoshift/values/clustersets/my-managed.yaml
# Change hubClusterSets → managedClusterSets, change "hub" to your clusterset name
# Remove labels marked "# hub only"

# Create per-cluster overrides
cp autoshift/values/clusters/_example.yaml autoshift/values/clusters/my-cluster.yaml
```

See `autoshift/README.md` for detailed chart documentation.

## Cluster labels

Values can be set on a per cluster and clusterset level to decide what features of AutoShift will be applied to each cluster. If a value is defined in helm values, a clusterset label and a cluster label precedence will be **cluster > clusterset > helm** values where helm values is the least.

Most policies are PolicyGenerator directories (no `values.yaml`); their configurable labels and default values are documented in `autoshift/values/clustersets/_example.yaml` — the canonical label catalog. The few policies that remain Helm charts (e.g. `policies/stable/openshift-gitops/`) also carry a `values.yaml` with chart defaults. Either way, values are set through clusterset labels in your `autoshift/values/clustersets/` files and further overridden by per-cluster labels in `autoshift/values/clusters/`.

## Operator version control

AutoShift v2 provides comprehensive version control for all managed operators through cluster labels. This feature you can pin operators to specific versions while maintaining automatic upgrade capabilities when desired.

### Version control behavior

When you specify a version for an operator:
- **Manual Install Plan Approval**: The operator subscription is automatically set to manual approval mode
- **Version Pinning**: Red Hat Advanced Cluster Management for Kubernetes will only approve install plans for the exact CSV (`ClusterServiceVersion`) specified
- **Controlled Upgrades**: Operators will not automatically upgrade beyond the specified version

When no version is specified:
- **Automatic Upgrades**: Operators use automatic install plan approval and follow normal upgrade paths
- **Channel-based Updates**: Operators receive updates based on their configured channel (stable, latest, etc.)

### Setting operator versions

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

### Available version labels

Every managed operator supports version control through its respective label:

| Operator                    | Version Label           | Example CSV                                        |
| --------------------------- | ----------------------- | -------------------------------------------------- |
| Red Hat Advanced Cluster Management | `acm-version`           | `advanced-cluster-management.v2.14.0`              |
| Advanced Cluster Security   | `acs-version`           | `rhacs-operator.v4.6.1`                            |
| OpenShift GitOps            | `gitops-version`        | `openshift-gitops-operator.v1.18.0`                |
| OpenShift Pipelines         | `pipelines-version`     | `openshift-pipelines-operator-rh.v1.18.1`          |
| OpenShift Data Foundation   | `odf-version`           | `odf-operator.v4.22.2-rhodf`                       |
| MetalLB                     | `metallb-version`       | `metallb-operator.v4.22.0-202608122145`            |
| Quay                        | `quay-version`          | `quay-operator.v3.18.0`                            |
| Developer Hub               | `dev-hub-version`       | `rhdh.v1.5.0`                                      |
| Developer Spaces            | `dev-spaces-version`    | `devspaces.v3.21.0`                                |
| Trusted Artifact Signer     | `tas-version`           | `rhtas-operator.v1.2.0`                            |
| Loki                        | `loki-version`          | `loki-operator.v6.3.0`                             |
| OpenShift Logging           | `logging-version`       | `cluster-logging.v6.3.0`                           |
| Cluster Observability       | `coo-version`           | `cluster-observability-operator.v0.4.0`            |
| Compliance Operator         | `compliance-version`    | `compliance-operator.v1.8.0`                       |
| LVM Storage                 | `lvm-version`           | `lvms-operator.v4.22.0`                            |
| Local Storage               | `local-storage-version` | `local-storage-operator.v4.22.0-202608122145`      |
| NMState                     | `nmstate-version`       | `kubernetes-nmstate-operator.4.22.0-202608130510`  |
| OpenShift Virtualization    | `virt-version`          | `kubevirt-hyperconverged-operator.v4.22.6`         |

### Finding available CSV versions

To find available CSV versions for operators, use the OpenShift CLI:

```bash
# List available CSV versions for an operator
oc get packagemanifests rhacs-operator -o jsonpath='{.status.channels[*].currentCSV}'

# Get all available versions in a channel
oc get packagemanifests openshift-pipelines-operator-rh -o yaml | grep currentCSV
```

> **Note**: Version control removes the need for install-plan-approval labels, as version specification automatically handles install plan management through Red Hat Advanced Cluster Management governance.

---

## Policy label reference

### Red Hat Advanced Cluster Management

> [!WARNING]
> Hub Clusters Only

| Variable                    | Type      | Default Value             | Notes |
|-----------------------------|-----------|---------------------------|-------|
| `self-managed`              | bool      | `true` or `false`         |       |
| `acm-enable-provisioning`   | bool      | `false`                   | Configures Red Hat Advanced Cluster Management to provision clusters |
| `acm-provisioning-storage-class` | string |                         | (optional) name of `StorageClass` to use if non default is desired |
| `acm-provisioning-database-size` | string | `10Gi`                  | `DatabaseStorage` defines the spec of the `PersistentVolumeClaim` to be
created for the database's filesystem. Minimum 10GiB is recommended. |
| `acm-provisioning-filesystem-storage-size` | string | `100Gi`       | `FileSystemStorage` defines the spec of the `PersistentVolumeClaim` to be
created for the assisted-service's filesystem (logs, etc). Minimum 100GiB recommended |
| `acm-provisioning-image-storage-size` | string | `100Gi`            | `ImageStorage` defines the spec of the `PersistentVolumeClaim` to be
created for each replica of the image service. Sized to hold the image catalog: 2GiB per `OSImage` entry. Red Hat recommends
`100Gi` when the list is not pinned. Pin `config.acm.provisioning.osImages` to run a smaller volume. |
| `acm-channel`               | string    | `release-2.14`            |       |
| `acm-version`               | string    | (optional)                | Specific CSV version for controlled upgrades |
| `acm-source`                | string    | `redhat-operators`        |       |
| `acm-source-namespace`      | string    | `openshift-marketplace`   |       |
| `acm-availability-config`   | string    | `Basic` or `High`         |       |
| `acm-observability`         | bool      | `true` or `false`         | this will enable observability utilizing a noobaa bucket for acm. OpenShift Data Foundation will have to be enabled as well |
| `acm-observability-custom-metrics` | bool | `false` | Collect metrics beyond the default allowlist. Requires `config.acm.observability.customMetrics` |
| `acm-search-storage`        | bool      | `true` or `false`         | Enable persistent storage for Red Hat Advanced Cluster Management Search (recommended for production) |
| `acm-search-storage-class`  | string    | `ocs-storagecluster-ceph-rbd` | Storage class for Search database |
| `acm-search-storage-size`   | string    | `100Gi`                   | Storage size for Search database. Sizing: Small (<50 clusters): 20Gi, Medium (50-200): 50Gi, Large (200-500): 100Gi, Very Large (500+): 200Gi+ |
| `acm-addon-tuning`          | bool      | `true` or `false`         | Enable add-on tuning for governance controllers. Recommended for 50+ managed clusters. See the sizing guidelines that follow. |
| `acm-addon-cpc-eval-concurrency` | string | `5`                  | config-policy-controller concurrent policy evaluations (default: 2) |
| `acm-addon-cpc-client-qps`  | string    | `75`                     | config-policy-controller K8s API client queries per second (QPS) (default: 30) |
| `acm-addon-cpc-client-burst` | string   | `100`                    | config-policy-controller K8s API client burst (default: 45) |
| `acm-addon-cpc-mem-request`  | string   | `256Mi`                  | config-policy-controller memory request |
| `acm-addon-cpc-cpu-request`  | string   | `150m`                   | config-policy-controller CPU request |
| `acm-addon-cpc-mem-limit`    | string   | `1Gi`                    | config-policy-controller memory limit |
| `acm-addon-gpf-eval-concurrency` | string | `5`                  | governance-policy-framework concurrent policy evaluations (default: 2) |
| `acm-addon-gpf-client-qps`  | string    | `75`                     | governance-policy-framework K8s API client QPS (default: 30) |
| `acm-addon-gpf-client-burst` | string   | `100`                    | governance-policy-framework K8s API client burst (default: 45) |
| `acm-addon-gpf-mem-request`  | string   | `128Mi`                  | governance-policy-framework memory request |
| `acm-addon-gpf-cpu-request`  | string   | `100m`                   | governance-policy-framework CPU request |
| `acm-addon-gpf-mem-limit`    | string   | `512Mi`                  | governance-policy-framework memory limit |

**Config block** (`config.acm.provisioning`):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `osImages` | list | | Red Hat Enterprise Linux CoreOS images the hub offers to the Assisted Installer. Each entry takes `openshiftVersion`, `version`, `cpuArchitecture` and `url`, plus an optional `rootFSUrl` |

Left unset, Red Hat Advanced Cluster Management offers its full default image catalog: every
supported Red Hat OpenShift Container Platform version for x86_64, arm64, s390x and ppc64le. That
catalog grows as releases are added, so Red Hat recommends `100Gi` of image storage to hold it,
which is what `acm-provisioning-image-storage-size` defaults to. Pin `osImages` to the
architectures and versions you provision to run a smaller volume. The image service downloads
every entry it is offered before it reports ready, so a volume smaller than the catalog it is
given leaves `assisted-image-service` unable to start.

This is the canonical path for every hub, connected or disconnected. A disconnected deployment
points the `url` of each entry at its mirror rather than at `mirror.openshift.com`.

`disconnected.osImages` is the earlier location for the same list. It is deprecated and still read,
but only when `config.acm.provisioning.osImages` is absent. Both paths are schema checked at
template time, so a misspelled field fails the build rather than being ignored.

**Red Hat Advanced Cluster Management Default compared to AutoShift Tuned Values:**

| Parameter | Red Hat Advanced Cluster Management Default | AutoShift Tuned |
|---|---|---|
| config-policy-controller eval-concurrency | 2 | 5 |
| config-policy-controller client-qps | 30 | 75 |
| config-policy-controller client-burst | 45 | 100 |
| config-policy-controller memory limit | 512Mi | 1Gi |
| governance-policy-framework eval-concurrency | 2 | 5 |
| governance-policy-framework client-qps | 30 | 75 |
| governance-policy-framework client-burst | 45 | 100 |
| governance-policy-framework memory limit | 256Mi | 512Mi |

**Add-on Tuning Sizing Guidelines:**
- **Small (< 50 clusters)**: Red Hat Advanced Cluster Management defaults are sufficient, tuning not needed
- **Medium (50-200 clusters)**: Enable tuning with AutoShift defaults
- **Large (200-500 clusters)**: Consider `acm-addon-cpc-eval-concurrency: '10'`, `acm-addon-cpc-mem-limit: '2Gi'`
- **Very Large (500+ clusters)**: Consider `acm-addon-cpc-eval-concurrency: '15'`, `acm-addon-cpc-client-qps: '150'`, `acm-addon-cpc-mem-limit: '4Gi'`

> **Note:** Increased concurrency/QPS increases CPU and memory on the controller pods, the Kubernetes API server, and the OpenShift API server. Concurrency/QPS/burst are set through `ManagedClusterAddOn` annotations per Red Hat Advanced Cluster Management 2.17 docs. Resource limits are set through `AddOnDeploymentConfig`.


For choosing metrics, cardinality cost, recording rules and storage sizing, see
[Observability metrics](observability-metrics.md).

**Config block** (`config.acm`):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `versions` | list | | Permitted CSVs for the operator. Overrides the `acm-version` label |
| `startingCSV` | string | | Starting CSV for the subscription |
| `observability.customMetrics.platform` | map | | Metrics collected from platform monitoring |
| `observability.customMetrics.userWorkload` | map | | Metrics collected from user workload monitoring |

Each of `platform` and `userWorkload` accepts `names` (metric names) and `matches` (label selectors,
written verbatim). The classic path also accepts `renames`, `recording_rules` and `collect_rules`,
which are Red Hat Advanced Cluster Management's own schema and pass through unchanged.

There are two delivery mechanisms, and which is live depends on the collector the hub runs:

| Collector | Mechanism |
|---|---|
| Classic MultiCluster Observability | `observability-metrics-custom-allowlist` config map |
| Multicluster observability add-on | `ScrapeConfig` federated by the `PrometheusAgent`, referenced from the `ClusterManagementAddOn` |

Both are rendered from the same config, so the same values work either way and the inactive one has
no effect. The add-on replaces the legacy collector, so where it is enabled the config map is
written and read by nothing. If a metric does not arrive, check which collector the hub runs first.

`recording_rules` work on both paths. On the classic path they go into the config map; with the
add-on they become a `PrometheusRule` on the managed cluster, and each `record` name is added to the
`ScrapeConfig` selectors automatically so the computed metric is federated without naming it twice.
A recording rule aggregates on the managed cluster before anything crosses the network, which is the
only effective control on cardinality for pod-scoped and virtual machine metrics.

In a multitiered rollup a metric is only forwarded if it is allowed on the hub whose collectors
gather it. Set this on every participating hub cluster set, not only the top one.

```yaml
config:
  acm:
    observability:
      customMetrics:
        platform:
          names:
            - node_vmstat_pgfault
```

### Managed AutoShift

> [!WARNING]
> Hub Clusters Only

Deploys a nested AutoShift onto each managed hub. The policy runs on that hub and creates an Argo CD
Application in the hub's own GitOps instance, so the hub goes on to configure its own spokes. Nothing
is created on the hub above it. See
[policies/stable/managed-autoshift](../policies/stable/managed-autoshift/README.md) and
[Hub of hubs](hub-of-hubs.md).

| Variable | Type | Default Value | Notes |
|----------|------|---------------|-------|
| `autoshift-enable-install` | bool | `false` | Opt this hub in. Requires `self-managed: 'false'`: `'true'` marks the hub the current AutoShift instance runs on, which is already deployed |

**Config block** (`config.managedAutoshift`), a list with one entry per deployment:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `appName` | string | the hub's cluster name | Argo CD Application name, and the `Release.Name` of the nested AutoShift. Max 11 characters, since it becomes `policies-<appName>` and that namespace is capped at 20. Never `autoshift`, which belongs to the top-level deployment |
| `repoUrl` | string | | Git repository holding the chart. Required in git mode, with `targetRevision` |
| `targetRevision` | string | `main` | Git branch or tag |
| `ociRepo` | string | | Registry namespace, for example `quay.io/autoshift`, **without** the `oci://` scheme. Setting it selects OCI mode instead of git. Policy charts are read from `<ociRepo>/policies` |
| `ociVersion` | string | | Chart version. Required whenever `ociRepo` is set |
| `gitopsNamespace` | string | `openshift-gitops` | Namespace on the managed hub for the Application and repository Secrets |
| `argoProject` | string | `default` | Argo CD project |
| `argoServer` | string | `https://kubernetes.default.svc` | Destination server, the managed hub itself |
| `valuesFiles` | list | `["values.<clusterName>.yaml"]` | Values files passed to the chart |
| `valuesRepoUrl` | string | | Keep values in their own repository, mounted as a second Argo CD source |
| `valuesTargetRevision` | string | `main` | Branch or tag of the values repository |
| `versionedClusterSets` | bool | `false` | Suffix clusterset names with the version tag |
| `useRepoSecret` | bool | `false` | Copy a repository Secret into the hub's Argo CD namespace, for private repositories |
| `repoSecretRef` | map | `{name: autoshift-repo-secret, namespace: <policy namespace>}` | Source Secret to copy |
| `valuesRepoSecretRef` | map | `{name: autoshift-values-repo-secret, namespace: <policy namespace>}` | Source Secret for the values repository |

An entry that names a reserved or over-long `appName`, or that sets `ociRepo` without `ociVersion`,
creates nothing rather than a broken Application.

### Cluster labels

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
| `metallb-bgp-1`                     | string            |                           | Name of config file for Border Gateway Protocol (BGP) Advertisement (copy this value if more than one, increasing number each time) |
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
| `gitops-namespace`              | string    | (`gitopsNamespace`)       | Per-cluster override of the ArgoCD namespace, e.g. in hub-of-hubs setups |
| `gitops-disable-default-argocd` | bool      | `true`                    | Controls `DISABLE_DEFAULT_ARGOCD_INSTANCE` on the operator Subscription |

#### Using a custom ArgoCD namespace

The ArgoCD namespace comes from `gitopsNamespace` in `autoshift/values/global.yaml` (default
`openshift-gitops`). Override it there or with `--set gitopsNamespace=<ns>`, and point the ArgoCD
Application that deploys AutoShift at the same namespace:

```yaml
# autoshift/values/global.yaml
gitopsNamespace: custom-gitops

# ArgoCD Application
spec:
  destination:
    namespace: custom-gitops    # match gitopsNamespace
    server: https://kubernetes.default.svc
```

With a custom namespace the operator's default ArgoCD instance in `openshift-gitops` is disabled.
To keep it running alongside yours:

```yaml
hubClusterSets:
  hub:
    labels:
      gitops-disable-default-argocd: 'false'
```


### Master nodes

Single Node OpenShift clusters as well as Compact Clusters have to rely on their master nodes to handle workloads. You may have to increase the number of pods per node in these resource constrained environments.

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `master-nodes`                    | bool              | `false`                   |       |
| `master-max-pods`                 | int               | `250`                     | The number of maximum pods per node. Up to 2500 supported dependent on hardware |

### Workload partitioning

CPU isolation through PerformanceProfile. Dedicates CPUs to the control plane (reserved) and makes the rest available for user workloads (isolated). See [workload-partitioning.md](workload-partitioning.md) for sizing guidelines, Non-Uniform Memory Access topology, and examples.

**Label:**

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `workload-partitioning`           | bool              | `false`                   | Enable the workload partitioning policy |

**Config block** (`config.workloadPartitioning`):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `reservedCpus` | string | **(required)** | CPU set for control plane / operating system / platform (e.g., `0-11,60-71`) |
| `isolatedCpus` | string | **(required)** | CPU set for user workloads (e.g., `12-59,72-119`) |
| `nodeSelector` | map | `node-role.kubernetes.io/master: ''` | Which nodes the `PerformanceProfile` targets |
| `numaTopology` | string | | `single-numa-node`, `best-effort`, or `restricted` |
| `realTimeKernel` | bool | `false` | Enable the real-time kernel |
| `globallyDisableIrqLoadBalancing` | bool | `false` | Disable interrupt request load balancing on isolated CPUs |
| `hugepages.defaultSize` | string | | Default huge page size (`1G`, `2M`) |
| `hugepages.pages` | list | | List of `{size, count, node}` allocations |

### Machine health checks

Automated node health monitoring and remediation.

| Variable                          | Type   | Default | Notes |
|-----------------------------------|--------|---------|-------|
| `machine-health-checks`           | bool   |         | Enable the `MachineHealthCheck` policy |
| `machine-health-checks-worker`    | bool   |         | Enable `MachineHealthCheck` (MHC) for all worker `MachineSets` (timeout=300s, `maxUnhealthy`=40%) |
| `machine-health-checks-infra`     | bool   |         | Enable MHC for all infra `MachineSets` (timeout=300s, `maxUnhealthy`=40%) |
| `machine-health-checks-storage`   | bool   |         | Enable MHC for storage `MachineSets` (timeout=600s, `maxUnhealthy`=1) |

**Notes:**
- Storage nodes are identified by `cluster.ocs.openshift.io/openshift-storage` label (same as OpenShift Data Foundation)
- Storage uses longer timeouts and `maxUnhealthy`=1 to allow Ceph recovery
- Never create `MachineHealthChecks` for control plane nodes

### Infra nodes

| Variable                            | Type              | Default Value             | Notes |
|-------------------------------------|-------------------|---------------------------|-------|
| `infra-nodes`                       | int               |                           | Number of infra nodes. If not set infra nodes are not managed, if 0 infra nodes will be deleted |
| `infra-nodes-provider`              | string            |                           | Provider type - 'aws', 'vmware', or 'test' |
| `infra-nodes-instance-type`         | string            |                           | AWS instance type |
| `infra-nodes-numcpu`                | int               |                           | Number of cpu per infra node |
| `infra-nodes-memory-mib`            | int               |                           | Memory mib per infra node |
| `infra-nodes-numcores-per-socket`   | int               |                           | Number of CPU Cores per socket |
| `infra-nodes-zone-[number]`         | string            |                           | Availability zone (e.g., infra-nodes-zone-1: 'us-east-2a') |



### Worker nodes

| Variable                            | Type              | Default Value             | Notes |
|-------------------------------------|-------------------|---------------------------|-------|
| `worker-nodes`                      | int               |                           | Number of worker nodes min if autoscale. If not set worker nodes are not managed, if blank worker nodes will be deleted |
| `worker-nodes-numcpu`               | int               |                           | Number of cpu per worker node |
| `worker-nodes-memory-mib`           | int               |                           | Memory mib per worker node |
| `worker-nodes-numcores-per-socket`  | int               |                           | Number of CPU Cores per socket |
| `worker-nodes-zones`                | <list<String>>    |                           | List of availability zones |

### Storage nodes

| Variable                            | Type           | Default Value | Notes |
| ----------------------------------- | -------------- | ------------- | ----- |
| `storage-nodes`                     | int            |               | Number of storage nodes min if autoscale. If not set storage nodes are not managed, if blank storage nodes will be deleted. Local Storage Operator will be installed if Storage Nodes are enabled |
| `storage-nodes-numcpu`              | int            |               | Number of cpu per storage node  |
| `storage-nodes-memory-mib`          | int            |               | Memory mib per storage node |
| `storage-nodes-numcores-per-socket` | int            |               | Number of CPU Cores per socket |
| `storage-nodes-zone-[number]`       | string         |               | Availability zone (e.g., storage-nodes-zone-1: 'us-east-2a') |
| `storage-nodes-instance-type`       | string         |               | Instance type for cloud provider |
| `storage-nodes-provider`            | string         |               | Provider type; valid choices: aws, vmware, baremetal |
| `storage-nodes-node-[iterator]`     | <list<String>> |               | List of node names to apply storage label to. Used for baremetal where `MachineSets` are not used. |

### Red Hat Advanced Cluster Security

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `acs`                             | bool              |                           | If not set Advanced Cluster Security will not be managed |
| `acs-egress-connectivity`         | string            | `Online`                  | Options are `Online` or `Offline`, use `Offline` if disconnected |
| `acs-channel`                     | string            | `stable`                  |       |
| `acs-version`                     | string            | (optional)                | Specific CSV version for controlled upgrades |
| `acs-source`                      | string            | `redhat-operators`        |       |
| `acs-source-namespace`            | string            | `openshift-marketplace`   |       |
| `acs-scanner-v4`                  | string            | `Enabled`                 | Scanner V4 component state (`Enabled` or `Disabled`) |
| `acs-monitoring`                  | bool              | `true`                    | Enable OpenShift monitoring integration for Central and `SecuredCluster` |
| `acs-vm-scanning`                 | bool              |                           | Enable VM scanning (Developer Preview, opt-in) |
| `acs-admission-control`           | bool              |                           | Enable admission control enforcement on `SecuredCluster` (opt-in, can block deployments) |
| `acs-network-policies`            | string            |                           | Network policy generation (`Enabled` or `Disabled`), only set when explicit control needed |
| `acs-auth-provider`               | string            | `openshift`               | Auth provider type (`openshift`). Hub only. Configures declarative RBAC |
| `acs-auth-min-role`               | string            | `None`                    | Minimum role for authenticated users. Hub only |
| `acs-auth-admin-group`            | string            | `cluster-admins`          | Group mapped to Admin role. Hub only |
| `acs-default-policies`            | bool              |                           | Deploy baseline `SecurityPolicy` CRDs (no privilege escalation, no root, no shell). Hub only |

### Developer spaces

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `dev-spaces`                          | bool              |                           | If not set Developer Spaces will not be managed |
| `dev-spaces-channel`                  | string            | `stable`                  |       |
| `dev-spaces-version`                  | string            | (optional)                | Specific CSV version for controlled upgrades |
| `dev-spaces-source`                   | string            | `redhat-operators`        |       |
| `dev-spaces-source-namespace`         | string            | `openshift-marketplace`   |       |

### Developer hub

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `dev-hub`                         | bool              |                           | If not set Developer Hub will not be managed |
| `dev-hub-channel`                 | string            | `fast`                    |       |
| `dev-hub-version`                 | string            | (optional)                | Specific CSV version for controlled upgrades |
| `dev-hub-source`                  | string            | `redhat-operators`        |       |
| `dev-hub-source-namespace`        | string            | `openshift-marketplace`   |       |

### OpenShift pipelines

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `pipelines`                       | bool              |                           | If not set OpenShift Pipelines will not be managed |
| `pipelines-channel`               | string            | `latest`                  |       |
| `pipelines-version`               | string            | (optional)                | Specific CSV version for controlled upgrades |
| `pipelines-source`                | string            | `redhat-operators`        |       |
| `pipelines-source-namespace`      | string            | `openshift-marketplace`   |       |

### Trusted artifact signer

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `tas`                             | bool              |                           | If not set Trusted Artifact Signer will not be managed |
| `tas-channel`                     | string            | `stable`                  |       |
| `tas-version`                     | string            | (optional)                | Specific CSV version for controlled upgrades |
| `tas-source`                      | string            | `redhat-operators`        |       |
| `tas-source-namespace`            | string            | `openshift-marketplace`   |       |

### Quay

Red Hat Quay is a container registry. The policy installs the operator, deploys a `QuayRegistry`,
and optionally provisions its databases through CloudNativePG.

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `quay`                            | bool              |                           | If not set Quay will not be managed |
| `quay-channel`                    | string            | `stable-3.18`             |       |
| `quay-version`                    | string            | (optional)                | Specific CSV version for controlled upgrades |
| `quay-source`                     | string            | `redhat-operators`        |       |
| `quay-source-namespace`           | string            | `openshift-marketplace`   |       |
| `quay-db-mode`                    | string            | `bundled`                 | `bundled`, `managed`, or `external`. See the following table |
| `quay-db-instances`               | int               | `2`                       | CloudNativePG replicas for the Quay database |
| `quay-clair-db-instances`         | int               | `2`                       | CloudNativePG replicas for the Clair database |
| `quay-db-backups`                 | bool              | `false`                   | Scheduled database backups. Requires `quay-db-mode: managed` and `odf` |

**Database modes** (`quay-db-mode`):

| Mode | Behavior |
|------|----------|
| `bundled` | The Quay Operator runs its own PostgreSQL deployments. The shipped default, and appropriate for proof of concept clusters |
| `managed` | AutoShift provisions two CloudNativePG clusters, `quay-db` and `clair-db`. Requires `cloudnative-pg: 'true'` on the same cluster |
| `external` | You supply `DB_URI` and the Clair connection strings yourself. See `configSecretRef` in the following section |

> [!NOTE]
> Red Hat Quay and Clair must not share a database, so `managed` mode creates two separate
> CloudNativePG clusters. PgBouncer is not supported with Red Hat Quay or Clair, so neither
> database gets a connection pooler.

**Config block** (`config.quay`):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `versions` | list | | Permitted operator CSV versions |
| `startingCSV` | string | | Initial install pin. An empty value lets Operator Lifecycle Manager choose |
| `superUsers` | list | `[quayadmin]` | Registry superusers. With OpenID Connect these are the OpenID Connect user names |
| `components.<kind>` | bool | `true` | Per component `managed` flag. Omit a component to leave it managed by the operator |
| `overrides.<kind>` | map | | Passed through to `spec.components[].overrides`. Accepts `affinity`, `annotations`, `env`, `labels`, `replicas`, `resources`, `securityContext`, `storageClassName`, `tls`, and `volumeSize`. See the warning that follows about `replicas` |
| `bootstrap.userInitialize` | bool | `false` | `FEATURE_USER_INITIALIZE`. Enables the unauthenticated first user endpoint |
| `bootstrap.xhrOnly` | bool | `true` | `BROWSER_API_CALLS_XHR_ONLY`. Restricts the registry API to browser calls |
| `bootstrap.userCreation` | bool | `false` | `FEATURE_USER_CREATION`. When false, only superusers create users |
| `bootstrap.programmatic` | bool | `false` | `FEATURE_PROGRAMMATIC_BOOTSTRAP`. Technology Preview in Red Hat Quay 3.18 |
| `tls.secretName` | string | | Name of a `kubernetes.io/tls` Secret for the registry certificate |
| `tls.certificate.issuerRef` | map | | Optional. Creates a cert-manager `Certificate` from this issuer |
| `tls.certificate.dnsNames` | list | | Optional. Defaults to the registry route host |
| `config` | map | | Extra `config.yaml` keys, merged after the preceding fields. Non-sensitive values only |
| `configSecretRef` | map | | `{name, namespace, key}` of a Secret holding sensitive `config.yaml` keys |
| `dbBackupSchedule` | string | `0 4 * * *` | Cron schedule for database backups |
| `dbBackupRetention` | string | `30d` | Backup retention period |

> [!WARNING]
> Setting `overrides.<kind>.replicas` while the horizontal pod autoscaler is managed is rejected by
> Red Hat Quay with `cannot override replicas with managed HPA`. This blocks the whole rollout: the
> `QuayRegistry` reports `RolloutBlocked` and no registry pods are created, even though the custom
> resource itself is valid. To pin replica counts, also set
> `components.horizontalpodautoscaler: false` in the same `config.quay` block.

> [!IMPORTANT]
> Values files are stored in Git, so credentials must never appear in `config.quay.config`. Put
> object storage keys, `DB_URI` for `external` mode, and OpenID Connect client secrets in a Secret
> that you create on the cluster, and reference it with `configSecretRef`. That Secret is merged
> last, so it overrides everything else.

The default configuration is secure: the unauthenticated first user endpoint is disabled and the
registry API is restricted to browser calls. A cluster with no OpenID Connect provider therefore has
no interactive administrator until you either configure OpenID Connect and list administrators in `superUsers`, or set
`bootstrap.userInitialize` and `bootstrap.xhrOnly` to bootstrap a local account by hand.

### OpenShift virtualization

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `virt`                            | bool              |                           | If not set OpenShift Virtualization will not be managed |
| `virt-channel`                    | string            | `stable`                  | KubeVirt-based virtualization platform for running VMs on OpenShift |
| `virt-version`                    | string            | (optional)                | Specific CSV version for controlled upgrades |
| `virt-source`                     | string            | `redhat-operators`        |       |
| `virt-source-namespace`           | string            | `openshift-marketplace`   |       |

### Developer OpenShift gitops

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `gitops-dev`                          | bool              |                           | If not set Developer OpenShift GitOps instances will not be managed |
| `gitops-dev-team-{INSERT_TEAM_NAME}`  | string        |                           | Team that can deploy onto cluster from dev team gitops. Must match a team in the `gitops-dev` helm chart values file |

### Loki

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `loki`                            | bool              |                           | If not set Loki will not be managed. Dependent on OpenShift Data Foundation Multi Object Gateway |
| `loki-channel`                    | string            | `stable-6.2`              |       |
| `loki-version`                    | string            | (optional)                | Specific CSV version for controlled upgrades |
| `loki-source`                     | string            | `redhat-operators`        |       |
| `loki-source-namespace`           | string            | `openshift-marketplace`   |       |
| `loki-size`                       | string            | `1x.extra-small`          |       |
| `loki-storageclass`               | string            | `gp3-csi`                 |       |
| `loki-lokistack-name`             | string            | `logging-lokistack`       |       |

### OpenShift logging

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `logging`                         | bool              |                           | If not set OpenShift Logging will not be managed, Dependent on Loki and Cluster Observability Operator |
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

### Compliance Operator: apply a Security Technical Implementation Guide

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `compliance`                          | bool              |                           | If not set Compliance Operator will not be managed. Helm chart config map must be set with profiles and remediations |
| `compliance-auto-remediate`           | bool              | `true`                    |       |
| `compliance-storage-class`            | string            |                           | `StorageClass` for compliance scan raw results. Use when default `StorageClass` is not available on master nodes (e.g., Ceph RBD) |
| `compliance-subscription-name`        | string            | `compliance-operator`     |       |
| `compliance-version`                  | string            | (optional)                | Specific CSV version for controlled upgrades |
| `compliance-source`                   | string            | `redhat-operators`        |       |
| `compliance-source-namespace`         | string            | `openshift-marketplace`   |       |
| `compliance-channel`                  | string            | `stable`                  |       |

### LVM Storage Operator

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `lvm`                                 | bool              | `false`                   | If not set the LVM Operator will not be managed |
| `lvm-channel`                         | string            | `stable-4.22`             | Operator channel |
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

### Ansible automation platform

| Variable                         | Type      | Default Value              | Notes |
|----------------------------------|-----------|----------------------------|-------|
| `aap`                            | bool      | `true` or `false`          |  |
| `aap-channel`                    | string    | `stable-2.5`             |  |
| `aap-install-plan-approval`      | string    | `Automatic`                |  |
| `aap-source`                     | string    | `redhat-operators`         |  |
| `aap-hub-disabled`               | bool      | `true` or `false`          | 'false' will include Hub content storage in your deployment, 'true' will omit.       |
| `aap-file-storage`               | bool      | `true` or `false`          | 'false' will use file storage for Hub content storage in your deployment, 'true' will omit. |
| `aap-file_storage_storage_class` | string    | `ocs-storagecluster-cephfs`| you will set the storage class for your file storage, defaults to OpenShift Data Foundation. you must have a `ReadWriteMany` capable storage class if using anything else. |
| `aap-file_storage_size`          | bool      | `10G`                      | set the pvc claim size for your file storage.  |
| `aap-s3-storage`                 | bool      | `true` or `false`          | 'false' will use OpenShift Data Foundation `NooBa` for Hub content storage in your deployment, 'true' will omit. |
| `aap-eda-disabled`               | bool      | `true` or `false`          | 'false' will include Event-Driven Ansible in your deployment, 'true' will omit. |
| `aap-lightspeed-disabled`        | bool      | `true` or `false`          | 'false' will include Red Hat Ansible Lightspeed in your deployment, 'true' will omit. |
| `aap-version`                    | bool      | `aap-operator.v2.6.0-0.1762261205`          | Specific CSV version for controlled upgrades  |
| `aap-custom-cabundle`            | bool      | `true` or `false`          | 'true' will inject cluster CA Bundle into AAP CRD |
| `aap-cabundle-name`              | string    | `user-ca-bundle`           |  name of the secret to be created for CA Bundle injection |

### OpenShift data foundation

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `odf`                             | bool              |                           | If not set OpenShift Data Foundation will not be managed. if Storage Nodes are enable will deploy OpenShift Data Foundation on local storage/ storage nodes |
| `odf-multi-cloud-gateway`         | string            |                           | values `standalone` or `standard`. Install OpenShift Data Foundation with only noobaa object gateway or full odf |
| `odf-noobaa-pvpool`                | bool              |                           | if not set noobaa will be deployed with default settings. Recommended do not set for cloud providers. Use pv pool for storage |
| `odf-noobaa-store-size`            | string            |                           | example `500Gi`. if pvpool set. Size of noobaa backing store |
| `odf-noobaa-store-num-volumes`     | string            |                           | example `1`. if pvpool set. number of volumes |
| `odf-ocs-storage-class-name`      | string            |                           | if not using local-storage, storage class to use for ocs |
| `odf-ocs-storage-size`            | string            |                           | storage size per nvme |
| `odf-ocs-storage-count`           | string            |                           | number of replica sets of nvme drives, note total amount will count * replicas |
| `odf-ocs-storage-replicas`        | string            |                           | replicas, `3` is recommended; if using `flexibleScaling` use `1` |
| `odf-ocs-flexible-scaling`        | bool              | `false`*                  | Sets failure domain to host and evenly spreads Object Storage Daemons over hosts. Defaults to true on baremetal with several storage nodes that is not a multiple of 3 |
| `odf-resource-profile`            | string            | `balanced`                | `lean`: suitable for clusters with limited resources, `balanced`: suitable for most use cases, `performance`: suitable for clusters with high amount of resources |
| `odf-default-storageclass`        | string            | `ocs-storagecluster-ceph-rbd` | Sets specified storage class as default and all others as non-default |
| `odf-csi-all-nodes`              | bool              | `false`                   | `true` runs CSI plugins on all nodes (masters, infra, storage) allowing PVCs on non-storage nodes. `false` restricts CSI plugins to storage-labeled nodes only |
| `odf-channel`                     | string            | `stable-4.22`             |       |
| `odf-version`                     | string            | (optional)                | Specific CSV version for controlled upgrades |
| `odf-source`                      | string            | `redhat-operators`        |       |
| `odf-source-namespace`            | string            | `openshift-marketplace`   |       |
| `odf-default-storageclass`        | string            | `ocs-storagecluster-ceph-rbd` | Sets specified storage class as default and all others as non-default |

### OpenShift internal registry
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

The Kubernetes NMState Operator declaratively configures Red Hat CoreOS network settings including bonds, virtual local area networks, static routes, and DNS. Network configuration is defined through structured YAML under `config.networking` in clusterset or cluster values files.

See `policies/stable/nmstate/README.md` for detailed documentation and examples.

#### Operator labels

| Label                           | Type           | Default Value         | Notes                                                                             |
| ------------------------------- | -------------- | --------------------- | --------------------------------------------------------------------------------- |
| `nmstate`                       | bool           | `false`               | Enable/disable the NMState Operator                                               |
| `nmstate-channel`               | string         | `stable`              | Operator channel                                                                  |
| `nmstate-version`               | string         | (optional)            | Specific CSV version for controlled upgrades                                      |
| `nmstate-source`                | string         | `redhat-operators`    | Operator catalog source                                                           |
| `nmstate-source-namespace`      | string         | `openshift-marketplace` | Catalog namespace                                                               |

#### NNCP configuration

NNCPs are generated from `config.networking` in values files. Each interface gets its own NNCP for fault isolation.

| Config Path | Generated NNCP | Notes |
|---|---|---|
| `networking.interfaces.{id}` (bond) | `nmstate-bond-{id}` | One per bond |
| `networking.interfaces.{id}` (VLAN) | `nmstate-vlan-{id}` | One per VLAN |
| `networking.interfaces.{id}` (ethernet) | `nmstate-ethernet-{id}` | One per ethernet |
| `networking.ovsBridges.{id}` | `nmstate-ovs-bridge-{id}` | One per OVS bridge |
| routes + DNS + `ovnMappings` | `nmstate-network-config` | Combined |
| `hosts.{name}.networking` | `nmstate-host-{name}` | Per-host `nodeSelector` |

#### Interface properties

| Property | Type | Required | Notes |
|----------|------|----------|-------|
| `type` | string | Yes | `bond`, `vlan`, or `ethernet` |
| `name` | string | Yes | nmstate interface name (e.g., `bond0`) |
| `state` | string | No | `up` (default), `down` |
| `mtu` | int | No | maximum transmission unit size |
| `mac` | string | No | MAC address — when set, adds `identifier: mac-address` to NNCP (nmstate matches by MAC instead of name) |
| `ipv4` | string | No | `disabled` (default), `dhcp`, `static` |
| `ipv6` | string | No | `disabled` (default), `dhcp`, `autoconf`, `static` |
| `mode` | string | Bond only | Bond mode (e.g., `802.3ad`, `active-backup`) |
| `ports` | list | Bond only | Member interfaces |
| `miimon` | int | No | Media Independent Interface monitoring interval (bond only) |
| `id` | int | VLAN only | VLAN ID |
| `base` | string | VLAN only | Parent interface name |

#### NMState example: bond + VLAN with static IPs

```yaml
config:
  networking:
    interfaces:
      mgmt:
        type: bond
        name: bond0
        mode: 802.3ad
        ports: [eno1, eno2]
        ipv4: disabled
        ipv6: disabled
      mgmt-vlan:
        type: vlan
        name: bond0.100
        id: 100
        base: bond0
        ipv4: static
        ipv6: disabled
    routes:
      default:
        destination: 0.0.0.0/0
        gateway: 10.0.0.1
        interface: bond0.100
    dns:
      servers: [10.0.0.53]
  hosts:
    master-0:
      networking:
        interfaces:
          mgmt-vlan:
            ipv4:
              addresses:
                - ip: 10.0.0.10
                  prefixLength: 25
```

### Manual remediations

Provides manual fixes and configurations that cannot be automated through operators, including managing allowed image registries for enhanced security.

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `manual-remediations`             | bool              |                           | If not set Manual Remediations will not be managed |
| `allowed-registries`              | <list<String>>    |                           | List of allowed container image registries. Controls which registries can be used for pulling images |
