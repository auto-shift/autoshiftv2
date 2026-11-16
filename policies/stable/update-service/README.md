# OpenShift Update Service

Installs the OpenShift Update Service Operator and an `UpdateService` that serves the OpenShift
update graph from a mirror registry. A disconnected cluster cannot read Red Hat's public graph, so
without this service `ClusterVersion` lists no updates and `openshift-upgrade` has nothing to apply.

Run it on the hub. Point each cluster at it through `config.openshift-upgrade.upgradeGraphUrl`. The
full disconnected upgrade flow, including release signatures, is in
[Disconnected clusters](../../../docs/ocp-upgrade.md#disconnected-clusters).

## What it deploys

| Policy | Creates |
|---|---|
| `policy-update-service-operator-install` | Namespace `openshift-update-service`, its `OperatorGroup` (`OwnNamespace`) and the `cincinnati-operator` subscription |
| `policy-update-service-config` | `UpdateService` from `config.update-service`, in `openshift-update-service`. Depends on the install |
| `policy-update-service-ready` | Inform only. Reports an `UpdateService` whose `ReconcileCompleted` condition is not `True` |

## Placement

Every cluster labeled `autoshift.io/update-service: 'true'`.

## Labels

| Label | Default | Description |
|---|---|---|
| `autoshift.io/update-service` | `'false'` | Places these policies |
| `autoshift.io/update-service-subscription-name` | `cincinnati-operator` | Operator package |
| `autoshift.io/update-service-channel` | `v1` | Operator channel |
| `autoshift.io/update-service-source` | `redhat-operators` | Catalog source. Becomes `<source>-<mirror-catalog-suffix>` on a `disconnected-mirror` cluster |
| `autoshift.io/update-service-source-namespace` | `openshift-marketplace` | Catalog source namespace |
| `autoshift.io/update-service-version` | unset | Pins the operator to one CSV |

## Config

Read from `config.update-service` in the cluster's rendered-config ConfigMap. Copy `releases` and
`graphDataImage` from the spec of the `updateService.yaml` that oc-mirror writes to
`working-dir/cluster-resources/` when the `ImageSetConfiguration` sets `graph: true`, or print them
with `scripts/oc-mirror-to-values.sh`. The policy fails, naming the key, when either is missing.

```yaml
config:
  update-service:
    releases: 'registry.example.com:5000/openshift/release-images'           # required
    graphDataImage: 'registry.example.com:5000/openshift/graph-image:latest'  # required
    replicas: 2          # optional, default 2; 2 or more adds a PodDisruptionBudget
    name: 'autoshift'    # optional, default autoshift; also names the Route
```

## Mirror registry trust

The operator reads the mirror registry CA from the key `updateservice-registry` in the ConfigMap that
`image.config.openshift.io/cluster` names in `additionalTrustedCA`. The `disconnected-mirror` policy
writes that key into `openshift-config/autoshift-registry-ca` whenever
`config.disconnected.mirrorRegistry` has a `ca` or `caRef`. A mirror with a publicly trusted
certificate needs neither.

## Graph URL

The service is served through a Route in `openshift-update-service`. Its URL is in the `UpdateService`
status:

```bash
oc get updateservice -n openshift-update-service autoshift -o jsonpath='{.status.policyEngineURI}'
```

Set `config.openshift-upgrade.upgradeGraphUrl` on each cluster to that URL plus
`/api/upgrades_info/v1/graph`. A cluster that does not trust the Route certificate cannot read the
graph.
