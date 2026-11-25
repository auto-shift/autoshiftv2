# Disconnected mirror

Points a cluster at a mirror registry: mirrored operator catalogs, image pull redirection, trust for
the mirror's certificate, release signatures, and the default OperatorHub catalogs turned off. It does not mirror
anything. Mirror the content first, for example with `oc mirror` and the `ImageSetConfiguration`
that `make generate-imageset` writes. The end-to-end flow is in
[disconnected deployments](../../../docs/disconnected.md).

## What it deploys

| Policy | Creates |
|---|---|
| `policy-disconnected-catalog-sources` | A `CatalogSource` named `<source>-<mirror-catalog-suffix>` in `openshift-marketplace` for each entry in `catalogs` |
| `policy-disconnected-image-mirrors` | `ImageDigestMirrorSet autoshift-disconnected-mirrors` from `mirrors` (an `ImageContentSourcePolicy` when `useIDMS` is false) and `ImageTagMirrorSet autoshift-disconnected-tag-mirrors` from `tagMirrors` |
| `policy-disconnected-registry-ca` | ConfigMap `openshift-config/autoshift-registry-ca` and `image.config.openshift.io/cluster` `spec.additionalTrustedCA` pointing at it. The CA is written under the registry host and under `updateservice-registry`, the key the OpenShift Update Service reads |
| `policy-disconnected-release-signatures` | ConfigMap `openshift-config-managed/mirrored-release-signatures` from `releaseSignatures`, so the Cluster Version Operator can verify an update. Deleted when the list is empty |
| `policy-disconnected-disable-catalogs` | `OperatorHub cluster` with `disableAllDefaultSources: true`, when `disableDefaultCatalogs` is true |
| `policy-disconnected-mirror-test` | Inform only. Reports a configured mirror `CatalogSource` that is not `READY`. Renders nothing when no catalogs are configured |

Both mirror sets use `mustonlyhave`, so removing an entry from values removes it from the cluster.
A change to either set rolls the `MachineConfigPool`s.

## Placement

Every cluster labeled `autoshift.io/disconnected-mirror: 'true'`.

## Labels

| Label | Default | Description |
|---|---|---|
| `autoshift.io/disconnected-mirror` | `'false'` | Places these policies, and switches every operator's catalog source to `<source>-<suffix>` |
| `autoshift.io/mirror-catalog-suffix` | `mirror` | Suffix for the mirrored `CatalogSource` names, for example `redhat-operators-mirror` |

The operator policies read these labels directly, because an `OperatorPolicy` cannot read the
rendered configuration. Each `catalogs[].source` must therefore be the base name the operator
policies expect, usually `redhat-operators` or `certified-operators`.

`catalogs` is required wherever `disconnected-mirror` is `'true'`: on the cluster, its clusterset or
the global config. The values validator fails `helm template` otherwise, because only `catalogs`
creates the `<source>-<suffix>` catalogs that the operator Subscriptions use.

## Config

Read from `config.disconnected` in the cluster's rendered-config ConfigMap. The policies fail when
that ConfigMap is missing rather than reporting Compliant.

```yaml
config:
  disconnected:
    mirrorRegistry:
      host: 'registry.example.com:5000'
      path: 'openshift'                # optional; used when an entry has no mirror
      caRef:                           # or ca: with an inline bundle
        name: 'cluster-ca-bundle'
        key: 'ca-bundle.crt'
        namespace: 'cluster-install-secrets'
      mirrors:                         # ImageDigestMirrorSet, digest pulls
        - source: 'registry.redhat.io'
          mirror: 'rhel'               # a path on host, never a full registry URL
      tagMirrors:                      # ImageTagMirrorSet, tag pulls
        - source: 'docker.io/hashicorp'
          mirror: 'hashicorp'
    catalogs:
      - source: redhat-operators
        imagePath: redhat/redhat-operator-index
        tag: v4.22                     # match the cluster's OpenShift minor
        priority: 10                   # optional; OLM prefers the higher value when resolving dependencies
    useIDMS: true                      # default true; false writes an ImageContentSourcePolicy instead
    disableDefaultCatalogs: true       # default false
    releaseSignatures:                 # required; [] for none
      - version: '4.22.18'             # optional, for people reading the list
        digest: 'sha256:<64 hex characters>'
        signatures:
          - '<base64 signature>'
```

`releaseSignatures` comes from the `signature-configmap.json` oc-mirror writes to
`working-dir/cluster-resources/`. `scripts/oc-mirror-to-values.sh` prints it as list entries. The
policy owns the ConfigMap with `mustonlyhave`: removing an entry removes its signatures from the
cluster, and signatures applied by hand are removed too. Keep an entry for every release a cluster
might still move to. See [Disconnected clusters](../../../docs/ocp-upgrade.md#disconnected-clusters).

Each `mirrors` and `tagMirrors` entry pairs a full image name with a path on the mirror:

| Field | Contains a registry | What it is |
|---|---|---|
| `source` | Always | The image name a cluster pulls, as written in the image reference. Copy it from the left side of oc-mirror's `idms-oc-mirror.yaml` |
| `mirror` | Never | A path under `host`. Every consumer prefixes `host`, so writing the registry here produces it twice |
| `host` | Is the registry | Written once |

A `source` can be a registry you own. A spoke that reads its update graph from the hub's OpenShift
Update Service is told to pull releases from the hub's mirror, so that name is its `source`.

Set `ca` or `caRef` only when the mirror's certificate is not publicly trusted. Without either,
`policy-disconnected-registry-ca` applies nothing. A `caRef` names a ConfigMap on the hub that
places the policy, never on the managed cluster: the policy reads it there and copies the CA to
each cluster. Its namespace defaults to the policy namespace, and the policy reads any other
namespace through the AutoShift policy service account.

## Before you disable the default catalogs

Disabling the default catalogs strands any `Subscription` that still names one, including operators
installed outside AutoShift. Those report `ResolutionFailed` and stop receiving updates. Check with:

```console
oc get sub -A -o custom-columns=NAME:.metadata.name,SOURCE:.spec.source
```
