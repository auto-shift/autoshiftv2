# gitops-agent (policy)

The Argo CD agent, for every team **and** for infra.

User-facing documentation lives in [docs/gitops-agent.md](../../../docs/gitops-agent.md). This file
covers the policy structure and the reasons behind it.

## Who drives it

One set of policies serves two callers, which differ only in where the mode and the config come
from:

| | Mode label | Config |
|---|---|---|
| A team | `gitops-dev-team-<team>` | `config.gitops.teams.<team>` |
| infra | `gitops-infra` | `config.gitops.infra` |

infra is handled as a team named `infra`, with one structural difference: on a hub its Argo CD
already exists. `policy-gitops-systems-argocd` places `infra-gitops` in the gitops namespace, and it
is placed on hub clustersets only. So infra's agent ATTACHES there, and that chart carries the agent
stanza, because a namespace holds one Argo CD and the Applications worth mirroring are the ones that
instance already owns.

On a spoke there is no such instance, so infra builds one like a team does, in
`openshift-gitops-<team>`. A spoke derives `push`, so this happens only where someone sets
`gitops-infra` to an agent mode deliberately. The case that pays is a MANAGED HUB, which runs its
own AutoShift and owns real Applications; a plain spoke is configured by push from its hub and has
nothing of its own to mirror.

The principal namespace is `agentNamespace`, default `openshift-gitops-<team>-agent`, and is this
chart's own on both sides.

The instance namespace is per cluster and the two halves resolve it independently, which is correct:
a hub template sees the TARGET cluster's labels, so the hub half reads the hub's gitops namespace
and the spoke half reads the spoke's. They are different clusters' namespaces and are meant to
differ. What must match across the halves is the agent name and the principal namespace, and both
derive from cluster name plus team, so they agree by construction.

The Red Hat Advanced Cluster Management add-on is the alternative, not part of this chart: see
`policy-gitops-addon` in [openshift-gitops](../openshift-gitops/README.md). The two are mutually
exclusive, which is why their config sits side by side under `config.gitops.infra`.

## Files

One policy per concern, each with its own placement file. Two policies pointing at one placement
fail with `placementBindingDefaults.name must be set but is empty`, and setting that name then
collides as soon as a second placement is shared. One placement per policy keeps the generated
`PlacementBinding` names unique without that knob.

| Manifest | Policy | Action | What it covers |
|---|---|---|---|
| `agent-hub/` | `policy-gitops-agent-hub` | enforce | Principal, certificate authority, token signing key, per-cluster client certificates, mapping secrets, scoped `AppProject` |
| `test/agent-hub-ready.yaml` | `policy-gitops-agent-hub-ready` | inform | The hub half is serving |
| `agent-spoke/` | `policy-gitops-agent-spoke` | enforce | Agent namespace, credentials, agent instance, narrow `ClusterRole` |
| `test/agent-spoke-ready.yaml` | `policy-gitops-agent-spoke-ready` | inform | The agent is connected |
| `agent-hub-remove/` | `policy-gitops-agent-hub-remove` | enforce | Hub teardown |
| `agent-hub-remove/` | `policy-gitops-agent-hub-removed` | inform | Hub teardown finished |
| `agent-spoke-remove/` | `policy-gitops-agent-spoke-remove` | enforce | Spoke teardown |
| `agent-spoke-remove/` | `policy-gitops-agent-spoke-removed` | inform | Spoke teardown finished |

Each teardown policy shares its manifest with an inform twin, deliberately. `mustnothave` under
enforce reports Compliant when the delete call returns rather than when the object is gone, so the
enforcing copy cannot say whether teardown finished and the inform copy is what answers that. The
two lists have to be identical for the barrier to mean anything, and one file is the only way to
guarantee it.

Teardown fires only on an `uninstall` mode value, never on a removed label. Dropping the label
leaves everything in place, because `musthave` never deletes.

## Naming

Everything this chart creates is named `gitops-agent-*`. Every secret name the principal and the
agent read is a settable field on the `ArgoCD` CR (`principal.tls.secretName`,
`principal.tls.rootCASecretName`, `principal.jwt.secretName`,
`principal.resourceProxy.secretName`, `principal.resourceProxy.caSecretName`, `agent.tls.*`), so
the names are ours and the upstream `argocd-agent-*` defaults are only defaults.

Four names are NOT ours and must stay as they are:

| Name | Who fixes it |
|---|---|
| `argocd.argoproj.io/secret-type: cluster` | Argo CD, on the mapping secret |
| `argocd-agent.argoproj-labs.io/agent-name` | argocd-agent, on the mapping secret |
| `managed-by: argocd-agent` | argocd-agent, the marker on secrets it manages |
| `argocd-agent-ca` in the add-on path | The `GitOpsCluster` controller, which is why cert-manager's `gitops-agent-ca` policy still emits that name |

Rename an object and its references in the same pass. Every reference here is a plain string field,
so a half-done rename leaves the principal pointing at a secret nothing creates, the agent with no
trust anchor, and both reporting Compliant.

## Things that will bite you

**The preamble is MIRRORED** with `gitops-dev`. It builds the team-to-mode map both charts depend
on, including the synthetic `infra` entry. Change it in one place and you must change it in the
other.

**Build the mode map before looping WHERE A DERIVED MODE CAN SATISFY THE GATE.** Iterating
`.ManagedClusterLabels` only sees clusters that carry the label, so it misses infra wherever infra's
mode is derived rather than set. The map always carries `infra`, deriving `hub` on a hub and `push`
elsewhere.

That matters for a gate a derived value can pass. `agent-hub.yaml` gates on `eq $mode "hub"` and
`gitops-dev.yaml` on `hub` or `standalone`, so both need the map. It does NOT matter for a gate only
an explicit value can pass: the readiness and teardown manifests gate on `hasPrefix "agent"` or
`eq "uninstall"`, and no derived mode is ever either of those, so iterating labels there is
equivalent and is not a bug to fix.

**The enrollment label key differs for infra.** Reading `gitops-dev-team-infra` finds no clusters,
leaving a principal with no mapping, certificate or namespace. Use the `$enrollKey` ternary.

**Match the mode with `hasPrefix "agent"`, never `eq "agent"`.** The label carries the mode, so the
value may be `agent`, `agent-managed` or `agent-autonomous`.

**`sourceNamespaces` is enumerated, never `'*'`.** A wildcard makes the Argo CD operator claim every
namespace on the cluster, and a namespace carries only one
`argocd.argoproj.io/managed-by-cluster-argocd` claim, so two wildcard principals take each other's
agent namespaces.

**On the hub, `sourceNamespaces` holds AGENT names, not cluster names.** Red Hat is explicit that
the namespace names must match the agent names.

**The `AppProject` is `mustonlyhave`.** Argo CD ships a `default` project wildcarded to every
destination, and `musthave` merges lists, so a scoped destination would be appended to the wildcard
and the restriction would be decorative.

**`fromConfigMap`, not `lookup`, for this cluster's config.** A missing rendered-config means the
values are not ready, and `lookup` would let every default fall through and build objects with the
wrong names, which `musthave` then never removes. The teardown policies are the exception and say so
in place: teardown has to work when the config is already gone.

**Quote every templated scalar.** A bare `*` is a YAML alias indicator and breaks the whole
`object-templates-raw` parse, taking down every object in the policy.

## Testing

`agent.enabled` is `false` in `_example.yaml`, so the validation suite never resolves the agent
blocks. Exercise them against a live hub, or enable them in a values override. For a team, the
labels `gitops-agent-enrolled` and `gitops-dev-team-<team>` must be set **together**: the former
places the spoke policy, the latter decides which teams.
