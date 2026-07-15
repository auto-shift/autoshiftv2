# OpenShift Fleet Upgrades

AutoShift performs Day-2 OpenShift upgrades the same way it does everything else: **label-driven
policies + clusterset membership**, reconciled by GitOps. There is no separate orchestration operator
— the rollout unit is the **clusterset**, and you stage a fleet upgrade by moving clusters between
clustersets in waves, verifying compliance between waves.

> **Why not TALM?** The Topology Aware Lifecycle Manager orchestrates per-cluster policy enforcement,
> but it requires literal upgrade coordinates baked into the root policy and cannot consume
> AutoShift's hub-template labels (it validates the hub-side policy, where `{{hub}}` values are
> unresolved). It's also a maintenance-mode, ZTP-oriented operator. ACM has no native progressive
> *policy* rollout either (the `RolloutStrategy` API is consumed by addons/ManifestWork, not
> Policies — `Policy`/`PlacementBinding` expose no rollout hook). So the staging lever that actually
> fits AutoShift is clusterset membership, controlled by you.

## The `openshift-upgrade` policy

`policies/stable/openshift-upgrade/` renders one `ClusterVersion` policy driven by labels:

- **`spec`** — `upstream` + `channel` + `desiredUpdate.version` from labels. When enforced, this sets
  the desired version and the CVO upgrades.
- **`status.history[].state: Completed`** — the completion gate. The policy is `Compliant` only once
  the upgrade has actually **finished**, so compliance is a trustworthy "this cluster is upgraded"
  signal. (`clusterversions/status` is a subresource, so `enforce` can't write it — it's compare-only.)
- **Semver guard** — a spoke-side `semverCompare` only asserts when `target > current`, so clusters
  already at/above the target are a Compliant no-op and downgrades are never attempted.

### Labels (set on the target clusterset)

| Label | Purpose | Example |
|---|---|---|
| `autoshift.io/openshift-upgrade` | opt the cluster in to Day-2 upgrades | `'true'` |
| `autoshift.io/openshift-version` | target version — shared with operator-channel tooling (upgrades only if `> current`) | `'4.20.28'` |
| `autoshift.io/openshift-upgrade-channel` | ClusterVersion channel | `'stable-4.20'` |
| `autoshift.io/openshift-upgrade-upstream` | OSUS graph (local URL when disconnected) | `https://api.openshift.com/...` |

## Validation is free

Because the policy is a normal ACM policy, you get fleet-wide validation with no extra tooling:

```bash
oc get policy -n policies-autoshift policy-openshift-upgrade \
  -o jsonpath='{range .status.status[*]}{.clustername}{"\t"}{.compliant}{"\n"}{end}'
```

And ArgoCD surfaces it too: OpenShift GitOps ships a health check for `Policy`, so the
`autoshift-openshift-upgrade` **Application is Healthy only when the policy is Compliant**. That's your
"this wave is done, proceed" gate — watch it in the ArgoCD UI or via `oc get application`.

## Rolling out an upgrade (or a new AutoShift version) in waves

The model is **blue/green clustersets** + **wave migration**:

1. **Deploy the new version** as a versioned clusterset (see [gradual-rollout.md](gradual-rollout.md)).
   Its `openshift-version` targets the new OCP version. The clusterset starts empty (or with a
   canary).
2. **Move a canary cluster** into the new clusterset. Its `openshift-upgrade` policy enforces → the
   CVO upgrades it → the policy goes `Compliant` when finished.
3. **Verify** the canary via policy compliance / ArgoCD health.
4. **Move the next wave**, verify, repeat until the fleet is migrated.

**Safety:** blast radius is controlled entirely by membership. **Never enable `openshift-upgrade` on
an already-populated clusterset** — every opted-in cluster in it would upgrade at once (ACM fans the
policy out with no staging). Always move clusters *into* the upgrading clusterset in controlled waves.

### Making the waves Argo-native

Rather than `oc label` by hand, keep clusterset membership **declarative in git** (a values-driven
cluster→clusterset map). A rollout is then a series of **commits** moving N clusters per wave; Argo
reconciles each; the ArgoCD compliance-health above tells you when to commit the next wave;
`git revert` is your rollback. The only thing not automated is "auto-proceed when green" — that single
gate is either your commit cadence or a thin script that reads compliance and moves the next wave.

> *(A declarative clusterset-membership generator is a planned follow-up; today, membership is set via
> `oc label` / cluster-install, and the wave discipline above still applies.)*

## Hub-of-hubs

Each layer's ACM only sees its own clusters, so a fleet upgrade is still **per-layer, top-down**
(hoh → spoke-hubs → leaf spokes): the ACM hub must run a version that supports the clusters it
manages. Apply the `openshift-upgrade` labels on the appropriate clusterset at each layer, and roll
each layer's waves in that order.

## See also

- [gradual-rollout.md](gradual-rollout.md) — versioned-clusterset blue/green migration
- [hub-of-hubs.md](hub-of-hubs.md) — HoH topology and the one-ACM-per-cluster constraint
