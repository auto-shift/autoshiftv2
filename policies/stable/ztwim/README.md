# ztwim

Deploys the Red Hat **Zero Trust Workload Identity Manager** operator and its SPIRE control plane,
giving every workload on the cluster a short-lived, cryptographically verifiable SPIFFE identity
(an SVID) instead of a long-lived secret.

The operator is the productized build of upstream SPIRE, the reference implementation of the SPIFFE
standard. AutoShift installs the operator and configures its operands; SPIRE itself then handles
attestation, issuance and rotation.

For a deployment that is misbehaving, see [TROUBLESHOOTING.md](TROUBLESHOOTING.md). Several
failure modes in this component report success, so start with the inform policies listed there.

## What it deploys

| Policy | Creates | Placement |
|---|---|---|
| `policy-ztwim-operator-install` | Operator subscription and namespace | `autoshift.io/ztwim: 'true'` |
| `policy-ztwim-instance` | `ZeroTrustWorkloadIdentityManager` (trust domain, cluster name) | same |
| `policy-ztwim-config` | `SpireServer`, `SpireAgent`, `SpiffeCSIDriver`, `SpireOIDCDiscoveryProvider` | same |
| `policy-ztwim-federation` | `ClusterFederatedTrustDomain` per foreign trust domain | `autoshift.io/ztwim-federation: 'true'` |
| `policy-ztwim-federation-views` | `ManagedClusterView` per mesh member, publishing its bundle and endpoint | `autoshift.io/cluster-type: 'hub'` |
| `policy-ztwim-federation-mesh` | `ClusterFederatedTrustDomain` per peer, bundle included | `autoshift.io/ztwim-federation-mesh` set |
| `policy-ztwim-ready` | inform only: SPIRE server readiness | same as install |
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

**High availability is a SPIRE feature that this Operator does not expose.** Upstream SPIRE
documents it as a first-class mode: configure every server in the trust domain against one shared
datastore, and each server maintains its own certificate authority, either self-signed or an
intermediate beneath a shared root. No shared key manager is needed, so the `disk` key manager this
Operator offers is sufficient. Scaling the StatefulSet by hand does produce a working pair, with
each server signing under its own authority, both authorities in the shared trust bundle, and agents
accepting an identity issued by either.

Do not rely on it. Red Hat lists high availability for the SPIRE server and the OpenID Connect
Discovery Provider as an unsupported configuration, which is why the `SpireServer` custom resource
has no `replicas` field and no `PodDisruptionBudget` field at v1.1.1. The Operator also restores the
replica count on any cluster not held in `CREATE_ONLY_MODE`. The gap is in the productization rather
than in SPIRE, so exposing a replica count is a reasonable request to make of Red Hat. Red Hat's own documentation for the `SpireServer` custom
resource, from OpenShift Container Platform 4.19 through 4.22, lists no such field and shows
`spire-server 1/1` with a single `spire-server-0` pod as the expected result. The operator CSV
declares `capabilities: Basic Install`, the lowest level. This is a limit of the operator, not of
AutoShift: no setting in this policy changes it. When the pod is down, agents keep serving cached SVIDs until
`config.ztwim.defaultX509Validity` expires, one hour by default, after which the trust domain stops
issuing. Plan maintenance inside that window.

**The persistence fields are immutable, and the API enforces it.** The custom resource definition
carries validation rules rejecting any change to `persistence.size`, `accessMode` or
`storageClass`, so editing `config.ztwim.persistence` after the first deployment makes the policy
fail rather than drift. Resizing means deleting the `SpireServer` resource, which discards the
certificate authority unless the volume is preserved by hand, so choose the size at install time.

**Federation cannot be turned off once enabled.** A validation rule states that federation
configuration cannot be removed once set, so even editing the resource directly will not clear it.
Removing `config.ztwim.federation` from values does not attempt the removal at all, it simply stops
AutoShift managing the field while federation carries on. Treat enabling federation as a one-way
decision.

**The SPIRE server is a singleton.** A validation rule requires `metadata.name` to be `cluster`, so
a second `SpireServer` resource is rejected at admission. Running more than one SPIRE server is
therefore not reachable by creating extra resources either. All five operand resources are
singletons in the same way.

### Fields the API refuses to change

Every rule below is enforced by the custom resource definitions, so **changing** one of these
values after the first deployment is rejected and the policy reports NonCompliant. Undoing one means
deleting the resource, which discards the certificate authority.

**Removing** a key behaves differently, and quietly. These manifests use `complianceType: musthave`,
which merges: when a key disappears from values the template stops emitting the field, and nothing
removes it from the resource. The setting stays in effect and the policy stays Compliant. Deleting
`config.ztwim.federation` does not turn federation off, it only stops AutoShift managing it. To
actually remove a setting, edit the resource directly, and expect the validation rules above to
refuse if the field is one of the immutable ones.

| Setting | Rule |
|---|---|
| `config.ztwim.trustDomain` | immutable |
| `config.ztwim.clusterName` | immutable |
| `config.ztwim.bundleConfigMap` | immutable |
| `config.ztwim.persistence.size`, `.accessMode`, `.storageClass` | immutable |
| `config.ztwim.federation` | cannot be removed once set |
| `config.ztwim.federation.bundleEndpoint.profile` | immutable once set |
| `config.ztwim.federation.bundleEndpoint.httpsWeb` | cannot switch between `acme` and `servingCert` |

Three further rules reject a resource outright rather than on change: `upstreamAuthority` accepts
exactly one of `certManager` or `vault`; a `federatesWith` entry that sets the `https_spiffe` profile
requires `endpointSpiffeId`; and `agent.workloadAttestorVerification: hostCert` requires both
`hostCertBasePath` and `hostCertFileName`.

**PostgreSQL is the supported production datastore.** Red Hat added it in 1.0.0 for production
persistence, so `config.ztwim.datastore.databaseType: postgres` is on supported ground, unlike
multiple replicas. Note the connection string is a plain field with no secret reference, so the
password is readable on the resource; client certificate authentication would avoid that but the
operator mounts `tlsSecretName` world-readable at mode 0644, which the PostgreSQL driver rejects.

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
GitOps-managed cluster, because `policy-ztwim-operator-install` restores it within seconds and
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

### Federating a mesh instead of nesting

Nesting puts every cluster in one trust domain beneath a single certificate authority, and each tier
adds an intermediate that every mutual TLS handshake verifies. Federation keeps each cluster's own
authority and exchanges trust bundles instead, so chains stay one certificate deep everywhere and
none of the create-only machinery is needed.

The step that usually makes federation awkward is knowing who the peers are and where their bundle
endpoints live, which is normally a manual exchange between cluster owners. Setting
`autoshift.io/ztwim-federation-mesh` to the same value on a set of clusters has AutoShift do it: the
cluster running Red Hat Advanced Cluster Management publishes each member's bundle, trust domain and
federation endpoint as `ManagedClusterView` resources, and each member reads its peers' views and
writes a `ClusterFederatedTrustDomain` for each peer domain. SPIRE then fetches the bundle from the
endpoint and keeps it current on its own.

AutoShift does not seed `trustDomainBundle`. The field is optional and takes a SPIFFE bundle in JWKS
form rather than the PEM that SPIRE's own `spire-bundle` ConfigMap holds, and nothing in a policy
template can convert between them. Setting it to PEM is worse than leaving it off: the API server
accepts it, the resource keeps an empty status, the policy reports Compliant, and the controller
manager silently drops the relationship with `Ignoring invalid ClusterFederatedTrustDomain` in its
log. Omitting it is also the right shape for `https_web`, whose whole point is that no trust has to
exist in advance.

Members federate only within their group, so the number of relationships follows the group rather
than the fleet. Federation is not transitive, which makes that the right shape: a mesh is exactly
the set of clusters that must authenticate one another.

A member holds one `ClusterFederatedTrustDomain` per peer **trust domain**, not per peer cluster,
named for the domain. Clusters and domains are not one to one: a nested hub and its spokes are a
single domain across several clusters, so a mesh containing such a pair would otherwise produce two
resources naming the same domain and differing only in whose endpoint they point at. The first peer
to report a domain supplies the endpoint for it. Peers sharing the member's own domain are left out
altogether, which is why a nested pair inside a mesh federates outward and not with itself.

Which peer supplies the endpoint follows the cluster list order, and AutoShift cannot tell a serving
endpoint from a dead one: the endpoint is a Route, and the Route exists whether or not the server
behind it has a federation block. Any healthy endpoint in a domain serves the same bundle, so the
choice only matters when one of them is not serving. Enable federation on every cluster in a shared
trust domain rather than on one of them.

Every member needs `config.ztwim.federation.bundleEndpoint` set, which is what makes the Operator
publish the endpoint peers fetch from. Note that enabling federation on a cluster already running
in create-only mode for nesting does not take effect until its operands are recreated, because the
Operator cannot rewrite the generated configuration while that mode is on.

**Each member's ingress wildcard certificate has to be one its peers trust.** AutoShift publishes
the bundle endpoint on a Route under `*.apps.<baseDomain>`, and the `https_web` profile
authenticates that endpoint with ordinary web public key infrastructure. A stock Red Hat OpenShift
cluster signs its ingress wildcard with a per-cluster self-signed authority, which a peer rejects.
Set `config.certManager.ingressCert` with a real issuer on every member, or run the mesh on
clusters whose ingress already carries a publicly issued wildcard. The backend certificate is the
service-serving certificate and is not part of this: the Route re-encrypts, so only what the router
presents has to be trusted.

**No policy can tell you the bundle actually transferred.** `ClusterFederatedTrustDomain` carries no
status, so a relationship that SPIRE accepted but cannot fetch looks identical to a working one from
Kubernetes. The inform policy checks that the resource exists, which is as far as it can see. Verify
the transfer in the server log:

```bash
oc logs -n zero-trust-workload-identity-manager spire-server-0 -c spire-server | grep bundle_client
# "Trust domain is now managed"  -> the relationship was accepted
# "Error updating bundle ... certificate signed by unknown authority"  -> the peer's ingress
#                                                                        certificate is not trusted

oc logs -n zero-trust-workload-identity-manager spire-server-0 -c spire-controller-manager \
  | grep "Ignoring invalid"
# any output here means a trust domain was dropped before SPIRE ever saw it
```

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

**The largest lever is `config.ztwim.nested.apiCaSecret`.** Certificate authority data is about
15KB of the 17KB a kubeconfig costs. Naming a Secret on the hub that holds the authority signing the
fleet's API server certificates lets every kubeconfig reference that one mounted file by path
instead of carrying a copy, which drops the per-spoke cost to a measured 1710 bytes and takes a hub
from about sixty spokes to roughly 600.

It requires the fleet to share that authority. Stock Red Hat OpenShift does not: each cluster signs
its own API certificate with a per-cluster signer. Replacing the API serving certificate from one
common issuer makes it true, which the `cert-manager` policy in this repository can do.

**That shared signer is a shared liability**, and the trade is worth making deliberately. The hub
validates every spoke's `k8s_psat` token against this one authority, so whoever holds it can present
a certificate for any cluster's API endpoint and pass `TokenReview` as that cluster. With
per-cluster signers the same compromise reaches exactly one spoke. The blast radius moves from one
cluster to the fleet, in exchange for the ceiling moving from 60 spokes to 600.

Two things make the trade smaller than it first sounds. The option exists only under nesting, where
the fleet has already accepted a single authority for workload identity, so it adds a second shared
authority rather than the first. And an API serving certificate is a narrower grant than the SPIRE
root: it lets an attacker impersonate an API server, not mint workload identities directly.

Two operational costs come with it. The hub reads one mounted file, so a wrong or stale copy fails
every spoke at once rather than one. And rotating that authority becomes a coordinated fleet-wide
event, where per-cluster signers rotate independently.

Worth knowing even without it: of the seven certificates in a default bundle, only the one that
signed the API endpoint takes part in verification. The rest cover localhost, the service network,
recovery and ingress, and travel in every kubeconfig for nothing.

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
