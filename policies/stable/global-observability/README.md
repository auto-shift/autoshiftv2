# Global Observability Policy

Adds a three-tier metrics rollup — **global hub → intermediate hubs → workload clusters** — on top of ACM MultiCluster Observability (MCO). Patches MCOA-managed `PrometheusAgent` templates on intermediate hubs so every workload cluster remote-writes its metrics directly to the global hub's Observatorium over mTLS, in addition to its local hub.

This chart owns only the global-rollup deltas. The base MCO lifecycle (namespace, pull secret, `thanos-object-storage`, the `MultiClusterObservability` CR with retention/storage settings) is owned by `policy-acm-observability` in the `advanced-cluster-management` chart.

See [architecture.md](architecture.md) for how the rollup works and why.

## Policies

| Policy | Runs on | Description |
|--------|---------|-------------|
| `policy-global-observability-mcoa` | All global-obs hubs | `musthave`-patches the `capabilities` block (platform analytics/logs/metrics, user-workload logs/metrics/traces) onto the existing `MultiClusterObservability` CR |
| `policy-global-observability-secrets` | Global hub only | Assembles the coalesced `global-observability-secrets` Secret (mTLS client cert + CA + Observatorium URL) in the policy namespace |
| `policy-global-observability-prometheus-exists` | Intermediate hubs | Inform-only gate: verifies the MCOA-created `PrometheusAgent` templates exist before patching |
| `policy-global-observability-prometheus` | Intermediate hubs | Stages the rollup secret (and any additional remote-write secrets) into the observability namespace and patches the `PrometheusAgent` templates with `spec.secrets` + `spec.remoteWrite` entries |

## PolicySets and Placement

| PolicySet | Policies | Placement criteria |
|-----------|----------|--------------------|
| `policyset-global-observability` | `*-mcoa` | `global-observability: 'true'` |
| `policyset-global-observability-secrets` | `*-secrets` | `global-observability: 'true'` AND `self-managed: 'true'` |
| `policyset-global-observability-prometheus` | `*-prometheus-exists`, `*-prometheus` | `global-observability: 'true'` AND `self-managed: 'false'` |

## Dependencies

| Policy | Depends on |
|--------|-----------|
| `policy-global-observability-mcoa` | `policy-acm-observability` |
| `policy-global-observability-secrets` | `policy-global-observability-mcoa` |
| `policy-global-observability-prometheus-exists` | `policy-global-observability-mcoa` |
| `policy-global-observability-prometheus` | `policy-global-observability-mcoa`, `policy-coo-operator-install`, `policy-global-observability-prometheus-exists` |

## Labels

All labels are prefixed with `autoshift.io/`.

| Label | Type | Default | Description |
|-------|------|---------|-------------|
| `global-observability` | bool | `'false'` | Enable this chart on a hub cluster |
| `self-managed` | bool | — | Discriminator: `'true'` = global hub (manages itself), `'false'` = intermediate hub (managed by the global hub). Drives placement and skip-on-global-hub logic. |

All other behavior is configured through the rendered-config ConfigMap (`config:` block in values files), not labels.

## Configuration (`config.globalObservability.*` in rendered-config)

Read by hub templates from `<cluster>.rendered-config`, falling back to chart `values.yaml` defaults.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `capabilities.platformAnalytics` | string bool | `"true"` | Incident detection + namespace/virtualization right-sizing |
| `capabilities.platformLogs` | string bool | `"true"` | Platform log collection |
| `capabilities.platformMetrics` | string bool | `"true"` | Platform metrics (default + UI) |
| `capabilities.userWorkloadLogs` | string bool | `"true"` | User-workload log collection (ClusterLogForwarder) |
| `capabilities.userWorkloadMetrics` | string bool | `"true"` | User-workload metrics |
| `capabilities.userWorkloadTraces` | string bool | `"true"` | User-workload trace collection + instrumentation |
| `scrapeInterval` | string | `300s` | `PrometheusAgent` scrape interval |
| `logLevel` | string | `warn` | `PrometheusAgent` log level |
| `additionalRemoteWrites` | list | `[]` | Extra remote-write targets (see below) |

### `additionalRemoteWrites[]`

Optional extra remote-write targets added alongside the built-in global rollup. Each entry:

| Key | Required | Default | Description |
|-----|----------|---------|-------------|
| `name` | yes | — | Remote-write entry name |
| `url` | yes | — | Remote-write endpoint URL |
| `caFile` / `certFile` / `keyFile` | yes | — | TLS file paths inside the agent pod (`/etc/prometheus/secrets/<secret-name>/...`) |
| `remoteTimeout` | no | `30s` | Remote-write timeout |
| `onSelfManagedHub` | no | `false` | `false`: emitted on intermediate hubs only; `true`: emitted on every hub |
| `secretRef.name` / `secretRef.namespace` | no | — | Hub secret replicated into the observability namespace via `copySecretData` and mounted into the agent |

Keep secret names short and alphanumeric-terminated: MCOA generates `secret-<name>` volume names truncated to 63 chars, and a cut landing on a non-alphanumeric character silently breaks the agent StatefulSet.

## Chart Values (`globalObservability.*`)

| Value | Default | Description |
|-------|---------|-------------|
| `namespace` | `open-cluster-management-observability` | Hub observability namespace |
| `capabilities.*` | all `"true"` | Defaults for the capability toggles above |
| `scrapeInterval` / `logLevel` | `300s` / `warn` | Defaults for the agent settings above |
| `spokeAgent.prometheusAgentNames` | `[mcoa-default-platform-metrics-collector-global, mcoa-default-user-workload-metrics-collector-global]` | MCOA `PrometheusAgent` templates to patch |
| `spokeAgent.globalHubRollup.name` | `acm-global-observability` | Built-in rollup remote-write entry name |
| `spokeAgent.globalHubRollup.secretName` | `global-observability-secrets` | Coalesced rollup secret name |
| `spokeAgent.globalHubRollup.secretNamespace` | `policies-autoshift` | Namespace of the coalesced secret on the global hub (see caveat below) |
| `spokeAgent.globalHubRollup.remoteTimeout` | `30s` | Rollup remote-write timeout |
| `spokeAgent.globalHubRollup.caFile` / `certFile` / `keyFile` | `/etc/prometheus/secrets/global-observability-secrets/{ca.crt,tls.crt,tls.key}` | Mount paths for the rollup mTLS files |

> **Caveat — `secretNamespace` must match the policy namespace.** The secrets policy writes into `policy_namespace` (computed by the ApplicationSet as `policies-<release-name>`, default `policies-autoshift`), but the rollup reads from the hardcoded `secretNamespace` default. If the AutoShift Application is released under any other name, override `spokeAgent.globalHubRollup.secretNamespace` to match or the rollup secret copy silently fails.

## Prerequisites

- `acm-observability: 'true'` on every participating hub — `policy-acm-observability` must be Compliant (base MCO CR + secrets)
- `coo: 'true'` on every participating hub — Cluster Observability Operator runs the `PrometheusAgent`s
- Do **not** set `acm.observability.enableMCOA: true` in rendered-config when using this chart's capability toggles — both policies `musthave`-patch `spec.capabilities`, and an additive patch cannot remove capabilities that `enableMCOA` already added. Leave it unset so this chart is the sole capabilities manager.
- For `additionalRemoteWrites` with `secretRef`: the referenced secret must exist on the global hub in the given namespace

## Examples

### Global hub (self-managed)

```yaml
# autoshift/values/clustersets/<global-hub>.yaml
selfManagedHubSet: hubofhubs
hubClusterSets:
  hubofhubs:
    labels:
      self-managed: 'true'
      acm-observability: 'true'
      coo: 'true'
      global-observability: 'true'
    config:
      globalObservability:
        scrapeInterval: '300s'
        logLevel: 'warn'
        capabilities:
          platformAnalytics: 'true'
          platformLogs: 'true'
          platformMetrics: 'true'
          userWorkloadLogs: 'true'
          userWorkloadMetrics: 'true'
          userWorkloadTraces: 'true'
        # Optional: fan out to an external sink from every hub
        additionalRemoteWrites:
          - name: external-monitoring
            onSelfManagedHub: true
            url: https://external.example.com/api/v1/receive
            remoteTimeout: 30s
            caFile: /etc/prometheus/secrets/external-certs/ca.crt
            certFile: /etc/prometheus/secrets/external-certs/tls.crt
            keyFile: /etc/prometheus/secrets/external-certs/tls.key
            secretRef:
              name: external-certs
              namespace: some-ns
```

### Intermediate hub (managed by the global hub)

```yaml
# autoshift/values/clustersets/hub1.yaml
hubClusterSets:
  hub1:
    labels:
      self-managed: 'false'
      acm-observability: 'true'
      coo: 'true'
      global-observability: 'true'
    config:
      globalObservability:
        capabilities:
          userWorkloadTraces: 'false'
```

Workload clusters need no labels — the patched `PrometheusAgent` template and its secret arrive via MCOA replication from their intermediate hub. The minimal enablement for the whole rollup is two labels per hub (`global-observability` + the correct `self-managed` value).
