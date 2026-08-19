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

The two policies are split so the console is never pointed at a plugin whose Service does not exist
yet: `policy-autoshift-console-enable` depends on `policy-autoshift-console`.

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

Read from `config.autoshiftConsole` in the cluster's rendered-config ConfigMap. Both keys optional.

```yaml
hubClusterSets:
  hub:
    config:
      autoshiftConsole:
        image: 'quay.io/autoshift/autoshift-console-plugin:ocp4.22'
        replicas: 2
    labels:
      autoshift-console: 'true'
```

Pin `image` to a released tag in production. In a disconnected environment the image is redirected by
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
