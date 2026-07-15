# AutoShift Scripts Documentation

This directory contains utility scripts for AutoShiftv2 policy generation and management.

## Quick Reference

| Script | Purpose |
|--------|---------|
| `generate-operator-policy.sh` | Generate new operator policies |
| `generate-policy.sh` | Generate configuration (non-operator) policies |
| `update-operator-policies.sh` | Regenerate existing policies from template |
| `generate-imageset-config.sh` | Generate ImageSetConfiguration for oc-mirror (auto-resolves dependencies) |
| `update-operator-channels.sh` | Update operator channels from catalog |
| `dev-checks.sh` | Run development quality checks (shellcheck, kubeconform) |
| `sync-bootstrap-values.sh` | Sync bootstrap chart values from policies |
| `create-quay-repos.sh` | Create Quay.io repositories for charts |
| `deploy-oci.sh` | Deploy AutoShift from OCI registry |
| `generate-bootstrap-installer.sh` | Generate release installation artifacts |
| `get-operator-dependencies.sh` | Extract operator dependencies from catalog |

---

## 📦 generate-operator-policy.sh

Generate RHACM operator policies for AutoShiftv2 with proper Helm chart structure.

### Usage

```bash
./scripts/generate-operator-policy.sh <component-name> <subscription-name> --channel <channel> --namespace <namespace> [options]
```

### Required Parameters

- `<component-name>`: Name for your policy component (e.g., 'cert-manager', 'metallb')
- `<subscription-name>`: Exact operator subscription name from catalog (e.g., 'cert-manager', 'metallb-operator')
- `--channel <channel>`: Operator channel to subscribe to (e.g., 'stable', 'fast', 'stable-v1')
- `--namespace <namespace>`: Target namespace for operator installation

### Optional Parameters

- `--version <version>`: Pin to specific operator version (CSV name, optional)
- `--namespace-scoped`: Generate a namespace-scoped operator policy (default: cluster-scoped)
- `--add-to-autoshift`: Automatically add the component to autoshift values files
- `--values-files <files>`: Comma-separated list of values files to update (e.g., 'hub,sbx')
- `--show-integration`: Show manual integration instructions
- `--help`: Display help message

### Examples

#### Generate a cluster-scoped operator policy
```bash
./scripts/generate-operator-policy.sh cert-manager cert-manager-operator --channel stable --namespace cert-manager
```

#### Generate with AutoShift integration
```bash
./scripts/generate-operator-policy.sh metallb metallb-operator --channel stable --namespace metallb-system --add-to-autoshift
```

#### Generate with version pinning
```bash
./scripts/generate-operator-policy.sh cert-manager cert-manager-operator --channel stable --namespace cert-manager --version cert-manager.v1.14.4 --add-to-autoshift
```

#### Generate a namespace-scoped operator
```bash
./scripts/generate-operator-policy.sh my-operator my-operator --channel stable --namespace my-operator --namespace-scoped
```

### Generated Structure

The script creates an ACM **PolicyGenerator** directory (Kustomize source), not a Helm chart:

```
policies/stable/<component-name>/
├── kustomization.yaml            # entrypoint: generators: [policy-generator-config.yaml]
├── policy-generator-config.yaml  # PolicyGenerator: policy graph, remediation, eval interval
├── placement.yaml                # Placement predicate (autoshift.io/<component>) + tolerations
├── README.md                     # Policy documentation
└── manifests/                    # bare resources — PG wraps each into the ConfigurationPolicy
    ├── namespace.yaml            #   the operator Namespace (raw)
    └── operator.yaml             #   the OperatorPolicy (first-class; carries ${REMEDIATION})
```

The manifests are **bare** — you author only the resource, and PolicyGenerator generates the
`ConfigurationPolicy` wrapper and injects `remediationAction`/`severity`/`evaluationInterval`. The
per-deployment `${POLICY_NAMESPACE}`, `${REMEDIATION}`, `${EVAL_COMPLIANT}`, `${EVAL_NONCOMPLIANT}`
tokens are substituted by the repo-server CMP before `kustomize build`.

### Key Features

- **Version Control**: Supports operator version pinning via CSV names for precise lifecycle management
- **Subscription Name Labels**: Automatically adds `<component>-subscription-name` label for operator tracking
- **Channel Configuration**: Sets up proper channel subscriptions
- **AutoShift Integration**: Optional automatic addition to hub values file (and `_example*.yaml`)
- **Namespace Support**: Handles both cluster-scoped and namespace-scoped operators (`targetNamespaces`)
- **PolicyGenerator native**: bare manifests + hand-authored placement; no Helm chart boilerplate

### Version Control

The script generates policies that use AutoShift's new version control approach:

- **Automatic Upgrades**: By default, operators upgrade automatically within their channel
- **Version Pinning**: Use `--version` to pin to a specific CSV for controlled deployments
- **Dynamic Control**: Cluster labels can override default behavior at runtime
- **No Install Plan Management**: Version control handles upgrade approval automatically

When `--version` is specified, the script adds version labels to AutoShift values files, enabling precise control over operator versions across your fleet.

### Configuration Labels

The operator manifest reads these AutoShift labels via hub templates (`{{hub … hub}}`), with the
defaults baked in as literals. Set them in values files to override per clusterset/cluster:

```yaml
<component>: "true"                           # Enable/disable the operator (placement predicate)
<component>-subscription-name: "<subscription-name>"  # Operator subscription name
<component>-channel: "<channel>"              # Operator channel
<component>-version: "operator-name.v1.x.x"  # Specific CSV version (optional)
<component>-source: "redhat-operators"               # Catalog source
<component>-source-namespace: "openshift-marketplace" # Catalog namespace
```


---

## 📦 generate-policy.sh

Generate RHACM configuration (non-operator) policies for AutoShiftv2. Use this for policies that configure cluster resources (ConfigurationPolicy) rather than install operators (OperatorPolicy).

### Usage

```bash
./scripts/generate-policy.sh [policy-name] [options]
```

Missing required values are prompted interactively.

### Parameters

- `<policy-name>` (positional): Kebab-case name for the policy (e.g., `my-config`, `dns-tolerations`)

### Options

- `--dir DIR`: Policy directory - existing or new (default: prompted)
- `--target TARGET`: Placement target: `hub`, `spoke`, `both`, `all` (default: prompted)
- `--label LABEL`: Label predicate key without `autoshift.io/` prefix (default: directory basename; ignored for `hub`/`all` targets)
- `--dependency POLICY`: Policy dependency name, repeatable (e.g., `--dependency lvm-operator-install`)
- `--add-to-autoshift`: Add enable label to AutoShift values files (only for `spoke`/`both` targets)
- `--values-files FILES`: Comma-separated list of values files to update (e.g., `hub,sbx`). Default: all non-example files
- `--help`: Display help message

### Examples

#### Generate a spoke configuration policy (new directory)
```bash
./scripts/generate-policy.sh my-config --dir policies/stable/my-component --target spoke
```

#### Add a configuration policy to an existing policy directory
```bash
./scripts/generate-policy.sh dns-config --dir policies/stable/openshift-dns --target hub
```

#### Generate with a dependency
```bash
./scripts/generate-policy.sh storage-config --dir policies/stable/odf-config --target both --dependency odf-operator-install
```

#### Generate with AutoShift integration
```bash
./scripts/generate-policy.sh my-config --dir policies/stable/my-component --target both --add-to-autoshift
```

#### Interactive mode (prompts for all values)
```bash
./scripts/generate-policy.sh
```

### Placement Targets

PG placements carry **no** `spec.clusterSets` — scoping comes from the ManagedClusterSetBindings the
top autoshift chart creates in the policy namespace, filtered by a label predicate:

| Target | Predicate |
|--------|-----------|
| `hub` | `autoshift.io/self-managed Exists` (hub-only marker; managed clusters never carry it) |
| `spoke` | `autoshift.io/self-managed DoesNotExist` **AND** `autoshift.io/<label> In ['true']` |
| `both` | `autoshift.io/<label> In ['true']` (hub + managed) |
| `all` | none (every bound cluster) |

### Behavior

- **New directory**: Creates a full PolicyGenerator dir (`kustomization.yaml`,
  `policy-generator-config.yaml`, `placement.yaml`, and a bare manifest).
- **Existing directory** (must already be a PolicyGenerator dir): adds a bare manifest, a
  `placement-<policy>.yaml`, and appends a `policies[]` entry (with its own placement + any
  dependencies) to `policy-generator-config.yaml`.

### Generated Structure (new directory)

```
policies/<dir-name>/
├── kustomization.yaml                  # entrypoint
├── policy-generator-config.yaml        # PolicyGenerator (one policies[] entry)
├── placement.yaml                      # Placement predicate for the target
└── manifests/
    └── <policy-name>.yaml              # bare placeholder resource (PG wraps it)
```

The generated manifest is a **bare** placeholder ConfigMap — replace it with your actual resource
(no ConfigurationPolicy wrapper). For hub templates, loops, or conditionals, replace it with a bare
`object-templates-raw:` manifest instead.

---

## 🔄 update-operator-policies.sh

Re-render every operator's bare OperatorPolicy manifest (`manifests/operator.yaml`, wherever it lives
under `manifests/`) from `manifest-operator.yaml.template`. Use this when the template gains a new
feature (e.g. a new OperatorPolicy field) and you want to propagate it to all operators at once. It
regenerates ONLY the OperatorPolicy manifest — not the Namespace, placement, or
`policy-generator-config.yaml` — and validates each dir with a PolicyGenerator render.

### Usage

```bash
./scripts/update-operator-policies.sh [options]
```

### Options

- `--operator NAME`: Only regenerate a specific operator (directory name, e.g., kiali)
- `--verbose`: Show the params extracted from each operator manifest
- `--help`: Display help message

### Examples

```bash
# Regenerate all operator policies
./scripts/update-operator-policies.sh

# Regenerate only tempo
./scripts/update-operator-policies.sh --operator tempo

# Regenerate with verbose output
./scripts/update-operator-policies.sh --verbose
```

### Workflow

The script regenerates policies and relies on git for review:

```bash
# 1. Run the script
./scripts/update-operator-policies.sh

# 2. Review changes
git diff

# 3. Discard all changes if not needed
git checkout -- policies/

# 4. Or selectively stage changes
git add -p
```

### How It Works

For each `manifests/**/operator.yaml` containing `kind: OperatorPolicy`, the script extracts the
operator's real params from the existing manifest and re-renders from the template:

- **component_name**: from `name: install-operator-<component>`
- **label_prefix**: from the `autoshift.io/<prefix>-channel` label (may differ from component_name, e.g. `virt`)
- **namespace**: the operatorGroup namespace
- **subscription_name / source / source_namespace / channel**: the `| default "…"` baked into each hub template
- **namespace-scoped**: re-injects `targetNamespaces` if the operatorGroup had it

Structural drift is reset to the template (that is the point); the extracted per-operator values are
preserved. An operator whose manifest is non-standard (a param can't be extracted) is **skipped**,
not clobbered — review those by hand. Trident (NetApp's custom install shape) is the current example.

---

## 📝 Template Files

The `scripts/templates/` directory contains templates used by the policy generator:

### Files

- `kustomization.yaml.template`: Kustomize entrypoint (shared by both generators)
- `pg-config-operator.yaml.template`: PolicyGenerator config for operator policies
- `placement-operator.yaml.template`: Placement for operator policies (`autoshift.io/<component>`)
- `manifest-namespace.yaml.template`: bare operator Namespace manifest
- `manifest-operator.yaml.template`: bare OperatorPolicy manifest (unescaped `{{hub … hub}}`)
- `pg-config.yaml.template`: PolicyGenerator config for configuration policies
- `manifest-config.yaml.template`: bare placeholder ConfigMap for configuration policies
- `README.md.template`: Policy documentation template (operator policies)

`update-operator-policies.sh` reuses `manifest-operator.yaml.template` (the same one
`generate-operator-policy.sh` writes), so a template change propagates through both paths.

### Template Variables

Templates use these placeholders (substituted by the generators; the `${...}` PolicyGenerator
tokens are left intact for the repo-server CMP):

Operator templates:
- `{{COMPONENT_NAME}}`: Component name used in policy/manifest names (e.g., 'cert-manager')
- `{{LABEL_PREFIX}}`: Prefix for cluster labels (e.g., 'cert-manager' in `autoshift.io/cert-manager-channel`); matches COMPONENT_NAME
- `{{SUBSCRIPTION_NAME}}`: Operator subscription name
- `{{NAMESPACE}}`: Target namespace for operator installation
- `{{CHANNEL}}`: Operator channel
- `{{SOURCE}}`: Operator catalog source (e.g., 'redhat-operators')
- `{{SOURCE_NAMESPACE}}`: Catalog source namespace (e.g., 'openshift-marketplace')

Configuration templates:
- `{{DIR_BASENAME}}`: Directory name (PolicyGenerator `metadata.name`)
- `{{POLICY_NAME}}`: Policy/manifest name (e.g., 'dns-tolerations')
- `{{LABEL}}`: Placement predicate label without the `autoshift.io/` prefix

## 🛠️ Development

### Adding New Templates

1. Create template file in `scripts/templates/`
2. Use consistent placeholder format: `{{VARIABLE_NAME}}`
3. Update generator script to use new template
4. Document template variables

### Testing Scripts

```bash
# PolicyGenerator render (needs: make install-policy-generator). The generators run this
# validation automatically, but you can re-run it by hand:
PG_RENDER() { KUSTOMIZE_PLUGIN_HOME=$PWD/.tools/kustomize-plugin .tools/kustomize build \
  --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone "$1"; }

# Test operator policy generation
./scripts/generate-operator-policy.sh test-op test-operator --channel stable --namespace test-operator
PG_RENDER policies/stable/test-op/
rm -rf policies/stable/test-op/

# Test configuration policy generation
./scripts/generate-policy.sh test-config --dir policies/stable/test-config --target both
PG_RENDER policies/stable/test-config/
rm -rf policies/stable/test-config/

# Test imageset generation
./scripts/generate-imageset-config.sh autoshift/values/clustersets/hub.yaml --operators-only --output test-imageset.yaml
cat test-imageset.yaml
rm test-imageset.yaml
```

### Common Issues

| Issue | Solution |
|-------|----------|
| Script permission denied | Run `chmod +x scripts/*.sh` |
| Bash version incompatibility | Scripts require Bash 3.2+ (macOS compatible) |
| Template not found | Ensure scripts/templates/ directory exists |
| Invalid YAML output | Check template indentation and escaping |

---

## 🔧 generate-imageset-config.sh

Generate ImageSetConfiguration YAML for oc-mirror disconnected mirroring.

This script automatically:
- Discovers all enabled operators from your values files
- Resolves operator dependencies recursively (e.g., `odf-operator` → `odf-dependencies` → sub-operators)
- Adds `defaultChannel` for each operator package (required by oc-mirror)
- Deduplicates cross-catalog dependencies (e.g., certified operators won't be added to the redhat catalog)
- Generates a complete `ImageSetConfiguration` with `apiVersion: mirror.openshift.io/v2alpha1`

### Usage

```bash
./scripts/generate-imageset-config.sh <values-files> [options]
```

### Examples

```bash
# Generate for single environment (auto-resolves all dependencies)
./scripts/generate-imageset-config.sh autoshift/values/clustersets/hub.yaml

# Operators only (skip OpenShift platform)
./scripts/generate-imageset-config.sh autoshift/values/clustersets/hub.yaml --operators-only

# Multiple environments (merges channels)
./scripts/generate-imageset-config.sh autoshift/values/clustersets/hub.yaml,autoshift/values/clustersets/sbx.yaml

# Custom output file
./scripts/generate-imageset-config.sh autoshift/values/clustersets/hub.yaml --output my-imageset.yaml

# Use pre-generated dependencies (for Windows or offline use)
./scripts/generate-imageset-config.sh autoshift/values/clustersets/hub.yaml --dependencies-file scripts/operator-dependencies.json
```

### Features

- **Automatic Dependency Resolution**: Recursively discovers operator dependencies from the Red Hat operator catalog. For example, `odf-operator` automatically includes `odf-dependencies`, which in turn includes `ocs-operator`, `mcg-operator`, etc.

- **Default Channel Inclusion**: oc-mirror requires the default channel for each operator package. The script automatically looks up and includes `defaultChannel` from the catalog cache, supporting `catalog.json`, `channel.json`, and `catalog.yaml` formats.

- **Cross-Catalog Deduplication**: Dependencies that belong to a different catalog (e.g., certified-operators) are not duplicated into the redhat-operators section.

- **Channel Merging**: When using multiple values files with different channels for the same operator, all channels are included.

- **Pull Secret Auto-Detection**: Automatically finds `pull-secret.json` or `pull-secret.txt` in the repo root. Can also be set via the `REGISTRY_AUTH_FILE` environment variable.

### Requirements

- `oc` CLI installed (for catalog extraction)
- `jq` installed (for JSON parsing)
- Pull secret in the repo root (`pull-secret.json` or `pull-secret.txt`) or `REGISTRY_AUTH_FILE` env var
- Operators must have `{operator}-subscription-name` labels in values files

See [Developer Guide](../docs/developer-guide.md#autoshift-scripts-and-label-requirements).

---

## 🔄 update-operator-channels.sh

Update operator channels to latest versions from the Red Hat operator catalog.

### Usage

```bash
./scripts/update-operator-channels.sh [options]
```

### Examples

```bash
# Dry run — pull secret auto-detected from repo root
./scripts/update-operator-channels.sh --dry-run

# Explicit pull secret
./scripts/update-operator-channels.sh --pull-secret pull-secret.json --dry-run

# Apply updates
./scripts/update-operator-channels.sh

# Check only (exit 1 if updates available, for CI)
./scripts/update-operator-channels.sh --check
```

### Features

- **Version-aware comparisons**: Only suggests upgrades, never downgrades. If your values file has a newer channel than the catalog, it shows "keeping current channel".
- **Auto-discovery**: Finds all operators from `{operator}-subscription-name` labels in values files.
- **Multi-format catalog support**: Reads channels from `catalog.json`, `catalog.yaml`, standalone channel files (`stable-3.16.json`, `channel.json`), and `channels/` subdirectories.
- **Pull secret auto-detection**: Finds `pull-secret.json` or `pull-secret.txt` in the repo root automatically.

### Requirements

- `oc` CLI installed
- `jq` installed
- Pull secret in the repo root (`pull-secret.json` or `pull-secret.txt`), `--pull-secret PATH`, or `REGISTRY_AUTH_FILE` env var

---

## 🧪 dev-checks.sh

Run development quality checks. Gracefully skips tools that aren't installed.

### Usage

```bash
./scripts/dev-checks.sh
```

### Checks Performed

- **shellcheck**: Lint shell scripts
- **kubeconform**: Validate Kubernetes manifests
- **helm lint**: Validate Helm charts

### Requirements (Optional)

```bash
brew install shellcheck kubeconform
```

---

## 🔗 sync-bootstrap-values.sh

Sync bootstrap chart values from policy chart values to ensure consistency.

### Usage

```bash
./scripts/sync-bootstrap-values.sh
```

Or via Makefile:

```bash
make sync-values
```

---

## 📦 create-quay-repos.sh

Create Quay.io repositories for all AutoShift Helm charts.

### Usage

```bash
./scripts/create-quay-repos.sh <quay-token> [organization]
```

### Example

```bash
./scripts/create-quay-repos.sh mytoken autoshift
```

---

## 🚀 deploy-oci.sh

Deploy AutoShift from OCI registry with pre-built configurations.

### Usage

```bash
./scripts/deploy-oci.sh --version <version> [options]
```

### Examples

```bash
# Deploy hub configuration
./scripts/deploy-oci.sh --version 1.0.0

# Deploy with OCI policies
./scripts/deploy-oci.sh --version 1.0.0 --oci-policies

# Dry run
./scripts/deploy-oci.sh --version 1.0.0 --dry-run
```

---

## 📋 generate-bootstrap-installer.sh

Generate installation artifacts for OCI releases.

### Usage

```bash
./scripts/generate-bootstrap-installer.sh <version> <registry> <namespace> <output-dir>
```

Used internally by `make release`.

---

## 🔍 get-operator-dependencies.sh

Extract operator dependencies from the Red Hat operator catalog. This script is automatically called by `generate-imageset-config.sh`, but can also be used standalone.

### How It Works

1. Extracts the operator catalog index image to a local cache (`.cache/catalog-cache/`)
2. Parses bundle metadata to find `olm.package.required` dependencies
3. Recursively resolves transitive dependencies
4. Merges in "known dependencies" from `known-dependencies.json` for operators that don't declare dependencies in the catalog (e.g., `odf-operator` → `odf-dependencies`)
5. Auto-detects catalog version from `openshift-version` in your values files

### Usage

```bash
./scripts/get-operator-dependencies.sh [options]
```

### Options

- `--catalog CATALOG`: Catalog image (overrides auto-detection from `openshift-version`)
- `--operators PKG1,PKG2`: Comma-separated list of operators to check
- `--all`: Show all operators with dependencies
- `--no-recursive`: Disable recursive resolution (recursive is default)
- `--json`: Output in JSON format
- `--cache-dir DIR`: Directory to cache extracted catalog
- `--pull-secret FILE`: Path to pull secret file (default: auto-detect from repo root)

### Examples

```bash
# Check specific operators (pull secret auto-detected, catalog version auto-detected)
./scripts/get-operator-dependencies.sh --operators odf-operator --json

# Check multiple operators
./scripts/get-operator-dependencies.sh --operators devspaces,odf-operator,rhacs-operator

# Show all operators with dependencies
./scripts/get-operator-dependencies.sh --all --json

# Use specific catalog version with explicit pull secret
./scripts/get-operator-dependencies.sh --catalog registry.redhat.io/redhat/redhat-operator-index:v4.17 --operators odf-operator --pull-secret pull-secret.json
```

### Known Dependencies

The file `scripts/known-dependencies.json` contains manual dependencies for operators that don't declare them in the catalog. Currently:

```json
{
  "odf-operator": ["odf-dependencies"]
}
```

The script will recursively resolve `odf-dependencies` from the catalog to get the full dependency tree.

---

## 🖥️ Platform Compatibility

### Self-Contained — No External Dependencies

All scripts keep temporary files inside the repo under `.tmp/` (gitignored). No script writes to `/tmp/` or any directory outside the repository.

### Pull Secret Handling

Scripts that access the Red Hat registry (`generate-imageset-config.sh`, `get-operator-dependencies.sh`, `update-operator-channels.sh`) share a common pull secret resolution order:

1. `--pull-secret <path>` flag (explicit)
2. `pull-secret.json` or `pull-secret.txt` in the repo root (auto-detected)
3. `REGISTRY_AUTH_FILE` environment variable

Place your pull secret in the repo root as `pull-secret.json` and all scripts will find it automatically. The file is gitignored.

### Windows (Git Bash)

Scripts that use `oc image extract` (catalog extraction) require symlink support, which Windows restricts by default. These scripts detect Git Bash and test for symlink capability before running:

- If **Developer Mode** is enabled (Settings > System > For developers), symlinks work and scripts run normally.
- If symlinks are not available, the script exits with instructions to either enable Developer Mode or run on Mac/Linux/WSL2.

For `generate-imageset-config.sh` on Windows without symlink support, use the `--dependencies-file` flag with the pre-generated `scripts/operator-dependencies.json`:

```bash
./scripts/generate-imageset-config.sh autoshift/values/clustersets/hub.yaml --dependencies-file scripts/operator-dependencies.json
```

### Catalog Version Auto-Detection

Scripts that extract the operator catalog auto-detect the catalog version from the `openshift-version` label in your clusterset values files (e.g., `openshift-version: '4.20.28'` resolves to `v4.20`). Use `--catalog` to override.

---

## 📚 See Also

- [AutoShift Developer Guide](../docs/developer-guide.md)
- [oc-mirror Documentation](https://docs.openshift.com/container-platform/latest/installing/disconnected_install/installing-mirroring-disconnected.html)