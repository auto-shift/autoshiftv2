# external-secrets-operator AutoShift Policy

## Overview
This policy installs the external-secrets-operator operator using AutoShift patterns.

## Status
✅ **Operator Installation**: Ready to deploy  
🔧 **Configuration**: Requires operator-specific setup (see below)

## Quick Deploy

### Test Locally
```bash
# Validate policy renders correctly
helm template policies/external-secrets-operator/
```

### Enable on Clusters
Edit AutoShift values files to add the operator labels:

```yaml
# In autoshift/values/clustersets/hub.yaml (or other clusterset files)
hubClusterSets:
  hub:
    labels:
      external-secrets-operator: 'true'
      external-secrets-operator-subscription-name: 'openshift-external-secrets-operator'
      external-secrets-operator-channel: 'stable-v1'
      external-secrets-operator-source: 'redhat-operators'
      external-secrets-operator-source-namespace: 'openshift-marketplace'
      # external-secrets-operator-version: 'external-secrets-operator.v1.x.x'  # Optional: pin to specific CSV version

managedClusterSets:
  managed:
    labels:
      external-secrets-operator: 'true'
      external-secrets-operator-subscription-name: 'openshift-external-secrets-operator'
      external-secrets-operator-channel: 'stable-v1'
      external-secrets-operator-source: 'redhat-operators'
      external-secrets-operator-source-namespace: 'openshift-marketplace'
      # external-secrets-operator-version: 'external-secrets-operator.v1.x.x'  # Optional: pin to specific CSV version

# For specific clusters (optional override)
clusters:
  my-cluster:
    labels:
      external-secrets-operator: 'true'
      external-secrets-operator-channel: 'fast'  # Override channel for this cluster
```

Labels are defined in values files only — never directly on managed clusters. The cluster-labels policy handles propagating these labels from the values files to managed clusters.

### AutoShift Policy Discovery
New policies are automatically discovered by the ApplicationSet. In Git mode, the ApplicationSet uses a `policies/*` wildcard to pick up all subdirectories. No manual registration is required — simply adding your policy folder under `policies/` is sufficient.

## Configuration

### Namespace Scope
This operator is configured as:
- **Cluster-scoped**: Manages resources across all namespaces (default)
- **Namespace-scoped**: Limited to specific target namespaces (if `targetNamespaces` enabled in values.yaml)

To change scope, edit `values.yaml` and uncomment/configure the `targetNamespaces` field.

### Version Control
This policy supports AutoShift's operator version control system:

- **Automatic Upgrades**: By default, the operator follows automatic upgrade paths within its channel
- **Version Pinning**: Add `external-secrets-operator-version` label to pin to a specific CSV version
- **Manual Control**: Pinned versions require manual updates to upgrade

To pin to a specific version, set the version label in your clusterset or per-cluster values file:
```yaml
external-secrets-operator-version: 'external-secrets-operator.v1.x.x'
```

Find available CSV versions:
```bash
# List available versions for this operator
oc get packagemanifests external-secrets-operator -o jsonpath='{.status.channels[*].currentCSV}'
```

## Secret Stores (`config.eso.secretStores`)

`policy-external-secrets-operator-stores` creates ESO `SecretStore` /
`ClusterSecretStore` objects on each managed cluster from the per-cluster
rendered-config ConfigMap (read via hub-template `lookup`, the same mechanism the
`cluster-config-maps` policy uses).

Three policies are driven from the per-cluster ESO config:

| Policy | Creates |
|---|---|
| `policy-external-secrets-operator-stores` | the `SecretStore` / `ClusterSecretStore` objects + their auth `Secret`s (`authSecretConfig`), from `config.eso.secretStores` |
| `policy-external-secrets-operator-cert-auth-rbac` | the RBAC backing the kubernetes-provider `cert` auth method (`certAuthRBAC`) — see [Kubernetes cert auth RBAC](#kubernetes-cert-auth-rbac-certauthrbac) |
| `policy-external-secrets-operator-secret-reader` | the `secret-reader` ServiceAccount + RBAC for consuming provisioned Secrets — see [Reading provisioned Secrets](#reading-provisioned-secrets-secret-reader) |

The first two read `config.eso.secretStores`, below.

`config.eso.secretStores` is a **list of single-key items**. The key selects the
object kind; the rendered manifest differs by kind:

| Key | Renders | Scope | `metadata.namespace` |
|---|---|---|---|
| `clusterSecretStore` | `kind: ClusterSecretStore` | cluster-wide | omitted |
| `secretStore` | `kind: SecretStore` | namespaced | **required** |

Each item has `name` (and `namespace` for `secretStore`), a `spec` (the ESO
`SecretStoreSpec`, rendered verbatim), and an optional `authSecretConfig` (provisions
the auth Secret the store references — see [Auth secrets](#auth-secrets-authsecretconfig)).
The provider is independent of the kind — any provider can be used under either kind.
Set this where the config lives (`hubClusterSets.*.config` /
`managedClusterSets.*.config` / per-cluster `clusters/<name>.yaml` config) — never as a
label.

### ClusterSecretStore vs SecretStore — what changes in `spec`

These rules come from the ESO API (see `eso-vault-reference.md` §1); the policy does
not enforce them, so set them correctly in config:

- `caProvider.namespace` and `serviceAccountRef.namespace` / `secretRef.namespace` on
  auth refs are **only honored by `ClusterSecretStore`**. On a `SecretStore` they are
  ignored (refs always resolve in the SecretStore's own namespace) — omit them.
- `spec.conditions` (namespace restrictions) apply to `ClusterSecretStore` **only**.
- For a `ClusterSecretStore`, set an explicit `.namespace` on every auth ref unless
  you want referent (per-consuming-namespace) resolution.

### Auth secrets (`authSecretConfig`)

Most auth methods reference a Kubernetes Secret (the Vault token, AppRole SecretID,
client cert/key, a bearer token, cloud creds, ...). `authSecretConfig` — an optional
key sibling to `spec` on any store item — provisions that Secret so you don't have to
manage it separately.

**`spec` is authoritative.** You write the auth ref in `spec` the normal ESO way; you do
**not** restate the Secret's name/key/namespace anywhere else. `authSecretConfig` only
adds the one new fact — where on the **hub** the value comes from — and names which ref
to mirror:

```yaml
authSecretConfig:
  fromRef: vaultToken      # which auth ref in spec to mirror (see table below)
  source:                  # the existing Secret on the HUB to copy the value from
    namespace: eso-auth-secrets
    name: vault-token
    key: token
spec:
  provider:
    vault:
      auth:
        tokenSecretRef:    # authoritative — the policy reads name/key/namespace from here
          name: vault-token
          key: token
```

The policy reads the `fromRef` ref out of `spec`, then creates a Secret on the managed
cluster named/keyed exactly as that ref says, with the value copied from `source` via the
hub `fromSecret` function. No `spec` mutation, nothing to keep in sync.

**Where the Secret is created:**
- `SecretStore` → the store's own namespace (ESO resolves auth refs there; the ref's
  `.namespace` is ignored).
- `ClusterSecretStore` → the ref's `.namespace`, which is therefore **required** (a CSS
  ref with no namespace means per-consuming-namespace/referent creds — provision those
  yourself).

**Whole-secret copy (omit `source.key`).** For a multi-key method (cert's
`clientCert` + `clientKey`), drop `source.key` and the **entire** hub Secret is copied
verbatim — all keys, original names. One entry instead of one per key. `fromRef` is then
only used to locate the target Secret's name/namespace (either cert ref does):

```yaml
authSecretConfig:
  fromRef: kubernetesCertClient            # locates target Secret eso-cert (from spec)
  source: { namespace: eso-auth-secrets, name: eso-cert-src }   # no key -> copy all keys
spec:
  provider:
    kubernetes:
      auth:
        cert:
          clientCert: { name: eso-cert, key: tls.crt }   # both refs name eso-cert;
          clientKey:  { name: eso-cert, key: tls.key }   # the hub Secret supplies both keys
```

The hub Secret's key names must match what the refs expect (`tls.crt`/`tls.key` here),
since whole-copy preserves names (no rename).

**List form.** `authSecretConfig` may also be a **list** of entries — use it to copy
distinct single keys, or to gather keys from *different* hub Secrets into one target
(entries whose refs share a Secret name merge):

```yaml
authSecretConfig:
  - fromRef: kubernetesCertClient
    source: { namespace: eso-auth-secrets, name: cert-bundle, key: tls.crt }
  - fromRef: kubernetesCertKey
    source: { namespace: eso-auth-secrets, name: key-vault, key: tls.key }
```

Notes:
- Single-key form: `source.key` and the ref's `key` may differ — the value is copied, the
  target key is taken from the spec ref. Whole-copy form: keys are preserved as-is.
- Omit `authSecretConfig` for auth methods that need no static Secret (Vault `kubernetes`
  auth via `serviceAccountRef`, or the Kubernetes provider's `serviceAccount` auth — ESO
  mints the token).
- The `source` Secret must exist on the hub and the policy must have RBAC to read it;
  otherwise the hub `fromSecret`/`lookup` fails the template.

#### Supported `fromRef` values

`fromRef` selects which ref in `spec` to mirror. The template **fails** on an unknown
`fromRef`, a provider not present in `spec.provider`, or a `fromRef` pointing at a ref
that isn't actually set in `spec`.

| `fromRef` | Reads the ref at `spec.provider.…` |
|---|---|
| `vaultToken` | `vault.auth.tokenSecretRef` |
| `vaultAppRoleSecretId` | `vault.auth.appRole.secretRef` |
| `vaultAppRoleRoleId` | `vault.auth.appRole.roleRef` |
| `vaultLdap` | `vault.auth.ldap.secretRef` |
| `vaultUserPass` | `vault.auth.userPass.secretRef` |
| `vaultJwt` | `vault.auth.jwt.secretRef` |
| `vaultKubernetes` | `vault.auth.kubernetes.secretRef` |
| `vaultCert` | `vault.auth.cert.secretRef` (client key) |
| `vaultCertClient` | `vault.auth.cert.clientCert` |
| `kubernetesToken` | `kubernetes.auth.token.bearerToken` |
| `kubernetesCertClient` | `kubernetes.auth.cert.clientCert` |
| `kubernetesCertKey` | `kubernetes.auth.cert.clientKey` |

`iam`/`gcp` (multi-field nested creds) are intentionally absent — provision those Secrets
out of band.

This table is the policy's `internal.authRefPaths` map in `values.yaml`, keyed by
`fromRef` token with value `[provider, ...path under provider.auth]`. To support a new
auth ref, add an entry there — no template edits needed.

### Vault provider — common options

```yaml
config:
  eso:
    secretStores:
      - clusterSecretStore:
          name: vault-backend
          spec:
            # controller: ""                 # optional: which ESO instance owns this store
            # refreshInterval: 0              # optional: seconds; how often store auth is revalidated
            conditions:                       # ClusterSecretStore only — restrict consuming namespaces
              - namespaceSelector:
                  matchLabels:
                    eso: 'enabled'
                # namespaces: [team-a, team-b]
                # namespaceRegexes: ['^app-.*$']
            provider:
              vault:
                server: 'https://vault.example.com:8200'   # required
                path: 'secret'                             # KV mount; /data auto-appended for v2
                version: 'v2'                              # v1 | v2 (default v2)
                # namespace: 'platform'                    # Vault Enterprise namespace (data requests)
                caProvider:                                # pull CA from a ConfigMap/Secret
                  type: ConfigMap                          # ConfigMap | Secret
                  name: vault-ca
                  key: ca.crt
                  namespace: 'external-secrets-operator'   # ClusterSecretStore only
                # caBundle: <base64 PEM>                   # OR inline the CA instead of caProvider
                auth:                                      # pick exactly ONE method
                  kubernetes:                              # see "Vault auth methods" below for all options
                    mountPath: 'kubernetes'                # default 'kubernetes'
                    role: 'eso-reader'                     # required (Vault role)
                    serviceAccountRef:
                      name: 'external-secrets'
                      namespace: 'external-secrets-operator'
                      # audiences: ['vault']
```

A namespaced equivalent (`SecretStore`) drops cross-namespace fields. The
`tokenSecretRef` is written normally in `spec`; `authSecretConfig` just provisions the
Secret it points at from a hub source:

```yaml
      - secretStore:
          name: vault-backend
          namespace: 'team-a'                  # required for SecretStore
          authSecretConfig:
            fromRef: vaultToken                # mirror spec.provider.vault.auth.tokenSecretRef
            source:
              namespace: 'eso-auth-secrets'    # hub namespace holding the token
              name: vault-token
              key: token
          spec:
            provider:
              vault:
                server: 'https://vault.example.com:8200'
                path: 'secret'
                version: 'v2'
                auth:
                  tokenSecretRef:              # authoritative; Secret created in team-a from source
                    name: vault-token
                    key: token
```

### Vault auth methods

Set exactly one of these under `spec.provider.vault.auth`. Fields/defaults are from
`eso-vault-reference.md` §2.2. On a `ClusterSecretStore` you may add `.namespace` to any
`secretRef` / `serviceAccountRef` to pin it to a central namespace; on a `SecretStore`
those `.namespace` fields are ignored. `auth.namespace` (Vault Enterprise auth namespace)
may be set alongside any method.

**`kubernetes`** — Vault Kubernetes auth (most common in-cluster):
```yaml
auth:
  kubernetes:
    mountPath: 'kubernetes'                  # default 'kubernetes'
    role: 'eso-reader'                       # required — Vault role bound to the SA
    serviceAccountRef:                       # preferred: mint a short-lived SA token
      name: 'external-secrets'
      namespace: 'external-secrets-operator' # ClusterSecretStore only
      # audiences: ['vault']
    # secretRef:                             # OR a Secret holding a SA JWT (key defaults to 'token')
    #   name: sa-token
    #   key: token
    # (omit both to use the ESO controller's own SA token)
```

**`tokenSecretRef`** — static Vault token:
```yaml
auth:
  tokenSecretRef:
    name: vault-token
    key: token                               # required
    # namespace: external-secrets-operator   # ClusterSecretStore only
```

**`appRole`** — AppRole RoleID + SecretID:
```yaml
auth:
  appRole:
    path: 'approle'                          # default 'approle' (mount path)
    roleId: '<role-id>'                      # inline RoleID...
    # roleRef:                               # ...OR pull RoleID from a Secret
    #   name: approle-creds
    #   key: role-id
    secretRef:                               # SecretID (required)
      name: approle-creds
      key: secret-id
```

**`ldap`**:
```yaml
auth:
  ldap:
    path: 'ldap'                             # default 'ldap'
    username: 'svc-user'                     # required
    secretRef:
      name: ldap-creds
      key: password
```

**`userPass`**:
```yaml
auth:
  userPass:
    path: 'userpass'                         # default 'userpass'
    username: 'svc-user'                     # required
    secretRef:
      name: userpass-creds
      key: password
```

**`jwt` / OIDC**:
```yaml
auth:
  jwt:
    path: 'jwt'                              # default 'jwt'
    role: 'my-jwt-role'
    # ---- one token source ----
    secretRef:                               # static JWT in a Secret
      name: jwt-token
      key: jwt
    # kubernetesServiceAccountToken:         # OR mint a SA token via TokenRequest
    #   serviceAccountRef:
    #     name: my-app-sa
    #     audiences: ['vault']
```

**`cert`** — TLS certificate auth method:
```yaml
auth:
  cert:
    path: 'cert'                             # default 'cert'
    vaultRole: 'my-cert-role'                # optional
    clientCert:                              # client cert
      name: cert-auth
      key: tls.crt
    secretRef:                               # client private key
      name: cert-auth
      key: tls.key
```

**`iam`** — AWS IAM auth (Vault verifies a signed STS request):
```yaml
auth:
  iam:
    path: 'aws'                              # Vault mount of the AWS auth method
    vaultRole: 'my-vault-aws-role'           # required
    region: 'us-east-1'
    # role: 'arn:aws:iam::111122223333:role/assume-first'  # optional AWS role to assume first
    # externalID: '...'                      # optional STS ExternalId
    # vaultAwsIamServerID: 'vault.example.com'             # X-Vault-AWS-IAM-Server-ID header
    secretRef:                               # static creds...
      accessKeyIDSecretRef:     { name: aws-creds, key: access-key-id }
      secretAccessKeySecretRef: { name: aws-creds, key: secret-access-key }
      sessionTokenSecretRef:    { name: aws-creds, key: session-token }    # required if temporary
    # jwt:                                    # ...OR IRSA via a SA
    #   serviceAccountRef: { name: irsa-sa }
    # (omit secretRef and jwt to use the controller pod's IRSA / EKS Pod Identity)
```

**`gcp`** — GCP IAM auth:
```yaml
auth:
  gcp:
    path: 'gcp'                              # default 'gcp'
    role: 'my-vault-gcp-role'                # required
    # projectID: 'my-project'
    # ---- credential source: one of these, or omit for pod identity ----
    workloadIdentity:                        # GKE Workload Identity
      serviceAccountRef: { name: ksa }
      clusterLocation: us-central1
      clusterName: my-gke
      clusterProjectID: my-project
    # secretRef:                             # OR a GCP SA key
    #   secretAccessKeySecretRef: { name: gcp-sa-key, key: key.json }
    # serviceAccountRef: { name: impersonate-this-gsa }    # OR SA impersonation
```

### Kubernetes provider — read Secrets from a remote cluster

Use this when the source of truth is native `Secret`s in another cluster's API server
(`eso-vault-reference.md` §3.2). ESO connects to the remote apiserver and reads
Secrets from `remoteNamespace`.

```yaml
config:
  eso:
    secretStores:
      - clusterSecretStore:
          name: remote-cluster
          spec:
            provider:
              kubernetes:
                server:
                  url: 'https://remote-apiserver.example.com:6443'  # default: in-cluster
                  caProvider:                                        # validate remote apiserver cert
                    type: ConfigMap
                    name: remote-ca
                    key: ca.crt
                    namespace: 'external-secrets-operator'
                  # caBundle: <base64 PEM>                           # OR inline the CA
                remoteNamespace: 'shared-secrets'                    # remote ns to read (default 'default')
                auth:
                  # ---- pick exactly ONE: serviceAccount | cert | token ----
                  serviceAccount:
                    name: eso-remote-reader
                    namespace: 'external-secrets-operator'
                    # audiences: ['https://remote-apiserver.example.com']
                  # cert:
                  #   clientCert: { name: remote-creds, key: tls.crt }
                  #   clientKey:  { name: remote-creds, key: tls.key }
                  # token:
                  #   bearerToken: { name: remote-creds, key: token }
                # authRef:                                           # OR a Secret bundling kubeconfig-style auth
                #   name: kubeconfig-secret
```

> **Out-of-band prerequisites (not created by this policy):** the `remote-ca`
> ConfigMap, the `eso-remote-reader` ServiceAccount and its token on this cluster,
> and RBAC on the *remote* cluster granting that identity `get`/`list` on Secrets in
> `remoteNamespace`. An `ExternalSecret` then reads remote Secrets where
> `remoteRef.key` = remote Secret name and `remoteRef.property` = a key within it.

### Kubernetes cert auth RBAC (`certAuthRBAC`)

When a store uses the **kubernetes** provider with **cert** auth
(`spec.provider.kubernetes.auth.cert`), the apiserver identifies the client by the
certificate's **CN** (a `User`). A **separate policy**,
`policy-external-secrets-operator-cert-auth-rbac`, reads the same
`config.eso.secretStores` list and generates the RBAC that grants that user secret
access — so the cert can read/write the secrets the store manages. (The
`...-stores` policy creates the store object; this one creates only the RBAC. Both are
gated on the `autoshift.io/external-secrets-operator` label.)

Add `certAuthRBAC` (sibling of `spec`) with the CN as `username`. For now the username
is a literal value (deriving it from the cert programmatically can come later). The
policy then creates a (Cluster)Role + binding granting `create`/`update`/`list`/`delete`
on `secrets`:

```yaml
- clusterSecretStore:
    name: remote-cert
    certAuthRBAC:
      username: 'eso-remote-cert-cn'        # the cert CN -> RBAC subject (kind: User)
      # namespaces: [team-a, team-b]        # optional: explicit target namespaces
      # verbs: [create, update, list, delete, get]   # optional: override the default verb set
    spec:
      provider:
        kubernetes:
          server: { url: 'https://remote-apiserver.example.com:6443' }
          remoteNamespace: 'shared-secrets'
          auth:
            cert:
              clientCert: { name: eso-remote-cert, key: tls.crt }
              clientKey:  { name: eso-remote-cert, key: tls.key }
```

Scope of the generated RBAC, by store kind:

| Store kind | Target namespaces | Objects created |
|---|---|---|
| `SecretStore` | `certAuthRBAC.namespaces`, else `remoteNamespace`, else the store's namespace | `Role` + `RoleBinding` per namespace |
| `ClusterSecretStore` (no scoping) | — (cluster-wide) | `ClusterRole` + `ClusterRoleBinding` |
| `ClusterSecretStore` with `certAuthRBAC.namespaces` or explicit `spec.conditions[].namespaces` | those namespaces | `ClusterRole` + a `RoleBinding` per namespace |

Notes:
- `username` is **required** when a kubernetes store uses cert auth — the template
  **fails** otherwise (`spec.provider.kubernetes.auth.cert requires
  certAuthRBAC.username`).
- RBAC bindings cannot express label selectors, so a `ClusterSecretStore` whose
  `conditions` use only `namespaceSelector`/`namespaceRegexes` (no explicit
  `namespaces`) falls back to a cluster-wide `ClusterRoleBinding`.
- Role/binding names are `eso-cert-<kind>-<storeName>`.
- The cert Secret itself (`clientCert`/`clientKey`) can be provisioned by an
  `authSecretConfig` entry that omits `source.key` (whole-Secret copy pulls both
  `tls.crt` + `tls.key` from one hub Secret). Or supply it out of band.

### Full examples

Complete `config.eso.secretStores` blocks you can drop into a clusterset
(`hubClusterSets.*.config` / `managedClusterSets.*.config`) or per-cluster
(`clusters/<name>.yaml`) config. Examples 1–2 write the auth ref in `spec` normally and
use `authSecretConfig` (`fromRef` + `source`) to provision the Secret it points at from
the hub. Example 3 shows cert auth, where `certAuthRBAC` drives the separate RBAC policy.

#### 1. Kubernetes provider + `authSecretConfig` (bearer-token auth)

Read native `Secret`s from a remote cluster's API server, authenticating with a bearer
token. The `token.bearerToken` ref is written in `spec`; `fromRef: kubernetesToken`
tells the policy to provision *that* Secret, copying the value from a hub Secret. Since
this is a `ClusterSecretStore`, the ref carries `.namespace` (where the Secret is created).

```yaml
config:
  eso:
    secretStores:
      - clusterSecretStore:
          name: remote-cluster
          authSecretConfig:
            fromRef: kubernetesToken               # mirror spec.provider.kubernetes.auth.token.bearerToken
            source:                                # existing Secret on the HUB to copy from
              namespace: eso-auth-secrets
              name: remote-reader-token
              key: token
          spec:
            provider:
              kubernetes:
                server:
                  url: https://remote-apiserver.example.com:6443
                  caProvider:                      # validate the remote apiserver cert
                    type: ConfigMap
                    name: remote-ca
                    key: ca.crt
                    namespace: external-secrets-operator   # ClusterSecretStore only
                remoteNamespace: shared-secrets    # remote namespace to read from
                auth:
                  token:                           # authoritative; Secret provisioned from source above
                    bearerToken:
                      name: remote-reader-token
                      key: token
                      namespace: external-secrets-operator   # CSS: where the Secret is created
```

> The remote apiserver's bearer token (a long-lived SA token, or a kubeconfig token)
> must already exist as the `remote-reader-token` Secret on the **hub** in
> `eso-auth-secrets`, and the identity it represents needs `get`/`list` on Secrets in
> `remoteNamespace` on the **remote** cluster. The `remote-ca` ConfigMap is an
> out-of-band prerequisite. `KubernetesAuth` accepts exactly one of
> `serviceAccount`/`cert`/`token`, so don't combine this with another method.

#### 2. Vault provider with OIDC (`jwt`) authentication

Vault's OIDC/JWT auth method verifies a JWT and returns a Vault token. Here a static JWT
lives in a hub Secret; the `jwt.secretRef` is written in `spec`, and `fromRef: vaultJwt`
provisions that Secret on the managed cluster from the hub source. The OIDC backend is
mounted at `oidc` (set `path` to your actual mount).

```yaml
config:
  eso:
    secretStores:
      - clusterSecretStore:
          name: vault-oidc
          authSecretConfig:
            fromRef: vaultJwt                      # mirror spec.provider.vault.auth.jwt.secretRef
            source:                                # existing Secret on the HUB to copy from
              namespace: eso-auth-secrets
              name: vault-oidc-jwt
              key: jwt
          spec:
            provider:
              vault:
                server: https://vault.example.com:8200    # required
                path: secret                       # KV mount; /data auto-appended for v2
                version: v2
                caProvider:                        # pull Vault's CA from a ConfigMap
                  type: ConfigMap
                  name: vault-ca
                  key: ca.crt
                  namespace: external-secrets-operator     # ClusterSecretStore only
                auth:
                  jwt:
                    path: oidc                     # mount path of the OIDC/JWT auth method (default 'jwt')
                    role: eso-reader               # Vault JWT/OIDC role bound to policies
                    secretRef:                     # authoritative; Secret provisioned from source above
                      name: vault-oidc-jwt
                      key: jwt
                      namespace: external-secrets-operator   # CSS: where the Secret is created
```

> The JWT must already exist as the `vault-oidc-jwt` Secret on the **hub** in
> `eso-auth-secrets`, and the Vault `oidc` role must accept that JWT's issuer/claims.
> For a workload-minted token instead of a static JWT, drop `authSecretConfig` and set
> `jwt.kubernetesServiceAccountToken.serviceAccountRef` in `spec` directly (ESO mints
> the SA token via the TokenRequest API — no static Secret needed).

#### 3. Kubernetes provider + cert auth (generates RBAC)

Authenticate to the remote apiserver with a client certificate. Set
`certAuthRBAC.username` to the cert's CN; the companion
`policy-external-secrets-operator-cert-auth-rbac` policy then creates the RBAC: this is
a `ClusterSecretStore` scoped to two namespaces, so a `ClusterRole` plus a `RoleBinding`
in each of `team-a`/`team-b` are generated (bound to `User: eso-remote-cert-cn`). The
cert/key Secret can be provisioned by an `authSecretConfig` that omits `source.key`
(whole-Secret copy — see [Auth secrets](#auth-secrets-authsecretconfig)) or supplied out
of band — shown out of band here.

```yaml
config:
  eso:
    secretStores:
      - clusterSecretStore:
          name: remote-cert
          certAuthRBAC:
            username: eso-remote-cert-cn           # cert CN -> RBAC subject (kind: User)
            # namespaces: [team-a, team-b]          # optional: override the derived target namespaces
            # verbs: [create, update, list, delete] # optional: override the default verb set
          spec:
            conditions:                            # restricts which namespaces may use the store;
              - namespaces:                        # explicit names also scope the generated RBAC
                  - team-a
                  - team-b
            provider:
              kubernetes:
                server:
                  url: https://remote-apiserver.example.com:6443
                  caProvider:
                    type: ConfigMap
                    name: remote-ca
                    key: ca.crt
                    namespace: external-secrets-operator
                remoteNamespace: shared-secrets
                auth:
                  cert:                            # client cert/key Secret (provision out of band)
                    clientCert: { name: eso-remote-cert, key: tls.crt }
                    clientKey:  { name: eso-remote-cert, key: tls.key }
```

> The `eso-remote-cert` Secret (with `tls.crt`/`tls.key`) and the `remote-ca` ConfigMap
> are out-of-band prerequisites. Drop the `conditions` block to make the generated RBAC
> cluster-wide (`ClusterRole` + `ClusterRoleBinding`); use a `secretStore` instead for a
> namespaced `Role` + `RoleBinding`.

## Reading provisioned Secrets (`secret-reader`)

The Secrets ESO provisions are consumed by other AutoShift components. Rather than each
reaching for ESO internals, `policy-external-secrets-operator-secret-reader` creates a
single read-only identity — the **`secret-reader` ServiceAccount** (in the ESO operator
namespace) — and grants it `get`/`list`/`watch` on `secrets` in a configured set of
namespaces (one `ClusterRole` named `eso-secret-reader` + a `RoleBinding` per namespace).

The namespace set is read from the per-cluster rendered config and de-duplicated:

| Config key | Shape | Role |
|---|---|---|
| `config.externalSecretsOperator.secretReaderNamespaces` | list | namespaces to grant read on |
| `config.defaultSecretsNamespace` | string (top-level) | the cluster's default Secrets namespace; **appended** to the list above |

```yaml
config:
  defaultSecretsNamespace: app-secrets        # top-level; appended to the reader namespaces
  externalSecretsOperator:
    secretReaderNamespaces:
      - team-a
      - team-b
# -> ServiceAccount secret-reader (ns: external-secrets-operator)
#    ClusterRole eso-secret-reader (get/list/watch secrets)
#    RoleBinding eso-secret-reader in team-a, team-b, app-secrets -> the SA
```

The ServiceAccount name defaults to `secret-reader` (`externalSecretsOperator.secretReaderName`
in `values.yaml`) and is created in `externalSecretsOperator.namespace`. With no namespaces
configured, the SA + ClusterRole are still created (no bindings).

### Verify

```bash
oc get clustersecretstores                       # Status / Capabilities / Ready
oc get secretstores -A
oc describe clustersecretstore <name>             # check Ready condition + reason

# cert-auth RBAC generated by policy-external-secrets-operator-cert-auth-rbac:
oc get clusterrole,clusterrolebinding | grep eso-cert-
oc get role,rolebinding -A | grep eso-cert-

# secret-reader identity + RBAC (policy-external-secrets-operator-secret-reader):
oc get serviceaccount secret-reader -n external-secrets-operator
oc get clusterrole eso-secret-reader
oc get rolebinding -A | grep eso-secret-reader
```

## Next Steps: Configuration

### 1. Explore Installed CRDs
After operator installation, check what Custom Resources are available:
```bash
# Wait for operator to install
oc get pods -n external-secrets-operator

# Check available CRDs
oc get crds | grep external-secrets-operator

# Explore CRD specifications
oc explain <CustomResourceName>
```

### 2. Create Configuration Policies
Add operator-specific configuration policies to `templates/` directory.

#### Common Patterns:
- `policy-external-secrets-operator-config.yaml` - Main configuration
- `policy-external-secrets-operator-<feature>.yaml` - Feature-specific configs

#### Template Structure:
```yaml
{{- $policyName := "policy-external-secrets-operator-config" }}
{{- $placementName := "placement-policy-external-secrets-operator-config" }}

apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: {{ $policyName }}
  namespace: {{ .Values.policy_namespace }}
  annotations:
    policy.open-cluster-management.io/standards: NIST SP 800-53
    policy.open-cluster-management.io/categories: CM Configuration Management
    policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
spec:
  disabled: false
  dependencies:
    - name: policy-external-secrets-operator-install
      namespace: {{ .Values.policy_namespace }}
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: external-secrets-operator-config
        spec:
          remediationAction: enforce
          severity: high
          evaluationInterval:
            compliant: {{ ((($.Values.autoshift).evaluationInterval).compliant) | default "10m" }}
            noncompliant: {{ ((($.Values.autoshift).evaluationInterval).noncompliant) | default "30s" }}
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: # Your operator's API version
                kind: # Your operator's Custom Resource
                metadata:
                  name: external-secrets-operator-config
                  namespace: {{ .Values.externalSecretsOperator.namespace }}
                spec:
                  # Your operator-specific configuration
                  # Use dynamic labels when needed:
                  # setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/external-secrets-operator-setting" | default "default-value" {{ "hub}}" }}'
          pruneObjectBehavior: None
---
# Use same placement as operator install or create specific targeting
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: {{ $placementName }}
  namespace: {{ .Values.policy_namespace }}
spec:
  clusterSets:
  {{- range $clusterSet, $value := $.Values.hubClusterSets }}
    - {{ $clusterSet }}
  {{- end }}
  {{- range $clusterSet, $value := $.Values.managedClusterSets }}
    - {{ $clusterSet }}
  {{- end }}
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchExpressions:
            - key: 'autoshift.io/external-secrets-operator'
              operator: In
              values:
              - 'true'
  tolerations:
    - key: cluster.open-cluster-management.io/unreachable
      operator: Exists
    - key: cluster.open-cluster-management.io/unavailable
      operator: Exists
---
apiVersion: policy.open-cluster-management.io/v1
kind: PlacementBinding
metadata:
  name: {{ $placementName }}
  namespace: {{ .Values.policy_namespace }}
placementRef:
  name: {{ $placementName }}
  apiGroup: cluster.open-cluster-management.io
  kind: Placement
subjects:
  - name: {{ $policyName }}
    apiGroup: policy.open-cluster-management.io
    kind: Policy
```

### 3. Reference Examples
**Study similar complexity policies:**
- **Simple**: `policies/openshift-gitops/` - Basic operator + ArgoCD config
- **Medium**: `policies/advanced-cluster-security/` - Multiple related policies
- **Complex**: `policies/metallb/` - Multiple configuration types (L2, BGP, etc.)
- **Advanced**: `policies/openshift-data-foundation/` - Storage cluster configuration

### 4. AutoShift Labels
Add configuration labels to `values.yaml` and use in templates:

```yaml
# Add to values.yaml AutoShift Labels Documentation:
# external-secrets-operator-setting<string>: Configuration option (default: 'value')
# external-secrets-operator-feature-enabled<bool>: Enable optional feature (default: 'false')
# external-secrets-operator-provider<string>: Provider-specific config (default: 'generic')

# Use in templates:
setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/external-secrets-operator-setting" | default "default-value" {{ "hub}}" }}'
```

## Common Patterns

### CSV Status Checking (Optional)
For operators that need installation verification:
```yaml
- objectDefinition:
    apiVersion: policy.open-cluster-management.io/v1
    kind: ConfigurationPolicy
    metadata:
      name: external-secrets-operator-csv-status
    spec:
      remediationAction: inform
      severity: high
      evaluationInterval:
        compliant: {{ ((($.Values.autoshift).evaluationInterval).compliant) | default "10m" }}
        noncompliant: {{ ((($.Values.autoshift).evaluationInterval).noncompliant) | default "30s" }}
      object-templates:
        - complianceType: musthave
          objectDefinition:
            apiVersion: operators.coreos.com/v1alpha1
            kind: ClusterServiceVersion
            metadata:
              namespace: {{ .Values.externalSecretsOperator.namespace }}
            status:
              phase: Succeeded
```

## Troubleshooting

### Policy Not Applied
1. Check cluster labels: `oc get managedcluster <cluster> --show-labels`
2. Verify placement: `oc get placement -n open-cluster-policies`
3. Check policy status: `oc describe policy policy-external-secrets-operator-install`

### Operator Installation Issues
1. Check subscription: `oc get subscription -n external-secrets-operator`
2. Check install plan: `oc get installplan -n external-secrets-operator`
3. Verify operator source exists: `oc get catalogsource -n openshift-marketplace`

### Template Rendering Issues
1. Test locally: `helm template policies/external-secrets-operator/`
2. Check hub escaping: Look for `{{ "{{hub" }} ... {{ "hub}}" }}` patterns
3. Validate YAML: `helm lint policies/external-secrets-operator/`

## Resources
- [Operator Documentation](https://operatorhub.io/operator/external-secrets-operator) - Find your operator details
- [AutoShift Developer Guide](../../docs/developer-guide.md) - Comprehensive policy development guide
- [ACM Policy Documentation](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes) - Policy syntax reference in Governence Section
- [Similar Policies](../) - Browse other policies for patterns and examples