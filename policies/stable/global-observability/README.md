# Global Observability Policy

Deploys and configures ACM MultiCluster Observability (MCO) on a global hub cluster, enabling centralized metrics collection from regional hubs via Thanos and Prometheus remote-write.

## Policies

| Policy | Description |
|--------|-------------|
| `policy-global-observability-mch` | Enables the `multicluster-observability` component on the MultiClusterHub |
| `policy-global-observability-config` | Creates the MCO namespace, pull-secret, CA bundle secret, and Thanos object-storage secret |
| `policy-global-observability-instance` | Creates the `MultiClusterObservability` CR with retention, storage, and addon settings |
| `policy-global-observability-secrets` | Builds the `global-observability-secrets` Secret containing mTLS certs and the observatorium API URL |
| `policy-global-observability-prometheus` | Patches PrometheusAgent templates on hub clusters with the built-in global hub rollup and any additional remote-write targets from rendered-config |

## PolicySets and Placement

| PolicySet | Targets | Placement Criteria |
|-----------|---------|-------------------|
| `policyset-global-observability-secrets` | Global hub only | `global-observability: 'true'` AND `self-managed: 'true'` |
| `policyset-global-observability` | All hub clusters | `global-observability: 'true'` |
| `policyset-global-observability-prometheus` | All hub clusters | `global-observability: 'true'` |

## Labels

All labels are prefixed with `autoshift.io/`.

### Enable/Disable

| Label | Type | Default | Description |
|-------|------|---------|-------------|
| `global-observability` | bool | `'false'` | Enable MultiCluster Observability on hub clusters |

### Placement Labels (used by PolicySet selectors)

| Label | Type | Description |
|-------|------|-------------|
| `self-managed` | bool | Distinguishes the global hub (`'true'`) from regional hubs (`'false'`). Controls which PolicySets target which hubs. |

### Configuration Labels

| Label | Type | Default | Description |
|-------|------|---------|-------------|
| `global-observability-storage-class` | string | cluster default | Storage class for Thanos persistent volumes |
| `global-observability-retention-raw` | string | `'5d'` | Retention period for raw resolution metrics |
| `global-observability-retention-5m` | string | `'14d'` | Retention period for 5-minute resolution metrics |
| `global-observability-retention-1h` | string | `'30d'` | Retention period for 1-hour resolution metrics |
| `global-observability-alertmanager-storage-size` | string | `'10Gi'` | Alertmanager PVC size |
| `global-observability-compact-storage-size` | string | `'10Gi'` | Compactor PVC size |
| `global-observability-receive-storage-size` | string | `'10Gi'` | Receiver PVC size |
| `global-observability-rule-storage-size` | string | `'10Gi'` | Rule PVC size |
| `global-observability-store-storage-size` | string | `'10Gi'` | Store gateway PVC size |

## Chart Values (`globalObservability.*`)

### General

| Value | Type | Default | Description |
|-------|------|---------|-------------|
| `namespace` | string | `open-cluster-management-observability` | Namespace where MCO resources are created |

### CA Bundle

| Value | Type | Default | Description |
|-------|------|---------|-------------|
| `caBundle.sourceName` | string | `user-ca-bundle` | ConfigMap name in `openshift-config` containing the CA bundle |
| `caBundle.sourceKey` | string | `ca-bundle.crt` | Key within the ConfigMap holding the CA certificate |

### Thanos Object Storage

| Value | Type | Default | Description |
|-------|------|---------|-------------|
| `thanosStorage.bucket` | string | `acm-dr` | S3 bucket name for Thanos metrics storage |
| `thanosStorage.endpoint` | string | `""` | S3 endpoint hostname (e.g. `s3.example.com`) |
| `thanosStorage.insecure` | bool | `false` | Whether to skip TLS verification for the S3 endpoint |
| `thanosStorage.source.namespace` | string | `""` | Namespace of the source secret containing S3 credentials |
| `thanosStorage.source.secretName` | string | `""` | Name of the source secret containing S3 credentials |
| `thanosStorage.source.accessKeyField` | string | `AWS_ACCESS_KEY_ID` | Key in the source secret holding the access key |
| `thanosStorage.source.secretKeyField` | string | `AWS_SECRET_ACCESS_KEY` | Key in the source secret holding the secret key |

### Retention and Storage

| Value | Type | Default | Description |
|-------|------|---------|-------------|
| `retentionResolutionRaw` | string | `5d` | Retention for raw resolution metrics |
| `retentionResolution5m` | string | `14d` | Retention for 5-minute downsampled metrics |
| `retentionResolution1h` | string | `30d` | Retention for 1-hour downsampled metrics |
| `enableDownsampling` | bool | `true` | Enable Thanos downsampling of stored metrics |
| `scrapeInterval` | string | `300s` | Prometheus scrape interval |
| `scrapeSizeLimitBytes` | string | `1073741824` | Maximum scrape size in bytes (1 GiB) |
| `workers` | int | `1` | Number of MCO addon workers |
| `alertmanagerStorageSize` | string | `10Gi` | Alertmanager PVC size |
| `compactStorageSize` | string | `10Gi` | Compactor PVC size |
| `receiveStorageSize` | string | `10Gi` | Receiver PVC size |
| `ruleStorageSize` | string | `10Gi` | Rule PVC size |
| `storeStorageSize` | string | `10Gi` | Store gateway PVC size |

### Spoke Agent

Controls how hub clusters forward metrics via PrometheusAgent remote-write.

#### Global Hub Rollup

Built-in remote-write that forwards metrics from regional hubs to the self-managed hub's observatorium. Always deployed, automatically skipped on the self-managed hub (MCOA handles local writes).

| Value | Type | Default | Description |
|-------|------|---------|-------------|
| `spokeAgent.prometheusAgentNames` | list | `[mcoa-default-platform-metrics-collector-global, mcoa-default-user-workload-metrics-collector-global]` | PrometheusAgent resources to patch on hubs |
| `spokeAgent.globalHubRollup.name` | string | `acm-global-observability` | Name of the built-in remote-write entry |
| `spokeAgent.globalHubRollup.secretName` | string | `global-observability-secrets` | Coalesced secret created by `policy-global-observability-secrets` |
| `spokeAgent.globalHubRollup.secretNamespace` | string | `open-cluster-policies` | Namespace where the coalesced secret lives on the hub |
| `spokeAgent.globalHubRollup.remoteTimeout` | string | `30s` | Timeout for remote-write requests |
| `spokeAgent.globalHubRollup.caFile` | string | `/etc/prometheus/secrets/global-observability-secrets/ca.crt` | Path to the CA file inside the PrometheusAgent pod |
| `spokeAgent.globalHubRollup.certFile` | string | `/etc/prometheus/secrets/global-observability-secrets/tls.crt` | Path to the client cert file |
| `spokeAgent.globalHubRollup.keyFile` | string | `/etc/prometheus/secrets/global-observability-secrets/tls.key` | Path to the client key file |

#### Additional Remote-Writes (rendered-config)

Optional list of extra remote-write targets configured per-cluster/clusterset via the rendered-config ConfigMap under `globalObservability.additionalRemoteWrites`. These are added alongside the built-in rollup. Secrets are always replicated from the hub via `copySecretData`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `additionalRemoteWrites[].name` | string | — | Name of the remote-write entry |
| `additionalRemoteWrites[].url` | string | — | Remote-write endpoint URL |
| `additionalRemoteWrites[].remoteTimeout` | string | `30s` | Timeout for remote-write requests |
| `additionalRemoteWrites[].onSelfManagedHub` | bool | `false` | When true, emit on the self-managed hub too |
| `additionalRemoteWrites[].caFile` | string | — | CA file path in the PrometheusAgent pod |
| `additionalRemoteWrites[].certFile` | string | — | Client cert file path |
| `additionalRemoteWrites[].keyFile` | string | — | Client key file path |
| `additionalRemoteWrites[].secretRef.name` | string | — | Secret name to replicate into the observability namespace |
| `additionalRemoteWrites[].secretRef.namespace` | string | — | Source namespace of the secret on the hub |

## Dependencies

| Policy | Depends On |
|--------|-----------|
| `policy-global-observability-prometheus` | `policy-global-observability-instance`, `policy-coo-operator-install` |

## Prerequisites

- The MultiClusterHub must have `multicluster-observability` enabled (handled by the MCH policy)
- A source secret with S3 credentials must exist at the location specified by `thanosStorage.source.*`
- A CA bundle ConfigMap must exist in `openshift-config` (specified by `caBundle.*`)
- For spoke agent functionality: the Cluster Observability Operator must be installed on hub clusters
- For additional remote-writes: referenced secrets must exist on the hub in their specified namespace

## Examples

### Labels Only

```yaml
# In autoshift/values/clustersets/hub.yaml
hubClusterSets:
  global-hub:
    labels:
      global-observability: 'true'
      self-managed: 'true'
      global-observability-storage-class: gp3-csi
      global-observability-retention-raw: '7d'

  regional-hub:
    labels:
      global-observability: 'true'
      self-managed: 'false'
```

### Labels with Config

```yaml
# In autoshift/values/clustersets/hub.yaml or autoshift/values/clusters/<cluster>.yaml
hubClusterSets:
  global-hub:
    labels:
      global-observability: 'true'
      self-managed: 'true'
    config:
      globalObservability:
        useAlternateCA: false
        storageClass: 'gp3-csi'
        retentionResolutionRaw: '7d'
        retentionResolution5m: '14d'
        retentionResolution1h: '30d'
        enableDownSampling: true
        interval: 300
        scrapeSizeLimitBytes: 1073741824
        scrapeWorkers: 1
        scrapeInterval: '300s'
        logLevel: 'warn'
        alertmanagerStorageSize: '10Gi'
        compactStorageSize: '10Gi'
        receiveStorageSize: '10Gi'
        ruleStorageSize: '10Gi'
        storeStorageSize: '10Gi'
        capabilities:
          platformAnalytics: 'true'
          platformLogs: 'true'
          platformMetrics: 'true'
          userWorkloadLogs: 'true'
          userWorkloadMetrics: 'true'
          userWorkloadTraces: 'true'
        thanosStorage:
          bucket: 'acm-dr'
          endpoint: 's3.example.com'
          insecure: false
          caBundle:
            sourceName: 'user-ca-bundle'
            sourceKey: 'ca-bundle.crt'
          source:
            namespace: 'my-secrets-ns'
            secretName: 'my-s3-secret'
            accessKeyField: 'AWS_ACCESS_KEY_ID'
            secretKeyField: 'AWS_SECRET_ACCESS_KEY'
        additionalRemoteWrites:
          - name: external-monitoring
            remoteTimeout: 30s
            onSelfManagedHub: true
            url: https://external.example.com/api/v1/receive
            caFile: /etc/prometheus/secrets/external-certs/ca.crt
            certFile: /etc/prometheus/secrets/external-certs/tls.crt
            keyFile: /etc/prometheus/secrets/external-certs/tls.key
            secretRef:
              name: external-certs
              namespace: some-ns

  regional-hub:
    labels:
      global-observability: 'true'
      self-managed: 'false'
```
