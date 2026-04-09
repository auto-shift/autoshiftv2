# Managed AutoShift Policy

Deploys an AutoShift ArgoCD Application on managed hub clusters (spoke hubs), enabling them to run their own AutoShift instance managed from a hub-of-hubs. Handles namespace creation, optional git repo secret replication, and the ArgoCD Application itself.

## Policies

| Policy | Description |
|--------|-------------|
| `policy-managed-autoshift-ns` | Creates the policy namespace (`policies-<appName>`) on the managed hub with monitoring labels |
| `policy-managed-autoshift-repo` | Replicates an ArgoCD repository Secret to the managed hub's GitOps namespace (when `useRepoSecret` is enabled) |
| `policy-managed-autoshift` | Creates the ArgoCD Application on the managed hub pointing to the `autoshift/` chart with the configured values files and repo |

## PolicySet and Placement

| PolicySet | Targets | Placement Criteria |
|-----------|---------|-------------------|
| `managed-autoshift` | Non-self-managed hub clusters | `gitops: 'true'` AND `autoshift-enable-install: 'true'` AND `self-managed` is not `'true'` (unless `enableSelfManagement` is set) |

The repo policy has its own Placement with the same label criteria but always excludes self-managed hubs (`self-managed: 'false'`).

## Labels

All labels are prefixed with `autoshift.io/`.

| Label | Type | Used In | Description |
|-------|------|---------|-------------|
| `autoshift-enable-install` | bool | Placement selector, hub template guard | Must be `'true'` for the managed AutoShift Application to be created |
| `gitops` | bool | Placement selector | Must be `'true'` — ensures GitOps is available on the target cluster |
| `self-managed` | bool | Placement selector | When `'true'`, excludes the cluster from placement (unless `enableSelfManagement` is set in chart values) |

## Rendered-Config Variables (`autoshift.*`)

These values are read from the per-cluster `rendered-config` ConfigMap on the hub via hub templates. They allow per-cluster override of the AutoShift deployment settings.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `appName` | string | `managed-autoshift` | Name of the ArgoCD Application created on the managed hub. Also used to derive the policy namespace (`policies-<appName>`) |
| `repoUrl` | string | Chart value / `""` | Git repository URL for the AutoShift source |
| `gitopsNamespace` | string | `openshift-gitops` | Namespace where the ArgoCD Application and repo Secret are created |
| `valuesFiles` | list | `["values.<clusterName>.yaml"]` | List of Helm values files passed to the ArgoCD Application source |
| `argoProject` | string | `default` | ArgoCD project for the Application |
| `argoServer` | string | `https://kubernetes.default.svc` | ArgoCD destination server URL |
| `targetRevision` | string | `main` | Git branch or tag to track |
| `useRepoSecret` | bool | `false` | When `true`, replicates a git repo Secret to the managed hub for private repo access |
| `repoSecretRef.name` | string | `autoshift-repo-secret` | Name of the source Secret on the hub to replicate |
| `repoSecretRef.namespace` | string | `<policy_namespace>` | Namespace of the source Secret on the hub |

## Chart Values (`autoshift.*`)

These are Helm chart-level defaults. Per-cluster rendered-config values take precedence at runtime.

| Value | Type | Default | Description |
|-------|------|---------|-------------|
| `appName` | string | `autoshift` | Default ArgoCD Application name |
| `repoUrl` | string | `https://github.com/auto-shift/autoshiftv2.git` | Default git repository URL |
| `valuesFile` | string | `values.hubofhubs.yaml` | Default values file |
| `argoProject` | string | `default` | Default ArgoCD project |
| `gitopsNamespace` | string | `openshift-gitops` | Default GitOps namespace |
| `enableSelfManagement` | bool | `false` | When `true`, allows the policy to also target the self-managed hub (removes the `self-managed` exclusion from placement) |

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

## Examples

### Labels Only

```yaml
# In autoshift/values/clustersets/hub.yaml
hubClusterSets:
  regional-hubs:
    labels:
      gitops: 'true'
      autoshift-enable-install: 'true'
      self-managed: 'false'
```

### Labels with Config

```yaml
# In autoshift/values/clustersets/hub.yaml or autoshift/values/clusters/<cluster>.yaml
hubClusterSets:
  regional-hubs:
    labels:
      gitops: 'true'
      autoshift-enable-install: 'true'
      self-managed: 'false'
    config:
      autoshift:
        appName: 'managed-autoshift'
        repoUrl: 'https://github.com/my-org/autoshiftv2.git'
        targetRevision: 'release-1.0'
        gitopsNamespace: 'openshift-gitops'
        argoProject: 'infrastructure'
        argoServer: 'https://kubernetes.default.svc'
        valuesFiles:
          - 'values/global.yaml'
          - 'values/clustersets/hub.yaml'
        useRepoSecret: false
        repoSecretRef:
          name: 'autoshift-repo-secret'
          namespace: 'open-cluster-policies'
```
