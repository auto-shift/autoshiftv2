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

The script creates the following structure:

```
policies/<component-name>/
├── Chart.yaml                    # Helm chart metadata
├── values.yaml                   # Default values with AutoShift labels
├── README.md                     # Policy documentation
└── templates/
    └── policy-<component>-operator-install.yaml  # RHACM OperatorPolicy
```

### Key Features

- **Version Control**: Supports operator version pinning via CSV names for precise lifecycle management
- **Subscription Name Labels**: Automatically adds `<component>-subscription-name` label for operator tracking
- **Channel Configuration**: Sets up proper channel subscriptions
- **AutoShift Integration**: Optional automatic addition to hub values file
- **Namespace Support**: Handles both cluster-scoped and namespace-scoped operators
- **Template Variables**: Uses consistent Helm templating for all values

### Version Control

The script generates policies that use AutoShift's new version control approach:

- **Automatic Upgrades**: By default, operators upgrade automatically within their channel
- **Version Pinning**: Use `--version` to pin to a specific CSV for controlled deployments
- **Dynamic Control**: Cluster labels can override default behavior at runtime
- **No Install Plan Management**: Version control handles upgrade approval automatically

When `--version` is specified, the script adds version labels to AutoShift values files, enabling precise control over operator versions across your fleet.

### Configuration Labels

Each generated policy includes these AutoShift labels in values.yaml:

```yaml
<component>: "true"                           # Enable/disable the operator
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
./scripts/generate-policy.sh my-config --dir policies/my-component --target spoke
```

#### Add a configuration policy to an existing policy directory
```bash
./scripts/generate-policy.sh dns-config --dir policies/openshift-dns --target hub
```

#### Generate with a dependency
```bash
./scripts/generate-policy.sh storage-config --dir policies/odf-config --target both --dependency odf-operator-install
```

#### Generate with AutoShift integration
```bash
./scripts/generate-policy.sh my-config --dir policies/my-component --target both --add-to-autoshift
```

#### Interactive mode (prompts for all values)
```bash
./scripts/generate-policy.sh
```

### Placement Targets

| Target | ClusterSets | Label Selector | Wrapping |
|--------|-------------|----------------|----------|
| `hub` | hubClusterSets only | None | `{{- if .Values.hubClusterSets }}` |
| `spoke` | managedClusterSets only | `autoshift.io/<label>` | None |
| `both` | hub + managed | `autoshift.io/<label>` | None |
| `all` | hub + managed | None (all clusters) | None |

### Behavior

- **New directory**: Creates full chart (`Chart.yaml`, `values.yaml`, and policy template)
- **Existing directory**: Only creates the policy template file; does not modify `Chart.yaml` or `values.yaml`

### Generated Structure (new directory)

```
policies/<dir-name>/
├── Chart.yaml                          # Helm chart metadata
├── values.yaml                         # Minimal values (policy_namespace only)
└── templates/
    └── policy-<policy-name>.yaml       # RHACM ConfigurationPolicy
```

The generated template includes a placeholder ConfigMap that should be replaced with your actual resource definition.

---

## 🔄 update-operator-policies.sh

Regenerate existing operator policies from the template. Use this when the template has been updated with new features and you want to apply those changes to all existing policies.

### Usage

```bash
./scripts/update-operator-policies.sh [options]
```

### Options

- `--operator NAME`: Only regenerate a specific operator (e.g., kiali)
- `--verbose`: Show detailed extraction info (component_name, component_camel, label_prefix)
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

The script extracts three values from each existing policy:

- **component_name**: From filename (e.g., `virtualization` from `policy-virtualization-operator-install.yaml`)
- **component_camel**: From `.Values.XXX.namespace` pattern (e.g., `virt`)
- **label_prefix**: From `autoshift.io/XXX-channel` pattern (e.g., `virt`)

This preserves operator-specific naming conventions when regenerating from the template.

---

## 📝 Template Files

The `scripts/templates/` directory contains templates used by the policy generator:

### Files

- `Chart.yaml.template`: Helm chart metadata template
- `values.yaml.template`: Default values with AutoShift labels (operator policies)
- `values-minimal.yaml.template`: Minimal values for configuration policies
- `policy-operator-install.yaml.template`: RHACM OperatorPolicy template
- `policy-config.yaml.template`: RHACM ConfigurationPolicy template (with placement markers)
- `README.md.template`: Policy documentation template

### Template Variables

Templates use these placeholders:

- `{{COMPONENT_NAME}}`: Component name used in policy names (e.g., 'virtualization' in `policy-virtualization-operator-install`)
- `{{COMPONENT_CAMEL}}`: Component name in camelCase for values.yaml references (e.g., 'virt' in `.Values.virt.namespace`)
- `{{LABEL_PREFIX}}`: Prefix used for cluster labels (e.g., 'virt' in `autoshift.io/virt-channel`). For new operators, this matches COMPONENT_NAME. For existing operators with different conventions, it preserves their label prefix.
- `{{SUBSCRIPTION_NAME}}`: Operator subscription name
- `{{NAMESPACE}}`: Target namespace for operator installation
- `{{CHANNEL}}`: Operator channel
- `{{SOURCE}}`: Operator catalog source (e.g., 'redhat-operators')
- `{{SOURCE_NAMESPACE}}`: Catalog source namespace (e.g., 'openshift-marketplace')
- `{{VERSION}}`: Operator version (CSV name, optional)
- `{{OPERATOR_NAME}}`: Formatted operator name (deprecated, use SUBSCRIPTION_NAME)
- `{{COMPONENT_NAME_LOWER}}`: Lowercase component name (deprecated)
- `{{TIMESTAMP}}`: Generation timestamp (deprecated)

## 🛠️ Development

### Adding New Templates

1. Create template file in `scripts/templates/`
2. Use consistent placeholder format: `{{VARIABLE_NAME}}`
3. Update generator script to use new template
4. Document template variables

### Testing Scripts

```bash
# Test policy generation
./scripts/generate-operator-policy.sh test-op test-operator --channel stable --namespace test-operator
helm template policies/test-op/
rm -rf policies/test-op/

# Test with version pinning
./scripts/generate-operator-policy.sh test-op test-operator --channel stable --namespace test-operator --version test-operator.v1.0.0
helm template policies/test-op/
rm -rf policies/test-op/

# Test configuration policy generation
./scripts/generate-policy.sh test-config --dir policies/test-config --target both
helm template policies/test-config/
rm -rf policies/test-config/

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
- Adds default channels for each operator (required by oc-mirror)
- Generates a complete ImageSetConfiguration ready for mirroring

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
```

### Features

- **Automatic Dependency Resolution**: Recursively discovers operator dependencies from the Red Hat operator catalog. For example, `odf-operator` automatically includes `odf-dependencies`, which in turn includes `ocs-operator`, `mcg-operator`, etc.

- **Default Channel Inclusion**: oc-mirror requires the default channel for each operator. The script automatically looks up and includes default channels from the catalog cache.

- **Channel Merging**: When using multiple values files with different channels for the same operator, all channels are included.

### Requirements

- `oc` CLI installed (for catalog extraction)
- `jq` installed (for JSON parsing)
- Pull secret configured for registry.redhat.io
- Operators must have `{operator}-subscription-name` labels in values files

See [Developer Guide](../docs/developer-guide.md#autoshift-scripts-and-label-requirements).

---

## 🔄 update-operator-channels.sh

Update operator channels to latest versions from the Red Hat operator catalog.

### Usage

```bash
./scripts/update-operator-channels.sh --pull-secret <file> [options]
```

### Examples

```bash
# Dry run (show what would change)
./scripts/update-operator-channels.sh --pull-secret ~/.docker/config.json --dry-run

# Apply updates
./scripts/update-operator-channels.sh --pull-secret ~/.docker/config.json

# Check only (exit 1 if updates available, for CI)
./scripts/update-operator-channels.sh --pull-secret ~/.docker/config.json --check
```

### Features

- **Version-aware comparisons**: Only suggests upgrades, never downgrades. If your values file has a newer channel than the catalog, it shows "up to date (catalog has older)".
- **Auto-discovery**: Finds all operators from `{operator}-subscription-name` labels in values files.
- **Multi-format catalog support**: Reads channels from `catalog.json`, standalone channel files (`stable-3.16.json`), and `channels/` subdirectories.

### Requirements

- `oc` CLI installed
- `jq` installed
- Pull secret with access to registry.redhat.io

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

1. Extracts the operator catalog index image to a local cache
2. Parses bundle metadata to find `olm.package.required` dependencies
3. Recursively resolves transitive dependencies
4. Merges in "known dependencies" from `known-dependencies.json` for operators that don't declare dependencies in the catalog (e.g., `odf-operator` → `odf-dependencies`)

### Usage

```bash
./scripts/get-operator-dependencies.sh [options]
```

### Options

- `--catalog CATALOG`: Catalog image (default: registry.redhat.io/redhat/redhat-operator-index:v4.18)
- `--operators PKG1,PKG2`: Comma-separated list of operators to check
- `--all`: Show all operators with dependencies
- `--no-recursive`: Disable recursive resolution (recursive is default)
- `--json`: Output in JSON format
- `--cache-dir DIR`: Directory to cache extracted catalog

### Examples

```bash
# Check specific operators with recursive resolution
./scripts/get-operator-dependencies.sh --operators odf-operator --json

# Check multiple operators
./scripts/get-operator-dependencies.sh --operators devspaces,odf-operator,rhacs-operator

# Show all operators with dependencies
./scripts/get-operator-dependencies.sh --all --json

# Use specific catalog version
./scripts/get-operator-dependencies.sh --catalog registry.redhat.io/redhat/redhat-operator-index:v4.17 --operators odf-operator
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

## 📚 See Also

- [AutoShift Developer Guide](../docs/developer-guide.md)
- [oc-mirror Documentation](https://docs.openshift.com/container-platform/latest/installing/disconnected_install/installing-mirroring-disconnected.html)