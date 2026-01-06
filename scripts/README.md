# AutoShift Scripts Documentation

This directory contains utility scripts for AutoShiftv2 policy generation and management.

## Quick Reference

| Script | Purpose |
|--------|---------|
| `generate-operator-policy.sh` | Generate new operator policies |
| `update-operator-policies.sh` | Regenerate existing policies from template |
| `generate-imageset-config.sh` | Generate ImageSetConfiguration for oc-mirror |
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
- `--add-to-autoshift`: Automatically add the component to autoshift/values.hub.yaml
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
- `values.yaml.template`: Default values with AutoShift labels
- `policy-operator-install.yaml.template`: RHACM OperatorPolicy template
- `policy-namespace-operator-install.yaml.template`: Namespace-scoped operator template
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

# Test oc-mirror imageset generation (see oc-mirror/README.md)
cd oc-mirror
./generate-imageset-config.sh values.hub.yaml --output test-imageset.yaml
cat test-imageset.yaml
rm test-imageset.yaml
cd ..
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

### Usage

```bash
./scripts/generate-imageset-config.sh <values-files> [options]
```

### Examples

```bash
# Generate for single environment
./scripts/generate-imageset-config.sh autoshift/values.hub.yaml --output imageset.yaml

# Operators only (skip OpenShift platform)
./scripts/generate-imageset-config.sh autoshift/values.hub.yaml --operators-only -o imageset.yaml

# Multiple environments (merges channels)
./scripts/generate-imageset-config.sh autoshift/values.hub.yaml,autoshift/values.sbx.yaml -o imageset.yaml
```

### Requirements

Operators must have `{operator}-subscription-name` labels in values files. See [Developer Guide](../docs/developer-guide.md#autoshift-scripts-and-label-requirements).

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
```

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

Extract operator dependencies from the Red Hat operator catalog.

### Usage

```bash
./scripts/get-operator-dependencies.sh [options]
```

### Examples

```bash
# Check specific operators
./scripts/get-operator-dependencies.sh --operators devspaces,odf-operator

# Show all operators with dependencies
./scripts/get-operator-dependencies.sh --all --json
```

---

## 📚 See Also

- [AutoShift Developer Guide](../docs/developer-guide.md)
- [oc-mirror Documentation](../oc-mirror/README.md)