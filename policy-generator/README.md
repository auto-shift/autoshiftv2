# PolicyGenerator Migration Guide

This directory contains policies migrated to the PolicyGenerator format. PolicyGenerator allows for cleaner separation between raw manifests and ACM policy wrappers.

## Directory Structure

```
policy-generator/
├── <policy-name>/
│   ├── kustomization.yaml           # Main kustomize config
│   ├── policy-generator.yaml        # PolicyGenerator config
│   ├── placement.yaml               # Placement + PlacementBinding
│   └── policy-manifests/<name>/
│       ├── kustomization.yaml       # References capabilities chart
│       └── values.yaml              # Values for the chart

capabilities/
├── <policy-name>/
│   ├── Chart.yaml
│   ├── values.yaml
│   └── templates/
│       └── *.yaml                   # Raw manifests or ConfigurationPolicies
```

## Variable Substitution

Variables are substituted by the CMP before PolicyGenerator runs:

| Variable | Description | Source |
|----------|-------------|--------|
| `${POLICY_NAMESPACE}` | Namespace for policies | ApplicationSet |
| `${CLUSTER_SETS}` | YAML list of cluster sets | ApplicationSet |

## Migration Patterns

### Pattern 1: Simple object-templates (Raw K8s Manifests)

**Original Helm policy:**
```yaml
ConfigurationPolicy:
  object-templates:
    - complianceType: musthave
      objectDefinition:
        kind: Namespace        # ← Extract this
        metadata:
          name: my-namespace
```

**Migrated to capabilities:**
```yaml
# capabilities/<name>/templates/namespace.yaml
kind: Namespace
metadata:
  name: {{ .Values.namespace }}
```

PolicyGenerator wraps this in ConfigurationPolicy automatically.

### Pattern 2: object-templates-raw (Keep as ConfigurationPolicy)

**Original Helm policy:**
```yaml
ConfigurationPolicy:
  object-templates-raw: |
    - complianceType: musthave
      objectDefinition:
        kind: SomeResource
        # Contains hub templates {{hub ... hub}}
```

**Migrated to capabilities:**
```yaml
# capabilities/<name>/templates/config-raw.yaml
# Keep the entire ConfigurationPolicy - PolicyGenerator includes as-is
apiVersion: policy.open-cluster-management.io/v1
kind: ConfigurationPolicy
metadata:
  name: my-config
spec:
  object-templates-raw: |
    - complianceType: musthave
      objectDefinition:
        kind: SomeResource
        # Hub templates preserved
```

### Pattern 3: OperatorPolicy

**Original Helm policy:**
```yaml
OperatorPolicy:
  name: install-my-operator
  subscription:
    name: my-operator
```

**Migrated to capabilities:**
```yaml
# capabilities/<name>/templates/operator-policy.yaml
# Keep as OperatorPolicy - PolicyGenerator includes as-is
apiVersion: policy.open-cluster-management.io/v1beta1
kind: OperatorPolicy
metadata:
  name: install-my-operator
spec:
  subscription:
    name: {{ .Values.operator.name }}
```

### Pattern 4: Status Check ConfigurationPolicy

**For inform-only status checks, keep as ConfigurationPolicy:**
```yaml
# capabilities/<name>/templates/status-check.yaml
apiVersion: policy.open-cluster-management.io/v1
kind: ConfigurationPolicy
metadata:
  name: my-status-check
spec:
  remediationAction: inform
  object-templates:
    - complianceType: musthave
      objectDefinition:
        kind: SomeResource
        status:
          ready: true
```

## Policy Dependencies

Add dependencies in policy-generator.yaml:
```yaml
policies:
  - name: policy-my-config
    dependencies:
      - name: policy-my-operator-install
        namespace: ${POLICY_NAMESPACE}
        compliance: Compliant
```

## Deployment Modes

### Git Mode (CMP)
- ApplicationSet uses `source.plugin: policygenerator`
- CMP substitutes variables at deploy time
- PolicyGenerator runs in ArgoCD

### OCI Mode (CI Build)
- Run `scripts/build-policygenerator-oci.sh`
- Converts to Helm chart with templated values
- Publish to OCI registry

## Examples

See these migrated policies for reference:
- `openshift-pipelines/` - Simple policy with OperatorPolicy
- `lvm/` - Complex policy with object-templates-raw
- `quay-configure/` - Policy wrapping external Helm chart
