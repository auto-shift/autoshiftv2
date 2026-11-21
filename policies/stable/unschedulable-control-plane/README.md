# unschedulable-control-plane

Taints the control-plane nodes listed in `config.unschedulableControlPlane.nodes` with
`autoshift.io/unschedulable-control-plane:NoSchedule`, so pods without a matching toleration cannot
schedule there. Once the pods already running are moved, only pods that tolerate the taint can run
there: the control-plane static pods, the DaemonSets, and any other pod with a matching toleration. It
is for compact clusters where most control-plane nodes stay schedulable and one or more are dedicated to
the control plane, such as the
[dual-room ACP topology](https://github.com/RedHatEdge/patterns/tree/main/patterns/dual-room-acp-topology#approach-a-stretched-acp).

| Policy | What it does |
|---|---|
| `policy-unschedulable-control-plane` | Adds the taint to each listed node, and removes it from any node that carries it but is no longer listed |

## Why a custom taint key

On a compact cluster with `mastersSchedulable: true`, the OpenShift scheduler operator removes the
standard `node-role.kubernetes.io/control-plane:NoSchedule` taint. A policy that set the standard key
would fight the operator. The custom key avoids that.

Application pods without a toleration for the key no longer schedule on the tainted nodes. System
DaemonSets (OVN-Kubernetes, CSI plugins, the machine config daemon) and the control-plane static pods
tolerate every taint, so they keep running there.

## Moving pods that are already running

`NoSchedule` keeps new pods off a node; it does not evict pods that were running there before the taint
was applied. The policy reports Compliant as soon as the taint is set, so move those pods once,
after it is:

```bash
oc adm drain <node> --ignore-daemonsets --delete-emptydir-data
oc adm uncordon <node>
```

Run the uncordon only after the drain finishes successfully. The drain stops on a pod that no
controller manages (`--force` deletes such pods, and they are not recreated), and it waits while a
PodDisruptionBudget blocks an eviction. Uncordoning before it finishes leaves the remaining pods on the
node. See the Red Hat OpenShift Container Platform documentation on evacuating pods from nodes.

A completed drain evicts every pod that is not a static pod or part of a DaemonSet, and the scheduler
places their replacements wherever they fit. Static control-plane pods and DaemonSets stay. After the
uncordon, a replacement can come back to this node only if it tolerates the taint.

## Placement

`placement.yaml` selects clusters labeled `autoshift.io/unschedulable-control-plane: 'true'`.

## Configuration

Node names live in config rather than labels: a label value is limited to 63 characters, which a node
hostname can exceed. Node names are usually specific to one cluster, so set them in
`clusters/<name>.yaml`:

```yaml
clusters:
  my-cluster:
    labels:
      unschedulable-control-plane: 'true'
    config:
      unschedulableControlPlane:
        nodes:                                     # required, at least one, unless releaseAll
          - 'master-4.my-cluster.example.com'
        # releaseAll: true                         # ignore nodes; remove the taint from every node
```

`docs/values-reference.md` has a 5-node example that combines this policy with ODF storage nodes.

## Behavior

- **Adding the taint** uses `musthave`, which merges one entry into `spec.taints` and leaves every other
  taint alone. The node lifecycle controller also writes taints, so the steady state never rewrites the
  list.
- **Removing the taint.** A node that carries the taint but is no longer listed has its taint list
  rebuilt from the live Node without that entry, with the rest of its spec carried over, under
  `mustonlyhave`. `musthave` cannot drop an entry from a list. This runs only for nodes being released.
  `mustnothave` is never used, because under enforce it would delete the Node.
- **Releasing every node.** An empty `nodes` list fails, so the last tainted node cannot be released by
  emptying the list. Set `releaseAll: true` instead: the policy ignores `nodes` and removes the taint
  from every node that carries it, then has nothing left to do and reports Compliant. It overrides an
  inherited `nodes` list rather than requiring it to be empty, because config is deep-merged and an
  empty list at the cluster level does not replace the clusterset's.
- **Disabling the policy** leaves existing taints in place. To stop using it: set `releaseAll: true`,
  wait for the taints to be removed, then set the `unschedulable-control-plane` label to `'false'` and
  remove the config.

The policy fails with a message naming the cause, rather than reporting Compliant with nothing applied
or creating anything, when:

- `config.unschedulableControlPlane.nodes` is missing or empty and `releaseAll` is not set, or it
  contains an empty or repeated name
- a listed Node does not exist. Without this check, `musthave` would create a bare Node object with that
  name
- a listed Node is not a control-plane node
- the cluster has no non-control-plane node and every control-plane node would be tainted, which would
  leave nowhere to schedule application workloads

## Layout

```
unschedulable-control-plane/
  kustomization.yaml            # entry point: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator: policy graph, remediation, evaluation interval
  placement.yaml                # Placement predicate and tolerations
  manifests/
    unschedulable-control-plane-taint.yaml   # adds and removes the taint
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/unschedulable-control-plane

# Full validation: render, hub and spoke template resolution, label contract
make test
```

`tools/testdata/unschedulable-control-plane-nodes.yaml` stubs a second control-plane node that carries
the taint but is not in the example config, so the suite renders the removal path.

## Verify

```bash
oc get nodes -o custom-columns='NAME:.metadata.name,TAINTS:.spec.taints[*].key'
```
