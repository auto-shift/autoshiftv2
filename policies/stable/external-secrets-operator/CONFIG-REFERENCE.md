# Configuration reference

Every variable this chart reads, organized by YAML block. There are three configuration
surfaces:

1. **Chart values** (`values.yaml`) — deployment-wide defaults, set where the chart is
   installed (the ArgoCD Application / helm invocation). One set per deployment.
2. **Runtime cluster config** — the `config:` section of the clusterset/cluster values files
   (`hubClusterSets.<set>.config` / `managedClusterSets.<set>.config`, plus per-cluster
   overrides). AutoShift renders it into a `<cluster>.rendered-config` ConfigMap in the policy
   namespace; the policies read it **per cluster** at ACM propagation time via hub templates.
3. **AutoShift labels** — flat `labels:` entries on the clusterset/cluster that drive the
   operator install policy.

**Precedence** (for keys that exist on both surfaces): per-cluster rendered config →
chart value → hard-coded default. The chart's `externalSecretsOperator.hubBootstrap.*`
block deliberately mirrors the runtime `config.eso.hubBootstrap.*` structure 1:1 so every
value falls back path-for-path. Keys that exist on only one surface are marked
**chart-only** or **runtime-only** in their description.

Types: `bool` values in the runtime config are real YAML booleans (`true`/`false`), not
strings. Durations (`1h`, `720h`, `5m0s`) are Go/cert-manager duration strings.

---

## 1. Chart values (`values.yaml`)

### Top level

| Key | Type | Default | Description |
|---|---|---|---|
| `policy_namespace` | string | `open-cluster-policies` | ACM namespace all Policies/PolicySets/Placements render into. Also the namespace the hub-bootstrap flow mints client certs into and grants spokes read on. |

### `externalSecretsOperator`

| Key | Type | Default | Description |
|---|---|---|---|
| `name` | string | `openshift-external-secrets-operator` | Operator Subscription name. |
| `namespace` | string | `external-secrets-operator` | Namespace the operator (and the `secret-reader` ServiceAccount) is installed into. |
| `channel` | string | `stable-v1` | Subscription channel. |
| `source` | string | `redhat-operators` | Catalog source. |
| `sourceNamespace` | string | `openshift-marketplace` | Catalog source namespace. |
| `operatorGroupName` | string | `external-secrets-operator-operator-group` | OperatorGroup name. |
| `targetNamespaces` | list(string) | unset | Optional. When set, the OperatorGroup targets these namespaces (namespace-scoped install) instead of all namespaces. |
| `pruneRemovedStores` | bool | `true` | Deployment default for pruning: when a store entry is **removed** from `config.eso.secretStores`, should everything it created be deleted? Baked onto every emitted object as an `autoshift.io/eso-prune` label **at emission time** — flipping it after removal does nothing (relabel/delete manually). Runtime override: `config.eso.pruneRemovedStores`; per-store override: `secretStores[].…​.prune`. |
| `secretReaderName` | string | `secret-reader` | Name of the read-only ServiceAccount (in `namespace`) other AutoShift components use to consume ESO-provisioned Secrets. Granted read via `config.externalSecretsOperator.secretReaderNamespaces` + `config.defaultSecretsNamespace`. |
| `externalSecretsConfig` | map | `appConfig.logLevel: 1`, `appConfig.webhookConfig.certificateCheckInterval: 5m0s`, `controllerConfig.networkPolicies: [allow-https-egress — core controller :443 out]`, `plugins.bitwardenSecretManagerProvider.mode: Disabled` | The `ExternalSecretsConfig` CR **spec**, verbatim — any CRD field goes here. Runtime override: `config.eso.externalSecretsConfig`, same shape, deep-merged over this (override wins; **lists replace wholesale** — an overriding `networkPolicies` list drops the default :443 allow unless restated; zero values — `0`/`false`/`""` — cannot override a non-zero lower-level value). The operator deny-alls operand egress except :6443 + DNS, hence the default :443 allow; providers on other ports need their own entry. |
| `certManagerResourceNamespace` | string | `cert-manager` | cert-manager "cluster resource namespace" — where the self-signed bootstrap CA Secret lives. Chart-only. |
| `hubCASourceNamespace` | string | `openshift-config-managed` | Fallback hub serving-CA ConfigMap namespace (used when the hub apiserver serves no custom named cert). Chart-only. |
| `hubCASourceName` | string | `kube-apiserver-server-ca` | Fallback serving-CA ConfigMap name. Chart-only. |
| `hubCASourceKey` | string | `ca-bundle.crt` | Fallback serving-CA ConfigMap key. Chart-only. |

### `externalSecretsOperator.hubBootstrap`

Defaults for the cluster→cluster bootstrap. Every key here (except the three marked
chart-only) can be overridden per deployment/cluster under `config.eso.hubBootstrap`.

| Key | Type | Default | Description |
|---|---|---|---|
| `hubServer` | string | `''` | Hub apiserver URL (e.g. `https://api.hub.example.com:6443`). Required unless `deriveHubUrl` is true. Runtime override: `config.eso.hubBootstrap.hubServer`. |
| `deriveHubUrl` | bool | `false` | If true AND `hubServer` is empty, the copy policy looks the hub apiserver URL up itself (hub-template lookup of the `apiserverurl.openshift.io` ClusterClaim on the **immediate propagating hub**). Prefer this over a static `hubServer` in multi-hop (global-hub → spoke-hub → leaf) topologies — each cluster resolves the hub that minted its cert. Runtime override: `config.eso.hubBootstrap.deriveHubUrl`. |
| `storePrefix` | string | `hub-bootstrap` | Names everything the bootstrap mints/copies: store (default), `<prefix>-client` Secret, `<prefix>-hub-ca` ConfigMap, `<prefix>-reader` Role, `<prefix>-ca` / issuers, `<prefix>-client-ca` ConfigMap. Chart-only (the spoke store *name* alone can be overridden at runtime via `config.eso.hubBootstrap.storeName`). |
| `clientCAConfigMap` | string | `''` → `<storePrefix>-client-ca` | Name of the `openshift-config` ConfigMap `APIServer.spec.clientCA` points at. Chart-only. |
| `authSecretRefreshInterval` | duration | `1h` | Default `refreshInterval` for the store-auth ExternalSecrets `policy-eso-secret-stores` emits (credential pulls through the bootstrap store). Per-store override: `secretStores[].authSecretConfig.refreshInterval`. Chart-only. |
| `teardown` | bool | `false` | Explicit decommission flag: every boot policy switches from provisioning to removal (spoke store + client secret + serving-CA copy, hub mint estate, reader RBAC, clientCA ConfigMap, `APIServer.spec.clientCA` → `""` — a disruptive apiserver rollout). Removing the `hubBootstrap` block WITHOUT this flag is a deliberate no-op. Runtime override: `config.eso.hubBootstrap.teardown`. See README → Decommissioning. |
| `mode` | string | `selfSigned` | Trust mode: `selfSigned` \| `externalCA` \| `externalCAReuseServingCert`. Selects who mints the client cert and what the hub `clientCA` trusts. One mode per hub (`APIServer.spec.clientCA` is a single field). Runtime override: `config.eso.hubBootstrap.mode`. See README → Trust modes. |
| `certManagerOperatorNamespace` | string | `cert-manager-operator` | Namespace holding the cert-manager-operator CSV the readiness gates check (`status.phase == Succeeded`). Chart-only. |
| `certManagerPolicyName` | string | `policy-cert-manager` | AutoShift Policy the readiness gates require Compliant (per cluster) when `externalCertAuthority.autoshiftProvisioned` is true. Chart-only. |

### `externalSecretsOperator.hubBootstrap.clientIdentity`

Client-cert CN + lifetime (used in `selfSigned` and `externalCA` modes; ignored in
`externalCAReuseServingCert`). Runtime overrides under
`config.eso.hubBootstrap.clientIdentity.*`.

The cert-shape fields (`certDuration` … `privateKeySize`) are **mode-gated**: in
`selfSigned` mode, unset means the defaults shown below; in `externalCA` mode, unset means
the field is **omitted** from the Certificate entirely so the external issuer's own
defaults/policy apply (prevents fighting an external CA's reissue rules).

| Key | Type | Default | Description |
|---|---|---|---|
| `certCNPrefix` | string | `autoshift-eso-client` | Client cert CN prefix. Full CN = `<prefix>.<managedClusterName>.<baseDomain>`. |
| `baseDomain` | string | `''` | CN base domain (FQDN tail). `selfSigned`: defaults to `autoshift.io`. `externalCA`: **required** (must satisfy the customer PKI). CN capped at 63 chars — the `managedClusterName` segment is truncated to fit; the policy fails loudly if no budget remains or two truncated names collide. |
| `certDuration` | duration | `720h` (selfSigned) / omit (externalCA) | cert-manager Certificate `duration` for client certs. |
| `certRenewBefore` | duration | `480h` (selfSigned) / omit (externalCA) | cert-manager `renewBefore` window. |
| `certUsages` | list(string) | `[client auth, digital signature, key encipherment]` (selfSigned) / omit (externalCA) | Certificate `usages`. If you set it, keep `client auth` — hub mTLS requires it. Not listed in `values.yaml` but settable there and at runtime. |
| `privateKeyAlgorithm` | string | `RSA` (selfSigned) / omit (externalCA) | Certificate `privateKey.algorithm`. Not listed in `values.yaml` but settable. |
| `privateKeySize` | int | `2048` (selfSigned) / omit (externalCA) | Certificate `privateKey.size`. Only emitted when `privateKeyAlgorithm` is set. |

### `externalSecretsOperator.hubBootstrap.externalCertAuthority`

External-CA trust plumbing (`externalCA` / `externalCAReuseServingCert` modes only).
Runtime overrides under `config.eso.hubBootstrap.externalCertAuthority.*`.

| Key | Type | Default | Description |
|---|---|---|---|
| `caTrustBundle.namespace` | string | `openshift-config` | Namespace of the external CA bundle ConfigMap (read on the hub at runtime, materialized into the clientCA ConfigMap). |
| `caTrustBundle.name` | string | `''` | Name of the external CA bundle ConfigMap. **Required** in both external modes. |
| `caTrustBundle.key` | string | `ca-bundle.crt` | Key in that ConfigMap holding the PEM bundle. |
| `certIssuer.name` | string | `''` | Spoke ClusterIssuer/Issuer the spoke mints its own client cert from. **Required** in `externalCA` mode; user-provided (a precondition), chained to the external CA. |
| `certIssuer.kind` | string | `ClusterIssuer` | `ClusterIssuer` or `Issuer`. |
| `certIssuer.group` | string | `cert-manager.io` | Issuer API group. |
| `autoshiftProvisioned` | bool | `true` | `true`: AutoShift provisions cert-manager — the readiness gate requires the cert-manager policy Compliant for this cluster AND introspects the issuer/serving cert. `false`: cert-manager/PKI is out-of-band — the gate only verifies the operator is installed (CSV Succeeded). |

### `externalSecretsOperator.hubBootstrap.diagnostics`

Per-cluster run-mode defaults for the five active cert boot policies (clientca-self,
clientca-self-wire, clientca-ext, serving-ca, boot-store). Readiness gates and the non-cert
policies are never suppressed. Runtime overrides under
`config.eso.hubBootstrap.diagnostics.*`. When both are set, `debugRender` wins for output.

| Key | Type | Default | Description |
|---|---|---|---|
| `readinessOnly` | bool | `false` | The active cert boot policies apply nothing (empty object stream, Compliant) — only the readiness gates do real work. Use to validate preconditions before standing the bootstrap up. |
| `debugRender` | bool | `false` | Each active policy applies nothing live and instead writes a `<configpolicy>-debug-render` ConfigMap holding the full object stream it WOULD have applied (resolved on the target cluster; Secret data replaced by a source descriptor). Switching back off mustnothave-clears the preview ConfigMap. |

### `externalSecretsOperator.bootPrereqs`

Hub-side RBAC scaffold for AutoShift internals (`policy-eso-boot-prereqs`). The hub-template
ServiceAccount (`autoshift-policy-service-account`) needs these grants — e.g. the readiness
gates read per-cluster Policy status. Chart-only. Set `rbac: []` (or omit) to render nothing
and skip the policy.

`bootPrereqs.rbac` is a list of grants; each grant:

| Key | Type | Default | Description |
|---|---|---|---|
| `name` | string | — (required) | Base name for the generated Role/RoleBinding (or ClusterRole/ClusterRoleBinding). |
| `serviceAccount.name` | string | — (required) | Subject ServiceAccount bound to the role. |
| `serviceAccount.namespace` | string | `policy_namespace` | Subject namespace. |
| `scope` | string | `namespaced` | `namespaced` → Role + RoleBinding per namespace; `cluster` → ClusterRole + ClusterRoleBinding. |
| `namespaces` | list(string) | `[policy_namespace]` | (namespaced scope only) namespaces to create the Role/RoleBinding in. |
| `rules` | list(map) | — (required) | Standard RBAC policyRules (`apiGroups`/`resources`/`verbs`/…) rendered verbatim. |

Shipped default: one grant (`eso-boot-policy-reader`) giving the hub-template SA
`get/list/watch` on `policies.policy.open-cluster-management.io` in the policy namespace.

### `internal.authRefPaths`

**Not per-cluster config.** Lookup table teaching `policy-eso-secret-stores` where each auth
method keeps its Secret refs in `spec`. Edit only to add a new auth method.

| Key | Type | Default | Description |
|---|---|---|---|
| `<token>.provider` | string | — | The `spec.provider.<provider>` key (`vault`, `kubernetes`, …). |
| `<token>.base` | string | — | Dotted path from `spec.provider.<provider>` to the block the components are relative to (e.g. `auth.cert`; `""` = refs sit directly on the provider block). |
| `<token>.components` | map | — | component-name → dotted path from `base` to that SecretKeySelector. Component names are what `authSecretConfig.sources` keys on. |

Shipped tokens (valid `authSecretConfig.fromRef` values): `vaultToken`, `vaultAppRole`,
`vaultLdap`, `vaultUserPass`, `vaultJwt`, `vaultKubernetes`, `vaultCert`, `vaultIam`,
`vaultGcp`, `kubernetesToken`, `kubernetesCert`.

---

## 2. Runtime cluster config (`config:` → rendered-config)

Set under `hubClusterSets.<set>.config` / `managedClusterSets.<set>.config` in the AutoShift
values files (per-cluster files may override). Everything here is evaluated **per cluster**.

### `config` (top-level keys this chart reads)

| Key | Type | Default | Description |
|---|---|---|---|
| `defaultSecretsNamespace` | string | unset | Default namespace ESO writes provisioned Secrets into. Appended automatically to `secretReaderNamespaces` for the secret-reader RBAC. Shared top-level key (not ESO-specific). |

### `config.externalSecretsOperator`

| Key | Type | Default | Description |
|---|---|---|---|
| `secretReaderNamespaces` | list(string) | `[]` | Namespaces the `secret-reader` ServiceAccount is granted Secret read on (`policy-eso-secret-reader`). `defaultSecretsNamespace` is appended automatically. |

### `config.eso`

| Key | Type | Default | Description |
|---|---|---|---|
| `pruneRemovedStores` | bool | chart `pruneRemovedStores` (`true`) | Deployment-wide prune default for removed store entries. Baked as the `autoshift.io/eso-prune` label at emission time. Per-store override below. |
| `secretStores` | list(map) | `[]` | The store list — see next section. |
| `externalSecretsConfig` | map | unset | Per-cluster `ExternalSecretsConfig` CR **spec** overlay — highest-precedence layer, deep-merged over the chart's `externalSecretsConfig` overlay and the `config*` defaults (this overlay wins; lists replaced wholesale). Zero values (`0`/`false`/`""`) here cannot override a non-zero chart default — set those at chart level. See README *ExternalSecretsConfig passthrough*. |
| `hubBootstrap` | map | unset | Cluster→cluster bootstrap config — see below. Present ⇒ the boot policies provision the store; **absent ⇒ no-op** (removal never tears anything down; use `teardown: true`). |

### `config.eso.secretStores[]` — list item wrapper

Each list item is a single-key map naming the kind:

| Key | Type | Default | Description |
|---|---|---|---|
| `clusterSecretStore` | map | — | Cluster-scoped store (exactly one of the two keys per item). |
| `secretStore` | map | — | Namespaced store. |

### Store object (under `clusterSecretStore:` / `secretStore:`)

| Key | Type | Default | Description |
|---|---|---|---|
| `name` | string | — (required) | Store name. |
| `namespace` | string | — (required for `secretStore`) | Namespace of a namespaced SecretStore (and where its auth Secrets resolve). ClusterSecretStores take no namespace — their auth refs carry `.namespace` instead. |
| `spec` | map | — (required) | The ESO `SecretStoreSpec`, **verbatim and authoritative** — written into the store object as-is. |
| `prune` | bool | `config.eso.pruneRemovedStores` | Per-store prune override: should this store's objects be deleted if the entry is later removed? Recorded on every emitted object as `autoshift.io/eso-prune` at emission time. |
| `authSecretConfig` | map | unset | Provision the Secret(s) the store's auth refs point at, via the two-hop hub transport — see below. Omit for ref-less auth (e.g. Vault `kubernetes` SA auth). |
| `caSource` | map | unset | (kubernetes provider) Hub ConfigMap holding the REMOTE apiserver's serving CA; delivered into this store's own `caProvider` ConfigMap on the spoke. Omit to supply the CA out of band. |
| `certAuthRBAC` | map | unset | (kubernetes provider, cert auth) Generate RBAC on the remote cluster granting the client-cert CN Secret access — see below. |

### `caSource`

| Key | Type | Default | Description |
|---|---|---|---|
| `namespace` | string | — (required) | Hub namespace holding the remote serving-CA bundle ConfigMap. Read on the **immediate propagating hub** — in multi-hop topologies a leaf's `caSource` CM must exist on its spoke-hub. |
| `name` | string | — (required) | ConfigMap name. |
| `key` | string | — (required) | Key holding the PEM bundle. Delivered to the ConfigMap the store's `spec…caProvider` names; `caProvider.namespace` unset defaults to the `<ManagedClusterName>` namespace where the CA is delivered. |

### `certAuthRBAC`

Scope follows the store: `SecretStore` → Role + RoleBinding in its namespace;
`ClusterSecretStore` without `spec.conditions` → ClusterRole + ClusterRoleBinding;
`ClusterSecretStore` with `spec.conditions[].namespaces` → ClusterRole + one RoleBinding per
listed namespace.

| Key | Type | Default | Description |
|---|---|---|---|
| `username` | string | — (required) | Client-cert CN = the RBAC subject (`User`). |
| `verbs` | list(string) | `[create, update, list, delete]` | Verbs granted on `secrets`. |

### `authSecretConfig`

Provisions each auth-ref target Secret through the two-hop transport: an ExternalSecret on
the hub optionally materializes the credential from an external store
(`policy-eso-hub-secrets`), then an ExternalSecret on the spoke pulls it through the
hub-bootstrap store (`policy-eso-secret-stores`). Target Secret name/key/namespace are read
from the ref **in `spec`** — never repeated here.

| Key | Type | Default | Description |
|---|---|---|---|
| `fromRef` | string | — (required) | Which auth method's refs in `spec` to mirror — one of the `internal.authRefPaths` tokens (see §1). |
| `refreshInterval` | duration | chart `hubBootstrap.authSecretRefreshInterval` (`1h`) | Spoke ExternalSecret re-pull cadence. |
| `sources` | map | — (required) | component-name → source entry (below). Component names come from the chosen `fromRef` token. Optional components may be left unsourced. |

### `authSecretConfig.sources.<component>`

| Key | Type | Default | Description |
|---|---|---|---|
| `hubSecretName` | string | — (required) | Secret in the hub policy namespace the spoke pulls (the bootstrap store's `remoteRef.key`). Components may share one hub Secret (entries merge) or use different ones. |
| `key` | string | unset | Property within the hub Secret → the ref's key. **Omit for a whole-Secret pull** (`dataFrom.extract`) — then all refs the `fromRef` covers must point at the same target Secret. |
| `external` | map | unset | Have `policy-eso-hub-secrets` (hubs only) materialize the hub Secret from an external store first. Omit when the Secret is already in the hub policy namespace (native/seeded) — natively-declared seeds are **verified** (existence + declared `key`; names only, never values) and a missing one is reported as **`pending`** (not an error) in `eso-hub-secrets-status`: chained stores whose seed is produced by another store's flow converge over subsequent evaluations, blocking nothing meanwhile. |

### `sources.<component>.external`

| Key | Type | Default | Description |
|---|---|---|---|
| `storeRef.name` | string | — (required) | Hub-side store holding the credential (e.g. a root store fed by a manually-seeded Secret). |
| `storeRef.kind` | string | — (required) | `ClusterSecretStore` or `SecretStore`. |
| `remoteRef.key` | string | — (required) | Key in that external store. |
| `remoteRef.property` | string | unset | Property within the remote value. |
| `remoteRef.version` | string | unset | Remote value version. |

### `config.eso.hubBootstrap`

Per-deployment/cluster overrides of the chart `hubBootstrap` block (§1) — same structure,
key for key. Only the keys below differ from or add to the chart surface:

| Key | Type | Default | Description |
|---|---|---|---|
| `hubServer` | string | chart `hubServer` | Hub apiserver URL. |
| `deriveHubUrl` | bool | chart `deriveHubUrl` (`false`) | Look the hub URL up via ClusterClaim when `hubServer` is empty. |
| `storeName` | string | chart `storePrefix` (`hub-bootstrap`) | **Runtime-only.** Name of the ClusterSecretStore created on the spoke — the name consumers put in `secretStoreRef`. All *other* minted-object names still derive from the chart `storePrefix`. |
| `mode` | string | chart `mode` (`selfSigned`) | Trust mode: `selfSigned` \| `externalCA` \| `externalCAReuseServingCert`. |
| `teardown` | bool | chart `teardown` (`false`) | Explicit decommission flag — see §1 and README → Decommissioning. |
| `clientIdentity.*` | — | chart values | Same keys as §1 `clientIdentity` (`certCNPrefix`, `baseDomain`, `certDuration`, `certRenewBefore`, `certUsages`, `privateKeyAlgorithm`, `privateKeySize`), same mode-gated defaults. |
| `externalCertAuthority.*` | — | chart values | Same keys as §1 `externalCertAuthority` (`caTrustBundle.{namespace,name,key}`, `certIssuer.{name,kind,group}`, `autoshiftProvisioned`). |
| `diagnostics.readinessOnly` | bool | chart value (`false`) | Per-cluster: active cert boot policies apply nothing. |
| `diagnostics.debugRender` | bool | chart value (`false`) | Per-cluster: active cert boot policies emit their debug-preview ConfigMap instead of applying. |

Not overridable at runtime (chart-only): `storePrefix`, `clientCAConfigMap`,
`authSecretRefreshInterval`, `certManagerOperatorNamespace`, `certManagerPolicyName`, and
the `certManagerResourceNamespace` / `hubCASource*` keys on the parent block.

---

## 3. AutoShift labels

Flat labels on the clusterset/cluster (`labels:` section) driving the operator install
policy. All values are strings.

| Label | Type | Default | Description |
|---|---|---|---|
| `external-secrets-operator` | 'true'/'false' | unset (not managed) | Placement label — `'true'` places the policies on the cluster; `'false'`/unset leaves the operator unmanaged. |
| `external-secrets-operator-subscription-name` | string | `openshift-external-secrets-operator` | Operator Subscription name. |
| `external-secrets-operator-channel` | string | `stable-v1` | Subscription channel. |
| `external-secrets-operator-version` | string | unset | Pin to a specific CSV; setting it switches the Subscription to manual approval. |
| `external-secrets-operator-source` | string | `redhat-operators` | Catalog source. |
| `external-secrets-operator-source-namespace` | string | `openshift-marketplace` | Catalog source namespace. |
