# servicemesh3operator AutoShift Policy

## Overview
This policy installs the **Sail Operator** (servicemeshoperator3) for OpenShift Service Mesh 3.x using AutoShift patterns.

**Scope**: This policy handles **operator installation only**. For mesh configuration (Istio control plane, ambient mode, Kiali, tracing), see the **servicemesh3-ambient** policy.

## Status
✅ **Operator Installation**: Ready to deploy  
🔧 **Configuration**: Requires operator-specific setup (see below)

## Quick Deploy

### Test Locally
```bash
# Validate policy renders correctly
helm template policies/servicemesh3operator/
```

### Enable on Clusters
Edit AutoShift values files to add the operator labels:

```yaml
# In autoshift/values/clustersets/hub.yaml (or other clusterset files)
hubClusterSets:
  hub:
    labels:
      servicemesh3operator: 'true'
      servicemesh3operator-subscription-name: 'servicemeshoperator3'
      servicemesh3operator-channel: 'stable'
      servicemesh3operator-source: 'redhat-operators'
      servicemesh3operator-source-namespace: 'openshift-marketplace'
      # servicemesh3operator-version: 'servicemeshoperator3.v1.x.x'  # Optional: pin to specific CSV version

managedClusterSets:
  managed:
    labels:
      servicemesh3operator: 'true'
      servicemesh3operator-subscription-name: 'servicemeshoperator3'
      servicemesh3operator-channel: 'stable'
      servicemesh3operator-source: 'redhat-operators'
      servicemesh3operator-source-namespace: 'openshift-marketplace'
      # servicemesh3operator-version: 'servicemeshoperator3.v1.x.x'  # Optional: pin to specific CSV version

# For specific clusters (optional override)
clusters:
  my-cluster:
    labels:
      servicemesh3operator: 'true'
      servicemesh3operator-channel: 'fast'  # Override channel for this cluster
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
- **Version Pinning**: Add `servicemesh3operator-version` label to pin to a specific CSV version
- **Manual Control**: Pinned versions require manual updates to upgrade

To pin to a specific version, set the version label in your clusterset or per-cluster values file:
```yaml
servicemesh3operator-version: 'servicemeshoperator3.v1.x.x'
```

Find available CSV versions:
```bash
# List available versions for this operator
oc get packagemanifests servicemeshoperator3 -o jsonpath='{.status.channels[*].currentCSV}'
```

## Next Steps: Configuration

After operator installation, configure the service mesh using the `servicemesh3-ambient` policy, which deploys Istio in ambient mode, Kiali, and distributed tracing.

## Common Patterns

### CSV Status Checking (Optional)
For operators that need installation verification:
```yaml
- objectDefinition:
    apiVersion: policy.open-cluster-management.io/v1
    kind: ConfigurationPolicy
    metadata:
      name: servicemesh3-csv-status
    spec:
      remediationAction: inform
      severity: high
      object-templates:
        - complianceType: musthave
          objectDefinition:
            apiVersion: operators.coreos.com/v1alpha1
            kind: ClusterServiceVersion
            metadata:
              namespace: {{ .Values.servicemesh3operator.namespace }}
            status:
              phase: Succeeded
```

## Troubleshooting

### Policy Not Applied
1. Check cluster labels: `oc get managedcluster <cluster> --show-labels`
2. Verify placement: `oc get placement -n open-cluster-policies`
3. Check policy status: `oc describe policy policy-servicemesh3-operator-install`

### Operator Installation Issues
1. Check subscription: `oc get subscription -n openshift-operators`
2. Check install plan: `oc get installplan -n openshift-operators`
3. Verify operator source exists: `oc get catalogsource -n openshift-marketplace`

### Template Rendering Issues
1. Test locally: `helm template policies/servicemesh3operator/`
2. Check hub escaping: Look for `{{ "{{hub" }} ... {{ "hub}}" }}` patterns
3. Validate YAML: `helm lint policies/servicemesh3operator/`

## Resources
- [Operator Documentation](https://operatorhub.io/operator/servicemeshoperator3) - Find your operator details
- [AutoShift Developer Guide](../../docs/developer-guide.md) - Comprehensive policy development guide
- [ACM Policy Documentation](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes) - Policy syntax reference in Governence Section
- [Similar Policies](../) - Browse other policies for patterns and examples
