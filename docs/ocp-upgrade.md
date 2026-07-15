# OpenShift Fleet Upgrades

AutoShift manages Day-2 OpenShift platform upgrades as a label-driven, GitOps-native capability, with
Red Hat's **Topology Aware Lifecycle Manager (TALM)** as an optional orchestration layer for canaried,
throttled, fleet-wide rollouts.

The capability has two halves that are worth separating:

- **Validation is native to ACM.** The `openshift-upgrade` policy is `inform` and reports `Compliant`
  only once a cluster has *finished* upgrading to the target version. `oc get policy` / the ACM
  compliance dashboard is your fleet-wide "did every cluster make it" view — no TALM required.
- **Orchestration is optional.** TALM drives the `inform` policy to `enforce` in canaried batches
  (canary → halt-on-failure → `maxConcurrency` throttle → pre-cache). Use it for large fleets and
  edge/SNO/disconnected clusters; for a small connected fleet, native label-wave enforcement is enough.

> **`openshift-upgrade` vs `openshift-version`:** these are different. `openshift-version` is consumed
> by `cluster-install` at *provision* time (Hive `ClusterImageSet`). `openshift-upgrade-version` is the
> Day-2 *upgrade target* for a running cluster. Do not conflate them.

## The `openshift-upgrade` policy

`policies/stable/openshift-upgrade/` is **inform-only** — a deliberate exception to the
`${REMEDIATION}` convention, because an OCP upgrade must never blanket-enforce under GitOps (a single
label bump would upgrade the whole fleet at once). It is a **single ClusterVersion policy** carrying
both the upgrade coordinates and the completion gate (the docs §13.6 shape, which TALM requires — it
rejects a ClusterVersion policy that lacks `upstream`/`channel`/`version`):

- **`spec`** — `upstream` + `channel` + `desiredUpdate.version` from labels. This is what TALM drives
  to `enforce` to trigger the upgrade.
- **`status.history[].state: Completed`** — the completion gate, so the policy is `Compliant` only
  once the upgrade has actually **finished**, not just when `desiredUpdate` was set.

Putting `status` in the enforced object is safe: `clusterversions/status` is a **subresource**, so
`enforce` cannot write it via the main resource — it stays **compare-only** even when TALM flips the
binding. status is never actually pushed onto the CVO; it only gates compliance.

Both use **static templates** (hub-template *values* only, no `{{- if }}` control flow). This is a
hard TALM requirement: TALM unmarshals `object-templates-raw` as YAML to inspect the policy, and
Go control-flow isn't valid YAML until rendered — a dynamic template fails validation with
`policy was unable to be unmmarshalled from object-templates-raw`. Version skew is therefore handled
by **cluster selection** — the CGU's `clusters` / `clusterLabelSelectors` picks which clusters to
upgrade — not by an in-policy semver guard. Don't put clusters already at/above the target in the
campaign.

### Labels (set on the target clusterset)

| Label | Purpose | Example |
|---|---|---|
| `autoshift.io/openshift-upgrade` | opt the cluster in | `'true'` |
| `autoshift.io/openshift-upgrade-channel` | ClusterVersion channel | `'stable-4.20'` |
| `autoshift.io/openshift-upgrade-version` | target version (upgrades only if `> current`) | `'4.20.12'` |
| `autoshift.io/openshift-upgrade-upstream` | optional OSUS/upgrade graph (disconnected/edge) | `''` |

## Validate (and optionally upgrade) without TALM

Set the labels above with `openshift-upgrade: 'true'`. The inform policy immediately gives you
fleet-wide validation:

```bash
oc get policy -n policies-autoshift policy-openshift-upgrade \
  -o jsonpath='{range .status.status[*]}{.clustername}{"\t"}{.compliant}{"\n"}{end}'
```

To actually roll the upgrade natively, gate enforcement by rolling clusters into the target label in
waves (relabel a batch, watch compliance, relabel the next). This upgrades *and* validates with no
extra operator — appropriate for small, connected fleets.

## Orchestrate with TALM (recommended for large / edge fleets)

### 1. Install TALM (per hub)

Set `autoshift.io/talm: 'true'` on each **self-managed-hub** clusterset. The
`topology-aware-lifecycle-manager` policy installs TALM on that hub (hub-scoped placement). In
hub-of-hubs this is **per-layer self-install** — every layer installs TALM on its own hub; there is no
detection or cross-layer propagation.

### 2. Author a ClusterGroupUpgrade (one-off, unique name)

CGUs are **one-shot** — a completed CGU will not run again ("you must create a new
ClusterGroupUpgrade CR when you need to update again"). So:

- **Unique name per campaign**, keyed to the target (`cgu-upgrade-4-20-12`).
- **Apply out-of-band** (`oc apply`) — do **not** put it under an ArgoCD Application with `selfHeal`
  (continuous reconcile fights the one-shot lifecycle). Store it in git as a record if you like, but
  don't reconcile it.
- **Prune** completed CGUs periodically; they accumulate as history.

See [`examples/cgu-ocp-upgrade.yaml`](examples/cgu-ocp-upgrade.yaml). Start it by patching
`spec.enable: true` inside your maintenance window:

```bash
oc apply -f docs/examples/cgu-ocp-upgrade.yaml
# ...at the window:
oc -n policies-autoshift patch clustergroupupgrade.ran.openshift.io/cgu-upgrade-4-20-12 \
  --type=merge -p '{"spec":{"enable":true}}'
oc -n policies-autoshift get cgu cgu-upgrade-4-20-12 -o jsonpath='{.status}' | jq
```

TALM upgrades the canary first, waits for the `openshift-upgrade` policy to report `Compliant`
(upgrade finished), then proceeds in batches of `maxConcurrency`, aborting if a batch times out.
`preCaching: true` stages release images before the window — important for SNO / limited bandwidth.

> **Timeouts:** OCP upgrades are slow. Size `remediationStrategy.timeout` in the hours-per-batch
> range, not minutes.

## Hub-of-hubs: one campaign per hub, top-down

Because each ACM only sees its own registered clusters (a hub-of-hubs cannot see a spoke's spokes),
a fleet upgrade is **N campaigns, one per hub** — TALM does not coordinate across layers. Order matters:

1. **hub-of-hubs** upgrades first (its self-managed local-cluster).
2. **spoke hubs** next — the hoh's TALM upgrades `hub1`/`hub2` (they are ManagedClusters on the hoh).
   A spoke hub must be back to `Ready` before it upgrades its own spokes (its AutoShift/TALM is
   disrupted during its own reboot).
3. **leaf spokes** last — each spoke hub's TALM upgrades its spokes.

The ACM hub must run a version that supports the clusters it manages, which is why this is strictly
top-down. Enable `talm: 'true'` and the `openshift-upgrade` labels on the appropriate clusterset at
**each** layer.

## Combined switch + upgrade (blue/green)

A version bump and a policy-set migration often travel together. TALM can do both in one campaign:
`beforeEnable.addClusterLabels` flips a cluster's clusterset from the old AutoShift deployment to the
new one, and `policy-openshift-upgrade` in `managedPolicies` upgrades OCP if the new deployment's
target is higher (a no-op when versions match). See
[`examples/cgu-switch-and-upgrade.yaml`](examples/cgu-switch-and-upgrade.yaml).

**Prefer sequential CGUs for now** — upgrade in place first, then switch — because the new
deployment's enforce-by-default operator policies fire on clusterset join, concurrent with the
TALM-gated upgrade. The combined form becomes clean once operator policies are inform-baseline (a
future enhancement).

## See also

- [gradual-rollout.md](gradual-rollout.md) — versioned-clusterset blue/green migration
- [hub-of-hubs.md](hub-of-hubs.md) — HoH topology and the one-ACM-per-cluster constraint
