# Managed AutoShift Policy

Deploys an AutoShift ArgoCD Application on managed hub clusters (spoke hubs), enabling them to run their own AutoShift instance managed from a hub-of-hubs. Handles namespace creation, optional git repo secret replication, and the ArgoCD Application itself.

> **Scope: the managed hub runs its own instance.** The Application is created in the managed hub's
> own Argo CD, pointing at that hub. The alternative "push" shape — an Application on the hub above,
> targeting the managed hub remotely — is deliberately not supported here: it would mean relaxing the
> Placement so the policy also lands on the self-managed hub, moving the safety check out of a
> declarative predicate and into a template guard. Use the manual second Application for that
> topology (see [Hub of hubs](../../../docs/hub-of-hubs.md)), or run the managed hub's infra agent
> managed, as below.
>
> **`agent-managed` hubs get the Application from the hub above.** When a managed hub's infra
> agent runs managed (`gitops-infra: 'agent-managed'`, or `'agent'` with
> `config.gitops.infra.agent.mode: managed`), `policy-managed-autoshift` does not write it there,
> because the principal owns that hub's Application resources and a second writer would fight it.
> `policy-managed-autoshift-agent` runs on the self-managed hub running the infra principal and
> writes the same Application into the `<cluster>-infra` namespace there, with
> `destination.name: <cluster>-infra`. The agent creates it in its own namespace on the managed hub
> and syncs it. The namespace and repository Secret policies still run on the managed hub, because
> the agent's repo server there fetches the source.
>
> Moving a hub between modes leaves the Application the previous mode wrote. Delete it by hand.

## Policies

| Policy | Description |
|--------|-------------|
| `policy-managed-autoshift-ns` | Creates the policy namespace (`policies-<appName>`) on the managed hub with monitoring labels |
| `policy-managed-autoshift-repo` | Replicates an ArgoCD repository Secret to the managed hub's GitOps namespace (when `useRepoSecret` is enabled) |
| `policy-managed-autoshift` | Creates the ArgoCD Application on the managed hub pointing to the `autoshift/` chart with the configured values files and repo |
| `policy-managed-autoshift-agent` | On the self-managed hub, creates the same Application for every managed hub whose infra agent runs managed, in that agent's `<cluster>-infra` namespace |
| `policy-managed-autoshift-test` | Inform only: each configured entry produced a synced Application on the managed hub |

## Placement

Each policy has its own Placement file, named after it; PolicyGenerator emits one PlacementBinding
per Placement. The namespace, repository Secret, test and Application placements select:

| Label | Required value | Why |
|-------|----------------|-----|
| `cluster-type` | `hub` | Only a hub runs a nested AutoShift |
| `gitops` | `true` | The policy creates an Argo CD Application, so GitOps must be present |
| `autoshift-enable-install` | `true` | Opt in per hub |
| `self-managed` | `false` | The label marks the hub this AutoShift instance itself runs on. That hub already has a top-level Application; this policy bootstraps the other hubs the deployment governs |

The opt-in predicates use `In` rather than `NotIn`. `NotIn` also matches clusters where the label
is absent, which would select hubs that never opted in.

The Application placement (`placement.yaml`) also excludes `gitops-infra: 'agent-managed'`, with
`NotIn` on purpose, so every other mode and an absent label stay selected. The agent placement
(`placement-agent.yaml`) selects `cluster-type: hub`, `self-managed: 'true'` and
`gitops-agent: 'true'` instead: the hub running the infra principal.

## Labels

All labels are prefixed with `autoshift.io/`.

| Label | Type | Used In | Description |
|-------|------|---------|-------------|
| `autoshift-enable-install` | bool | Placement selector, hub template guard | Must be `'true'` for the managed AutoShift Application to be created |
| `gitops` | bool | Placement selector | Must be `'true'` — ensures GitOps is available on the target cluster |
| `self-managed` | bool | Placement selector | Must be `'false'` on the managed hub. `'true'` marks the hub this AutoShift instance runs on, which is already deployed; the agent placement selects that hub |
| `gitops-infra` | string | Application placement, hub template guard | `agent-managed`, or `agent` with `config.gitops.infra.agent.mode: managed`, moves the Application to the hub above |
| `gitops-agent` | bool | Agent placement selector | Must be `'true'` on the self-managed hub for `policy-managed-autoshift-agent` to run there |

## Rendered-Config Variables (`config.managedAutoshift[]`)

These values are read from the per-cluster `rendered-config` ConfigMap on the hub via hub templates. They allow per-cluster override of the AutoShift deployment settings.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `appName` | string | the hub's cluster name | Name of the generated Argo CD Application, and the `Release.Name` of the nested AutoShift. **Max 11 characters**, because it becomes `policies-<appName>` and that namespace is capped at 20. The name `autoshift` is refused: it belongs to the top-level deployment, and this policy uses `mustonlyhave`, so it would overwrite it. An entry breaking either rule creates nothing |
| `repoUrl` | string | `""` | Git repository URL for the AutoShift source. Required in git mode, alongside `targetRevision` |
| `gitopsNamespace` | string | the managed hub's `gitops-namespace` label, else `openshift-gitops` | Namespace where the ArgoCD Application and repo Secret are created. It must be the namespace the managed hub's infra Argo CD runs in, which is why it follows that label by default |
| `valuesFiles` | list | `["values.<clusterName>.yaml"]` | List of Helm values files passed to the ArgoCD Application source |
| `argoProject` | string | `default` | ArgoCD project for the Application |
| `argoServer` | string | `https://kubernetes.default.svc` | Destination server for the generated Application. Ignored when the hub above writes it, which targets the agent with `destination.name: <cluster>-infra` |
| `targetRevision` | string | `main` | Git branch or tag to track |
| `ociRepo` | string | `""` | Registry namespace holding the AutoShift charts, for example `quay.io/autoshift`. Give it **without** the `oci://` scheme: Argo CD's Helm-OCI source takes a bare `host/path`. Setting it selects OCI mode. The nested deployment reads its policy charts from `<ociRepo>/policies`, derived automatically. Mutually exclusive with `repoUrl`/`targetRevision` |
| `ociVersion` | string | `""` | Chart version to pin. Required whenever `ociRepo` is set; without it no Application is created |
| `useRepoSecret` | bool | `false` | When `true`, replicates a git repo Secret to the managed hub for private repo access |
| `repoSecretRef.name` | string | `autoshift-repo-secret` | Name of the source Secret on the hub to replicate |
| `repoSecretRef.namespace` | string | the policy namespace | Namespace of the source Secret on the hub |
| `valuesRepoUrl` | string | Undefined | URL for the values repo to pull the values from if different from the code repo |
| `valuesRepoSecretRef` | object | undefined | Object containing name and namespace reference for a repositor secret allowing argo to sync with the `valuesRepoUrl` |
| `valuesRepoSecretRef.name` | string | undefined | Name of the Argo CD repository Secret containing the values repo creds |
| `valuesRepoSecretRef.namespace` | string | undefined | Name of the Argo CD repository Secret containing the values repo creds |
| `versionedClusterSets` | bool | `false` | Suffix clusterset names with the version tag on the nested instance |
| `helmValues` | map | | Arbitrary values merged into the generated Application's `helm.values`, for anything the fields above do not cover, such as `autoshift.dryRun` |


## Dependencies

| Policy | Depends On |
|--------|-----------|
| `policy-managed-autoshift-repo` | `policy-gitops-systems-argocd`, `policy-acm-mch-install` |
| `policy-managed-autoshift` | `policy-gitops-systems-argocd`, `policy-managed-autoshift-repo` |
| `policy-managed-autoshift-agent` | none: the repository policy runs on the managed hub, and a dependency resolves against the same cluster |

## Prerequisites

- OpenShift GitOps must be installed and an ArgoCD instance running on the target hub
- ACM MultiClusterHub must be installed on the target hub
- The AutoShift git repository must be accessible from the target hub (or `useRepoSecret` must be configured for private repos)
- A per-cluster rendered-config ConfigMap must exist in the policy namespace with the `autoshift` config block

## Example

```yaml
# In autoshift/values/clustersets/hub.yaml or autoshift/values/clusters/<cluster>.yaml
hubClusterSets:
  regional-hubs:
    labels:
      gitops: 'true'
      autoshift-enable-install: 'true'
      self-managed: 'false'
    config:
      managedAutoshift:
        - appName: 'managed-autoshift'
          repoUrl: 'https://github.com/my-org/autoshiftv2.git'
          targetRevision: 'release-1.0'
          gitopsNamespace: 'openshift-gitops'
          argoProject: 'infrastructure'
          argoServer: 'https://kubernetes.default.svc'
          valuesFiles:
            - 'values/global.yaml'
            - 'values/clustersets/hub.yaml'
          useRepoSecret: true
          repoSecretRef:
            name: 'autoshift-repo-secret'
            namespace: 'open-cluster-policies'
```
