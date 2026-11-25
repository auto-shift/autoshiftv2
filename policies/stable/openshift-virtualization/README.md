# openshift-virtualization

Installs Red Hat OpenShift Virtualization, applies the `HyperConverged` custom resource, and
optionally creates the localnet secondary networks that virtual machines attach to.

| Policy | Remediation | What it does |
|---|---|---|
| `policy-virt-operator-install` | `${REMEDIATION}` | Creates `openshift-cnv` and installs the operator through an `OperatorPolicy` |
| `policy-virtualization-hyperconverged` | `${REMEDIATION}` | Applies the `HyperConverged` custom resource with `mustonlyhave`. Waits for the operator install |
| `policy-virtualization-networks` | `${REMEDIATION}` | One localnet `ClusterUserDefinedNetwork` per entry in `config.networking.cudn.vlans` |
| `policy-virtualization-networks-ready` | inform | Reports a configured `ClusterUserDefinedNetwork` that is missing or not `NetworkCreated` |

## Labels

Set these in a clusterset or cluster values file, never directly on a `ManagedCluster`.

| Label | Default | Description |
|---|---|---|
| `virt` | `'false'` | Places the operator install and `HyperConverged` policies |
| `virt-networks` | `'false'` | Places the network policies, where `virt` is also `'true'` |
| `virt-subscription-name` | `kubevirt-hyperconverged` | Operator package name |
| `virt-channel` | `stable` | Operator channel |
| `virt-version` | none | Operator CSV to pin, when `config.virt.versions` is not set |
| `virt-source` | `redhat-operators` | Catalog source |
| `virt-source-namespace` | `openshift-marketplace` | Catalog source namespace |

## Virtual machine networks

A localnet network connects virtual machines to a physical network through an Open vSwitch bridge
on each node, such as a bridge on a bond. It takes two parts:

1. **The bridge and its OVN bridge mapping on the nodes**, from the nmstate policy:
   `config.networking.ovsBridges` builds the bridge on the bond, and `config.networking.ovnMappings`
   maps a localnet name to it. See the [nmstate README](../nmstate/README.md).
2. **A `ClusterUserDefinedNetwork` per VLAN**, from this policy, naming that localnet in
   `physicalNetworkName`.

```yaml
labels:
  nmstate: 'true'
  virt: 'true'
  virt-networks: 'true'
config:
  networking:
    ovsBridges:
      vmbr:
        name: br-vm
        ports:
          - bond1
    ovnMappings:
      vm:
        localnet: physnet-vm
        bridge: br-vm
    cudn:
      physicalNetworkName: physnet-vm   # default for every entry
      vlans:
        - id: 100                       # ClusterUserDefinedNetwork vlan-100, Namespace label vlan-100
        - id: 200
          name: storage-200
          mtu: 9000
        - name: flat-net                # untagged
          selectorKey: example.com/flat-net
```

| Key | Where | Default | Notes |
|---|---|---|---|
| `physicalNetworkName` | `cudn` or entry | none, required | An `ovnMappings` localnet. 1-253 characters, without `,` or `:` |
| `mtu` | `cudn` or entry | the OVN-Kubernetes default | 576-65536 |
| `id` | entry | none | VLAN ID, 1-4094, in access mode. Omit for an untagged network |
| `name` | entry | `vlan-<id>` | The `ClusterUserDefinedNetwork` name, and the attachment name a virtual machine references. Required without `id` |
| `selectorKey` | entry | `vlan-<id>`, or `name` without `id` | A Namespace that carries this label, with any value, joins the network |

IP address management is always `Disabled`, because OpenShift Virtualization does not support it
on localnet networks for virtual machines. Addresses come from the physical network or the guest.

The values are validated when the `autoshift` chart renders, by
`autoshift/templates/_validate-virt-networks.tpl`. `virt-networks: 'true'` with no `vlans` fails
there, and a placed policy with no `vlans` reports `NonCompliant` rather than `Compliant`.

### Join a network

Label the Namespace with the entry's `selectorKey`, for example `vlan-100: 'true'`.
OVN-Kubernetes then creates a `NetworkAttachmentDefinition` named after the network in that
Namespace. The selector must not match `default` or an `openshift-*` Namespace.

### Change or remove a network

The network spec of a `ClusterUserDefinedNetwork` cannot be changed after it is created. Changing
`physicalNetworkName`, `mtu` or `id` for an existing entry is rejected by the API, and
`policy-virtualization-networks` reports `NonCompliant`. The policy does not re-create the network:
OVN-Kubernetes holds the delete while virtual machines use it. To change one, stop or move the
virtual machines on it, delete the `ClusterUserDefinedNetwork`, and let the policy create it again.

Removing an entry from `vlans` leaves its `ClusterUserDefinedNetwork` in place. Delete it by hand
once nothing uses it.

## Verify

```bash
oc get clusteruserdefinednetwork -l autoshift.io/managed-by=openshift-virtualization
oc get network-attachment-definitions -A
```
