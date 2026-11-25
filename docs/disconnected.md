# Disconnected deployments

AutoShift runs on clusters that cannot reach Red Hat or any other public registry. You provide a
mirror registry and the content in it. AutoShift configures every cluster to use that mirror, serves
the OpenShift update graph from it, and supplies the release signatures that upgrades need.

This page is the entry point. The policy READMEs and the upgrade guide hold the detail it links to.

## What you provide and what AutoShift configures

| You provide | AutoShift configures, from values |
|---|---|
| A mirror registry, reachable from every cluster, running outside the clusters it serves | `ImageDigestMirrorSet` and `ImageTagMirrorSet` redirecting pulls to it |
| The mirror's CA bundle, when its certificate is not publicly trusted | Trust for that CA on every cluster, including the key the OpenShift Update Service reads |
| Mirrored release images, operator catalogs and the update graph image, from oc-mirror v2 | A `CatalogSource` per mirrored catalog, every operator subscription moved to it, the default catalogs turned off |
| Release signatures, from the same oc-mirror run | The signature ConfigMap the Cluster Version Operator verifies upgrades against |
| | An OpenShift Update Service serving the mirrored update graph, and each cluster pointed at it |

Everything in the right column is driven by the `disconnected-mirror` label and `config.disconnected`,
on a clusterset or a single cluster. See the
[disconnected mirror policy](../policies/stable/disconnected-mirror/README.md) for each key.

## The mirror registry

Run the mirror registry outside every cluster that pulls from it. During an upgrade each node
reboots into the new release and must pull images, including the pause image every pod starts from,
before any pod on it can run. A registry hosted on a cluster it serves can be offline exactly when
its own node needs it, and that node never recovers on its own.

Each site can have its own mirror. Mirror every site with the same `ImageSetConfiguration`, so the
releases, digests and signatures match.

## Mirror the content

Generate the `ImageSetConfiguration` from your values files, so it holds exactly the operators your
clusters enable:

```bash
bash scripts/generate-imageset-config.sh \
  autoshift/values/clustersets/hub-disconnected.yaml,autoshift/values/clustersets/managed-disconnected.yaml \
  --output imageset-config.yaml

oc mirror -c imageset-config.yaml --workspace file://mirror docker://registry.example.com:8443 --v2
```

`make generate-imageset` builds the release image set instead, with every operator AutoShift can
install. Either output sets `graph: true`, so oc-mirror builds the graph data image for the
OpenShift Update Service.

The generated file covers what AutoShift installs. Add, before mirroring:

- Operators installed outside AutoShift. With the default catalogs turned off, their pods cannot
  restart unless their images are mirrored.
- Images other workloads pull by tag, as `additionalImages`, with a matching `tagMirrors` entry.
  A digest mirror set does not redirect a pull by tag.
- A `defaultChannel` for each package you filter to a channel other than its catalog default, if
  you edit the file by hand. oc-mirror refuses to filter the catalog without one.

oc-mirror writes the cluster resources it expects to `working-dir/cluster-resources/`. AutoShift
does not apply those files. Print the values they correspond to instead:

```bash
./scripts/oc-mirror-to-values.sh mirror/working-dir/cluster-resources
```

The script prints `releaseSignatures`, one entry per mirrored release, and `config.update-service`,
which is only needed when the mirror does not use oc-mirror's default paths. Set `mirrors` and
`tagMirrors` from `idms-oc-mirror.yaml` and `itms-oc-mirror.yaml`, as a path on the mirror host.

## Install AutoShift without internet access

The two bootstrap charts install Red Hat Advanced Cluster Management and OpenShift GitOps before
AutoShift exists to configure the mirror. On a cluster with no route to the default catalogs:

1. Apply the oc-mirror `cs-*.yaml` catalog, or create a `CatalogSource` for the mirrored index, and
   the ImageDigestMirrorSet and ImageTagMirrorSet oc-mirror wrote, by hand. AutoShift takes these
   over once it runs.
2. Set `source` to that catalog in both bootstrap installs. See the
   [quick start](quickstart.md#installation-from-source).
3. Point AutoShift at a chart source the cluster can reach: a Git server inside the network, or the
   OCI charts copied to the mirror. See [releases and OCI](releases.md#disconnected-and-air-gapped-environments).

## Values

Two example profiles show a complete disconnected layout, one clusterset per file:

| File | Clusterset | What it shows |
|---|---|---|
| `autoshift/values/clustersets/hub-disconnected.yaml` | `hub-disconnected` | A hub with its own site mirror, running the OpenShift Update Service |
| `autoshift/values/clustersets/managed-disconnected.yaml` | `managed-disconnected` | Spokes at a second site with their own mirror, upgrading from the hub's graph |

Replace the example hosts, CA references and signatures with your own. The keys that matter most:

| Key | Required | Notes |
|---|---|---|
| `disconnected-mirror` label | yes | Places the mirror policies and moves every operator to `<source>-<mirror-catalog-suffix>` |
| `config.disconnected.mirrorRegistry.host` | yes | `host:port`. Every `mirror` value is a path under it |
| `config.disconnected.mirrorRegistry.ca` or `caRef` | when the certificate is private | `caRef` names a ConfigMap on the hub that manages the cluster, created out of band. AutoShift copies the CA to each cluster |
| `config.disconnected.mirrorRegistry.mirrors`, `tagMirrors` | yes | `source` is the full image name, registry included; `mirror` is a path under `host`, never a registry |
| `config.disconnected.catalogs` | yes | Each `source` is the base name the operator policies use, such as `redhat-operators` |
| `config.disconnected.releaseSignatures` | yes | `[]` for none. The list is the whole set: removing an entry removes its signatures from the clusters |
| `update-service` label | on the clusters that serve the graph | Usually the hub only |

Copy each `source` from the left side of oc-mirror's `idms-oc-mirror.yaml` as written, and each
`mirror` from the right side with the host removed. AutoShift adds `host` to every `mirror`, so a
registry written there is doubled. For the same reason, the CA for a spoke site lives on the hub:
`caRef` is read where the policy is placed, and one ConfigMap per site serves every cluster at it.

When spokes use a different mirror from the hub that serves their update graph, the graph names
release images on the hub's mirror. Add a `mirrors` entry from the hub's
`<host>/openshift/release-images` to the spoke site's own path, as `managed-disconnected.yaml` does.
Spokes that share the hub's mirror, or read the graph from a service at their own site, do not need
it.

## Upgrades

A disconnected cluster upgrades the same way a connected one does, through the `openshift-upgrade`
labels. It finds its update graph without configuration, from an OpenShift Update Service on the
cluster itself or on the hub that manages it, and an extra check blocks an upgrade whose target
release has no signature on the cluster. See
[disconnected clusters](ocp-upgrade.md#disconnected-clusters) in the upgrade guide, including where
the service runs in a hub of hubs.

## Common problems

| Symptom | Cause |
|---|---|
| A pod reports `RegistryUnavailable` or `ImagePullBackOff` for an image on a public registry | The image is not mirrored, or it is pulled by tag and has no `tagMirrors` entry |
| An operator subscription reports `ResolutionFailed` after the default catalogs are turned off | The subscription was created outside AutoShift and still names a default catalog |
| `policy-disconnected-mirror-test` is NonCompliant | A mirrored catalog is not `READY`: its index image cannot be pulled, or the cluster does not yet trust the mirror CA |
| `policy-disconnected-release-signatures` is NonCompliant after an upgrade of AutoShift | `releaseSignatures` is not set. Set it, to `[]` if there are none |
| The channel policy fails with "no update graph for this disconnected cluster" | No OpenShift Update Service on the cluster or its hub, and no `upgradeGraphUrl` |
| The channel policy fails with "graphSource is local" or "graphSource is hub" | `graphSource` names a cluster with no OpenShift Update Service Route. Enable `update-service` there, or change `graphSource` |
| `openshift-upgrade-signature` names a digest | The target release has no signature in `releaseSignatures` |
| Nodes stay `NotReady` after rebooting into a new release | The mirror is unreachable, often because it runs on a node of the same cluster |
