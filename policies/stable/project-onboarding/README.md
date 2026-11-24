# project-onboarding

TODO: Describe what this directory configures, and the `autoshift.io/` labels and `config.` keys it
reads.

| Policy | What it does |
|---|---|
| `policy-project-onboarding` | TODO: the resource `manifests/project-onboarding.yaml` manages |

## Placement

Every cluster, hub or managed, with the label `autoshift.io/project-onboarding: 'true'`.
Set the label in the values files, never directly on a cluster.

`placement.yaml` holds the predicate. A policy added later with a different predicate gets its own
`placement-<policy>.yaml`.

## Layout

```
project-onboarding/
  kustomization.yaml            # entry point: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator: policy graph, remediation, evaluation interval
  placement.yaml                # Placement predicate and tolerations
  manifests/
    project-onboarding.yaml        # bare resource; PolicyGenerator wraps it in a ConfigurationPolicy
```

Write each manifest as the bare resource. PolicyGenerator generates the `ConfigurationPolicy` around
it. A resource that needs hub templates, loops, or conditionals goes in an `object-templates-raw`
manifest instead.

To add a policy to this directory, run the generator again with the same `--dir`, then add a row to
the policy table:

```bash
./scripts/generate-policy.sh <name> --dir policies/stable/project-onboarding --target <hub|spoke|both|all>
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/project-onboarding

# Full validation: render, hub and spoke template resolution, label contract
make test
```
