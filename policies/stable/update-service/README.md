# OpenShift Update Service

Installs the OpenShift Update Service Operator and an `UpdateService` that serves the OpenShift
update graph from a mirror registry. A disconnected cluster cannot read Red Hat's public graph, so
without this service `ClusterVersion` lists no updates and `openshift-upgrade` has nothing to apply.

Run it on the hub, or on each managed hub in a hub of hubs. Disconnected clusters find it on their
own: the `openshift-upgrade` channel policy uses a service on the cluster itself, then one on the
hub that manages it. The full disconnected upgrade flow, including release signatures and hub of
hubs layouts, is in
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

Read from `config.update-service` in the cluster's rendered-config ConfigMap. Every key is optional.
`releases` and `graphDataImage` default to the paths oc-mirror v2 uses under
`config.disconnected.mirrorRegistry`. Set them only for another layout, copying the spec of the
`updateService.yaml` oc-mirror writes to `working-dir/cluster-resources/`. The policy fails when
neither the keys nor `mirrorRegistry.host` is set.

```yaml
config:
  update-service:
    releases: 'registry.example.com:5000/openshift/release-images'           # default <host>[/<path>]/openshift/release-images
    graphDataImage: 'registry.example.com:5000/openshift/graph-image:latest'  # default <host>[/<path>]/openshift/graph-image:latest
    replicas: 2          # default 2; 2 or more adds a PodDisruptionBudget
    name: 'autoshift'    # default autoshift; also names the Route that clusters find
```

## Mirror registry trust

The operator reads the mirror registry CA from the key `updateservice-registry` in the ConfigMap that
`image.config.openshift.io/cluster` names in `additionalTrustedCA`. The `disconnected-mirror` policy
writes that key into `openshift-config/autoshift-registry-ca` whenever
`config.disconnected.mirrorRegistry` has a `ca` or `caRef`. A mirror with a publicly trusted
certificate needs neither.

## Graph URL

The service is served through the Route `<name>-route` in `openshift-update-service`. Clusters find it
by that name, so set `config.update-service.name` where every cluster sees it if you change it. To
check the URL:

```bash
oc get updateservice -n openshift-update-service autoshift -o jsonpath='{.status.policyEngineURI}'
```

`config.openshift-upgrade.upgradeGraphUrl` overrides the lookup for a cluster that should read a
different service. A cluster that does not trust the Route certificate cannot read the graph.
