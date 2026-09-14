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
| `policy-ztwim-nested-create-only` | `CREATE_ONLY_MODE` on the operator | `ztwim-nested-role` in `hub`,`spoke` |
| `policy-ztwim-nested-hub` | gRPC Route, per-spoke kubeconfig, `k8s_psat` patch, downstream entries | `ztwim-nested-role: 'hub'` |
| `policy-ztwim-nested-spoke` | trust bundle, upstream agent, upstream CSI, upstream authority | `ztwim-nested-role: 'spoke'` |
| `policy-ztwim-nested-hub-effective` | inform only: hub-side policies actually produced something | `ztwim-nested-role: 'hub'` |
| `policy-ztwim-nested-spoke-effective` | inform only: spoke-side policies actually produced something | `ztwim-nested-role: 'spoke'` |
| `policy-ztwim-nested-spoke-chained` | inform only: the CA chain actually formed | `ztwim-nested-role: 'spoke'` |
| `policy-ztwim-operand-drift` | inform only: operands still on the previous release's images | same as install |
| `policy-ztwim-operand-reconcile` | deletes a stale operand so the operator recreates it | `autoshift.io/ztwim-operand-reconcile: 'true'` |

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
Every key is optional for a standalone cluster. Nested SPIRE is the exception: it requires
`trustDomain` and `nested.hubEndpoint`, neither of which has a useful default.

| Key | Default | Notes |
|---|---|---|
| `trustDomain` | cluster base domain | The SPIFFE identity namespace. See the warning below. |
| `clusterName` | ManagedCluster name | Stamped into every agent SPIFFE ID by the node attestor. |
| `jwtIssuer` | `https://spire-oidc.<ingress domain>` | Must resolve under the `*.apps` wildcard. |
| `caSubject` | `autoshift` / `US` / `AutoShift` | Subject of the SPIRE CA certificate. |
| `persistence` | `5Gi`, `ReadWriteOnce` | Datastore volume. `storageClass` is omitted unless set. |
| `datastore` | `sqlite3` | See the database note below. |
| `upstreamAuthority` | omitted | Chains the SPIRE CA under an external authority. |
| `federatedTrustDomains` | empty | One `ClusterFederatedTrustDomain` per foreign domain. Requires the `ztwim-federation` label. |
| `federation` | omitted | `SpireServer.spec.federation`: this server's own bundle endpoint and peers. Distinct from the row above. |
| `bundleConfigMap` | `spire-bundle` | ConfigMap the server publishes its trust bundle into. |
| `subscriptionName` | operator package name | Override only for a mirrored or renamed package. |
| `caKeyType` | `rsa-2048` | CRD restricts this to FIPS-approved algorithms. |
| `caValidity` / `defaultX509Validity` / `defaultJWTValidity` | `24h` / `1h` / `5m` | CA and SVID lifetimes. |
| `agent` | inherits `logLevel` | `logLevel`, `workloadAttestorVerification`. |
| `csiDriver` | `csi.spiffe.io` | `pluginName`, `agentSocketPath`. |
| `oidcDiscoveryProvider` | 1 replica, managed Route | `logLevel`, `replicaCount`, `managedRoute`. |
| `nested.hubEndpoint` | derived from the hub's Route | Override only for an external load balancer in front of the hub. |

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

**Nested SPIRE works, but it is expensive.** See the section below before enabling it. The cost is
real: the `SpireServer` CRD exposes only the `certManager` and `vault` upstream authority plugins,
so the `spire` plugin that nesting needs is reached by patching the `server.conf` the operator
generates, held in place by `CREATE_ONLY_MODE`. That flag is read from a single environment
variable by **every** ZTWIM controller, so once it is on, no operand change reconciles on that
cluster until the resource is deleted and recreated.

**The SPIRE server cannot run in high availability.** The `SpireServer` CRD carries no `replicas`
field at v1.1.1, so one `spire-server` pod is the only configuration available, and there is no
field for a `PodDisruptionBudget` either. Red Hat's own documentation for the `SpireServer` custom
resource, from OpenShift Container Platform 4.19 through 4.22, lists no such field and shows
`spire-server 1/1` with a single `spire-server-0` pod as the expected result. The operator CSV
declares `capabilities: Basic Install`, the lowest level. This is a limit of the operator, not of
AutoShift: no setting in this policy changes it. When the pod is down, agents keep serving cached SVIDs until
`config.ztwim.defaultX509Validity` expires, one hour by default, after which the trust domain stops
issuing. Plan maintenance inside that window.

**The persistence fields are effectively immutable.** Red Hat documents `persistence.size`,
`accessMode` and `storageClass` as unchangeable once set, and the underlying StatefulSet volume
claim template cannot change either. Changing `config.ztwim.persistence` after the first deployment
is accepted by the API and then does nothing. On a nested cluster `CREATE_ONLY_MODE` hides it a
second time. Resizing means deleting the `SpireServer` resource, which discards the CA unless the
volume is preserved by hand. Choose the size at install time.

**The datastore defaults to SQLite.** That is a single replica backed by one volume, which is
appropriate for most clusters but is not highly available. The CRD also accepts `postgres` and
`mysql`. Note that `connectionString` is a literal string with no secret reference, so a connection
string carrying a password must not be committed to a values file. Use client certificate
authentication through `datastore.tlsSecretName` instead.

## Scheduling and resources

Every operand accepts `resources`, `tolerations` and `nodeSelector`. Top-level `config.ztwim` keys
apply to the SPIRE server; each operand sub-map (`agent`, `csiDriver`, `oidcDiscoveryProvider`)
overrides for itself.

Two defaults are deliberate and worth knowing about.

**The agent and CSI driver DaemonSets tolerate every taint.** A workload can be scheduled onto any
node that admits workloads, and if the agent and the CSI driver are not on that node the workload
cannot obtain an SVID. There is no error in that case: the Workload API socket is simply absent.
Because AutoShift's own `infra-nodes` and `storage-nodes` policies taint most nodes, anything
narrower leaves SPIRE covering a fraction of the cluster. This is the same posture the container
network interface and CSI DaemonSets take. To narrow it, set an explicit list; setting an explicit
empty list clears the default rather than restoring it.

**Requests are set, limits are not.** Requests are what lift the pods out of `BestEffort`, where
the kubelet evicts the trust domain's CA ahead of almost anything else. A CPU limit on a signing
service only buys throttling. Add limits through `config.ztwim.resources` if a quota requires them.

## Upgrades

On an ordinary cluster an upgrade is a channel or version change and needs nothing from this
section. On a **nested** cluster it does, because `CREATE_ONLY_MODE` changes what an upgrade means.

**Nested clusters pin themselves.** `upgradeApproval` is derived: any cluster with
`ztwim-nested-role` set resolves to `None`, so no InstallPlan is approved and the operator stays
where it is. Every other cluster keeps `Automatic`. Override with
`autoshift.io/ztwim-upgrade-approval`.

The reason is that in create-only mode the operator creates absent objects but never updates
existing ones. After an operator upgrade the SPIRE workloads keep running the previous release's
images indefinitely, and nothing reports it: the CSV is `Succeeded`, and the operand CRs still
report `Ready=True` with `All components are ready`. That status tracks readiness, not conformance.
AutoShift also owns hand-patched content inside the generated `server.conf`, written against a
particular release's shape.

**`policy-ztwim-operand-drift` is the check.** It compares each running container against the
`RELATED_IMAGE_*` environment on the installed operator, which is the source of truth for what each
operand should run. A violation names the stale workload directly, for example
`ztwim-stale-operand-spire-agent`.

**Applying an operand change under create-only means deleting the workload.** That is the only
lever: the create path still runs. Clearing `CREATE_ONLY_MODE` instead does not work on a
GitOps-managed cluster, because `policy-ztwim-nested-create-only` restores it within seconds and
Argo CD reverts an attempt to inform that policy. A window would also hand `server.conf` back to the
operator while the enforcing policies re-apply their patches, with no defined end to the fight.

Set `autoshift.io/ztwim-operand-reconcile: 'true'` to have AutoShift delete drifted operands so the
operator recreates them, or delete them by hand:

```bash
oc delete daemonset spire-agent -n zero-trust-workload-identity-manager
```

On a nested **hub**, recreating the `spire-server` StatefulSet costs two restarts: the recreated
StatefulSet lacks the per-spoke kubeconfig volumes that `psat-clusters.yaml` owns, and that policy
re-applies them on its next evaluation. SPIRE is down across both. This is why the reconcile is
opt-in.

### Upgrade sequence for a nested cluster

1. Confirm `policy-ztwim-operand-drift` is Compliant, so you start from a known state.
2. Move `autoshift.io/ztwim-version` and `ztwim-channel` to the target release in the values file.
3. Set `autoshift.io/ztwim-upgrade-approval: 'Automatic'` for the upgrade, and let the operator roll.
4. `policy-ztwim-operand-drift` goes NonCompliant, naming each stale operand. Expected.
5. Set `autoshift.io/ztwim-operand-reconcile: 'true'`. The operands are recreated at the new images.
6. Confirm `policy-ztwim-nested-spoke-chained` returns to Compliant, then set both labels back.

Step 6 matters: a `server.conf` shape change between releases breaks the nested patches, and the
chained check is what catches it.

## Nested SPIRE

A hub SPIRE server signs the spoke's intermediate CA, so both clusters chain to one root. Enable it
with `autoshift.io/ztwim-nested-role` set to `hub` on the signing cluster and `spoke` on each signed
cluster, plus the two required config keys:

```yaml
config:
  ztwim:
    trustDomain: 'spire.example.com'
```

`trustDomain` must be the **same literal on both ends**. The spoke's upstream-authority plugin
derives the expected upstream server ID from its own trust domain, so a mismatch fails the TLS
handshake with `unexpected ID`. It is deliberately not derived: it is a fleet-wide choice, and
changing it later is destructive.

The hub endpoint needs no configuration. The spoke-side policy runs hub templates, which resolve
against the hub, so it reads the `spire-server-grpc` Route directly off the hub cluster and follows
it if the host ever changes. Set `nested.hubEndpoint` only to override that, for an external load
balancer in front of the hub. Both the upstream agent and the upstream-authority plugin use it, and
the plugin dials it on port 443.

No external secret store is involved. The spoke's TokenReview credential comes from ACM's
`ManagedServiceAccount` add-on, and the hub assembles the kubeconfig from state it already holds.
The hub trust bundle is public CA material, delivered by a hub template.

### How many spokes a hub carries

Every spoke costs the hub one `ManagedServiceAccount`, one key in the `spoke-kubeconfigs` Secret,
and two `ClusterStaticEntry` resources. The pod specification does not grow: all spoke kubeconfigs
share one Secret, mounted once at `/run/spire/spoke-kubeconfigs`, where each key appears as a file.

The limit is therefore the Secret, not the pod. Kubernetes caps a Secret at 1048576 bytes of
decoded data, so:

```
spokes per hub  =  1048576 / bytes per kubeconfig
```

**Do not treat that as a fixed number.** A kubeconfig is dominated by the spoke's API server CA
bundle, which varies widely between environments. On a default Red Hat OpenShift cluster the
measured split is 15356 bytes of certificate authority data against 1335 bytes of token and 351
bytes of everything else, giving 17042 bytes and around 60 spokes. Append a corporate certificate
chain and the same hub carries closer to 35. Replace the per-cluster bundles with one shared root
referenced by path instead of inlined, and it rises into the hundreds.

Measure it for a real fleet rather than assuming:

```bash
oc get secret spoke-kubeconfigs -n zero-trust-workload-identity-manager \
  -o jsonpath='{.data}' | wc -c        # divide 1048576 by the per-spoke share
```

Two further limits arrive before any hardware does:

* The SPIRE server reads `server.conf` only at startup, so onboarding a spoke restarts it. The pod
  template carries the ConfigMap resource version to make that restart happen exactly when the
  configuration changes, and not on a token rotation. During the restart no spoke can renew its
  intermediate CA.
* The datastore defaults to SQLite, a single writer, and the server cannot run more than one replica.

For a fleet larger than one hub can carry, shard into regional hubs rather than widening a single
one. Nesting is a tree and nothing requires it to be two levels deep.

Set `autoshift.io/ztwim-nested-role` to `both` on the middle tier. A regional hub is then a nested
spoke of the hub-of-hubs and a nested hub to its own spokes, squaring whatever one hub carries.

Both manifest sets are placed by the **hub-of-hubs** instance, because a regional hub is not managed
by its own Red Hat Advanced Cluster Management and cannot place policies on itself. One label has to
select both, which is what `both` is for.

The templates need no change for this, and the reason is worth understanding before altering them:

* The **spoke-side** manifests use hub templates. Those resolve against whichever cluster placed the
  policy, so on a regional hub they read the hub-of-hubs and it correctly dials the root.
* The **hub-side** manifests enumerate clusters with spoke templates, evaluated locally on the
  regional hub, where its own Red Hat Advanced Cluster Management can see its spokes. The
  hub-of-hubs cannot see those spokes at all, so a hub template would find nothing and, because an
  empty render is compliant, would report success while doing nothing.

### The one manual step

SPIRE caches its CA in the datastore. A spoke that has already minted a self-signed root keeps it
even after the upstream authority is configured, so the volume must be cleared once, by an
operator, on the spoke:

```bash
oc scale sts spire-server -n zero-trust-workload-identity-manager --replicas=0
oc delete pvc spire-data-spire-server-0 -n zero-trust-workload-identity-manager
oc scale sts spire-server -n zero-trust-workload-identity-manager --replicas=1
```

Do this only after all three nested policies report Compliant. No policy attempts it.

### Why there are three inform checks

An enforcing policy that renders **no** object templates reports **Compliant** — ACM's own words are
*"contains no object templates to check, and thus has no violations"*. The nested manifests guard
their output on inputs being present, so a missing `trustDomain` or an unresolved image makes a
policy render nothing and go green while doing nothing at all.

The three inform policies exist because an enforcing policy's own status is therefore not evidence
that it did anything:

| Check | Catches |
|---|---|
| `nested-hub-effective` | hub-side policies rendered nothing |
| `nested-spoke-effective` | spoke-side policies rendered nothing |
| `nested-spoke-chained` | everything rendered and the chain still did not form |

The third is the important one. A trust domain that differs between hub and spoke, an upstream
authority patch that did not apply, or a datastore PVC that was never cleared all produce a spoke
that looks entirely healthy and is not chained to anything.

They detect; they do not gate. None is a dependency of an enforcing policy, because the effect they
assert cannot exist until the enforcer has run.

> **Hub-of-hubs caveat.** `nested-spoke-chained` compares the spoke against the hub that
> **propagates the policy**. In a stacked topology where the nested hub is not the propagating hub,
> its hub template resolves against the wrong cluster and the result is misleading. The trust bundle
> delivery has the same constraint, so this is not new, but do not rely on the check there.

### Confirming the chain

The spoke's trust bundle becomes byte-identical to the hub's root once nesting takes effect:

```bash
# on the hub
oc get cm spire-bundle -n zero-trust-workload-identity-manager -o jsonpath='{.data.bundle\.crt}' | sha256sum
# on the spoke -- same digest means the spoke trusts the hub root
```

On the hub, the spoke's agent appears in `spire-server agent list` as
`spiffe://<trust-domain>/spire/agent/k8s_psat/<spoke>/<uid>`, and
`spire-server entry show -downstream` lists the spoke's server entry with `Downstream: true`.

## Supportability

Red Hat publishes capability annotations on the operator bundle. These are the values on
`zero-trust-workload-identity-manager.v1.1.1`.

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
under `registry.redhat.io`, and the full set is discoverable for mirroring: seven CSV
`relatedImages` entries plus the operator's own image from the CSV deployment specification, which
together cover every image the operator runs.

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
