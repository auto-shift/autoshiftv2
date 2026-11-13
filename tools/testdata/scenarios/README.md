# Scenarios

The end-to-end suite resolves every policy against the values the example files declare, so each
label is tested with one value. A scenario adds another: a deployment of its own, written as input
values, a fake cluster, and the output that deployment must render.

`TestScenarios` in `tools/internal/resolver/scenario_test.go` runs every directory here as part of
`make test`. Adding coverage is a new directory, with no Go changes.

## When to add one

Add a scenario when a policy branches on a label or config value that `_example.yaml` does not
select. Mode labels are the usual case: `quay-db-mode` has three values, and the example can hold
only one of them.

Prefer declaring a key in `_example.yaml` when the example can hold it without displacing another
value. A scenario is for the values it cannot hold at the same time.

## Layout

```text
scenarios/<name>/
  values.yaml                      input: a values file, layered on _example.yaml
  cluster/*.yaml                   optional: objects already on the fake cluster
  expected/*.yaml                  output: objects any policy must render
  expected/<policy-name>/*.yaml    output: objects that Policy must render
  absent/*.yaml                    optional: objects no policy may render
  absent/<policy-name>/*.yaml      optional: objects that Policy may not render
```

## How it runs

1. The top-level chart renders `global.yaml`, `_example.yaml` and `values.yaml` together, exactly as
   an Argo CD Application renders a per-cluster override. Its validation templates run.
2. The labels and config in `values.yaml` are applied over the example's. Clusterset config is the
   base and cluster config is merged over it, as in a cluster's rendered-config ConfigMap. A cluster
   label overrides the same clusterset label.
3. The fake cluster is `tools/testdata/`, with the scenario's `cluster/` objects replacing any object
   of the same kind, namespace and name.
4. Every policy chart resolves against that, with the same hard failures as the end-to-end suite:
   render errors, hub and spoke resolution errors, and invalid resolved YAML.
5. Each Placement is evaluated against the scenario's labels. A Policy whose Placement does not
   select the cluster never reaches it, so its objects are left out of every check below. A Policy
   no PlacementBinding names counts as placed.
6. Each object in `expected/` is compared with the objects the placed policies would apply, and no
   object in `absent/` may be among them.
7. In each chart an expected object came from, a placed `ConfigurationPolicy` that applies nothing
   fails. It would report Compliant while enforcing nothing. There is no allowlist.

Scenarios resolve against the hub profile. To model a managed cluster, set `self-managed: 'false'`
in the scenario's labels.

## values.yaml

Use the same shape as a file under `autoshift/values/`, and set only what differs from the example.
Open the file with a comment saying what deployment this is and what it guards. The comment is
required, because a scenario nobody can explain is one nobody can safely change.

```yaml
# Red Hat Quay on databases run outside the cluster. The credentials come from the
# quay-config-extra Secret in cluster/.
clusters:
  quay-external-db:
    labels:
      quay: 'true'
      quay-db-mode: 'external'
    config:
      quay:
        configSecretRef:
          name: quay-config-extra
```

`hubClusterSets`, `managedClusterSets` and one entry under `clusters` are supported, each with
`labels` and `config`. Anything else fails the load, because the chart would render it but the
policy pass would not read it, and the scenario would only appear to test it.

A label that no `_example*.yaml` declares fails the load, so a misspelled label cannot quietly test
the default. Quote label values: labels are strings.

As in any values file, no credentials. Put them in `cluster/`.

## cluster/

Objects the policies look up, in the format of `tools/testdata/`. Secrets may use `stringData`,
which is encoded to `data` before the policies read it, so the files stay readable.

`tools/testdata/` is not allowlisted in `.gitleaks.toml`, so use low-entropy placeholder values such
as `testtest`.

## expected/

Each document is part of an object the deployment must render. Write only the fields that matter.
`kind` and `metadata.name` are required and select the rendered object, with `metadata.namespace`
when it is set. A file under `expected/<policy-name>/` matches only objects that Policy applies,
which matters when two policies render the same object, as the managed and bundled Quay policies do.

```yaml
apiVersion: quay.redhat.com/v1
kind: QuayRegistry
metadata:
  name: registry
  namespace: quay-enterprise
spec:
  components:
    - kind: postgres
      managed: false
```

At least one rendered object must be selected, and every selected one must contain the expected
object. Containment follows ACM `musthave`, with three additions for tests:

| Expected | Passes when the rendered object |
|---|---|
| A map | Has every key, each containing the expected value |
| A list | Has, for each expected entry, some entry that contains it, in any order |
| A scalar | Has the same value and type: `false` is not `'false'` |
| `{}` | Has the key, with any map |
| `null` | Does not have the key |
| A map, where the rendered value is a string | Holds YAML that contains the map, so an embedded file such as a Secret's `config.yaml` is checked by key |

A Secret's `data` values are base64-decoded before the comparison, so expected values are written as
text. Every policy template and every object in a `ConfigurationPolicy`'s `object-templates` or
`object-templates-raw` is compared. A resolved `object-templates-raw` block that does not parse as
YAML fails the scenario.

A failure names the expected file, the rendered object, and each difference by path, such as
`data.ssl.cert: expected absent, found testtest`.

## absent/

Objects the deployment must not render, in the same layout as `expected/`. Only `kind`,
`metadata.name` and `metadata.namespace` select; other fields are ignored. Any placed policy that
renders a matching object fails the scenario, or, under `absent/<policy-name>/`, only that Policy.

Use it for what a mode must omit, such as the CloudNativePG clusters in external mode, or for
two policies that must not both write one object.

## Proving a scenario works

A scenario that passes against broken code proves nothing. After writing one, break the code it
guards, confirm the scenario fails with a message that names the problem, and put the code back.
