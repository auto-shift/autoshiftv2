# External values repository

AutoShift separates **code** from **configuration**. The `autoshiftv2` repo holds policy charts and
the ApplicationSet; your values — which clusters exist, what each clusterset turns on — belong in a
repo you own. ArgoCD stitches the two together with a **multi-source Application**.

This is the recommended production model. The alternative — editing `autoshift/values/` in a fork, or
pasting labels inline into the Application — works for a sandbox but makes every AutoShift upgrade a
merge conflict.

## Why separate them

| Problem with in-repo / inline values | With an external values repo |
|---|---|
| Upgrading AutoShift means rebasing your fork | Bump `targetRevision`; your config is untouched |
| Site config is mixed into shared code | One repo per site, owned by whoever runs the site |
| Inline `helm.values` silently drifts from the curated profiles | Profiles stay canonical; you record only your deltas |
| No review trail for a config change | A cluster change is a PR against your own repo |
| Secrets and site topology sit in a public fork | Your repo, your access control |

## What goes where

| Belongs in `autoshiftv2` | Belongs in your values repo |
|---|---|
| `policies/**` — the charts | `global.yaml` — fleet-wide settings |
| `autoshift/templates/**` — the ApplicationSet | `clustersets/*.yaml` — profiles per clusterset |
| `autoshift/values/**` — reference profiles | `clusters/*.yaml` — per-cluster overrides |
| Anything you'd send upstream in a PR | Anything specific to your estate |

Treat the shipped `autoshift/values/` as **reference material**: copy a profile as your starting
point, then diverge in your own repo.

## Repo layout

Mirror the shipped structure so paths stay predictable:

```
site-config/
  global.yaml
  clustersets/
    hub.yaml
    managed.yaml
  clusters/
    spoke1.yaml
    spoke2.yaml
```

Nothing enforces this layout — `valueFiles` paths are explicit — but matching the upstream shape
makes it obvious which reference profile a file descends from.

## The multi-source application

Two sources: your values repo carries a `ref`, and the chart source refers to it with `$ref/`.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  project: default
  sources:
    # 1) Values. `ref` names it; no `path`, because nothing is rendered from this source.
    - repoURL: https://gitlab.apps.example.com/site/site-config.git
      targetRevision: main
      ref: siteValues

    # 2) The chart, rendered with values pulled from source 1.
    - repoURL: https://github.com/auto-shift/autoshiftv2.git
      targetRevision: main
      path: autoshift
      helm:
        valueFiles:
          - $siteValues/global.yaml
          - $siteValues/clustersets/hub.yaml
          - $siteValues/clusters/spoke1.yaml
        # Injected by Argo CD from this source, so the repository is named once. In a
        # multi-source Application these resolve to the source that declares them, not to the
        # values repository above.
        parameters:
          - name: autoshiftGitRepo
            value: $ARGOCD_APP_SOURCE_REPO_URL
          - name: autoshiftGitBranchTag
            value: $ARGOCD_APP_SOURCE_TARGET_REVISION
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: false
      selfHeal: true
```

The `ref` source must be a **git** repo — you cannot `$ref` a Helm/OCI source. The values repo is
fetched but never rendered, so it needs no `path` and produces no resources of its own.

Requires ArgoCD 2.8+ (OpenShift GitOps 1.9+). AutoShift targets `gitops-1.21`, so this is
comfortably available.

### OCI mode

Same shape, with the chart pulled from the registry instead of git:

```yaml
  sources:
    - repoURL: https://gitlab.apps.example.com/site/site-config.git
      targetRevision: main
      ref: siteValues

    - repoURL: quay.io/autoshift
      chart: autoshift
      targetRevision: "0.0.1"
      helm:
        valueFiles:
          - $siteValues/global.yaml
          - $siteValues/clustersets/hub.yaml
        values: |
          # Where the policy charts are published. Change this if you release your own charts.
          autoshiftOciRepo: oci://quay.io/autoshift/policies
          policyGenerator: false
        # Injected from this source's targetRevision, so the release is pinned once.
        parameters:
          - name: autoshiftOciVersion
            value: $ARGOCD_APP_SOURCE_TARGET_REVISION
```

## Merge semantics

Two rules govern what actually reaches the chart:

**1. `valueFiles` are applied in order; later files win.** Put the broadest first:

```yaml
valueFiles:
  - $siteValues/global.yaml            # fleet-wide
  - $siteValues/clustersets/hub.yaml   # clusterset profile
  - $siteValues/clusters/spoke1.yaml   # per-cluster override — wins
```

**2. Maps deep-merge; they are not replaced.** A file (or an inline `values:` block) that sets one
label under `hubClusterSets.hub.labels` **adds** it — every other label from earlier files survives.
This is what makes a delta file practical:

```yaml
# clusters/spoke1.yaml — adds tempo without restating the profile
hubClusterSets:
  hub:
    labels:
      tempo: 'true'
```

Note this cuts both ways: because maps merge rather than replace, omitting a label in a later file
does **not** remove one an earlier file set. To unset it, give it an **empty value**: cluster-labels
encodes that as a delete sentinel and strips the label from the `ManagedCluster`:

```yaml
clusters:
  spoke1:
    labels:
      odf:            # empty — removes the label inherited from the clusterset profile
```

Inline `helm.values` is merged last of all, so reserve it for deployment identity — the OCI/git
coordinates, `versionedClusterSets`, `policyGenerator` — not for configuration that belongs in a file.

## Constraints that bite

**`policyGenerator` is required for git mode, recommended-off for OCI.** Git mode renders
PolicyGenerator directories through the `ConfigManagementPlugin` (CMP) sidecar, so `policyGenerator: true` is mandatory —
without it those directories never render and most policies never deploy. Setting it to `false` in
git mode fails at render time:

```
Git/source mode requires policyGenerator: true.
```

OCI ships prerendered charts and never calls the CMP, so `false` is **recommended but not required**.
The shipped `global.yaml` defaults to `true` for git mode; an OCI deployment that inherits it still
works correctly — the deploy mode is chosen by `autoshiftOciRepo`, not by this flag. Overriding
to `false` simply avoids configuring a sidecar nothing uses.

**Application name ≤ 11 characters.** The policy namespace is `policies-<app-name>`, capped at 20
characters and validated at render time. See [gradual-rollout.md](gradual-rollout.md).

**Private values repos need credentials.** Register the repo with ArgoCD before the Application
references it. For an on-cluster GitLab with a self-signed certificate, the repo secret also needs
`insecure: "true"`, and the certificate Subject Alternative Name must match the actual ingress domain.

**The values repo has no schema.** A typing error in a label key is not a render error — it produces a label
nothing consumes. The policy-validation suite only lints labels inside `autoshiftv2`. Compare against
`autoshift/values/clustersets/_example.yaml`, which is the catalog of every declared label.

## Verifying before you commit

Render locally with your own files. Keep the release name ≤ 11 characters so it matches what ArgoCD
will use:

```bash
git clone https://github.com/auto-shift/autoshiftv2.git
git clone https://gitlab.apps.example.com/site/site-config.git

helm template autoshift ./autoshiftv2/autoshift \
  -f site-config/global.yaml \
  -f site-config/clustersets/hub.yaml \
  -f site-config/clusters/spoke1.yaml
```

A naming or `policyGenerator` violation fails here with an explicit message, rather than as a
sync error later.

Once deployed, confirm ArgoCD resolved both sources:

```bash
oc get application.argoproj.io autoshift -n openshift-gitops \
  -o jsonpath='{range .spec.sources[*]}{.repoURL}{"\t"}{.ref}{"\n"}{end}'

oc get application.argoproj.io -n openshift-gitops   # the ApplicationSet's children
```

## Migrating an existing deployment

1. Create the values repo and copy in the profiles you currently use from `autoshift/values/`.
2. Move anything from the Application's inline `helm.values` into those files — keep only deployment
   identity inline.
3. Render locally (as described earlier) and diff against what is live, so the migration is provably a no-op.
4. Convert `source:` to `sources:` with the `ref` block, and apply.

Because step 3 proves the rendered output is unchanged, the switch itself should produce no policy
churn.

## See also

- [gradual-rollout.md](gradual-rollout.md) — running two versions side by side
- [cluster-set-assignment.md](cluster-set-assignment.md) — moving clusters between releases from git
- [values-reference.md](values-reference.md) — every label and config key
- [quickstart.md](quickstart.md) — the single-source Application this replaces
