# cloudnative-pg AutoShift Policy

## Overview
This policy installs the cloudnative-pg operator by using AutoShift patterns.

## Status
✅ **Operator Installation**: Ready to deploy  
🔧 **Configuration**: Requires operator-specific setup (see below)

## Quick Deploy

### Test Locally
```bash
# A PolicyGenerator directory, not a Helm chart: rendering it needs the ${...}
# placeholders substituted first. The validation suite does that, resolves hub and
# spoke templates, and is what CI runs.
cd tools && go test -tags integration ./internal/resolver/...
```

### Enable on Clusters
Edit AutoShift values files to add the operator labels:

```yaml
# In autoshift/values/clustersets/hub.yaml (or other clusterset files)
hubClusterSets:
  hub:
    labels:
      cloudnative-pg: 'true'
      cloudnative-pg-subscription-name: 'cloudnative-pg'
      cloudnative-pg-channel: 'stable-v1'
      cloudnative-pg-source: 'certified-operators'
      cloudnative-pg-source-namespace: 'openshift-marketplace'
      # cloudnative-pg-version: 'cloudnative-pg.v1.x.x'  # Optional: pin to specific CSV version

managedClusterSets:
  managed:
    labels:
      cloudnative-pg: 'true'
      cloudnative-pg-subscription-name: 'cloudnative-pg'
      cloudnative-pg-channel: 'stable-v1'
      cloudnative-pg-source: 'certified-operators'
      cloudnative-pg-source-namespace: 'openshift-marketplace'
      # cloudnative-pg-version: 'cloudnative-pg.v1.x.x'  # Optional: pin to specific CSV version

# For specific clusters (optional override)
clusters:
  my-cluster:
    labels:
      cloudnative-pg: 'true'
      cloudnative-pg-channel: 'fast'  # Override channel for this cluster
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
- **Version Pinning**: Add `cloudnative-pg-version` label to pin to a specific CSV version
- **Manual Control**: Pinned versions require manual updates to upgrade

To pin to a specific version, set the version label in your clusterset or per-cluster values file:
```yaml
cloudnative-pg-version: 'cloudnative-pg.v1.x.x'
```

Find available CSV versions:
```bash
# List available versions for this operator
oc get packagemanifests cloudnative-pg -o jsonpath='{.status.channels[*].currentCSV}'
```

## Images

AutoShift does not choose PostgreSQL or PgBouncer images. A `Cluster` or `Pooler` that names no image
runs the default compiled into the Operator release. The Operator writes that default into the
`Cluster` `spec.imageName` when it creates the `Cluster`, so an existing database keeps its image
across Operator upgrades and only a new `Cluster` gets the newer default. A `Pooler` keeps no image
in its spec and follows the default of the running Operator. Applications that need a specific image
set it in their own configuration:

| Application | PostgreSQL | PgBouncer |
|---|---|---|
| Red Hat Quay | `config.quay.dbImage` | Not used |
| GitLab | `config.gitlab.dbImage` | `config.gitlab.dbPoolerImage` |
| JFrog Artifactory | `config.jfrog.dbImage` | `config.jfrog.dbPoolerImage` |

Set the PostgreSQL image before the first deployment. A lower major on a running `Cluster` is
rejected, and the database policy reports NonCompliant with `cannot be updated` until the image is
put back. Removing either setting later leaves the last image in place and the policy Compliant,
because `musthave` never removes a field.

### Updating in a maintenance window

Nothing in a database changes until git does, so the window is the merge:

1. Pin the Operator with `cloudnative-pg-version` and raise it only in a window. An Operator
   upgrade restarts every database one instance at a time to replace its instance manager, ends
   with a switchover of each primary, and moves every `Pooler` without `dbPoolerImage` to the new
   default PgBouncer image.
2. Raise `config.<app>.dbImage` in a window. A new minor version rolls the replicas and then the
   primary. A new major version stops the database for an in-place `pg_upgrade`: take a backup first
   and check that the application supports that major.
3. Set `config.<app>.dbPrimaryUpdate: supervised` to hold the switchover of the primary in both
   cases. CloudNativePG updates the replicas and waits until you promote one with
   `oc cnpg promote <cluster> <instance>`. A single-instance database rejects `supervised`, and the
   database policy reports `cannot be updated`.

The Operator does not expose its default image to the cluster, so nothing reports that a newer
PostgreSQL minor version is available. Check the CloudNativePG release notes when you plan a window.

### Disconnected clusters

The Operator pulls its defaults by tag, and its bundle does not list them, so oc-mirror does not
copy them from the catalog. `make generate-imageset` reads both defaults from the release at the
head of each mirrored channel and adds them to `additionalImages`. oc-mirror writes an
`ImageTagMirrorSet` for them in `itms-oc-mirror.yaml`. When AutoShift manages the mirror
configuration instead, add a `tagMirrors` entry for `ghcr.io/cloudnative-pg` under
`config.disconnected`. An image set in application configuration is not added: mirror it yourself.
Keep the earlier defaults in the mirror registry after an Operator upgrade, because existing
databases still pull the image they were created with.

## Next Steps: Configuration

### 1. Explore Installed CRDs
After operator installation, check what Custom Resources are available:
```bash
# Wait for operator to install
oc get pods -n openshift-operators

# Check available CRDs
oc get crds | grep cloudnative-pg

# Explore CRD specifications
oc explain <CustomResourceName>
```

### 2. Create Configuration Policies
Add operator-specific configuration policies to `templates/` directory.

#### Common Patterns:
- `policy-cloudnative-pg-config.yaml` - Main configuration
- `policy-cloudnative-pg-<feature>.yaml` - Feature-specific configs

#### Template Structure:
```yaml
{{- $policyName := "policy-cloudnative-pg-config" }}
{{- $placementName := "placement-policy-cloudnative-pg-config" }}

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
    - name: policy-cloudnative-pg-operator-install
      namespace: {{ .Values.policy_namespace }}
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: cloudnative-pg-config
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
                  name: cloudnative-pg-config
                  namespace: {{ .Values.cloudnativePg.namespace }}
                spec:
                  # Your operator-specific configuration
                  # Use dynamic labels when needed:
                  # setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/cloudnative-pg-setting" | default "default-value" {{ "hub}}" }}'
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
            - key: 'autoshift.io/cloudnative-pg'
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
- **Simple**: `policies/stable/openshift-gitops/` - Basic operator + ArgoCD config
- **Medium**: `policies/stable/advanced-cluster-security/` - Multiple related policies
- **Complex**: `policies/stable/metallb/` - Multiple configuration types (L2, BGP, etc.)
- **Advanced**: `policies/stable/openshift-data-foundation/` - Storage cluster configuration

### 4. AutoShift Labels
Add configuration labels to `values.yaml` and use in templates:

```yaml
# Add to values.yaml AutoShift Labels Documentation:
# cloudnative-pg-setting<string>: Configuration option (default: 'value')
# cloudnative-pg-feature-enabled<bool>: Enable optional feature (default: 'false')
# cloudnative-pg-provider<string>: Provider-specific config (default: 'generic')

# Use in templates:
setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/cloudnative-pg-setting" | default "default-value" {{ "hub}}" }}'
```

## Common Patterns

### CSV Status Checking (Optional)
For operators that need installation verification:
```yaml
- objectDefinition:
    apiVersion: policy.open-cluster-management.io/v1
    kind: ConfigurationPolicy
    metadata:
      name: cloudnative-pg-csv-status
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
              namespace: {{ .Values.cloudnativePg.namespace }}
            status:
              phase: Succeeded
```

## Troubleshooting

### Policy Not Applied
1. Check cluster labels: `oc get managedcluster <cluster> --show-labels`
2. Verify placement: `oc get placement -n open-cluster-policies`
3. Check policy status: `oc describe policy policy-cloudnative-pg-operator-install`

### Operator Installation Issues
1. Check subscription: `oc get subscription -n openshift-operators`
2. Check install plan: `oc get installplan -n openshift-operators`
3. Verify operator source exists: `oc get catalogsource -n openshift-marketplace`

### Template Rendering Issues
1. Validate rendering: `cd tools && go test -tags integration ./internal/resolver/...`
2. Check hub escaping: Look for `{{ "{{hub" }} ... {{ "hub}}" }}` patterns
3. Read the failure: the suite names the chart and the stage that failed (render, hub resolution, spoke resolution, YAML validation, label contract)

## Resources
- [Operator Documentation](https://operatorhub.io/operator/cloudnative-pg) - Find your operator details
- [AutoShift Developer Guide](../../../docs/developer-guide.md) - Comprehensive policy development guide
- [ACM Policy Documentation](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes) - Policy syntax reference in Governence Section
- [Similar Policies](../../README.md) - Browse other policies for patterns and examples