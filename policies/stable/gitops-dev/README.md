# gitops-dev (policy)

Self-contained per-team Argo CD instances.

## What it builds

Each team named under `config.gitops.teams.<team>` gets an Argo CD instance in
`openshift-gitops-<team>`, with its RBAC, console link and cluster registration. The
`gitops-dev-team-<team>` label decides where that instance lands:

| Label value | What this chart builds |
|---|---|
| `hub` | The team instance on a hub, managing spokes by push |
| `standalone` | The team instance on a spoke, reporting to nothing |
| `agent`, `agent-autonomous`, `agent-managed` | Nothing. The agent modes are built by [gitops-agent](../gitops-agent/README.md) |

infra takes the `standalone` branch but never the `hub` branch: its hub instance is `infra-gitops`,
owned by `policy-gitops-systems-argocd`, and a second one here would fight it.

For the agent modes, and for the mode catalog as a whole, see
[docs/gitops-agent.md](../../../docs/gitops-agent.md).

## Files

| Path | Policy | What it builds |
|---|---|---|
| `manifests/` | `policy-gitops-dev` | The team's Argo CD instance, RBAC, console link and cluster registration |

The agent policies used to live here and now sit in
[gitops-agent](../gitops-agent/README.md). They moved because they serve infra as well as teams, and
naming them after `gitops-dev` made infra's principal read as a dev-team object.

## Things that will bite you

**The preamble is MIRRORED** between this chart and `gitops-agent`. It derives the team-to-mode map
that both depend on, including the synthetic `infra` entry. Change it in one place and you must
change it in the other, or the two charts disagree about which teams exist.

## Testing

The validation suite resolves this chart against every example profile, so a rendering break shows
up in `cd tools && go test -tags integration ./...`. What it cannot see is which branch a live
cluster takes, because that depends on the `cluster-type` label the `cluster-labels` policy stamps
at runtime. Check both `hub` and `standalone` against a real hub and a real spoke.
