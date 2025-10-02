# AutoShift Scripts Documentation

This directory contains utility scripts for AutoShiftv2 policy generation and management.

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

- `{{COMPONENT_NAME}}`: Component name (e.g., 'cert-manager')
- `{{SUBSCRIPTION_NAME}}`: Operator subscription name
- `{{NAMESPACE}}`: Target namespace for operator installation
- `{{CHANNEL}}`: Operator channel
- `{{SOURCE}}`: Operator catalog source (e.g., 'redhat-operators')
- `{{SOURCE_NAMESPACE}}`: Catalog source namespace (e.g., 'openshift-marketplace')
- `{{VERSION}}`: Operator version (CSV name, optional)
- `{{COMPONENT_CAMEL}}`: Component name in camelCase for values.yaml
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

## 📚 See Also

- [AutoShift Developer Guide](../README-DEVELOPER.md)
- [Policy Development Guide](../docs/policy-development.md)