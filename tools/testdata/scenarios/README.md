# Scenarios

The end-to-end suite resolves every policy against the values the example files declare, so each
label is tested with one value. A scenario adds another: a deployment of its own, written as the
values file a user would write, with a statement of what it must render.

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
  values.yaml     a values file, layered on autoshift/values/clustersets/_example.yaml
  scenario.yaml   what the deployment must render
  stubs/*.yaml    optional cluster objects the policies look up
```

## How it runs

1. The top-level chart renders `global.yaml`, `_example.yaml` and `values.yaml` together, exactly as
   an Argo CD Application renders a per-cluster override. Its validation templates run.
2. The labels and config in `values.yaml` are applied over the example's. Clusterset config is the
   base and cluster config is merged over it, as in a cluster's rendered-config ConfigMap.
3. Stubs come from `tools/testdata/`, minus any listed under `drop`, with the scenario's `stubs/`
   replacing any object of the same kind, namespace and name.
4. Every policy chart resolves against that, with the same hard failures as the end-to-end suite:
   render errors, hub and spoke resolution errors, and invalid resolved YAML.
5. A `ConfigurationPolicy` in one of the scenario's `policies` that applies nothing fails. It would
   report Compliant while enforcing nothing. There is no allowlist.
6. Each `expect` entry is checked against the objects the resolved policies would apply.

Scenarios resolve against the hub profile. To model a managed cluster, set `self-managed: 'false'`
in the scenario's labels.

## values.yaml

Use the same shape as a file under `autoshift/values/`. Set only what differs from the example:

```yaml
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

As in any values file, no credentials. Put them in `stubs/`.

## scenario.yaml

```yaml
description: >-
  What deployment this is and what it guards. Required.

# The policies this scenario is about. Every chart resolves either way. These are the ones
# whose ConfigurationPolicies must not render empty, and each must exist.
policies:
  - stable/quay

# Stubs to remove from tools/testdata/ for this scenario. Each must match one.
drop:
  - {kind: Secret, namespace: quay-enterprise, name: quay-db-app}

expect:
  - policy: policy-quay-deploy    # the enclosing Policy; optional
    kind: Secret                  # required
    namespace: quay-enterprise    # optional
    name: quay-registry-config-bundle  # optional
    # count: 1                    # exact number of matches; omitted means at least one,
    #                             # and count: 0 asserts the object is not rendered
    fields:                       # checked on every match
      - path: stringData["clair-config.yaml"]
        contains: host=external-clair-pg
      - path: spec.components[kind=postgres].managed
        equals: false
```

Unknown keys fail the load, so a misspelled `equal:` cannot assert nothing.

An expectation is matched against every object a policy would apply: each top-level document, each
policy template, and each object in a `ConfigurationPolicy`'s `object-templates` or
`object-templates-raw`.

### Paths

| Form | Meaning |
|---|---|
| `spec.replicas` | Map keys, separated by dots |
| `spec.containers[0]` | List index |
| `spec.components[kind=postgres]` | The list entry whose `kind` is `postgres`. It must be the only one, so a duplicated entry fails |
| `stringData["clair-config.yaml"]` | A key that contains dots |

### Field checks

Each entry has a `path` and exactly one of:

| Check | Passes when |
|---|---|
| `equals: <value>` | The value is present and equal, including type: `false` is not `'false'` |
| `contains: <text>` | The value is a string containing the text |
| `notContains: <text>` | The value is a string without the text. A missing value fails |
| `present: true` | The path exists |
| `absent: true` | The path does not exist |

## stubs/

Cluster objects the policies look up, in the format of `tools/testdata/`. A stub replaces the
object of the same kind, namespace and name there. Secrets may use `stringData`, which is encoded to
`data` before the policies read it, so stubs stay readable.

`tools/testdata/` is not allowlisted in `.gitleaks.toml`, so use low-entropy placeholder values such
as `testtest`.

## Proving a scenario works

A scenario that passes against broken code proves nothing. After writing one, break the code it
guards, confirm the scenario fails with a message that names the problem, and put the code back.
