# Provisioning Baremetal Clusters with AutoShift

This guide covers provisioning baremetal OpenShift clusters using AutoShift's cluster-install policies, ACM Assisted Installer, and the SiteConfig operator.

## Overview

AutoShift provisions baremetal clusters through three ACM policies that chain together:

1. **policy-cluster-install-prereqs** - Creates the cluster namespace, ClusterImageSet, and KlusterletAddonConfig
2. **policy-cluster-install-secrets** - Copies BMC credentials and pull secrets into the cluster namespace
3. **policy-cluster-install-siteconfig** - Creates all SiteConfig resources (ConfigMaps + ClusterInstance) that drive the Assisted Installer

Each policy depends on the previous one being Compliant before it runs. If source secrets don't exist, the secrets policy stays NonCompliant and no cluster is deployed.

## Architecture

```
values files                    ACM Policies (hub templates)
     |                                |
     v                                v
cluster-config-maps policy    cluster-install policies
     |                                |
     v                                v
raw ConfigMaps ----merge----> rendered-config ConfigMaps
                                      |
                                      v
                              prereqs -> secrets -> siteconfig
                                                      |
                                                      v
                                              ClusterInstance
                                                      |
                                                      v
                                        AgentClusterInstall (+ mirrorRegistryRef),
                                        ClusterDeployment, InfraEnv (+ CA),
                                        ManagedCluster, BareMetalHosts (+ rootDeviceHints),
                                        NMStateConfigs, mirror-registry-config
```

Cluster configuration is defined in values files and stored as ConfigMaps on the hub. ACM policies read these ConfigMaps at runtime via hub templates, merge clusterset defaults with per-cluster overrides, and generate all provisioning resources. This means adding a new cluster only requires adding a values file - no Helm re-rendering or ArgoCD sync needed.

## Prerequisites

- A hub cluster running AutoShift with ACM
- The `cluster-install: 'true'` label on the hub clusterset (enables SiteConfig component on MCH)
- The `acm-enable-provisioning: 'true'` label on the hub clusterset (enables provisioning infrastructure)
- Source secrets pre-created (see [Create Source Secrets](#step-2-create-source-secrets))

## Configuration Structure

Cluster provisioning config lives under the `config` key in cluster or clusterset values files. The config is split into four sections:

```yaml
clusters:
  my-cluster:
    config:
      networking:        # Reusable by other policies (e.g., nmstate)
        ...
      hosts:             # Reusable by other policies (e.g., nmstate)
        ...
      disconnected:      # Shared with disconnected-mirror policy
        ...
      clusterInstall:    # Install-specific settings
        ...
```

### networking

Network configuration shared across policies (cluster-install, nmstate). Defines SDN networks, interface topology, routes, and DNS.

```yaml
networking:
  clusterNetwork:
    cidr: 10.128.0.0/14
    hostPrefix: 23
  machineNetwork:
    cidr: '10.0.0.0/25'
  serviceNetwork:
    - 172.30.0.0/16
  # NMState interface topology — used by both siteconfig (NMStateConfig) and nmstate (NNCP)
  interfaces:
    eno1:
      type: ethernet
      name: eno1
      state: up
      ipv4: disabled
      ipv6: disabled
    eno2:
      type: ethernet
      name: eno2
      state: up
      ipv4: disabled
      ipv6: disabled
    mgmt:
      type: bond
      name: bond0
      mode: active-backup
      ports: [eno1, eno2]
      ipv4: disabled
      ipv6: disabled
    mgmt-vlan:
      type: vlan
      name: bond0.100
      id: 100
      base: bond0
      ipv4: static               # per-host IPs in hosts section
      ipv6: disabled
  routes:
    default:
      destination: 0.0.0.0/0
      gateway: '10.0.0.1'
      interface: bond0.100
  dns:
    servers: [10.0.0.53]
```

See [policies/nmstate/README.md](../policies/nmstate/README.md) for the full interface config reference.

### hosts

Per-host hardware and networking configuration. Each key is the short hostname (siteconfig constructs the FQDN as `{key}.{clusterName}.{baseDomain}`).

```yaml
hosts:
  master-0:
    role: master                           # 'master' (default) or 'worker'
    bmcIP: '192.168.1.10'
    bmcPrefix: 'redfish-virtualmedia'      # BMC protocol prefix
    bmcEndpoint: '/redfish/v1/Systems/1'   # optional, overrides cluster-level
    bootMACAddress: '00:00:00:00:00:01'
    primaryMac: '00:00:00:00:00:02'        # MAC for bond, defaults to first interface
    rootDeviceHints:                        # optional, disk selection for OS install
      deviceName: '/dev/sda'
    interfaces:                             # hardware interfaces for NMStateConfig
      - macAddress: '00:00:00:00:00:01'
        name: 'eno1'
      - macAddress: '00:00:00:00:00:02'
        name: 'eno2'
    networking:                             # per-host network overrides
      interfaces:
        mgmt-vlan:                          # references topology interface ID
          ipv4:
            addresses:
              - ip: 10.0.0.10
                prefixLength: 25
```

**role** — Required for the SiteConfig ClusterInstance. Defaults to `master`. Set to `worker` for dedicated worker nodes. The number of hosts with `role: master` must match `controlPlaneAgents`.

**rootDeviceHints** — Optional hints for the Metal3 BareMetalHost to select the installation disk. Supported hints: `deviceName`, `serialNumber`, `model`, `vendor`, `wwn`, `hctl`, `rotational`, `minSizeGigabytes`.


### disconnected

Disconnected mirror registry configuration. This single block drives both install-time config (mirrorRegistryRef on AgentClusterInstall, CA in InfraEnv, ClusterImageSet releaseImage) and post-install config (IDMS/ICSP, CatalogSources via the disconnected-mirror policy).

```yaml
disconnected:
  mirrorRegistry:
    host: 'mirror.example.com:5000'        # registry host:port
    path: 'ocp'                             # optional, image path prefix
    releaseImage: 'openshift/ocp-release'   # optional, defaults to openshift-release-dev/ocp-release
                                            # path depends on how oc-mirror stored the content
    ca: |                                   # CA bundle for the mirror registry
      -----BEGIN CERTIFICATE-----
      ...
    # caRef:                               # OR reference a hub ConfigMap
    #   name: 'cluster-ca-bundle'
    #   key: 'ca-bundle.crt'
    #   namespace: 'cluster-install-secrets'
    mirrors:                                # source → mirror path mappings
      - source: quay.io/openshift-release-dev/ocp-release
        mirror: openshift/release-images  # path in mirror registry (host/mirror)
      - source: quay.io/openshift-release-dev/ocp-v4.0-art-dev
        mirror: openshift/release
      - source: registry.redhat.io          # no mirror = host/path/source
      - source: quay.io
      - source: registry.access.redhat.com
  useIDMS: true                             # IDMS (OCP 4.13+) or ICSP (4.12-)
  disableDefaultCatalogs: true              # disable default OperatorHub catalogs
  catalogs:                                 # CatalogSource name = {source}-{mirror-catalog-suffix label}
    - source: redhat-operators
      imagePath: redhat/redhat-operator-index
      tag: v4.20
      publisher: Red Hat
    - source: certified-operators
      imagePath: redhat/certified-operator-index
      tag: v4.20
      publisher: Red Hat
  osImages:                                 # RHCOS images for hub AgentServiceConfig (disconnected only)
    - openshiftVersion: '4.20'              # Major.Minor
      version: '420.86.202301311551-0'      # RHCOS version string
      cpuArchitecture: x86_64
      url: 'https://mirror.example.com/rhcos/rhcos-live.x86_64.iso'
```

When `disconnected.mirrorRegistry` is configured:

- **ClusterImageSet** `releaseImage` points to the mirror registry instead of `quay.io` (the Assisted Installer does NOT use IDMS for pulling the release image)
- **mirror-registry-config ConfigMap** is created with `registries.conf` (TOML) and `ca-bundle.crt` in the cluster namespace
- **AgentClusterInstall** gets `mirrorRegistryRef` pointing to this ConfigMap
- **InfraEnv** gets `additionalTrustBundle` from the CA
- **disconnected-mirror policy** reads the same config for:
  - IDMS/ICSP — redirects image pulls from source registries to mirror
  - CatalogSources — mirrored operator catalogs
  - OperatorHub disable — disables default catalog sources
  - **Registry CA trust** — creates a ConfigMap in `openshift-config` with the CA and patches `image.config.openshift.io/cluster` so the managed cluster trusts the mirror registry post-install
- **ACM provisioning policy** reads the hub's disconnected config for:
  - `mirrorRegistryRef` on AgentServiceConfig — so the Assisted Installer trusts the mirror
  - `osImages` — custom live ISO and rootfs URLs for disconnected boot

**`osImages`** — For disconnected environments, the Assisted Installer can't download RHCOS images from `mirror.openshift.com`. Download them and host on a local HTTP server:

```bash
# Download RHCOS images for your OCP version
curl -O https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.20/latest/rhcos-live.x86_64.iso
curl -O https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.20/latest/rhcos-live-rootfs.x86_64.img

# Host on a local HTTP server accessible from the hub
cp rhcos-live.x86_64.iso /var/www/html/rhcos/
```

The RHCOS version string (for the `version` field) can be found in the ISO filename or via `openshift-install coreos print-stream-json`.

**Labels still required** for operator catalog source switching (OperatorPolicy can only read labels):

```yaml
labels:
  disconnected-mirror: 'true'        # placement + operator source ternary
  mirror-catalog-suffix: 'mirror'    # CatalogSource naming: {source}-{suffix}
```

### clusterInstall

Install-specific configuration. The `createCluster: 'true'` flag triggers provisioning.

```yaml
clusterInstall:
  createCluster: 'true'              # Required - triggers provisioning
  baseDomain: example.com
  openshiftVersion: '4.20.12'
  cpuArch: x86_64                    # default: x86_64
  openshiftChannel: stable           # ClusterImageSet channel label (default: stable)
  clusterImageSet: ''                # optional, overrides openshiftVersion+cpuArch
  controlPlaneAgents: 3              # 1 = SNO
  workerAgents: 0                    # default: (len hosts) - controlPlaneAgents
  apiVip: '10.0.0.1'                # required for multi-node
  ingressVip: '10.0.0.2'            # required for multi-node
  mastersSchedulable: false          # default: false
  pullSecretRef: 'default-pull-secret'
  bmcCredentialRef: 'default-bmc-cred'
  bmcEndpoint: '/redfish/v1/Systems/1'
  secretSourceNamespace: 'cluster-install-secrets'
  # SSH Public Key — provide inline OR reference a ConfigMap (not both)
  sshPublicKey: 'ssh-rsa ...'              # option 1: inline value
  # sshPublicKeyRef:                       # option 2: reference a hub ConfigMap
  #   name: 'cluster-ssh-keys'
  #   key: 'ssh-public-key'
  #   namespace: 'cluster-install-secrets' # optional, defaults to policy namespace
  ntpSources:                        # optional NTP servers
    - 10.0.0.1
  klusterletAddons:                  # optional override (defaults below)
    - applicationManager
    - certPolicyController
    - policyController
```

## Step-by-Step Guide

### Step 1: Enable Cluster Install on the Hub

Add the required labels to your hub clusterset values file:

```yaml
# autoshift/values/clustersets/hub.yaml
hubClusterSets:
  hub:
    labels:
      cluster-install: 'true'
      acm-enable-provisioning: 'true'
```

The `cluster-install` label:
- Enables the SiteConfig component on the MultiClusterHub
- Gates the cluster-install policy placement (policies only run on hubs with this label)

### Step 2: Create Source Secrets and ConfigMaps

The cluster-install policies look up secrets and ConfigMaps from a source namespace on the hub cluster. These must exist before provisioning.

#### Create the source namespace

```bash
oc create namespace cluster-install-secrets
```

#### Required: BMC credentials

One secret per unique BMC credential set. The `bmcCredentialRef` in cluster config references these by name. The secrets policy copies them into each cluster's namespace.

```bash
# Default BMC credential (referenced by clusterInstall.bmcCredentialRef)
oc create secret generic default-bmc-cred \
  -n cluster-install-secrets \
  --from-literal=username=<bmc-username> \
  --from-literal=password=<bmc-password>

# Per-host overrides (optional — referenced by hosts.<name>.bmcCredentialRef.name)
oc create secret generic custom-bmc-cred \
  -n cluster-install-secrets \
  --from-literal=username=<other-username> \
  --from-literal=password=<other-password>
```

#### Required: Pull secret

The pull secret for pulling OpenShift images. For disconnected environments, this must include auth for the mirror registry.

```bash
# From a file (recommended — download from console.redhat.com)
oc create secret generic default-pull-secret \
  -n cluster-install-secrets \
  --from-file=.dockerconfigjson=<path-to-pull-secret.json> \
  --type=kubernetes.io/dockerconfigjson

# Or inline (connected environments)
oc create secret docker-registry default-pull-secret \
  -n cluster-install-secrets \
  --docker-server=quay.io \
  --docker-username=<username> \
  --docker-password=<password>
```

#### Optional: SSH public key ConfigMap

Instead of embedding the SSH key inline in values, reference a ConfigMap. Useful when shared across clusters.

```bash
oc create configmap cluster-ssh-keys \
  -n cluster-install-secrets \
  --from-file=ssh-public-key=$HOME/.ssh/id_rsa.pub
```

Then reference in cluster config:

```yaml
clusterInstall:
  sshPublicKeyRef:
    name: 'cluster-ssh-keys'
    key: 'ssh-public-key'
    namespace: 'cluster-install-secrets'
```

#### Optional: CA trust bundle ConfigMap (disconnected)

For disconnected environments, the mirror registry CA bundle. Instead of embedding inline in `disconnected.mirrorRegistry.ca`, reference a ConfigMap.

```bash
oc create configmap cluster-ca-bundle \
  -n cluster-install-secrets \
  --from-file=ca-bundle.crt=/path/to/ca-bundle.crt
```

Then reference in cluster config:

```yaml
disconnected:
  mirrorRegistry:
    caRef:
      name: 'cluster-ca-bundle'
      key: 'ca-bundle.crt'
      namespace: 'cluster-install-secrets'
```

#### Quick setup: all resources at once

```bash
# Create namespace
oc create namespace cluster-install-secrets

# BMC credentials
oc create secret generic default-bmc-cred \
  -n cluster-install-secrets \
  --from-literal=username=admin \
  --from-literal=password=<bmc-password>

# Pull secret (from Red Hat console download)
oc create secret generic default-pull-secret \
  -n cluster-install-secrets \
  --from-file=.dockerconfigjson=~/pull-secret.json \
  --type=kubernetes.io/dockerconfigjson

# SSH key
oc create configmap cluster-ssh-keys \
  -n cluster-install-secrets \
  --from-file=ssh-public-key=$HOME/.ssh/id_rsa.pub

# CA bundle (disconnected only)
oc create configmap cluster-ca-bundle \
  -n cluster-install-secrets \
  --from-file=ca-bundle.crt=/path/to/ca-bundle.crt
```

#### Verify everything exists

```bash
oc get secret,configmap -n cluster-install-secrets
```

> **Note:** Per-host BMC credentials can override the default by setting `bmcCredentialRef` on individual hosts. The secrets policy will copy from the specified source.

### Step 3: Define the Cluster

Create a values file for your cluster under `autoshift/values/clusters/`:

```yaml
# autoshift/values/clusters/my-cluster.yaml
clusters:
  my-cluster:
    config:
      clusterSet: managed
      networking:
        clusterNetwork:
          cidr: 10.128.0.0/14
          hostPrefix: 23
        machineNetwork:
          cidr: '10.0.0.0/25'
        serviceNetwork:
          - 172.30.0.0/16
        interfaces:
          eno1:
            type: ethernet
            name: eno1
            state: up
            ipv4: disabled
            ipv6: disabled
          eno2:
            type: ethernet
            name: eno2
            state: up
            ipv4: disabled
            ipv6: disabled
          mgmt:
            type: bond
            name: bond0
            mode: active-backup
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
            gateway: '10.0.0.1'
            interface: bond0.100
        dns:
          servers: [10.0.0.53]
      hosts:
        master-0:
          role: master
          bmcIP: '192.168.1.10'
          bmcPrefix: 'redfish-virtualmedia'
          bootMACAddress: 'aa:bb:cc:dd:ee:01'
          primaryMac: 'aa:bb:cc:dd:ee:02'
          rootDeviceHints:
            deviceName: '/dev/sda'
          interfaces:
            - macAddress: 'aa:bb:cc:dd:ee:01'
              name: 'eno1'
            - macAddress: 'aa:bb:cc:dd:ee:02'
              name: 'eno2'
          networking:
            interfaces:
              mgmt-vlan:
                ipv4:
                  addresses:
                    - ip: 10.0.0.10
                      prefixLength: 25
        master-1:
          role: master
          bmcIP: '192.168.1.11'
          bmcPrefix: 'redfish-virtualmedia'
          bootMACAddress: 'aa:bb:cc:dd:ee:11'
          primaryMac: 'aa:bb:cc:dd:ee:12'
          rootDeviceHints:
            deviceName: '/dev/sda'
          interfaces:
            - macAddress: 'aa:bb:cc:dd:ee:11'
              name: 'eno1'
            - macAddress: 'aa:bb:cc:dd:ee:12'
              name: 'eno2'
          networking:
            interfaces:
              mgmt-vlan:
                ipv4:
                  addresses:
                    - ip: 10.0.0.11
                      prefixLength: 25
        master-2:
          role: master
          bmcIP: '192.168.1.12'
          bmcPrefix: 'redfish-virtualmedia'
          bootMACAddress: 'aa:bb:cc:dd:ee:21'
          primaryMac: 'aa:bb:cc:dd:ee:22'
          rootDeviceHints:
            deviceName: '/dev/sda'
          interfaces:
            - macAddress: 'aa:bb:cc:dd:ee:21'
              name: 'eno1'
            - macAddress: 'aa:bb:cc:dd:ee:22'
              name: 'eno2'
          networking:
            interfaces:
              mgmt-vlan:
                ipv4:
                  addresses:
                    - ip: 10.0.0.12
                      prefixLength: 25
      clusterInstall:
        createCluster: 'true'
        baseDomain: example.com
        openshiftVersion: '4.20.12'
        controlPlaneAgents: 3
        apiVip: '10.0.0.2'
        ingressVip: '10.0.0.3'
        sshPublicKey: 'ssh-rsa AAAAB3...'
        pullSecretRef: 'default-pull-secret'
        bmcCredentialRef: 'default-bmc-cred'
        secretSourceNamespace: 'cluster-install-secrets'
```

### Step 4: Add the Values File to ArgoCD

Add your cluster values file to the AutoShift ArgoCD Application:

```yaml
spec:
  source:
    helm:
      valueFiles:
        - values/global.yaml
        - values/clustersets/hub.yaml
        - values/clusters/my-cluster.yaml
```

After ArgoCD syncs, the cluster-config-maps policy will create raw and rendered-config ConfigMaps, and the cluster-install policies will begin provisioning.

### Step 5: Monitor Provisioning

Check the policy chain:

```bash
# All three should be Compliant for provisioning to proceed
oc get policies -A | grep cluster-install
```

Check the created resources:

```bash
# Namespace and prereqs
oc get ns my-cluster
oc get clusterimageset | grep my-cluster

# Secrets (BMC creds + pull secret)
oc get secrets -n my-cluster

# SiteConfig resources
oc get configmaps -n my-cluster
oc get clusterinstance -n my-cluster

# Provisioning sub-resources
oc get agentclusterinstall -n my-cluster
oc get clusterdeployment -n my-cluster
oc get infraenv -n my-cluster
oc get baremetalhost -n my-cluster
oc get nmstateconfig -n my-cluster
oc get managedcluster my-cluster
```

Monitor the installation progress:

```bash
oc get agentclusterinstall -n my-cluster -w
```

## Clusterset Defaults

Common settings can be defined at the clusterset level and inherited by all clusters. Per-cluster values override clusterset defaults.

```yaml
# autoshift/values/clustersets/hub.yaml
hubClusterSets:
  hub:
    config:
      clusterInstall:
        secretSourceNamespace: 'cluster-install-secrets'
        bmcCredentialRef: 'default-bmc-cred'
        bmcEndpoint: '/redfish/v1/Systems/1'
        pullSecretRef: 'default-pull-secret'
```

Per-cluster values only need to specify what differs:

```yaml
clusters:
  my-cluster:
    config:
      clusterInstall:
        createCluster: 'true'
        baseDomain: example.com
        openshiftVersion: '4.20.12'
        # inherits secretSourceNamespace, bmcCredentialRef, etc. from clusterset
```

## SSH Key and CA Bundle from ConfigMaps

Instead of embedding SSH public keys and CA trust bundles inline in values files, you can reference a ConfigMap on the hub cluster. This is useful when the same key or bundle is shared across clusters or managed by a different team.

Create the ConfigMaps:

```bash
oc create configmap cluster-ssh-keys \
  -n cluster-install-secrets \
  --from-file=ssh-public-key=$HOME/.ssh/id_rsa.pub

oc create configmap cluster-ca-bundle \
  -n cluster-install-secrets \
  --from-file=ca-bundle.crt=/path/to/ca-bundle.crt
```

Reference them in your cluster config:

```yaml
clusterInstall:
  sshPublicKeyRef:
    name: 'cluster-ssh-keys'
    key: 'ssh-public-key'
    namespace: 'cluster-install-secrets'   # optional, defaults to policy namespace

disconnected:
  mirrorRegistry:
    caRef:
      name: 'cluster-ca-bundle'
      key: 'ca-bundle.crt'
      namespace: 'cluster-install-secrets' # optional, defaults to policy namespace
```

The refs are resolved at runtime by ACM policies via hub template `lookup`. If the referenced ConfigMap does not exist, the policy falls back to the inline value. If neither is set, the field is empty.

This is a good candidate for clusterset defaults — define the refs once and all clusters inherit them:

```yaml
hubClusterSets:
  hub:
    config:
      clusterInstall:
        sshPublicKeyRef:
          name: 'cluster-ssh-keys'
          key: 'ssh-public-key'
          namespace: 'cluster-install-secrets'
```

## Hub-of-Hubs

The cluster-install policies support hub-of-hubs deployments. The placement uses the `autoshift.io/cluster-install: 'true'` label, so policies propagate to any hub cluster with that label - not just the self-managed hub.

Each spoke hub:
- Evaluates the policies against its own rendered-config ConfigMaps
- Uses its own source secrets namespace
- Provisions clusters independently

To enable on a spoke hub clusterset:

```yaml
# In the hub-of-hubs values
hub1:
  labels:
    cluster-install: 'true'
    acm-enable-provisioning: 'true'
```

## SNO (Single Node OpenShift)

For single-node clusters, set `controlPlaneAgents: 1` and define one host:

```yaml
clusters:
  my-sno:
    config:
      networking:
        clusterNetwork:
          cidr: 10.128.0.0/14
          hostPrefix: 23
        machineNetwork:
          cidr: '10.0.0.0/25'
        serviceNetwork:
          - 172.30.0.0/16
        interfaces:
          mgmt:
            type: ethernet
            name: eno1
            ipv4: dhcp
            ipv6: disabled
      hosts:
        master-0:
          bmcIP: '192.168.1.10'
          bmcPrefix: 'redfish-virtualmedia'
          bootMACAddress: 'aa:bb:cc:dd:ee:01'
          interfaces:
            - macAddress: 'aa:bb:cc:dd:ee:01'
              name: 'eno1'
      clusterInstall:
        createCluster: 'true'
        baseDomain: example.com
        openshiftVersion: '4.20.12'
        controlPlaneAgents: 1
        sshPublicKey: 'ssh-rsa ...'
```

SNO clusters automatically get `userManagedNetworking: true` and do not require `apiVip`/`ingressVip`.

## Dependency Chain and Safety

The policy dependency chain prevents partial deployments:

```
prereqs (Compliant) --> secrets (Compliant) --> siteconfig
```

- If source secrets don't exist, the secrets policy stays **NonCompliant** and siteconfig never runs
- If the rendered-config ConfigMap doesn't exist yet (new cluster, no ManagedCluster object), the cluster-config-maps policy handles this via its second loop that checks for `createCluster: 'true'`
- Setting `createCluster` to anything other than `'true'` (or removing it) stops provisioning for that cluster

## Validation

AutoShift validates cluster-install configuration at Helm render time via `_validate-cluster-install.tpl`. This catches config errors before they reach ACM. Validated fields include:

- **Required fields**: `baseDomain`, `openshiftVersion` (or `clusterImageSet`), `sshPublicKey` (or ref), `pullSecretRef`, `bmcCredentialRef`
- **Multi-node**: `apiVip` and `ingressVip` required when `controlPlaneAgents > 1`
- **Host counts**: Number of hosts must match `controlPlaneAgents` + `workerAgents`
- **Role counts**: Number of hosts with `role: master` must match `controlPlaneAgents`
- **SNO**: Exactly 1 host when `controlPlaneAgents: 1`
- **Disconnected**: `url` required when `sources` defined, `ca` or `caRef` required when `sources` defined, `url` required when `catalogs` defined
- **Catalog entries**: `source`, `imagePath`, `tag` required for each catalog
- **OS images**: `openshiftVersion`, `url`, `rootFSUrl` required for each osImage entry
- **rootDeviceHints**: Only valid hint keys accepted
- **Networking**: Interface types, modes, VLAN base references, static IP addresses validated

Test your config locally before deploying:

```bash
helm template autoshift/ -f autoshift/values/clusters/my-cluster.yaml
```

## Troubleshooting

### Policies stuck at Pending

Check the dependency chain - a downstream policy stays Pending until its dependency is Compliant:

```bash
oc describe configurationpolicy policy-cluster-install-secrets -n local-cluster
```

### Secrets policy NonCompliant

The source secrets don't exist. Check they're in the right namespace:

```bash
oc get secrets -n cluster-install-secrets
```

### ClusterInstance not created

Check the siteconfig policy for template errors:

```bash
oc describe configurationpolicy policy-cluster-install-siteconfig -n local-cluster
```

### BareMetalHosts stuck in registering

The BMC is unreachable. Verify BMC IP, credentials, and network connectivity from the hub cluster.
