# OpenShift Fleet Upgrades

AutoShift performs Day 2 OpenShift upgrades the same way it does everything else: **label-driven
policies + clusterset membership**, reconciled by GitOps. There is no separate orchestration operator
— the rollout unit is the **clusterset**, and you stage a fleet upgrade by moving clusters between
clustersets in waves, verifying compliance between waves.

> **Why not Topology Aware Lifecycle Manager?** It orchestrates per-cluster policy enforcement,
> but it requires literal upgrade coordinates baked into the root policy and cannot consume
> AutoShift's hub-template labels (it validates the hub-side policy, where `{{hub}}` values are
> unresolved). it is also a maintenance-mode, ZTP-oriented operator. Red Hat Advanced Cluster Management for Kubernetes has no native progressive
> *policy* rollout either (the `RolloutStrategy` API is consumed by add-ons/ManifestWork, not
> Policies — `Policy`/`PlacementBinding` expose no rollout hook). So the staging lever that actually
> fits AutoShift is clusterset membership, controlled by you.

## The `openshift-upgrade` policies

`policies/stable/openshift-upgrade/` renders **four** policies, all sharing one placement gated on
`autoshift.io/openshift-upgrade: 'true'`. They run as a staged sequence — set the channel, validate
the target, drive the upgrade, report completion:

| # | Policy | Mode | Role |
|---|---|---|---|
| 1 | `policy-openshift-upgrade-channel` | enforce | Sets the `ClusterVersion` channel first, so `availableUpdates` recomputes for the desired channel. Without this, a y-stream target could deadlock the following check. |
| 2 | `policy-openshift-upgrade-allowed` | inform | Asserts the target is a valid, available update. **Policy 3 depends on it**, so an unreachable version blocks the upgrade instead of half-applying it. |
| 3 | `policy-openshift-upgrade` | enforce | Sets `upstream` and `desiredUpdate.version`; the Cluster Version Operator (CVO) does the upgrade. |
| 4 | `policy-openshift-upgrade-status` | inform | The completion gate — Compliant only once the cluster has actually reached the target. **This is the one to watch for wave rollouts.** |

Two guards keep this safe:

- **Semver guard** (policy 3) — asserts `desiredUpdate` only when `target > current`, so clusters
  already at or above the target are a Compliant no-op and downgrades are never attempted. It also
  skips while the cluster is already `Progressing`, so it never fights an in-flight upgrade.
- **Dependency gate** (policy 3 → policy 2) — a typing error'd or unavailable `openshift-version` surfaces as
  a clear NonCompliant message on policy 2 and policy 3 simply never fires.

> **How completion is detected.** Policy 4 does *not* assert `status.history` as a policy field. Red Hat Advanced Cluster Management
> does not reliably match status **lists** (`conditions`, `history`), so the policy computes the latest
> `Completed` history entry in Go template logic and, until the target is reached, forces NonCompliant
> with a `mustnothave` on the `ClusterVersion` — an object that always exists, making it a reliable
> existence check rather than a flappy list match. `clusterversions/status` is a subresource, so
> `enforce` could not write it in any case. Upgrade *failure* visibility comes from OpenShift's own
> upgrade alerts, not from a policy check.

### Labels (set on the target clusterset)

| Label | Purpose | Example |
|---|---|---|
| `autoshift.io/openshift-upgrade` | opt the cluster in to Day 2 upgrades | `'true'` |
| `autoshift.io/openshift-version` | target version — shared with operator-channel tooling (upgrades only if `> current`) | `'4.22.8'` |
| `autoshift.io/openshift-upgrade-channel` | ClusterVersion channel (default `stable-4.22`) | `'stable-4.22'` |
| `autoshift.io/openshift-upgrade-upstream` | OpenShift Update Service graph (local URL when disconnected) | `https://api.openshift.com/...` |

## Validation is free

Because these are normal Red Hat Advanced Cluster Management policies, you get fleet-wide validation with no extra tooling. Query
**`policy-openshift-upgrade-status`** — the completion gate, not the enforcing policy:

```bash
oc get policy -n policies-autoshift policy-openshift-upgrade-status \
  -o jsonpath='{range .status.status[*]}{.clustername}{"\t"}{.compliant}{"\n"}{end}'
```

`policy-openshift-upgrade` goes Compliant as soon as the desired fields are *set*, which happens long
before the CVO finishes. Watching it would declare a wave done while clusters are still upgrading.

To see the whole sequence for one cluster, including which stage is blocking:

```bash
oc get policy -n policies-autoshift -l '!policy.open-cluster-management.io/root-policy' \
  -o custom-columns=NAME:.metadata.name,COMPLIANT:.status.compliant | grep openshift-upgrade
```

ArgoCD surfaces it too: OpenShift GitOps ships a health check for `Policy`, so the
`autoshift-openshift-upgrade` **Application is Healthy only when all four policies are Compliant** —
which, because policy 4 is included, means the upgrade has finished. that is your "this wave is done,
proceed" gate. Expect the Application to sit Degraded for the duration of an upgrade; that is the
gate working, not a fault.

## Rolling out an upgrade (or a new AutoShift version) in waves

The model is **blue/green clustersets** + **wave migration**:

1. **Deploy the new version** as a versioned clusterset (see [gradual-rollout.md](gradual-rollout.md)).
   Its `openshift-version` targets the new OpenShift Container Platform version. The clusterset starts empty (or with a
   canary).
2. **Move a canary cluster** into the new clusterset. The channel policy sets the channel, the allowed
   check validates the target, the upgrade policy sets `desiredUpdate` → the CVO upgrades it.
3. **Verify** the canary through `policy-openshift-upgrade-status` / ArgoCD health.
4. **Move the next wave**, verify, repeat until the fleet is migrated.

**Safety:** blast radius is controlled entirely by membership. **Never enable `openshift-upgrade` on
an already-populated clusterset** — every opted-in cluster in it would upgrade at once (Red Hat Advanced Cluster Management fans the
policy out with no staging). Always move clusters *into* the upgrading clusterset in controlled waves.

### Making the waves Argo-native

Rather than `oc label` by hand, keep clusterset membership **declarative in git** using the
[cluster-set-assignment](cluster-set-assignment.md) policy: set `config.clusterSet` and
`config.versionTag` on the cluster in `autoshift/values/clusters/<name>.yaml`, and the policy stamps
the clusterset label for you.

A rollout is then a series of **commits** moving N clusters per wave; Argo reconciles each; the
ArgoCD compliance-health described earlier tells you when to commit the next wave; `git revert` is your rollback.
The only thing not automated is "auto-proceed when green" — that gate is either your commit cadence
or a thin script that reads compliance and moves the next wave.

Assignment is **owner-guarded**: the policy only stamps clusters its own deployment already owns, so
two AutoShift releases cannot fight over a cluster and a hand-labeled cluster is never stolen. Note
the corollary — if a cluster is under cluster-set-assignment, `oc label` is not a durable override;
the policy re-stamps it. Move it in git instead.

## Hub-of-hubs

Each layer's Red Hat Advanced Cluster Management only sees its own clusters, so a fleet upgrade is still **per-layer, top-down**
(hub-of-hubs → spoke-hubs → leaf spokes): the Red Hat Advanced Cluster Management hub must run a version that supports the clusters it
manages. Apply the `openshift-upgrade` labels on the appropriate clusterset at each layer, and roll
each layer's waves in that order.

## See also

- [gradual-rollout.md](gradual-rollout.md) — versioned-clusterset blue/green migration
- [cluster-set-assignment.md](cluster-set-assignment.md) — declarative clusterset membership (the
  GitOps way to move waves)
- [hub-of-hubs.md](hub-of-hubs.md) — hub-of-hubs topology and the one-instance-per-cluster Red Hat Advanced Cluster Management constraint
