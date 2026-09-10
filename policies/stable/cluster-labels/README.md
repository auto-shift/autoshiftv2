# cluster-labels

The label engine of AutoShift: it propagates the labels declared in the values repo onto the
`ManagedCluster` objects on each hub, where every other AutoShift policy reads them (placement
selectors, hub-template `.ManagedClusterLabels` lookups). Policies are label-triggered plug-in
modules; this chart is the label writer that drives them.

## How it works

1. **Values → ConfigMaps** (`config-maps.yaml`, Helm): each deployment materializes its own
   clusterset and per-cluster label declarations as ConfigMaps in its policy namespace —
   `cluster-set.<name>` and `managed-cluster.<name>`, labelled `autoshift.io/cluster-labels`.
   Label keys are prefixed with `autoshift.io/` (configurable via `autoshiftLabelPrefix`);
   blank values are stored as the tombstone `_`, which suppresses the label at render time.
2. **ConfigMaps → ManagedCluster labels** (`policy-cluster-labels.yaml`, ACM runtime): a policy
   placed on every hub clusterset enumerates ALL ManagedClusters on that hub, gathers the
   label ConfigMaps from ALL namespaces (so every deployment's declarations are honored —
   this is what makes multi-deployment work), and rewrites each cluster's labels with
   `mustonlyhave`. Two configurations claiming the same clusterset fail loudly.

Per cluster, the applied set is: **cluster labels > clusterset labels > existing
non-autoshift labels** — plus the derived system labels below. A cluster whose clusterset has
no registered configuration gets every `autoshift.io/*` label stripped and is otherwise left
alone.

## Derived system labels (never set these in values)

| Label | Value | Derived where |
|---|---|---|
| `autoshift.io/owning-namespace` | policy namespace of the deployment whose config claims the clusterset | ACM runtime, this chart |
| `autoshift.io/owning-deployment` | the owning namespace minus its `policies-` prefix | ACM runtime, this chart |
| `autoshift.io/cluster-type` | `hub` \| `spoke` | Helm render, top-level `autoshift` chart |

`cluster-type` is a property of which bucket a clusterset is declared in, so it is resolved at
Helm render time: `autoshift/templates/cluster-labels-configmaps.yaml` appends
`cluster-type: 'hub'` to every `hubClusterSets` entry's labels and `cluster-type: 'spoke'` to
every `managedClusterSets` entry's, after the declared labels so a values-file value cannot
win. From there it rides the normal clusterset-label path onto member clusters — this chart
treats it like any other declared label.

There is no separate "self-managed hub" value. A self-managed hub is `cluster-type: hub`
combined with the existing `self-managed: 'true'` label; consumers match on both. Declaring
`self-managed` under `managedClusterSets` fails the render
(`autoshift/templates/_validate-clustersets.tpl`).

A per-cluster entry under `clusters` can still override `cluster-type`, since cluster labels
outrank clusterset labels. Nothing needs that — don't.

## Policies rendered

| Template | What it renders |
|---|---|
| `config-maps.yaml` | the `cluster-set.*` / `managed-cluster.*` label ConfigMaps |
| `policy-cluster-labels.yaml` | `policy-selfmanagedhub-labels` and/or `policy-managedhub-labels` (one per hub flavor present in `hubClusterSets`), each with its own Placement and PlacementBinding. The managed-hub variant depends on `policy-managed-hub-namespace`. |
| `policy-check-policy-namespace.yaml` | inform-only guard on managed hubs: flags NonCompliant when the policy namespace carries `autoshift.io/createdByAutoshift` (i.e. it must be admin-created, not autoshift-created) |
| `policy-cluster-labels-debug.yaml` | (`debug: true`) writes the computed label sets to a `cluster-set-<name>-lookup-debug` ConfigMap instead of only applying them |

## Values

| Key | Default | Purpose |
|---|---|---|
| `enabled` | `true` | render the label ConfigMaps |
| `debug` | `false` | render the debug policy |
| `autoshiftLabelPrefix` | `autoshift.io/` | prefix applied to all declared label keys |
| `policy_namespace` | `open-cluster-policies` | namespace for policies + label ConfigMaps |
| `hubClusterSets` / `managedClusterSets` / `clusters` | — | label declarations (see `autoshift/values/clustersets/_example.yaml`) |
