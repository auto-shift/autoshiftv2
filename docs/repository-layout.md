# Repository layout

Where to make a given change. This page is organized by task rather than by directory, because the
question people actually have is "where does this edit go", and because a directory listing goes
stale the moment something is renamed.

For what the pieces do rather than where they live, read [Architecture](architecture.md). For how
to build a policy once you know where it goes, read the [Developer guide](developer-guide.md).

## Where a change goes

| To do this | Edit this |
|---|---|
| Add or change a policy | `policies/<tier>/<name>/`, where the tier is `stable`, `certified`, or `community` |
| Change what a whole clusterset gets | `autoshift/values/clustersets/<name>.yaml` |
| Override a single cluster | `autoshift/values/clusters/<name>.yaml` |
| Change something for every deployment | `autoshift/values/global.yaml` |
| Declare a new `autoshift.io/*` label | `autoshift/values/clustersets/_example.yaml`, or a `clusters/_example*.yaml` for a per-cluster label |
| Change how policies are discovered or what values reach them | `autoshift/templates/autoshift-app-set.yaml` |
| Change how an operator gets installed, for every policy | `components/operator-install/` |
| Change what bootstrap installs before AutoShift exists | `advanced-cluster-management/` or `openshift-gitops/` |
| Change validation, or add a stub for a hub resource | `tools/internal/resolver/`, `tools/testdata/` |
| Change a generator or release step | `scripts/`, `Makefile` |
| Change what runs before a commit | `.githooks/pre-commit` |
| Change published documentation | `README.md`, `docs/`, and the policy `README.md` files |

## The parts worth knowing

**`autoshift/`** is the top-level Helm chart, and the only chart a user installs directly. Its
`templates/autoshift-app-set.yaml` is the ApplicationSet that discovers and deploys every policy.
Nothing registers a policy by hand: a directory containing `kustomization.yaml` is rendered by the
PolicyGenerator plugin, and one containing `Chart.yaml` is rendered as Helm.

**`autoshift/values/`** is the whole configuration surface, split so that files compose. Combine
`global.yaml` with one clusterset profile and any per-cluster overrides. The `_example*.yaml` files
are not profiles to deploy: they are the catalog of every available label and config key, and the
validation suite treats them as the contract. A label a policy reads must be declared in one of
them or the build fails.

**`policies/`** holds one policy per directory, in three tiers named for the operator catalog the
policy installs from. See [the policies README](../policies/README.md). Most are PolicyGenerator
directories; four are still Helm charts.

**`components/`** holds charts shared by many policies, currently the operator install interface.
It is a pass-through: the Red Hat Advanced Cluster Management specifics, such as hub templates and
version resolution, belong in the calling policy rather than here.

**`tools/`** is a separate Go module holding the validation suite, so its commands run from inside
that directory rather than from the repository root.

## Generated, not source

These exist in a working checkout but are excluded from version control. Do not edit them, and do
not commit them:

| Path | Where it comes from |
|---|---|
| `.tools/` | `make install-policy-generator` |
| `.vale/styles/RedHat/` | `vale sync`. The vocabulary beside it, under `.vale/styles/config/`, is ours and is tracked |
| `.helm-charts/`, `autoshift/files/`, `release-artifacts/` | the release targets in the `Makefile` |

## Do not edit

`tools/internal/resolver/e2e_test.go` is the validation suite itself. When a policy fails it, fix
the policy or add a stub to `tools/testdata/`. Changing the test to accept broken output removes the
only check that a policy renders and resolves.

Substitution variables such as `${REMEDIATION}` and `${CLUSTER_SET_SUFFIX}` belong in a policy's
`policy-generator-config.yaml` and `placement.yaml`, never in its `manifests/`. The sidecar
substitutes them before `kustomize build` runs, and manifests contain hub template variables that
substitution would overwrite.
