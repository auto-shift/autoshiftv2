# Managed AutoShift Policy

Deploys an AutoShift ArgoCD Application on managed hub clusters (spoke hubs), enabling them to run their own AutoShift instance managed from a hub-of-hubs. Handles namespace creation, optional git repo secret replication, and the ArgoCD Application itself.

> **Scope: the managed hub runs its own instance.** The Application is created in the managed hub's
> own Argo CD, pointing at that hub. The alternative "push" shape — an Application on the hub above,
> targeting the managed hub remotely — is deliberately not supported here: it would mean relaxing the
> Placement so the policy also lands on the self-managed hub, moving the safety check out of a
> declarative predicate and into a template guard. Use the manual second Application for that
> topology (see [Hub of hubs](../../../docs/hub-of-hubs.md)), pending Argo CD agent support.

## Policies

| Policy | Description |
|--------|-------------|
| `policy-managed-autoshift-ns` | Creates the policy namespace (`policies-<appName>`) on the managed hub with monitoring labels |
| `policy-managed-autoshift-repo` | Replicates an ArgoCD repository Secret to the managed hub's GitOps namespace (when `useRepoSecret` is enabled) |
| `policy-managed-autoshift` | Creates the ArgoCD Application on the managed hub pointing to the `autoshift/` chart with the configured values files and repo |

## Placement

All three policies share one Placement (`placement.yaml`); PolicyGenerator emits the PlacementBinding.

| Label | Required value | Why |
|-------|----------------|-----|
| `cluster-type` | `hub` | Only a hub runs a nested AutoShift |
| `gitops` | `true` | The policy creates an Argo CD Application, so GitOps must be present |
| `autoshift-enable-install` | `true` | Opt in per hub |
| `self-managed` | `false` | The label marks the hub this AutoShift instance itself runs on. That hub already has a top-level Application; this policy bootstraps the other hubs the deployment governs |

Every predicate uses `In` rather than `NotIn`. `NotIn` also matches clusters where the label is
absent, which would select hubs that never opted in.

## Labels

All labels are prefixed with `autoshift.io/`.

| Label | Type | Used In | Description |
|-------|------|---------|-------------|
| `autoshift-enable-install` | bool | Placement selector, hub template guard | Must be `'true'` for the managed AutoShift Application to be created |
| `gitops` | bool | Placement selector | Must be `'true'` — ensures GitOps is available on the target cluster |
| `self-managed` | bool | Placement selector | Must be `'false'`. `'true'` marks the hub this AutoShift instance runs on, which is already deployed |

## Rendered-Config Variables (`config.managedAutoshift[]`)

These values are read from the per-cluster `rendered-config` ConfigMap on the hub via hub templates. They allow per-cluster override of the AutoShift deployment settings.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `appName` | string | the hub's cluster name | Name of the generated Argo CD Application, and the `Release.Name` of the nested AutoShift. **Max 11 characters**, because it becomes `policies-<appName>` and that namespace is capped at 20. The name `autoshift` is refused: it belongs to the top-level deployment, and this policy uses `mustonlyhave`, so it would overwrite it. An entry breaking either rule creates nothing |
| `repoUrl` | string | `""` | Git repository URL for the AutoShift source. Required in git mode, alongside `targetRevision` |
| `gitopsNamespace` | string | `openshift-gitops` | Namespace where the ArgoCD Application and repo Secret are created |
| `valuesFiles` | list | `["values.<clusterName>.yaml"]` | List of Helm values files passed to the ArgoCD Application source |
| `argoProject` | string | `default` | ArgoCD project for the Application |
| `argoServer` | string | `https://kubernetes.default.svc` | Destination server for the generated Application |
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
| `policy-managed-autoshift` | `policy-gitops-systems-argocd`, `policy-acm-mch-install`, `policy-managed-autoshift-repo` |

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
