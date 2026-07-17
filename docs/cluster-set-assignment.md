# Declarative ClusterSet Assignment

Assign a cluster to a `ManagedClusterSet` — and therefore to an AutoShift deployment/release that owns
it — declaratively from the cluster's values, instead of `oc label`. This is the GitOps mechanism for
the versioned blue/green **rollover**: move a cluster from one release to the next with a commit.

## The fields (per-cluster `config`)

In `autoshift/values/clusters/<name>.yaml`:

```yaml
clusters:
  spoke1:
    config:
      clusterSet: 'managed'          # base clusterset name (omit to manage the clusterset manually)
      clusterSetVersion: '0.0.2'     # OPTIONAL target release → clusterset 'managed-0-0-2'
```

The `cluster-set-assignment` policy composes the target clusterset and stamps
`cluster.open-cluster-management.io/clusterset` on the ManagedCluster:

- **`clusterSetVersion` set** → target = `<clusterSet>-<sanitized version>` (e.g. `managed-0-0-2`). This
  is the git-driven rollover knob — the suffix comes from the value, **not** the deployment's own version.
- **`clusterSetVersion` omitted** → target = `<clusterSet><this deployment's ${CLUSTER_SET_SUFFIX}>`
  (matches cluster-install's provisioning behavior, so the two never fight).
- **`clusterSet` omitted** → the policy does nothing; manage the clusterset however you like.

## Ownership: hands off, never steals

The policy only stamps a cluster it's **allowed to own** — the ManagedCluster is unowned (no
`autoshift.io/owning-namespace`) **or** already owned by this deployment. So one release can hand a
cluster off, but a release can never yank a cluster another release owns. (`cluster-labels` then
re-stamps `owning-namespace` to whichever deployment owns the new clusterset.)

## The rollover (versioned blue/green)

With v1 (`0.0.1`, owns `managed-0-0-1`) and v2 (`0.0.2`, owns `managed-0-0-2`) both deployed and both
defining the cluster:

1. Bump `clusterSetVersion: 0.0.1 → 0.0.2` for the cluster, commit.
2. v1 (still the owner at reconcile) composes `managed-0-0-2` **from the git value** and stamps it →
   ownership flips to v2 → v2's `cluster-labels`/policies take over → v1 backs off.
3. **Roll out in waves** by bumping `clusterSetVersion` on the canary cluster first, verifying, then the
   rest — each wave a commit. **`git revert` = rollback.**

> **Important — one shared value:** `clusterSetVersion` must be a value **both** release Applications
> read the *same* (a shared `valueFiles` entry). If v1 says `0.0.1` and v2 says `0.0.2` for the same
> cluster, they fight (v1→`managed-0-0-1`, v2→`managed-0-0-2`, flapping). Put the per-cluster
> `clusterSet`/`clusterSetVersion` in a values file both releases include.

## See also
- [gradual-rollout.md](gradual-rollout.md) — versionedClusterSets blue/green
- [hub-of-hubs.md](hub-of-hubs.md) — ownership across layers
