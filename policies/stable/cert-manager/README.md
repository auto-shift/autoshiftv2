# cert-manager AutoShift Policy

## Overview
This policy installs the openshift-cert-manager-operator operator using AutoShift patterns, and
provisions a built-in CA (`policy-cert-manager-ca`).

## Built-in CA (`autoshift-ca`)
`policy-cert-manager-ca` creates the `autoshift-ca` ClusterIssuer — the default TLS issuer components use
via their `tlsIssuer` (GitLab, Keycloak, Vault, …). The **signer is a ref**, so you choose the trust root:

**Placement is a label; the issuer ref (data) is in `config.certManager.ca.issuer`.**

| `config.certManager.ca.issuer.name` | Result |
|---|---|
| `autoshift-selfsigned` (default) | A **self-signed root** is generated (the bootstrap selfSigned issuer is created only in this case). |
| your `Issuer`/`ClusterIssuer` name | `autoshift-ca` becomes an **intermediate CA signed by your issuer** — cert-manager does the signing; AutoShift never handles your CA key. |

- `.issuer.kind` (default `ClusterIssuer`) and `.issuer.group` (default `cert-manager.io`) complete the ref —
  set `.group` for external issuers (Venafi, step-ca, AWS PCA).
- The signer must be able to issue a **CA cert** (`isCA`) — a `ca` issuer over your root / Venafi / step.
  Public ACME **cannot** (it won't issue CA certs; use ACME for leaf/Route certs, not the CA).
- **Opt out** entirely with `autoshift.io/cert-manager-ca: 'false'` (handled by placement) — then components
  must set their own `tlsIssuer`.
- **Two-tier note**: if every cluster's `autoshift-ca` chains to the same enterprise root, spokes trust the
  hub automatically (no CA distribution needed).

## Cluster serving certs — API & Ingress (opt-in, safe, gated)

cert-manager can also manage the cluster's two external serving certs. Both are **opt-in, default OFF** (the
**enable is a label**; issuer refs + SANs are **data in `config.certManager`**), and **separately gated**
because they have very different blast radius:

| | `cert-manager-api-cert` (label) | `cert-manager-ingress-cert` (label) |
|---|---|---|
| Target | `APIServer/cluster` `namedCertificates` for **`api.<domain>`** | `IngressController/default` `defaultCertificate` (**`*.apps.<domain>`** + bare `apps.<domain>`) |
| Behavior | **Additive + SNI** — internal cert & `api-int` untouched | **Replaces** the wildcard (console, CLI, all routes) |
| Risk | Low | Higher |
| Config | `config.certManager.apiCert.{issuer,extraSANs}` | `config.certManager.ingressCert.{issuer,extraSANs}` |

Issuer defaults to `autoshift-ca`; on a cluster with a real ACME issuer set e.g.
`config.certManager.apiCert.issuer.name: zerossl-production-aws`. Secret/Certificate names are
`cert-manager-api-cert` / `cert-manager-ingress-cert`.

**Why it's safe / how it falls back:**
- **Readiness gate** — the policy always creates the cert-manager `Certificate`, but only patches the
  APIServer/IngressController **once that `Certificate` is `Ready`**. A failed issuance leaves the cluster on
  its default cert (never points it at a cert that hasn't issued).
- **Additive merge** (`musthave`) — doesn't clobber other operator-managed spec fields.
- **Non-disruptive rotation** — cert-manager keeps the same secret name; the router hot-reloads and the API
  server dynamically reloads. Only the *first* API add rolls one kube-apiserver revision (no reboot).
- **Later failure = alert, not auto-revert** — `policy-cert-manager-{api,ingress}-cert-ready` (inform)
  surface a `Certificate` going NotReady. We deliberately do **not** auto-remove the patch (un-merging would
  flap kube-apiserver revisions). To revert, remove the field from the CR (or set the label `false` and
  delete the `namedCertificate`/`defaultCertificate` entry) — a manual, deliberate step.
- ⚠️ **Never** name-cert the internal `api-int.<domain>` — it degrades the cluster. The policy only ever
  templates `api.<domain>`.

**Self-signed CA + Ingress — trust companion.** If the ingress issuer is the self-signed `autoshift-ca`, the
cluster's internal clients (console → routes) must also **trust** that CA, or the console breaks. Per the
Red Hat *Configuring certificates* guide, create a `custom-ca` ConfigMap of the CA in `openshift-config` and
patch `proxy/cluster` `spec.trustedCA.name: custom-ca`. Caveats: `trustedCA` is a cluster **singleton**
(conflicts if you already manage it) and applying it triggers a brief per-node kubelet/CRI-O restart. This
step is **not automated** here (default-off, disruptive) — prefer a **real/enterprise issuer for ingress**
(`cert-manager-ingress-cert-issuer`) to avoid it entirely.

**Rollout order:** enable `cert-manager-api-cert` first (low risk), confirm `oc get co kube-apiserver`
settles (PROGRESSING True→False), then `cert-manager-ingress-cert`.

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
      cert-manager: 'true'
      cert-manager-subscription-name: 'openshift-cert-manager-operator'
      cert-manager-channel: 'stable-v1'
      cert-manager-source: 'redhat-operators'
      cert-manager-source-namespace: 'openshift-marketplace'
      # cert-manager-version: 'openshift-cert-manager-operator.v1.x.x'  # Optional: pin to specific CSV version

managedClusterSets:
  managed:
    labels:
      cert-manager: 'true'
      cert-manager-subscription-name: 'openshift-cert-manager-operator'
      cert-manager-channel: 'stable-v1'
      cert-manager-source: 'redhat-operators'
      cert-manager-source-namespace: 'openshift-marketplace'
      # cert-manager-version: 'openshift-cert-manager-operator.v1.x.x'  # Optional: pin to specific CSV version

# For specific clusters (optional override)
clusters:
  my-cluster:
    labels:
      cert-manager: 'true'
      cert-manager-channel: 'fast'  # Override channel for this cluster
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
- **Version Pinning**: Add `cert-manager-version` label to pin to a specific CSV version
- **Manual Control**: Pinned versions require manual updates to upgrade

To pin to a specific version, set the version label in your clusterset or per-cluster values file:
```yaml
cert-manager-version: 'openshift-cert-manager-operator.v1.x.x'
```

Find available CSV versions:
```bash
# List available versions for this operator
oc get packagemanifests openshift-cert-manager-operator -o jsonpath='{.status.channels[*].currentCSV}'
```

## Next Steps: Configuration

### 1. Explore Installed CRDs
After operator installation, check what Custom Resources are available:
```bash
# Wait for operator to install
oc get pods -n cert-manager-operator

# Check available CRDs
oc get crds | grep cert-manager

# Explore CRD specifications
oc explain <CustomResourceName>
```

### 2. Create Configuration Policies
Add operator-specific configuration policies to `templates/` directory.

#### Common Patterns:
- `policy-cert-manager-config.yaml` - Main configuration
- `policy-cert-manager-<feature>.yaml` - Feature-specific configs

#### Template Structure:
```yaml
{{- $policyName := "policy-cert-manager-config" }}
{{- $placementName := "placement-policy-cert-manager-config" }}

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
    - name: policy-cert-manager-operator-install
      namespace: {{ .Values.policy_namespace }}
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: cert-manager-config
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
                  name: cert-manager-config
                  namespace: {{ .Values.certManager.namespace }}
                spec:
                  # Your operator-specific configuration
                  # Use dynamic labels when needed:
                  # setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/cert-manager-setting" | default "default-value" {{ "hub}}" }}'
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
            - key: 'autoshift.io/cert-manager'
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
# cert-manager-setting<string>: Configuration option (default: 'value')
# cert-manager-feature-enabled<bool>: Enable optional feature (default: 'false')
# cert-manager-provider<string>: Provider-specific config (default: 'generic')

# Use in templates:
setting: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/cert-manager-setting" | default "default-value" {{ "hub}}" }}'
```

## Common Patterns

### CSV Status Checking (Optional)
For operators that need installation verification:
```yaml
- objectDefinition:
    apiVersion: policy.open-cluster-management.io/v1
    kind: ConfigurationPolicy
    metadata:
      name: cert-manager-csv-status
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
              namespace: {{ .Values.certManager.namespace }}
            status:
              phase: Succeeded
```

## Troubleshooting

### Policy Not Applied
1. Check cluster labels: `oc get managedcluster <cluster> --show-labels`
2. Verify placement: `oc get placement -n open-cluster-policies`
3. Check policy status: `oc describe policy policy-cert-manager-operator-install`

### Operator Installation Issues
1. Check subscription: `oc get subscription -n cert-manager-operator`
2. Check install plan: `oc get installplan -n cert-manager-operator`
3. Verify operator source exists: `oc get catalogsource -n openshift-marketplace`

### Template Rendering Issues
1. Validate rendering: `cd tools && go test -tags integration ./internal/resolver/...`
2. Check hub escaping: Look for `{{ "{{hub" }} ... {{ "hub}}" }}` patterns
3. Read the failure: the suite names the chart and the stage that failed (render, hub resolution, spoke resolution, YAML validation, label contract)

## Resources
- [Operator Documentation](https://operatorhub.io/operator/openshift-cert-manager-operator) - Find your operator details
- [AutoShift Developer Guide](../../../docs/developer-guide.md) - Comprehensive policy development guide
- [ACM Policy Documentation](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes) - Policy syntax reference in Governence Section
- [Similar Policies](../../README.md) - Browse other policies for patterns and examples