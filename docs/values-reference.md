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
    hub-disconnected.yaml            # Hub clusterset — disconnected, mirror and update service at the hub site
    hubofhubs.yaml                   # Hub-of-hubs clusterset
    hub1.yaml                        # Spoke hub (managed by hub-of-hubs)
    hub2.yaml                        # Spoke hub (managed by hub-of-hubs)
    managed.yaml                     # Managed spoke clusterset — full enterprise
    managed-disconnected.yaml        # Managed spoke clusterset — disconnected, own site mirror
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
      acs-version: 'rhacs-operator.v4.11.2'

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
| Advanced Cluster Security   | `acs-version`           | `rhacs-operator.v4.11.2`                           |
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
| Multicluster Global Hub     | `global-hub-version`    | `multicluster-global-hub-operator-rh.v1.8.0`       |

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
| `acm-observability`         | bool      | `true` or `false`         | Enable Red Hat Advanced Cluster Management observability on a hub. Where metrics are stored is `acm-observability-storage` |
| `acm-observability-storage` | string    | `noobaa`                  | `noobaa` creates a NooBaa bucket and requires Red Hat OpenShift Data Foundation. `external-s3` reads `config.acm.observability.thanosStorage` and does not require OpenShift Data Foundation |
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
| `acm-addon-gpf-mem-request`  | string   | `256Mi`                  | governance-policy-framework memory request |
| `acm-addon-gpf-cpu-request`  | string   | `100m`                   | governance-policy-framework CPU request |
| `acm-addon-gpf-mem-limit`    | string   | `1Gi`                    | governance-policy-framework memory limit. The add-on delivers its own tuning, so an OOMKilled one has to be patched by hand |

For `external-s3`, set `config.acm.observability.thanosStorage` on each hub clusterset. Create a Secret on that hub, in `source.namespace` and named `source.secretName`, with data keys `access-key` and `secret-key`. The policy looks those keys up. They are not config.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `bucket` | string | | S3 bucket name |
| `endpoint` | string | | S3 endpoint, including the port (`s3.example.com:443`) |
| `insecure` | bool | `false` | Skip TLS verification of the endpoint |
| `useClusterCA` | bool | `false` | Verify the endpoint with the hub cluster CA (`/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`) |
| `source.namespace` | string | | Namespace of the credentials Secret on this hub |
| `source.secretName` | string | | Name of the credentials Secret |

`bucket`, `endpoint`, `source.namespace`, and `source.secretName` are required when `acm-observability-storage` is `external-s3`. A missing one fails the policy instead of writing an empty Secret.

The global-observability chart reads `scrapeInterval` (default `300s`), `logLevel` (default `warn`), and `additionalRemoteWrites` from `config.globalObservability`. `policy-acm-observability` applies MCOA `capabilities` (each defaults to `"true"`) from `config.acm.observability.capabilities`.

**Config block** (`config.acm.provisioning`):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `osImages` | list | | Red Hat Enterprise Linux CoreOS images the hub offers to the Assisted Installer. Each entry takes `openshiftVersion`, `version`, `cpuArchitecture` and `url`, plus an optional `rootFSUrl` |
| `provisioningIP` | string | | Sets `provisioningIP` on the metal3 `Provisioning` CR. Ironic only puts an IP SAN in its TLS certificate when this is set, so BMCs that require HTTPS with a valid certificate need it to mount virtual-media ISOs from `https://<ip>:6183`. With `provisioningNetwork: Disabled`, metal3 does not assign this address to any interface, so it must be an IP the node already has. On single-node hubs, use the node IP. A value that is not an IPv4 or IPv6 address fails the policy. Removing the key leaves the last value on the CR; clear it there by hand |

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
Application in the hub's own GitOps instance, so the hub goes on to configure its own spokes. A hub
whose infra agent runs managed (`gitops-infra: 'agent-managed'`) gets the same Application from the
hub above instead, written into its `<cluster>-infra` namespace there and delivered by the infra
principal. See
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
| `gitopsNamespace` | string | the managed hub's `gitops-namespace` label, else `openshift-gitops` | Namespace on the managed hub for the Application and repository Secrets. It must be where that hub's infra Argo CD runs |
| `argoProject` | string | `default` | Argo CD project |
| `argoServer` | string | `https://kubernetes.default.svc` | Destination server, the managed hub itself |
| `valuesFiles` | list | `["values.<clusterName>.yaml"]` | Values files passed to the chart |
| `valuesRepoUrl` | string | | Keep values in their own repository, mounted as a second Argo CD source |
| `valuesTargetRevision` | string | `main` | Branch or tag of the values repository |
| `versionedClusterSets` | bool | `false` | Suffix clusterset names with the version tag |
| `useRepoSecret` | bool | `false` | Copy a repository Secret into the hub's Argo CD namespace, for private repositories |
| `repoSecretRef` | map | name `autoshift-repo-secret`, namespace `<policy namespace>` | Source Secret to copy |
| `valuesRepoSecretRef` | map | name `autoshift-values-repo-secret`, namespace `<policy namespace>` | Source Secret for the values repository |

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
| `gitops-infra`                  | string    | `hub` on a hub, else `push` | The shape of infra Argo CD on this cluster: `hub`, `push`, `standalone`, `agent`, `agent-autonomous`, `agent-managed`, or `uninstall` to tear down its agent. See [Argo CD agent](gitops-agent.md) |
| `gitops-agent`                  | bool      | `false`                   | Hub label. Run AutoShift's infra Argo CD Agent principal on this hub. Settings are in `config.gitops.infra.agent`. Independent of this hub's own `gitops-infra` mode. `uninstall` tears the principal down |
| `gitops-agent-teams`            | bool      | `false`                   | Hub label. Run a principal here for each team with `config.gitops.teams.<team>.agent.enabled` in this hub's config, whether or not this hub carries that team's `gitops-dev-team-<team>` label |
| `gitops-agent-enrolled`         | bool      | `false`                   | Cluster label. Place the spoke agent policy here, for clusters whose `gitops-dev-team-<team>` label carries an agent mode |

**Config block** (`config.gitops`):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `namespace` | string | (`gitopsNamespace`) | Argo CD namespace for the whole deployment. Overrides `gitopsNamespace` |
| `defaultInstance` | bool | `false` | Keep the operator default Argo CD instance in `openshift-gitops`. Drives `gitops-disable-default-argocd` |
| `policyGenerator` | bool | (deployment flag) | Install the PolicyGenerator plugin sidecar in the infra repo server. Git and source hubs must set `true`. Read only from the self-managed hub cluster set |
| `teams` | map | | Developer Argo CD instances, one entry per team. See [Developer OpenShift gitops](#developer-openshift-gitops) |
| `infra` | map | | Tuning for the infra Argo CD instance, plus `infra.agent`. See the tables below |

**Config block** (`config.gitops.infra.agent`):

Settings for AutoShift's own infra agent, which the `gitops-agent` label switches on. It takes the
same keys as `config.gitops.teams.<team>.agent` except `enabled`, which the label replaces. Two
defaults differ, because infra Applications configure the cluster itself: `destinationNamespaces`
defaults to `*` and `clusterResources` to every group and kind. The principal namespace is
`config.gitops.infra.agentNamespace`, default `openshift-gitops-infra-agent`.

**Config block** (`config.gitops.infra`):

Every field is optional. A field left unset falls back to the chart default in
`policies/stable/openshift-gitops/values.yaml`, which is the same file the bootstrap chart reads, so
bootstrap and Day 2 agree. Set a field here to override it for one cluster or cluster set.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | `infra-gitops` | Name of the infra Argo CD instance. Every policy that targets the instance by name reads it: the instance itself, its cluster-admin binding, the console link, and the infra agent that attaches to it on a hub. On the self-managed hub the bootstrap chart creates the instance as its `gitops.argoName`, so keep the two equal there. The namespace is the cluster's `gitops-namespace` label |
| `rbac_policies` | list | admin for `system:cluster-admins`, `cluster-admins` and `openshift-systems` | The complete Argo CD RBAC policy. Setting it replaces the default list rather than appending. A `g` line assigns a subject to a role and accepts a user or a group, but OpenShift login identifies users by an opaque Dex `sub` claim rather than a username, so name OpenShift Groups here. `defaultPolicy` is empty, so an empty list leaves no access apart from the built-in admin account |
| `disableAdmin` | bool | `false` | Disable the Argo CD built-in admin account |
| `server.limits` | map | cpu `500m`, memory `256Mi` | Argo CD server resource limits |
| `server.requests` | map | cpu `125m`, memory `128Mi` | Argo CD server resource requests |
| `server.autoscale.enabled` | bool | `true` | Horizontal pod autoscaling for the server |
| `server.autoscale.maxReplicas` | int | `5` | Upper bound for server autoscaling |
| `server.autoscale.targetCPUUtilizationPercentage` | int | `50` | CPU target that triggers server autoscaling |
| `repo.replicas` | int | `1` | Repo server replicas. One is enough once `useManifestGeneratePaths` scopes each render to a single policy directory. Raise for availability or a much larger policy set |
| `repo.limits` | map | cpu `2000m`, memory `2048Mi` | Repo server resource limits |
| `repo.requests` | map | cpu `500m`, memory `512Mi` | Repo server resource requests |
| `repo.cluster_ca_bundle` | bool | `false` | Inject the cluster trusted certificate authority bundle into the repo server. The `gitops-cluster-ca-bundle` label overrides this |
| `controller.statusProcessors` | int | `20` | Concurrent status reconciliations. AutoShift creates an Application per policy, so even a small hub carries dozens. This is the main lever on how fast the fleet converges, and also on peak memory, because each in-flight comparison holds an Application's rendered and live state at once. Raise `controller.limits.memory` with it |
| `controller.operationProcessors` | int | `10` | Concurrent sync operations |
| `controller.goMemLimitPercent` | int | `85` | Soft ceiling for the Go runtime, as a percentage of `controller.limits.memory`. The runtime does not read the container memory limit, so without this a refresh storm is terminated before garbage collection runs. Argo CD recommends 80-90%. Derived, so the two cannot drift |
| `controller.goMemLimit` | string | `''` | Explicit override for the derived ceiling, for memory limits not expressed in `Gi` or `Mi`. When neither this nor the derivation yields a value, the variable is omitted rather than guessed |
| `controller.respectRBAC` | string | `normal` | `normal` or `strict`. Stops the controller watching resource kinds it has no permission to list |
| `resourceExclusions` | string | Event, PackageManifest, compliance results | Kinds kept out of the cluster cache, which is the controller's largest allocation. Replaces the list rather than adding to it. A kind listed here cannot be deployed by this instance, so restrict it to generated resources |
| `controller.limits` | map | cpu `2000m`, memory `8Gi` | Application controller resource limits, sized for a full refresh storm rather than steady state |
| `controller.requests` | map | cpu `250m`, memory `1Gi` | Application controller resource requests. Measured peak was 843Mi on a hub with 51 Applications and 194 Policies during a full refresh. Requests are deliberately below limits so the pod is Burstable and does not reserve the ceiling |
| `ha.enabled` | bool | `false` | Run Argo CD in high availability mode |
| `ha.limits` | map | cpu `500m`, memory `256Mi` | High availability resource limits |
| `ha.requests` | map | cpu `250m`, memory `128Mi` | High availability resource requests |
| `redis.limits` | map | cpu `500m`, memory `256Mi` | Redis resource limits |
| `redis.requests` | map | cpu `250m`, memory `128Mi` | Redis resource requests |
| `dex.limits` | map | cpu `500m`, memory `256Mi` | Dex resource limits |
| `dex.requests` | map | cpu `250m`, memory `128Mi` | Dex resource requests |
| `policyGenerator.limits` | map | cpu `2000m`, memory `2048Mi` | PolicyGenerator plugin sidecar resource limits. Present only when the plugin is enabled |
| `policyGenerator.tarExclusions` | list | `.git/*` | Paths excluded from the repository tarball the repo server streams to a plugin sidecar. This setting is repo server wide, so it affects every plugin rendered Application on this Argo CD. Only `.git` is listed because no repository serves manifests from it. Adding generic names such as `docs/` risks silently emptying another team's Application |
| `policyGenerator.useManifestGeneratePaths` | bool | `true` | Honour the `argocd.argoproj.io/manifest-generate-paths` annotation that the AutoShift `ApplicationSet` stamps on each generated policy Application, so a policy ships only its own directory and `components/`. The flag has no effect on Applications without the annotation |
| `policyGenerator.requests` | map | cpu `500m`, memory `512Mi` | PolicyGenerator plugin sidecar resource requests. This container runs the render, so it is the one to raise when manifest generation times out |
| `applicationSet.limits` | map | cpu `2`, memory `1Gi` | ApplicationSet controller resource limits |
| `applicationSet.requests` | map | cpu `250m`, memory `512Mi` | ApplicationSet controller resource requests |

```yaml
clusterSets:
  hub:
    config:
      gitops:
        infra:
          controller:
            statusProcessors: 20
            operationProcessors: 10
          repo:
            replicas: 5
```

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

#### Changing the infra Argo CD name or namespace on a running cluster

Per cluster, the namespace is the `gitops-namespace` label and the name is
`config.gitops.infra.name`. Changing either builds a new instance and leaves the old one running,
because nothing is removed automatically. Two AutoShift Applications then reconcile the same
objects until you remove the old instance:

1. Change the label, the name, or both, and wait until the new instance is up and
   `policy-gitops-systems-argocd` reports Compliant.
2. Delete the old top-level AutoShift `Application` in the old namespace. Check it has no
   `resources-finalizer.argocd.argoproj.io` first, or its resources are deleted with it.
3. Delete the old `ApplicationSet` with `--cascade=orphan`. Otherwise it keeps restoring the
   finalizer on the `Application` objects it generated.
4. Remove the finalizer from each remaining `Application` in the old namespace, then delete them.
   The new instance adopts their resources.
5. Delete the old `ArgoCD` and its `cluster-admin-<old-name>-argocd-application-controller`
   binding. When the name and namespace change together, also look for an instance with the NEW
   name in the OLD namespace: the name can reach the policy before the label does, and it builds
   one there for that interval.


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
| `infra-nodes-provider`              | string            |                           | Provider type: `aws`, `vmware`, or `baremetal`. Bare metal reads the nodes from `config.infraNodes.nodes`, see [Bare-metal nodes](#bare-metal-nodes) |
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
| `storage-nodes-node-[iterator]`     | <list<String>> |               | List of node names to apply storage label to. Used for baremetal where `MachineSets` are not used. `config.storageNodes.nodes` takes precedence when set |

### Bare-metal nodes

Bare metal has no `MachineSet`, so AutoShift labels and taints existing nodes in place. List them
by Node name, as `oc get nodes` shows it, in the cluster's `config`. Placement still comes from the
labels: set `infra-nodes-provider` or `storage-nodes-provider` to `baremetal`, and `infra-nodes` or
`storage-nodes` to a count other than `0`. For infra the count only switches the policy on. For
storage it must equal the number of listed nodes, because the storage readiness check compares the
two.

```yaml
clusters:
  my-cluster:
    config:
      infraNodes:
        nodes:
          - name: 'infra-0.example.com'      # no taints key: the default infra taints
          - name: 'infra-1.example.com'
            taints:                          # replaces the defaults; [] means no taints
              - key: 'node-role.kubernetes.io/infra'
                effect: 'NoSchedule'
          - name: 'infra-2.example.com'
            removeTaints:                    # default taints minus NoExecute
              - key: 'node-role.kubernetes.io/infra'
                effect: 'NoExecute'
      storageNodes:
        infraRole: false                     # default true
        nodes:
          - name: 'storage-0.example.com'
            taints:
              - key: 'node.ocs.openshift.io/storage'
                value: 'true'
                effect: 'NoSchedule'
```

| Key | Type | Default | Notes |
|---|---|---|---|
| `infraNodes.nodes[].name` | string | | Node name. Required. A name with no matching Node fails the policy rather than creating one |
| `infraNodes.nodes[].taints` | list | `node-role.kubernetes.io/infra` with `NoSchedule` and `NoExecute` | Taints for this node, in Kubernetes `taints` form. The defaults match the infra `MachineSets`. `[]` adds no taints |
| `infraNodes.nodes[].removeTaints` | list | | Taints to remove, each a `key` with an optional `effect`. Without `effect` it matches every effect of the key. Wins over `taints` and the defaults |
| `storageNodes.infraRole` | bool | `true` | Whether storage nodes also get `node-role.kubernetes.io/infra`. `false` keeps them out of the infra role, and so out of the infra `MachineConfigPool` and the infra workload node selectors. Applies to the label-listed nodes too |
| `storageNodes.nodes[].name` | string | | Node name. Required. Wins over the `storage-nodes-node-[iterator]` labels |
| `storageNodes.nodes[].taints` | list | none | Taints for this node. Red Hat OpenShift Data Foundation tolerates `node.ocs.openshift.io/storage=true:NoSchedule` |
| `storageNodes.nodes[].removeTaints` | list | | Taints to remove, as for `infraNodes.nodes[].removeTaints`. Wins over `taints` |

The infra readiness check tests each listed node: the workload moves for ingress, monitoring, the image
registry and GitOps wait until every listed node is Ready with the infra role. Those workloads
tolerate only the `node-role.kubernetes.io/infra` taints, so any other taint on an infra node keeps
them off it.

A taint leaves a node only through `removeTaints`. Taking a taint out of `taints`, or a node out of
the list, leaves the node as it was, because the policies add and never subtract. While a taint
named in `removeTaints` is on a node, the policy rewrites that node once: the live `spec`, minus the
removed taints, plus any wanted taint it lacks. Labels merge as usual. After the taint is gone, the
entry can stay or go. Changing the `value` of a listed taint works the same way: the rewrite
replaces the taint with the same `key` and `effect`, since Kubernetes allows only one.

A cordoned node gets its labels but no taint change. The policy reports Compliant while it waits,
and the taints change on the first evaluation after the node is uncordoned.

Labels are never removed. Removing a node from a list or setting `infraRole: false` later leaves its
labels in place. Clean them up by hand:

```bash
oc label node <name> node-role.kubernetes.io/infra-
oc label node <name> cluster.ocs.openshift.io/openshift-storage-
```

### Red Hat Advanced Cluster Security

Container security across build, deploy, and runtime. Central and its configuration run on hub
clusters; every selected cluster runs `SecuredCluster`. Day 2 settings live in a `config.acs` block;
labels are reserved for the operator subscription and for choices that decide which policies are
placed on a cluster.

Three settings are labels rather than config, because each one selects which policies a cluster
receives: the authentication provider is `acs-auth-provider`, whether this cluster runs Central is
`acs-central`, and the baseline policies are `acs-default-policies`. Earlier releases took these as
`config.acs.auth.provider`, `config.acs.central.deploy` and `config.acs.defaultPolicies`; those keys
are no longer read and setting them has no effect.

**Labels:**

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `acs`                             | bool              |                           | If not set Advanced Cluster Security will not be managed |
| `acs-central`                     | bool              | `true`                    | Deploys Central on this hub. Opt out with `false`; a cluster with no such label still gets Central |
| `acs-default-policies`            | bool              | off                       | Deploy the baseline `SecurityPolicy` resources. Hub only, and requires the Config-as-Code component |
| `acs-auth-provider`               | string            | (unset)                   | Identity provider for Central. `openshift` configures OpenShift auth, which grants Admin to `config.acs.auth.adminGroup`. A cluster with no such label gets no auth configuration at all |
| `acs-registration`                | string            | `crs`                     | How secured clusters first authenticate to Central: `crs`, `manual`, or `initBundle` (legacy). Selects which registration policies are placed. Clusters with no such label get `crs` |
| `acs-subscription-name`           | string            | `rhacs-operator`          |       |
| `acs-channel`                     | string            | `stable`                  |       |
| `acs-version`                     | string            | (optional)                | Specific CSV version for controlled upgrades |
| `acs-source`                      | string            | `redhat-operators`        |       |
| `acs-source-namespace`            | string            | `openshift-marketplace`   |       |

**Config block** (`config.acs`):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `egressConnectivity` | string | `Online` | `Online` or `Offline`; use `Offline` when disconnected |
| `scannerV4` | string | `Enabled` | Scanner V4 state. `SecuredCluster` maps `Enabled` to `AutoSense` |
| `scanner` | string | `AutoSense` | `SecuredCluster` Scanner v2: `AutoSense` or `Disabled` |
| `monitoring` | bool | `true` | OpenShift monitoring integration |
| `exposeMetrics` | string | | `Enabled` or `Disabled`; Prometheus endpoint on Central, Scanner and Scanner V4 |
| `networkPolicies` | string | | Network policy generation (`Enabled` or `Disabled`); omit to leave the default |
| `vmScanning` | bool | `false` | Technology Preview. Sets `ROX_VIRTUAL_MACHINES`; scanning also needs an agent inside each guest, which AutoShift does not manage |
| `configAsCode` | string | | `Enabled` or `Disabled`. Deploys the component that reconciles `SecurityPolicy` resources into Central. Disabling it silently stops `defaultPolicies` from taking effect |
| `telemetry` | bool | `false` | Central telemetry reporting |
| `notifierSecretsEncryption` | bool | `false` | Encrypt notifier secrets at rest |
| `registryOverride` | string | | Override the image registry, for disconnected mirrors |
| `auditLogs` | string | `Auto` | OpenShift audit log collection: `Auto`, `Enabled`, `Disabled` |
| `fileActivityMonitoring` | string | `Disabled` | Technology Preview. Per-node file activity monitoring |
| `additionalCAs` | list | | Extra trusted root certificate authorities, as `{name, content}` entries |
| `pinToNodes` | string | | `None` or `InfraRole`. Shorthand that sets `nodeSelector` and `tolerations` itself; cannot be combined with them |
| `nodeSelector` | map | | Custom placement for Deployment-based components; ignored when `pinToNodes` is set |
| `tolerations` | list | | Custom taints to tolerate; ignored when `pinToNodes` is set |
| `db.size` | string | | Central database volume size, for example `100Gi` |
| `db.storageClassName` | string | | Blank uses the cluster default storage class |
| `db.claimName` | string | `central-db` | The Central database claim. Blank uses the Operator's own name. Naming a second claim leaves Central `Irreconcilable` while still reporting `Available` |
| `collector.collection` | string | `CORE_BPF` | `CORE_BPF` is recommended; `EBPF` is deprecated |
| `processBaselines.autoLock` | string | `Disabled` | Lock process baselines when the observation period ends |
| `processIndicators.persistence` | string | `Enabled` | Persist process indicators |
| `processIndicators.excludeOpenshiftNs` | string | `Disabled` | Drop indicators originating in OpenShift namespaces |
| `processIndicators.excludeNamespaceRegex` | string | | Additional namespaces to exclude |
| `admissionControl.enabled` | bool | `false` | Sets `enforcement`. The older `listenOn*` and `contactImageScanners` fields are deprecated |
| `admissionControl.bypass` | string | `BreakGlassAnnotation` | `BreakGlassAnnotation` or `Disabled` |
| `admissionControl.replicas` | int | `3` | Admission control pod replicas |
| `admissionControl.failurePolicy` | string | `Ignore` | `Ignore` fails open; `Fail` fails closed |
| `auth.minimumRole` | string | `None` | Minimum role for authenticated users. Hub only |
| `auth.adminGroup` | string | `cluster-admins` | Group mapped to the Admin role. Hub only |
| `central.endpoint` | string | | Blank discovers Central's route on this hub |
| `registration.validFor` | string | `168h` | Cluster registration secret lifetime. It only has to cover mint through registration, not the life of the cluster |
| `registration.maxClusters` | int | `1` | Clusters one secret may register. One secret is minted per secured cluster, so a leaked token registers exactly one cluster. `0` removes the limit |
| `registration.roxctlImage` | string | | Blank builds `registry.redhat.io/advanced-cluster-security/rhacs-roxctl-rhel9` at the tag matching the installed operator version. Set it explicitly for a disconnected mirror, because the default is a tag on `registry.redhat.io` rather than a mirrored digest |

See the [policy README](../policies/stable/advanced-cluster-security/README.md) for the registration
modes and for where Central runs.

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
| `dbImage` | string | | `quay-db-mode` `managed` only. PostgreSQL image for `quay-db` and `clair-db`. Empty means the CloudNativePG Operator default |
| `dbPrimaryUpdate` | string | `unsupervised` | `quay-db-mode` `managed` only. `supervised` updates the replicas and waits for a manual switchover of the primary. Needs two or more instances |

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

**Config block** (`config.gitops.teams.<team>.projects`):

AppProjects for the team, keyed by name. Each entry is the `AppProject` spec as written, created
where the team's Argo CD reads it and enforced exactly. With an agent, `default` is derived from the
agent settings and an entry named `default` replaces it. See
[Argo CD agent: Projects](gitops-agent.md#projects), which also covers labeling the namespaces a
team deploys into.

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
| `compliance`                          | bool              |                           | If not set Compliance Operator will not be managed. Scans, pinned profiles and the remediation reject list are set in `config.compliance`; see [Compliance and STIG](compliance.md) |
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

Labels install the operator and select modes. Instance settings live in `config.aap.instance`.

| Variable                         | Type      | Default Value              | Notes |
|----------------------------------|-----------|----------------------------|-------|
| `aap`                            | bool      | `true` or `false`          |  |
| `aap-channel`                    | string    | `stable-2.6`               |  |
| `aap-install-plan-approval`      | string    | `Automatic`                |  |
| `aap-source`                     | string    | `redhat-operators`         |  |
| `aap-version`                    | string    | (optional)                 | Specific CSV version for controlled upgrades |
| `aap-noobaa-s3-storage`          | bool      | `false`                    | 'true' creates an OpenShift Data Foundation NooBaa bucket and backs Hub content with it. |
| `aap-custom-cabundle`            | bool      | `false`                    | 'true' injects the cluster CA bundle into the instance through the `user-ca-bundle` Secret. |

**Config block** (`config.aap`):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `versions` | list | | Permitted operator CSV versions |
| `startingCSV` | string | | Initial install pin. An empty value lets Operator Lifecycle Manager choose |
| `instance` | map | | `AnsibleAutomationPlatform` spec fields. Any field of the spec is accepted |

Fields in `instance` use the custom resource's own names, which are snake_case, such as
`file_storage_size`, rather than the lowerCamelCase of AutoShift's own keys. List them with
`oc explain ansibleautomationplatform.spec --recursive`. A misspelled top-level field is rejected:
`policy-aap-instance` reports NonCompliant with `unknown field` and applies nothing. A misspelled
field inside a component block such as `hub`, `controller`, or `eda` is accepted and stored, but the
operator ignores it and every policy stays Compliant.

`policy-aap-ready-test` reports NonCompliant until the instance reconciles successfully, which covers
every enabled component. Check it after changing `instance`, because `policy-aap-instance` confirms
only that the spec was applied. Disabling a component that is already deployed does not remove its
custom resource, so a leftover failing `AutomationHub`, for example, keeps the check NonCompliant
until you delete it.

The instance spec starts from a base of the controller and Hub enabled, Event-Driven Ansible and
Red Hat Ansible Lightspeed disabled, and `redis_mode: standalone`. The mode labels add their fields,
and `instance` is merged last. The merge is key by key and a value in `instance` wins, including
`false`, `0`, and an empty string. A list replaces the default list whole. A field an earlier layer
adds cannot be removed from `instance`, only changed.

Hub content needs storage. Set `aap-noobaa-s3-storage`, or set it in `instance.hub`:

```yaml
config:
  aap:
    instance:
      eda:
        disabled: false
      hub:
        storage_type: file
        file_storage_size: 10Gi
        file_storage_storage_class: ocs-storagecluster-cephfs  # must support ReadWriteMany
```

To use your own S3 bucket, create a Secret in `ansible-automation-platform` on the cluster with
`s3-access-key-id`, `s3-secret-access-key`, and `s3-bucket-name`, plus `s3-region` or `s3-endpoint`,
then name it in the configuration. Credentials never go in a values file. `policy-aap-hub-s3-test`
reports when the Secret or one of its keys is missing.

```yaml
config:
  aap:
    instance:
      hub:
        storage_type: S3
        object_storage_s3_secret: aap-s3
```

**Deprecated labels.** These are still applied when set, and `config.aap.instance` wins over them.
Move each to the field shown.

| Label | Replacement in `config.aap.instance` |
|-------|--------------------------------------|
| `aap-hub-disabled` | `hub.disabled` |
| `aap-eda-disabled` | `eda.disabled` |
| `aap-lightspeed-disabled` | `lightspeed.disabled` |
| `aap-file-storage: 'true'` | `hub.storage_type: file` |
| `aap-storage-type` | `hub.storage_type` |
| `aap-file_storage_size` | `hub.file_storage_size` |
| `aap-file_storage_storage_class` | `hub.file_storage_storage_class` |

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

### Project onboarding

Creates an OpenShift Project (a Namespace with Project annotations) for each entry in
`config.projectOnboarding.projects`, with templated labels and annotations, RoleBindings for admin, write, and read
groups, a ResourceQuota and LimitRange from a named size, NetworkPolicies, an EgressFirewall, and an
AdminNetworkPolicy from a named network profile and the project's own rules, and an EgressIP from
the project's own addresses. Admin network policies add cluster-wide rules that no project can
override. Shared conventions live in `config.projectOnboarding.defaults`. A project that cannot be onboarded is
skipped and listed in the status ConfigMap of its shard, `project-onboarding-status-<shard>`. The
projects are split by a hash of the name into eight shard Policies, `policy-project-onboarding-0`
to `-7`, each with an inform-only `policy-project-onboarding-<shard>-test` that is NonCompliant
while its shard's status ConfigMap exists. With `project-onboarding-prune` off, stale owned objects
and dropped catalog labels also keep the `-test` Policy NonCompliant, without a status ConfigMap. The
hub keeps each resolved shard Policy below 1 MiB: past that, the remaining projects of the shard in
name order are not rendered and are listed under the status key `project-onboarding.size` (about
278 fully featured or 728 groups-only projects per cluster with pruning on, 244 or 558 with it off,
fewer with long names or names that fall unevenly into the shards). More than 3,200 declared
projects, or more than a shard can hold even with projects skipped, fail the hub template. After a project moves to a live new namespace, a namespace that still carries its
label, other than a platform namespace, loses its ownership labels with pruning on, and is reported
with pruning off. With `project-onboarding-delete-namespaces` and `project-onboarding-prune` both
`'true'`, the namespace of a removed project is deleted, irreversibly and with everything in it, but
only when onboarding created it, the project set `deleteOnRemoval: true`, no current project
declares it, it holds no storage, and the project has stayed undeclared for the grace period after
the namespace was marked. See the [policy README](../policies/stable/project-onboarding/README.md).

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `project-onboarding`                  | bool              | `false`                   | If not set, no projects are onboarded |
| `project-onboarding-prune`            | bool              | `false`                   | `true` deletes owned objects that are no longer declared and removes dropped catalog labels. Any other value only reports them. Never deletes a namespace on its own |
| `project-onboarding-delete-namespaces` | bool             | `false`                   | `true`, with `project-onboarding-prune` also `true`, deletes the namespace of a removed project that onboarding created and that set `deleteOnRemoval: true`, unless a current project declares it or it holds storage (PersistentVolumeClaims, VolumeSnapshots, ObjectBucketClaims, DataVolumes, or VirtualMachines). The namespace is first marked and deleted only after `projectOnboarding.defaults.namespaceDeleteGracePeriod`. Irreversible: every object in the namespace is deleted. Adopted and pre-existing namespaces are never deleted. Any other value keeps the namespace and lists it in the status ConfigMap |

Configuration keys, set in the `config:` block:

| Key                                           | Type              | Default Value             | Notes |
|-----------------------------------------------|-------------------|---------------------------|-------|
| `projectOnboarding.defaults.groups.<level>`              | string            |                           | Group-name template for `admin`, `write`, or `read`, for example `K8S_{team}_ADMINS_{environment}` |
| `projectOnboarding.defaults.roles.<level>`               | string            | `admin`, `edit`, `view`   | ClusterRole bound at each level |
| `projectOnboarding.defaults.labels.<entry>`              | map               |                           | Label catalog entry: `enabled` (a Boolean, default `true`; any other value is reported), `key`, and `value`. Key and value can be templates. A placeholder is any `{...}`; values are inserted once and never expanded again. Two entries setting one key to different values are reported, and the first by name applies |
| `projectOnboarding.defaults.annotations.<entry>`         | map               |                           | Annotation catalog entry, same shape as labels. The key must be a qualified name, as a label key is; any other key is skipped and reported |
| `projectOnboarding.defaults.allowedPlatformKeys.<key>`   | bool              | (none allowed)            | `true` lets projects set this exact platform label or annotation key. Without it, `network.openshift.io/policy-group`, `pod-security.kubernetes.io/*`, `openshift.io/run-level`, `openshift.io/sa.scc.*`, `openshift.io/node-selector`, `scheduler.alpha.kubernetes.io/*`, `kubernetes.io/metadata.name`, and `security.openshift.io/*` are skipped and reported. Never a prefix |
| `projectOnboarding.defaults.sizes.<size>.quota`          | map               |                           | `spec.hard` of the ResourceQuota `autoshift-quota`, resource name to quantity, for example `pods: '50'`. A key without `/` must be a standard quota resource such as `pods` or `requests.cpu`. A quantity not in the canonical form the API server stores, such as `0.5` for `500m` or `1024Mi` for `1Gi`, renders as written and is listed as a status note |
| `projectOnboarding.defaults.sizes.<size>.limitRange`     | map               |                           | The `Container` item of the LimitRange `autoshift-limits`: `default`, `defaultRequest`, `max`, `min`, and `maxLimitRequestRatio`, each a map of resource name to quantity. A key without `/` must be `cpu`, `memory`, `ephemeral-storage`, or `hugepages-<size>` |
| `projectOnboarding.defaults.networkProfiles.<profile>.defaultDeny` | map     |                           | `ingress` and `egress`, each `true` or `false` as a Boolean, not a string. A profile or a project `network` key other than `defaultDeny` and `rules` is an error. Renders `autoshift-default-deny-ingress` and `autoshift-default-deny-egress`; `egress` also ends the EgressFirewall `default` with a final `Deny` |
| `projectOnboarding.defaults.networkProfiles.<profile>.rules.<rule>` | map    |                           | A network rule: `direction` (`ingress` or `egress`), `action` (`allow` or `deny`), `from` or `to` with peers, optional `ports` (numbers, names, or `port` and `protocol` maps; TCP by default; an empty list is an error), `enabled`, and `description`. Peers, whose `true` is a Boolean, not a string: `sameNamespace`, `namespaces` (`labels` or `names`), `pods` (`labels`), `router`, `monitoring`, `dns` (egress), `cidrs`, and `dnsNames` (egress; an IP address, a cluster-internal name such as `x.svc.cluster.local`, or a label over 63 characters is an error). In a direction with `defaultDeny`, each in-cluster allow rule renders the NetworkPolicy `autoshift-rule-<rule>`; without one, an allow renders no NetworkPolicy, since that would isolate the pods. An ingress `cidrs` allow there renders an `ipBlock` that excepts the cluster and service networks inside it; a CIDR inside one of them, or any while they cannot be read, is an error. An IPv4-mapped IPv6 CIDR is an error. Each egress rule to `cidrs` or `dnsNames`, allow or deny, renders entries of the EgressFirewall `default`, project rules before profile rules. An egress allow CIDR inside the cluster or service network, or an egress deny CIDR overlapping one, is an error. Each in-cluster deny rule renders a `Deny` in the AdminNetworkPolicy `autoshift-project-<project>` (priority 50), project rules before profile rules. An ingress deny from `cidrs` and a deny of `router` are errors. See the README for every peer and limit |
| `projectOnboarding.defaults.adminNetworkPolicies.<entry>` | map              |                           | A rule no project can override, with the fields of a network rule plus `appliesTo`: exactly one of `allProjects: true` (every onboarded project), `labels` (onboarded projects with these namespace labels), or `namespaceSelector` (any namespaces, a label selector passed through; an empty selector also selects every `openshift-*` namespace, so a `deny` with it breaks the platform). `allProjects` and `labels` match every namespace with the `autoshift.io/project` label, including `mode: inform` projects and removed projects whose namespace keeps the label. Renders the AdminNetworkPolicy `autoshift-admin-<entry>`, priority 10 upward in entry-name order, so at most 40 entries; during a renumber two can briefly share a priority. Peers: `namespaces`, `router` (allow only), `monitoring`, `dns`, and egress `cidrs` (a CIDR with host bits set or an IPv4-mapped IPv6 range is an error); port numbers only, an empty `ports` list is an error, and `ports` apply to the peers other than `dns`, so `ports` with `dns` as the only peer is an error. `enabled: false` or removing an entry deletes its AdminNetworkPolicy only with `project-onboarding-prune` set to `'true'`; with pruning off the stale one keeps enforcing at its old priority, is reported, and can share a priority with a renumbered live entry, in an undefined order. Any invalid entry stops the whole admin tier and is listed under the status key `projectOnboarding.defaults.adminNetworkPolicies` |
| `projectOnboarding.defaults.maxNamespaceDeletes`         | int               | (no limit)                | Circuit breaker for namespace deletion. `0` deletes nothing and reports each candidate. A positive N deletes nothing when more than N namespaces whose grace period is over, counted across all shards, would be deleted in one evaluation, and lists them all in the status. Marking a namespace is not counted. An integer up to 999,999,999, also in exponent form such as `1e+06`; any other value is reported and treated as `0` |
| `projectOnboarding.defaults.namespaceDeleteGracePeriod`  | string            | `24h`                     | How long a removed project must stay undeclared before its namespace is deleted. The first evaluation only marks the namespace with `autoshift.io/onboarding-pending-deletion-since`; declaring the project again removes the mark. Hours, minutes, and seconds, in that order and each optional, such as `24h`, `90m`, or `1h30m`, at least `1h`; a shorter one is reported and `24h` is used. A change applies to existing marks too. Any other value is reported, and nothing is marked or deleted. Turning `project-onboarding-delete-namespaces` or `project-onboarding-prune` off removes every mark. An empty or missing `projects` never marks or deletes |
| `networking.clusterNetwork`, `networking.serviceNetwork` | list  | (optional)                | Fallback cluster network and service network CIDRs for `autoshift-default-deny-egress` and the EgressFirewall CIDR checks, used only when the managed cluster's `Network` config `cluster` gives none. The cluster-install shape: `clusterNetwork` entries are maps with `cidr` |
| `projectOnboarding.projects.<name>.namespace`                   | string            | the project name          | Namespace to create or adopt, a string. `null` uses the project name, and so does `''`, with a status note. A namespace that several projects declare belongs to the project named after it, otherwise to the first by name; the others are skipped and reported. The namespace and the project name must be DNS-1123 labels. A platform namespace (`openshift`, `default`, `multicluster-engine`, `hive`, or a name starting with `kube-`, `openshift-`, or `open-cluster-management`) skips the project and is reported, with no override |
| `projectOnboarding.projects.<name>.displayName`                 | string            | (optional)                | Becomes `openshift.io/display-name`. `description` and `requester` work the same way. A list or map is reported, and so is a catalog annotation that sets the same key, which the field overrides |
| `projectOnboarding.projects.<name>.groups.<level>`              | string            | (optional)                | Overrides the resolved group name for one level. `''`, `null`, or `false` drops the level for the project. A level other than `admin`, `write`, and `read` is reported. A `write` or `read` override with an unresolved placeholder, or a blank one, gets no RoleBinding and is listed as a status note, which is not an error |
| `projectOnboarding.projects.<name>.labels.<entry>`              | map               | (optional)                | Overrides a catalog entry by name, or adds a project-only label |
| `projectOnboarding.projects.<name>.annotations.<entry>`         | map               | (optional)                | Overrides an annotation catalog entry, or adds one |
| `projectOnboarding.projects.<name>.size`                        | string            | (optional)                | Selects `projectOnboarding.defaults.sizes.<size>`. Without it, no ResourceQuota or LimitRange is rendered, and a `quota` or `limitRange` set on the project is reported. An undefined size, or one that is not a string, skips the project and is reported |
| `projectOnboarding.projects.<name>.quota`, `projectOnboarding.projects.<name>.limitRange` | map       | (optional)                | Merged over the selected size key by key. An empty value (`''` or `null`) is ignored and cannot remove a key of the size. A `limitRange` field that is set and not a map, such as `max: 0`, is reported |
| `projectOnboarding.projects.<name>.networkProfile`              | string            | (optional)                | Selects `projectOnboarding.defaults.networkProfiles.<profile>`. An undefined profile, or a value that is not a string, skips the project and is reported |
| `projectOnboarding.projects.<name>.network.rules.<rule>`        | map               | (optional)                | Overrides the profile's rule of the same name field by field, or adds a rule. `enabled: false` removes an inherited rule. A project allow also overrides a profile deny in the AdminNetworkPolicy. A rule that cannot be rendered skips the project and is reported |
| `projectOnboarding.projects.<name>.network.defaultDeny`         | map               | (optional)                | Overrides the profile's `defaultDeny.ingress` or `defaultDeny.egress` |
| `projectOnboarding.projects.<name>.egressIP`                    | list              | (optional)                | IPv4 or IPv6 addresses, without a prefix length, for the cluster-scoped EgressIP `autoshift-<name>`, which selects the project namespace. Replaced whole by a lower level; an address listed twice is rendered once. Not a list, an invalid address, a loopback, link-local, multicast, broadcast, or reserved address, an address inside the cluster or service network (checked on the managed cluster), or an address another project also declares skips the project and is reported (on every project that declares it). Needs nodes labeled `k8s.ovn.org/egress-assignable` and addresses in their subnet |
| `projectOnboarding.projects.<name>.adopt`                       | bool              | `false`                   | `true` lets the project take over an existing namespace that no project owns. A namespace owned by another project is never taken over. A value other than `true` or `false` is reported and counts as `false` |
| `projectOnboarding.projects.<name>.deleteOnRemoval`             | bool              | `false`                   | `true` lets onboarding delete the namespace once the project is removed from `config.projectOnboarding.projects`, if onboarding created it, the cluster labels `project-onboarding-delete-namespaces` and `project-onboarding-prune` are `true`, it holds no storage, and the grace period after it is marked is over. Recorded on the namespace while the project is declared. Ignored, with a status note, with `adopt: true`. A value other than `true` or `false` is reported and counts as `false` |
| `projectOnboarding.projects.<name>.mode`                        | string            | `enforce`                 | `enforce` or `inform`. With `adopt: true` and no mode, the default is `inform`. Any other value, including `false` (`mode: off` in YAML), is reported and treated as `inform` |
| `projectOnboarding.projects.<name>.<field>`                     | string            | (optional)                | Any other key is a placeholder value for `{field}` in templates; `{name}` is always the project name |

### Custom SecurityContextConstraints

Creates a SecurityContextConstraints object for each entry in `config.customSccs`,
a ClusterRole that grants `use` on it, and a RoleBinding in each namespace whose ServiceAccounts are
listed. Fields that an entry does not set keep the posture of the built-in `nonroot-v2` SCC. Access
is granted through RBAC only, never through the `users` or `groups` fields of the SCC. An entry that
cannot be rendered is skipped and listed in the `custom-sccs-status` ConfigMap, which keeps
`policy-custom-sccs-test` NonCompliant while errors exist. Objects that are no longer declared are reported, never
deleted. See the [policy README](../policies/stable/custom-sccs/README.md).

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `custom-sccs`                         | bool              | `false`                   | If not set, no SCCs are managed |

Configuration keys, set in the `config:` block:

| Key                                           | Type              | Default Value             | Notes |
|-----------------------------------------------|-------------------|---------------------------|-------|
| `customSccs.<name>.enabled`   | bool              | `true`                    | `false` renders nothing for the entry. Its existing objects are reported until removed by hand |
| `customSccs.<name>.spec`      | map               | `nonroot-v2` posture      | SCC fields merged over the defaults key by key. A strategy such as `runAsUser` replaces the default strategy whole. `users` and `groups` are rejected. See the README for the defaults and the accepted fields |
| `customSccs.<name>.serviceAccounts` | map         | (optional)                | `<namespace>/<serviceaccount>: true` grants `use` through the RoleBinding `autoshift-scc-<name>` in that namespace. `false` keeps a ServiceAccount out, so a cluster file can revoke one that a clusterset file grants. A `true` ServiceAccount in a platform namespace (`kube-*`, `openshift`, `openshift-*`, `default`, `open-cluster-management*`, `multicluster-engine`, `hive`) is an error, with no override. A RoleBinding of that name not labeled `autoshift.io/managed-by: custom-sccs` is reported and left alone |
| `customSccs.<name>.allowPrivileged` | bool        | `false`                   | Required to set `allowPrivilegedContainer`, any `allowHost*` field to `true`, `runAsUser.type` or `seLinuxContext.type` to `RunAsAny`, a `hostPath`, `*`, or node or network storage volume such as `nfs` or `flexVolume`, a privileged or host-affecting capability such as `SYS_ADMIN` or `SYS_NICE`, an unconfined SELinux type such as `spc_t`, an SELinux level that weakens MCS separation (no categories, more than two, or a range, such as `s0` or `s0:c0.c511`), an empty `seccompProfiles`, or any `allowedUnsafeSysctls`. Without it those entries are rejected. An existing SCC or ClusterRole of the generated name that custom-sccs does not own holds the entry. The full list is in the policy README |

### Namespace RBAC

Creates ServiceAccounts, Roles, and RoleBindings in namespaces that already exist, from
`config.namespaceRbac`, for workload identities that need a few exact permissions in one namespace.
Roles and RoleBindings are enforced exactly, so rules and subjects are replaced rather than merged.
A RoleBinding binds a declared Role or the built-in ClusterRole `edit` or `view`, never another
ClusterRole. Every rule passes a safety gate, and no namespace or cluster-scoped object is ever created. A
long-lived token Secret is created only for a minter-only ServiceAccount that requests one. An entry that is refused or cannot be rendered is skipped and listed in the
`namespace-rbac-status` ConfigMap, which keeps `policy-namespace-rbac-test` NonCompliant while
errors exist. An
object of a declared name that exists without the `autoshift.io/managed-by: namespace-rbac` label
is reported and never changed. Objects that are no longer declared are reported, never deleted. See
the [policy README](../policies/stable/namespace-rbac/README.md).

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `namespace-rbac`                      | bool              | `false`                   | If not set, no namespace RBAC is managed |

Configuration keys, set in the `config:` block:

| Key                                           | Type              | Default Value             | Notes |
|-----------------------------------------------|-------------------|---------------------------|-------|
| `namespaceRbac.<namespace>`                   | map               |                           | One existing namespace, a DNS-1123 label. Only the keys below are allowed. A platform namespace (`kube-*`, `openshift`, `openshift-*`, `default`, `open-cluster-management*`, `multicluster-engine`, `hive`, `local-cluster`, `policies-*`, `hypershift`, `clusters`, `clusters-*`) or the namespace of a ManagedCluster on the managed cluster is an error, with no override. A namespace that does not exist is reported, never created |
| `namespaceRbac.<namespace>.serviceAccounts`   | map               | (optional)                | ServiceAccount name to `true` (create and keep), `false` (no longer declared, reported if it exists), or `{tokenSecret: <name>}`: `true` plus a long-lived token Secret of type `kubernetes.io/service-account-token`, the one exception to TokenRequest-only tokens. It is refused unless every RoleBinding of the policy that binds the ServiceAccount points to a minter-only Role (only `serviceaccounts/token` `create` and `serviceaccounts` `get`, for ServiceAccounts declared `true`), and it is listed as an informational line in `namespace-rbac-status`. While a ServiceAccount has a `tokenSecret`, it cannot be bound to other Roles, no Role can read its Secret or mint its tokens, and Roles that create workloads in its namespace are refused. Live RoleBindings that name it are checked before the Secret is rendered. `default` is an error. A ServiceAccount that already exists without the `autoshift.io/managed-by: namespace-rbac` label is reported, never taken over |
| `namespaceRbac.<namespace>.roles.<name>.rules` | list             |                           | Required. Rules with `apiGroups`, `resources`, `verbs`, and an optional non-empty `resourceNames`. Rejected: `*` anywhere, the `escalate`, `bind`, and `impersonate` verbs, RBAC objects, `securitycontextconstraints`, `nodes`, `pods/exec` and the other subresources that reach into workloads, `builds/custom`, Red Hat Advanced Cluster Management resources such as `manifestworks` and `policies`, `secrets` and `serviceaccounts/token` without `resourceNames`, a `serviceaccounts/token` name that is not a `true` entry of `serviceAccounts`, and writes to `namespaces` or `projects`. A Role is held on the managed cluster when its `secrets` names include a ServiceAccount token or `dockercfg` Secret, or its `serviceaccounts/token` names include a ServiceAccount without the managed-by label. A held object keeps its live rules or subjects. Granting `create` or `patch` on `buildconfigs` effectively grants read access to every Secret of the namespace. The full list is in the policy README |
| `namespaceRbac.<namespace>.roleBindings.<name>.role` | string     |                           | A Role declared in `roles` of the same namespace. Set exactly one of `role` and `clusterRole` |
| `namespaceRbac.<namespace>.roleBindings.<name>.clusterRole` | string |                         | `edit` or `view`, in place of `role`: a RoleBinding to the built-in ClusterRole, limited to the namespace of the entry. Any other ClusterRole, `admin` and `cluster-admin` included, is an error. A ServiceAccount that a long-lived token can act as is never a subject, and `edit` is refused in a namespace with a `tokenSecret` ServiceAccount. See the policy README |
| `namespaceRbac.<namespace>.roleBindings.<name>.serviceAccounts` | map |                         | Required. `<namespace>/<serviceaccount>` to `true` or `false`. Subjects are ServiceAccounts only. A subject in a platform or ManagedCluster namespace, or named `default`, is an error. A subject whose namespace does not exist on the managed cluster is left out and reported. A RoleBinding with no `true` subject is no longer declared |

### Project networks

Attaches projects to a VLAN. Each entry in `config.projectNetworks` becomes a
ClusterUserDefinedNetwork with `Localnet` topology, which is always a secondary network, on an
access VLAN. A namespace attaches when its labels match the entry's `namespaceSelector`, so the
`vlan` label of project onboarding is the usual way a project opts in. The OVN bridge mapping for
the physical network comes from the `nmstate` policy and is not created here. An invalid entry fails
the policy with a message naming the entry. The network of a ClusterUserDefinedNetwork is
immutable, so a changed `vlan`, `physicalNetworkName`, or `subnets` is reported, not applied; use a
new entry name instead. An existing network without the `autoshift.io/managed-by: project-networks`
label is never taken over. A removed entry is reported and never deleted. The reports come from
`policy-project-networks-test`. See the
[policy README](../policies/stable/project-networks/README.md).

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `project-networks`                    | bool              | `false`                   | If not set, no project networks are managed |

Configuration keys, set in the `config:` block:

| Key                                           | Type              | Default Value             | Notes |
|-----------------------------------------------|-------------------|---------------------------|-------|
| `projectNetworks.<name>`                      | map               |                           | One ClusterUserDefinedNetwork named `<name>`, a DNS-1123 label. Only the keys below are allowed |
| `projectNetworks.<name>.vlan`                 | int               |                           | Required. Access VLAN ID, 1 to 4094 |
| `projectNetworks.<name>.physicalNetworkName`  | string            |                           | Required. The `localnet` name of an OVN bridge mapping from the `nmstate` policy's `ovnMappings`. Must not contain `,`, `:`, whitespace, or control characters |
| `projectNetworks.<name>.subnets`              | list              | (optional)                | One CIDR, or one IPv4 and one IPv6 CIDR, each a valid CIDR. A value that is set but not a list is an error. OVN-Kubernetes assigns pod addresses from them. Without subnets, or with an empty list, IPAM is disabled (`ipam.mode: Disabled`) |
| `projectNetworks.<name>.namespaceSelector`    | map               |                           | Required and not empty. A label selector with `matchLabels`, `matchExpressions`, or both. Quote label values, for example `'true'`. Keys and values must be valid Kubernetes label keys and values. It needs a positive requirement (a `matchLabels` pair, `In`, or `Exists` on a key other than `kubernetes.io/metadata.name`), so it cannot match every namespace, and it must not name a platform namespace such as `openshift-*` or `kube-system` |

### EgressIP nodes

Makes nodes assignable for EgressIP objects, such as the ones that project onboarding creates. Each
selected node gets the label `k8s.ovn.org/egress-assignable: ""`. Only that label is added; no other
field of the Node is changed. OVN-Kubernetes assigns an EgressIP address only to an assignable node
whose node subnet contains the address. A node that does not exist is reported, not created (apart from a node deleted between the lookup and the apply).
The label is never removed automatically: a labeled node that is no longer selected is reported in
the `egressip-nodes-status` ConfigMap, which keeps `policy-egressip-nodes-test` NonCompliant, and
you remove the label
with `oc label node <name> k8s.ovn.org/egress-assignable-`. An invalid key fails the policy with a
message naming the key. See the [policy README](../policies/stable/egressip-nodes/README.md).

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `egressip-nodes`                      | bool              | `false`                   | If not set, no node is labeled |

Configuration keys, set in the `config:` block:

| Key                                           | Type              | Default Value             | Notes |
|-----------------------------------------------|-------------------|---------------------------|-------|
| `egressipNodes.<node>`                        | bool              |                           | `true` selects the node named `<node>`, a DNS-1123 subdomain. `false` never selects it, even when `nodeSelector` matches it, so a cluster file can exclude a node that a clusterset file selects |
| `egressipNodes.nodeSelector.matchLabels`      | map               | (optional)                | Also selects every node with all of these labels. Must not be empty. Quote label values, for example `''` or `'true'` |

### Manual remediations

One policy per Security Technical Implementation Guide finding the Compliance Operator ships no automatic remediation for, because each needs a site decision: permitted registries, banner text, audit log destination.

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `manual-remediations`             | bool              |                           | Enables the whole set. Each remediation is configured under `config.manualRemediations` and renders nothing until its key is set; see [Compliance and STIG](compliance.md) |

### Helper job images

AutoShift runs a small number of its own Jobs that need only the `oc` command, such as the Red Hat
Advanced Cluster Security registration Job and the Vault initialization Job. Each resolves its image
at runtime, highest priority first:

1. `config.images.cli`, if set.
2. The cluster's own `openshift/cli` ImageStream. That entry is a digest against the release payload,
   so an ImageDigestMirrorSet rewrites it in a disconnected deployment and no ImageTagMirrorSet is
   required.
3. The in-cluster registry, which is the last resort and is absent wherever the registry Operator is
   set to `Removed`, as on bare metal and in most disconnected deployments.

Set the value only to pin a specific mirrored image. It belongs in `config`, not in a label: an image
reference contains characters that are not legal in a Kubernetes label value.

| Variable | Type | Default Value | Notes |
|----------|------|---------------|-------|
| `images.cli` | string | | Image for AutoShift helper Jobs. Blank resolves from the `openshift/cli` ImageStream |
