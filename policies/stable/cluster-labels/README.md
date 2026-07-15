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
   Each `cluster-set.<name>` ConfigMap also carries a `cluster-set-type` data key (`hub` for
   `hubClusterSets`, `spoke` for `managedClusterSets`, the `type` field — default `spoke` —
   for `clusterSets`) that feeds the `cluster-type` derivation below.
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

| Label | Applied to | Value |
|---|---|---|
| `autoshift.io/owning-namespace` | clusters + clustersets | policy namespace of the deployment whose config claims the clusterset |
| `autoshift.io/owning-deployment` | clusters + clustersets | the owning namespace minus its `policies-` prefix |
| `autoshift.io/cluster-type` | clusters | `selfManagedHub` \| `managedHub` \| `spoke` |
| `autoshift.io/cluster-set-type` | clustersets | `hub` \| `spoke` (echoed onto the `ManagedClusterSet` by `policy-cluster-set-labels.yaml`) |

`cluster-type` derivation: every `cluster-set.<name>` ConfigMap carries a `cluster-set-type`
data key (`hub` for `hubClusterSets`, `spoke` for `managedClusterSets`, the `type` field for
`clusterSets`). This policy reads it during its runtime ConfigMap sweep — deployments are
templated independently and never see each other's values, but the cluster-wide
label-selected lookup sees every deployment's CMs — and types each member cluster from its
set: `hub`-set members split into `selfManagedHub`/`managedHub` on the set's `self-managed`
label (checked against the labels being applied in the same pass, so the type never lags a
config change); `spoke`-set members are `spoke`. The derived value is written after the
user-label merge, so a `cluster-type` declared in values is ignored. Clusters in sets without
a `cluster-set-type` get no `cluster-type`.

## Policies rendered

| Template | What it renders |
|---|---|
| `config-maps.yaml` | the `cluster-set.*` / `managed-cluster.*` label ConfigMaps |
| `policy-cluster-labels.yaml` | `policy-selfmanagedhub-labels` and/or `policy-managedhub-labels` (one per hub flavor present in `hubClusterSets`). The managed-hub variant depends on `policy-managed-hub-namespace`. |
| `policy-cluster-set-labels.yaml` | `policy-selfmanagedhub-cluster-set-labels` and/or `policy-managedhub-cluster-set-labels` (same hub-flavor split and dependency pattern as above): rewrites each `ManagedClusterSet`'s labels from the same sources as the cluster policy, minus the `managed-cluster.*` ConfigMaps — the set's declared labels (tombstones suppressed), plus derived `owning-namespace` / `owning-deployment` / `cluster-set-type`, plus existing non-autoshift labels. Sets with no registered configuration get every `autoshift.io/*` label stripped. |
| `policysets.yaml` | all placement for the two policies above: one `policyset-<flavor>-cluster-labels` PolicySet per hub flavor grouping both policies, bound to one shared `placement-<flavor>-cluster-labels` Placement on the matching sets. The policy files render no Placement/PlacementBinding of their own. |
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
