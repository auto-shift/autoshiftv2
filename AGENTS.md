# AGENTS.md

Instructions for coding agents working in this repository. Applies to any agent; nothing here is
tool-specific. Keep this file under 200 lines. Detail belongs in `docs/`, linked from here.

## What AutoShift is

An Infrastructure-as-Code framework for configuring OpenShift clusters at scale. It deploys Red Hat
Advanced Cluster Management policies through OpenShift GitOps to run Day 2 configuration across a
hub and its managed clusters. Read [docs/architecture.md](docs/architecture.md) before making
structural changes.

## Repository layout

| Path | What it is |
|---|---|
| `autoshift/` | Top-level Helm chart. `templates/autoshift-app-set.yaml` is the ApplicationSet that deploys every policy |
| `autoshift/values/global.yaml` | Shared config, always included first |
| `autoshift/values/clustersets/` | Per-clusterset profiles (`hub.yaml`, `managed.yaml`, `hubofhubs.yaml`, ...) plus `_example.yaml`, the full label catalog |
| `autoshift/values/clusters/` | Per-cluster overrides plus `_example*.yaml` reference files |
| `policies/{stable,certified,community}/<name>/` | One policy per directory, auto-discovered |
| `components/operator-install/` | Shared source-agnostic operator install interface |
| `advanced-cluster-management/`, `openshift-gitops/` | Bootstrap charts, installed before AutoShift itself |
| `tools/` | Go module holding the policy validation suite |
| `scripts/` | Generators and release tooling |
| `docs/` | Published documentation; `mkdocs.yaml` at the repo root configures the site |

Policies are discovered by a glob on a marker file: a directory with `kustomization.yaml` is
rendered by the PolicyGenerator plugin, one with `Chart.yaml` and no kustomization is rendered as
Helm. Adding a directory under `policies/<tier>/` is all that is required. Never register a policy
by hand anywhere. `excludePolicies` in values opts one out.

## Toolchain

Nothing below is vendored. A fresh clone can run none of the gates until these are present.

| Tool | Needed for | Get it |
|---|---|---|
| `helm`, `git`, `yq` | rendering, release tooling | `make validate` checks these three |
| `go` | the validation suite | version comes from `tools/go.mod` |
| kustomize + PolicyGenerator | rendering any PolicyGenerator policy | `make install-policy-generator`, stages both into `.tools/` |
| `gitleaks` | the pre-commit secret scan | the hook warns and continues when absent |
| `vale` | the prose gate | in the devcontainer; run `vale sync`, the rules are not in the repo |
| `zensical` | the docs build gate | in the devcontainer; else `pip install -r docs/requirements.txt` |
| `oc` | anything against a live cluster | not needed to render or validate |

The devcontainer carries all of it, and its `postCreateCommand` runs `make install-policy-generator`
and points `core.hooksPath` at `.githooks`. Outside it, run both of those yourself, or the
pre-commit hook never fires and PolicyGenerator policies fail to render.

Versions are pinned exactly and bumped by Dependabot or Renovate. When adding one, put it in a
manifest or give it a `# renovate:` annotation: a version inline in a `run:` step is frozen forever.

## Commands

```bash
make install-policy-generator   # one-time: kustomize + PolicyGenerator plugin into .tools/
make test                       # the validation suite, exactly what CI runs (~30s)
make verify                     # make test + helm lint + prose lint + docs build
```

`make test` is the one command after changing `policies/`, `autoshift/values/` or `tools/`. It wraps
`cd tools && go test -tags integration ./...` and matches the CI job by construction, so there is
nothing to remember about which checks CI adds. The pre-commit hook runs the same suite, plus
gitleaks and helm lint on changed charts. `make verify` adds every chart's helm lint, Vale over the
prose and the documentation build; Vale and Zensical live in the devcontainer, and without them
those two steps report skipped rather than failing.

- `helm template` release names must be 11 characters or fewer. The default `release-name` produces
  a 21-character policy namespace and trips the naming validator.
- The top-level chart is covered: `TestAutoShiftChart_ValuesProfiles` renders it against every
  clusterset profile and layers every `clusters/_example*.yaml` override onto `_example.yaml`, which
  is the only thing that executes `autoshift/templates/_validate-*.tpl`. Rendering it by hand is for
  inspecting output, not for coverage.

## Non-negotiables

- **Labels live in values files only.** Never put an `autoshift.io/*` label directly on a
  ManagedCluster, and never tell a user to. The `cluster-labels` policy renders values into
  ConfigMaps and stamps them onto each cluster at runtime. See
  [docs/config-and-labels.md](docs/config-and-labels.md).
- **Every new `autoshift.io/<key>` must be declared** in `autoshift/values/clustersets/_example.yaml`
  or a `clusters/_example*.yaml`. The label contract check fails the build otherwise, and names the
  policy that consumed the undeclared label.
- **Never hard code policy counts** in documentation. Policies are added constantly. Write "policies",
  not a number.
- **No credentials in values files.** Reference a Secret created out of band through
  `configSecretRef`. Stubs in `tools/testdata/` must use low-entropy fake values, because that
  directory is deliberately not allowlisted in `.gitleaks.toml`.
- **Keep the `evaluationInterval` block** on every ConfigurationPolicy, with both `compliant` and
  `noncompliant`. The default is `watch`, and the value must be a literal rather than a hub template.
- **New and changed policies must fail loudly when required input is missing**, rather than mask a
  required lookup with `| default dict` or render nothing: an empty `ConfigurationPolicy` reports
  Compliant. Most existing policies still mask it. [Failing on missing input](docs/policy-behavior.md#failing-on-missing-input).
- **Do not edit `tools/internal/resolver/e2e_test.go`** to make a policy pass. Fix the policy, or add
  a testdata stub.
- **Never run an SVG optimizer over `docs/diagrams/`.** Each `.drawio.svg` carries its own editable
  source in an attribute that optimizers strip. The picture survives, the source does not, and the
  loss is invisible until someone tries to edit.

## Authoring a policy

Scaffold first, then customize. Do not hand-write a policy directory.

```bash
./scripts/generate-operator-policy.sh <name> <package> --channel stable --namespace <ns>
./scripts/generate-policy.sh <name>          # non-operator configuration
```

Most policies are PolicyGenerator directories:

```
policies/<tier>/<name>/
  policy-generator-config.yaml   # the policy graph: names, dependencies, remediation
  kustomization.yaml
  placement.yaml                 # authored in full; PolicyGenerator emits only the PlacementBinding
  manifests/                     # a DIRECTORY path, new files are picked up automatically
  test/                          # inform-only compliance assertions
  README.md
```

Four Helm holdouts remain and keep the older chart shape: `cluster-config-maps`, `cluster-labels`,
`openshift-gitops`, `policy-foundation`.

Conventions:

- Policy `policy-<component>-operator-install`, placement
  `placement-policy-<component>-operator-install`.
- Standard operator labels: `<component>`, `<component>-subscription-name`, `<component>-channel`,
  `<component>-source`, `<component>-source-namespace`, `<component>-version`.
- `${POLICY_NAMESPACE}`, `${REMEDIATION}`, `${EVAL_COMPLIANT}`, `${EVAL_NONCOMPLIANT}` and
  `${CLUSTER_SET_SUFFIX}` are substituted by the repo-server sidecar before `kustomize build` runs.
  They belong in `policy-generator-config.yaml` and `placement.yaml`, never in `manifests/`, whose
  hub templates contain `$vars` that substitution would clobber.
- An existence check or readiness assertion belongs in `test/` as its own inform policy, never
  bundled into an enforcing one. A root `remediationAction` overrides its children.
- New policy directories need a `README.md`.

The full walkthrough, from researching an operator to deploying it, is in the
[developer guide](docs/developer-guide.md).

## Hub templates

Hub templates (`{{hub ... hub}}`) resolve on the hub before distribution; regular `{{ ... }}`
resolve on the managed cluster. Both must be escaped inside a Helm chart:

```yaml
'{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/some-label" | default "value" {{ "hub}}" }}'
```

Three rules cause most of the breakage:

1. Inside a YAML block scalar, a `{{-` directive must sit at the **same indentation** as the content
   lines around it. A shallower indent trims past the newline into the previous line and produces
   invalid YAML.
2. Any expression producing multiple lines must be piped through `autoindent`. Plain `toYaml`
   outputs at column 0, which terminates the enclosing block scalar.
3. Blank lines inside a block scalar must carry spaces to the block's indentation, or Kubernetes
   re-serializes `|` as `>` and merges the lines. Inside `object-templates-raw` that padding instead
   forces the whole block into a quoted scalar and breaks hub resolution, so use a `#` line there.

Hub templates do not support Go comments; `{{hub /* ... */ hub}}` is a parse error. Use `{{/* */}}`
only in `object-templates-raw`, written as exactly `{{- /*` with one space.

Full pitfalls and function guidance: [developer guide](docs/developer-guide.md#hub-template-pitfalls).
Runtime behavior the validation suite cannot catch, including the `musthave` merge and create
semantics and the version-dependent Sprig function set (2.15 and earlier lack `trimPrefix`,
`trimSuffix`, `compact`, and `toString`; 2.16 and later have them):
[docs/policy-behavior.md](docs/policy-behavior.md). Read that page before writing a policy that
depends on overriding existing state.

## Validation suite

`make test` runs the end-to-end pipeline over every policy chart, the top-level chart against every
values profile, a mutation sweep that introduces a deliberate defect per case and asserts the
pipeline reports it, a check that no `ConfigurationPolicy` applies nothing in every profile (one with
nothing to apply reports Compliant; exemptions in `.github/empty-policy-allowlist.yaml`), and the
unit tests. Every policy chart is resolved against five
cluster profiles, the primary hub plus one per
`autoshift/values/clusters/_example-cluster-install-*.yaml` file. Dropping in a new variant file adds
a profile with no test-code change.

**The suite renders only what the example values declare.** A branch gated on a config key that no
`_example*.yaml` sets never renders, so it is never checked. Declare the key rather than leaving it
commented out when you add a branch that reads a Secret, a ConfigMap or a trust bundle. Real bugs
have hidden in commented-out branches while every gate passed.

It validates rendering and resolution, not enforcement semantics, and it cannot model policy
dependencies: every policy resolves regardless of `dependencies`, so a policy reading an object that
another policy creates needs that object stubbed in `tools/testdata/`. It does not cover
multi-cluster topology such as the hub-of-hubs `managedHub` target.
`.github/label-lint-allowlist.yaml` exempts intentional label deviations. A chart calling
`fromSecret` or `fromConfigMap` against a real hub resource needs a stub in `tools/testdata/`.
Full breakdown: [developer guide](docs/developer-guide.md#what-the-suite-covers).

## Documentation

`README.md`, `AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md`, `docs/` and every `policies/*/*/*.md` are
linted with Red Hat's Vale style, and the build fails on error-level findings. `make verify` runs the
same scope as CI. Before committing documentation, read
[docs/documentation-style.md](docs/documentation-style.md). The rules that catch people most often:
full `Red Hat` product names, no em dashes, no contractions, and the banned-term list, which
includes `IPI`, `hardcoded`, `vs`, and `a number of`.

Never run a scripted find and replace over prose without matching whole terms, excluding code
blocks, and reading the diff. It has corrupted this documentation set repeatedly.

Diagram authoring, including the house style and the color palette, is in
[docs/diagrams/README.md](docs/diagrams/README.md).

## Where to look next

Beyond the pages linked earlier: [quick start](docs/quickstart.md) to install,
[values reference](docs/values-reference.md) for every label,
[hub-of-hubs](docs/hub-of-hubs.md) for stacked topology,
[cluster install](docs/cluster-install.md) for provisioning,
[releases](docs/releases.md) for OCI mode, and [CONTRIBUTING.md](CONTRIBUTING.md) for sign-off and
pull request rules.
