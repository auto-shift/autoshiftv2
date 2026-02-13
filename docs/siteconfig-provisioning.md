# ClusterInstance Provisioning (Day 0/1)

AutoShift v2 supports Day 0/1 cluster provisioning using the ClusterInstance API (OCP 4.20+ / ACM 2.15+). This enables provisioning bare-metal clusters from the hub cluster using the same label-driven policy approach as Day 2 governance.

## Architecture

```
AutoShift Values File (e.g., values.hub.ran-du.yaml)
  clusters:
    my-sno-site:
      labels:
        cluster-instance: 'true'        <-- Enables provisioning
        cluster-instance-base-domain: example.com
        cluster-instance-node-1-hostname: ...
        sriov: 'true'                   <-- Day 2 labels
        ptp: 'true'

Hub Cluster
  policies/cluster-instance/
    --> Reads clusters from values file
    --> Creates Policy targeting hub
    --> ConfigurationPolicy creates Namespace + ClusterInstance CR
    --> ACM SiteConfig operator provisions the cluster
    --> Day 2 labels activate governance policies automatically
```

## Prerequisites

1. **ACM 2.15+** with SiteConfig operator installed
2. **ClusterImageSet** created for your target OCP version
3. **Secrets** created in the cluster namespace on the hub (see below)

## Quick Start

### 1. Create Required Secrets

Before enabling `cluster-instance: 'true'`, create these secrets on the hub cluster:

```bash
# Create the cluster namespace
oc create namespace my-sno-site

# Pull secret
oc create secret generic pull-secret -n my-sno-site \
  --from-file=.dockerconfigjson=pull-secret.json \
  --type=kubernetes.io/dockerconfigjson

# SSH public key
oc create secret generic ssh-key -n my-sno-site \
  --from-literal=ssh-publickey='ssh-rsa AAAA...'

# BMC credentials (one per node)
oc create secret generic my-sno-site-bmc-secret -n my-sno-site \
  --from-literal=username=admin \
  --from-literal=password=password
```

Alternatively, use RHACM Credentials, Sealed Secrets, or Vault/ESO for secret management.

### 2. Add Cluster to Values File

In your AutoShift values file (e.g., `values.hub.ran-du.yaml`), add a cluster entry:

```yaml
clusters:
  my-sno-site:
    labels:
      ### Enable ClusterInstance provisioning
      cluster-instance: 'true'
      cluster-instance-base-domain: example.com
      cluster-instance-image-set: openshift-v4.18.28
      cluster-instance-pull-secret: pull-secret
      cluster-instance-ssh-secret: ssh-key
      cluster-instance-clusterset: ran-du
      # Network (address and prefix are separate -- no / allowed in label values)
      cluster-instance-machine-network: '10.0.0.0'
      cluster-instance-machine-network-prefix: '24'
      ### Node 1
      cluster-instance-node-1-hostname: my-sno-site.example.com
      cluster-instance-node-1-role: master
      cluster-instance-node-1-bmc-type: idrac-virtualmedia
      cluster-instance-node-1-bmc-host: '10.0.0.1'
      cluster-instance-node-1-bmc-system: System.Embedded.1
      cluster-instance-node-1-bmc-secret: my-sno-site-bmc-secret
      cluster-instance-node-1-boot-mac: 'AA.BB.CC.DD.EE.FF'
      # cluster-instance-node-1-root-device: sda
      ### Day 2 labels (activated after cluster is provisioned)
      sriov: 'true'
      ptp: 'true'
      sctp: 'true'
      performance-profile: 'true'
      workload-partitioning: 'true'
```

### 3. Commit and Push

```bash
git add autoshift/values.hub.ran-du.yaml
git commit -m "Add my-sno-site cluster provisioning"
git push
```

ArgoCD syncs the policy, which creates the ClusterInstance CR on the hub, triggering provisioning.

## Labels Reference

IMPORTANT: All label values must be k8s-label-safe (alphanumeric, dots, hyphens, underscores only -- no slashes or colons). The template reconstructs full values (CIDRs, BMC addresses, MAC addresses, device paths) from label-safe components.

### Required Labels

| Label | Description |
|-------|-------------|
| `cluster-instance` | `'true'` to enable provisioning |
| `cluster-instance-base-domain` | Base domain (e.g., `example.com`) |
| `cluster-instance-image-set` | ClusterImageSet name (e.g., `openshift-v4.18.28`) |
| `cluster-instance-node-{N}-hostname` | Node FQDN |
| `cluster-instance-node-{N}-bmc-host` | BMC IP address or hostname |
| `cluster-instance-node-{N}-bmc-secret` | Name of BMC credentials Secret |
| `cluster-instance-node-{N}-boot-mac` | Boot MAC address (dots, e.g., `AA.BB.CC.DD.EE.FF`) |

### Optional Labels (with defaults)

| Label | Default | Description |
|-------|---------|-------------|
| `cluster-instance-pull-secret` | `pull-secret` | Pull secret name in cluster namespace |
| `cluster-instance-ssh-secret` | `ssh-key` | SSH key secret name in cluster namespace |
| `cluster-instance-clusterset` | (none) | ManagedClusterSet to join |
| `cluster-instance-cluster-network` | `10.128.0.0` | Pod network address |
| `cluster-instance-cluster-network-prefix` | `14` | Pod network CIDR prefix |
| `cluster-instance-cluster-network-host-prefix` | `23` | Per-node subnet prefix |
| `cluster-instance-service-network` | `172.30.0.0` | Service network address |
| `cluster-instance-service-network-prefix` | `16` | Service network CIDR prefix |
| `cluster-instance-machine-network` | `10.0.0.0` | Machine/BMC network address |
| `cluster-instance-machine-network-prefix` | `24` | Machine network CIDR prefix |
| `cluster-instance-node-{N}-role` | `master` | Node role (`master` or `worker`) |
| `cluster-instance-node-{N}-bmc-type` | `redfish-virtualmedia` | BMC type prefix |
| `cluster-instance-node-{N}-bmc-system` | `System.Embedded.1` | Redfish system ID |
| `cluster-instance-node-{N}-root-device` | (none) | Root device without `/dev/` prefix (e.g., `sda`) |

### BMC Address Construction

The BMC address is constructed from label-safe components:

```
{bmc-type}+https://{bmc-host}/redfish/v1/Systems/{bmc-system}
```

Common `bmc-type` values:

| Type | Description |
|------|-------------|
| `redfish-virtualmedia` | HPE iLO, generic Redfish with virtual media |
| `idrac-virtualmedia` | Dell iDRAC with virtual media |
| `redfish` | Standard Redfish without virtual media |

Common `bmc-system` values:

| System ID | Description |
|-----------|-------------|
| `System.Embedded.1` | Dell iDRAC (default) |
| `1` | HPE iLO, Supermicro, generic Redfish |

### Network CIDR Construction

Network CIDRs are split into address and prefix labels (no `/` allowed in label values). The template reconstructs the CIDR:

```
{address}/{prefix}
```

For example, `cluster-instance-machine-network: '10.0.0.0'` + `cluster-instance-machine-network-prefix: '24'` becomes `10.0.0.0/24`.

### Root Device Construction

The root device label contains just the device name without the `/dev/` prefix. The template prepends `/dev/`:

```
cluster-instance-node-1-root-device: sda  -->  /dev/sda
```

### Node Indexing

Nodes are indexed 1-10 using `{N}` in label names. For SNO, use `node-1`. For compact (3 masters), use `node-1` through `node-3`. For standard (3 master + 2 worker), use `node-1` through `node-5` with appropriate roles.

### SSH Key Secret

The SSH public key is read from a Secret on the hub cluster using ACM hub templates (`fromSecret`). The Secret must:

- Exist in the cluster namespace (same name as cluster)
- Have a data key named `ssh-publickey`
- Be created before enabling `cluster-instance: 'true'`

```bash
oc create secret generic ssh-key -n my-sno-site \
  --from-literal=ssh-publickey='ssh-rsa AAAA...'
```

## Day 2 Integration

All non-`cluster-instance-*` labels in the cluster entry are passed as `clusterLabels` in the ClusterInstance spec with the `autoshift.io/` prefix. When ACM creates the ManagedCluster:

1. ACM applies `clusterLabels` to the `ManagedCluster` object
2. AutoShift's Placement rules match the `autoshift.io/*` labels
3. Day 2 policies are automatically applied to the new cluster
4. Full Day 0 --> Day 2 lifecycle is automated

```yaml
# In values file:
clusters:
  my-site:
    labels:
      cluster-instance: 'true'    # --> provisioning config (not a clusterLabel)
      sriov: 'true'               # --> autoshift.io/sriov: 'true' on ManagedCluster
      ptp: 'true'                 # --> autoshift.io/ptp: 'true' on ManagedCluster
```

## Example: SNO RAN-DU Site

```yaml
clusters:
  edge-site-01:
    labels:
      cluster-instance: 'true'
      cluster-instance-base-domain: ran.example.com
      cluster-instance-image-set: openshift-v4.18.28
      cluster-instance-clusterset: ran-du
      cluster-instance-machine-network: '172.20.10.0'
      cluster-instance-machine-network-prefix: '24'
      cluster-instance-node-1-hostname: edge-site-01.ran.example.com
      cluster-instance-node-1-role: master
      cluster-instance-node-1-bmc-type: idrac-virtualmedia
      cluster-instance-node-1-bmc-host: '172.20.10.5'
      cluster-instance-node-1-bmc-system: System.Embedded.1
      cluster-instance-node-1-bmc-secret: edge-site-01-bmc
      cluster-instance-node-1-boot-mac: 'B4.96.91.1A.2C.30'
      # cluster-instance-node-1-root-device: sda
      # Day 2: full RAN-DU policy stack
      sriov: 'true'
      ptp: 'true'
      sctp: 'true'
      performance-profile: 'true'
      workload-partitioning: 'true'
      reduce-monitoring: 'true'
      disable-network-diagnostics: 'true'
      node-feature-discovery: 'true'
      nmstate: 'true'
      lvm: 'true'
```

## Example: 3-Node Compact

```yaml
clusters:
  dc-compact-01:
    labels:
      cluster-instance: 'true'
      cluster-instance-base-domain: dc.example.com
      cluster-instance-image-set: openshift-v4.18.28
      cluster-instance-clusterset: managed
      cluster-instance-node-1-hostname: master-0.dc-compact-01.dc.example.com
      cluster-instance-node-1-role: master
      cluster-instance-node-1-bmc-type: redfish-virtualmedia
      cluster-instance-node-1-bmc-host: '10.1.0.10'
      cluster-instance-node-1-bmc-system: '1'
      cluster-instance-node-1-bmc-secret: dc-compact-01-bmc-0
      cluster-instance-node-1-boot-mac: 'AA.BB.CC.00.11.22'
      cluster-instance-node-2-hostname: master-1.dc-compact-01.dc.example.com
      cluster-instance-node-2-role: master
      cluster-instance-node-2-bmc-type: redfish-virtualmedia
      cluster-instance-node-2-bmc-host: '10.1.0.11'
      cluster-instance-node-2-bmc-system: '1'
      cluster-instance-node-2-bmc-secret: dc-compact-01-bmc-1
      cluster-instance-node-2-boot-mac: 'AA.BB.CC.00.11.33'
      cluster-instance-node-3-hostname: master-2.dc-compact-01.dc.example.com
      cluster-instance-node-3-role: master
      cluster-instance-node-3-bmc-type: redfish-virtualmedia
      cluster-instance-node-3-bmc-host: '10.1.0.12'
      cluster-instance-node-3-bmc-system: '1'
      cluster-instance-node-3-bmc-secret: dc-compact-01-bmc-2
      cluster-instance-node-3-boot-mac: 'AA.BB.CC.00.11.44'
      # Day 2
      odf: 'true'
      compliance: 'true'
      acs: 'true'
```

## Secrets Management

ClusterInstance CRs **reference** secrets by name. Supported approaches:

| Method | Description |
|--------|-------------|
| Manual | `oc create secret` before enabling cluster-instance |
| RHACM Credentials | Use ACM UI to create credentials |
| Sealed Secrets | Encrypt secrets in Git |
| Vault / ESO | External Secrets Operator with HashiCorp Vault |

## Troubleshooting

```bash
# Check policy status
oc get policy -n <policy-namespace> policy-cluster-instance

# Check ConfigurationPolicy compliance
oc get configurationpolicy -A | grep cluster-instance

# Check ClusterInstance status
oc get clusterinstance -A

# Check provisioning progress
oc get agentclusterinstall -A
oc get baremetalhost -A

# Check if secrets exist in cluster namespace
oc get secret -n <cluster-name>

# Verify the policy renders correctly
helm template policies/cluster-instance/ -f autoshift/values.hub.ran-du.yaml
```
