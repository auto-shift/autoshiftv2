# Disconnected mirror

Points a cluster at a mirror registry: mirrored operator catalogs, image pull redirection, trust for
the mirror's certificate, and the default OperatorHub catalogs turned off. It does not mirror
anything. Mirror the content first, for example with `oc mirror` and the `ImageSetConfiguration`
that `make generate-imageset` writes.

## What it deploys

| Policy | Creates |
|---|---|
| `policy-disconnected-catalog-sources` | A `CatalogSource` named `<source>-<mirror-catalog-suffix>` in `openshift-marketplace` for each entry in `catalogs` |
| `policy-disconnected-image-mirrors` | `ImageDigestMirrorSet autoshift-disconnected-mirrors` from `mirrors` (an `ImageContentSourcePolicy` when `useIDMS` is false) and `ImageTagMirrorSet autoshift-disconnected-tag-mirrors` from `tagMirrors` |
| `policy-disconnected-registry-ca` | ConfigMap `openshift-config/autoshift-registry-ca` and `image.config.openshift.io/cluster` `spec.additionalTrustedCA` pointing at it |
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
    useIDMS: true
    disableDefaultCatalogs: true
```

`mirror` is a path that every consumer prefixes with `host`. Writing the full registry there
produces the host twice.

Set `ca` or `caRef` only when the mirror's certificate is not publicly trusted. Without either,
`policy-disconnected-registry-ca` applies nothing. A `caRef` is read from the hub, with the
namespace defaulting to the policy namespace.

## Before you disable the default catalogs

Disabling the default catalogs strands any `Subscription` that still names one, including operators
installed outside AutoShift. Those report `ResolutionFailed` and stop receiving updates. Check with:

```console
oc get sub -A -o custom-columns=NAME:.metadata.name,SOURCE:.spec.source
```

## Testing against a connected hub

`autoshift/values/clusters/local-cluster-disconnected-test.yaml` exercises every policy here on a
connected hub, using `registry.redhat.io` as the mirror. Its mirror entries redirect a name that
does not resolve, so a pull through that name succeeds only when the mirror set works.
