# Global Observability

Three-tier metrics rollup for a hub-of-hubs topology: **global hub → intermediate hubs → workload clusters**.

Each workload cluster dual-writes metrics: to its intermediate hub (the default MultiCluster Observability Add-on path) and directly to the global hub Observatorium over mTLS. The intermediate hub is where the template is patched; metrics do not hop through its Thanos.

This chart owns only the rollup. Base MultiCluster Observability (MCO), including the namespace, pull secret, object storage, the `MultiClusterObservability` custom resource, and MultiCluster Observability Add-on (MCOA) capabilities, is owned by `policy-acm-observability` in the `advanced-cluster-management` chart.

| Document | Use it for |
|----------|------------|
| [quickstart.md](quickstart.md) | Enable the rollup from a working AutoShift deployment |
| [architecture.md](architecture.md) | How the rollup works and why |
| This README | Labels, config keys, policies, and examples |

## How it fits together

| Tier | Labels | What this chart does |
|------|--------|----------------------|
| Global hub | `global-observability: 'true'`, `self-managed: 'true'` | Assembles the coalesced `global-observability-secrets` Secret (mTLS client cert, CA, Observatorium URL) |
| Intermediate hub | `global-observability: 'true'`, `self-managed: 'false'` | Stages that secret into the observability namespace and patches the MCOA `PrometheusAgent` templates with a global remote-write |
| Workload cluster | none | Receives the patched agent and secret through MCOA replication; dual-writes automatically |

`self-managed` is the placement discriminator. The global hub already receives metrics from its own managed clusters through native MCOA, so injecting a "write to global hub" remote-write there would be a self-loop. The prometheus PolicySet therefore excludes `self-managed: 'true'`.

## Policies

| Policy | Runs on | Mode | Description |
|--------|---------|------|-------------|
| `policy-global-observability-secrets` | Global hub | enforce | Builds `global-observability-secrets` in the policy namespace from the Observatorium Route and signer certificates |
| `policy-global-observability-prom-test` | Intermediate hubs | inform | Gate: MCOA must have created its `PrometheusAgent` templates before patching. A policy-created agent is ignored by MCOA replication |
| `policy-global-observability-prometheus` | Intermediate hubs | enforce | Copies the rollup secret (and any `additionalRemoteWrites` secrets) into the observability namespace, then patches the agent templates |

## Placement

Both PolicySets also require `acm-observability: 'true'`, so the policies land only where `policy-acm-observability` is placed. Without that predicate a dependency on it can sit Pending forever.

| PolicySet | Policies | Placement |
|-----------|----------|-----------|
| `policyset-global-observability-secrets` | `*-secrets` | `global-observability: 'true'` AND `acm-observability: 'true'` AND `self-managed: 'true'` |
| `policyset-global-observability-prometheus` | `*-prom-test`, `*-prometheus` | `global-observability: 'true'` AND `acm-observability: 'true'` AND `self-managed: 'false'` |

## Dependencies

| Policy | Depends on |
|--------|------------|
| `policy-global-observability-secrets` | `policy-acm-observability` |
| `policy-global-observability-prom-test` | `policy-acm-observability` |
| `policy-global-observability-prometheus` | `policy-acm-observability`, `policy-coo-operator-install`, `policy-global-observability-prom-test` |

## Labels

All labels are prefixed with `autoshift.io/`. Set them in values files only; never stamp them onto a `ManagedCluster` by hand.

| Label | Type | Default | Description |
|-------|------|---------|-------------|
| `global-observability` | string bool | `'false'` | Enable this chart on a hub |
| `self-managed` | string bool | (required) | `'true'` = global hub; `'false'` = intermediate hub. Drives placement |
| `acm-observability` | string bool | (required) | Required on every participating hub. Base MCO must be Compliant first |
| `coo` | string bool | (required) | Required on every participating hub. Cluster Observability Operator runs the agents |

All other behavior comes from rendered-config (`config:` in values), not labels.

## Configuration (`config.globalObservability.*`)

Read when the chart patches `PrometheusAgent` templates. Unset keys use the defaults in the table.

Base MCO settings (`capabilities`, Thanos storage, retention) are under `config.acm.observability` and are documented in the [advanced-cluster-management README](../advanced-cluster-management/README.md) and [quickstart.md](quickstart.md).

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `scrapeInterval` | string | `300s` | `PrometheusAgent` scrape interval |
| `logLevel` | string | `warn` | `PrometheusAgent` log level |
| `additionalRemoteWrites` | list | `[]` | Extra remote-write targets alongside the built-in global rollup |

### `additionalRemoteWrites[]`

Optional targets beyond the built-in rollup. Each entry:

| Key | Required | Default | Description |
|-----|----------|---------|-------------|
| `name` | yes | (required) | Remote-write entry name |
| `url` | yes | (required) | Remote-write endpoint URL |
| `caFile` / `certFile` / `keyFile` | yes | (required) | TLS paths inside the agent pod (`/etc/prometheus/secrets/<secret-name>/...`) |
| `remoteTimeout` | no | `30s` | Remote-write timeout |
| `onSelfManagedHub` | no | `false` | `false`: intermediate hubs only; `true`: every hub that carries this config |
| `secretRef.name` / `secretRef.namespace` | no | (optional) | Hub Secret replicated into the observability namespace and mounted into the agent |

Keep secret names short and alphanumeric-terminated. MCOA generates `secret-<name>` volume names truncated to 63 characters; a cut that lands on a non-alphanumeric character silently breaks the agent StatefulSet.

Removing an `additionalRemoteWrites` entry does take effect: the prometheus policy rebuilds the lists it owns and applies `mustonlyhave` so deletions are not left behind as stale remote-writes.

## Built-in rollup defaults

Hard-coded in the chart. Override only when the AutoShift release name (and therefore the policy namespace) is not `autoshift`.

| Value | Default | Description |
|-------|---------|-------------|
| Observability namespace | `open-cluster-management-observability` | Hub namespace where MCOA templates and staged secrets live |
| Agent templates patched | `mcoa-default-platform-metrics-collector-global`, `mcoa-default-user-workload-metrics-collector-global` | MCOA-owned `PrometheusAgent` names |
| Rollup remote-write name | `acm-global-observability` | Built-in entry injected on intermediate hubs |
| Rollup secret name | `global-observability-secrets` | Coalesced mTLS + URL secret |
| Rollup secret namespace | `policies-autoshift` | Where the secrets policy writes on the global hub |
| Rollup timeout | `30s` | Built-in remote-write timeout |
| TLS mount paths | `/etc/prometheus/secrets/global-observability-secrets/{ca.crt,tls.crt,tls.key}` | Paths the agent uses for the rollup |

> [!IMPORTANT]
> The secrets policy writes into `policy_namespace` (`policies-<release-name>`, default `policies-autoshift`), but the rollup reads from the hard-coded `secretNamespace` default. If the AutoShift Application is released under another name, override `spokeAgent.globalHubRollup.secretNamespace` to match or the secret copy fails silently.

## Prerequisites

On every participating hub:

- `acm-observability: 'true'` and `policy-acm-observability` Compliant (base MCO custom resource, capabilities, and storage)
- `coo: 'true'` so the Cluster Observability Operator runs the `PrometheusAgent`s
- Object storage for that hub's own MCO stack (`acm-observability-storage: 'noobaa'` or `'external-s3'`; see the quick start)

For `additionalRemoteWrites` with `secretRef`, the referenced Secret must exist on the hub in the given namespace before the prometheus policy runs.

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
      acm-observability-storage: 'noobaa'   # or external-s3; see quickstart.md
      coo: 'true'
      global-observability: 'true'
    config:
      acm:
        observability:
          capabilities:
            platformAnalytics: 'true'
            platformLogs: 'true'
            platformMetrics: 'true'
            userWorkloadLogs: 'true'
            userWorkloadMetrics: 'true'
            userWorkloadTraces: 'true'
      globalObservability:
        scrapeInterval: '300s'
        logLevel: 'warn'
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
      acm-observability-storage: 'noobaa'
      coo: 'true'
      global-observability: 'true'
    config:
      acm:
        observability:
          capabilities:
            userWorkloadTraces: 'false'   # example: leave traces off on this hub
      globalObservability:
        scrapeInterval: '300s'
        logLevel: 'warn'
```

Workload clusters need no labels. The patched `PrometheusAgent` template and its secret arrive through MCOA replication from their intermediate hub. The minimum enablement for the whole rollup is `global-observability`, the correct `self-managed` value, plus `acm-observability` and `coo` on each hub.

## Validate

```bash
cd tools && go test -tags integration ./internal/resolver/...
```
