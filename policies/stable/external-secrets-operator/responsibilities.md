# Responsibilities

Who does what in this chart, broken down three ways: per PolicySet (the deployment/placement
unit you see in the ACM console), per template file, and — inside each file — per Policy and
per ConfigurationPolicy. Use this when you need to answer "which policy owns that object?" or
"where does this behavior live?" without reading the templates.

Companion docs: [README.md](README.md) (operational reference),
[quickstart.md](quickstart.md) (getting started),
[mechanics.md](mechanics.md) (how the mechanisms work), [CONFIG-REFERENCE.md](CONFIG-REFERENCE.md)
(every knob), [troubleshooting.md](troubleshooting.md) (when it breaks).

## How to read this

- **PolicySet** — grouping + placement unit. All placement for this chart lives in
  `templates/policysets.yaml`; the policy files themselves carry no Placement/PlacementBinding.
- **Policy** — one per template file (`policy-eso-*`). The unit of compliance rollup and of the
  description annotation shown in the ACM console.
- **ConfigurationPolicy (CP)** — the worker unit inside a Policy. Most files pair an **enforce**
  CP (does the work, records per-item failures into a status ConfigMap instead of hard-failing
  the template) with an **inform gate** CP (reads that status ConfigMap and goes NonCompliant
  with the recorded failure details). The gate is where you look for *why* something didn't
  happen; the enforce CP is *what* happens.
- **Render gates** — hub-only files are wrapped in `{{ if .Values.hubClusterSets }}` and don't
  render at all in a spokes-only deployment. The five boot policies additionally honor the
  `hubBootstrap.diagnostics` toggles: `readinessOnly` (render only the gates, apply nothing) and
  `debugRender` (emit a preview ConfigMap of what the policy WOULD apply; wins over
  readinessOnly).

---

## PolicySets (`templates/policysets.yaml`)

This file owns ALL placement: six PolicySets, each with its own Placement + PlacementBinding.
Every Placement uses the same predicate (`autoshift.io/external-secrets-operator: 'true'`) plus
unreachable/unavailable tolerations; the groups differ only in clusterSet scope (hubs-only vs
hubs+managed) and member intent. The two hub-only groups are wrapped in
`{{ if .Values.hubClusterSets }}`.

| PolicySet | Scope | Members | Responsibility |
|---|---|---|---|
| `policyset-eso-install` | all placed clusters | policy-eso-install | Operator installation and the ExternalSecretsConfig CR that deploys the controller pods. |
| `policyset-eso-secret-stores` | all placed clusters | policy-eso-secret-stores, policy-eso-cert-auth-rbac | User-declared secret stores: the store objects, their auth-credential transport (spoke hop), and kubernetes-provider cert-auth RBAC. |
| `policyset-eso-secret-reader` | all placed clusters | policy-eso-secret-reader | Read-only consumption ServiceAccount + RBAC for ESO-provisioned Secrets. |
| `policyset-eso-boot-spoke` | all placed clusters | policy-eso-boot-readiness-spoke, policy-eso-boot-store | Spoke half of the hub bootstrap: per-mode readiness gate, then the hub-bootstrap ClusterSecretStore build. Hubs are members too — a hub gets a bootstrap store like any spoke. |
| `policyset-eso-boot-hub` | hubs only | policy-eso-boot-prereqs, policy-eso-boot-readiness-hub, policy-eso-boot-serving-ca, policy-eso-boot-clientca-self, policy-eso-boot-clientca-self-wire, policy-eso-boot-clientca-ext | Hub half of the hub bootstrap: hub-template RBAC prereqs, readiness gate, client-CA mint/wire (per trust mode), serving-CA discovery. |
| `policyset-eso-hub-secrets` | hubs only | policy-eso-hub-secrets | Hop 1 of the two-hop credential transport: materialize external-origin store credentials on the hub for every owned cluster. |

---

## Per-file breakdown

### `templates/policy-eso-install.yaml`

**Policy `policy-eso-install`** — set: `policyset-eso-install` (all clusters). Installs the
operator and stands up the operand.

| CP | Kind | Responsibility |
|---|---|---|
| `eso-operator-ns` | ConfigurationPolicy (enforce) | Create the operator namespace with the `openshift.io/cluster-monitoring` label. |
| `install-operator-eso` | OperatorPolicy (enforce) | Subscription/OperatorGroup/CSV lifecycle for the external-secrets-operator package (channel, source, install plan approval from values). |
| `eso-config` | ConfigurationPolicy (enforce) | Apply the `ExternalSecretsConfig` CR named `cluster` — this is what actually deploys the controller pods. Spec = chart default from `values.yaml` (`externalSecretsOperator.externalSecretsConfig`, verbatim) hub-merged under the per-cluster `config.eso.externalSecretsConfig` overlay from the rendered config. Precedence: cluster > clusterset > deployment defaults > chart values. Lists replace wholesale (an overriding `networkPolicies` list must restate the default :443 rule). This is also the passthrough for operand NetworkPolicies, trustedCABundle, and component env overrides. |

### `templates/policy-eso-secret-stores.yaml`

**Policy `policy-eso-secret-stores`** — set: `policyset-eso-secret-stores` (all clusters). The
main user-facing workhorse: everything declared under `config.eso.secretStores` lands here.

| CP | Responsibility |
|---|---|
| `eso-secret-stores` (enforce) | Per declared store: (1) emit the `SecretStore`/`ClusterSecretStore` object from the user spec, with namespace-inference defaults (kubernetes-provider caProvider namespace defaults to where the delivered CA lands); (2) when `caSource` is set, copy the named hub ConfigMap's CA into a delivered-CA ConfigMap on the cluster; (3) when `authSecretConfig` is set, emit the spoke-side hop-2 `ExternalSecret`s that pull the store's auth credentials from the hub-bootstrap store into the exact Secret refs named in the store spec (paths resolved via `internal.authRefPaths`); (4) bake an `autoshift.io/eso-prune` label capturing, at emission time, whether the object may be deleted once its store leaves the config (store-level `prune`, else `config.eso.pruneRemovedStores`, else chart default), and sweep removed stores accordingly. Per-store failures are recorded into the status ConfigMap, not template-fatal. |
| `eso-secret-stores-gate` (inform) | Read the status ConfigMap and surface per-store failures as NonCompliant detail. |

### `templates/policy-eso-cert-auth-rbac.yaml`

**Policy `policy-eso-cert-auth-rbac`** — set: `policyset-eso-secret-stores` (all clusters).
Companion to secret-stores for kubernetes-provider stores that authenticate with a client cert.

| CP | Responsibility |
|---|---|
| `eso-cert-auth-rbac` (enforce) | From each store's `certAuthRBAC` block, create the RBAC that grants the client-cert CN its Secret access, scoped to match the store: ClusterRole + ClusterRoleBinding for cluster-scoped grants, Role + RoleBinding for namespaced grants. Sweeps stale RBAC when stores or grants are removed. |
| `eso-cert-auth-rbac-gate` (inform) | Status-ConfigMap gate for the above. |

### `templates/policy-eso-secret-reader.yaml`

**Policy `policy-eso-secret-reader`** — set: `policyset-eso-secret-reader` (all clusters).

| CP | Responsibility |
|---|---|
| `eso-secret-reader` (enforce) | Create the read-only secret-reader ServiceAccount and RBAC that AutoShift components use to consume ESO-provisioned Secrets. No gate — nothing conditional to report. |

### `templates/policy-eso-boot-prereqs.yaml` *(renders only when `hubClusterSets` is set)*

**Policy `policy-eso-boot-prereqs`** — set: `policyset-eso-boot-hub` (hubs only).

| CP | Responsibility |
|---|---|
| `eso-boot-prereqs` (enforce) | Hub-side RBAC scaffold for AutoShift internals: grant the ACM hub-template ServiceAccount the reads the other boot policies' hub templates need, driven by the config `bootPrereqs.rbac` grant list. Must be compliant before the hub readiness gate passes. |

### `templates/policy-eso-boot-readiness-hub.yaml` *(hub-gated render)*

**Policy `policy-eso-boot-readiness-hub`** — set: `policyset-eso-boot-hub` (hubs only).
Readiness gate for the hub-side boot policies.

| CP | Responsibility |
|---|---|
| `eso-boot-readiness-hub-report` (enforce) | Evaluate the per-trust-mode preconditions on the hub — cert-manager present and healthy (checked via its policy compliance/placement, not a self-managed assumption), issuer readiness, serving-cert health, boot-prereqs done — and write the result to the readiness status ConfigMap. |
| `eso-boot-readiness-hub-gate` (inform) | Go NonCompliant with the recorded reasons until the hub is ready. |

### `templates/policy-eso-boot-readiness-spoke.yaml`

**Policy `policy-eso-boot-readiness-spoke`** — set: `policyset-eso-boot-spoke` (all clusters).

| CP | Responsibility |
|---|---|
| `eso-boot-readiness-spoke-report` (enforce) | Assert the per-mode PKI preconditions on the target cluster (client cert minted/present for this cluster, serving CA discoverable, mode-specific inputs) before the bootstrap store may be built; write results to the status ConfigMap. |
| `eso-boot-readiness-spoke-gate` (inform) | Surface unmet preconditions. |

### `templates/policy-eso-boot-serving-ca.yaml` *(hub-gated render; boot-body define `eso.bootBody.servingCa`)*

**Policy `policy-eso-boot-serving-ca`** — set: `policyset-eso-boot-hub` (hubs only).

| CP | Responsibility |
|---|---|
| `eso-boot-serving-ca` (enforce) | Discover the hub apiserver's serving CA at runtime — a custom named serving cert if the APIServer config declares one, else the operator-managed bundle — and stash it in the policy namespace, where boot-store (and per-store `caSource` delivery) pick it up for spokes. |

### `templates/policy-eso-boot-clientca-self.yaml` *(hub-gated render; boot-body define `eso.bootBody.clientcaSelf`)*

**Policy `policy-eso-boot-clientca-self`** — set: `policyset-eso-boot-hub` (hubs only). Active
in `selfSigned` mode; renders inert otherwise.

| CP | Responsibility |
|---|---|
| `eso-boot-clientca-self` (enforce) | Mint the self-signed bootstrap CA, then for every ManagedCluster carrying an `autoshift.io/owning-namespace` label (across ALL deployments, not just this one): mint a per-cluster client cert (CN = `<prefix>.<cluster>.<baseDomain>`, with the CN-truncation rule) into that cluster's owning namespace, and create the reader Role/RoleBinding there so the CN can read exactly its deployment's secrets. Tenancy is per-owning-namespace by design. Includes cleanup sweeps for departed clusters/certs. |
| `eso-boot-clientca-self-gate` (inform) | Status-ConfigMap gate. |

### `templates/policy-eso-boot-clientca-self-wire.yaml` *(hub-gated render; define `eso.bootBody.clientcaSelfWire`)*

**Policy `policy-eso-boot-clientca-self-wire`** — set: `policyset-eso-boot-hub` (hubs only).
Split from the mint policy so the atomic object-templates evaluation can't deadlock (the wire
step reads what the mint step creates).

| CP | Responsibility |
|---|---|
| `eso-boot-clientca-self-wire` (enforce) | Wire the minted bootstrap CA into `APIServer.spec.clientCA` (additive — merged into the existing bundle; triggers one kube-apiserver rollout on first set). |

### `templates/policy-eso-boot-clientca-ext.yaml` *(hub-gated render; define `eso.bootBody.clientcaExt`)*

**Policy `policy-eso-boot-clientca-ext`** — set: `policyset-eso-boot-hub` (hubs only). Active
in the external trust modes (`externalCA`, `externalCAReuseServingCert`); no minting happens.

| CP | Responsibility |
|---|---|
| `eso-boot-clientca-ext` (enforce) | Materialize the externally supplied CA bundle into the apiserver clientCA ConfigMap, and create per-owning-namespace reader RBAC for the spoke-derived cert CNs (the CN contract: spokes present certs the external CA issued; the hub must authorize those CNs without ever seeing the keys). |
| `eso-boot-clientca-ext-gate` (inform) | Status-ConfigMap gate. |

### `templates/policy-eso-boot-store.yaml` *(boot-body define `eso.bootBody.bootStore`)*

**Policy `policy-eso-boot-store`** — set: `policyset-eso-boot-spoke` (all clusters). The
payoff of the boot chain: after this, the cluster can pull hub secrets.

| CP | Responsibility |
|---|---|
| `eso-boot-store` (enforce) | Build the hub-bootstrap `ClusterSecretStore` on the cluster: copy this cluster's client-cert Secret and the hub serving CA from the owning deployment's policy namespace (via `copySecretData`/`fromConfigMap` — never a Secret lookup), and point a kubernetes-provider store at the hub apiserver over mTLS with `remoteNamespace` = the policy namespace. Store name comes from the runtime `storeName` override, else the chart `storePrefix`. |
| `eso-boot-store-gate` (inform) | Status-ConfigMap gate. |

### `templates/policy-eso-hub-secrets.yaml` *(hub-gated render)*

**Policy `policy-eso-hub-secrets`** — set: `policyset-eso-hub-secrets` (hubs only). Hop 1 of
the two-hop credential transport (hop 2 is in policy-eso-secret-stores on the spoke).

| CP | Responsibility |
|---|---|
| `eso-hub-secrets` (enforce) | Runtime-sweep the rendered config of every cluster this hub owns; for each store credential source with an `external` block, materialize it as an ExternalSecret in the owning namespace (pulling from the named external store on the hub) so the spoke's hop-2 ExternalSecret finds it at `hubSecretName`. For native sources (no `external`), verify the seed Secret exists — missing means *pending* (someone still has to seed it), not error. Runtime lookups keep this correct on self-managed and managed hubs alike. |
| `eso-hub-secrets-gate` (inform) | Status-ConfigMap gate (per-source pending/failed detail). |

### `templates/_eso-boot-status.tpl` *(helpers, renders nothing by itself)*

Shared Helm defines for the fail-to-status-ConfigMap pattern used by every enforce/gate pair:

- `eso.boot.statusReport` — emit the per-policy status ConfigMap block that the enforce CP
  writes its success/failure entries into.
- `eso.boot.statusReportPerStore` — the per-item (per-store / per-source) variant used where
  one policy tracks many independent units.

The boot policy *bodies* are each single-sourced in a per-file define (`eso.bootBody.*`,
listed above) so the live, readinessOnly, and debugRender render paths can't drift apart —
edit the define, not an inline copy.
