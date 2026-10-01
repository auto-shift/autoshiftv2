# advanced-cluster-management (policy)

This policy installs and configures Red Hat Advanced Cluster Management. Like every other policy it is
rendered by **PolicyGenerator** (`kustomization.yaml` → `policy-generator-config.yaml`), generating the
policies for operator install, MultiClusterHub, observability, search-storage, addon-tuning, and
provisioning.

The operator install uses the **shared `components/operator-install` chart** (like quay/odf/etc.) —
`manifests/operator-install/kustomization.yaml`. Two things about the values it passes are worth noting:

## Own-namespace (SingleNamespace) install mode

ACM installs scoped to its own namespace, so the caller sets `targetNamespaces`:

```yaml
namespace: open-cluster-management
targetNamespaces:
  - open-cluster-management        # OperatorGroup scoped to its own namespace
```

The shared component emits `targetNamespaces` only when a caller supplies it; the default (omitted) is
an AllNamespaces / cluster-scoped OperatorGroup. ACM is one of the few operators that needs the scoped
form. (ACM was hand-written as a raw `OperatorPolicy` until the component gained `targetNamespaces`
support; it now uses the common chart.)

## Creates the policy namespace too

ACM's install also creates the AutoShift policy namespace as an extra namespace, via the component's
`namespaces` list:

```yaml
namespaces:
  - ${POLICY_NAMESPACE}
```

## Bootstrap note

ACM is a **bootstrap operator**: the root-level `advanced-cluster-management/` Helm chart is
`helm install`-ed during bootstrap phase 1 to stand up ACM before ArgoCD/PolicyGenerator exist. This
`policies/` version then manages ACM day-2 through PolicyGenerator once the CMP is available.

## Object storage

`autoshift.io/acm-observability` enables the stack. `autoshift.io/acm-observability-storage` chooses where Thanos stores metrics. The two modes are separate policies, so a hub on external S3 is not held for Red Hat OpenShift Data Foundation.

| Value | What runs |
|---|---|
| `noobaa` (default, including when the label is unset) | `policy-acm-observability-noobaa` creates an `ObjectBucketClaim` and builds `thanos-object-storage` from the bucket the claim produces. It depends on `policy-storage-cluster-test`, so OpenShift Data Foundation has to be enabled. |
| `external-s3` | Set `acm-observability-s3-namespace` to the namespace of Secret `thanos-s3-bucket`. `policy-acm-observability-external-s3` writes one Secret, `thanos-object-storage`, from that lookup. |

```yaml
labels:
  acm-observability: 'true'
  acm-observability-storage: 'external-s3'
  acm-observability-s3-namespace: 'secrets-namespace'
```

```bash
oc create secret generic thanos-s3-bucket \
  -n secrets-namespace \
  --from-literal=BUCKET_NAME=<bucket> \
  --from-literal=BUCKET_HOST=<s3-hostname> \
  --from-literal=BUCKET_PORT=443 \
  --from-literal=AWS_ACCESS_KEY_ID=<access> \
  --from-literal=AWS_SECRET_ACCESS_KEY=<secret>
```

Create that Secret on every hub that runs observability, including each intermediate hub. One Secret holds the bucket name, the endpoint, and the credentials. NooBaa splits those across a ConfigMap and a Secret because that is what its claim controller writes. External S3 does not.

## Version pinning

Standard: `config.acm.versions` (and optional `config.acm.startingCSV`) pins the permitted CSV(s), else
the `autoshift.io/acm-version` label is used — the dual-mode block lives in the operator-install caller.

## Custom metrics

Full guidance, including cardinality cost and storage sizing, is in
[docs/observability-metrics.md](../../../docs/observability-metrics.md).

MultiCluster Observability collects a fixed default set of metrics. To collect more, enable
`autoshift.io/acm-observability-custom-metrics` and set `config.acm.observability.customMetrics`:

```yaml
config:
  acm:
    observability:
      customMetrics:
        platform:
          names:
            - node_vmstat_pgfault
        userWorkload:
          names:
            - my_app_requests_total
```

There are **two** delivery mechanisms and which one is live depends on the collector:

| Collector | Mechanism | Policy |
|---|---|---|
| Classic MultiCluster Observability | `observability-metrics-custom-allowlist` ConfigMap | `policy-acm-custom-metrics` |
| Multicluster observability add-on (MCOA) | `ScrapeConfig` federated by the `PrometheusAgent`, referenced from the `ClusterManagementAddOn` | `policy-acm-custom-metrics` and `policy-acm-custom-metrics-addon` |

Both are emitted from the same config, so the same values work either way and the inactive one is
inert. MCOA replaces the legacy collector, so on an MCOA hub the ConfigMap is applied and read by
nothing: if a metric does not arrive, check which collector the hub runs before debugging the
config.

`names` become `{__name__="<metric>"}` federation selectors; `matches` pass through verbatim so a
label selector can be written directly.

Two limits worth knowing:

- `recording_rules` work on both paths: the config map on the classic path, a `PrometheusRule` on
  MCOA. Each `record` name is added to the `ScrapeConfig` selectors automatically. Note the API
  groups differ and a wrong one is ignored silently: `ScrapeConfig` is `monitoring.rhobs`,
  `PrometheusRule` is `monitoring.coreos.com`.
- `policy-acm-custom-metrics-addon` reads the live `ClusterManagementAddOn`, appends its references
  and writes the object back whole. It preserves anything not named `autoshift-custom-*`, so MCOA's
  own configs survive. Removing a metric tier from config removes its `ScrapeConfig` contents, but
  the dangling reference in the add-on has to be cleared by hand.

In a multitiered rollup a metric is only forwarded if it is allowed on the hub whose collectors
gather it, so set this on every participating hub cluster set, not only the top one.
