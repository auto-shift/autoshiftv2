# project-networks

Attaches projects to VLANs. For each entry in `config.projectNetworks` the policy creates a
ClusterUserDefinedNetwork (`k8s.ovn.org/v1`) with `Localnet` topology on an access VLAN.
OVN-Kubernetes then creates a NetworkAttachmentDefinition of the same name in every namespace that
matches the entry's `namespaceSelector`. Localnet networks are always secondary networks: pods and
virtual machines attach through the `k8s.v1.cni.cncf.io/networks` annotation and keep their default
pod network, so a namespace can join or leave a VLAN at any time.

The policy consumes the physical network and does not create it. The OVN bridge mapping named by
`physicalNetworkName` comes from the `nmstate` policy's `ovnMappings` (a
NodeNetworkConfigurationPolicy `spec.desiredState.ovn.bridge-mappings` entry with that `localnet`).

## Policies

| Policy | Remediation | What it does |
|---|---|---|
| `policy-project-networks` | `${REMEDIATION}`: enforce, or inform under `autoshift.dryRun` | Applies each declared ClusterUserDefinedNetwork from `manifests/project-networks.yaml` that is absent, or owned with an unchanged `spec.network` |
| `policy-project-networks-test` | Always inform | From `test/project-networks.yaml`: asserts every declared network exactly as declared, so a missing, drifted, or held network is NonCompliant, and reports a missing CRD and owned networks that are no longer declared. It never changes anything |

Both files are the same template and differ only in the `$want` line. The
inform-only checks are a policy of their own, as the [developer
guide](../../../docs/developer-guide.md#gating-on-cluster-state) requires: a `remediationAction` on
the root of one Policy overrides all of its children, so a check bundled into the enforcing Policy
would start changing what it should only report.

`policy-project-networks-test` is Compliant only when every declared network exists and matches,
none is held, and nothing stale is left. It asserts each declared network itself, so it does not
report Compliant before the enforcing policy has created it, so depend on it, not on `policy-project-networks`, when another
policy needs that state. `policy-project-networks` has no status ConfigMap; it stays Compliant while
an entry is held, because a held entry is not applied by design.

Neither Policy depends on the other:

- `policy-project-networks` must not depend on `policy-project-networks-test`. The test policy is
  NonCompliant exactly while the enforcing policy has work to do or errors to report, so the
  enforcing policy would never run.
- `policy-project-networks-test` does not depend on `policy-project-networks`. A dependency would
  hold the test policy Pending whenever the enforcing one is NonCompliant, which under dry run is
  whenever anything would change, and would hide its reports exactly when they are wanted.

## Enable

| Label | Default | Description |
|---|---|---|
| `autoshift.io/project-networks` | `'false'` | Set `'true'` to manage the networks in `config.projectNetworks` |

Set it in a clusterset or cluster values file, without the `autoshift.io/` prefix. Never put it on
a ManagedCluster by hand.

## Configuration

```yaml
config:
  projectNetworks:
    vlan-70:
      vlan: 70                            # required, 1 to 4094
      physicalNetworkName: physnet-vlan70 # required, a localnet in the nmstate ovnMappings
      subnets: ['10.70.0.0/24']           # optional; omit for IPAM-less
      namespaceSelector:                  # required: which namespaces attach
        matchLabels:
          network.example.com/vlan-70: 'true'
```

| Key | Type | Required | Default | Notes |
|---|---|---|---|---|
| `<name>` | DNS-1123 label | yes | none | The ClusterUserDefinedNetwork name |
| `vlan` | integer | yes | none | Access VLAN ID, from 1 to 4094 |
| `physicalNetworkName` | string | yes | none | Must not contain `,`, `:`, whitespace, or control characters |
| `subnets` | list of CIDR strings | no | `[]`, IPAM disabled | One CIDR, or one IPv4 and one IPv6 CIDR, each a valid IPv4 or IPv6 CIDR. OVN-Kubernetes assigns addresses from them. Without subnets, or with an empty list, `ipam.mode` is `Disabled`: OVN-Kubernetes assigns only MAC addresses, and addresses come from the workload or from DHCP on the VLAN |
| `namespaceSelector` | label selector | yes | none | A non-empty label selector, `matchLabels`, `matchExpressions`, or both, that cannot match every namespace. See [Namespace selector guard](#namespace-selector-guard) |

Any invalid value fails the hub template and nothing is applied, see [Validation](#validation).

With project onboarding, a catalog label such as `network.example.com/vlan-{vlan}: 'true'` and a
project field `vlan: '70'` label the project namespace, and the selector above attaches it.

Rendered for the entry above:

```yaml
apiVersion: k8s.ovn.org/v1
kind: ClusterUserDefinedNetwork
metadata:
  name: vlan-70
  labels:
    autoshift.io/managed-by: project-networks
spec:
  namespaceSelector:
    matchLabels:
      network.example.com/vlan-70: 'true'
  network:
    topology: Localnet
    localnet:
      role: Secondary
      physicalNetworkName: physnet-vlan70
      vlan:
        mode: Access
        access:
          id: 70
      subnets: ['10.70.0.0/24']
```

## Validation

Any invalid entry fails the hub template of the policy, with a message that names the entry and the
problem, for example `config.projectNetworks.vlan-70: vlan is required and must be an integer from 1
to 4094`. Nothing is applied or reported until it is fixed. An unknown key in an entry is an error,
so a misspelled key cannot be silently ignored. Two subnets of the same IP family, and a malformed
`matchExpressions` entry (unknown key, an operator other than `In`, `NotIn`, `Exists`, or
`DoesNotExist`, `values` that do not fit the operator, a `key` that is not a valid label key, or a
value that is not a string or not a valid label value), are errors too. So is a `matchLabels` key
that is not a valid label key (an optional DNS subdomain prefix and `/`, then a name of at most 63
characters) or a value that is not a valid label value (at most 63 characters, alphanumeric at both
ends, with `-`, `_`, and `.` inside). So is a `matchLabels` that is set but is not a map, or a
`matchExpressions` or expression `values` that is set but is not a list, such as `matchLabels:
false`: the type is checked before any default applies, and `null` counts as unset. So are a
`physicalNetworkName` with whitespace or control
characters, which would never match a bridge mapping, a `subnets` value that is set but not a list
(such as `""`, `{}`, `false`, or `0`), and a subnet that is not a valid IPv4 or IPv6 CIDR (such as
`10.70.0.0/33` or `10.70/24`). Strings containing `{{` or `}}` are rejected.

### Namespace selector guard

OVN-Kubernetes creates the NetworkAttachmentDefinition in every namespace that the selector matches,
and any user who can create a pod in one of them can attach it to the VLAN. A selector that matches
every namespace, `openshift-*` and `kube-system` included, is therefore refused rather than
documented as a risk: a mistake is reported on the hub before it reaches the cluster, and no valid
use needs such a selector.

- The selector needs at least one positive requirement: a `matchLabels` pair, an `In` expression, or
  an `Exists` expression. `Exists` on `kubernetes.io/metadata.name` does not count, because every
  namespace has that label. A selector made only of `NotIn` and `DoesNotExist` expressions is
  refused.
- A platform namespace as a `kubernetes.io/metadata.name` value in `matchLabels` or an `In`
  expression is refused: `kube-*`, `openshift`, `openshift-*`, `default`,
  `open-cluster-management*`, `multicluster-engine`, and `hive`.

The guard cannot see which labels the platform namespaces carry. Select on a label that only your
project namespaces have, such as one set by project onboarding, and not on a label that OpenShift
also sets on its own namespaces, such as `openshift.io/cluster-monitoring`.

## Ownership and changes

The enforcing Policy, `policy-project-networks`, applies an entry when no
ClusterUserDefinedNetwork of that name exists, or when the existing one carries
`autoshift.io/managed-by: project-networks` and its `spec.network` equals the entry's. The spec is
matched exactly, so a removed selector key goes away. Other labels and annotations are kept.

`spec.network` is immutable in OVN-Kubernetes. A changed `vlan`, `physicalNetworkName`, or `subnets`
for an existing network would be rejected on every evaluation, so the entry is held instead:
`policy-project-networks-test` renders the declared network, which is NonCompliant and shows the
difference, and the live network stays as it is. To change a network, declare it under a new entry
name and move the namespaces, or delete the old ClusterUserDefinedNetwork by hand once no workload
uses it and let the policy re-create it. The `namespaceSelector` can be changed in place. Narrowing
it, or removing the namespace label it matches, detaches the namespace from the VLAN: its
NetworkAttachmentDefinition is removed, and a restarted pod that still requests the network in its
`k8s.v1.cni.cncf.io/networks` annotation usually fails to start until the annotation is removed.

An existing ClusterUserDefinedNetwork without the managed-by label is never taken over. The entry is
held and reported the same way.

## Removal

Deletion is never automatic. A network with the managed-by label that is no longer declared is
reported by `policy-project-networks-test` under `stale.ClusterUserDefinedNetwork..<name>` of the
ConfigMap `project-networks-test-report`, which can never exist (its key `_unsatisfiable` is in both
`data` and `binaryData`, which the API server refuses), so it stays NonCompliant until the network is
deleted by hand or declared again. The test policy has no `mustnothave`, so even Enforce clicked on
it in the console deletes nothing. Deleting a ClusterUserDefinedNetwork that pods still use is blocked by
OVN-Kubernetes until they are gone.

## Prerequisites

- OpenShift with the ClusterUserDefinedNetwork `k8s.ovn.org/v1` API and its `Localnet` topology.
  Verified against the OpenShift 4.22 API. Without the CRD nothing is applied, and
  `policy-project-networks-test` reports the CRD as missing while entries are declared.
- An OVN bridge mapping for each `physicalNetworkName` on the nodes, from the `nmstate` policy.
- Each `vlan` carried as a tagged VLAN on the physical network behind the bridge mapping.

## Placement

Every cluster, hub or managed, with the label `autoshift.io/project-networks: 'true'`. Set the label
in the values files, never directly on a cluster. The Policies share `placement.yaml` and one
PlacementBinding, `binding-policy-project-networks`.

## Layout

```
project-networks/
  kustomization.yaml            # entry point: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator: policy graph, remediation, evaluation interval
  placement.yaml                # Placement predicate and tolerations
  manifests/
    project-networks.yaml       # object-templates-raw, $want enforce: policy-project-networks
  test/
    project-networks.yaml       # the same template, $want inform: policy-project-networks-test
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/project-networks

# Full validation: render, hub and spoke template resolution, label contract
make test
```
