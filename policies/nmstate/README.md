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

## Network Configuration Labels

All network configuration labels use the `autoshift.io/` prefix (e.g., `autoshift.io/nmstate-bond-1`).

### Bond Interfaces

Create bonded network interfaces by defining labels with the pattern `nmstate-bond-{N}` where `{N}` is a unique identifier.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-bond-{N}` | Bond interface name | `bond0` |
| `nmstate-bond-{N}-mode` | Bond mode | `802.3ad`, `active-backup`, `balance-rr` |
| `nmstate-bond-{N}-port-{M}` | Port interfaces (numbered) | `eno1`, `eno2` |
| `nmstate-bond-{N}-mtu` | MTU size | `9000` |
| `nmstate-bond-{N}-mac` | MAC address (use dots) | `aa.bb.cc.dd.ee.ff` |
| `nmstate-bond-{N}-miimon` | MII monitoring interval (ms) | `100` |
| `nmstate-bond-{N}-state` | Interface state | `up` (default), `down` |

#### Bond IPv4 Configuration

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-bond-{N}-ipv4` | IPv4 mode | `dhcp`, `static`, `disabled` |
| `nmstate-bond-{N}-ipv4-address-{M}` | Static IP address | `192.168.1.10` |
| `nmstate-bond-{N}-ipv4-address-{M}-cidr` | CIDR prefix length | `24` |
| `nmstate-bond-{N}-ipv4-gateway` | Default gateway | `192.168.1.1` |

#### Bond IPv6 Configuration

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-bond-{N}-ipv6` | IPv6 mode | `dhcp`, `autoconf`, `static`, `disabled` |
| `nmstate-bond-{N}-ipv6-address-{M}` | Static IPv6 address | `2001:db8::10` |
| `nmstate-bond-{N}-ipv6-address-{M}-cidr` | CIDR prefix length | `64` |
| `nmstate-bond-{N}-ipv6-gateway` | Default gateway | `2001:db8::1` |

### VLAN Interfaces

Create VLAN interfaces on top of bonds or other interfaces.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-vlan-{N}` | VLAN interface name | `bond0.100` |
| `nmstate-vlan-{N}-id` | VLAN ID | `100` |
| `nmstate-vlan-{N}-base` | Base interface | `bond0` |
| `nmstate-vlan-{N}-mtu` | MTU size | `1500` |
| `nmstate-vlan-{N}-state` | Interface state | `up` (default), `down` |
| `nmstate-vlan-{N}-ipv4` | IPv4 mode | `dhcp`, `static`, `disabled` |
| `nmstate-vlan-{N}-ipv4-address-{M}` | Static IP address | `10.100.0.10` |
| `nmstate-vlan-{N}-ipv4-address-{M}-cidr` | CIDR prefix length | `24` |

### Ethernet Interfaces

Configure standalone ethernet interfaces.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-ethernet-{N}` | Interface name | `eno1` |
| `nmstate-ethernet-{N}-mac` | MAC address (use dots) | `aa.bb.cc.dd.ee.ff` |
| `nmstate-ethernet-{N}-mtu` | MTU size | `9000` |
| `nmstate-ethernet-{N}-state` | Interface state | `up` (default), `down` |

### Static Routes

Define static routes for custom network paths.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-route-{N}-dest` | Destination network | `10.0.0.0` |
| `nmstate-route-{N}-cidr` | CIDR prefix length | `8` |
| `nmstate-route-{N}-gateway` | Next hop address | `192.168.1.1` |
| `nmstate-route-{N}-interface` | Outgoing interface | `bond0` |
| `nmstate-route-{N}-metric` | Route metric (optional) | `100` |
| `nmstate-route-{N}-table` | Routing table ID (optional) | `254` |

### DNS Configuration

Configure DNS servers and search domains.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-dns-server-{N}` | DNS server IP | `8.8.8.8` |
| `nmstate-dns-search-{N}` | Search domain | `example.com` |

### Node Selector

Target specific nodes for the network configuration.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-nodeselector-{N}-key` | Node label key | `node-role.kubernetes.io/worker` |
| `nmstate-nodeselector-{N}-value` | Node label value | `` (empty for exists) |

### OVS Bridge (for User Defined Networks)

Create OVS bridges for use with OpenShift User Defined Networks (UDN) localnet topology.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-ovs-bridge-{N}` | OVS bridge name | `ovs-br1` |
| `nmstate-ovs-bridge-{N}-port-{M}` | Port interfaces (numbered) | `eth1`, `bond0` |
| `nmstate-ovs-bridge-{N}-stp` | Spanning tree protocol | `false` (default) |
| `nmstate-ovs-bridge-{N}-allow-extra-patch-ports` | Allow OVN patch ports | `true` (default) |
| `nmstate-ovs-bridge-{N}-mcast-snooping` | Multicast snooping | `true` (optional) |

### OVN Bridge Mapping (for UDN Localnet)

Map OVS bridges to OVN localnet networks for User Defined Networks. This is required for UDN localnet topology to connect pods/VMs to physical networks.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-ovn-mapping-{N}-localnet` | Localnet network name | `localnet1` |
| `nmstate-ovn-mapping-{N}-bridge` | OVS bridge to map | `ovs-br1` |

### Host-Specific Configuration

For scenarios where different nodes need different configurations (e.g., different static IPs), use the `nmstate-host-{H}` prefix. Each host identifier `{H}` creates a separate NNCP targeting that specific hostname.

| Label | Description | Example |
|-------|-------------|---------|
| `nmstate-host-{H}-hostname` | Target node hostname | `worker-0.ocp.example.com` |
| `nmstate-host-{H}-bond-{N}` | Bond interface name | `bond0` |
| `nmstate-host-{H}-bond-{N}-mode` | Bond mode | `802.3ad` |
| `nmstate-host-{H}-bond-{N}-port-{M}` | Bond port | `eno1` |
| `nmstate-host-{H}-bond-{N}-ipv4` | IPv4 mode | `static` |
| `nmstate-host-{H}-bond-{N}-ipv4-address-{M}` | Static IP | `192.168.1.10` |
| `nmstate-host-{H}-bond-{N}-ipv4-address-{M}-cidr` | CIDR prefix | `24` |

The same pattern applies for all other interface types:
- `nmstate-host-{H}-vlan-{N}...`
- `nmstate-host-{H}-ethernet-{N}...`
- `nmstate-host-{H}-ovs-bridge-{N}...`
- `nmstate-host-{H}-ovn-mapping-{N}...`
- `nmstate-host-{H}-route-{N}...`
- `nmstate-host-{H}-dns-server-{N}`
- `nmstate-host-{H}-dns-search-{N}`

## Examples

### Basic Bond with DHCP

```yaml
# In values.hub.yaml under your clusterset labels:
labels:
  nmstate: 'true'
  nmstate-bond-1: 'bond0'
  nmstate-bond-1-mode: '802.3ad'
  nmstate-bond-1-port-1: 'eno1'
  nmstate-bond-1-port-2: 'eno2'
  nmstate-bond-1-ipv4: 'dhcp'
  nmstate-bond-1-ipv6: 'disabled'
```

### Bond with Static IP and Jumbo Frames

```yaml
labels:
  nmstate: 'true'
  nmstate-bond-1: 'bond0'
  nmstate-bond-1-mode: '802.3ad'
  nmstate-bond-1-mtu: '9000'
  nmstate-bond-1-miimon: '100'
  nmstate-bond-1-port-1: 'eno1'
  nmstate-bond-1-port-2: 'eno2'
  nmstate-bond-1-ipv4: 'static'
  nmstate-bond-1-ipv4-address-1: '192.168.1.10'
  nmstate-bond-1-ipv4-address-1-cidr: '24'
  nmstate-bond-1-ipv4-gateway: '192.168.1.1'
  nmstate-bond-1-ipv6: 'disabled'
```

### Multiple Bonds with VLANs

```yaml
labels:
  nmstate: 'true'
  # Management bond
  nmstate-bond-1: 'bond0'
  nmstate-bond-1-mode: '802.3ad'
  nmstate-bond-1-port-1: 'eno1'
  nmstate-bond-1-port-2: 'eno2'
  nmstate-bond-1-ipv4: 'dhcp'
  # Storage bond
  nmstate-bond-2: 'bond1'
  nmstate-bond-2-mode: 'active-backup'
  nmstate-bond-2-mtu: '9000'
  nmstate-bond-2-port-1: 'eno3'
  nmstate-bond-2-port-2: 'eno4'
  nmstate-bond-2-ipv4: 'disabled'
  # Storage VLAN on bond1
  nmstate-vlan-1: 'bond1.100'
  nmstate-vlan-1-id: '100'
  nmstate-vlan-1-base: 'bond1'
  nmstate-vlan-1-ipv4: 'static'
  nmstate-vlan-1-ipv4-address-1: '10.100.0.10'
  nmstate-vlan-1-ipv4-address-1-cidr: '24'
```

### Static Routes and DNS

```yaml
labels:
  nmstate: 'true'
  # Bond configuration...
  nmstate-bond-1: 'bond0'
  nmstate-bond-1-mode: '802.3ad'
  nmstate-bond-1-port-1: 'eno1'
  nmstate-bond-1-port-2: 'eno2'
  nmstate-bond-1-ipv4: 'dhcp'
  # Static route to datacenter network
  nmstate-route-1-dest: '10.0.0.0'
  nmstate-route-1-cidr: '8'
  nmstate-route-1-gateway: '192.168.1.1'
  nmstate-route-1-interface: 'bond0'
  # DNS servers
  nmstate-dns-server-1: '10.0.0.53'
  nmstate-dns-server-2: '10.0.0.54'
  nmstate-dns-search-1: 'cluster.local'
  nmstate-dns-search-2: 'example.com'
```

### Apply Only to Worker Nodes

```yaml
labels:
  nmstate: 'true'
  nmstate-bond-1: 'bond0'
  nmstate-bond-1-mode: '802.3ad'
  nmstate-bond-1-port-1: 'eno1'
  nmstate-bond-1-port-2: 'eno2'
  nmstate-bond-1-ipv4: 'dhcp'
  # Only apply to worker nodes
  nmstate-nodeselector-1-key: 'node-role.kubernetes.io/worker'
  nmstate-nodeselector-1-value: ''
```

### OVS Bridge with OVN Mapping for UDN

This example creates an OVS bridge with a bonded interface and maps it to an OVN localnet for use with User Defined Networks (UDN) or ClusterUserDefinedNetwork (CUDN).

```yaml
labels:
  nmstate: 'true'
  # First create a bond for the physical NICs
  nmstate-bond-1: 'bond1'
  nmstate-bond-1-mode: '802.3ad'
  nmstate-bond-1-port-1: 'eno3'
  nmstate-bond-1-port-2: 'eno4'
  nmstate-bond-1-ipv4: 'disabled'
  # Create OVS bridge with bond1 as port
  nmstate-ovs-bridge-1: 'ovs-br1'
  nmstate-ovs-bridge-1-port-1: 'bond1'
  # Map the OVS bridge to OVN localnet
  nmstate-ovn-mapping-1-localnet: 'localnet1'
  nmstate-ovn-mapping-1-bridge: 'ovs-br1'
  # Apply to worker nodes
  nmstate-nodeselector-1-key: 'node-role.kubernetes.io/worker'
  nmstate-nodeselector-1-value: ''
```

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
  nmstate-host-1-hostname: 'worker-0.ocp.example.com'
  nmstate-host-1-bond-1: 'bond0'
  nmstate-host-1-bond-1-mode: '802.3ad'
  nmstate-host-1-bond-1-port-1: 'eno1'
  nmstate-host-1-bond-1-port-2: 'eno2'
  nmstate-host-1-bond-1-ipv4: 'static'
  nmstate-host-1-bond-1-ipv4-address-1: '192.168.1.10'
  nmstate-host-1-bond-1-ipv4-address-1-cidr: '24'
  nmstate-host-1-bond-1-ipv6: 'disabled'
  # Worker 1 - 192.168.1.11
  nmstate-host-2-hostname: 'worker-1.ocp.example.com'
  nmstate-host-2-bond-1: 'bond0'
  nmstate-host-2-bond-1-mode: '802.3ad'
  nmstate-host-2-bond-1-port-1: 'eno1'
  nmstate-host-2-bond-1-port-2: 'eno2'
  nmstate-host-2-bond-1-ipv4: 'static'
  nmstate-host-2-bond-1-ipv4-address-1: '192.168.1.11'
  nmstate-host-2-bond-1-ipv4-address-1-cidr: '24'
  nmstate-host-2-bond-1-ipv6: 'disabled'
  # Worker 2 - 192.168.1.12
  nmstate-host-3-hostname: 'worker-2.ocp.example.com'
  nmstate-host-3-bond-1: 'bond0'
  nmstate-host-3-bond-1-mode: '802.3ad'
  nmstate-host-3-bond-1-port-1: 'eno1'
  nmstate-host-3-bond-1-port-2: 'eno2'
  nmstate-host-3-bond-1-ipv4: 'static'
  nmstate-host-3-bond-1-ipv4-address-1: '192.168.1.12'
  nmstate-host-3-bond-1-ipv4-address-1-cidr: '24'
  nmstate-host-3-bond-1-ipv6: 'disabled'
```

This generates three separate NNCPs:
- `nmstate-host-1` → targets `worker-0.ocp.example.com`
- `nmstate-host-2` → targets `worker-1.ocp.example.com`
- `nmstate-host-3` → targets `worker-2.ocp.example.com`

## MAC Address Format

Kubernetes labels cannot contain colons (`:`), so MAC addresses must be specified using dots (`.`) as separators:

```yaml
# Correct
nmstate-bond-1-mac: 'aa.bb.cc.dd.ee.ff'

# Incorrect (will fail)
nmstate-bond-1-mac: 'aa:bb:cc:dd:ee:ff'
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

After updating your labels in `values.hub.yaml`:

```bash
helm template autoshift autoshift -f autoshift/values.hub.yaml | oc apply -f -
```

The policy will propagate to clusters with the `nmstate: 'true'` label and create the appropriate NodeNetworkConfigurationPolicy resources.
