# AutoShift Console Plugin

Deploys the AutoShift OpenShift console dynamic plugin — a read-only **AutoShift** section in the
**Fleet management** perspective (Fleet / Cluster Sets / Clusters / Stacks) that surfaces AutoShift's
clustersets, resolved configuration and OpenShift version tracking. It sits alongside Red Hat Advanced
Cluster Management's own views, after Governance.

Plugin source lives in a separate repository: `auto-shift/autoshift-console-plugin`.

> Custom console plugin code is not supported by Red Hat — only Cooperative community support is
> available. That is why this policy sits in the `community/` tier.

## What it deploys

| Resource | Purpose |
|---|---|
| `Namespace autoshift-console` | Plugin namespace, `pod-security.kubernetes.io/enforce: restricted` |
| `ConfigMap autoshift-console-nginx-conf` | nginx TLS listener on 9443 serving the plugin assets |
| `Deployment autoshift-console` | Serves the webpack module-federation bundle |
| `Service autoshift-console` | ClusterIP 9443; service-ca mints `autoshift-console-cert` |
| `ConsolePlugin autoshift-console` | Registers the plugin with the console |
| `Console cluster` (patch) | Appends `autoshift-console` to `spec.plugins` |

The `Console` patch uses `complianceType: musthave`, which **appends** to `spec.plugins`.
`mustonlyhave` would evict every other console plugin on the cluster, including ACM's — do not change it.

`policy-autoshift-console-enable` depends on `policy-autoshift-console`, so the console is never
pointed at a plugin whose Service does not exist yet.

`policy-autoshift-console-available` is inform-only and reports when the plugin has no running pods.
It exists because the other two cannot see that: policy 1 asserts the Deployment **object**, which a
Deployment stuck in `ImagePullBackOff` still satisfies, and policy 2 deliberately goes quiet until
pods are up. Without it a failed pull leaves every policy Compliant and the console simply empty.

Compatibility is enforced by the image tag rather than a configured floor. The tag names the
OpenShift minor it targets, so the policy derives it from the cluster's actual running version in
`ClusterVersion` history, not the desired `openshift-version` label. A cluster therefore always
pulls its own build, and a minor with no published build fails the pull instead of running a pod
that renders nothing. Publishing a new tag is what makes a minor supported; there is no floor to
keep in step with the plugin's `package.json`.

The report is a Deployment that nothing creates, carrying only `metadata`; the wording comes from
`customMessage`, not the object. Two properties of it are load-bearing, both verified on a live hub.
It is **unconstructible**: a Deployment without `spec.selector` is rejected as invalid, so
remediation cannot satisfy it and nothing is created. And it is **never produced by any policy**, so
it cannot drift into existence.

Both matter because the root `Policy` remediationAction overrides the child `ConfigurationPolicy`,
which means one Enforce click in the console reaches this policy. Under that override a `mustnothave`
sentinel on a cluster singleton such as `ClusterVersion` becomes a delete, and a `musthave` on a
normal absent object becomes a create. Asserting a real resource does not work either: the plugin
Deployment and its namespace both outlive the condition that produced them, because removing a
policy removes nothing.

One caveat this design accepts: a failed pull cannot distinguish an unpublished minor from an image
that was never mirrored into a disconnected registry. The message names both.

## Placement

Hub-only, opt-in. The Placement requires **both**:

- `autoshift.io/cluster-type: 'hub'` — the plugin reads the cluster-labels, cluster-config and
  rendered-config ConfigMaps, which only exist where an AutoShift instance runs (this includes spoke
  hubs in a hub-of-hubs topology; each shows only the clusters its own ACM can see).
- `autoshift.io/autoshift-console: 'true'`

## Labels

| Label | Default | Description |
|---|---|---|
| `autoshift.io/autoshift-console` | — | Set `'true'` to deploy the plugin |

## Config

Read from `config.autoshiftConsole` in the cluster's rendered-config ConfigMap. All keys optional.

```yaml
hubClusterSets:
  hub:
    config:
      autoshiftConsole:
        repository: 'quay.io/autoshift/autoshift-console-plugin'
        replicas: 2
    labels:
      autoshift-console: 'true'
```

The tag is appended as `:ocp<minor>` from the cluster's running version, so `repository` is all most
deployments set. A cluster whose minor has no published build fails the pull, so check which minors
are tagged before enabling the label on a clusterset:
[quay.io/autoshift/autoshift-console-plugin](https://quay.io/repository/autoshift/autoshift-console-plugin?tab=tags). An explicit `image` overrides both and is how a digest gets pinned, but it applies
verbatim to every cluster the clusterset covers: pinning one tag across a fleet spanning minors
deploys the wrong bundle to all but one of them. Pin per clusterset, or per cluster.

In a disconnected environment the image is redirected by
the cluster-wide `ImageDigestMirrorSet` the `disconnected-mirror` policy manages — no per-policy
mirror suffix is involved, since this is a plain image rather than an OLM catalog source.

## Stack grouping

The plugin discovers components at runtime from ArgoCD Applications, Placements, PlacementDecisions
and Policies, so a newly added AutoShift policy appears with no plugin rebuild. The only thing it
cannot derive is which components form an application stack. That resolves in order:

1. `ConfigMap autoshift-console-catalog` in the `autoshift-console` namespace, if an admin creates one
2. the default catalog baked into the plugin image
3. auto-grouping by config key, with anything unmatched under **Other**

## Verification

```bash
oc get policy -n policies-autoshift | grep autoshift-console
oc get consoleplugin autoshift-console
oc get pods -n autoshift-console
# must still list ACM's plugins alongside autoshift-console
oc get console.operator.openshift.io cluster -o jsonpath='{.spec.plugins}'
```

Then reload the console: an **AutoShift** section appears in the **Fleet management** perspective.
