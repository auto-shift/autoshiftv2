# node-feature-discovery AutoShift Policy

## Overview
This policy installs the nfd operator using AutoShift patterns.

## Status
✅ **Operator Installation**: Ready to deploy  
🔧 **Configuration**: Requires operator-specific setup (see below)

## Quick Deploy

### Test Locally
```bash
# Validate policy renders correctly
helm template policies/node-feature-discovery/
```

### Enable on Clusters
Edit AutoShift values files to add the operator labels:

```yaml
# In autoshift/values/clustersets/hub.yaml (or other clusterset files)
hubClusterSets:
  hub:
    labels:
      node-feature-discovery: 'true'
      node-feature-discovery-subscription-name: 'nfd'
      node-feature-discovery-channel: 'stable'
      node-feature-discovery-source: 'redhat-operators'
      node-feature-discovery-source-namespace: 'openshift-marketplace'
      # node-feature-discovery-version: 'nfd.v1.x.x'  # Optional: pin to specific CSV version

managedClusterSets:
  managed:
    labels:
      node-feature-discovery: 'true'
      node-feature-discovery-subscription-name: 'nfd'
      node-feature-discovery-channel: 'stable'
      node-feature-discovery-source: 'redhat-operators'
      node-feature-discovery-source-namespace: 'openshift-marketplace'
      # node-feature-discovery-version: 'nfd.v1.x.x'  # Optional: pin to specific CSV version

# For specific clusters (optional override)
clusters:
  my-cluster:
    labels:
      node-feature-discovery: 'true'
      node-feature-discovery-channel: 'fast'  # Override channel for this cluster
```

Labels are defined in values files only — never directly on managed clusters. The cluster-labels policy handles propagating these labels from the values files to managed clusters.

### AutoShift Policy Discovery
New policies are automatically discovered by the ApplicationSet. In Git mode, the ApplicationSet uses a `policies/*` wildcard to pick up all subdirectories. No manual registration is required — simply adding your policy folder under `policies/` is sufficient.

## Configuration

### Namespace Scope
This operator is configured as:
- **Cluster-scoped**: Manages resources across all namespaces (default)
- **Namespace-scoped**: Limited to specific target namespaces (if `targetNamespaces` enabled in values.yaml)

To change scope, edit `values.yaml` and uncomment/configure the `targetNamespaces` field.

### Version Control
This policy supports AutoShift's operator version control system:

- **Automatic Upgrades**: By default, the operator follows automatic upgrade paths within its channel
- **Version Pinning**: Add `node-feature-discovery-version` label to pin to a specific CSV version
- **Manual Control**: Pinned versions require manual updates to upgrade

To pin to a specific version, set the version label in your clusterset or per-cluster values file:
```yaml
node-feature-discovery-version: 'nfd.v1.x.x'
```

Find available CSV versions:
```bash
# List available versions for this operator
oc get packagemanifests nfd -o jsonpath='{.status.channels[*].currentCSV}'
```

## Next Steps: Configuration

### 1. Explore Installed CRDs
After operator installation, check what Custom Resources are available:
```bash
# Wait for operator to install
oc get pods -n openshift-nfd

# Check available CRDs
oc get crds | grep node-feature-discovery

# Explore CRD specifications
oc explain <CustomResourceName>
```

### 2. Create Configuration Policies
Add operator-specific configuration policies to `templates/` directory.

#### Common Patterns:
- `policy-node-feature-discovery-config.yaml` - Main configuration
- `policy-node-feature-discovery-<feature>.yaml` - Feature-specific configs

#### Template Structure:
```yaml
{{- $policyName := "policy-node-feature-discovery-config" }}
{{- $placementName := "placement-policy-node-feature-discovery-config" }}

apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: {{ $policyName }}
  namespace: {{ .Values.policy_namespace }}
  annotations:
    policy.open-cluster-management.io/standards: NIST SP 800-53
    policy.open-cluster-management.io/categories: CM Configuration Management
    policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
spec:
  disabled: false
  dependencies:
    - name: policy-node-feature-discovery-operator-install
      namespace: {{ .Values.policy_namespace }}
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: node-feature-discovery-config
        spec:
          remediationAction: enforce
          severity: high
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: # Your operator's API version
                kind: # Your operator's Custom Resource
                metadata:
                  name: node-feature-discovery-config
                  namespace: {{ .Values.nodeFeatureDiscovery.namespace }}
                spec:
                  # Your operator-specific configuration
                  # Use dynamic labels when needed:
                  # setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/node-feature-discovery-setting" | default "default-value" {{ "hub}}" }}'
          pruneObjectBehavior: None
---
# Use same placement as operator install or create specific targeting
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: {{ $placementName }}
  namespace: {{ .Values.policy_namespace }}
spec:
  clusterSets:
  {{- range $clusterSet, $value := $.Values.hubClusterSets }}
    - {{ $clusterSet }}
  {{- end }}
  {{- range $clusterSet, $value := $.Values.managedClusterSets }}
    - {{ $clusterSet }}
  {{- end }}
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchExpressions:
            - key: 'autoshift.io/node-feature-discovery'
              operator: In
              values:
              - 'true'
  tolerations:
    - key: cluster.open-cluster-management.io/unreachable
      operator: Exists
    - key: cluster.open-cluster-management.io/unavailable
      operator: Exists
---
apiVersion: policy.open-cluster-management.io/v1
kind: PlacementBinding
metadata:
  name: {{ $placementName }}
  namespace: {{ .Values.policy_namespace }}
placementRef:
  name: {{ $placementName }}
  apiGroup: cluster.open-cluster-management.io
  kind: Placement
subjects:
  - name: {{ $policyName }}
    apiGroup: policy.open-cluster-management.io
    kind: Policy
```

### 3. Reference Examples
**Study similar complexity policies:**
- **Simple**: `policies/openshift-gitops/` - Basic operator + ArgoCD config
- **Medium**: `policies/advanced-cluster-security/` - Multiple related policies
- **Complex**: `policies/metallb/` - Multiple configuration types (L2, BGP, etc.)
- **Advanced**: `policies/openshift-data-foundation/` - Storage cluster configuration

### 4. AutoShift Labels
Add configuration labels to `values.yaml` and use in templates:

```yaml
# Add to values.yaml AutoShift Labels Documentation:
# node-feature-discovery-setting<string>: Configuration option (default: 'value')
# node-feature-discovery-feature-enabled<bool>: Enable optional feature (default: 'false')
# node-feature-discovery-provider<string>: Provider-specific config (default: 'generic')

# Use in templates:
setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/node-feature-discovery-setting" | default "default-value" {{ "hub}}" }}'
```

## Common Patterns

### CSV Status Checking (Optional)
For operators that need installation verification:
```yaml
- objectDefinition:
    apiVersion: policy.open-cluster-management.io/v1
    kind: ConfigurationPolicy
    metadata:
      name: node-feature-discovery-csv-status
    spec:
      remediationAction: inform
      severity: high
      object-templates:
        - complianceType: musthave
          objectDefinition:
            apiVersion: operators.coreos.com/v1alpha1
            kind: ClusterServiceVersion
            metadata:
              namespace: {{ .Values.nodeFeatureDiscovery.namespace }}
            status:
              phase: Succeeded
```

### ArgoCD Sync Annotations (If Needed)
For policies requiring special sync behavior:
```yaml
annotations:
  argocd.argoproj.io/sync-options: Prune=false,SkipDryRunOnMissingResource=true
  argocd.argoproj.io/compare-options: IgnoreExtraneous
  argocd.argoproj.io/sync-wave: "1"
```

## Troubleshooting

### Policy Not Applied
1. Check cluster labels: `oc get managedcluster <cluster> --show-labels`
2. Verify placement: `oc get placement -n open-cluster-policies`
3. Check policy status: `oc describe policy policy-node-feature-discovery-operator-install`

### Operator Installation Issues
1. Check subscription: `oc get subscription -n openshift-nfd`
2. Check install plan: `oc get installplan -n openshift-nfd`
3. Verify operator source exists: `oc get catalogsource -n openshift-marketplace`

### Template Rendering Issues
1. Test locally: `helm template policies/node-feature-discovery/`
2. Check hub escaping: Look for `{{ "{{hub" }} ... {{ "hub}}" }}` patterns
3. Validate YAML: `helm lint policies/node-feature-discovery/`

## Resources
- [Operator Documentation](https://operatorhub.io/operator/nfd) - Find your operator details
- [AutoShift Developer Guide](../../docs/developer-guide.md) - Comprehensive policy development guide
- [ACM Policy Documentation](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes) - Policy syntax reference in Governence Section
- [Similar Policies](../) - Browse other policies for patterns and examples