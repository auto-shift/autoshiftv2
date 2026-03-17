# NMState Policy

This policy automates the deployment of the Kubernetes NMState Operator and manages NodeNetworkConfigurationPolicy (NNCP) resources through structured YAML configuration.

## Overview

NMState provides a Kubernetes-native way to configure network interfaces on cluster nodes. This AutoShift policy allows you to:

1. Install the NMState operator
2. Configure network interfaces (bonds, VLANs, ethernet) via structured config
3. Define static routes, DNS settings, OVS bridges, and OVN mappings
4. Generate per-host NNCPs with automatic FQDN hostname resolution

## Enabling NMState

Set the following label on your cluster or clusterset:

```yaml
nmstate: 'true'
```

## Operator Configuration

These labels control the NMState operator installation:

| Label | Description | Default |
|-------|-------------|---------|
| `nmstate` | Enable/disable the operator | `'false'` |
| `nmstate-subscription-name` | Subscription name | `kubernetes-nmstate-operator` |
| `nmstate-channel` | Operator channel | `stable` |
| `nmstate-version` | Pin to specific CSV version | (latest) |
| `nmstate-source` | Catalog source | `redhat-operators` |
| `nmstate-source-namespace` | Catalog namespace | `openshift-marketplace` |

## How It Works

1. You define interface topology, routes, and DNS under `config.networking` in clusterset or cluster values files
2. You define per-host overrides (IPs, ports) under `config.hosts.*.networking`
3. The `cluster-config-maps` policy merges clusterset + cluster config into rendered-config ConfigMaps
4. The nmstate NNCP policy reads the rendered-config via hub templates and generates NNCPs

## Config Structure

All networking configuration lives under `config.networking` in your values files.

### Interface Topology

`config.networking.interfaces` is a **map keyed by user-chosen ID**. Each interface has a `type` field: `bond`, `vlan`, or `ethernet`.

```yaml
config:
  networking:
    interfaces:
      mgmt:                          # user-chosen ID
        type: bond
        name: bond0                  # nmstate interface name
        state: up                    # default: up
        mode: 802.3ad                # bond mode (required for bonds)
        mtu: 9000                    # optional
        miimon: 100                  # MII monitoring interval (optional)
        mac: 'aa:bb:cc:dd:ee:ff'     # optional — sets MAC and adds identifier: mac-address
        ports:                       # bond member interfaces (required for bonds)
          - eno1
          - eno2
        ipv4: disabled               # disabled | dhcp | static
        ipv6: disabled               # disabled | dhcp | autoconf | static

      mgmt-vlan:
        type: vlan
        name: bond0.100
        id: 100                      # VLAN ID (required for vlans)
        base: bond0                  # parent interface (required for vlans)
        mtu: 1500
        ipv4: static                 # per-host IPs in hosts section
        ipv6: disabled

      nic3:
        type: ethernet
        name: eno3
        mac: 'aa:bb:cc:dd:ee:03'     # adds identifier: mac-address to NNCP
        mtu: 9000
        ipv4: dhcp
        ipv6: disabled
```

### MAC-Based Interface Identification

When `mac` is set on an interface, the generated NNCP includes `identifier: mac-address`. This tells nmstate to match the interface by MAC address instead of name — useful for heterogeneous hardware where NIC names vary across hosts but MACs are known.

```yaml
    interfaces:
      # Define ethernet ports by MAC — nmstate matches by MAC, not name
      port1:
        type: ethernet
        name: port1                  # logical name (used in bond ports list)
        mac: '00:23:45:67:89:1a'    # identifier: mac-address auto-added
      port2:
        type: ethernet
        name: port2
        mac: '00:23:45:67:89:1b'
      # Bond references the MAC-identified ports by name
      mgmt:
        type: bond
        name: bond0
        mode: 802.3ad
        ports: [port1, port2]       # nmstate resolves via MAC on port1/port2
        ipv4: dhcp
```

The `name` field on MAC-identified interfaces is a logical name — nmstate uses it for internal reference (e.g., bond port lists, VLAN base interface) but matches the physical NIC by MAC.

### OVS Bridges

```yaml
    ovsBridges:
      br1:
        name: ovs-br1
        ports: [bond1]
        stp: false                   # default: false
        allowExtraPatchPorts: true   # default: true
        mcastSnooping: false         # optional
```

### OVN Bridge Mappings

```yaml
    ovnMappings:
      net1:
        localnet: localnet1
        bridge: ovs-br1
```

### Static Routes

```yaml
    routes:
      default:
        destination: 0.0.0.0/0
        gateway: 10.0.0.1
        interface: bond0.100
      dc:
        destination: 10.0.0.0/8
        gateway: 192.168.1.1
        interface: bond0
        metric: 100                  # optional
        tableId: 254                 # optional
```

### DNS

```yaml
    dns:
      servers:
        - 10.0.0.53
        - 10.0.0.54
      search:
        - example.com
```

### Node Selector

Applied to all cluster-wide NNCPs:

```yaml
    nodeSelector:
      node-role.kubernetes.io/worker: ''
```

## Per-Host Overrides

Each host can override interface properties from the topology. Per-host overrides reference **topology interface IDs**.

```yaml
  hosts:
    master-0:
      networking:
        interfaces:
          mgmt-vlan:                 # references topology interface ID
            ipv4:
              addresses:
                - ip: 10.0.0.10
                  prefixLength: 25
            ipv6:                    # IPv6 static addresses
              addresses:
                - ip: 'fd00::10'
                  prefixLength: 64
          mgmt:                      # override bond ports per-host
            ports: [enp3s0f0, enp3s0f1]
        routes:
          extra:
            destination: 172.16.0.0/12
            gateway: 10.100.1.1
            interface: bond0.100
        dns:
          servers: [10.0.0.55]
          search: [special.example.com]
```

### Per-Host Hostname Resolution

Per-host NNCPs use `kubernetes.io/hostname` as the nodeSelector. The hostname is resolved in this order:

1. **`hosts.*.hostname`** — if set, uses that value as-is (for non-standard node names)
2. **Auto-constructed** — `{mapKey}.{clusterDomain}` where `clusterDomain` is looked up from `dns.config.openshift.io/cluster` on the managed cluster

For cluster-install clusters, the FQDN is constructed in siteconfig as `{mapKey}.{clusterName}.{baseDomain}`.

## Generated NNCPs

Each interface gets its own NNCP for fault isolation:

| Config Path | NNCP Name | Notes |
|---|---|---|
| `networking.interfaces.{id}` (bond) | `nmstate-bond-{id}` | One per bond |
| `networking.interfaces.{id}` (vlan) | `nmstate-vlan-{id}` | One per VLAN |
| `networking.interfaces.{id}` (ethernet) | `nmstate-ethernet-{id}` | One per ethernet |
| `networking.ovsBridges.{id}` | `nmstate-ovs-bridge-{id}` | One per OVS bridge |
| routes + dns + ovnMappings | `nmstate-network-config` | Combined into one |
| `hosts.{name}.networking` | `nmstate-host-{name}` | Per-host with hostname nodeSelector |

**Per-host override rule**: If ANY host defines per-host overrides for an interface, that interface gets per-host NNCPs instead of a cluster-wide NNCP.

## IPv4 Modes

| Mode | Result |
|------|--------|
| `disabled` | `enabled: false` |
| `dhcp` | `enabled: true, dhcp: true` |
| `static` | `enabled: true, dhcp: false` — per-host addresses required |

## IPv6 Modes

| Mode | Result |
|------|--------|
| `disabled` | `enabled: false` |
| `dhcp` | `enabled: true, dhcp: true` |
| `autoconf` | `enabled: true, autoconf: true` (SLAAC) |
| `static` | `enabled: true, dhcp: false, autoconf: false` — per-host addresses required |

## Examples

### Basic Bond with DHCP

```yaml
clusters:
  my-cluster:
    config:
      networking:
        interfaces:
          mgmt:
            type: bond
            name: bond0
            mode: 802.3ad
            ports: [eno1, eno2]
            ipv4: dhcp
            ipv6: disabled
```

Generates: `nmstate-bond-mgmt`

### Bond + VLAN with Static IPs

```yaml
clusters:
  my-cluster:
    config:
      networking:
        interfaces:
          mgmt:
            type: bond
            name: bond0
            mode: 802.3ad
            mtu: 9000
            miimon: 100
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
          search: [example.com]
      hosts:
        master-0:
          networking:
            interfaces:
              mgmt-vlan:
                ipv4:
                  addresses:
                    - ip: 10.0.0.10
                      prefixLength: 25
        master-1:
          networking:
            interfaces:
              mgmt-vlan:
                ipv4:
                  addresses:
                    - ip: 10.0.0.11
                      prefixLength: 25
```

Generates:
- `nmstate-bond-mgmt` — cluster-wide bond (DHCP disabled, no per-host overrides)
- `nmstate-host-master-0` — VLAN with static IP 10.0.0.10
- `nmstate-host-master-1` — VLAN with static IP 10.0.0.11
- `nmstate-network-config` — default route + DNS

### Dual-Stack (IPv4 + IPv6)

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
      v4-vlan:
        type: vlan
        name: bond0.100
        id: 100
        base: bond0
        ipv4: static
        ipv6: disabled
      v6-vlan:
        type: vlan
        name: bond0.200
        id: 200
        base: bond0
        ipv4: disabled
        ipv6: static
    routes:
      default-v4:
        destination: 0.0.0.0/0
        gateway: 10.0.0.1
        interface: bond0.100
      default-v6:
        destination: '::/0'
        gateway: 'fd00::1'
        interface: bond0.200
    dns:
      servers: [10.0.0.53, 'fd00::53']
  hosts:
    master-0:
      networking:
        interfaces:
          v4-vlan:
            ipv4:
              addresses:
                - ip: 10.0.0.10
                  prefixLength: 25
          v6-vlan:
            ipv6:
              addresses:
                - ip: 'fd00::10'
                  prefixLength: 64
```

### OVS Bridge with OVN Mapping for UDN

```yaml
config:
  networking:
    interfaces:
      udn:
        type: bond
        name: bond1
        mode: 802.3ad
        ports: [eno3, eno4]
        ipv4: disabled
        ipv6: disabled
    ovsBridges:
      br1:
        name: ovs-br1
        ports: [bond1]
        stp: false
        allowExtraPatchPorts: true
    ovnMappings:
      net1:
        localnet: localnet1
        bridge: ovs-br1
    nodeSelector:
      node-role.kubernetes.io/worker: ''
```

Generates:
- `nmstate-bond-udn` — bond1 with worker nodeSelector
- `nmstate-ovs-bridge-br1` — OVS bridge with bond1 port
- `nmstate-network-config` — OVN bridge mapping

### Per-Host Bond Port Overrides

When hosts have different NIC names but the same topology:

```yaml
config:
  networking:
    interfaces:
      mgmt:
        type: bond
        name: bond0
        mode: active-backup
        ports: [eno1, eno2]        # default ports
        ipv4: dhcp
        ipv6: disabled
  hosts:
    worker-0:
      networking:
        interfaces:
          mgmt:
            ports: [enp3s0f0, enp3s0f1]   # different NICs on this host
    worker-1:
      networking:
        interfaces:
          mgmt:
            ports: [enp4s0f0, enp4s0f1]   # different NICs on this host
```

## Validation

The `_validate-cluster-install.tpl` validates at Helm render time:

- Interface `type` and `name` required
- Bond requires `mode` and `ports`
- VLAN requires `id` and `base`
- `ipv4` must be `disabled`, `dhcp`, or `static`
- `ipv6` must be `disabled`, `dhcp`, `autoconf`, or `static`
- Static interfaces require at least one host with addresses
- Per-host interface references must exist in topology
- Per-host addresses require `ip` and `prefixLength`
- Routes require `destination`, `gateway`, and `interface`

## Testing

```bash
# Render nmstate chart
helm template test policies/nmstate/ -f policies/nmstate/values.yaml

# Render with cluster values
helm template test policies/nmstate/ -f policies/nmstate/values.yaml \
  -f autoshift/values/global.yaml \
  -f autoshift/values/clustersets/hub.yaml \
  -f autoshift/values/clusters/test-cluster.yaml
```
