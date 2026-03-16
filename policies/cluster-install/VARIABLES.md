# Helm Chart Variable Reference

Complete reference of all variables across the three Helm charts, including where each is defined and which template(s) consume it.

---

## Chart: cluster-install

### Cluster-Level Values

Defined in per-cluster values files (e.g., `my-cluster.yaml`, `example-cluster.yaml`).

| Variable | Type | Default | Description | Used By Templates |
|----------|------|---------|-------------|-------------------|
| `clusterName` | String | `$.Release.Name` | Name of the cluster, without the base domain (e.g., `my-cluster`) | `0-Namespace.yaml`, `0-ClusterImageSet.yaml`, `0-SecretReplicationPolicy.yaml`, `1-AgentClusterInstall.yaml`, `1-BareMetalHost.yaml`, `1-ClusterDeployment.yaml`, `1-InfraEnv.yaml`, `1-networkConfigs.yaml` |
| `baseDomain` | String | None | Base DNS domain for the cluster (e.g., `example.com`). Appended to hostnames for FQDN generation. | `1-BareMetalHost.yaml`, `1-ClusterDeployment.yaml`, `1-networkConfigs.yaml` |
| `openshiftVersion` | String | None | OpenShift version to install (e.g., `4.19.9`). Not required if `clusterImageSet` is provided. | `0-ClusterImageSet.yaml`, `1-AgentClusterInstall.yaml` |
| `clusterImageSet` | String | None | Name of an existing ClusterImageSet to use. When set, suppresses creation of a new ClusterImageSet and overrides `openshiftVersion`/`cpuArch`. | `0-ClusterImageSet.yaml`, `1-AgentClusterInstall.yaml` |
| `cpuArch` | String | `x86_64` | CPU architecture of the target cluster | `0-ClusterImageSet.yaml`, `1-InfraEnv.yaml` |
| `apiVip` | IPv4 String | None | API VIP address (`api.<clusterName>.<baseDomain>`). Only required when `controlPlaneAgents > 1`. | `1-AgentClusterInstall.yaml` |
| `ingressVip` | IPv4 String | None | Ingress VIP address (`*.apps.<clusterName>.<baseDomain>`). Only required when `controlPlaneAgents > 1`. | `1-AgentClusterInstall.yaml` |
| `controlPlaneAgents` | Integer | `3` | Number of control plane / master nodes. Set to `1` for single-node OpenShift (enables `userManagedNetworking`, skips VIPs). | `1-AgentClusterInstall.yaml` |
| `workerAgents` | Integer | `(host count) - controlPlaneAgents` | Number of worker nodes. Auto-calculated from the length of the `hosts` list minus `controlPlaneAgents` if not set. | `1-AgentClusterInstall.yaml` |
| `mastersSchedulable` | Boolean | `false` | Whether workloads can be scheduled on control plane nodes. Only applies when `controlPlaneAgents > 1`. | `1-AgentClusterInstall.yaml` |
| `sshPublicKey` | String | `""` | SSH public key installed on hosts via ignition for remote access | `1-AgentClusterInstall.yaml`, `1-InfraEnv.yaml` |
| `pullSecretRef` | String | `default-pull-secret` | Name of the existing secret (in `secretSourceNamespace`) containing the OpenShift pull secret to replicate | `0-SecretReplicationPolicy.yaml` |
| `bmcCredentialRef` | String | `default-bmc-cred` | Name of the existing secret to copy BMC credentials from. Overridden by per-host `bmcCredentialRef` if set. | `0-SecretReplicationPolicy.yaml` |
| `bmcEndpoint` | URL Path String | None | Global default BMC API endpoint path (e.g., `/redfish/v1/Systems/1`). Overridden by per-host `bmcEndpoint`. | `1-BareMetalHost.yaml` |
| `managedClusterSet` | String | None | Name of the RHACM cluster set to add the cluster to. **Note: defined in cluster values but not currently referenced by any template.** | *(unused)* |
| `ntpSources` | List of IPv4 | None | NTP server IP addresses for host time synchronization | `1-InfraEnv.yaml` |
| `hosts` | List of Strings | None | List of hostnames (keys from `all_hosts` in `allHosts.yaml`) that this cluster will be installed on | `0-SecretReplicationPolicy.yaml`, `1-AgentClusterInstall.yaml`, `1-BareMetalHost.yaml`, `1-networkConfigs.yaml` |
| `useBond` | Boolean | `true` | Whether to configure network bonding (active-backup bond0) on hosts | `1-networkConfigs.yaml` |
| `useDHCP` | Boolean | `false` | Whether to enable DHCP on the VLAN interface (within the bond configuration) | `1-networkConfigs.yaml` |

### Networking Values

Defined under the `networking` key in per-cluster values files.

| Variable | Type | Default | Description | Used By Templates |
|----------|------|---------|-------------|-------------------|
| `networking.vlan` | Integer | `0` | VLAN ID for the cluster's primary machine network interfaces. Used to create `bond0.<vlan>` interface. | `1-networkConfigs.yaml` |
| `networking.machinePrefixLength` | Integer | None | Subnet prefix length for host IPs on the machine network (e.g., `25` for /25) | `1-networkConfigs.yaml` |
| `networking.defaultGateway` | IPv4 String | None | Default gateway IP for all cluster nodes | `1-networkConfigs.yaml` |
| `networking.clusterNetwork.cidr` | IPv4 CIDR | None | CIDR for the internal cluster (pod) network (e.g., `10.128.0.0/14`) | `1-AgentClusterInstall.yaml` |
| `networking.clusterNetwork.hostPrefix` | Integer | None | Subnet prefix for individual host allocations within the cluster network (e.g., `23`) | `1-AgentClusterInstall.yaml` |
| `networking.machineNetwork.cidr` | IPv4 CIDR | None | CIDR for the physical network underlay that hosts communicate on (e.g., `10.84.41.0/25`) | `1-AgentClusterInstall.yaml` |
| `networking.serviceNetwork` | List of IPv4 CIDR | None | List of CIDRs for Kubernetes service networks (e.g., `[172.30.0.0/16]`) | `1-AgentClusterInstall.yaml` |
| `networking.dnsServers` | List of IPv4 | None | DNS server IPs that hosts will be configured to use | `1-networkConfigs.yaml` |

### Global Values

Defined in `globalValues.yaml`. Shared across all cluster deployments.

| Variable | Type | Default | Description | Used By Templates |
|----------|------|---------|-------------|-------------------|
| `policyNamespace` | String | `policies-cluster-installs` | Namespace where RHACM policies are created. **Required** — template fails if empty. | `0-Namespace.yaml`, `0-SecretReplicationPolicy.yaml`, `0-managedClusterSetBinding.yaml` |
| `secretSourceNamespace` | String | `cluster-installs` | Namespace to source secrets from for replication via RHACM policy. **Required** — template fails if empty. | `0-SecretReplicationPolicy.yaml` |
| `clusterSet` | String | `hubofhubs` | RHACM cluster set representing the hub cluster. Used for ManagedClusterSetBinding and policy placement. | `0-managedClusterSetBinding.yaml`, `0-SecretReplicationPolicy.yaml` |
| `recreateHosts` | String (`"true"`/`"false"`) | `"true"` | When `"true"`, adds `argocd.argoproj.io/sync-options: Replace=true` to BareMetalHost resources, forcing recreation on changes. | `1-BareMetalHost.yaml` |
| `caTrustBundle` | Multi-line String | `""` | Additional CA certificates to trust, injected into the InfraEnv for hosts during discovery | `1-InfraEnv.yaml` |

### Per-Host Values

Defined in `allHosts.yaml` under the `all_hosts` dictionary. Each key is a hostname referenced from the cluster `hosts` list.

| Variable | Type | Default | Description | Used By Templates |
|----------|------|---------|-------------|-------------------|
| `all_hosts.<hostname>.bmcIP` | IPv4 String | None | IP address of the BMC/IPMI interface | `1-BareMetalHost.yaml` |
| `all_hosts.<hostname>.bmcPrefix` | String | None | Protocol prefix for BMC address: `redfish-virtualmedia` or `idrac-virtualmedia` | `1-BareMetalHost.yaml` |
| `all_hosts.<hostname>.bmcEndpoint` | URL Path String | Falls back to global `bmcEndpoint` | BMC API endpoint path (e.g., `/redfish/v1/Systems/1`). Overrides the cluster-level `bmcEndpoint`. | `1-BareMetalHost.yaml` |
| `all_hosts.<hostname>.bmcCredentialRef` | Dict | None | Optional per-host override for BMC credential secret reference | `0-SecretReplicationPolicy.yaml` |
| `all_hosts.<hostname>.bmcCredentialRef.name` | String | Falls back to cluster `bmcCredentialRef` | Name of the secret containing BMC credentials for this host | `0-SecretReplicationPolicy.yaml` |
| `all_hosts.<hostname>.bmcCredentialRef.namespace` | String | Falls back to `secretSourceNamespace` | Namespace of the BMC credential secret for this host | `0-SecretReplicationPolicy.yaml` |
| `all_hosts.<hostname>.bootMACAddress` | MAC String | None | MAC address the host boots from during installation | `1-BareMetalHost.yaml` |
| `all_hosts.<hostname>.interfaces` | List | None | List of network interfaces for this host | `1-networkConfigs.yaml` |
| `all_hosts.<hostname>.interfaces[].macAddress` | MAC String | Random MAC generated | MAC address of the interface. If empty, a random MAC is generated at template time. | `1-networkConfigs.yaml` |
| `all_hosts.<hostname>.interfaces[].name` | String | `eno<index+1>` (e.g., `eno1`, `eno2`) | Name to assign to the interface. Auto-generated from index if not specified. | `1-networkConfigs.yaml` |
| `all_hosts.<hostname>.interfaces[].disabled` | Boolean | `false` | If `true`, excludes this interface from the bond port list | `1-networkConfigs.yaml` |
| `all_hosts.<hostname>.primaryMac` | MAC String | First interface's `macAddress` | Primary MAC address for the host, used as the bond MAC. Defaults to the first interface's MAC. | `1-networkConfigs.yaml` |
| `all_hosts.<hostname>.IP` | IPv4 String | None | Routable IP address for this node on the machine network (not OOB/BMC) | `1-networkConfigs.yaml` |

---

## Chart: cluster-install-bootstrap

Defined in `helm-charts/cluster-install-bootstrap/values.yaml`. Used to create an ArgoCD Application that deploys the cluster-install chart.

| Variable | Type | Default | Description | Used By Templates |
|----------|------|---------|-------------|-------------------|
| `gitopsNamespace` | String | `openshift-gitops` | Namespace where the ArgoCD Application resource is created | `clusterApplication.yaml` |
| `repoUrl` | String | None | Git repository URL containing the cluster-install chart | `clusterApplication.yaml` |
| `targetRevision` | String | `main` | Git branch or revision to track | `clusterApplication.yaml` |
| `argoProject` | String | `default` | ArgoCD project the Application belongs to | `clusterApplication.yaml` |

---

## Values File Loading Order

When deployed via the bootstrap chart, ArgoCD loads values files in this order (last wins for conflicts):

1. `allHosts.yaml` — host inventory
2. `globalValues.yaml` — shared configuration
3. `<release-name>.yaml` — per-cluster configuration (e.g., `my-cluster.yaml`)
