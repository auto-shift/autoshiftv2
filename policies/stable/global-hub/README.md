# global-hub

Installs and configures multicluster global hub, the Red Hat Advanced Cluster Management component
that manages several Red Hat Advanced Cluster Management hubs from one hub and gathers their policy
compliance, cluster inventory and alerts.

Global hub runs on the top-level hub of a hub of hubs and imports the hubs beneath it as managed
hubs. See [hub of hubs](../../../docs/hub-of-hubs.md) for the surrounding topology.

## What it deploys

| Policy | Runs on | Purpose |
|---|---|---|
| `policy-global-hub-operator-install` | The global hub | Installs the operator in `multicluster-global-hub`. Depends on `policy-acm-mch-install` |
| `policy-global-hub-instance` | The global hub | Creates the `MulticlusterGlobalHub` resource. The operator then deploys the manager, Grafana, and its own AMQ Streams (Kafka) and PostgreSQL |
| `policy-global-hub-managed-hubs` | The global hub | Copies `autoshift.io/global-hub-deploy-mode` from each `ManagedCluster` to `global-hub.open-cluster-management.io/deploy-mode`, the label that imports a hub |

All three are placed by the `global-hub: 'true'` label. Do not add a separate AMQ Streams operator
policy: the global hub operator installs and manages AMQ Streams itself, and a second policy would
conflict with it.

## Enable it

Enable global hub on the hub of hubs. Settings are config, and only the enable label and the
operator install labels are labels:

```yaml
# autoshift/values/clustersets/hubofhubs.yaml
hubClusterSets:
  hubofhubs:
    config:
      global-hub:
        availabilityConfig: High
        enableMetrics: 'true'
        installAgentOnLocal: 'true'
        postgresRetention: 18m
    labels:
      global-hub: 'true'
      global-hub-channel: release-1.8
```

Then import each managed hub by setting `global-hub-deploy-mode` on its own clusterset:

```yaml
# autoshift/values/clustersets/hub1.yaml
hubClusterSets:
  hub1:
    labels:
      self-managed: 'false'
      global-hub-deploy-mode: 'default'
```

### Import mode

AutoShift supports default mode only. The managed hub must run with self-management off,
`self-managed: 'false'`, as every hub-of-hubs managed hub already does, and the chart refuses
`global-hub-deploy-mode: 'default'` without it.

The chart refuses Red Hat's `hosted` mode. Hosted mode lets a managed hub keep its own
`local-cluster`, but it disables the `policyController`, `certPolicyController` and
`applicationManager` add-ons for that hub, so the hub of hubs could no longer place AutoShift
policies on it. It also needs a `kubeconfig` import secret kept valid by hand, and has no global
hub backup and restore.

After an upgrade of global hub, Red Hat documents adding the deploy-mode label back to each
managed hub by hand. `policy-global-hub-managed-hubs` enforces that label continuously, so it is
added back without any manual step.

### Stop importing a managed hub

Removing `global-hub-deploy-mode` from a managed hub's clusterset stops AutoShift enforcing the
import. It does not detach the hub: `policy-global-hub-managed-hubs` never removes the operator's
`global-hub.open-cluster-management.io/deploy-mode` label, so the hub stays imported. Detach it
explicitly, on the global hub:

```bash
oc label managedcluster <hub> global-hub.open-cluster-management.io/deploy-mode-
```

Red Hat does not document a detach procedure for global hub 1.8. Check that the global hub agent
add-on is removed from the hub afterwards, with
`oc get managedclusteraddon -n <hub> | grep global-hub`.

## Config

`config.global-hub`, in the rendered config of the global hub. Every key is optional.

| Key | Default | Purpose |
|---|---|---|
| `availabilityConfig` | `High` | `High` or `Basic` |
| `enableMetrics` | `true` | Metrics for the built-in Kafka and PostgreSQL. `true` or `false`, quoted or not |
| `installAgentOnLocal` | `true` | Install the global hub agent on the global hub itself. `true` or `false`, quoted or not |
| `postgresRetention` | `18m` | Built-in PostgreSQL retention |
| `consumerGroupPrefix` | `''` | Kafka consumer group prefix. Always written, so clearing it removes a prefix set earlier |
| `strimziChannel` | `amq-streams-3.1.x` | AMQ Streams channel, used on a disconnected hub |

A missing rendered config, or one without a `config` key, fails `policy-global-hub-instance`,
rather than creating the instance from defaults. So does a boolean setting that is not `true` or
`false`.

## Labels

| Label | Default | Purpose |
|---|---|---|
| `global-hub` | `false` | Install global hub on this hub |
| `global-hub-subscription-name` | `multicluster-global-hub-operator-rh` | Operator package |
| `global-hub-channel` | `release-1.8` | Operator channel |
| `global-hub-source`, `global-hub-source-namespace` | `redhat-operators`, `openshift-marketplace` | Catalog source |
| `global-hub-version` | Unset | Pin a CSV, which sets manual approval |
| `global-hub-deploy-mode` | Unset | On a managed hub clusterset: `default` imports it |

## Observability

Red Hat Advanced Cluster Management observability is supported on the global hub and shows the
health of the managed hubs. Enable `acm-observability` before global hub where you can: Red Hat
documents that later updates to a `MultiClusterObservability` enabled after global hub was
installed do not take effect.

## Disconnected

1. Mirror both `multicluster-global-hub-operator-rh` and `amq-streams`.
2. Set `disconnected-mirror: 'true'` on the global hub.

With `disconnected-mirror: 'true'`, `policy-global-hub-instance` adds the annotations that point the
operator at the mirrored AMQ Streams catalog:

```yaml
global-hub.open-cluster-management.io/strimzi-catalog-source-name: <source>-<suffix>
global-hub.open-cluster-management.io/strimzi-catalog-source-namespace: openshift-marketplace
global-hub.open-cluster-management.io/strimzi-subscription-package-name: amq-streams
global-hub.open-cluster-management.io/strimzi-subscription-channel: amq-streams-3.1.x
```

On a FIPS cluster, the Kafka operator can run out of memory. Red Hat documents raising its memory
limit to at least 512M.

## Troubleshooting

| Symptom | Check |
|---|---|
| The operator does not install | `policy-acm-mch-install` is Compliant; `oc get sub,csv -n multicluster-global-hub` |
| The instance is not created | `policy-global-hub-operator-install` is Compliant; `oc get multiclusterglobalhub -A` |
| A managed hub is not imported | `oc get managedcluster <hub> --show-labels`, and `global-hub-deploy-mode` is set on its clusterset |
| A default-mode import does not complete | The managed hub has `self-managed: 'false'`; `oc get managedclusteraddon multicluster-global-hub-controller -n <hub>` |
| Kafka or PostgreSQL pods are missing | The operator manages them: `oc get kafka,pods -n multicluster-global-hub` |

## Resources

- [Hub of hubs](../../../docs/hub-of-hubs.md)
- [Red Hat: multicluster global hub](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes/2.17/html/multicluster_global_hub/index)
- [Developer guide](../../../docs/developer-guide.md)
