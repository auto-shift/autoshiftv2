# Gradual rollout with multiple versions

This guide shows how to deploy multiple versions of AutoShift side-by-side for gradual rollouts.

## Overview

Deploy two AutoShift releases simultaneously using the `versionedClusterSets` feature:
- `as-0-0-1` with `versionedClusterSets: true` automatically creates `hub-0-0-1` clusterset
- `as-0-0-2` with `versionedClusterSets: true` automatically creates `hub-0-0-2` clusterset

Migrate clusters by moving them from one clusterset to another.

## How it works

When `versionedClusterSets: true`, the version/branch is automatically appended to all ClusterSet names:

**OCI Mode** (uses `autoshiftOciVersion`):
| Values Definition | `autoshiftOciVersion` | Resulting ClusterSet |
|-------------------|---------------------|----------------------|
| `hubClusterSets.hub` | `0.0.1` | `hub-0-0-1` |
| `managedClusterSets.managed` | `0.0.2` | `managed-0-0-2` |

**Git Mode** (uses `autoshiftGitBranchTag`):
| Values Definition | `autoshiftGitBranchTag` | Resulting ClusterSet |
|-------------------|----------------------|----------------------|
| `hubClusterSets.hub` | `main` | `hub-main` |
| `hubClusterSets.hub` | `feature/new-policy` | `hub-feature-new-policy` |
| `hubClusterSets.hub` | `v0.0.1` | `hub-v0-0-1` |

The value is sanitized for DNS compatibility (dots, slashes replaced with dashes, lowercased).

### Naming constraint: keep the application name short

The policy namespace is derived from the **ArgoCD Application name** (`policies-<app-name>`) and the
chart validates it at render time:

```
Release name 'autoshift-0-0-1' produces policy namespace 'policies-autoshift-0-0-1'
(24 chars, max 20). Shorten the Helm release name to 11 chars or fewer.
```

Therefore the Application name must be **11 characters or fewer**, which rules out the obvious
`autoshift-0-0-1`. This guide uses `as-0-0-1` / `as-0-0-2` — short enough, and the version stays
visible in the namespace. `autoshift1` and `shift-0-0-1` also fit.

Note this limit applies to the **Application name only**. The clusterset suffix is independent: it
comes from `autoshiftOciVersion` / `autoshiftGitBranchTag`, so clustersets remain `hub-0-0-1` and
`managed-0-0-1` regardless of what you shorten the Application to. The two names do not have to
match, and nothing breaks if they differ.

## Prerequisites

- OpenShift cluster with Red Hat Advanced Cluster Management for Kubernetes and GitOps installed
- Access to OCI registry (`oci://quay.io/autoshift`)
- Multiple managed clusters or self-managed hub

## Step-by-step guide

### 1. Deploy current version (v0.0.1)

```bash
cat <<'EOF' | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: as-0-0-1
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: quay.io/autoshift
    chart: autoshift
    targetRevision: "0.0.1"
    helm:
      # The packaged chart ships its values/ directory, so the curated profiles are available in
      # OCI mode exactly as they are from git. Reference them rather than restating labels inline.
      valueFiles:
        - values/global.yaml
        - values/clustersets/hub.yaml
        - values/clustersets/managed.yaml
      # Inline values are for deployment identity only — what makes THIS deployment different.
      values: |
        autoshift:
          dryRun: false

        # Where the policy charts are published. Change this if you release your own charts.
        autoshiftOciRepo: oci://quay.io/autoshift/policies

        # Recommended in OCI mode: prerendered charts never use the policy-generator CMP, and
        # values/global.yaml defaults it to true for git mode. Not required — leaving it true
        # only configures a sidecar nothing calls.
        policyGenerator: false

        # Automatically append version to clusterset names
        versionedClusterSets: true
      # Injected from this Application's targetRevision, so the release is pinned in one place.
      # This is what keeps a side-by-side rollout honest: the chart version and the policy version
      # cannot drift apart.
      parameters:
        - name: autoshiftOciVersion
          value: $ARGOCD_APP_SOURCE_TARGET_REVISION
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
EOF
```

### 2. Assign clusters to v0.0.1

There are two ways to do this. The imperative form that follows is fine for a sandbox or a one-off, but for
a real rollout prefer the **declarative** path — see
[Declarative migration](#declarative-migration-preferred) before you start labelling by hand.

```bash
# For self-managed hub (clusterset name = hub + suffix = hub-0-0-1)
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub-0-0-1 --overwrite

# For managed clusters (clusterset name = managed + suffix = managed-0-0-1)
oc label managedcluster spoke-cluster-1 cluster.open-cluster-management.io/clusterset=managed-0-0-1 --overwrite
oc label managedcluster spoke-cluster-2 cluster.open-cluster-management.io/clusterset=managed-0-0-1 --overwrite
oc label managedcluster spoke-cluster-3 cluster.open-cluster-management.io/clusterset=managed-0-0-1 --overwrite
```

### 3. Verify v0.0.1 deployment

```bash
# Check Application synced
oc get application.argoproj.io as-0-0-1 -n openshift-gitops

# Check policy namespace (uses ArgoCD app name)
oc get namespace policies-as-0-0-1

# Check clustersets were created with suffix
oc get managedclusterset hub-0-0-1
oc get managedclusterset managed-0-0-1

# Verify cluster membership
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=hub-0-0-1
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-1
```

### 4. Deploy new version (v0.0.2)

```bash
cat <<'EOF' | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: as-0-0-2
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: quay.io/autoshift
    chart: autoshift
    targetRevision: "0.0.2"
    helm:
      valueFiles:
        - values/global.yaml
        - values/clustersets/hub.yaml
        - values/clustersets/managed.yaml
      values: |
        autoshift:
          dryRun: false

        # Where the policy charts are published. Change this if you release your own charts.
        autoshiftOciRepo: oci://quay.io/autoshift/policies

        policyGenerator: false
        versionedClusterSets: true

        # Only the DELTA from the curated profiles. Helm deep-merges maps, so this adds tempo
        # to the labels from hub.yaml/managed.yaml — it does not replace them.
        hubClusterSets:
          hub:
            labels:
              tempo: 'true'

        managedClusterSets:
          managed:
            labels:
              tempo: 'true'
      # Injected from this Application's targetRevision, so the release is pinned in one place.
      # This is what keeps a side-by-side rollout honest: the chart version and the policy version
      # cannot drift apart.
      parameters:
        - name: autoshiftOciVersion
          value: $ARGOCD_APP_SOURCE_TARGET_REVISION
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
EOF
```

### 5. Migrate canary cluster

Move one cluster to test the new version:

```bash
# Move a single spoke cluster to new version
oc label managedcluster spoke-cluster-1 cluster.open-cluster-management.io/clusterset=managed-0-0-2 --overwrite

# Verify migration
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-2
```

### 6. Validate and continue migration

```bash
# Check policy compliance on canary cluster
oc get policies -n policies-as-0-0-2 -o custom-columns=NAME:.metadata.name,COMPLIANCE:.status.compliant

# Migrate more clusters after validation
oc label managedcluster spoke-cluster-2 cluster.open-cluster-management.io/clusterset=managed-0-0-2 --overwrite
oc label managedcluster spoke-cluster-3 cluster.open-cluster-management.io/clusterset=managed-0-0-2 --overwrite

# Finally migrate hub
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub-0-0-2 --overwrite
```

### 7. Cleanup old version

After all clusters are migrated:

```bash
# Verify no clusters remain on old version
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=hub-0-0-1
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-1
# Should return empty

# Delete old AutoShift deployment
oc delete application.argoproj.io as-0-0-1 -n openshift-gitops

# Clustersets will be cleaned up with the application, or manually:
oc delete managedclusterset hub-0-0-1 managed-0-0-1
```

## Declarative migration (preferred)

Everything described earlier moves clusters with `oc label`, which is imperative and leaves no record of why a
cluster sits where it does. The [cluster-set-assignment](cluster-set-assignment.md) policy does the
same job from git: set the target release on the cluster's own values file and let the policy stamp
the label.

```yaml
# autoshift/values/clusters/spoke-cluster-1.yaml
clusters:
  spoke-cluster-1:
    config:
      clusterSet: 'managed'      # base name; the suffix is derived
      versionTag: '0.0.2'        # the release that should own this cluster
```

`versionTag` is sanitized exactly like the deployment's own suffix (dots and slashes to dashes,
lowercased), so `0.0.2` resolves to `managed-0-0-2`.

A wave is then a **commit** that bumps `versionTag` for N clusters; ArgoCD reconciles it; `git revert`
is the rollback. Two properties matter for rollouts:

- **Owner-guarded**: a deployment only re-stamps clusters it already owns, so two releases running
  side by side cannot fight over a cluster, and a cluster you assigned by hand is never stolen.
- **`oc label` is not a durable override** for a cluster under cluster-set-assignment — the policy
  will re-stamp it. Move it in git instead.

Omit `clusterSet` entirely to keep managing that cluster's membership manually.

## Rollback

Move clusters back to the old version — in git, by reverting the `versionTag` change:

```bash
git revert <wave-commit>
```

Alternatively, imperatively, if the cluster is not under cluster-set-assignment:

```bash
oc label managedcluster spoke-cluster-1 cluster.open-cluster-management.io/clusterset=managed-0-0-1 --overwrite
```

Because the old deployment is still running and its clusterset still exists, rollback is just
membership moving back — no redeploy.

## Monitoring

### View cluster distribution

```bash
echo "=== v0.0.1 Clusters ==="
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=hub-0-0-1 -o name
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-1 -o name

echo "=== v0.0.2 Clusters ==="
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=hub-0-0-2 -o name
oc get managedclusters -l cluster.open-cluster-management.io/clusterset=managed-0-0-2 -o name
```

### Check policy compliance

```bash
# Old version
oc get policies -n policies-as-0-0-1 -o custom-columns=NAME:.metadata.name,COMPLIANCE:.status.compliant

# New version
oc get policies -n policies-as-0-0-2 -o custom-columns=NAME:.metadata.name,COMPLIANCE:.status.compliant
```

## Naming summary

With `versionedClusterSets: true`, names are automatically generated:

| Component | v0.0.1 | v0.0.2 |
|-----------|--------|--------|
| ArgoCD Application | `as-0-0-1` | `as-0-0-2` |
| `autoshiftOciVersion` | `0.0.1` | `0.0.2` |
| Hub ClusterSet | `hub-0-0-1` (auto) | `hub-0-0-2` (auto) |
| Managed ClusterSet | `managed-0-0-1` (auto) | `managed-0-0-2` (auto) |
| Policy Namespace | `policies-as-0-0-1` | `policies-as-0-0-2` |

## Best practices

1. **Start with one canary cluster** - Validate before broader rollout
2. **Use dry run first** - Set `dryRun: true` on new version to preview changes
3. **Keep old version running** - do not delete until all clusters migrated
4. **Document configuration differences** - Track what changed between versions
5. **Monitor Red Hat Advanced Cluster Management console** - Watch for policy violations during migration
6. **Keep Application names ≤ 11 chars** - anything longer fails naming validation at render time
7. **Move waves in git** - use [cluster-set-assignment](cluster-set-assignment.md) so each wave is a
   reviewable commit and `git revert` is the rollback

## See also

- [external-values-repo.md](external-values-repo.md) — keep these values in your own repo instead of
  inline, and combine them with the chart through a multi-source Application
- [cluster-set-assignment.md](cluster-set-assignment.md) — declarative clusterset membership
- [ocp-upgrade.md](ocp-upgrade.md) — by using this same wave model to stage OpenShift upgrades

## Support

- **Issues**: https://github.com/auto-shift/autoshiftv2/issues
- **Red Hat Advanced Cluster Management ClusterSets**: https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes
