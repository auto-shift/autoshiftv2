# Workload identity topology

AutoShift can give a fleet SPIFFE workload identity in two shapes. **Nesting** puts every cluster in
one trust domain under a single certificate authority. **Federation** gives every cluster its own
trust domain and exchanges trust bundles between them. This page explains which to choose, and
records the decisions behind how AutoShift implements them.

Both are delivered by the `ztwim` policy, which installs the Red Hat Zero Trust Workload Identity
Manager and its operands. See the
[policy README](../policies/stable/ztwim/README.md) for labels and configuration.

## Choosing between them

Start here. The two shapes answer different questions, and only one of them is about scale.

| | Nested | Federated |
|---|---|---|
| Trust domains | One, fleet wide | One per cluster |
| Certificate authority | Hub signs an intermediate for each spoke | Each cluster keeps its own |
| How a workload sees another cluster's workloads | Same trust domain | Foreign domain it explicitly trusts |
| Certificate chain depth | Grows with each tier | Always one authority deep |
| Adding a cluster | Requires hub coordination | Requires only that peers learn its bundle |
| Blast radius of a compromised authority | The whole fleet | One cluster |
| Requires `CREATE_ONLY_MODE` | Yes | No |
| Can also federate | **No** | n/a |

### Choose nesting when

- Workloads must present identities from a **single trust domain** across the fleet, because a
  consuming system authorizes on the trust domain rather than on an explicit federation list.
- You want one authority to be the root of trust for everything, and you accept that its compromise
  is fleet wide.
- The fleet has a clear hierarchy that already matches hub and spoke.

### Choose federation when

- Clusters belong to **different owners, tenants or security boundaries**, and a shared authority
  would be a shared liability.
- You want to add and remove clusters without touching a central signing authority.
- Any member runs **disconnected**, or the fleet uses **its own certificate authority**. Federation
  with the `https_spiffe` profile depends on no certificate authority at all, which nesting's
  machinery does not change but which matters for how bundles are exchanged.
- You want chains to stay one certificate deep regardless of fleet size.

### They are mutually exclusive on a cluster

This is not a style preference, it is a consequence of how the Operator works. Nesting requires `CREATE_ONLY_MODE`, which tells the Operator to create objects it does not
find and never to update the ones it does. AutoShift needs that so its hand written additions to the
generated `server.conf` survive.

The federation configuration is written into that same generated file by the Operator. Under
create only mode the Operator never rewrites it, so a nested cluster publishes a bundle endpoint
Route with nothing listening behind it and peers receive `503`. The cluster looks configured and is
not.

Set `autoshift.io/ztwim-nested-role` for nesting, or leave it unset for federation. A cluster with a
nested role is not a federation member, whatever else is configured.

## Architecture decisions

Each decision below is recorded with what forced it, so a later change can tell whether the
constraint still holds.

### Federation uses the `https_spiffe` profile

**Decision.** `config.ztwim.federation.bundleEndpoint.profile` defaults to `https_spiffe`, which is
also the Operator's own default. `https_web` remains available and is the better choice for a fleet
whose members already carry publicly issued ingress certificates, because it needs no seeded bundle.

**Why.** The two profiles differ in how a peer authenticates a bundle endpoint.

`https_web` validates the endpoint's TLS certificate against the SPIRE server's truststore. That
store is whatever the container image ships and **cannot be extended**. The `SpireServer` resource
exposes no volumes and no environment, and the Operator reverts edits to the generated StatefulSet
within about fifteen seconds. Adding a certificate authority to the cluster through
`Proxy.spec.trustedCA` does not reach it either, because nothing mounts the injected bundle into
that pod. So `https_web` works only where every member's ingress wildcard chains to an authority
already inside the image, which excludes any fleet that uses its own authority, and every
disconnected site.

`https_spiffe` authenticates the endpoint by the SPIFFE SVID it presents, validated against the
peer's trust bundle. No certificate authority participates.

**Cost.** The profile is immutable once the resource exists, so changing it recreates the
`SpireServer`. The trust domain and its certificate authority survive, because they live in the
datastore rather than in the resource.

### The bundle endpoint Route terminates according to the profile

**Decision.** `passthrough` under `https_spiffe`, `reencrypt` under `https_web`.

**Why.** `https_spiffe` requires the peer to complete TLS with the SPIRE server itself, so it can
see the server's SVID. A `reencrypt` Route terminates at the router and presents the router's own
certificate, so the peer would validate the wrong identity. In practice it fails earlier: the router
cannot validate the SVID the backend serves and returns `503`.

`https_web` wants the opposite, because the certificate the peer validates is the router's wildcard.

The Operator's own federation Route makes the same switch, which is a useful cross check.

### The first bundle is seeded, and it must be JWKS

**Decision.** The mesh policy writes `trustDomainBundle` on each
`ClusterFederatedTrustDomain`, taking the value from a bundle published in JWKS form.

**Why.** `https_spiffe` cannot bootstrap: validating a peer's SVID needs that peer's bundle, which
is what the relationship exists to fetch. `trustDomainBundle` carries the first bundle and breaks
the circle. SPIRE refreshes from the endpoint on its own afterwards.

The field accepts a SPIFFE bundle in JWKS only. The sole bundle the Operator publishes as a
Kubernetes object is the `spire-bundle` ConfigMap, which is PEM, and no policy template can convert
one to the other: a JWKS entry for an RSA authority needs the modulus and exponent as separate
base64url fields. Building an entry from the certificate alone does not work either. Measured on a
live cluster, the controller manager rejects it with `go-jose/go-jose: invalid RSA key, missing n/e
values`.

**The failure mode is silent**, which is why this is worth stating. A rejected bundle leaves the
resource with an empty status and the policy reporting Compliant, while the relationship is dropped.
Only the controller manager log shows `Ignoring invalid ClusterFederatedTrustDomain`.

### Bundles are fetched by External Secrets Operator, not a Job

**Decision.** A `SecretStore` using the webhook provider fetches each cluster's own bundle endpoint,
and an `ExternalSecret` materialises it. AutoShift copies the result into a ConfigMap.

**Why a fetch at all.** Given the JWKS constraint above, the bundle has to come from the endpoint,
which already serves exactly the right document.

**Why External Secrets.** It is declarative, refreshes on an interval, and reports conditions that a
policy can assert on. A Job would need its own scheduling, image and rotation handling.

**Why it needs no trust.** The fetch targets the Service rather than the Route, so it never leaves
the cluster, and the endpoint's certificate is validated with the OpenShift service certificate
authority that every cluster already has. Nothing depends on the ingress wildcard, on a public
authority, or on outbound connectivity, which is what makes it work disconnected.

**Why a ConfigMap and not a Secret.** `ManagedClusterView` refuses Secrets outright, and the hub
needs to read this to reach peers. The bundle is public key material in any case, with no private
components, and is already served unauthenticated to anyone who asks. A Secret would add no
protection while blocking the one thing the bundle must do.

**Two prerequisites, both owned by the External Secrets policy.** The controller only runs when an
`ExternalSecretsConfig` exists, and the Operator applies a deny all egress policy to its operands
that opens only `6443` and DNS. A store on any other port fails with
`InvalidProviderConfig ... i/o timeout`, which names neither TLS nor network policy and reads like a
certificate problem. Egress for `8443` is configured in that resource.

### The datastore is SQLite by default and CloudNativePG by choice

**Decision.** `config.ztwim.datastore.databaseType` defaults to `sqlite3`. Setting it to `postgres`,
with `autoshift.io/cloudnative-pg: 'true'`, backs SPIRE with a CloudNativePG cluster instead.

**Why it is usually not about scale.** The spoke ceiling is set by the size of the aggregated
kubeconfig Secret, not by the datastore, so a database does not raise it. See
[A nested hub carries spokes in one Secret](#a-nested-hub-carries-spokes-in-one-secret).

What the datastore does carry is every agent and registration entry behind those spokes, which is
not the same count: each spoke contributes an agent per node, so a hub at the flattened ceiling of
roughly 600 spokes holds tens of thousands of agents and their entries. SQLite is unremarkable at
the default ceiling of 60. At 600 it has **not** been measured, and a single file on one
ReadWriteOnce volume is a poor shape for that load whatever a benchmark would say. Size the
datastore deliberately before planning a hub in the hundreds. Two things motivate a database at any
scale.

**Backup.** The datastore holds the trust domain's root certificate authority and every registration
entry. With SQLite on a volume claim there is no backup path at all: losing the volume invalidates
every identity in the domain, and because the persistence settings are immutable an administrator
cannot even resize the volume without destroying the authority. CloudNativePG brings scheduled
backup and point in time recovery for the one piece of state that cannot be reconstructed.

**High availability.** SPIRE scales horizontally by running several servers against **one shared**
datastore. The servers are not meant to share signing keys: each mints its own authority, and the
bundle in the datastore is the union of them, so an agent validates an SVID issued by any of them. A
per replica SQLite file is exactly what makes more than one replica impossible.

**The limit today.** The `SpireServer` resource has no replicas field, so the Operator creates one
server whatever the datastore. A database is the prerequisite for high availability, not the
delivery of it. The CloudNativePG cluster defaults to two instances so the database tier survives a
node loss even though SPIRE does not.

**Switching datastore destroys the trust domain.** A different datastore is an empty datastore, so
SPIRE mints a new authority and the old root is gone. Choose before the first deployment.

**The switch is guarded on the database actually existing.** The CloudNativePG cluster and the
`SpireServer` are separate policies with no ordering between them, and adding a dependency would
stall every cluster that does not run CloudNativePG. So the policy uses postgres only once the
generated password Secret is present, and stays on SQLite until then. The password is read on the
managed cluster rather than the hub, so it never passes through a values file or Git. It does land
in the `SpireServer` resource, because the connection string is a plain field with no secret
reference.

### SPIRE is scheduled as platform infrastructure

**Decision.** Tolerations default to the infra taint. Resource requests are set; limits are not.

**Why tolerations.** SPIRE is platform infrastructure and belongs on an infra node, and AutoShift's
own `infra-nodes` policy taints those nodes. An empty default would leave the server unschedulable
on exactly the clusters this ships to.

That matters more than it first appears. A SQLite datastore is a ReadWriteOnce volume pinned to one
availability zone, so if no node in that zone will accept the pod, the authority cannot start
anywhere, and the only way out is destroying the volume and the trust domain root with it. Storage
taints are deliberately **not** tolerated; add them through `config.ztwim.tolerations` if that is
where SPIRE belongs.

**Why requests without limits.** Requests keep the pod out of BestEffort, where the kubelet evicts
the trust domain's root authority ahead of almost anything else. A CPU limit on a signing service
buys throttling and little else. Add limits through `config.ztwim.resources` if a quota requires
them.

### Neither topology delivers a highly available SPIRE server

**Decision.** One SPIRE server per cluster, whichever topology is chosen. Plan for it rather than
around it.

**Why.** The `SpireServer` resource has no replicas field, so the Operator creates exactly one
server. A shared datastore is the prerequisite for running more, and SPIRE supports it — several
servers against one database, each minting its own authority, with the datastore bundle holding the
union so an agent validates an SVID from any of them — but the resource offers no way to ask for it.

Scaling the generated StatefulSet by hand is not a workaround, and it behaves differently by
topology:

- Under nesting, `CREATE_ONLY_MODE` stops the Operator reverting the change, so a hand scaled
  StatefulSet survives. Treat it as unsupported: nothing reconciles it, and an Operator upgrade may
  reclaim it.
- Under federation there is no create only mode, so the Operator reconciles the StatefulSet back.
  Measured: an edit to the generated StatefulSet was reverted within fifteen seconds.

**What each topology does give you.** Federation isolates faults: every cluster holds its own
authority, so one cluster's SPIRE being down affects only that cluster's workloads. Nesting
concentrates them: the hub is a single point of failure for issuing and renewing spoke
intermediates, and its loss eventually stops the whole fleet rather than one cluster.

The database tier is highly available in both: the CloudNativePG cluster defaults to two instances.
That protects the state, not the service.

### A nested hub carries spokes in one Secret

**Decision.** Every spoke kubeconfig lives in a single Secret keyed by cluster name, mounted once.

**Why.** A Secret per spoke would need a volume and a mount per spoke on the one server pod, growing
the pod specification without bound and leaving the kubelet holding a watch per spoke on a single
pod. Keyed into one Secret, each key surfaces as a file under one mount and the pod specification
stays constant.

The ceiling therefore moves to the Secret size limit, 1048576 bytes of decoded data, divided by the
size of one kubeconfig. That divisor is **not** a constant and should not be treated as one: a
kubeconfig is dominated by the spoke's API server certificate authority data, around 15KB against
1.3KB of token on a default cluster, giving 17042 bytes and roughly 60 spokes. Append a corporate
certificate chain and the same hub carries far fewer.

`config.ztwim.nested.apiCaSecret` removes most of it. Naming one Secret that holds the authority
signing the fleet's API certificates lets every kubeconfig reference a mounted file by path instead
of carrying its own copy, dropping the per spoke cost to a measured 1710 bytes, which is roughly
600 spokes per hub.

It is opt in because it requires the fleet to share that authority, which stock OpenShift does not,
and because **a shared signer is a shared liability**. The hub validates every spoke's token against
that one authority, so whoever holds it can present a certificate for any cluster's API endpoint and
pass `TokenReview` as that cluster; per-cluster signers contain the same compromise to one spoke.
The blast radius moves from one cluster to the fleet in exchange for the ceiling moving from 60
spokes to 600.

The trade is smaller than it first sounds, because the option exists only under nesting, where the
fleet has already accepted one authority for workload identity, and because an API serving
certificate is a narrower grant than the SPIRE root. It is still a trade, and it is the same
argument that pushes multi-owner fleets towards federation in the first place.

Beyond whatever a single hub carries, shard: a middle tier with
`autoshift.io/ztwim-nested-role: 'both'` squares the reachable fleet.

### Immutable fields are recreated rather than reported

**Decision.** The `SpireServer` object template sets `recreateOption: IfRequired`.

**Why.** The bundle endpoint profile, the federation block and every persistence setting are
immutable. An update touching one is rejected and the policy reports `cannot be updated, likely due
to immutable fields not matching`, which is a dead end that a person has to clear by hand.

Recreating is safe for the identity that matters. The trust domain's certificate authority and all
registration entries live in the datastore, not in the resource: a postgres datastore is an external
database and a sqlite one is a retained volume claim. Agents keep their SVIDs and do not
re-attest.

## Security properties of the seeded bundle

Seeding changes the trust model, and the change is worth stating rather than leaving implicit.

**The bundle itself is not a secret.** It holds public key material only: modulus, exponent and
certificate chain, with no private components. It is already served unauthenticated to any peer that
asks, which is what a bundle endpoint is for. Carrying it in a ConfigMap rather than a Secret adds
no exposure.

**Seeding introduces a trust injection point that `https_web` does not have.** Under `https_web` a
server fetches each peer's bundle itself and validates it with web public key infrastructure, so
there is nothing on the path to tamper with. Under `https_spiffe` the first bundle is written into
`trustDomainBundle` from a ConfigMap on the member, and **whoever can write that ConfigMap decides
which authority that member's peers will trust for its trust domain**. Poisoning it means peers
accept SVIDs minted by the attacker as belonging to that domain.

**Two reconcilers narrow that considerably.** External Secrets rewrites the Secret from the live
endpoint every five minutes, and the policy rewrites the ConfigMap from that Secret on every
evaluation with `musthave` and enforce. Both therefore converge on what the cluster's own SPIRE
server actually serves, so an edit to either object is corrected rather than persisting. Poisoning
the seed is not a write, it is a write that has to be sustained against two controllers.

**What remains.** A poisoned value can still reach peers inside one reconcile window, and once a
bundle has been applied to a peer's datastore it is not obvious that correcting the resource removes
it again. Writing that ConfigMap is also a **lower privilege** than control of the SPIRE signing
key, so the path lowers the bar even though it does not open one that was closed. Treat write access
to that namespace as equivalent to control of that cluster's workload identity, and keep it scoped
accordingly.

**What does not widen the surface.** The fetch runs against the in-cluster Service and is validated
with the OpenShift service certificate authority, so it never leaves the cluster and adds no
external dependency. `ManagedClusterView` is read only, and the hub could already read those
ConfigMaps through its managed cluster connection. The egress rule that lets External Secrets reach
the endpoint is scoped to the SPIRE namespace rather than opening the port fleet wide.

**One exposure that does change.** Under `https_spiffe` the endpoint Route is `passthrough`, so the
SPIRE server terminates TLS itself rather than the router doing it. The server is directly reachable
from wherever that ingress is reachable. The bundle it serves is public by design, but this is a
larger surface than `reencrypt`, where the router absorbs the connection. Set
`config.ztwim.federation.publishEndpointRoute` to `false` and
`config.ztwim.federation.bundleEndpointHost` to a private address where clusters share a network.

**Net against `https_web`.** `https_spiffe` authenticates a peer by its SVID rather than by trusting
whichever public authorities happen to be in the container image, which is the stronger position.
The seeding requirement is the price of that, and the mitigation is ordinary Kubernetes access
control on one namespace.

## What a federated member needs

- `autoshift.io/ztwim: 'true'` and no `autoshift.io/ztwim-nested-role`
- `autoshift.io/ztwim-federation-mesh` set to the same value on every member of the mesh
- `config.ztwim.federation.bundleEndpoint` configured, which is what makes the Operator publish an
  endpoint for peers to fetch from
- The External Secrets controller running, with egress allowed on `8443`

Members federate only within their mesh group, so the number of relationships follows the group
rather than the fleet. Federation is not transitive, which makes that the right shape: a mesh is
exactly the set of clusters that must authenticate one another.

## Verifying it works

No policy can tell you a bundle actually transferred. `ClusterFederatedTrustDomain` carries no
status, so a relationship SPIRE accepted but cannot fetch looks identical to a working one. Check
the server log:

```bash
oc logs -n zero-trust-workload-identity-manager spire-server-0 -c spire-server | grep bundle_client
# "Trust domain is now managed"  the relationship was accepted
# "Bundle refreshed"             the fetch succeeded
# "Error updating bundle"        it did not

oc logs -n zero-trust-workload-identity-manager spire-server-0 -c spire-controller-manager \
  | grep "Ignoring invalid"
# any output means a trust domain was dropped before SPIRE ever saw it
```
