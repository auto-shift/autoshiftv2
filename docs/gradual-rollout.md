# Gradual Rollout with Multiple Versions

This guide shows how to deploy multiple versions of AutoShift side-by-side for gradual rollouts.

## Overview

Deploy two AutoShift releases simultaneously using the `versionedClusterSets` feature:
- `autoshift-0-0-1` with `versionedClusterSets: true` automatically creates `hub-0-0-1` clusterset
- `autoshift-0-0-2` with `versionedClusterSets: true` automatically creates `hub-0-0-2` clusterset

Migrate clusters by moving them from one clusterset to another.

## How It Works

When `versionedClusterSets: true`, the version/branch is automatically appended to all ClusterSet names:

**OCI Mode** (uses `autoshiftOciVersion`):
| Values Definition | autoshiftOciVersion | Resulting ClusterSet |
|-------------------|---------------------|----------------------|
| `hubClusterSets.hub` | `0.0.1` | `hub-0-0-1` |
| `managedClusterSets.managed` | `0.0.2` | `managed-0-0-2` |

**Git Mode** (uses `autoshiftGitBranchTag`):
| Values Definition | autoshiftGitBranchTag | Resulting ClusterSet |
|-------------------|----------------------|----------------------|
| `hubClusterSets.hub` | `main` | `hub-main` |
| `hubClusterSets.hub` | `feature/new-policy` | `hub-feature-new-policy` |
| `hubClusterSets.hub` | `v0.0.1` | `hub-v0-0-1` |

The value is sanitized for DNS compatibility (dots, slashes replaced with dashes, lowercased).

## Prerequisites

- OpenShift cluster with ACM and GitOps installed
- Access to OCI registry (`oci://quay.io/autoshift`)
- Multiple managed clusters or self-managed hub

## Step-by-Step Guide

### 1. Deploy Current Version (v0.0.1)

```bash
cat <<'EOF' | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift-0-0-1
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: oci://quay.io/autoshift
    chart: autoshift
    targetRevision: "0.0.1"
    helm:
      values: |
        autoshift:
          dryRun: false

        autoshiftOciRegistry: true
        autoshiftOciRepo: oci://quay.io/autoshift/policies
        autoshiftOciVersion: "0.0.1"

        # Automatically append version to clusterset names
        versionedClusterSets: true

        # Base names - will become hub-0-0-1, managed-0-0-1
        selfManagedHubSet: hub

        hubClusterSets:
          hub:
            labels:
              self-managed: 'true'
              openshift-version: '4.18.28'
              gitops: 'true'
              acm-channel: release-2.14
              acm-observability: 'true'
              acs: 'true'
              odf: 'true'
              loki: 'true'
              logging: 'true'

        managedClusterSets:
          managed:
            labels:
              openshift-version: '4.18.28'
              acs: 'true'
              odf: 'true'
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
EOF
```

### 2. Assign Clusters to v0.0.1

```bash
# For self-managed hub (clusterset name = hub + suffix = hub-0-0-1)
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub-0-0-1 --overwrite

# For managed clusters (clusterset name = managed + suffix = managed-0-0-1)
oc label managedcluster spoke-cluster-1 cluster.open-cluster-management.io/clusterset=managed-0-0-1 --overwrite
oc label managedcluster spoke-cluster-2 cluster.open-cluster-management.io/clusterset=managed-0-0-1 --overwrite
oc label managedcluster spoke-cluster-3 cluster.open-cluster-management.io/clusterset=managed-0-0-1 --overwrite
```

### 3. Verify v0.0.1 Deployment

```bash
# Check Application synced
oc get application autoshift-0-0-1 -n openshift-gitops

# Check policy namespace (uses ArgoCD app name)
oc get namespace policies-autoshift-0-0-1

# Check clustersets were created with suffix
oc get managedclusterset hub-0-0-1
oc get managedclusterset managed-0-0-1

# Verify cluster membership
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=hub-0-0-1
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-1
```

### 4. Deploy New Version (v0.0.2)

```bash
cat <<'EOF' | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift-0-0-2
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: oci://quay.io/autoshift
    chart: autoshift
    targetRevision: "0.0.2"
    helm:
      values: |
        autoshift:
          dryRun: false

        autoshiftOciRegistry: true
        autoshiftOciRepo: oci://quay.io/autoshift/policies
        autoshiftOciVersion: "0.0.2"

        # Automatically append version to clusterset names
        versionedClusterSets: true

        # Same base names - will become hub-0-0-2, managed-0-0-2
        selfManagedHubSet: hub

        hubClusterSets:
          hub:
            labels:
              self-managed: 'true'
              openshift-version: '4.18.28'
              gitops: 'true'
              acm-channel: release-2.14
              acm-observability: 'true'
              acs: 'true'
              odf: 'true'
              loki: 'true'
              logging: 'true'
              # New features in v0.0.2
              tempo: 'true'

        managedClusterSets:
          managed:
            labels:
              openshift-version: '4.18.28'
              acs: 'true'
              odf: 'true'
              tempo: 'true'
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
EOF
```

### 5. Migrate Canary Cluster

Move one cluster to test the new version:

```bash
# Move a single spoke cluster to new version
oc label managedcluster spoke-cluster-1 cluster.open-cluster-management.io/clusterset=managed-0-0-2 --overwrite

# Verify migration
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-2
```

### 6. Validate and Continue Migration

```bash
# Check policy compliance on canary cluster
oc get policies -n policies-autoshift-0-0-2 -o custom-columns=NAME:.metadata.name,COMPLIANCE:.status.compliant

# Migrate more clusters after validation
oc label managedcluster spoke-cluster-2 cluster.open-cluster-management.io/clusterset=managed-0-0-2 --overwrite
oc label managedcluster spoke-cluster-3 cluster.open-cluster-management.io/clusterset=managed-0-0-2 --overwrite

# Finally migrate hub
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub-0-0-2 --overwrite
```

### 7. Cleanup Old Version

After all clusters are migrated:

```bash
# Verify no clusters remain on old version
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=hub-0-0-1
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-1
# Should return empty

# Delete old AutoShift deployment
oc delete application autoshift-0-0-1 -n openshift-gitops

# Clustersets will be cleaned up with the application, or manually:
oc delete managedclusterset hub-0-0-1 managed-0-0-1
```

## Rollback

Move clusters back to the old version:

```bash
oc label managedcluster spoke-cluster-1 cluster.open-cluster-management.io/clusterset=managed-0-0-1 --overwrite
```

## Monitoring

### View Cluster Distribution

```bash
echo "=== v0.0.1 Clusters ==="
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=hub-0-0-1 -o name
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-1 -o name

echo "=== v0.0.2 Clusters ==="
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=hub-0-0-2 -o name
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-2 -o name
```

### Check Policy Compliance

```bash
# Old version
oc get policies -n policies-autoshift-0-0-1 -o custom-columns=NAME:.metadata.name,COMPLIANCE:.status.compliant

# New version
oc get policies -n policies-autoshift-0-0-2 -o custom-columns=NAME:.metadata.name,COMPLIANCE:.status.compliant
```

## Naming Summary

With `versionedClusterSets: true`, names are automatically generated:

| Component | v0.0.1 | v0.0.2 |
|-----------|--------|--------|
| ArgoCD Application | `autoshift-0-0-1` | `autoshift-0-0-2` |
| autoshiftOciVersion | `0.0.1` | `0.0.2` |
| Hub ClusterSet | `hub-0-0-1` (auto) | `hub-0-0-2` (auto) |
| Managed ClusterSet | `managed-0-0-1` (auto) | `managed-0-0-2` (auto) |
| Policy Namespace | `policies-autoshift-0-0-1` | `policies-autoshift-0-0-2` |

## Best Practices

1. **Start with one canary cluster** - Validate before broader rollout
2. **Use dry run first** - Set `dryRun: true` on new version to preview changes
3. **Keep old version running** - Don't delete until all clusters migrated
4. **Document configuration differences** - Track what changed between versions
5. **Monitor ACM console** - Watch for policy violations during migration

## Support

- **Issues**: https://github.com/auto-shift/autoshiftv2/issues
- **ACM ClusterSets**: https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes
