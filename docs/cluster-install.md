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
                                        AgentClusterInstall, ClusterDeployment,
                                        InfraEnv, ManagedCluster, BareMetalHosts,
                                        NMStateConfigs
```

Cluster configuration is defined in values files and stored as ConfigMaps on the hub. ACM policies read these ConfigMaps at runtime via hub templates, merge clusterset defaults with per-cluster overrides, and generate all provisioning resources. This means adding a new cluster only requires adding a values file - no Helm re-rendering or ArgoCD sync needed.

## Prerequisites

- A hub cluster running AutoShift with ACM
- The `cluster-install: 'true'` label on the hub clusterset (enables SiteConfig component on MCH)
- The `acm-enable-provisioning: 'true'` label on the hub clusterset (enables provisioning infrastructure)
- Source secrets pre-created (see [Create Source Secrets](#step-2-create-source-secrets))

## Configuration Structure

Cluster provisioning config lives under the `config` key in cluster or clusterset values files. The config is split into three sections:

```yaml
clusters:
  my-cluster:
    config:
      networking:        # Reusable by other policies (e.g., nmstate)
        ...
      hosts:             # Reusable by other policies (e.g., nmstate)
        ...
      clusterInstall:    # Install-specific settings
        ...
```

### networking

Network configuration shared across policies. Defines cluster, machine, and service networks plus DNS and VLAN settings.

```yaml
networking:
  vlan: 100
  machinePrefixLength: 25
  defaultGateway: '10.0.0.1'
  dnsServers:
    - 10.0.0.53
  clusterNetwork:
    cidr: 10.128.0.0/14
    hostPrefix: 23
  machineNetwork:
    cidr: '10.0.0.0/25'
  serviceNetwork:
    - 172.30.0.0/16
```

### hosts

Per-host hardware configuration. Each key is the hostname.

```yaml
hosts:
  master-0:
    bmcIP: '192.168.1.10'
    bmcPrefix: 'redfish-virtualmedia'     # BMC protocol prefix
    bmcEndpoint: '/redfish/v1/Systems/1'  # optional, overrides cluster-level
    bmcCredentialRef:                      # optional, overrides cluster-level
      name: 'custom-cred'
      namespace: 'custom-ns'
    bootMACAddress: '00:00:00:00:00:01'
    IP: '10.0.0.10'
    primaryMac: '00:00:00:00:00:02'       # MAC for bond, defaults to first interface
    interfaces:
      - macAddress: '00:00:00:00:00:01'
        name: 'eno1'                      # optional, auto-generated if omitted
      - macAddress: '00:00:00:00:00:02'
        name: 'eno2'
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
  # SSH Public Key — provide inline OR reference a ConfigMap (not both)
  sshPublicKey: 'ssh-rsa ...'              # option 1: inline value
  # sshPublicKeyRef:                       # option 2: reference a hub ConfigMap
  #   name: 'cluster-ssh-keys'
  #   key: 'ssh-public-key'
  #   namespace: 'cluster-install-secrets' # optional, defaults to policy namespace
  # CA Trust Bundle — provide inline OR reference a ConfigMap (not both)
  caTrustBundle: |                         # option 1: inline value
    -----BEGIN CERTIFICATE-----
    ...
  # caTrustBundleRef:                      # option 2: reference a hub ConfigMap
  #   name: 'cluster-ca-bundle'
  #   key: 'ca-bundle.crt'
  #   namespace: 'cluster-install-secrets' # optional, defaults to policy namespace
  secretSourceNamespace: 'cluster-install-secrets'
  managedClusterSet: 'default'
  useBond: true                      # default: true
  useDHCP: false                     # default: false
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

### Step 2: Create Source Secrets

The cluster-install policies copy secrets from a source namespace into each cluster's namespace. You must pre-create these secrets before provisioning.

Create the namespace:

```bash
oc create namespace cluster-install-secrets
```

Create the BMC credential secret (username/password for IPMI/Redfish):

```bash
oc create secret generic default-bmc-cred \
  -n cluster-install-secrets \
  --from-literal=username=<bmc-username> \
  --from-literal=password=<bmc-password>
```

Create the pull secret (for pulling OpenShift images):

```bash
oc create secret docker-registry default-pull-secret \
  -n cluster-install-secrets \
  --docker-server=quay.io \
  --docker-username=<username> \
  --docker-password=<password>
```

> **Note:** Per-host BMC credentials can override the default by setting `bmcCredentialRef` on individual hosts. The secrets policy will copy from the specified source.

### Step 3: Define the Cluster

Create a values file for your cluster under `autoshift/values/clusters/`:

```yaml
# autoshift/values/clusters/my-cluster.yaml
clusters:
  my-cluster:
    config:
      networking:
        vlan: 100
        machinePrefixLength: 25
        defaultGateway: '10.0.0.1'
        dnsServers:
          - 10.0.0.53
        clusterNetwork:
          cidr: 10.128.0.0/14
          hostPrefix: 23
        machineNetwork:
          cidr: '10.0.0.0/25'
        serviceNetwork:
          - 172.30.0.0/16
      hosts:
        master-0:
          bmcIP: '192.168.1.10'
          bmcPrefix: 'redfish-virtualmedia'
          bootMACAddress: 'aa:bb:cc:dd:ee:01'
          IP: '10.0.0.10'
          primaryMac: 'aa:bb:cc:dd:ee:02'
          interfaces:
            - macAddress: 'aa:bb:cc:dd:ee:01'
              name: 'eno1'
            - macAddress: 'aa:bb:cc:dd:ee:02'
              name: 'eno2'
        master-1:
          bmcIP: '192.168.1.11'
          bmcPrefix: 'redfish-virtualmedia'
          bootMACAddress: 'aa:bb:cc:dd:ee:11'
          IP: '10.0.0.11'
          primaryMac: 'aa:bb:cc:dd:ee:12'
          interfaces:
            - macAddress: 'aa:bb:cc:dd:ee:11'
              name: 'eno1'
            - macAddress: 'aa:bb:cc:dd:ee:12'
              name: 'eno2'
        master-2:
          bmcIP: '192.168.1.12'
          bmcPrefix: 'redfish-virtualmedia'
          bootMACAddress: 'aa:bb:cc:dd:ee:21'
          IP: '10.0.0.12'
          primaryMac: 'aa:bb:cc:dd:ee:22'
          interfaces:
            - macAddress: 'aa:bb:cc:dd:ee:21'
              name: 'eno1'
            - macAddress: 'aa:bb:cc:dd:ee:22'
              name: 'eno2'
      clusterInstall:
        createCluster: 'true'
        baseDomain: example.com
        openshiftVersion: '4.20.12'
        controlPlaneAgents: 3
        apiVip: '10.0.0.2'
        ingressVip: '10.0.0.3'
        sshPublicKey: 'ssh-rsa AAAAB3...'
        managedClusterSet: 'default'
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
oc get policies -n open-cluster-policies | grep cluster-install
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
        useBond: true
        useDHCP: false
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

Create the ConfigMap:

```bash
oc create configmap cluster-ssh-keys \
  -n cluster-install-secrets \
  --from-file=ssh-public-key=$HOME/.ssh/id_rsa.pub

oc create configmap cluster-ca-bundle \
  -n cluster-install-secrets \
  --from-file=ca-bundle.crt=/path/to/ca-bundle.crt
```

Reference it in your cluster config:

```yaml
clusterInstall:
  sshPublicKeyRef:
    name: 'cluster-ssh-keys'
    key: 'ssh-public-key'
    namespace: 'cluster-install-secrets'   # optional, defaults to policy namespace
  caTrustBundleRef:
    name: 'cluster-ca-bundle'
    key: 'ca-bundle.crt'
    namespace: 'cluster-install-secrets'   # optional, defaults to policy namespace
```

The ref is resolved at runtime by the ACM policy via a hub template `lookup`. If the referenced ConfigMap does not exist, the policy falls back to the inline `sshPublicKey` or `caTrustBundle` value. If neither is set, the field is empty.

This is a good candidate for clusterset defaults — define the ref once and all clusters inherit it:

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
      hosts:
        master-0:
          bmcIP: '192.168.1.10'
          bmcPrefix: 'redfish-virtualmedia'
          bootMACAddress: 'aa:bb:cc:dd:ee:01'
          IP: '10.0.0.10'
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
