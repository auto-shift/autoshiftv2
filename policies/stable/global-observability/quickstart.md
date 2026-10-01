# Global Observability — Quickstart

Gets the three-tier metrics rollup (global hub → intermediate hubs → workload clusters) running from a working AutoShift deployment. See [architecture.md](architecture.md) for how it works and [README.md](README.md) for the full label/values reference.

## What you'll end up with

- Full MCO stack (Thanos + Observatorium + Grafana) on the global hub **and** on each intermediate hub
- MCOA capabilities (platform/user-workload metrics, logs, traces) enabled everywhere
- Every workload cluster dual-writing metrics: to its own intermediate hub, and directly to the global hub over mTLS

## Prerequisites

- AutoShift deployed on the global hub, with each intermediate hub imported as a managed cluster in its own hub clusterset (e.g. `hubofhubs` self-managed, `hub1`/`hub2` managed)
- Object storage on each hub. `acm-observability-storage: 'noobaa'` (the default) provisions a NooBaa bucket and requires OpenShift Data Foundation. `external-s3` uses a bucket you already have and does not require OpenShift Data Foundation
- Working pull-secret in `openshift-config` on each hub (copied automatically)

Three policy layers are involved, all enabled by labels:

| Layer | Chart | Enable label | What it does |
|-------|-------|--------------|--------------|
| Base MCO | `advanced-cluster-management` (`policy-acm-observability`) | `acm-observability: 'true'` | Namespace, pull secret, and the `MultiClusterObservability` object. `noobaa` or `external-s3` writes `thanos-object-storage` |
| COO | `cluster-observability` (`policy-coo-operator-install`) | `coo: 'true'` | Cluster Observability Operator — runs the `PrometheusAgent`s |
| Rollup | `global-observability` (this chart) | `global-observability: 'true'` | MCOA capabilities patch, rollup secret, `PrometheusAgent` remote-write patching |

## Step 1 — Create the S3 credentials secret on each hub

Skip this step when `acm-observability-storage` is `noobaa` or unset. NooBaa creates the bucket.

For `external-s3`, set `acm-observability-s3-namespace` to the namespace where Secret `thanos-s3-bucket` lives on that hub. `policy-acm-observability-external-s3` builds `thanos-object-storage` with `fromSecret` against that Secret. There is no values entry for the bucket. The inform policy stays NonCompliant until every key below can be read. Create the Secret on the global hub **and** on every intermediate hub, in the namespace you chose:

```bash
oc create namespace acm-thanos --dry-run=client -o yaml | oc apply -f -   # if it doesn't exist on that hub
oc create secret generic thanos-s3-bucket \
  -n acm-thanos \
  --from-literal=BUCKET_NAME=<bucket> \
  --from-literal=BUCKET_HOST=<s3-hostname> \
  --from-literal=BUCKET_PORT=443 \
  --from-literal=AWS_ACCESS_KEY_ID=<S3_ACCESS_KEY> \
  --from-literal=AWS_SECRET_ACCESS_KEY=<S3_SECRET_KEY>
```

Each hub runs its own MCO stack, so each needs its own bucket + credentials.

## Step 2 — Label and configure the global hub

In the global hub's clusterset values file:

```yaml
# autoshift/values/clustersets/hubofhubs.yaml  (your self-managed hub clusterset)
selfManagedHubSet: hubofhubs
hubClusterSets:
  hubofhubs:
    labels:
      self-managed: 'true'            # ← global hub discriminator
      acm-observability: 'true'       # base MCO stack
      # noobaa (default) needs OpenShift Data Foundation. external-s3 does not.
      acm-observability-storage: 'external-s3'
      acm-observability-s3-namespace: 'acm-thanos'   # namespace of Secret thanos-s3-bucket on this hub
      coo: 'true'                     # Cluster Observability Operator
      global-observability: 'true'    # this chart
    config:
      globalObservability:
        # All capabilities default to "true" — override only to disable
        # capabilities:
        #   userWorkloadTraces: 'false'
        # scrapeInterval: '300s'
        # logLevel: 'warn'
```

> **Important:** leave `acm.observability.enableMCOA` unset. This chart's `policy-global-observability-mcoa` owns the `capabilities` block; setting `enableMCOA: true` makes both policies patch it and disabling toggles stops working.

## Step 3 — Label and configure each intermediate hub

The same labels, with `self-managed: 'false'`. Each intermediate hub still needs its own `thanos-s3-bucket` Secret from step 1 when it uses `external-s3`:

```yaml
# autoshift/values/clustersets/hub1.yaml
hubClusterSets:
  hub1:
    labels:
      self-managed: 'false'           # ← managed by the global hub
      acm-observability: 'true'
      acm-observability-storage: 'external-s3'   # or 'noobaa' if this hub runs OpenShift Data Foundation
      acm-observability-s3-namespace: 'acm-thanos'   # namespace of Secret thanos-s3-bucket on this hub
      coo: 'true'
      global-observability: 'true'
    config:
      globalObservability: {}
```

Workload clusters need **no labels or config** — the patched `PrometheusAgent` and its mTLS secret arrive automatically via MCOA replication.

## Step 4 — Deploy and wait for convergence

Commit and push; ArgoCD syncs the policies. Expected convergence order (on the global hub):

```bash
oc get policies -n policies-autoshift | grep -E 'acm-observability|coo|global-observability'
```

1. `policy-acm-observability` → Compliant (MCO CR created, Thanos pods running)
2. `policy-coo-operator-install` → Compliant on every hub
3. `policy-global-observability-mcoa` → Compliant (capabilities patched; MCOA starts creating `PrometheusAgent` templates)
4. `policy-global-observability-secrets` → Compliant (global hub only — rollup secret assembled)
5. `policy-global-observability-prom-test` → Compliant per intermediate hub once MCOA has created its templates (this can lag a few minutes — it's the gate)
6. `policy-global-observability-prometheus` → Compliant (templates patched)

## Step 5 — Verify the rollup

On an **intermediate hub** — template patched and secret staged:

```bash
oc -n open-cluster-management-observability get prometheusagents
oc -n open-cluster-management-observability get secret global-observability-secrets
oc -n open-cluster-management-observability get prometheusagent \
  mcoa-default-platform-metrics-collector-global \
  -o jsonpath='{.spec.remoteWrite[*].name}'
# expect: ...acm-global-observability
```

On a **workload cluster** — MCOA delivered both:

```bash
oc -n open-cluster-management-addon get prometheusagent
oc -n open-cluster-management-addon get secret global-observability-secrets
```

On the **global hub** — metrics from workload clusters of intermediate hubs arriving: open Grafana (`Route` in `open-cluster-management-observability`) and query any metric filtered by a workload cluster's `cluster` label, e.g. `up{cluster="<workload-cluster-name>"}`.

## Common issues

| Symptom | Cause / fix |
|---------|-------------|
| `policy-acm-observability-external-s3-test` stays NonCompliant | Step 1 Secret is missing or misnamed **on that hub**, or it lacks `BUCKET_NAME`, `BUCKET_HOST`, `BUCKET_PORT`, `AWS_ACCESS_KEY_ID`, or `AWS_SECRET_ACCESS_KEY`. It is read locally on each hub, not from the global hub. OpenShift Data Foundation is not involved |
| `policy-acm-observability-noobaa` stays Pending | That hub is in noobaa mode and OpenShift Data Foundation is not Compliant yet. Switch the hub to `external-s3` if it should not wait on Data Foundation |
| `*-prom-test` stays NonCompliant | MCOA hasn't created its `PrometheusAgent` templates yet — verify MCOA is running and capabilities were patched (`oc get mco observability -o yaml`). This is the gate working as intended. |
| `*-prometheus` NonCompliant: "rollup secret … not found" | The chart's `spokeAgent.globalHubRollup.secretNamespace` (default `policies-autoshift`) doesn't match your policy namespace — override it if your AutoShift release is not named `autoshift` |
| Agent pod fails to roll out silently after adding `additionalRemoteWrites` | Secret-name truncation: MCOA volume names are `secret-<name>` cut at 63 chars; keep secret names short and alphanumeric-terminated |
| Disabling a capability toggle has no effect | `acm.observability.enableMCOA` is set — unset it so this chart is the sole capabilities manager |
