# oc-mirror Tekton Pipeline

This directory contains Tekton Pipeline resources for building and deploying the oc-mirror container with AutoShift integration.

## Overview

The pipeline uses standard Tekton ClusterTasks (`git-clone`, `buildah`) to provide automated build and deployment of oc-mirror container images. The container supports multiple authentication methods for different deployment scenarios:

**Deployment Modes:**
- **Job**: One-time execution for immediate mirroring
- **CronJob**: Scheduled execution for regular mirroring updates  
- **Deployment**: Persistent service for on-demand mirroring

**Authentication:**
- **Standard Mount Point**: `/workspace/pull-secret.txt` for all scenarios
- **Kubernetes**: Secret automatically mounted by pipeline
- **Podman/Docker**: Manual mount from host file

## Pipeline Tasks

1. **fetch-source** - Uses `git-clone` ClusterTask to clone repository
2. **validate-pull-secret** - Custom task to validate/create pull secret
3. **build-image** - Uses `buildah` ClusterTask to build container
4. **deploy-oc-mirror** - Custom task to deploy oc-mirror workload

## Files

- `01-pipeline.yaml` - Main pipeline definition using ClusterTasks
- `02-task-validate-pull-secret.yaml` - Task to validate pull secret
- `03-task-deploy.yaml` - Task to deploy oc-mirror workload
- `04-pipelinerun.yaml` - Example pipeline run
- `05-resources.yaml` - Required resources (ServiceAccount, PVC, Secrets, Triggers)

## Prerequisites

1. **OpenShift Pipelines Operator** installed (provides `git-clone` and `buildah` ClusterTasks)
2. **Red Hat pull secret** for accessing Red Hat registries
3. **Storage class** available for PVCs (defaults to `gp3-csi`)

## Quick Start

### 1. Install Resources

```bash
# Apply all pipeline resources
oc apply -f oc-mirror/pipeline/

# Verify installation
oc get pipeline,task -n openshift-pipelines | grep oc-mirror
```

### 2. Create Required Secrets

#### A. Create Pull Secret for Red Hat Registries

Download your pull secret from the [Red Hat Console](https://console.redhat.com/openshift/install/pull-secret):

```bash
# Option 1: Create from downloaded pull-secret file
oc create secret generic oc-mirror-pull-secret \
  --from-file=.dockerconfigjson=/path/to/pull-secret.json \
  --type=kubernetes.io/dockerconfigjson \
  --namespace=openshift-pipelines

# Option 2: Create from your cluster's existing pull secret
oc get secret pull-secret -n openshift-config -o jsonpath='{.data.\.dockerconfigjson}' | \
  base64 -d > /tmp/pull-secret.json

oc create secret generic oc-mirror-pull-secret \
  --from-file=.dockerconfigjson=/tmp/pull-secret.json \
  --type=kubernetes.io/dockerconfigjson \
  --namespace=openshift-pipelines

# Clean up temp file
rm -f /tmp/pull-secret.json

# Verify the secret
oc get secret oc-mirror-pull-secret -n openshift-pipelines
```

#### B. Create Registry Authentication Secret

```bash
# Create registry auth secret for internal OpenShift registry
oc create secret docker-registry pipeline-registry-auth \
  --docker-server=image-registry.openshift-image-registry.svc:5000 \
  --docker-username=unused \
  --docker-password=$(oc whoami -t) \
  --namespace=openshift-pipelines

# Verify the secret
oc get secret pipeline-registry-auth -n openshift-pipelines
```

### 3. Run Pipeline

#### Option A: One-time Job Deployment

```bash
# Run pipeline to deploy as Job
oc create -f oc-mirror/pipeline/04-pipelinerun.yaml
```

#### Option B: Scheduled CronJob Deployment

```bash
# Create PipelineRun with CronJob mode
cat << EOF | oc apply -f -
apiVersion: tekton.dev/v1beta1
kind: PipelineRun
metadata:
  generateName: oc-mirror-cronjob-
  namespace: openshift-pipelines
spec:
  pipelineRef:
    name: oc-mirror-build-deploy
  params:
    - name: deploy-mode
      value: "cronjob"
    - name: schedule
      value: "0 2 * * 0"  # Weekly at 2 AM Sunday
    - name: workflow
      value: "workflow-to-disk"
  workspaces:
    - name: shared-workspace
      volumeClaimTemplate:
        spec:
          accessModes: [ReadWriteOnce]
          resources:
            requests:
              storage: 5Gi
    - name: dockerconfig-ws
      secret:
        secretName: pipeline-registry-auth
    - name: mirror-data
      persistentVolumeClaim:
        claimName: oc-mirror-workspace
EOF
```

#### Option C: Persistent Deployment

```bash
# Create PipelineRun with Deployment mode
cat << EOF | oc apply -f -
apiVersion: tekton.dev/v1beta1
kind: PipelineRun
metadata:
  generateName: oc-mirror-deployment-
  namespace: openshift-pipelines
spec:
  pipelineRef:
    name: oc-mirror-build-deploy
  params:
    - name: deploy-mode
      value: "deployment"
    - name: workflow
      value: "bash"  # Interactive shell
  workspaces:
    - name: shared-workspace
      volumeClaimTemplate:
        spec:
          accessModes: [ReadWriteOnce]
          resources:
            requests:
              storage: 5Gi
    - name: dockerconfig-ws
      secret:
        secretName: pipeline-registry-auth
    - name: mirror-data
      persistentVolumeClaim:
        claimName: oc-mirror-workspace
EOF
```

## Configuration

### Pipeline Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `git-url` | `https://github.com/auto-shift/autoshiftv2.git` | Git repository URL |
| `git-revision` | `main` | Git branch/tag to build |
| `image` | `image-registry.openshift-image-registry.svc:5000/openshift-pipelines/oc-mirror-autoshift:latest` | Full container image name with registry and tag |
| `values-file` | `values.hub.yaml` | AutoShift values file |
| `deploy-mode` | `job` | Deployment mode (job/cronjob/deployment) |
| `schedule` | `0 2 * * 0` | CronJob schedule |
| `workflow` | `workflow-to-disk` | oc-mirror workflow |

### Available Workflows

- `workflow-to-disk` - Mirror images to disk for air-gapped environments
- `workflow-to-mirror` - Direct registry-to-registry mirroring
- `generate-imageset` - Generate ImageSetConfiguration only
- `mirror-to-disk` - Mirror to disk (existing ImageSet)
- `disk-to-mirror` - Upload from disk to registry
- `delete-generate` - Generate deletion ImageSet
- `delete-execute` - Execute image deletion

## Monitoring

### Check Pipeline Status

```bash
# View pipeline runs
oc get pipelinerun -n openshift-pipelines

# View specific run logs
oc logs -f -n openshift-pipelines pipelinerun/<run-name>
```

### Check Deployed oc-mirror

```bash
# View oc-mirror resources
oc get all,pvc,cm,secret -n oc-mirror -l app.kubernetes.io/name=oc-mirror

# Check Job/CronJob status
oc get jobs,cronjobs -n oc-mirror

# View oc-mirror logs
oc logs -n oc-mirror job/<job-name>
```

## Webhook Integration (Optional)

The pipeline includes GitHub webhook support for automated builds:

### 1. Expose EventListener

```bash
# Create Route for EventListener
oc expose svc el-oc-mirror-event-listener -n openshift-pipelines
```

### 2. Configure GitHub Webhook

1. Get webhook URL: `oc get route el-oc-mirror-event-listener -n openshift-pipelines`
2. In GitHub repository settings, add webhook:
   - URL: `https://<route-url>`
   - Content type: `application/json`
   - Events: `Push events`

## Troubleshooting

### Common Issues

1. **Pull Secret Not Found**
   ```bash
   # Check if secret exists
   oc get secret oc-mirror-pull-secret -n openshift-pipelines
   
   # If missing, create it:
   oc create secret generic oc-mirror-pull-secret \
     --from-file=.dockerconfigjson=/path/to/pull-secret.json \
     --type=kubernetes.io/dockerconfigjson \
     --namespace=openshift-pipelines
   ```

2. **Registry Authentication Failures**
   ```bash
   # Recreate registry auth secret
   oc delete secret pipeline-registry-auth -n openshift-pipelines || true
   oc create secret docker-registry pipeline-registry-auth \
     --docker-server=image-registry.openshift-image-registry.svc:5000 \
     --docker-username=unused \
     --docker-password=$(oc whoami -t) \
     --namespace=openshift-pipelines
   ```

3. **Invalid Pull Secret Format**
   ```bash
   # Validate pull secret format
   oc get secret oc-mirror-pull-secret -n openshift-pipelines \
     -o jsonpath='{.data.\.dockerconfigjson}' | base64 -d | jq .
   
   # Should show valid JSON with auths section
   ```

4. **Storage Issues**
   - Verify storage class exists: `oc get storageclass`
   - Adjust PVC size in resources if needed

5. **Pipeline Failures**
   - Check TaskRun logs: `oc logs -f taskrun/<task-run-name>`
   - Verify ServiceAccount permissions

### Debug Commands

```bash
# Check pipeline resources
oc describe pipeline oc-mirror-build-deploy -n openshift-pipelines

# View task definitions
oc describe task oc-mirror-validate-pull-secret -n openshift-pipelines
oc describe task oc-mirror-deploy -n openshift-pipelines

# Check workspace PVC
oc describe pvc oc-mirror-workspace -n openshift-pipelines

# View deployed oc-mirror
oc describe deployment oc-mirror-deployment -n oc-mirror
```

## Advanced Usage

### Custom Values File

```bash
# Use different AutoShift values file
cat << EOF | oc apply -f -
apiVersion: tekton.dev/v1beta1
kind: PipelineRun
metadata:
  generateName: oc-mirror-sbx-
  namespace: openshift-pipelines
spec:
  pipelineRef:
    name: oc-mirror-build-deploy
  params:
    - name: values-file
      value: "values.sbx.yaml"
    - name: workflow
      value: "workflow-to-disk"
  # ... workspaces
EOF
```

### Multi-Environment Deployment

Deploy different configurations for different environments by running the pipeline with different parameters for each environment.

## Alternative: Podman/Docker Usage

If you prefer to run oc-mirror without Kubernetes pipeline:

```bash
# 1. Build the container
podman build -f oc-mirror/Containerfile -t oc-mirror-autoshift:latest .

# 2. Run with mounted pull secret
podman run --rm \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk

# 3. Use different values file
podman run --rm \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest workflow-to-disk --values-file values.sbx.yaml

# 4. Interactive mode
podman run -it \
  -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro \
  -v oc-mirror-data:/workspace/content \
  oc-mirror-autoshift:latest bash
```

## Quick Command Reference

```bash
# 1. Install pipeline
oc apply -f oc-mirror/pipeline/

# 2. Create pull secret (required)
oc create secret generic oc-mirror-pull-secret \
  --from-file=.dockerconfigjson=/path/to/pull-secret.json \
  --type=kubernetes.io/dockerconfigjson \
  --namespace=openshift-pipelines

# 3. Create registry secret (required)
oc create secret docker-registry pipeline-registry-auth \
  --docker-server=image-registry.openshift-image-registry.svc:5000 \
  --docker-username=unused \
  --docker-password=$(oc whoami -t) \
  --namespace=openshift-pipelines

# 4. Copy pull secret to oc-mirror namespace (done automatically by deploy task)
# The deploy task will copy the secret when creating the oc-mirror namespace

# 5. Run pipeline
oc create -f oc-mirror/pipeline/04-pipelinerun.yaml

# 6. Check status
oc get pipelinerun -n openshift-pipelines
oc logs -f -n openshift-pipelines pipelinerun/<run-name>
```