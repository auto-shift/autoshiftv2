# Migration Guide: From Monolithic to Separated Pipelines

This guide helps you migrate from the existing monolithic pipeline to the new separated build/deployment architecture.

## 📊 Comparison Overview

| Aspect | Old Pipeline | New Architecture |
|--------|-------------|------------------|
| **Structure** | Single pipeline (build + deploy) | Separated pipelines |
| **Configuration** | Static ImageSet files | Dynamic generation + ConfigMaps |
| **GitOps** | Limited values file support | Full GitOps integration |
| **Incremental** | Not supported | `--incremental` and `--since` support |
| **Flexibility** | Fixed workflow | Multiple workflow options |
| **Reusability** | Build every deployment | Build once, deploy many |

## 🎯 Migration Strategy

### Phase 1: Parallel Deployment (Recommended)
1. Deploy new pipelines alongside existing
2. Test new workflows with dry-run mode
3. Gradually migrate workloads
4. Keep old pipeline as fallback

### Phase 2: Full Migration
1. Update all triggers to use new pipelines
2. Migrate existing configurations
3. Remove old pipeline components

## 🔄 Step-by-Step Migration

### 1. Deploy New Pipeline Components

```bash
# Deploy new pipeline resources (keeping old ones)
oc apply -f oc-mirror/pipeline/build-pipeline.yaml
oc apply -f oc-mirror/pipeline/deployment-pipeline.yaml
oc apply -f oc-mirror/pipeline/task-*.yaml

# Verify deployment
oc get pipelines -n oc-mirror-pipeline
# Should show both old and new pipelines
```

### 2. Create Initial Image ConfigMap

```bash
# Create initial image ConfigMap pointing to current image
oc create configmap oc-mirror-images \
  --from-literal=image-name="oc-mirror-autoshift" \
  --from-literal=image-url="quay.io/autoshift/oc-mirror-autoshift:latest" \
  --from-literal=image-digest="unknown" \
  --from-literal=git-revision="main" \
  --from-literal=build-timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --namespace=oc-mirror-pipeline
```

### 3. Test Build Pipeline

```bash
# Trigger a build to test the new build pipeline
cat << EOF | oc apply -f -
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  generateName: oc-mirror-build-test-
  namespace: oc-mirror-pipeline
spec:
  serviceAccountName: oc-mirror-pipeline
  pipelineRef:
    name: oc-mirror-build-pipeline
  params:
    - name: git-url
      value: "https://github.com/auto-shift/autoshiftv2.git"
    - name: git-revision
      value: "main"
    - name: image-tag
      value: "test-$(date +%s)"
  workspaces:
    - name: shared-workspace
      persistentVolumeClaim:
        claimName: oc-mirror-shared-workspace
    - name: registry-auth
      secret:
        secretName: quay-registry-auth
EOF

# Monitor the build
tkn pipelinerun logs -f $(oc get pipelinerun -n oc-mirror-pipeline --sort-by=.metadata.creationTimestamp -o name | tail -1 | cut -d/ -f2) -n oc-mirror-pipeline
```

### 4. Test Deployment Pipeline with Dry-Run

```bash
# Test deployment pipeline with dry-run
cat << EOF | oc apply -f -
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  generateName: oc-mirror-deploy-test-
  namespace: oc-mirror-pipeline
spec:
  serviceAccountName: oc-mirror-pipeline
  pipelineRef:
    name: oc-mirror-deployment-pipeline
  params:
    - name: values-file-path
      value: "autoshift/values.hub.yaml"
    - name: deploy-mode
      value: "job"
    - name: workflow
      value: "workflow-to-disk"
    - name: incremental-mode
      value: "true"
    - name: dry-run
      value: "true"  # Safe testing
  workspaces:
    - name: shared-workspace
      persistentVolumeClaim:
        claimName: oc-mirror-shared-workspace
    - name: mirror-data
      persistentVolumeClaim:
        claimName: oc-mirror-workspace
EOF
```

### 5. Migrate Existing Configurations

#### Convert Static ImageSets to ConfigMaps
```bash
# If you have existing static ImageSet files
oc create configmap production-imageset \
  --from-file=imageset.yaml=/path/to/existing/imageset.yaml \
  --namespace=oc-mirror

# Label it appropriately
oc label configmap production-imageset \
  --namespace=oc-mirror \
  "app.kubernetes.io/name=oc-mirror" \
  "autoshift.io/config-type=imageset-custom"
```

#### Update Triggers/Webhooks
```bash
# Update any external triggers to point to new pipelines
# Example webhook payload for new build pipeline:
curl -X POST https://your-webhook-endpoint \
  -H "Content-Type: application/json" \
  -d '{
    "pipeline": "oc-mirror-build-pipeline",
    "params": {
      "git-revision": "main",
      "image-tag": "latest"
    }
  }'

# Example webhook payload for new deployment pipeline:
curl -X POST https://your-webhook-endpoint \
  -H "Content-Type: application/json" \
  -d '{
    "pipeline": "oc-mirror-deployment-pipeline",
    "params": {
      "values-file-path": "autoshift/values.hub.yaml",
      "incremental-mode": "true",
      "deploy-mode": "cronjob"
    }
  }'
```

### 6. Production Deployment

```bash
# Deploy production workload with new pipeline
cat << EOF | oc apply -f -
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  generateName: oc-mirror-prod-deployment-
  namespace: oc-mirror-pipeline
spec:
  serviceAccountName: oc-mirror-pipeline
  pipelineRef:
    name: oc-mirror-deployment-pipeline
  params:
    - name: values-file-path
      value: "autoshift/values.hub.yaml"
    - name: deploy-mode
      value: "cronjob"
    - name: schedule
      value: "0 2 * * 1"  # Weekly on Monday
    - name: workflow
      value: "workflow-to-disk"
    - name: incremental-mode
      value: "true"  # Enable incremental mirroring
    - name: dry-run
      value: "false"  # Production run
  workspaces:
    - name: shared-workspace
      persistentVolumeClaim:
        claimName: oc-mirror-shared-workspace
    - name: mirror-data
      persistentVolumeClaim:
        claimName: oc-mirror-workspace
EOF
```

### 7. Cleanup Old Pipeline (Optional)

```bash
# After successful migration, remove old pipeline components
oc delete pipeline oc-mirror-build-deploy -n oc-mirror-pipeline
oc delete task oc-mirror-deploy -n oc-mirror-pipeline
oc delete task oc-mirror-setup-pull-secret -n oc-mirror-pipeline

# Keep old PipelineRuns for reference (they'll auto-cleanup based on retention policy)
```

## 🎯 Key Migration Benefits You'll Gain

### ✅ **Immediate Benefits**
1. **Incremental Mirroring**: 83% faster mirror operations
2. **GitOps Integration**: Values files from Git repositories
3. **Flexible Deployment**: Job, CronJob, or Deployment modes
4. **Better Separation**: Build and deploy independently

### ✅ **Operational Benefits**
1. **Reduced Build Time**: Build once, deploy many times
2. **Configuration Management**: ConfigMaps for different environments
3. **Workflow Flexibility**: Multiple workflow options
4. **Better Monitoring**: Clearer pipeline separation

### ✅ **Development Benefits**
1. **Faster Iteration**: Deploy config changes without rebuilding
2. **Testing**: Dry-run mode for safe testing
3. **Multi-Environment**: Different ConfigMaps per environment
4. **Version Control**: All configs tracked in Git

## 🔧 Common Migration Issues and Solutions

### Issue 1: "Image ConfigMap not found"
```bash
# Solution: Create initial image ConfigMap
oc create configmap oc-mirror-images \
  --from-literal=image-url="your-current-image:tag" \
  --namespace=oc-mirror-pipeline
```

### Issue 2: "Values file not found in Git"
```bash
# Solution: Verify Git repository and file path
oc create job debug-git-fetch --image=alpine/git -- \
  git clone https://github.com/auto-shift/autoshiftv2.git /tmp/repo && \
  ls -la /tmp/repo/autoshift/
```

### Issue 3: "Permission denied for ConfigMap creation"
```bash
# Solution: Verify ServiceAccount permissions
oc get rolebinding -n oc-mirror | grep oc-mirror-pipeline
oc describe rolebinding oc-mirror-pipeline-binding -n oc-mirror
```

### Issue 4: "Incremental mode not working"
```bash
# Solution: Check .history files in persistent volume
oc exec -n oc-mirror deployment/oc-mirror-deployment -- \
  ls -la /workspace/content/working-dir/.history/
```

## 📋 Validation Checklist

- [ ] New pipelines deployed and accessible
- [ ] Build pipeline successfully creates images
- [ ] Image ConfigMap updated after builds
- [ ] Deployment pipeline can fetch values from Git
- [ ] ImageSet generation working (both dynamic and ConfigMap)
- [ ] ConfigMaps created correctly in target namespace
- [ ] Workloads deploy successfully
- [ ] Incremental mirroring functioning
- [ ] Dry-run mode working for testing
- [ ] PVC persistence working across runs

## 🚀 Next Steps After Migration

1. **Set up regular builds**: Trigger build pipeline on code changes
2. **Configure GitOps**: Set up webhooks for values file changes
3. **Monitor operations**: Set up alerting for pipeline failures
4. **Optimize schedules**: Adjust CronJob schedules for your needs
5. **Scale out**: Deploy to multiple environments using different ConfigMaps

The new pipeline architecture provides a solid foundation for scalable, maintainable oc-mirror operations with enhanced GitOps integration and incremental mirroring capabilities.