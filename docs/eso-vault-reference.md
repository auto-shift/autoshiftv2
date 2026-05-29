# External Secrets Operator (ESO) — Vault & Cross-Cluster Reference

> Self-contained build reference for automating ESO. Field names, types, defaults, and behaviors are taken directly from the ESO source (`github.com/external-secrets/external-secrets`, `main` @ 2026-05-28, release line **v2.5.0**). API group: **`external-secrets.io/v1`** (the storage/served version; `v1beta1` and `v1alpha1` are deprecated aliases that still convert to `v1`). Generators live under **`generators.external-secrets.io/v1alpha1`**. `PushSecret` is currently **`external-secrets.io/v1alpha1`**.

---

## 0. Mental model

ESO is a controller that reconciles four kinds of object into native Kubernetes `Secret`s (or other manifests):

- **`SecretStore` / `ClusterSecretStore`** — *where* and *how to authenticate* to a backend (Vault, another cluster, a cloud SM). Namespaced vs cluster-scoped.
- **`ExternalSecret` / `ClusterExternalSecret`** — *what* to fetch and *how to shape* the resulting Secret. `ClusterExternalSecret` is a template that fans out `ExternalSecret`s into selected namespaces.
- **`PushSecret`** — reverse direction: take a local Secret (or generator output) and write it *into* the backend.
- **Generators** (`generators.external-secrets.io`) — produce ephemeral/dynamic values (e.g. `VaultDynamicSecret`, `Password`, `ECRAuthorizationToken`).

Reconcile loop per `ExternalSecret`: resolve `secretStoreRef` → instantiate provider client → authenticate → fetch (`data`/`dataFrom`) → apply `rewrite`/conversion/decoding → render `target.template` → create/patch the target Secret per `creationPolicy`. Re-runs every `refreshInterval`.

**Controller selection.** Every store has `spec.controller` (string). An ESO controller instance is started with a `--controller-class` name and only reconciles stores whose `controller` matches (empty matches the default instance). This is the `ingressClassName` analogue — use it to run multiple ESO deployments side-by-side.

---

## 1. SecretStore vs ClusterSecretStore

Both share the identical `spec` (`SecretStoreSpec`). The only differences are scope and namespace resolution of secret references.

| Aspect | `SecretStore` (`ss`) | `ClusterSecretStore` (`css`) |
|---|---|---|
| Scope | Namespaced | Cluster |
| Referenced by | `ExternalSecret` in the same namespace | any `ExternalSecret` in the cluster, via `secretStoreRef.kind: ClusterSecretStore` |
| `SecretKeySelector`/`ServiceAccountSelector` `.namespace` | ignored (always the ES's namespace) | **honored** — you can/should set `.namespace` on auth secret refs; otherwise ESO uses the *referent* namespace (the ES's namespace) |
| `caProvider.namespace` | must be empty (rejected) | allowed |
| Namespace restriction | n/a | `spec.conditions[]` (see below) |

### 1.1 `SecretStoreSpec` fields

```yaml
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore           # or SecretStore
metadata:
  name: vault-backend
spec:
  controller: ""                   # optional; selects which ESO instance owns this store
  refreshInterval: 0               # int SECONDS. How often the store's auth/validation is refreshed.
                                   #   0/empty => controller default. (Distinct from ExternalSecret.refreshInterval.)
  retrySettings:                   # HTTP retry on backend failures
    maxRetries: 5                  # *int32
    retryInterval: "10s"           # *string, Go duration
  conditions:                      # ClusterSecretStore ONLY — restrict which namespaces may use this store
    - namespaceSelector:           # metav1.LabelSelector
        matchLabels:
          environment: prod
      namespaces:                  # explicit names (ORed with the selector across the whole condition list)
        - team-a
        - team-b
      namespaceRegexes:            # regex match on namespace name
        - "^app-.*$"
  provider:                        # exactly ONE provider key (MinProperties=1, MaxProperties=1)
    vault: { ... }
```

**`conditions` semantics:** each condition may set any of `namespaceSelector`, `namespaces`, `namespaceRegexes`. A namespace is allowed if it matches **any** condition. Within a condition the three matchers are ORed. If `conditions` is empty, **all** namespaces may use the ClusterSecretStore. Use this to stop arbitrary teams from reading a privileged store.

**Status.** ESO writes `status.conditions[]` (type `Ready`, reasons `Valid`, `InvalidProviderConfig`, `ValidationFailed`, `InvalidStoreConfiguration`, …) and `status.capabilities` (`ReadOnly`/`WriteOnly`/`ReadWrite`). `kubectl get css` shows `Status`, `Capabilities`, `Ready`.

### 1.2 Referent authentication (the cross-namespace / multi-tenant pattern)

When a `ClusterSecretStore` references a Secret/ServiceAccount **without** specifying `.namespace`, ESO resolves it relative to the namespace of the *ExternalSecret that triggered the reconcile* (the "referent"). This lets a single ClusterSecretStore work for many namespaces, each supplying its own credentials/ServiceAccount under a shared name. Set an explicit `.namespace` to instead pin to one central credential.

---

## 2. The Vault provider (`spec.provider.vault`)

### 2.1 Top-level connection fields (`VaultProvider`)

| Field | Type | Default | Notes |
|---|---|---|---|
| `server` | string (**required**) | — | e.g. `https://vault.example.com:8200` |
| `path` | *string | — | KV mount path, e.g. `secret`. For KV **v2**, the `/data` infix is appended automatically if absent. |
| `version` | enum `v1`\|`v2` | **`v2`** | KV engine version. |
| `namespace` | *string | — | Vault **Enterprise** namespace for *data* requests (e.g. `ns1`). |
| `caBundle` | []byte (base64 in YAML) | — | PEM CA to validate the Vault server cert (HTTPS only). Falls back to system roots if unset. |
| `caProvider` | *CAProvider | — | Pull the CA from a `Secret`/`ConfigMap` instead of inlining (see §2.6). |
| `tls` | VaultClientTLS | — | **mTLS to the Vault server** (client cert/key). Distinct from `auth.cert`. |
| `tls.certSecretRef` | *SecretKeySelector | key `tls.crt` | Client cert for transport mTLS. |
| `tls.keySecretRef` | *SecretKeySelector | key `tls.key` | Client private key for transport mTLS. |
| `readYourWrites` | bool | false | Enterprise read-after-write consistency (sends replication state). |
| `forwardInconsistent` | bool | false | Forward read-after-write to the leader instead of retry-looping. |
| `headers` | map[string]string | — | Extra HTTP headers on every Vault request. |
| `checkAndSet` | *VaultCheckAndSet | — | KV v2 PushSecret CAS: `{required: true}` forces check-and-set on writes. |
| `auth` | *VaultAuth | — | Authentication block (see §2.2). Exactly one method. |

> **mTLS vs cert auth — don't confuse them.** `spec.provider.vault.tls` is *transport-layer* client certs (the Vault listener requires mutual TLS). `spec.provider.vault.auth.cert` is the *TLS certificate auth method* (`vault auth enable cert`) used to obtain a token. You can use either, both, or neither.

### 2.2 Auth methods (`VaultAuth`) — exactly one of these

`auth.namespace` (*string) sets the Vault Enterprise namespace **to authenticate against**, which may differ from the data `namespace`; defaults to `provider.vault.namespace` if set.

#### a) `tokenSecretRef` — static token
```yaml
auth:
  tokenSecretRef:
    name: vault-token
    key: token            # required
    namespace: secrets-ns # css only
```

#### b) `appRole`
```yaml
auth:
  appRole:
    path: approle         # default "approle" (mount path)
    roleId: "<role-id>"   # inline RoleID...
    roleRef:              # ...OR pull RoleID from a Secret (key required)
      name: approle-creds
      key: role-id
    secretRef:            # SecretID (required)
      name: approle-creds
      key: secret-id
```
Provide `roleId` **or** `roleRef`, plus `secretRef`.

#### c) `kubernetes` — Vault's Kubernetes auth method (most common in-cluster)
```yaml
auth:
  kubernetes:
    mountPath: kubernetes   # JSON key is "mountPath"; default "kubernetes"
    role: my-vault-role     # required — Vault role bound to SA + policies
    # ONE token source:
    serviceAccountRef:      # request/use this SA's token (preferred)
      name: my-app-sa
      audiences: ["vault"]  # optional extra audiences
    # secretRef:            # OR a Secret holding a SA JWT (key defaults to "token")
    #   name: sa-token
    #   key: token
    # (if neither set, the controller's own SA token is used)
```
Token source precedence: `serviceAccountRef` → `secretRef` → controller SA. `serviceAccountRef` uses the TokenRequest API to mint a short-lived token bound to `audiences`.

#### d) `ldap`
```yaml
auth:
  ldap:
    path: ldap            # default "ldap"
    username: svc-user    # required
    secretRef: { name: ldap-creds, key: password }
```

#### e) `userPass`
```yaml
auth:
  userPass:
    path: userpass        # default "userpass"
    username: svc-user    # required
    secretRef: { name: userpass-creds, key: password }
```

#### f) `jwt` / OIDC
```yaml
auth:
  jwt:
    path: jwt             # default "jwt"
    role: my-jwt-role
    # ONE of:
    secretRef: { name: jwt-token, key: jwt }      # static JWT in a Secret
    kubernetesServiceAccountToken:                # OR mint a SA token via TokenRequest
      serviceAccountRef: { name: my-app-sa, audiences: ["vault"] }
      # audiences/expirationSeconds on this struct are DEPRECATED — prefer serviceAccountRef.audiences
```

#### g) `cert` — TLS certificate auth method
```yaml
auth:
  cert:
    path: cert            # default "cert"
    vaultRole: my-cert-role          # optional Vault role
    clientCert: { name: cert-auth, key: tls.crt }   # client cert
    secretRef:  { name: cert-auth, key: tls.key }   # client private key
```

#### h) `iam` — AWS IAM auth method (Vault verifies a signed STS request)
```yaml
auth:
  iam:
    path: aws                       # Vault mount of the AWS auth method
    vaultRole: my-vault-aws-role    # required (JSON key "vaultRole")
    region: us-east-1
    role: arn:aws:iam::111122223333:role/assume-first   # AWS role to assume first (optional)
    externalID: "..."               # optional STS ExternalId
    vaultAwsIamServerID: vault.example.com  # X-Vault-AWS-IAM-Server-ID anti-replay header
    # credential source — pick one, or omit for controller pod identity (IRSA / EKS Pod Identity):
    secretRef:
      accessKeyIDSecretRef:     { name: aws-creds, key: access-key-id }
      secretAccessKeySecretRef: { name: aws-creds, key: secret-access-key }
      sessionTokenSecretRef:    { name: aws-creds, key: session-token }   # required if creds are temporary
    # jwt:
    #   serviceAccountRef: { name: irsa-sa }   # IRSA-enabled SA
```
If neither `secretRef` nor `jwt` is set, the controller pod's own AWS identity (IRSA or EKS Pod Identity) is used.

#### i) `gcp` — GCP IAM auth method
```yaml
auth:
  gcp:
    path: gcp               # default "gcp"
    role: my-vault-gcp-role # required
    projectID: my-project
    location: global
    # credential source — pick one, or omit for pod identity (GKE Workload Identity / SA key):
    secretRef: { secretAccessKeySecretRef: { name: gcp-sa-key, key: key.json } }
    workloadIdentity:
      serviceAccountRef: { name: ksa }
      clusterLocation: us-central1
      clusterName: my-gke
      clusterProjectID: my-project
    serviceAccountRef: { name: impersonate-this-gsa }   # SA impersonation
```

> **Auth method summary (all supported by ESO):** `tokenSecretRef`, `appRole`, `kubernetes`, `ldap`, `userPass`, `jwt` (incl. OIDC + TokenRequest), `cert`, `iam` (AWS), `gcp`. Azure-specific Vault auth is not a distinct ESO Vault auth block — use `jwt`/`kubernetes` or workload identity upstream.

### 2.3 Minimal working Vault ClusterSecretStore (KV v2, Kubernetes auth)
```yaml
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata:
  name: vault-kv
spec:
  provider:
    vault:
      server: https://vault.example.com:8200
      path: secret            # /data is auto-appended for v2
      version: v2
      namespace: team-a       # Enterprise only; drop for OSS Vault
      caProvider:
        type: ConfigMap
        name: vault-ca
        key: ca.crt
        namespace: external-secrets
      auth:
        kubernetes:
          mountPath: kubernetes
          role: eso-reader
          serviceAccountRef:
            name: external-secrets
            namespace: external-secrets
```

### 2.4 KV v1 vs v2 — path behavior
- **v2** (default): physical API path is `<mount>/data/<secret>` for reads, `<mount>/metadata/<secret>` for metadata/list. ESO inserts `/data` automatically; in `ExternalSecret.data[].remoteRef.key` you write the *logical* path (`myapp/config`), not `secret/data/myapp/config`.
- **v1**: path is `<mount>/<secret>` directly; no versioning, no `version` selection on reads.
- `remoteRef.version` selects a KV **v2** secret version (e.g. `"3"`); ignored on v1.
- `remoteRef.property` selects a single JSON field inside the secret value. Supports dotted/gjson paths for nested JSON.

### 2.5 Vault dynamic secrets (generator) — `VaultDynamicSecret`
For leased/dynamic backends (database creds, PKI, transit, `auth/token/create`, etc.). It is a **generator**, referenced from `ExternalSecret.dataFrom[].sourceRef.generatorRef`.

```yaml
apiVersion: generators.external-secrets.io/v1alpha1
kind: VaultDynamicSecret
metadata:
  name: db-creds
spec:
  path: database/creds/my-role     # Vault path to read/write
  method: GET                      # GET (default-ish), POST, etc.
  parameters: {}                   # JSON body for write/POST methods
  getParameters:                   # query-string params for GET (key -> []values)
    list: ["true"]
  resultType: Data                 # Data (default) | Auth | Raw
                                   #   Data = response.data; Auth = response.auth (e.g. token create); Raw = whole response
  allowEmptyResponse: false        # don't error when no data returned
  retrySettings: { maxRetries: 3, retryInterval: "5s" }
  provider:                        # SAME VaultProvider block as a SecretStore (server/auth/path/...)
    server: https://vault.example.com:8200
    auth:
      kubernetes: { mountPath: kubernetes, role: eso-dyn, serviceAccountRef: { name: external-secrets } }
```
Consume it:
```yaml
spec:
  dataFrom:
    - sourceRef:
        generatorRef:
          apiVersion: generators.external-secrets.io/v1alpha1
          kind: VaultDynamicSecret
          name: db-creds
```
`resultType: Auth` is the idiom for `auth/token/create` (the token lands in `response.auth`, not `response.data`).

### 2.6 `CAProvider` (shared by Vault + Kubernetes providers)
```yaml
caProvider:
  type: Secret            # "Secret" | "ConfigMap"
  name: vault-ca
  key: ca.crt             # key within the Secret/ConfigMap
  namespace: external-secrets   # ClusterSecretStore only
```

---

## 3. Cross-cluster: pulling secrets from other clusters

Two supported patterns.

### 3.1 Pattern A — Centralized Vault as the hub (recommended for many clusters)
Every cluster runs ESO; all of them point a `ClusterSecretStore` at one shared Vault. "Cross-cluster" reduces to "many clusters authenticating to one Vault." Mechanics:

- Each cluster gets its own Vault **Kubernetes auth backend mount** (e.g. `auth/kubernetes-clusterA`) configured with that cluster's API server CA + JWT reviewer, OR all clusters use a shared **JWT/OIDC** mount keyed on the cluster's OIDC issuer.
- Use distinct Vault **roles**/**policies** per cluster/namespace for blast-radius isolation. With `kubernetes` auth, bind the role to specific `bound_service_account_names` / `bound_service_account_namespaces`.
- Vault **namespaces** (Enterprise) cleanly partition tenants: set `provider.vault.namespace` per store.
- No ESO-to-foreign-apiserver connectivity needed — only egress to Vault. This is the most scalable model.

### 3.2 Pattern B — Kubernetes provider (read Secrets from a remote cluster's API server)
ESO connects directly to another cluster's kube-apiserver and reads native `Secret`s there. Use when the "source of truth" is Secrets in a hub cluster rather than Vault.

```yaml
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata:
  name: remote-cluster
spec:
  provider:
    kubernetes:
      server:
        url: https://remote-apiserver.example.com:6443   # default kubernetes.default (in-cluster)
        caBundle: <base64 PEM>                            # or:
        caProvider: { type: ConfigMap, name: remote-ca, key: ca.crt, namespace: external-secrets }
      remoteNamespace: shared-secrets   # namespace in the REMOTE cluster to read from (default "default")
      auth:                             # exactly ONE of cert/token/serviceAccount (MinProperties=1,MaxProperties=1)
        serviceAccount:                 # use a (local) SA token honored by the remote cluster
          name: eso-remote-reader
          namespace: external-secrets
          audiences: ["https://remote-apiserver.example.com"]
        # cert:                         # OR client-cert auth against remote apiserver
        #   clientCert: { name: remote-creds, key: tls.crt }
        #   clientKey:  { name: remote-creds, key: tls.key }
        # token:                        # OR a static bearer token
        #   bearerToken: { name: remote-creds, key: token }
      # authRef:                        # OR a single Secret containing full auth info
      #   name: kubeconfig-secret
```

`remoteRef.key` = the **remote Secret name**; `remoteRef.property` = a key within that Secret's `data`. Use `find`/`dataFrom` to pull whole remote Secrets. RBAC on the remote cluster must grant the identity `get`/`list` on Secrets in `remoteNamespace`.

**Auth block rules:** `KubernetesAuth` enforces exactly one of `cert`, `token`, `serviceAccount`. `authRef` is an alternative top-level path (a Secret bundling kubeconfig-style auth).

---

## 4. ExternalSecret — the per-secret knobs

### 4.1 Full annotated spec
```yaml
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: app-secrets
spec:
  refreshInterval: "1h0m0s"        # *metav1.Duration. Default "1h0m0s". "0s" => fetch once, never refresh.
  refreshPolicy: Periodic          # Periodic (default behavior) | CreatedOnce | OnChange
                                   #   CreatedOnce: create if absent, never update
                                   #   OnChange:    re-sync only when ES metadata/spec changes
                                   #   Periodic:    re-sync every refreshInterval (0 disables periodic)
  secretStoreRef:
    name: vault-kv
    kind: ClusterSecretStore       # SecretStore (default) | ClusterSecretStore

  target:
    name: app-secrets              # resulting Secret name; defaults to ES .metadata.name
    creationPolicy: Owner          # Owner (default) | Orphan | Merge | None
    deletionPolicy: Retain         # Retain (default) | Delete | Merge
    immutable: false               # set the created Secret immutable: true
    template:                      # blueprint for the resulting Secret (see §4.4)
      type: Opaque
      engineVersion: v2            # only v2 is valid now
      mergePolicy: Replace         # Replace (default) | Merge
      metadata:
        labels:   { app: web }
        annotations: { team: payments }
        finalizers: []
      data:
        application.yaml: |
          db: "{{ .db_password }}"
      templateFrom:
        - configMap:
            name: tmpl
            items: [{ key: config.tmpl, templateAs: Values }]   # Values | KeysAndValues
          target: Data             # Data (default) | Annotations | Labels | nested path for manifest targets
        - literal: "{{ .token }}"
    # manifest:                    # ADVANCED: create a non-Secret resource instead of a Secret
    #   apiVersion: v1
    #   kind: ConfigMap

  data:                            # explicit per-key mappings
    - secretKey: db_password       # key in the resulting Secret
      remoteRef:
        key: myapp/config          # Vault logical path (no /data for v2)
        property: db_password      # select a field within the secret JSON
        version: "3"               # KV v2 version (optional)
        metadataPolicy: None       # None (default) | Fetch  (fetch provider tags/labels)
        conversionStrategy: Default# Default | Unicode
        decodingStrategy: None     # None (default) | Auto | Base64 | Base64URL
        nullBytePolicy: Ignore     # Ignore (default) | Fail
      sourceRef:                   # OPTIONAL per-key store override
        storeRef: { name: other-store, kind: ClusterSecretStore }

  dataFrom:                        # bulk fetch; entries merged in order (later overrides earlier)
    - extract:                     # pull ALL key/values from one secret
        key: myapp/config
        conversionStrategy: Default
        decodingStrategy: None
        metadataPolicy: None
    - find:                        # discover many secrets
        path: myapp/               # optional path prefix to scope the search
        name: { regexp: "^db_.*" } # match by name
        tags: { env: prod }        # match by tags/labels
        conversionStrategy: Default
        decodingStrategy: None
      rewrite:                     # transform discovered keys (layered, first->last)
        - regexp: { source: "^db_(.*)$", target: "$1" }
        - transform: { template: "{{ .value | upper }}" }
        - merge:
            into: combined
            strategy: Extract      # Extract (default) | JSON
            priority: [a, b]
            priorityPolicy: Strict # Strict (default) | IgnoreNotFound
            conflictPolicy: Error  # Error (default) | Ignore
      sourceRef:
        storeRef: { name: vault-kv, kind: ClusterSecretStore }
        # generatorRef: { ... }    # use a generator instead of a store (Extract/Find not allowed with generators)
```

### 4.2 `data` vs `dataFrom`
- **`data[]`** — one explicit mapping per Secret key. Use `property` to pluck a single field; `version` for KV v2 history.
- **`dataFrom[].extract`** — splat every field of one provider secret into the Secret. Keys = the provider secret's field names.
- **`dataFrom[].find`** — discover multiple provider secrets by `path` prefix, `name.regexp`, and/or `tags`, then merge. **Find/Extract cannot be combined with a generator `sourceRef`.**
- Merge order: `dataFrom` entries apply in list order; later entries overwrite earlier keys. `data[]` mappings are applied on top.

### 4.3 Policies (precise semantics from source)

**`target.creationPolicy`:**
- `Owner` (default) — ESO creates the Secret and sets an `ownerReference` to the ExternalSecret (Secret deleted when ES is deleted).
- `Orphan` — creates the Secret with **no** ownerReference (survives ES deletion).
- `Merge` — does **not** create the Secret; merges its data into a pre-existing Secret (you must create the Secret yourself, e.g. via Helm). ES does not own it.
- `None` — never create/update a Secret (reserved for injector-style use).

**`target.deletionPolicy`:**
- `Retain` (default) — if the provider secret disappears, keep the target Secret. If a provider secret is missing, ES goes into `SecretSyncedError`.
- `Delete` — when all provider secrets are gone, delete the target Secret. A missing provider secret is **not** an error.
- `Merge` — remove only the keys sourced from the provider, leave the Secret itself. Missing provider secret is not an error.

**`refreshPolicy`:** `Periodic` (re-sync each `refreshInterval`; `0s` disables), `CreatedOnce` (create-if-absent, never update), `OnChange` (re-sync only when the ES spec/metadata changes — useful to avoid hammering the backend).

**`conversionStrategy`:** `Default` keeps values as-is; `Unicode` decodes `\uXXXX` escape sequences (handy for some providers' JSON encoding).

**`decodingStrategy`:** `None` (raw), `Base64`, `Base64URL`, `Auto` (sniff and decode if it looks encoded). Applied to the fetched value before storing.

**`nullBytePolicy`:** `Ignore` keeps NUL bytes in fetched data; `Fail` errors the reconcile if NUL bytes are present.

**`metadataPolicy`:** `Fetch` pulls provider-side tags/labels/metadata (for Vault KV v2, `custom_metadata`) so they're available to templates; `None` skips it.

### 4.4 Templating (`target.template`)
- `engineVersion: v2` is the only valid value (v1 removed). Go `text/template` + Sprig-like functions.
- `data` — map of output key → template string. Reference fetched values as `{{ .someKey }}`.
- `templateFrom[]` — load templates from a `ConfigMap`/`Secret`; `items[].templateAs` = `Values` (template only values) or `KeysAndValues` (template keys too); `target` = where output goes (`Data`/`Annotations`/`Labels`, or a nested path when `target.manifest` is set); `literal` is an inline template string.
- `mergePolicy`: `Replace` (template output replaces the data map) or `Merge` (overlay on top of fetched keys).
- `metadata.{labels,annotations,finalizers}` are applied to the resulting Secret. `type` sets the Secret type (e.g. `kubernetes.io/dockerconfigjson`, `kubernetes.io/tls`).
- Common helper functions available in v2 templates include `toString`, `toJson`/`fromJson`, `b64enc`/`b64dec`, `pkcs12key`/`pkcs12cert`, `jwkPrivateKeyPem`/`jwkPublicKeyPem`, `filterPEM`. (Build TLS/dockerconfig secrets without external tooling.)

### 4.5 `target.manifest` (generic target)
Set `target.manifest.{apiVersion,kind}` to make ESO render a **non-Secret** resource (ConfigMap, CR like an ArgoCD `Application`, etc.) from the template instead of a `Secret`. `templateFrom[].target` then accepts nested dotted paths (e.g. `spec.database.config`). Treat encryption/RBAC carefully — generic targets are not encrypted-at-rest like Secrets necessarily are.

---

## 5. ClusterExternalSecret — fan-out

Templates an `ExternalSecret` into many namespaces.

```yaml
apiVersion: external-secrets.io/v1
kind: ClusterExternalSecret
metadata:
  name: shared-secrets
spec:
  externalSecretName: app-secrets        # name of the ES created in each ns (defaults to CES name)
  externalSecretMetadata:
    labels:   { managed-by: eso }
    annotations: {}
  refreshTime: "1m0s"                     # *Duration. How often the CONTROLLER rechecks namespaces (JSON key "refreshTime")
  namespaceSelectors:                     # []LabelSelector, ORed. PREFERRED selection mechanism.
    - matchLabels: { environment: prod }
    - matchExpressions:
        - { key: team, operator: In, values: [payments, identity] }
  # namespaces: [team-a, team-b]          # DEPRECATED: explicit names, ORed with selector results
  # namespaceSelector: {...}              # DEPRECATED single selector — use namespaceSelectors
  externalSecretSpec:                     # the full ExternalSecretSpec (§4) replicated into each ns
    refreshInterval: "1h"
    secretStoreRef: { name: vault-kv, kind: ClusterSecretStore }
    target: { name: app-secrets, creationPolicy: Owner }
    data:
      - secretKey: db_password
        remoteRef: { key: myapp/config, property: db_password }
```
- Selection: `namespaceSelectors` (ORed) ∪ `namespaces`. New namespaces matching a selector get the ES automatically on the next `refreshTime`.
- `status.provisionedNamespaces` lists where it succeeded; `status.failedNamespaces[]` records per-namespace errors.
- Use `ClusterExternalSecret` + `ClusterSecretStore` together for true "define once, distribute everywhere."

---

## 6. PushSecret — write secrets INTO the backend (Vault)

`PushSecret` (`external-secrets.io/v1alpha1`) takes a local Secret (or generator output) and writes it to one or more stores.

```yaml
apiVersion: external-secrets.io/v1alpha1
kind: PushSecret
metadata:
  name: push-to-vault
spec:
  refreshInterval: "1h0m0s"               # default "1h0m0s"
  updatePolicy: Replace                   # Replace (default) | IfNotExists
  deletionPolicy: None                    # None (default) | Delete  (delete from provider when PushSecret/source deleted)
  secretStoreRefs:                        # one OR many targets
    - name: vault-kv
      kind: ClusterSecretStore            # SecretStore (default) | ClusterSecretStore
    # - labelSelector: { matchLabels: { tier: prod } }   # OR select stores by label
  selector:                               # exactly one of secret/generatorRef
    secret:
      name: app-secrets                   # local Secret in the PushSecret's namespace
      # selector: { matchLabels: {...} }  # OR push many Secrets by label
    # generatorRef: { apiVersion: generators.external-secrets.io/v1alpha1, kind: Password, name: pw }
  template: { ... }                       # optional ExternalSecretTemplate applied before push
  data:
    - match:
        secretKey: db_password            # key in the local Secret
        remoteRef:
          remoteKey: myapp/config         # destination path in Vault
          property: db_password           # write into a single field (merges into existing secret)
      conversionStrategy: None            # None (default) | ReverseUnicode
      metadata: {}                        # provider-specific (Vault: drives custom_metadata behavior)
  dataTo:                                 # bulk push without per-key mapping
    - storeRef: { name: vault-kv, kind: ClusterSecretStore }
      remoteKey: myapp/bundle             # if set: bundle ALL matched keys into ONE secret as JSON
      # (omit remoteKey to push each key as its own provider secret)
```

**Vault push behavior (from source):**
- ESO stamps `custom_metadata: { managed-by: external-secrets }` on pushed KV v2 secrets and **refuses to overwrite** a secret not carrying that marker (`"secret not managed by external-secrets"`). This prevents clobbering manually-managed secrets.
- `updatePolicy: IfNotExists` only creates absent keys; `Replace` overwrites.
- `provider.vault.checkAndSet.required: true` forces CAS on every write (KV v2) to prevent lost updates.
- `property` set → merge/patch a single field; unset → push the whole Secret as the secret body.
- `deletionPolicy: Delete` removes the provider secret when the PushSecret (or its source) is deleted; `None` leaves it.

---

## 7. Field defaults cheat-sheet

| Object.field | Default |
|---|---|
| `vault.version` | `v2` |
| `vault.auth.kubernetes.mountPath` | `kubernetes` |
| `vault.auth.appRole.path` | `approle` |
| `vault.auth.ldap.path` | `ldap` |
| `vault.auth.userPass.path` | `userpass` |
| `vault.auth.jwt.path` | `jwt` |
| `vault.auth.cert.path` | `cert` |
| `vault.auth.gcp.path` | `gcp` |
| `kubernetes.server.url` | `kubernetes.default` |
| `kubernetes.remoteNamespace` | `default` |
| `externalSecret.refreshInterval` | `1h0m0s` |
| `externalSecret.target.creationPolicy` | `Owner` |
| `externalSecret.target.deletionPolicy` | `Retain` |
| `externalSecret.template.engineVersion` | `v2` |
| `externalSecret.template.mergePolicy` | `Replace` |
| `remoteRef.metadataPolicy` | `None` |
| `remoteRef.conversionStrategy` | `Default` |
| `remoteRef.decodingStrategy` | `None` |
| `remoteRef.nullBytePolicy` | `Ignore` |
| `pushSecret.refreshInterval` | `1h0m0s` |
| `pushSecret.updatePolicy` | `Replace` |
| `pushSecret.deletionPolicy` | `None` |
| `pushSecretStoreRef.kind` | `SecretStore` |
| `secretStoreRef.kind` | `SecretStore` |
| `generatorRef.apiVersion` | `generators.external-secrets.io/v1alpha1` |
| `vaultDynamicSecret.resultType` | `Data` |

---

## 8. Validation rules & gotchas (enforced by CRD/webhook)

- `SecretStoreProvider`: exactly one provider key (`MinProperties=1, MaxProperties=1`). Setting two providers is rejected.
- `KubernetesAuth`: exactly one of `cert`/`token`/`serviceAccount`.
- `VaultAuth`: only one auth method should be populated (documented; pick one).
- `StoreSourceRef`/`StoreGeneratorSourceRef`: exactly one of `storeRef`/`generatorRef`.
- `ExternalSecretRewrite`: exactly one of `merge`/`regexp`/`transform` per entry.
- `caProvider.namespace` may only be set in a **ClusterSecretStore**.
- For ClusterSecretStore, set `.namespace` on every auth `secretRef`/`serviceAccountRef` unless you intend referent (per-ES-namespace) resolution.
- KV v2 path: write the **logical** key in `remoteRef.key` (`app/config`), never the physical `secret/data/app/config`.
- `data[].sourceRef.generatorRef` is deprecated/non-functional — use generators via `dataFrom[].sourceRef.generatorRef`.
- `refreshInterval: 0s` + default `refreshPolicy` = fetch once. To force "never update after create," prefer `refreshPolicy: CreatedOnce`.
- ESO must have RBAC to `create/update/patch` Secrets in target namespaces, and (for `serviceAccountRef`) `create` on `serviceaccounts/token`.

---

## 9. End-to-end example: central Vault → many namespaces

```yaml
# 1) One cluster-wide store pointing at central Vault (KV v2, k8s auth, Enterprise namespace)
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata: { name: central-vault }
spec:
  conditions:
    - namespaceSelector: { matchLabels: { eso: "enabled" } }
  provider:
    vault:
      server: https://vault.corp:8200
      path: kv
      version: v2
      namespace: platform
      caProvider: { type: ConfigMap, name: vault-ca, key: ca.crt, namespace: external-secrets }
      auth:
        kubernetes:
          mountPath: kubernetes-prod-cluster
          role: eso-reader
          serviceAccountRef: { name: external-secrets, namespace: external-secrets }
---
# 2) Fan a templated ExternalSecret into every labeled namespace
apiVersion: external-secrets.io/v1
kind: ClusterExternalSecret
metadata: { name: app-config }
spec:
  refreshTime: "1m"
  namespaceSelectors:
    - matchLabels: { eso: "enabled" }
  externalSecretSpec:
    refreshInterval: "15m"
    refreshPolicy: Periodic
    secretStoreRef: { name: central-vault, kind: ClusterSecretStore }
    target:
      name: app-config
      creationPolicy: Owner
      deletionPolicy: Delete
      template:
        type: Opaque
        data:
          DATABASE_URL: "postgres://{{ .user }}:{{ .password }}@{{ .host }}:5432/app"
    dataFrom:
      - extract: { key: app/database }
```

---

## 10. Quick verification commands

```bash
kubectl get clustersecretstores            # css: Status / Capabilities / Ready
kubectl get secretstores -A                # ss
kubectl get externalsecrets -A             # es: Store / Ready / Last Sync
kubectl get clusterexternalsecrets         # ces
kubectl describe externalsecret <name>     # see SecretSynced / SecretSyncedError + message
kubectl get externalsecret <name> -o jsonpath='{.status.conditions}'

# Confirm the served/stored API version and full field set on YOUR cluster:
kubectl explain externalsecret.spec --recursive --api-version=external-secrets.io/v1
kubectl explain clustersecretstore.spec.provider.vault.auth --api-version=external-secrets.io/v1
kubectl get crd externalsecrets.external-secrets.io -o jsonpath='{.spec.versions[*].name}'
```

> Always cross-check `kubectl explain ... --api-version=external-secrets.io/v1` against the installed chart version — field availability tracks the ESO version deployed in *your* cluster, which may lag this reference (built against v2.5.0 / `main`).
