# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

AutoShiftv2 is an Infrastructure-as-Code (IaC) framework for managing OpenShift Platform Plus components using Red Hat Advanced Cluster Management (RHACM) and OpenShift GitOps. It provides declarative management of day-2 operations across hub and managed OpenShift clusters.

## Architecture

The framework consists of three main architectural layers:

1. **Hub Cluster**: Hosts RHACM and OpenShift GitOps that manage all other clusters
2. **GitOps Layer**: Uses ArgoCD ApplicationSets to deploy policies across cluster sets
3. **Policy Layer**: Helm charts in `policies/` directory that define OpenShift Platform Plus components

### Key Components

- **autoshift/**: Main Helm chart that creates ArgoCD ApplicationSet to deploy all policies
- **policies/**: Individual Helm charts for each OpenShift Platform Plus component (ACM, ACS, ODF, etc.)
- **openshift-gitops/**: Bootstrap chart for initial GitOps installation
- **advanced-cluster-management/**: Bootstrap chart for initial ACM installation

## Common Commands

### Installation Commands
```bash
# Install OpenShift GitOps
helm upgrade --install openshift-gitops openshift-gitops -f policies/openshift-gitops/values.yaml

# Install Advanced Cluster Management
helm upgrade --install advanced-cluster-management advanced-cluster-management -f policies/advanced-cluster-management/values.yaml

# Install AutoShift (creates ApplicationSet that deploys all policies)
export APP_NAME="autoshift"
export REPO_URL="https://github.com/auto-shift/autoshiftv2.git"
export TARGET_REVISION="main"
export VALUES_FILE="values.hub.yaml"
export ARGO_PROJECT="default"
export GITOPS_NAMESPACE="openshift-gitops"
cat << EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: $APP_NAME
  namespace: $GITOPS_NAMESPACE
spec:
  destination:
    namespace: ''
    server: https://kubernetes.default.svc
  source:
    path: autoshift
    repoURL: $REPO_URL
    targetRevision: $TARGET_REVISION
    helm:
      valueFiles:
        - $VALUES_FILE
      values: |-
        autoshiftGitRepo: $REPO_URL
        autoshiftGitBranchTag: $TARGET_REVISION
  sources: []
  project: $ARGO_PROJECT
  syncPolicy:
    automated:
      prune: false
      selfHeal: true
EOF
```

### Verification Commands
```bash
# Check OpenShift GitOps pods
oc get pods -n openshift-gitops

# Check ACM installation status
oc get mch -A -w

# Check ArgoCD instance
oc get argocd -A

# Check cluster sets
oc get managedclustersets
```

## Configuration System

AutoShiftv2 uses a three-tiered configuration system with precedence: **cluster labels > clusterset labels > helm values**

### Key Configuration Files
- `autoshift/values.hub.yaml`: Default hub cluster configuration
- `autoshift/values.hub.baremetal-sno.yaml`: Single Node OpenShift configuration
- `autoshift/values.sbx.yaml`: Sandbox environment configuration
- `policies/*/values.yaml`: Individual policy default values

### Identifying Configuration Variables
When documenting or working with policy configurations, **always check the actual policy templates** rather than relying solely on values.yaml files. Look for:

1. **ManagedClusterLabels references** in templates:
   ```yaml
   index .ManagedClusterLabels "autoshift.io/gitops-channel"
   ```

2. **Placement matchExpressions** that show required labels:
   ```yaml
   matchExpressions:
     - key: 'autoshift.io/gitops'
       operator: In
       values:
       - 'true'
   ```

3. **Template variables** like:
   ```yaml
   channel: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/gitops-channel" | default "{{ .Values.gitops.channel }}" {{ "hub}}" }}'
   ```

Search commands to find actual label usage:
```bash
# Find all ManagedClusterLabels references
grep -r "ManagedClusterLabels" policies/*/templates/

# Find all autoshift.io labels
grep -r "autoshift\.io/" policies/*/templates/

# Find placement match expressions
grep -A5 "matchExpressions:" policies/*/templates/
```

### Feature Flags
Components are enabled/disabled using boolean labels like:
- `metallb: 'true'` - Enable MetalLB
- `acs: 'true'` - Enable Advanced Cluster Security
- `odf: 'true'` - Enable OpenShift Data Foundation
- `logging: 'true'` - Enable OpenShift Logging

## Policy Structure

Each policy in `policies/` follows this pattern:
- **Chart.yaml**: Helm chart metadata
- **values.yaml**: Default configuration values
- **templates/**: Kubernetes manifests with ACM Policy wrappers
- **files/**: Static configuration files (e.g., MetalLB configs)

### Policy Template Pattern
Policies use ACM Policy resources with Placement and PlacementBinding to target specific cluster sets:
- Policy: Defines the desired state
- Placement: Selects target clusters using cluster sets
- PlacementBinding: Links Policy to Placement

### AutoShift Policy Patterns

AutoShift policies follow these standard patterns:

1. **OperatorPolicy Pattern** (90% of policies):
   ```yaml
   - objectDefinition:
       apiVersion: policy.open-cluster-management.io/v1beta1
       kind: OperatorPolicy
       spec:
         subscription:
           channel: '{{hub index .ManagedClusterLabels "autoshift.io/component-channel" | default "stable" hub}}'
   ```

2. **ConfigurationPolicy Pattern** (post-installation configuration):
   ```yaml
   - objectDefinition:
       apiVersion: policy.open-cluster-management.io/v1
       kind: ConfigurationPolicy
       spec:
         object-templates:
           - complianceType: musthave
   ```

3. **Advanced object-templates-raw Pattern** (complex resource generation):
   ```yaml
   spec:
     object-templates-raw: |
       {{- $zones := list {{hub range $label, $value := .ManagedClusterLabels hub}}... }}
   ```

### Policy Template Structure

**IMPORTANT**: AutoShift policies are **always rendered** without conditional wrapping. Control is via placement label selectors, not helm template conditionals.

❌ **Incorrect** (don't use conditional rendering):
```yaml
{{- if (.Values.component.enabled | default false) }}
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
# ... policy content
{{- end }}
```

✅ **Correct** (always render, control via placement):
```yaml
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-component-install
# ... policy content with placement targeting specific labels
```

### ACM Two-Stage Templating

ACM uses a two-stage templating process:
1. **Helm processes** `{{ .Values }}` syntax first
2. **ACM processes** `{{hub}}` functions second

When using hub functions in Helm templates, escape them properly:

```yaml
# For simple hub functions:
channel: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/component-channel" | default "{{ .Values.component.channel }}" {{ "hub}}" }}'

# For object-templates-raw sections (complex):
object-templates-raw: |
  {{ "{{-" }} $variable := {{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/label" {{ "hub}}" }} {{ "}}" }}
  {{ "{{-" }} range $item := $variable {{ "}}" }}
  - complianceType: musthave
    objectDefinition:
      name: example-{{ "{{" }} $item {{ "}}" }}
  {{ "{{-" }} end {{ "}}" }}
```

## ACM Policy Capabilities

### Policy Types

1. **OperatorPolicy** (Preferred for operator management):
   - Manages complete operator lifecycle (subscription, operator group, install plans)
   - Handles upgrade approval settings and channel management
   - Automatically manages dependencies between operators
   - Monitors ClusterServiceVersion (CSV) status
   - Use for 90% of AutoShift operator installations

2. **ConfigurationPolicy** (General resource management):
   - Creates, updates, or enforces any Kubernetes resource
   - Supports multiple compliance types: `musthave`, `mustonlyhave`, `mustnothave`
   - Can use `object-templates` or `object-templates-raw` for templating
   - Enables complex validation and conditional logic
   - Use for configuration, post-install setup, custom resources

3. **CertificatePolicy** (Certificate management):
   - Validates certificate expiration and compliance
   - Monitors certificate chains and authorities
   - Ensures certificate rotation policies
   - Used for security compliance in AutoShift

4. **IamPolicy** (Identity and Access Management):
   - Enforces RBAC and security policies
   - Validates user permissions and roles
   - Ensures compliance with identity standards
   - Integrates with OpenShift authentication

5. **PolicySet** (Policy grouping and coordination):
   - Groups related policies for coordinated deployment
   - Manages dependencies between multiple policies
   - Provides unified status reporting
   - Enables complex governance scenarios

### Remediation Actions

- **enforce**: Automatically apply/create/update resources to match desired state
- **inform**: Only report compliance status, don't make changes
- **enforceWhereSupported**: Enforce where possible, inform elsewhere

### Compliance Types

- **musthave**: Resource must exist with at least the specified fields
- **mustonlyhave**: Resource must exist exactly as specified (removes extra fields)
- **mustnothave**: Resource must not exist

### Policy Dependencies

Use dependencies to ensure proper ordering:
```yaml
spec:
  dependencies:
    - name: policy-prerequisite
      namespace: open-cluster-policies
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy
```

### Hub Template Functions

Available in `{{hub}}` functions:
- **index .ManagedClusterLabels**: Access cluster labels and annotations
- **lookup**: Query existing resources on the cluster
- **range**: Iterate over collections  
- **if/else**: Conditional logic and ternary operators
- **default**: Provide fallback values
- **print, printf**: String formatting and concatenation
- **substr, upper, lower**: String manipulation
- **b64enc, b64dec**: Base64 encoding/decoding
- **eq, ne, lt, gt**: Comparison operators
- **and, or, not**: Logical operators
- **len**: Get length of strings, arrays, or maps
- **hasPrefix, hasSuffix, contains**: String matching functions
- **split, join**: String array operations
- **toYaml, fromYaml**: YAML serialization/deserialization
- **env**: Access environment variables (hub cluster context)

### Advanced Template Processing

1. **Multi-Resource Generation**: Create multiple resources dynamically
2. **Conditional Resource Types**: Generate different resource types based on conditions  
3. **Complex Data Transformation**: Process and transform cluster data
4. **Cross-Resource Dependencies**: Reference data from other cluster resources
5. **Environment-Specific Configuration**: Adapt policies based on cluster environment
6. **Validation Logic**: Implement custom validation within policies

### Advanced Templating Features

1. **Dynamic Resource Generation**:
   ```yaml
   object-templates-raw: |
     {{- range $zone := $zones }}
     - complianceType: musthave
       objectDefinition:
         metadata:
           name: resource-{{ $zone }}
     {{- end }}
   ```

2. **Conditional Resource Creation**:
   ```yaml
   {{- if eq $provider "aws" }}
   - complianceType: musthave
     objectDefinition: # AWS-specific resource
   {{- end }}
   ```

3. **Cluster Information Lookup**:
   ```yaml
   {{- $infrastructure_id := (lookup "config.openshift.io/v1" "Infrastructure" "" "cluster").status.infrastructureName }}
   ```

### Policy Status and Monitoring

- Policies report compliance status: `Compliant`, `NonCompliant`, `Pending`
- Use `oc get policies -A` to check status across clusters
- Monitor via ACM governance dashboard
- Policy violations generate events and alerts

### Policy Evaluation and Status

- **Compliance States**: `Compliant`, `NonCompliant`, `Pending`, `Unknown`
- **Real-time Monitoring**: Continuous policy evaluation and status updates
- **Event Generation**: Policy violations trigger Kubernetes events
- **Detailed Reporting**: Granular compliance information per cluster
- **History Tracking**: Maintains compliance state changes over time

### Policy Automation

1. **GitOps Integration**: Automated policy deployment via ArgoCD ApplicationSets
2. **Template Processing**: Dynamic policy generation based on cluster attributes
3. **Bulk Operations**: Manage policies across multiple clusters simultaneously
4. **Helm-based Deployment**: Policies deployed as Helm charts through AutoShift framework

### Security and Governance Features

- **Admission Control**: Validate resources before creation
- **Drift Detection**: Monitor configuration changes and unauthorized modifications
- **Compliance Frameworks**: Support for security standards (NIST, PCI, SOX)
- **Audit Trails**: Comprehensive logging of policy actions and changes
- **Risk Assessment**: Automated compliance scoring and risk evaluation

### Best Practices

1. **Use OperatorPolicy for operators** - preferred over ConfigurationPolicy for operator management
2. **Always provide defaults** - use `| default "value"` in hub functions  
3. **Test templates** - verify `helm template` renders correctly
4. **Use proper escaping** - follow two-stage templating patterns
5. **Implement dependencies** - ensure proper installation order
6. **Choose appropriate severity** - `low`, `medium`, `high`, `critical`
7. **Set pruneObjectBehavior** - usually `None` to prevent unwanted deletions
8. **Use PolicySets** - group related policies for complex scenarios
9. **Monitor compliance** - regularly check policy status and violations
10. **Leverage GitOps** - automate policy deployment and updates
11. **Document labels** - clearly define autoshift.io label usage and precedence
12. **Test placement** - verify policies target correct clusters and cluster sets

## Cluster Management

### Cluster Sets
- **Hub Cluster Sets**: Defined in `hubClusterSets` section, manage hub cluster components
- **Managed Cluster Sets**: Defined in `managedClusterSets` section, manage spoke cluster components
- **Individual Clusters**: Override settings in `clusters` section

### Label System
Uses `autoshift.io/` prefixed labels for feature enablement:
- `autoshift.io/metallb: 'true'`
- `autoshift.io/acs-channel: 'stable'`
- `autoshift.io/odf: 'true'`

## Development Workflow

When modifying policies:
1. Update the appropriate policy's `values.yaml` and templates
2. Test changes on development cluster sets first
3. Policies auto-sync via ArgoCD when changes are committed
4. Monitor ACM governance dashboard for policy compliance

## Important Notes

- All policies use Helm templating with Go template syntax
- ArgoCD manages the GitOps workflow, not direct kubectl/oc commands
- Cluster labels override all other configuration sources
- Policy changes propagate automatically through GitOps sync
- Use `oc get policies -A` to check policy status across clusters