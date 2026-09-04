# global-hub AutoShift Policy

Installs and configures **Multicluster Global Hub** — the Red Hat ACM component that
manages multiple ACM hub clusters from a single hub and aggregates their policy
compliance, cluster inventory, and alerts.

Global Hub is a **hub-of-hubs feature**: it runs on the top-level (self-managed) hub and
imports the spoke hubs beneath it as *managed hubs*. See
[docs/hub-of-hubs.md](../../../docs/hub-of-hubs.md) for the surrounding topology.

## What this chart deploys

| Policy | Runs on | Purpose |
|---|---|---|
| `policy-global-hub-operator-install` | the global hub | Installs the operator (namespace-scoped in `multicluster-global-hub`). **Depends on `policy-acm-mch-install`** — ACM must be installed first. |
| `policy-global-hub-instance` | the global hub | Creates the `MulticlusterGlobalHub` CR (the operand). The operator then deploys the manager, Grafana, and its own **built-in AMQ Streams (Kafka) and PostgreSQL**. |
| `policy-global-hub-managed-hubs` | the global hub | Stamps `global-hub.open-cluster-management.io/deploy-mode` onto every ManagedCluster that carries `autoshift.io/global-hub-deploy-mode`, importing those hubs into Global Hub. |

All three are placed by the `autoshift.io/global-hub: 'true'` label, so they land on the
hub whose clusterset enables Global Hub (the self-managed hub-of-hubs).

> **AMQ Streams / Kafka and PostgreSQL are installed and managed by the Global Hub
> operator itself** (the Strimzi subscription is labeled `managed-by: global-hub-operator`).
> Do **not** add a separate AMQ Streams operator policy — it would conflict.

## Enable it

Global Hub runs on the **self-managed hub** (the hub-of-hubs), so enable it there — not on
spoke/managed clustersets:

```yaml
# autoshift/values/clustersets/hubofhubs.yaml
hubClusterSets:
  hubofhubs:
    labels:
      global-hub: 'true'
      global-hub-channel: release-1.8
      # operand (MulticlusterGlobalHub CR) config:
      global-hub-availability-config: High        # High (default) or Basic
      global-hub-enable-metrics: 'true'
      global-hub-install-agent-on-local: 'true'   # global hub also manages its own clusters
      global-hub-postgres-retention: 18m
```

Then import the spoke hubs by setting `global-hub-deploy-mode` on **their** clustersets
(e.g. `hub1.yaml`, `hub2.yaml` in the hub-of-hubs values):

```yaml
# autoshift/values/clustersets/hub1.yaml
hubClusterSets:
  hub1:
    labels:
      global-hub-deploy-mode: 'default'   # 'default' or 'hosted'
```

Labels live in values files only — `cluster-labels` propagates them to the managed
clusters; `policy-global-hub-managed-hubs` then translates the AutoShift label into the
real `global-hub.open-cluster-management.io/deploy-mode` label the operator watches.

## Labels

| Label | Default | Purpose |
|---|---|---|
| `global-hub` | `false` | Enable the operator + CR on this hub |
| `global-hub-subscription-name` | `multicluster-global-hub-operator-rh` | OLM package |
| `global-hub-channel` | `release-1.8` | Operator channel |
| `global-hub-source` / `-source-namespace` | `redhat-operators` / `openshift-marketplace` | Catalog source |
| `global-hub-version` | _(unset)_ | Pin a CSV (sets manual approval) |
| `global-hub-availability-config` | `High` | `High` or `Basic` |
| `global-hub-enable-metrics` | `true` | Metrics for built-in Kafka/PostgreSQL |
| `global-hub-install-agent-on-local` | `true` | Install the Global Hub agent on the global hub itself |
| `global-hub-postgres-retention` | `18m` | Built-in PostgreSQL retention |
| `global-hub-consumer-group-prefix` | _(unset)_ | Optional Kafka consumer group prefix |
| `global-hub-strimzi-channel` | `amq-streams-3.1.x` | AMQ Streams channel (disconnected mirror) |
| `global-hub-deploy-mode` | _(unset)_ | Set on a **managed hub** clusterset to import it: `default` or `hosted` |

## Disconnected / mirrored environments

The operator pulls AMQ Streams from a catalog, so in a mirror you must:

1. **Mirror both packages** — `multicluster-global-hub-operator-rh` **and** `amq-streams`
   (add them to your `ImageSetConfiguration`).
2. Set `autoshift.io/disconnected-mirror: 'true'` (and `mirror-catalog-suffix`) on the hub.

When `disconnected-mirror` is true, `policy-global-hub-instance` adds the Strimzi
catalog-source annotations to the CR so the operator finds the mirrored AMQ Streams:

```yaml
global-hub.open-cluster-management.io/strimzi-catalog-source-name: <source>-<suffix>
global-hub.open-cluster-management.io/strimzi-catalog-source-namespace: openshift-marketplace
global-hub.open-cluster-management.io/strimzi-subscription-package-name: amq-streams
global-hub.open-cluster-management.io/strimzi-subscription-channel: amq-streams-3.1.x
```

In connected environments these annotations are omitted (the operator uses its defaults).

## Validate

```bash
helm template policies/stable/global-hub/
cd tools && go test -tags integration -count=1 ./internal/resolver/...
```

## Troubleshooting

| Symptom | Check |
|---|---|
| Operator never installs | `policy-acm-mch-install` Compliant? `oc get sub,csv -n multicluster-global-hub` |
| CR not created | `policy-global-hub-operator-install` Compliant first; `oc get multiclusterglobalhub -A` |
| Managed hub not imported | `oc get managedcluster <hub> --show-labels \| grep deploy-mode`; confirm `global-hub-deploy-mode` set on its clusterset |
| Kafka/Postgres pods missing | They are operator-managed: `oc get kafka,pods -n multicluster-global-hub` |

## Resources
- [docs/hub-of-hubs.md](../../../docs/hub-of-hubs.md) — hub-of-hubs topology
- [Red Hat: Multicluster global hub](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes/latest/html/multicluster_global_hub/index)
- [AutoShift Developer Guide](../../../docs/developer-guide.md)
