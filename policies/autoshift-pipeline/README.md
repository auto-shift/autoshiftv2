# AutoShift Pipeline Policy

This ACM policy provides automated CI/CD capabilities for AutoShift deployments using Tekton Pipelines. The pipeline validates repository structure, deploys AutoShift applications via ArgoCD, and monitors deployment status.

## Features

- **Repository validation**: Validates AutoShift repository structure
- **ArgoCD deployment**: Creates/updates ArgoCD Applications for AutoShift
- **Status monitoring**: Waits for successful deployment completion
- **Disconnected environment support**: Works in air-gapped environments
- **Label-based configuration**: Configurable via cluster labels

## Architecture

The policy creates three Tekton tasks when deployed:

1. **validate-autoshift-structure** - Validates repository structure and required files
2. **deploy-autoshift** - Creates/updates ArgoCD Application for AutoShift deployment
3. **wait-for-argocd-sync** - Monitors ArgoCD Application until sync completes

## Configuration

The pipeline is configured entirely through cluster labels:

| Label | Type | Description | Default |
|-------|------|-------------|---------|
| `autoshift.io/autoshift-pipeline` | bool | Enable/disable pipeline deployment | - |
| `autoshift.io/autoshift-pipeline-git-repo` | string | Git repository URL | `https://github.com/auto-shift/autoshiftv2.git` |
| `autoshift.io/autoshift-pipeline-cli-image` | string | CLI container image for disconnected environments | `registry.redhat.io/openshift4/ose-cli:latest` |

## Deployment

This policy is automatically deployed as part of the AutoShift ApplicationSet when the appropriate labels are configured. No separate installation is required.

### Enable in Hub Cluster

Add to your `autoshift/values.hub.yaml`:

```yaml
hubClusterSets:
  hub:
    labels:
      # Enable OpenShift Pipelines (required dependency)
      pipelines: 'true'
      # Enable AutoShift Pipeline
      autoshift-pipeline: 'true'
```

### Enable in Managed Clusters

Add to your cluster labels or clusterset configuration:

```yaml
managedClusterSets:
  managed:
    labels:
      pipelines: 'true'
      autoshift-pipeline: 'true'
```

### Disconnected Environments

For air-gapped deployments, specify your internal CLI image:

```yaml
hubClusterSets:
  hub:
    labels:
      pipelines: 'true'
      autoshift-pipeline: 'true'
      autoshift-pipeline-cli-image: 'registry.internal.mil/openshift4/ose-cli:v4.18'
```

## Dependencies

- **OpenShift Pipelines**: Must be enabled (`pipelines: 'true'`)
- **OpenShift GitOps**: Required for ArgoCD Application deployment
- **Advanced Cluster Management**: Policy management and deployment

## Integration

This pipeline integrates with the AutoShift GitOps workflow:

1. **Policy Deployment**: Deployed automatically via AutoShift ApplicationSet
2. **Label Configuration**: Controlled through cluster labels in values files
3. **Pipeline Execution**: Triggered manually or via webhooks to deploy AutoShift
4. **ArgoCD Integration**: Creates/updates AutoShift ArgoCD Applications
5. **Status Monitoring**: Ensures successful deployment completion