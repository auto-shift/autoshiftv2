# external-secrets-operator AutoShift Policy

## Overview
This policy installs the external-secrets-operator operator using AutoShift patterns.

## Status
✅ **Operator Installation**: Ready to deploy  
🔧 **Configuration**: Requires operator-specific setup (see below)

## Quick Deploy

### Test Locally
```bash
# Validate policy renders correctly
helm template policies/external-secrets-operator/
```

### Enable on Clusters
Edit AutoShift values files to add the operator labels:

```yaml
# In autoshift/values/clustersets/hub.yaml (or other clusterset files)
hubClusterSets:
  hub:
    labels:
      external-secrets-operator: 'true'
      external-secrets-operator-subscription-name: 'openshift-external-secrets-operator'
      external-secrets-operator-channel: 'stable-v1'
      external-secrets-operator-source: 'redhat-operators'
      external-secrets-operator-source-namespace: 'openshift-marketplace'
      # external-secrets-operator-version: 'external-secrets-operator.v1.x.x'  # Optional: pin to specific CSV version

managedClusterSets:
  managed:
    labels:
      external-secrets-operator: 'true'
      external-secrets-operator-subscription-name: 'openshift-external-secrets-operator'
      external-secrets-operator-channel: 'stable-v1'
      external-secrets-operator-source: 'redhat-operators'
      external-secrets-operator-source-namespace: 'openshift-marketplace'
      # external-secrets-operator-version: 'external-secrets-operator.v1.x.x'  # Optional: pin to specific CSV version

# For specific clusters (optional override)
clusters:
  my-cluster:
    labels:
      external-secrets-operator: 'true'
      external-secrets-operator-channel: 'fast'  # Override channel for this cluster
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
- **Version Pinning**: Add `external-secrets-operator-version` label to pin to a specific CSV version
- **Manual Control**: Pinned versions require manual updates to upgrade

To pin to a specific version, set the version label in your clusterset or per-cluster values file:
```yaml
external-secrets-operator-version: 'external-secrets-operator.v1.x.x'
```

Find available CSV versions:
```bash
# List available versions for this operator
oc get packagemanifests external-secrets-operator -o jsonpath='{.status.channels[*].currentCSV}'
```

## SecretStore / ClusterSecretStore Configuration

Beyond installing the operator, this policy renders ESO `SecretStore` and `ClusterSecretStore`
resources from per-cluster rendered-config ConfigMaps. Real config lives in cluster/clusterset
values files under `config.eso.stores`; the stores policy reads each cluster's rendered-config
at runtime and emits matching resources.

### Enabling

The single label `autoshift.io/external-secrets-operator: 'true'` opts a cluster into both the
operator install policy and the stores policy. A cluster that has the label but no
`config.eso.stores` entries gets the operator installed and an empty stores policy — no stores
rendered, no harm done.

### Supported providers

The chart's accepted provider list is configured in `values.yaml` under
`esoStores.validProviders` and currently covers:

- `vault` (HashiCorp Vault)
- `aws` (AWS Secrets Manager / Parameter Store)
- `azurekv` (Azure Key Vault)
- `gcpsm` (Google Cloud Secret Manager)
- `kubernetes` (remote/local Kubernetes Secret store)

Per-provider field schemas, auth methods, and minimal vs fully-fleshed examples are documented
in `values.yaml` as commented YAML. Adding a new provider requires both updating
`esoStores.validProviders` and confirming the rendering path passes through the user's
`provider.<type>` block as-is (it does — there is no per-provider field-level logic).

### Per-store cluster filtering

Each store entry may carry a `clusterSelector` block evaluated at runtime against
`.ManagedClusterLabels` on the receiving cluster. Both `matchLabels` and `matchExpressions`
(operators `In`, `NotIn`, `Exists`, `DoesNotExist`) are supported. Entries whose selector does
not match the current cluster are skipped silently. Entries with no `clusterSelector` always
render.

This lets a single clusterset-level config declare a store and restrict it to a subset of
clusters in the set without splitting the config.

### SecretStore vs ClusterSecretStore

Each entry's `kind` field selects between namespaced `SecretStore` (per-namespace, lives in
`namespace`) and cluster-scoped `ClusterSecretStore` (no `metadata.namespace`, optional
`conditions[]` to restrict consuming namespaces). Default is `ClusterSecretStore`, configured
under `esoStores.defaults.kind` in `values.yaml`.

For `SecretStore` entries, the target namespace falls back to `esoStores.defaults.namespace`
(default: `external-secrets-operator`) when the entry omits `.namespace`.

For `ClusterSecretStore`, any `*SecretRef.namespace` and `caProvider.namespace` fields inside
the provider block are required (the cluster store has no namespace to inherit from). The chart
does not enforce this — ESO surfaces a runtime error if missing.

### Validation

Two layers, both sharing `esoStores.validProviders`:

- **Chart-render time** (Helm): rejects inline default stores in `.Values.esoStores.stores` that
  are missing `.name`, declare zero or multiple keys under `.provider`, or name a provider not
  in `validProviders`. Failure aborts `helm template` with a descriptive error.
- **Hub-template runtime** (per-cluster): re-runs the same checks against the live store list
  pulled from rendered-config. Any malformed entry causes the entire stores policy to fail to
  render on that cluster, surfacing as **whole-policy noncompliance** with the validation error
  visible in policy status. Bad entries do not partially apply.

### Example: clusterset config with two stores

```yaml
# In an autoshift clusterset values file
config:
  eso:
    stores:
      - name: vault-shared
        kind: ClusterSecretStore
        clusterSelector:
          matchLabels:
            tier: prod
        conditions:
          - namespaceSelector:
              matchLabels:
                eso-tier: shared
        provider:
          vault:
            server: https://vault.corp.example.com:8200
            path: kv
            version: v2
            auth:
              kubernetes:
                mountPath: kubernetes
                role: shared-reader
                serviceAccountRef:
                  name: eso-vault-sa
                  namespace: external-secrets-operator
      - name: aws-team-a
        kind: SecretStore
        namespace: team-a
        provider:
          aws:
            service: SecretsManager
            region: us-east-1
            auth:
              jwt:
                serviceAccountRef:
                  name: team-a-irsa-sa
```

The first store deploys only to clusters labeled `tier: prod` and is consumable from any
namespace labeled `eso-tier: shared`. The second deploys everywhere in the clusterset and is
namespaced to `team-a`.

## Next Steps: Configuration

### 1. Explore Installed CRDs
After operator installation, check what Custom Resources are available:
```bash
# Wait for operator to install
oc get pods -n external-secrets-operator

# Check available CRDs
oc get crds | grep external-secrets-operator

# Explore CRD specifications
oc explain <CustomResourceName>
```

### 2. Create Configuration Policies
Add operator-specific configuration policies to `templates/` directory.

#### Common Patterns:
- `policy-external-secrets-operator-config.yaml` - Main configuration
- `policy-external-secrets-operator-<feature>.yaml` - Feature-specific configs

#### Template Structure:
```yaml
{{- $policyName := "policy-external-secrets-operator-config" }}
{{- $placementName := "placement-policy-external-secrets-operator-config" }}

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
    - name: policy-external-secrets-operator-install
      namespace: {{ .Values.policy_namespace }}
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: external-secrets-operator-config
        spec:
          remediationAction: enforce
          severity: high
          evaluationInterval:
            compliant: {{ ((($.Values.autoshift).evaluationInterval).compliant) | default "10m" }}
            noncompliant: {{ ((($.Values.autoshift).evaluationInterval).noncompliant) | default "30s" }}
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: # Your operator's API version
                kind: # Your operator's Custom Resource
                metadata:
                  name: external-secrets-operator-config
                  namespace: {{ .Values.externalSecretsOperator.namespace }}
                spec:
                  # Your operator-specific configuration
                  # Use dynamic labels when needed:
                  # setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/external-secrets-operator-setting" | default "default-value" {{ "hub}}" }}'
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
            - key: 'autoshift.io/external-secrets-operator'
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
# external-secrets-operator-setting<string>: Configuration option (default: 'value')
# external-secrets-operator-feature-enabled<bool>: Enable optional feature (default: 'false')
# external-secrets-operator-provider<string>: Provider-specific config (default: 'generic')

# Use in templates:
setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/external-secrets-operator-setting" | default "default-value" {{ "hub}}" }}'
```

## Common Patterns

### CSV Status Checking (Optional)
For operators that need installation verification:
```yaml
- objectDefinition:
    apiVersion: policy.open-cluster-management.io/v1
    kind: ConfigurationPolicy
    metadata:
      name: external-secrets-operator-csv-status
    spec:
      remediationAction: inform
      severity: high
      evaluationInterval:
        compliant: {{ ((($.Values.autoshift).evaluationInterval).compliant) | default "10m" }}
        noncompliant: {{ ((($.Values.autoshift).evaluationInterval).noncompliant) | default "30s" }}
      object-templates:
        - complianceType: musthave
          objectDefinition:
            apiVersion: operators.coreos.com/v1alpha1
            kind: ClusterServiceVersion
            metadata:
              namespace: {{ .Values.externalSecretsOperator.namespace }}
            status:
              phase: Succeeded
```

## Troubleshooting

### Policy Not Applied
1. Check cluster labels: `oc get managedcluster <cluster> --show-labels`
2. Verify placement: `oc get placement -n open-cluster-policies`
3. Check policy status: `oc describe policy policy-external-secrets-operator-install`

### Operator Installation Issues
1. Check subscription: `oc get subscription -n external-secrets-operator`
2. Check install plan: `oc get installplan -n external-secrets-operator`
3. Verify operator source exists: `oc get catalogsource -n openshift-marketplace`

### Template Rendering Issues
1. Test locally: `helm template policies/external-secrets-operator/`
2. Check hub escaping: Look for `{{ "{{hub" }} ... {{ "hub}}" }}` patterns
3. Validate YAML: `helm lint policies/external-secrets-operator/`

## Resources
- [Operator Documentation](https://operatorhub.io/operator/external-secrets-operator) - Find your operator details
- [AutoShift Developer Guide](../../docs/developer-guide.md) - Comprehensive policy development guide
- [ACM Policy Documentation](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes) - Policy syntax reference in Governence Section
- [Similar Policies](../) - Browse other policies for patterns and examples