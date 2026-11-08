# connectivity-link

Installs Red Hat Connectivity Link and creates the Kuadrant stack it manages: Authorino for
authentication and authorization, and Limitador for rate limiting. Models-as-a-Service in Red Hat
OpenShift AI enforces API keys through Authorino and quotas through Limitador, so a MaaS gateway
never programs without this stack.

## Policies

| Policy | Mode | What it does |
|---|---|---|
| `policy-rhcl-operator-install` | enforce | The `rhcl-operator` subscription, in `openshift-operators` |
| `policy-rhcl-kuadrant` | enforce | The `kuadrant-system` namespace, the `Kuadrant` and the cluster-wide `Authorino`. Waits for the Operator |
| `policy-rhcl-ready` | inform | `Kuadrant` reports `Ready` |
| `policy-rhcl-authorino-cert` | enforce | Annotates the Authorino Service so service-ca issues its serving certificate. Waits for `policy-rhcl-ready` |
| `policy-rhcl-authorino-cert-ready` | inform | The `authorino-server-cert` Secret exists |

`rhcl` abbreviates the policy names to stay within the 40-character limit.

## Enabling

```yaml
hubClusterSets:
  hub:
    labels:
      connectivity-link: 'true'
      connectivity-link-subscription-name: rhcl-operator
      connectivity-link-channel: stable
      connectivity-link-source: redhat-operators
      connectivity-link-source-namespace: openshift-marketplace
      # connectivity-link-version: 'rhcl-operator.v1.x.y'   # optional CSV pin
```

Set labels in values files only. The `cluster-labels` policy stamps them onto each cluster.

## Packaging: 1.5 and 1.4

The channel decides which packaging you get. The `rhcl-operator` package's default channel,
`stable`, still installs 1.4. Set `connectivity-link-channel: stable-1.5` for 1.5.

- **1.5:** the bundle declares no OLM dependencies. Its one Deployment,
  `kuadrant-operator-controller-manager`, deploys the Authorino, Limitador, DNS and MCP Gateway
  Operators itself through a `KuadrantControlPlane` custom resource, applies their CRDs, and takes
  their images from the bundle's related images.
- **1.4:** OLM installs the Authorino, Limitador and DNS Operators as separate dependencies of
  `rhcl-operator`, each with its own CSV.

The policies are the same for both versions, and install none of those Operators by name.

Do not manage or delete the `KuadrantControlPlane` named `default`. The Operator creates it, and
deleting it cascade-deletes Authorino, Limitador, DNS and the MCP Gateway.

## The Kuadrant stack

The subscription installs the Operator into `openshift-operators`. The components that enforce
anything run from `kuadrant-system`, and exist only once the `Kuadrant` and `Authorino` custom
resources do.

- `Kuadrant` enables observability, so the Operator creates the Limitador and Authorino monitors
  that user workload monitoring scrapes. MaaS usage metrics depend on them.
- `Authorino` is cluster-wide, because MaaS authorizes requests for every model namespace through
  one instance.
- `policy-rhcl-ready` matters because MaaS waits for `Kuadrant` to be `Ready`. Without this check
  the failure surfaces much later, as a MaaS Gateway that never programs, with nothing pointing back
  here.

## Authorino TLS, and the deadlock it avoids

MaaS talks to Authorino over TLS, with a serving certificate from OpenShift's service-ca operator.
The pieces are circular:

1. Authorino will not start with TLS enabled until its certificate Secret exists.
2. service-ca issues the certificate when the Authorino Service is annotated.
3. The Authorino Operator creates that Service only once Authorino starts.

Requesting TLS in the initial `Authorino` therefore guarantees it never starts, and leaves no
Service to annotate. `Kuadrant` then reports:

```
Ready=False AuthorinoNotReady: listener secret name authorino-server-cert not found
```

This policy drives `spec.listener.tls.enabled` from a managed-cluster lookup of the Secret, so the
sequence runs itself: Authorino starts with TLS off, its Service appears,
`policy-rhcl-authorino-cert` annotates it, service-ca issues the Secret, and the next evaluation
turns TLS on. Three details make this the only arrangement that converges:

- `spec.listener.tls` is a required field, so it cannot be left to the Operator's default.
- `musthave` merges, so removing the field from a manifest would not remove it from a cluster that
  already has it. Only driving the value converges.
- Only one enforcing policy can own the field. Two policies writing it would fight on every
  evaluation.

The certificate comes from service-ca, not from cert-manager, as in the Red Hat procedure for MaaS:
the Envoy-to-Authorino connection is trusted through the cluster's internal CA bundle.

## Operator memory, set on the Subscription

On Connectivity Link 1.5 the `kuadrant-operator-controller-manager` is OOMKilled at its shipped
memory limit (CONNLINK-1878). The operator-install policy sets `spec.config.resources` on the
Subscription: requests `200m` CPU and `512Mi` memory, limits `200m` CPU and `2Gi` memory.

- **Not the Deployment:** OLM regenerates the operator Deployment from the CSV, so a patch applied
  to the Deployment directly is reset on the next reconcile or upgrade. The Subscription field is
  the one OLM applies and keeps.
- **CPU is restated:** `config.resources` replaces the container's whole resources block, so the
  CPU values are the shipped defaults, included to keep them.
- **Harmless on 1.4,** where the shipped limit is enough.

Check that OLM applied it:

```bash
oc get deploy kuadrant-operator-controller-manager -n openshift-operators \
  -o jsonpath='{.spec.template.spec.containers[0].resources}{"\n"}'
```

## MCP Gateway

Connectivity Link 1.5 includes the MCP Gateway Operator. The Kuadrant Operator deploys it through
`KuadrantControlPlane`, with the other components, so no separate Operator is installed for it. The
MCP catalog in Red Hat OpenShift AI needs it. On 1.4 it is not included.

## Verifying

```bash
oc get kuadrant kuadrant -n kuadrant-system -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}{"\n"}'
oc get authorino authorino -n kuadrant-system -o jsonpath='{.spec.listener.tls.enabled}{"\n"}'
oc get secret authorino-server-cert -n kuadrant-system
oc get pods -n kuadrant-system
```
