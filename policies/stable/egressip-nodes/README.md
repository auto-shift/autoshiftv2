# egressip-nodes

Makes nodes assignable for EgressIP objects. OVN-Kubernetes assigns an EgressIP address only to a
node that carries the label `k8s.ovn.org/egress-assignable`; without one, every EgressIP stays
unassigned and the pods it selects keep leaving the cluster from their node address. This policy
adds that label, with an empty value, to the nodes selected in `config.egressipNodes`. Use it with
any feature that creates EgressIP objects, such as the EgressIP of project onboarding.

OVN-Kubernetes has one more requirement that this policy cannot check: an EgressIP address must be
in the node subnet of an assignable node, the subnet of the network interface that holds the node
IP address. An address outside every assignable node subnet is never assigned. Select nodes on the
network your EgressIP addresses come from.

## Policies

| Policy | Remediation | What it does |
|---|---|---|
| `policy-egressip-nodes` | `${REMEDIATION}`: enforce, or inform under `autoshift.dryRun` | Adds `k8s.ovn.org/egress-assignable` to the selected nodes from `manifests/egressip-nodes.yaml`, and writes the `egressip-nodes-status` ConfigMap while there is something to report |
| `policy-egressip-nodes-test` | Always inform | From `test/egressip-nodes-status.yaml`: asserts that the status ConfigMap is absent. It never changes anything |

The inform-only check is a policy of its own, as the [developer
guide](../../../docs/developer-guide.md#gating-on-cluster-state) requires: a `remediationAction` on
the root of one Policy overrides all of its children, so a check bundled into the enforcing Policy
would start changing what it should only report.

`policy-egressip-nodes-test` is Compliant only when every selected node exists and carries the
label, the selector matches a node, and no labeled node is left unselected, so depend on it, not on
`policy-egressip-nodes`, when another policy needs that state. `policy-egressip-nodes` reports
Compliant once its labels and its status ConfigMap are written, even when the status lists errors.

Neither Policy depends on the other:

- `policy-egressip-nodes` must not depend on `policy-egressip-nodes-test`. The test policy is
  NonCompliant exactly while the enforcing policy has work to do or errors to report, so the
  enforcing policy would never run.
- `policy-egressip-nodes-test` does not depend on `policy-egressip-nodes`. A dependency would hold
  the test policy Pending whenever the enforcing one is NonCompliant, which under dry run is
  whenever anything would change, and would hide its reports exactly when they are wanted.

## Enable

| Label | Default | Description |
|---|---|---|
| `autoshift.io/egressip-nodes` | `'false'` | `'true'` labels the nodes in `config.egressipNodes` |

Set it in a clusterset or cluster values file, without the `autoshift.io/` prefix. Never put it on
a ManagedCluster by hand.

## Configuration

```yaml
config:
  egressipNodes:
    worker-0.example.com: true     # a node name and true or false
    worker-1.example.com: false    # never selected, even when nodeSelector matches it
    nodeSelector:                  # optional: also select every node with all of these labels
      matchLabels:
        node-role.kubernetes.io/worker: ''
```

- `<node>`: a node name, a DNS-1123 subdomain, set to `true` or `false`. `true` selects the node.
  `false` never selects it, so a cluster file can exclude a node that a clusterset file selects.
- `nodeSelector.matchLabels`: optional. Selects every node with all of these labels. It must not
  be empty. Quote label values, for example `''` or `'true'`. A selector that matches every node
  also selects control plane nodes on clusters that have separate ones, so choose labels that only
  your egress nodes carry.

| Key | Type | Default | Example | Invalid input |
|---|---|---|---|---|
| `<node>` | `true` or `false`, keyed by a node name (DNS-1123 subdomain) | not selected | `worker-0.example.com: true` | Hub template fails, nothing applied |
| `nodeSelector.matchLabels` | non-empty map of label key to label value | no selector | `node-role.kubernetes.io/worker: ''` | Empty, unknown sibling key, or invalid label: hub template fails |

`config.egressipNodes` is a map, not a list, because AutoShift replaces lists whole when it merges
values files. The key `nodeSelector` is reserved and cannot clash with a node name, which has no
upper-case letters.

Each selected node that exists gets this, and nothing else of the Node is changed (`musthave`):

```yaml
apiVersion: v1
kind: Node
metadata:
  name: worker-0.example.com
  labels:
    k8s.ovn.org/egress-assignable: ""
```

## Validation

Any invalid key or value fails the hub template of the policy, with every error listed and each
naming its key, for example `config.egressipNodes.worker-0.example.com: value is not true or
false`. Nothing is applied until it is fixed. A node name that is not a DNS-1123 subdomain, a value
that is not a boolean, a `nodeSelector` with a key other than `matchLabels`, an empty
`matchLabels`, and an invalid label key or value are errors. A string with `{{` or `}}` is never a
valid name or label, so configuration cannot inject a template. Validated data reaches the managed
cluster only as a quoted JSON string.

## Reports

The policy renders only nodes that its lookup on the managed cluster found, so a missing node is
reported instead of created. The one exception is a node deleted in the moment between that lookup
and the controller's own check: the controller then re-creates a bare Node object with only its
name and this label, which is reported nowhere and can be deleted by hand.

These are reported in the ConfigMap `egressip-nodes-status` in
`open-cluster-management-agent-addon`, one data key per node:

- a node set to `true` that does not exist;
- a node that carries `k8s.ovn.org/egress-assignable` but is not selected, including one labeled
  by hand before the policy was enabled;
- the key `nodeSelector`, when the selector matches no node.

`policy-egressip-nodes` writes the ConfigMap while there is something to report and deletes it
otherwise. The inform-only `policy-egressip-nodes-test` asserts that the ConfigMap is absent, so it
is NonCompliant while a report stands. Read the reports with:

```bash
oc get configmap egressip-nodes-status -n open-cluster-management-agent-addon -o yaml
```

## Removing the label

The policy never removes the label: deletion is a manual decision. Removing a node from
`config.egressipNodes`, or setting it to `false`, leaves the label in place and reports the node.
OVN-Kubernetes moves the EgressIP addresses of a node that loses the label to another assignable
node, so check that one is left first, then remove the label:

```bash
oc label node worker-0.example.com k8s.ovn.org/egress-assignable-
```

## Dry run

With `autoshift.dryRun`, the policy changes nothing. It reports each selected node that lacks the
label, and the status ConfigMap it would write, with the reports in its data.

## Placement

Every cluster, hub or managed, with the label `autoshift.io/egressip-nodes: 'true'`. Set the label
in the values files, never directly on a cluster. The Policies share `placement.yaml` and one
PlacementBinding, `binding-policy-egressip-nodes`.

## Layout

```
egressip-nodes/
  kustomization.yaml            # entry point: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator: policy graph, remediation, evaluation interval
  placement.yaml                # Placement predicate and tolerations
  manifests/
    egressip-nodes.yaml         # object-templates-raw, $want enforce: policy-egressip-nodes
  test/
    egressip-nodes-status.yaml  # the status ConfigMap, mustnothave: policy-egressip-nodes-test
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/egressip-nodes

# Full validation: render, hub and spoke template resolution, label contract
make test
```
