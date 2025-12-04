# Gradual Rollout with Multiple Versions

This guide shows how to deploy multiple versions of AutoShift side-by-side for gradual rollouts using ACM ClusterSets.

## Use Case

Deploy AutoShift v0.0.2 to a subset of clusters while keeping v0.0.1 running on others, then gradually migrate clusters from the old version to the new version.

## Overview

The approach uses **ACM ClusterSets** to partition clusters:
- Deploy `autoshift-stable` managing the `hub-stable` clusterset (v0.0.1)
- Deploy `autoshift-canary` managing the `hub-canary` clusterset (v0.0.2)
- Move clusters between clustersets in ACM to migrate them

## Prerequisites

- OpenShift cluster with ACM installed
- Multiple managed clusters (or self-managed hub)
- Understanding of ACM ClusterSets and ManagedClusterSets

## Step-by-Step Guide

### 1. Create ClusterSets in ACM

Create separate clustersets for stable and canary versions:

```bash
# Create stable clusterset
cat <<EOF | oc apply -f -
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: hub-stable
spec:
  clusterSelector:
    selectorType: LabelSelector
    labelSelector:
      matchLabels:
        autoshift-version: stable
EOF

# Create canary clusterset
cat <<EOF | oc apply -f -
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: hub-canary
spec:
  clusterSelector:
    selectorType: LabelSelector
    labelSelector:
      matchLabels:
        autoshift-version: canary
EOF

# Bind clustersets to openshift-gitops namespace
oc apply -f - <<EOF
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSetBinding
metadata:
  name: hub-stable
  namespace: openshift-gitops
spec:
  clusterSet: hub-stable
---
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSetBinding
metadata:
  name: hub-canary
  namespace: openshift-gitops
spec:
  clusterSet: hub-canary
EOF
```

### 2. Label Clusters

Assign clusters to clustersets using labels:

```bash
# Put most clusters in stable
oc label managedcluster cluster-1 autoshift-version=stable
oc label managedcluster cluster-2 autoshift-version=stable
oc label managedcluster cluster-3 autoshift-version=stable

# Put a few clusters in canary for testing
oc label managedcluster cluster-4 autoshift-version=canary

# For self-managed hub cluster (local-cluster)
oc label managedcluster local-cluster autoshift-version=stable
```

### 3. Create Values Files

Create separate values files for each version:

**values.stable.yaml:**
```yaml
# Stable version configuration
selfManagedHubSet: hub-stable

hubClusterSets:
  hub-stable:
    labels:
      self-managed: 'true'
      openshift-version: '4.18.28'
      # ... your stable configuration

managedClusterSets:
  managed-stable:
    labels:
      openshift-version: '4.18.28'
      # ... your stable configuration
```

**values.canary.yaml:**
```yaml
# Canary version configuration
selfManagedHubSet: hub-canary

hubClusterSets:
  hub-canary:
    labels:
      self-managed: 'true'
      openshift-version: '4.18.28'
      # ... your canary configuration (possibly with new features)

managedClusterSets:
  managed-canary:
    labels:
      openshift-version: '4.18.28'
      # ... your canary configuration
```

### 4. Deploy Multiple Versions

Deploy both versions as separate ArgoCD Applications:

**Deploy Stable (v0.0.1):**
```bash
cat <<EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift-stable
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: oci://quay.io/autoshift
    chart: autoshift
    targetRevision: "0.0.1"
    helm:
      valueFiles:
        - values.hub.yaml
      values: |
        selfManagedHubSet: hub-stable
        autoshiftOciRegistry: true
        autoshiftOciRepo: oci://quay.io/autoshift/policies
        autoshiftOciVersion: "0.0.1"

        hubClusterSets:
          hub-stable:
            labels:
              self-managed: 'true'
              openshift-version: '4.18.28'
              gitops: 'true'
              acm: 'true'
              # ... other labels
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
EOF
```

**Deploy Canary (v0.0.2):**
```bash
cat <<EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift-canary
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: oci://quay.io/autoshift
    chart: autoshift
    targetRevision: "0.0.2"
    helm:
      valueFiles:
        - values.hub.yaml
      values: |
        selfManagedHubSet: hub-canary
        autoshiftOciRegistry: true
        autoshiftOciRepo: oci://quay.io/autoshift/policies
        autoshiftOciVersion: "0.0.2"

        hubClusterSets:
          hub-canary:
            labels:
              self-managed: 'true'
              openshift-version: '4.18.28'
              gitops: 'true'
              acm: 'true'
              # ... other labels (possibly new features)
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
EOF
```

### 5. Verify Deployments

Check that both versions are running:

```bash
# Check Applications
oc get application -n openshift-gitops

# Check ApplicationSets
oc get applicationset -n openshift-gitops

# Check policies for stable version
oc get policies -A | grep autoshift-stable

# Check policies for canary version
oc get policies -A | grep autoshift-canary

# Verify cluster placement
oc get placementdecisions -A
```

### 6. Migrate Clusters Gradually

Move clusters from stable to canary one at a time:

```bash
# Migrate cluster-1 to canary
oc label managedcluster cluster-1 autoshift-version=canary --overwrite

# Wait and verify the migration
oc get placementdecisions -A
oc get policies -A | grep cluster-1

# After validation, migrate more clusters
oc label managedcluster cluster-2 autoshift-version=canary --overwrite
oc label managedcluster cluster-3 autoshift-version=canary --overwrite
```

### 7. Complete Migration

Once all clusters are on the new version:

```bash
# Option A: Remove stable deployment
oc delete application autoshift-stable -n openshift-gitops

# Option B: Make canary the new stable
# 1. Move all clusters to stable clusterset
oc label managedcluster --all autoshift-version=stable --overwrite

# 2. Update stable deployment to v0.0.2
oc patch application autoshift-stable -n openshift-gitops \
  --type=merge \
  -p '{"spec":{"source":{"targetRevision":"0.0.2"}}}'

# 3. Remove canary deployment
oc delete application autoshift-canary -n openshift-gitops
```

## Monitoring and Troubleshooting

### Check Which Version Manages Each Cluster

```bash
# View cluster labels
oc get managedclusters --show-labels

# View clusterset membership
oc get managedclustersets -o yaml

# View placement decisions
oc get placementdecisions -A -o yaml
```

### View Policy Compliance Per Version

```bash
# Stable version policies
oc get policies -A -l app.kubernetes.io/instance=autoshift-stable

# Canary version policies
oc get policies -A -l app.kubernetes.io/instance=autoshift-canary
```

### Common Issues

**Issue: Cluster not picking up policies**
- Check cluster has correct `autoshift-version` label
- Verify clusterset binding exists in openshift-gitops namespace
- Check placement rules in policies

**Issue: Policies from both versions applying to same cluster**
- Verify cluster only has one `autoshift-version` label
- Check clusterset selectors don't overlap

**Issue: ArgoCD conflicts between versions**
- Each version creates separate ApplicationSet with unique name
- Policy namespaces use release name: `policies-autoshift-stable`, `policies-autoshift-canary`
- No conflicts should occur

## Best Practices

1. **Start Small**: Begin with 1-2 canary clusters
2. **Monitor Closely**: Watch policy compliance during migration
3. **Document Changes**: Note configuration differences between versions
4. **Test Rollback**: Verify you can move clusters back to stable if needed
5. **Clean Up**: Remove old version once migration is complete
6. **Use GitOps**: Store your Application manifests in Git for repeatability

## Advanced: Three-Stage Rollout

For larger deployments, use three stages:

```bash
# Create three clustersets
- hub-stable (v0.0.1) - 80% of clusters
- hub-canary (v0.0.2) - 5% of clusters
- hub-beta (v0.0.2) - 15% of clusters

# Deploy three versions
- autoshift-stable → manages hub-stable
- autoshift-canary → manages hub-canary
- autoshift-beta → manages hub-beta

# Migration flow
canary (5%) → beta (20%) → stable (80%)
```

## Alternative: Using ManagedClusterSetBindings

Instead of labels, you can manually assign clusters to clustersets:

```bash
# Create clusterset with explicit cluster list (no label selector)
cat <<EOF | oc apply -f -
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: hub-stable
spec:
  clusterSelector:
    selectorType: ExclusiveClusterSetLabel
EOF

# Explicitly add clusters to the set
oc label managedcluster cluster-1 cluster.open-cluster-management.io/clusterset=hub-stable
oc label managedcluster cluster-2 cluster.open-cluster-management.io/clusterset=hub-stable
```

This gives more explicit control but requires manual management of cluster membership.

## Support

- **ACM ClusterSets**: https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/2.10/html/clusters/cluster_mce_overview#managedclustersets-intro
- **Issues**: https://github.com/auto-shift/autoshiftv2/issues
