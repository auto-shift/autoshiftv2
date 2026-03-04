# NMState Policy

This policy automates the deployment of the Kubernetes NMState Operator and manages NodeNetworkConfigurationPolicy (NNCP) resources through labels.

## Overview

NMState provides a Kubernetes-native way to configure network interfaces on cluster nodes. This Autoshift policy allows you to:

1. Install the NMState operator
2. Configure network interfaces (bonds, VLANs, ethernet) via labels
3. Define static routes and DNS settings
4. Target specific nodes using node selectors

## Enabling NMState

Set the following label on your cluster or clusterset:

```yaml
nmstate: 'true'
```

## Operator Configuration

| Label | Description | Default |
|-------|-------------|---------|
| `nmstate` | Enable/disable the operator | `'false'` |
| `nmstate-subscription-name` | Subscription name | `kubernetes-nmstate-operator` |
| `nmstate-channel` | Operator channel | `stable` |
| `nmstate-version` | Pin to specific CSV version | (latest) |
| `nmstate-source` | Catalog source | `redhat-operators` |
| `nmstate-source-namespace` | Catalog namespace | `openshift-marketplace` |

## Identifier Naming

All identifiers (`{id}`, `{M}`) in label patterns can be **numbers or names**:
- Numbers: `1`, `2`, `3`
- Names: `mgmt`, `storage`, `nic1`, `primary`, `worker0`

**Constraint**: Identifiers must **NOT** contain hyphens (`-`). This is how the dynamic discovery algorithm distinguishes base labels from sub-labels. Use alphanumeric characters, underscores, and dots only.

## Generated NNCPs

Each interface gets its own NNCP for fault isolation (one bad config doesn't block others):

| Condition | NNCP Name | Contents |
|-----------|-----------|----------|
| Each bond | `nmstate-bond-{id}` | Single bond interface |
| Each VLAN | `nmstate-vlan-{id}` | Single VLAN interface |
| Each ethernet | `nmstate-ethernet-{id}` | Single ethernet config |
| Each OVS bridge | `nmstate-ovs-bridge-{id}` | Single OVS bridge config |
| Any routes, DNS, or OVN | `nmstate-network-config` | Routes + DNS + OVN mappings (combined) |

Each NNCP includes the shared nodeSelector (if defined). Per-host NNCPs are named `nmstate-host-{id}`.

## Network Configuration Labels

All network configuration labels use the `autoshift.io/` prefix (e.g., `autoshift.io/nmstate-bond-mgmt`).

### Bond Interfaces

Create bonded network interfaces by defining labels with the pattern `nmstate-bond-{id}` where `{id}` is a unique identifier.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-bond-{id}` | Bond interface name | `bond0` |
| `nmstate-bond-{id}-mode` | Bond mode | `802.3ad`, `active-backup`, `balance-rr` |
| `nmstate-bond-{id}-port-{M}` | Port interfaces | `eno1`, `eno2` |
| `nmstate-bond-{id}-mtu` | MTU size | `9000` |
| `nmstate-bond-{id}-mac` | MAC address (use dots) | `aa.bb.cc.dd.ee.ff` |
| `nmstate-bond-{id}-miimon` | MII monitoring interval (ms) | `100` |
| `nmstate-bond-{id}-state` | Interface state | `up` (default), `down` |

#### Bond IPv4 Configuration

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-bond-{id}-ipv4` | IPv4 mode | `dhcp`, `static`, `disabled` |
| `nmstate-bond-{id}-ipv4-address-{M}` | Static IP address | `192.168.1.10` |
| `nmstate-bond-{id}-ipv4-address-{M}-cidr` | CIDR prefix length | `24` |
| `nmstate-bond-{id}-ipv4-gateway` | Default gateway | `192.168.1.1` |

#### Bond IPv6 Configuration

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-bond-{id}-ipv6` | IPv6 mode | `dhcp`, `autoconf`, `static`, `disabled` |
| `nmstate-bond-{id}-ipv6-address-{M}` | Static IPv6 address | `2001:db8::10` |
| `nmstate-bond-{id}-ipv6-address-{M}-cidr` | CIDR prefix length | `64` |
| `nmstate-bond-{id}-ipv6-gateway` | Default gateway | `2001:db8::1` |

### VLAN Interfaces

Create VLAN interfaces on top of bonds or other interfaces.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-vlan-{id}` | VLAN interface name | `bond0.100` |
| `nmstate-vlan-{id}-id` | VLAN ID | `100` |
| `nmstate-vlan-{id}-base` | Base interface | `bond0` |
| `nmstate-vlan-{id}-mtu` | MTU size | `1500` |
| `nmstate-vlan-{id}-state` | Interface state | `up` (default), `down` |
| `nmstate-vlan-{id}-ipv4` | IPv4 mode | `dhcp`, `static`, `disabled` |
| `nmstate-vlan-{id}-ipv4-address-{M}` | Static IP address | `10.100.0.10` |
| `nmstate-vlan-{id}-ipv4-address-{M}-cidr` | CIDR prefix length | `24` |

### Ethernet Interfaces

Configure standalone ethernet interfaces.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-ethernet-{id}` | Interface name | `eno1` |
| `nmstate-ethernet-{id}-mac` | MAC address (use dots) | `aa.bb.cc.dd.ee.ff` |
| `nmstate-ethernet-{id}-mtu` | MTU size | `9000` |
| `nmstate-ethernet-{id}-state` | Interface state | `up` (default), `down` |

### Static Routes

Define static routes for custom network paths.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-route-{id}-dest` | Destination network | `10.0.0.0` |
| `nmstate-route-{id}-cidr` | CIDR prefix length | `8` |
| `nmstate-route-{id}-gateway` | Next hop address | `192.168.1.1` |
| `nmstate-route-{id}-interface` | Outgoing interface | `bond0` |
| `nmstate-route-{id}-metric` | Route metric (optional) | `100` |
| `nmstate-route-{id}-table` | Routing table ID (optional) | `254` |

### DNS Configuration

Configure DNS servers and search domains.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-dns-server-{id}` | DNS server IP | `8.8.8.8` |
| `nmstate-dns-search-{id}` | Search domain | `example.com` |

### Node Selector

Target specific nodes for the network configuration.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-nodeselector-{id}-prefix` | Node label prefix (optional) | `node-role.kubernetes.io` |
| `nmstate-nodeselector-{id}-name` | Node label name | `worker` |
| `nmstate-nodeselector-{id}-value` | Node label value | `` (empty for exists) |

### OVS Bridge (for User Defined Networks)

Create OVS bridges for use with OpenShift User Defined Networks (UDN) localnet topology.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-ovs-bridge-{id}` | OVS bridge name | `ovs-br1` |
| `nmstate-ovs-bridge-{id}-port-{M}` | Port interfaces | `eth1`, `bond0` |
| `nmstate-ovs-bridge-{id}-stp` | Spanning tree protocol | `false` (default) |
| `nmstate-ovs-bridge-{id}-allow-extra-patch-ports` | Allow OVN patch ports | `true` (default) |
| `nmstate-ovs-bridge-{id}-mcast-snooping` | Multicast snooping | `true` (optional) |

### OVN Bridge Mapping (for UDN Localnet)

Map OVS bridges to OVN localnet networks for User Defined Networks. This is required for UDN localnet topology to connect pods/VMs to physical networks.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-ovn-mapping-{id}-localnet` | Localnet network name | `localnet1` |
| `nmstate-ovn-mapping-{id}-bridge` | OVS bridge to map | `ovs-br1` |

### Host-Specific Configuration

For scenarios where different nodes need different configurations (e.g., different static IPs), use the `nmstate-host-{id}` prefix. Each host identifier `{id}` creates a separate NNCP targeting that specific hostname.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-host-{id}-hostname` | Target node hostname | `worker-0.ocp.example.com` |
| `nmstate-host-{id}-bond-{id}` | Bond interface name | `bond0` |
| `nmstate-host-{id}-bond-{id}-mode` | Bond mode | `802.3ad` |
| `nmstate-host-{id}-bond-{id}-port-{M}` | Bond port | `eno1` |
| `nmstate-host-{id}-bond-{id}-ipv4` | IPv4 mode | `static` |
| `nmstate-host-{id}-bond-{id}-ipv4-address-{M}` | Static IP | `192.168.1.10` |
| `nmstate-host-{id}-bond-{id}-ipv4-address-{M}-cidr` | CIDR prefix | `24` |

The same pattern applies for all other interface types:
- `nmstate-host-{id}-vlan-{id}...`
- `nmstate-host-{id}-ethernet-{id}...`
- `nmstate-host-{id}-ovs-bridge-{id}...`
- `nmstate-host-{id}-ovn-mapping-{id}...`
- `nmstate-host-{id}-route-{id}...`
- `nmstate-host-{id}-dns-server-{id}`
- `nmstate-host-{id}-dns-search-{id}`

## Examples

### Basic Bond with DHCP

```yaml
# In your clusterset values file (e.g., autoshift/values/clustersets/hub.yaml):
labels:
  nmstate: 'true'
  nmstate-bond-mgmt: 'bond0'
  nmstate-bond-mgmt-mode: '802.3ad'
  nmstate-bond-mgmt-port-1: 'eno1'
  nmstate-bond-mgmt-port-2: 'eno2'
  nmstate-bond-mgmt-ipv4: 'dhcp'
  nmstate-bond-mgmt-ipv6: 'disabled'
```

### Bond with Static IP and Jumbo Frames

```yaml
labels:
  nmstate: 'true'
  nmstate-bond-mgmt: 'bond0'
  nmstate-bond-mgmt-mode: '802.3ad'
  nmstate-bond-mgmt-mtu: '9000'
  nmstate-bond-mgmt-miimon: '100'
  nmstate-bond-mgmt-port-1: 'eno1'
  nmstate-bond-mgmt-port-2: 'eno2'
  nmstate-bond-mgmt-ipv4: 'static'
  nmstate-bond-mgmt-ipv4-address-1: '192.168.1.10'
  nmstate-bond-mgmt-ipv4-address-1-cidr: '24'
  nmstate-bond-mgmt-ipv4-gateway: '192.168.1.1'
  nmstate-bond-mgmt-ipv6: 'disabled'
```

### Multiple Bonds with VLANs

```yaml
labels:
  nmstate: 'true'
  # Management bond
  nmstate-bond-mgmt: 'bond0'
  nmstate-bond-mgmt-mode: '802.3ad'
  nmstate-bond-mgmt-port-1: 'eno1'
  nmstate-bond-mgmt-port-2: 'eno2'
  nmstate-bond-mgmt-ipv4: 'dhcp'
  # Storage bond
  nmstate-bond-storage: 'bond1'
  nmstate-bond-storage-mode: 'active-backup'
  nmstate-bond-storage-mtu: '9000'
  nmstate-bond-storage-port-1: 'eno3'
  nmstate-bond-storage-port-2: 'eno4'
  nmstate-bond-storage-ipv4: 'disabled'
  # Storage VLAN on bond1
  nmstate-vlan-storage: 'bond1.100'
  nmstate-vlan-storage-id: '100'
  nmstate-vlan-storage-base: 'bond1'
  nmstate-vlan-storage-ipv4: 'static'
  nmstate-vlan-storage-ipv4-address-1: '10.100.0.10'
  nmstate-vlan-storage-ipv4-address-1-cidr: '24'
```

This generates three NNCPs:
- `nmstate-bond-mgmt` — contains `bond0`
- `nmstate-bond-storage` — contains `bond1`
- `nmstate-vlan-storage` — contains `bond1.100`

### Static Routes and DNS

```yaml
labels:
  nmstate: 'true'
  # Bond configuration...
  nmstate-bond-mgmt: 'bond0'
  nmstate-bond-mgmt-mode: '802.3ad'
  nmstate-bond-mgmt-port-1: 'eno1'
  nmstate-bond-mgmt-port-2: 'eno2'
  nmstate-bond-mgmt-ipv4: 'dhcp'
  # Static route to datacenter network
  nmstate-route-dc-dest: '10.0.0.0'
  nmstate-route-dc-cidr: '8'
  nmstate-route-dc-gateway: '192.168.1.1'
  nmstate-route-dc-interface: 'bond0'
  # DNS servers
  nmstate-dns-server-primary: '10.0.0.53'
  nmstate-dns-server-secondary: '10.0.0.54'
  nmstate-dns-search-cluster: 'cluster.local'
  nmstate-dns-search-corp: 'example.com'
```

This generates two NNCPs:
- `nmstate-bond-mgmt` — contains `bond0`
- `nmstate-network-config` — contains routes + DNS

### Apply Only to Worker Nodes

```yaml
labels:
  nmstate: 'true'
  nmstate-bond-mgmt: 'bond0'
  nmstate-bond-mgmt-mode: '802.3ad'
  nmstate-bond-mgmt-port-1: 'eno1'
  nmstate-bond-mgmt-port-2: 'eno2'
  nmstate-bond-mgmt-ipv4: 'dhcp'
  # Only apply to worker nodes
  nmstate-nodeselector-worker-prefix: 'node-role.kubernetes.io'
  nmstate-nodeselector-worker-name: 'worker'
  nmstate-nodeselector-worker-value: ''
```

### OVS Bridge with OVN Mapping for UDN

This example creates an OVS bridge with a bonded interface and maps it to an OVN localnet for use with User Defined Networks (UDN) or ClusterUserDefinedNetwork (CUDN).

```yaml
labels:
  nmstate: 'true'
  # First create a bond for the physical NICs
  nmstate-bond-udn: 'bond1'
  nmstate-bond-udn-mode: '802.3ad'
  nmstate-bond-udn-port-1: 'eno3'
  nmstate-bond-udn-port-2: 'eno4'
  nmstate-bond-udn-ipv4: 'disabled'
  # Create OVS bridge with bond1 as port
  nmstate-ovs-bridge-br1: 'ovs-br1'
  nmstate-ovs-bridge-br1-port-1: 'bond1'
  # Map the OVS bridge to OVN localnet
  nmstate-ovn-mapping-net1-localnet: 'localnet1'
  nmstate-ovn-mapping-net1-bridge: 'ovs-br1'
  # Apply to worker nodes
  nmstate-nodeselector-worker-prefix: 'node-role.kubernetes.io'
  nmstate-nodeselector-worker-name: 'worker'
  nmstate-nodeselector-worker-value: ''
```

This generates three NNCPs:
- `nmstate-bond-udn` — contains `bond1`
- `nmstate-ovs-bridge-br1` — contains `ovs-br1`
- `nmstate-network-config` — contains OVN bridge mapping

After applying this configuration, you can create a ClusterUserDefinedNetwork that references `localnet1`:

```yaml
apiVersion: k8s.ovn.org/v1
kind: ClusterUserDefinedNetwork
metadata:
  name: my-localnet
spec:
  namespaceSelector:
    matchLabels:
      network: localnet1
  network:
    topology: Localnet
    localnet:
      role: Secondary
      physicalNetworkName: localnet1
```

### Per-Host Static IP Configuration

When each node needs a unique static IP address:

```yaml
labels:
  nmstate: 'true'
  # Worker 0 - 192.168.1.10
  nmstate-host-worker0-hostname: 'worker-0.ocp.example.com'
  nmstate-host-worker0-bond-mgmt: 'bond0'
  nmstate-host-worker0-bond-mgmt-mode: '802.3ad'
  nmstate-host-worker0-bond-mgmt-port-1: 'eno1'
  nmstate-host-worker0-bond-mgmt-port-2: 'eno2'
  nmstate-host-worker0-bond-mgmt-ipv4: 'static'
  nmstate-host-worker0-bond-mgmt-ipv4-address-1: '192.168.1.10'
  nmstate-host-worker0-bond-mgmt-ipv4-address-1-cidr: '24'
  nmstate-host-worker0-bond-mgmt-ipv6: 'disabled'
  # Worker 1 - 192.168.1.11
  nmstate-host-worker1-hostname: 'worker-1.ocp.example.com'
  nmstate-host-worker1-bond-mgmt: 'bond0'
  nmstate-host-worker1-bond-mgmt-mode: '802.3ad'
  nmstate-host-worker1-bond-mgmt-port-1: 'eno1'
  nmstate-host-worker1-bond-mgmt-port-2: 'eno2'
  nmstate-host-worker1-bond-mgmt-ipv4: 'static'
  nmstate-host-worker1-bond-mgmt-ipv4-address-1: '192.168.1.11'
  nmstate-host-worker1-bond-mgmt-ipv4-address-1-cidr: '24'
  nmstate-host-worker1-bond-mgmt-ipv6: 'disabled'
  # Worker 2 - 192.168.1.12
  nmstate-host-worker2-hostname: 'worker-2.ocp.example.com'
  nmstate-host-worker2-bond-mgmt: 'bond0'
  nmstate-host-worker2-bond-mgmt-mode: '802.3ad'
  nmstate-host-worker2-bond-mgmt-port-1: 'eno1'
  nmstate-host-worker2-bond-mgmt-port-2: 'eno2'
  nmstate-host-worker2-bond-mgmt-ipv4: 'static'
  nmstate-host-worker2-bond-mgmt-ipv4-address-1: '192.168.1.12'
  nmstate-host-worker2-bond-mgmt-ipv4-address-1-cidr: '24'
  nmstate-host-worker2-bond-mgmt-ipv6: 'disabled'
```

This generates three separate NNCPs:
- `nmstate-host-worker0` → targets `worker-0.ocp.example.com`
- `nmstate-host-worker1` → targets `worker-1.ocp.example.com`
- `nmstate-host-worker2` → targets `worker-2.ocp.example.com`

## MAC Address Format

Kubernetes labels cannot contain colons (`:`), so MAC addresses must be specified using dots (`.`) as separators:

```yaml
# Correct
nmstate-bond-mgmt-mac: 'aa.bb.cc.dd.ee.ff'

# Incorrect (will fail)
nmstate-bond-mgmt-mac: 'aa:bb:cc:dd:ee:ff'
```

The policy will automatically convert dots to colons when generating the NNCP.

## Legacy File-Based Configuration

For backward compatibility, you can still use file-based NNCP configurations by placing YAML files in the `policies/nmstate/files/` directory and referencing them with labels:

```yaml
labels:
  nmstate: 'true'
  nmstate-nncp-mybond: 'my-bond-config'  # References files/my-bond-config.yaml
```

This method is deprecated in favor of the label-based configuration described above.

## Applying Changes

After updating your labels in the values files, push your changes to your git repo. The ApplicationSet will automatically detect the changes and propagate the policies to clusters with the `nmstate: 'true'` label, creating the appropriate NodeNetworkConfigurationPolicy resources.

To test locally before pushing:

```bash
helm template autoshift autoshift -f autoshift/values/global.yaml -f autoshift/values/clustersets/hub.yaml -f autoshift/values/clustersets/managed.yaml
```
