# Global Observability — Architecture Reference

## MCOA PrometheusAgent Replication Flow

MCOA (MultiCluster Observability Addon) manages the lifecycle of PrometheusAgent resources:

1. **Hub-side templates**: MCOA creates PrometheusAgent resources in `open-cluster-management-observability` on each hub. These serve as templates defining the scrape and remote-write configuration for that hub's managed clusters.
2. **Replication to managed clusters**: MCOA copies the PrometheusAgent from the hub's `open-cluster-management-observability` namespace to each managed cluster's `open-cluster-management-addon` namespace.
3. **Secret replication**: Any secret listed in the PrometheusAgent's `spec.secrets` is automatically copied by MCOA from `open-cluster-management-observability` (on the hub) to `open-cluster-management-addon` (on the managed cluster). Secrets only need to exist in the hub's observability namespace — MCOA handles the last mile.

## Hub Topology

```
Global Hub (self-managed=true)
├── Runs MCO + Thanos + Observatorium (the global observability stack)
├── MCOA creates PrometheusAgent templates for its managed clusters
├── Managed clusters already write to this hub's observatorium via MCOA defaults
│
├── Regional Hub A (self-managed=false, managed by global hub)
│   ├── Also runs MCO + MCOA for its own managed clusters
│   ├── MCOA creates PrometheusAgent templates for its managed clusters
│   └── Needs additional remote-write to forward metrics UP to the global hub
│
└── Regional Hub B (self-managed=false, managed by global hub)
    └── Same as Regional Hub A
```

## Policy Chain

Policies execute in dependency order. Each depends on the previous completing successfully.

### Phase 1: Enable MCO on Hub (`policy-global-observability-mch`)

- **Placement**: All hubs with `global-observability: 'true'` (via `policyset-global-observability`)
- **Depends on**: `policy-acm-mch-install`
- **Action**: Patches the MultiClusterHub CR to enable the `multicluster-observability` component
- **Effect**: MCO operator starts, creates the observability namespace and its controllers

### Phase 2: Prerequisite Resources (`policy-global-observability-config`)

- **Placement**: All hubs with `global-observability: 'true'` (via `policyset-global-observability`)
- **Depends on**: `policy-global-observability-mch`
- **Action**: Creates resources in `open-cluster-management-observability`:
  - Namespace (with cluster-monitoring label)
  - Pull secret (copied from `openshift-config/pull-secret`)
  - CA bundle secret (conditionally, from `openshift-config/<configurable>` when `useAlternateCA` is set in rendered-config)
  - `thanos-object-storage` secret (S3 credentials copied from a source secret, bucket/endpoint from rendered-config)
- **Template resolution**: Uses mixed hub templates (for rendered-config lookups) and regular templates (for on-cluster secret reads). The hub resolves config values, the managed cluster resolves the actual secret data.

### Phase 3: MCO Instance (`policy-global-observability-instance`)

- **Placement**: All hubs with `global-observability: 'true'` (via `policyset-global-observability`)
- **Depends on**: `policy-global-observability-config`
- **Action**: Creates the `MultiClusterObservability` CR with:
  - Retention settings (raw, 5m, 1h)
  - Storage sizes (alertmanager, compact, receive, rule, store)
  - Storage class (optional)
  - Addon scrape settings (interval, size limit, workers)
  - MCOA capabilities toggles (platform analytics/logs/metrics, user workload logs/metrics/traces)
- **Config source**: All values from rendered-config `globalObservability` key, falling back to Helm values defaults
- **Effect**: MCO creates the Thanos stack, observatorium, and MCOA starts creating PrometheusAgent templates

### Phase 4: Global Hub Secrets Assembly (`policy-global-observability-secrets`)

- **Placement**: Self-managed hub only (`global-observability: 'true'` AND `self-managed: 'true'`, via `policyset-global-observability-secrets`)
- **Depends on**: None (no explicit dependency, but MCO must be running for source secrets to exist)
- **Action**: Assembles the coalesced `global-observability-secrets` secret in the **policy namespace** (`open-cluster-policies`) by reading from the self-managed hub's observability namespace:
  - `tls.crt` / `tls.key` from the observability signer client cert
  - `ca.crt` from `observability-managed-cluster-certs`
  - `observatorium.url` discovered from the `observatorium-api` Route
- **Why in the policy namespace?**: This secret is the source of truth that hub templates in the spoke policy will read via `copySecretData`. Placing it in the policy namespace makes it accessible to hub template resolution for all target clusters.
- **Template type**: Regular templates (`{{ }}`) — these resolve on the self-managed hub itself (the managed cluster in ACM terms), reading secrets and routes that exist locally.

### Phase 5: Spoke Agent — Secret Staging + PrometheusAgent Patching (`policy-global-observability-prometheus`)

- **Placement**: All hubs with `global-observability: 'true'` (via `policyset-global-observability-prometheus`)
- **Depends on**: `policy-global-observability-instance`, `policy-coo-operator-install`
- **Two separate concerns:**

#### 5a. Built-in global hub rollup

Hardcoded in the template using `globalHubRollup` values. Not part of the generic remote-write list.

**Secret replication** (`global-observability-rollup-secret` ConfigurationPolicy):
- Copies the coalesced secret from its source namespace to `open-cluster-management-observability`
- On self-managed hub: regular template `copySecretData` (local copy — secret exists on this cluster)
- On regional hubs: hub template `copySecretData` (reads from the global hub's policy namespace)

**PrometheusAgent patching**:
- Adds the rollup secret to `spec.secrets` and the rollup remote-write entry to `spec.remoteWrite`
- Skipped on self-managed hub (`if ne $isSelfManaged "true"`) — MCOA already handles local writes
- URL resolved at runtime via `fromSecret` reading the `observatorium.url` key from the replicated secret

#### 5b. Additional remote-writes (`additionalRemoteWrites`)

Driven by the `additionalRemoteWrites` list in values. Each entry is independent.

**Secret replication** (one `global-observability-secret-<name>` ConfigurationPolicy per secretRef):
- `fromHub: true` — secret lives on the global hub. Self-managed hub copies locally (regular template), regional hubs copy from hub (hub template).
- `fromHub: false` — secret exists locally on each hub. Always uses regular template `copySecretData`.

**PrometheusAgent patching**:
- Secrets and remote-write entries appended to the PrometheusAgent alongside the built-in rollup
- Gated by `onSelfManagedHub`: when false (default), skipped on self-managed hub; when true, emitted everywhere

## Secret Lifecycle

```
Self-Managed Hub                           Regional Hub
────────────────                           ────────────
open-cluster-management-observability:
  ├─ observability-*-signer-client-cert
  ├─ observability-managed-cluster-certs
  └─ observatorium-api Route
        │
        ▼ (Phase 4: field-by-field
           composition via regular
           templates)
open-cluster-policies:
  └─ global-observability-secrets
        │                                        │
        ▼ (Phase 5a: regular template            ▼ (Phase 5a: hub template
           copySecretData — local copy)             copySecretData — from hub)
        │                                        │
open-cluster-management-observability:   open-cluster-management-observability:
  └─ global-observability-secrets          └─ global-observability-secrets
        │                                        │
        ▼ (MCOA replication)                     ▼ (MCOA replication)
Managed Cluster:                         Managed Cluster:
  open-cluster-management-addon:           open-cluster-management-addon:
    └─ global-observability-secrets          └─ global-observability-secrets
```

## PolicySet Summary

| PolicySet | Placement | Policies |
|---|---|---|
| `policyset-global-observability-secrets` | `global-observability: 'true'` + `self-managed: 'true'` | `policy-global-observability-secrets` |
| `policyset-global-observability` | `global-observability: 'true'` (all hubs) | `policy-global-observability-mch`, `policy-global-observability-config`, `policy-global-observability-instance` |
| `policyset-global-observability-prometheus` | `global-observability: 'true'` (all hubs) | `policy-global-observability-prometheus` |

## `onSelfManagedHub` Logic (additionalRemoteWrites only)

Each `additionalRemoteWrites` entry has an `onSelfManagedHub` flag:

| `onSelfManagedHub` | Self-Managed Hub | Regional Hubs | Use Case |
|---|---|---|---|
| `false` (default) | Skipped | Emitted | Targets that only regional hubs should write to |
| `true` | Emitted | Emitted | External targets that all hubs should write to |

The built-in global hub rollup is always skipped on self-managed hub — this is not configurable, since MCOA handles local writes natively.

The condition in the template: `if not (and (eq $isSelfManaged "true") (not <onSelfManagedHub>))` — emit unless we're on the self-managed hub AND the entry is NOT flagged for self-managed.

## `fromHub` Logic (additionalRemoteWrites secretRefs)

Each `secretRef` has a `fromHub` flag controlling where the secret is sourced:

| `fromHub` | Self-Managed Hub | Regional Hubs | Use Case |
|---|---|---|---|
| `true` | Regular template (local copy) | Hub template (from global hub) | Secret assembled/stored on the global hub |
| `false` (default) | Regular template (local copy) | Regular template (local copy) | Secret exists independently on each hub |
