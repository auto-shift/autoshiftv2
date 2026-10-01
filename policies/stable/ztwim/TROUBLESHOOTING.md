# Troubleshooting ztwim

Field notes for a SPIRE deployment that is not behaving. Start with the inform policies, because
several failure modes in this component report success.

## Start here: the inform policies

An unsatisfied guard renders no object templates, and a ConfigurationPolicy with no object templates
reports Compliant. An enforcing policy reading green is therefore not evidence that it did anything.
These checks assert the effects instead, and each violation is named after what failed.

| Policy | Violating object | Meaning |
|---|---|---|
| `policy-ztwim-ready` | SPIRE server readiness | Installed but never became ready |
| `policy-ztwim-operand-drift` | `ztwim-stale-operand-<workload>` | Operator upgraded, that workload still runs the previous images |
| `policy-ztwim-datastore-effective` | `ztwim-server-conf-still-<type>` | Configuration asked for PostgreSQL, the running server is on something else |
| `policy-ztwim-datastore-effective` | `ztwim-cnpg-cluster-missing` | The CloudNativePG cluster was never created |
| `policy-ztwim-nested-hub-effective` | hub objects | Hub side produced nothing |
| `policy-ztwim-nested-spoke-effective` | spoke objects | Spoke side produced nothing |
| `policy-ztwim-nested-spoke-chained` | bundle comparison | Spoke still serves its own root rather than the hub's |

## Symptoms

### A workload gets no identity, and there is no error

The Workload API socket is simply absent from that node. Check that the SPIFFE CSI driver runs
there:

```bash
oc get pods -n zero-trust-workload-identity-manager -l app.kubernetes.io/name=spiffe-csi-driver -o wide
```

The driver is a DaemonSet that tolerates every taint by default. If it is missing from a node,
either that node is at pod capacity or `config.ztwim.csiDriver.tolerations` was narrowed.

### A pod hangs in ContainerCreating forever

If it mounts a `csi.spiffe.io` volume and the CSI driver is not on its node, the volume never mounts
and the pod waits indefinitely. This affects the SPIRE OpenID Connect Discovery Provider too, which
mounts the same volume. Same check as above.

### A policy is Compliant but nothing happened

Expected when a guard is unsatisfied. Consult the inform policies in the table above, which is the
reason they exist.

### An operand change does not take effect

On a nested cluster `CREATE_ONLY_MODE` is on, and the Operator creates absent objects but never
updates existing ones. Editing the custom resource leaves the workload at its current generation
indefinitely. Delete the workload so the Operator rebuilds it from the resource:

```bash
oc delete daemonset spire-agent -n zero-trust-workload-identity-manager
```

Setting `autoshift.io/ztwim-operand-reconcile` to `'true'` automates this for image drift. On a
nested hub, recreating the SPIRE server StatefulSet costs two restarts, because the per-spoke
kubeconfig volumes are reapplied afterwards by `psat-clusters.yaml`.

### A spoke never attests to its hub

Ask the hub which agents reached it:

```bash
oc exec spire-server-0 -n zero-trust-workload-identity-manager -c spire-server -- \
  /spire-server agent list -socketPath /tmp/spire-server/private/api.sock | grep k8s_psat
```

If the spoke is absent, the hub could not run a TokenReview against it. Confirm the kubeconfig path
in the generated configuration matches where the Secret is mounted:

```bash
oc get cm spire-server -n zero-trust-workload-identity-manager \
  -o jsonpath='{.data.server\.conf}' | python3 -m json.tool | grep kube_config_file
oc exec spire-server-0 -n zero-trust-workload-identity-manager -c spire-server -- \
  ls -l /run/spire/spoke-kubeconfigs/
```

Each spoke is one file named after the cluster. A path ending in `/kubeconfig` is stale.

### The chain will not form

The spoke holds a root of its own. SPIRE keeps the authority it minted on first boot until the
datastore is cleared, so a correct configuration with the wrong bundle is a stable state. Compare
the two bundles:

```bash
oc get cm spire-bundle -n zero-trust-workload-identity-manager -o jsonpath='{.data.bundle\.crt}' | sha256sum
```

Run it against both clusters. If they differ, clear the spoke datastore as described in the policy
README, or set `autoshift.io/ztwim-nested-auto-reset` to `'true'`. Clear the datastore only once the
upstream agent is Ready, otherwise the server self-signs again on restart.

### The resource says PostgreSQL, the server runs SQLite

`CREATE_ONLY_MODE` again: the resource changed and the generated configuration did not. Confirm what
the process actually loaded, which is what `policy-ztwim-datastore-effective` checks:

```bash
oc get cm spire-server -n zero-trust-workload-identity-manager \
  -o jsonpath='{.data.server\.conf}' | python3 -c \
  "import json,sys; print(json.load(sys.stdin)['plugins']['DataStore'][0]['sql']['plugin_data']['database_type'])"
```

Delete the `spire-server` ConfigMap and StatefulSet so both are rebuilt. With an external datastore
this is not destructive.

### The SPIRE server will not schedule

Two causes, and the second is easy to miss. The server tolerates only the infrastructure taint by
default, so a cluster whose nodes carry others needs `config.ztwim.tolerations`. More awkwardly, the
datastore volume is ReadWriteOnce and pinned to one availability zone, so the pod cannot move
outside that zone. If no node there will take it, the server cannot start anywhere. An external
PostgreSQL datastore avoids this, because the volume then holds only key material.

### A policy reports a missing Route or other kind

```
couldn't find mapping resource with kind Route in API version route.openshift.io/v1
```

This is an API discovery problem in the policy controller rather than a configuration fault. It
clears on its own. Investigate only if it persists.

## What an outage costs

Credentials are cached, so the SPIRE server going down is not immediately visible. How long that
lasts depends entirely on which credential a consumer holds.

| Credential | Default lifetime | Renewal begins | Survives an outage of |
|---|---|---|---|
| JWT-SVID | 5m | about 2m30s | about 5 minutes |
| X.509-SVID | 1h | about 30m | about 1 hour |
| Server certificate authority | 24h | about 12h | up to 24 hours |

JSON Web Token identities expire fastest, so anything validating them through the OpenID Connect
Discovery Provider fails within minutes. Certificate identities used for mutual TLS last an hour.
On a nested spoke the intermediate authority lasts a day, so a hub outage degrades downstream
clusters slowly.

Three things fail immediately regardless of lifetime: new nodes cannot attest, new registration
entries do not propagate, and an identity that was never issued cannot be issued.

## Commands worth knowing

The binaries are at the filesystem root, not under `/opt`.

```bash
# registration entries and attested agents
oc exec spire-server-0 -n zero-trust-workload-identity-manager -c spire-server -- \
  /spire-server entry show -socketPath /tmp/spire-server/private/api.sock
oc exec spire-server-0 -n zero-trust-workload-identity-manager -c spire-server -- \
  /spire-server agent list -socketPath /tmp/spire-server/private/api.sock

# fetch an identity from inside a workload that mounts the CSI volume
oc exec -n <namespace> <pod> -- \
  /spire-agent api fetch x509 -socketPath /spiffe-workload-api/spire-agent.sock

# compare running images against what the Operator declares
oc get deploy zero-trust-workload-identity-manager-controller-manager \
  -n zero-trust-workload-identity-manager \
  -o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' | grep RELATED_IMAGE
```

## Things that look wrong and are not

`oc get subscription` resolves to the Red Hat Advanced Cluster Management resource, not the Operator
Lifecycle Manager one, and returns nothing. Use the full name:

```bash
oc get subscriptions.operators.coreos.com -A | grep zero-trust
```

Operand resources report `Ready=True` with `All components are ready` whenever the workloads exist,
regardless of whether they match the resource. That status tracks readiness rather than conformance,
which is why `policy-ztwim-operand-drift` compares images instead.
