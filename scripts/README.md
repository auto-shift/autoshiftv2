# AutoShift Scripts Documentation

This directory contains utility scripts for AutoShiftv2 policy generation and management.

## 📦 generate-operator-policy.sh

Generate RHACM operator policies for AutoShiftv2 with proper Helm chart structure.

### Usage

```bash
./scripts/generate-operator-policy.sh <component-name> <subscription-name> --channel <channel> [options]
```

### Required Parameters

- `<component-name>`: Name for your policy component (e.g., 'cert-manager', 'metallb')
- `<subscription-name>`: Exact operator subscription name from catalog (e.g., 'cert-manager', 'metallb-operator')
- `--channel <channel>`: Operator channel to subscribe to (e.g., 'stable', 'fast', 'stable-v1')

### Optional Parameters

- `--namespace-scoped`: Generate a namespace-scoped operator policy (default: cluster-scoped)
- `--add-to-autoshift`: Automatically add the component to autoshift/values.hub.yaml
- `--help`: Display help message

### Examples

#### Generate a cluster-scoped operator policy
```bash
./scripts/generate-operator-policy.sh cert-manager cert-manager --channel stable
```

#### Generate with AutoShift integration
```bash
./scripts/generate-operator-policy.sh metallb metallb-operator --channel stable --add-to-autoshift
```

#### Generate a namespace-scoped operator
```bash
./scripts/generate-operator-policy.sh my-operator my-operator --channel stable --namespace-scoped
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

- **Subscription Name Labels**: Automatically adds `<component>-subscription-name` label for operator tracking
- **Channel Configuration**: Sets up proper channel subscriptions
- **AutoShift Integration**: Optional automatic addition to hub values file
- **Namespace Support**: Handles both cluster-scoped and namespace-scoped operators
- **Template Variables**: Uses consistent Helm templating for all values

### Configuration Labels

Each generated policy includes these AutoShift labels in values.yaml:

```yaml
<component>: "true"                           # Enable/disable the operator
<component>-subscription-name: "<subscription-name>"  # Operator subscription name
<component>-channel: "<channel>"              # Operator channel
<component>-install-plan-approval: "Automatic"        # Approval strategy
<component>-source: "redhat-operators"               # Catalog source
<component>-source-namespace: "openshift-marketplace" # Catalog namespace
```

## 🔄 generate-imageset-config.sh

Generate ImageSetConfiguration for oc-mirror from AutoShift values files.

### Usage

```bash
./scripts/generate-imageset-config.sh [options]
```

### Options

- `--values-files <files>`: Comma-separated list of values files (default: values.hub.yaml)
- `--include-operators`: Include operator catalog sources
- `--openshift-version <version>`: OpenShift version (default: 4.18.0)
- `--channel <channel>`: OpenShift channel (default: stable-4.18)
- `--output <file>`: Output file name (default: imageset-config-<suffix>.yaml)
- `--registry <registry>`: Target registry for mirroring
- `--help`: Display help message

### Examples

#### Generate for single environment
```bash
./scripts/generate-imageset-config.sh \
  --values-files values.hub.yaml \
  --include-operators \
  --output imageset-hub.yaml
```

#### Generate for multiple environments with channel merging
```bash
./scripts/generate-imageset-config.sh \
  --values-files values.hub.yaml,values.sbx.yaml,values.prod.yaml \
  --include-operators \
  --openshift-version 4.18.0 \
  --registry registry.internal.example.com:5000
```

#### Generate OpenShift-only configuration
```bash
./scripts/generate-imageset-config.sh \
  --openshift-version 4.18.0 \
  --channel stable-4.18 \
  --output imageset-openshift-only.yaml
```

### Features

- **Multi-file Support**: Process multiple values files and merge operator channels
- **Channel Merging**: Combines multiple channels when operators appear in multiple files
- **Operator Detection**: Automatically finds all enabled operators with subscription names
- **Flexible Output**: Customizable output location and registry settings
- **OpenShift Versions**: Supports any OpenShift 4.x version

### Output Format

Generates a valid ImageSetConfiguration for oc-mirror:

```yaml
apiVersion: mirror.openshift.io/v1alpha2
kind: ImageSetConfiguration
storageConfig:
  registry:
    imageURL: <registry>/openshift/release/metadata:latest
mirror:
  platform:
    channels:
      - name: stable-4.18
        type: ocp
        minVersion: 4.18.0
        maxVersion: 4.18.0
  operators:
    - catalog: registry.redhat.io/redhat/redhat-operator-index:v4.18
      packages:
        - name: openshift-gitops-operator
          channels:
            - name: latest
        - name: advanced-cluster-management
          channels:
            - name: release-2.14
        # ... additional operators
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
- `{{CHANNEL}}`: Operator channel
- `{{OPERATOR_NAME}}`: Formatted operator name (deprecated, use SUBSCRIPTION_NAME)
- `{{COMPONENT_NAME_LOWER}}`: Lowercase component name
- `{{TIMESTAMP}}`: Generation timestamp

## 🛠️ Development

### Adding New Templates

1. Create template file in `scripts/templates/`
2. Use consistent placeholder format: `{{VARIABLE_NAME}}`
3. Update generator script to use new template
4. Document template variables

### Testing Scripts

```bash
# Test policy generation
./scripts/generate-operator-policy.sh test-op test-operator --channel stable
helm template policies/test-op/
rm -rf policies/test-op/

# Test imageset generation
./scripts/generate-imageset-config.sh \
  --values-files values.hub.yaml \
  --include-operators \
  --output test-imageset.yaml
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

## 📚 See Also

- [AutoShift Developer Guide](../README-DEVELOPER.md)
- [Policy Development Guide](../docs/policy-development.md)