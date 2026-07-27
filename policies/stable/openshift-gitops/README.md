# openshift-gitops (policy)

**This policy is a plain Helm chart, not a PolicyGenerator policy — on purpose.**

Every other policy under `policies/` is rendered by PolicyGenerator: a `policy-generator-config.yaml`
is discovered by the ArgoCD **PolicyGenerator ConfigManagementPlugin (CMP)** sidecar, which runs
`kustomize` + PolicyGenerator to wrap plain manifests into ACM `Policy` objects. `openshift-gitops`
**cannot** work that way, because it is the chart that *creates that machinery in the first place*.

## Why not PolicyGenerator

`openshift-gitops` bootstraps the two things PolicyGenerator depends on:

1. **OpenShift GitOps (ArgoCD) itself** — the engine that runs every AutoShift ArgoCD Application,
   including the ApplicationSet that deploys all the other `policies/*`.
2. **The PolicyGenerator CMP** — `templates/policy-generator-cmp.yaml` deploys the
   `ConfigManagementPlugin` named `policy-generator` (plus its sidecar wiring on the ArgoCD CR via
   `policy-gitops-systems-argocd.yaml`). Until this exists, no `policy-generator-config.yaml` anywhere
   in the repo can be rendered.

That is a chicken-and-egg: PolicyGenerator can't render the chart whose job is to install
PolicyGenerator. So this chart emits its `Policy` objects **directly as Helm templates**.

It is also installed by `helm install` during **bootstrap phase 1**, before ArgoCD (and therefore the
CMP) exist at all — another reason it must stand alone as a Helm chart.

## Dual role (bootstrap + day-2)

- **Day-0 bootstrap:** the root-level `openshift-gitops/` chart is `helm install`-ed to stand up ArgoCD
  and ship a day-0 copy of the CMP config.
- **Day-2 self-heal:** this `policies/stable/openshift-gitops/` chart is deployed as a self-healing
  ArgoCD Application that continuously reconciles the GitOps configuration — the operator install, the
  ArgoCD instances, the PolicyGenerator CMP, the user CA bundle, and the console link.

## Consequences for authors

Because the `Policy` objects here are hand-written Helm templates (not PolicyGenerator output):

- **Hub templates must be Helm-escaped**: write `{{ "{{hub" }} … {{ "hub}}" }}` (and `{{ "{{hub-" }}`
  for the trim form), because Helm renders the file before ACM ever sees it. Contrast the raw
  `{{hub … hub}}` you can write in a PolicyGenerator manifest.
- **The operator install is hand-written too** (`policy-gitops-operator-install.yaml`), not the shared
  `components/operator-install` chart — the whole `OperatorPolicy` lives inline in a Helm template.
- Placements, `PlacementBinding`s, and `remediationAction` are authored by hand here, not generated.

Version pinning still behaves like every other operator: set `config.gitops.versions` (and optional
`config.gitops.startingCSV`), else the `autoshift.io/gitops-version` label is used — implemented inline
in `policy-gitops-operator-install.yaml`.
