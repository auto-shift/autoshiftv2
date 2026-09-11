# ztwim

Deploys the Red Hat **Zero Trust Workload Identity Manager** operator and its SPIRE control plane,
giving every workload on the cluster a short-lived, cryptographically verifiable SPIFFE identity
(an SVID) instead of a long-lived secret.

The operator is the productized build of upstream SPIRE, the reference implementation of the SPIFFE
standard. AutoShift installs the operator and configures its operands; SPIRE itself then handles
attestation, issuance and rotation.

## What it deploys

| Policy | Creates | Placement |
|---|---|---|
| `policy-ztwim-operator-install` | Operator subscription and namespace | `autoshift.io/ztwim: 'true'` |
| `policy-ztwim-instance` | `ZeroTrustWorkloadIdentityManager` (trust domain, cluster name) | same |
| `policy-ztwim-config` | `SpireServer`, `SpireAgent`, `SpiffeCSIDriver`, `SpireOIDCDiscoveryProvider` | same |
| `policy-ztwim-federation` | `ClusterFederatedTrustDomain` per foreign trust domain | `autoshift.io/ztwim-federation: 'true'` |
| `policy-ztwim-ready` | inform only: SPIRE server readiness | same as install |

The policies are chained with `dependencies`, because the operand controllers read the trust domain
and cluster name from the `ZeroTrustWorkloadIdentityManager` singleton.

## Enable it

```yaml
hubClusterSets:
  hub:
    labels:
      ztwim: 'true'
```

Everything else has a working default. The trust domain becomes the cluster base domain and the
SPIRE CA is self-signed, which is enough to start issuing SVIDs on a single cluster.

## Configuration

All settings are data, so they live in `config.ztwim` rather than in labels. The full shape is in
[`autoshift/values/clustersets/_example.yaml`](../../../autoshift/values/clustersets/_example.yaml).
Every key is optional.

| Key | Default | Notes |
|---|---|---|
| `trustDomain` | cluster base domain | The SPIFFE identity namespace. See the warning below. |
| `clusterName` | ManagedCluster name | Stamped into every agent SPIFFE ID by the node attestor. |
| `jwtIssuer` | `https://spire-oidc.<ingress domain>` | Must resolve under the `*.apps` wildcard. |
| `caSubject` | `autoshift` / `US` / `AutoShift` | Subject of the SPIRE CA certificate. |
| `persistence` | `5Gi`, `ReadWriteOnce` | Datastore volume. `storageClass` is omitted unless set. |
| `datastore` | `sqlite3` | See the database note below. |
| `upstreamAuthority` | omitted | Chains the SPIRE CA under an external authority. |
| `federatedTrustDomains` | empty | Requires the `ztwim-federation` label. |

> **Changing the trust domain is destructive.** SPIRE caches its CA and every issued identity
> against the trust domain in its datastore. Changing it after the fact orphans that state, and
> recovery means clearing the datastore volume. Decide it before the first deployment.

### Rooting the SPIRE CA in an upstream authority

By default the SPIRE server mints its own self-signed root. To chain it under an existing authority,
set `config.ztwim.upstreamAuthority`. The CRD supports exactly two plugins:

```yaml
config:
  ztwim:
    upstreamAuthority:
      certManager:
        issuerName: 'autoshift-ca'
        issuerKind: 'ClusterIssuer'
        namespace: 'cert-manager'
```

`autoshift-ca` is the built-in cert-manager issuer from the
[`cert-manager`](../cert-manager/README.md) policy, so this needs `cert-manager: 'true'` and
`cert-manager-ca: 'true'` on the same cluster.

> **This alone does not give you a shared fleet root.** `autoshift-ca` is self-signed **per
> cluster**, so each cluster gets its own root and chaining to it only adds a layer. For one root
> across the fleet, also point `config.certManager.ca.issuer` at a signer common to every cluster,
> which makes `autoshift-ca` an intermediate rather than a root.

The `vault` plugin is the other option, taking a Vault address, PKI mount point and Kubernetes auth
role.

### Federating with another trust domain

Federation joins **independent** trust domains: each keeps its own CA and they exchange trust
bundles over a federation endpoint.

```yaml
hubClusterSets:
  hub:
    labels:
      ztwim: 'true'
      ztwim-federation: 'true'
    config:
      ztwim:
        federatedTrustDomains:
          - trustDomain: 'other.example.com'
            bundleEndpointURL: 'https://spire-federation.apps.other.example.com'
            bundleEndpointProfile:
              type: 'https_spiffe'
              endpointSPIFFEID: 'spiffe://other.example.com/spire/server'
```

Federation is not transitive, and it does not propagate along an upstream authority chain. Every
cluster that must authenticate a foreign domain needs its own entry.

## Known limits

**Nested SPIRE is not available.** The `SpireServer` CRD exposes only the `certManager` and `vault`
upstream authority plugins. It has no `aws_pca` plugin and no `spire` plugin, so a topology where a
hub SPIRE server signs a spoke SPIRE server cannot be expressed through the operator API. Reaching
it means editing the `server.conf` JSON that the operator generates and pinning the operator into
`CREATE_ONLY_MODE`, which stops it reconciling **every** operand on that cluster. That work is
scoped separately and is deliberately not part of this policy.

**The datastore defaults to SQLite.** That is a single replica backed by one volume, which is
appropriate for most clusters but is not highly available. The CRD also accepts `postgres` and
`mysql`. Note that `connectionString` is a literal string with no secret reference, so a connection
string carrying a password must not be committed to a values file. Use client certificate
authentication through `datastore.tlsSecretName` instead.

## Supportability

Red Hat publishes capability annotations on the operator bundle. These are the values on
`zero-trust-workload-identity-manager.v1.1.1`, verified against a live cluster.

| Capability | Declared | What it means here |
|---|---|---|
| `fips-compliant` | **true** | Supported on a FIPS-enabled cluster. |
| `proxy-aware` | true | Honours the cluster egress proxy. |
| `csi` | true | Ships the SPIFFE CSI driver. |
| `disconnected` | **false** | Not declared air-gap capable. See below. |
| `tls-profiles` | **false** | Does **not** honour the cluster `TLSSecurityProfile`. |

### FIPS

The operator is built with the Red Hat FIPS Go toolchain
(`GOEXPERIMENT=strictfipsruntime`, `-tags=strictfipsruntime,openssl`), so crypto runs through the
RHEL OpenSSL FIPS module rather than the Go standard library, and the container base is RHEL 9.

The CRD also constrains key selection to FIPS-approved algorithms. Both `caKeyType` and
`jwtKeyType` accept only `rsa-2048`, `rsa-4096`, `ec-p256` and `ec-p384`. Upstream SPIRE also
offers `ed25519`, which is not FIPS-approved and is deliberately absent, so no value of
`config.ztwim` can select a non-approved algorithm. The `autoshift-ca` issuer uses ECDSA P-384, so
chaining SPIRE under it stays consistent.

### Disconnected

The `disconnected: false` annotation means Red Hat has not declared or tested air-gapped support.
It does **not** indicate missing image metadata: every image this operator runs is digest-pinned
under `registry.redhat.io`, and the full set is discoverable for mirroring (seven CSV
`relatedImages` entries plus the operator's own image from the CSV deployment spec). An audit of a
running install found all eight declared images in use and no undeclared image.

This policy still applies the standard AutoShift disconnected source rule: with
`autoshift.io/disconnected-mirror: 'true'` the catalog source becomes
`<source>-<mirror-catalog-suffix>`. Mirroring works mechanically, but treat a disconnected
deployment as unsupported by Red Hat until that annotation changes.

### TLS profiles

Because `tls-profiles` is false, the cluster-wide `TLSSecurityProfile` does not propagate to SPIRE
endpoints. A hardened deployment cannot centrally enforce cipher suites on them, so do not assume
cluster TLS policy covers this component.

## Verify

```bash
oc get zerotrustworkloadidentitymanager,spireserver,spireagent cluster
oc get pods -n zero-trust-workload-identity-manager

# Confirm how the CA is rooted
oc logs spire-server-0 -c spire-server -n zero-trust-workload-identity-manager \
  | grep "X509 CA activated"
# self_signed=true          -> no upstreamAuthority configured
# self_signed=false + id    -> chained under cert-manager or Vault

# Registered agents, one per node
oc exec spire-server-0 -c spire-server -n zero-trust-workload-identity-manager \
  -- /spire-server agent list
```

To hand an identity to a workload, mount the CSI volume:

```yaml
volumes:
  - name: spiffe-workload-api
    csi:
      driver: csi.spiffe.io
      readOnly: true
```

## Validate changes

```bash
cd tools && go test -tags integration -count=1 ./internal/resolver/...
```
