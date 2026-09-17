# gitops-dev (policy)

Per-team Argo CD instances, and optionally a per-team **Argo CD agent**.

User-facing documentation lives in [docs/gitops-agent.md](../../../docs/gitops-agent.md). This file
covers the policy structure and the reasons behind it.

## What it builds

Each team named under `config.gitops.teams.<team>` gets an Argo CD instance in
`openshift-gitops-<team>`. The `gitops-dev-team-<team>` label decides what that means on each
cluster:

| Label value | What runs here |
|---|---|
| `hub` | The team instance, on a hub |
| `standalone` | The team instance, on a spoke that reports to nothing |
| `agent`, `agent-autonomous`, `agent-managed` | The team's agent, reporting to that team's principal on the hub |

## Files

One policy per concern, each with its own placement, because PolicyGenerator derives the
`PlacementBinding` name from the policy and two policies sharing a placement file collide with
`already registered id`.

| Path | Policy | Where it lands |
|---|---|---|
| `manifests/` | `policy-gitops-dev` | The team instance |
| `agent-hub/` | `policy-gitops-dev-agent-hub` | Hub: principal, CA, JWT key, per-cluster client certs, mapping secrets, scoped `AppProject` |
| `agent-spoke/` | `policy-gitops-dev-agent-spoke` | Spoke: namespace, credentials, agent instance, narrow `ClusterRole` |
| `test/agent-hub-ready.yaml` | `policy-gitops-dev-agent-hub-ready` | Inform, hub |
| `test/agent-spoke-ready.yaml` | `policy-gitops-dev-agent-spoke-ready` | Inform, spoke |

The split is only for readability. `agent-hub` was one 486-line manifest and the enforcing and
inform halves were bundled, which a root `remediationAction` would have overridden.

## Things that will bite you

**Gate on the label, not on `$isHub`.** That variable means "carries a `self-managed` label at
all", which is **true for a managed hub** as well as a self-managed one. A spoke promoted to a
managed hub silently stopped rendering the spoke policy, and nothing reported it: `musthave` never
deletes, so the running agent survived on objects created while it was still a spoke.

**Match the label with `hasPrefix "agent"`, never `eq "agent"`.** The label carries the mode, so
`agent-managed` is not `agent`. An exact match enrolled nobody and dropped that cluster's client
certificate, mapping secret, namespace, and `sourceNamespaces` entry.

**`sourceNamespaces` is enumerated, never `'*'`.** A wildcard makes the Argo CD operator claim
*every* namespace on the cluster, and a namespace carries only one
`argocd.argoproj.io/managed-by-cluster-argocd` claim. Two wildcard principals contest all of them
and the loser is denied its own agent namespaces with nothing logged above debug.

**`sourceNamespaces` on the hub uses AGENT names, not cluster names.** Red Hat is explicit that the
namespace names must match the agent names. Get it wrong and the principal can neither deploy nor
monitor, in either mode, without reporting anything.

**The `AppProject` is `mustonlyhave`.** Argo CD ships a `default` project wildcarded to every
destination. `musthave` merges lists and never removes a field, so a scoped destination is appended
to the wildcard and the restriction is decorative.

**Use `lookup`, not `fromSecret`, for anything this policy creates.** `fromSecret` errors on a
missing secret and fails the whole policy, including the `Certificate` that would have created it.
That is a deadlock on first install. `lookup` returns empty, so guard on the result.

**Quote every templated scalar.** A bare `*` is a YAML alias indicator and breaks the entire
`object-templates-raw` parse, taking down every object in the policy rather than one.

**Rendering nothing is a legitimate result, so do not manufacture a violation for it.** A hub with
the policy placed but no enrolled cluster has nothing to create, and an empty `ConfigurationPolicy`
reports Compliant with no detail. Both agent policies therefore set `customMessage.compliant` in
`policy-generator-config.yaml` to say what a Compliant verdict covered. Do not use
`{{ .DefaultMessage }}` in it: the validation suite resolves the message and has no such field.

## Testing

`agent.enabled` is `false` in `_example.yaml`, so the validation suite never resolves the agent
blocks. Exercise them against a live hub, or temporarily enable them in a values override. The
labels `gitops-agent-enrolled` and `gitops-dev-team-<team>` must be set **together**: the former
places the spoke policy, the latter decides which teams, and setting one without the other builds
the entire hub side while no agent ever appears.
