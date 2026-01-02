# Adding New Operators to AutoShift

This guide walks you through adding a new operator to AutoShift and contributing it upstream so it works in production environments.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Step 1: Research the Operator](#step-1-research-the-operator)
- [Step 2: Generate the Policy](#step-2-generate-the-policy)
- [Step 3: Understand Generated Files](#step-3-understand-generated-files)
- [Step 4: Add Labels to Values Files](#step-4-add-labels-to-values-files)
- [Step 5: Add Operator Configuration (Optional)](#step-5-add-operator-configuration-optional)
- [Step 6: Test Locally](#step-6-test-locally)
- [Step 7: Contribute Upstream](#step-7-contribute-upstream)
- [Complete Example Walkthrough](#complete-example-walkthrough)
- [Troubleshooting](#troubleshooting)

---

## Overview

AutoShift uses Red Hat Advanced Cluster Management (RHACM) policies to deploy and manage operators across OpenShift clusters. When you add a new operator to AutoShift, you're creating:

1. **A Helm chart** in `policies/<component-name>/` containing the RHACM policy
2. **Cluster labels** in `autoshift/values.*.yaml` files that enable/configure the operator
3. **Optional configuration policies** for operator-specific custom resources

The policy is automatically picked up by ArgoCD's ApplicationSet and deployed to clusters that have the matching labels.

---

## Prerequisites

- Access to a hub cluster running ACM (for testing)
- `oc` CLI installed and logged into a cluster
- `helm` CLI installed
- Git repository cloned locally:

```bash
git clone https://github.com/auto-shift/autoshiftv2.git
cd autoshiftv2
```

---

## Step 1: Research the Operator

Before generating a policy, gather the operator's details from OperatorHub.

### Find the Operator in the Catalog

```bash
# List available operators (search for yours)
oc get packagemanifests -n openshift-marketplace | grep -i <operator-keyword>

# Example: searching for cert-manager
oc get packagemanifests -n openshift-marketplace | grep -i cert
```

### Get Operator Details

```bash
# Get full details about the operator
oc describe packagemanifest <operator-name> -n openshift-marketplace

# Example output includes:
# - Package name (subscription name)
# - Available channels
# - Current CSV versions
# - Catalog source
```

### Key Information to Collect

| Field | Description | Example |
|-------|-------------|---------|
| **Package Name** | The OLM subscription name | `openshift-pipelines-operator-rh` |
| **Channel** | The update channel | `stable`, `fast`, `pipelines-1.20` |
| **Catalog Source** | Where the operator comes from | `redhat-operators` |
| **Source Namespace** | Usually `openshift-marketplace` | `openshift-marketplace` |
| **Target Namespace** | Where the operator installs | `openshift-operators`, custom namespace |
| **Install Mode** | Cluster-scoped or namespace-scoped | Most are cluster-scoped |

### Get Available Versions

```bash
# List available versions in a channel
oc get packagemanifests <operator-name> -o jsonpath='{.status.channels[*].currentCSV}'

# Get all channels with their versions
oc get packagemanifest <operator-name> -o yaml | grep -A2 "channels:"
```

---

## Step 2: Generate the Policy

Use the policy generator script to create the Helm chart structure.

### Basic Usage

```bash
./scripts/generate-operator-policy.sh <component-name> <subscription-name> \
  --channel <channel> \
  --namespace <namespace> \
  [options]
```

### Required Parameters

| Parameter | Description |
|-----------|-------------|
| `component-name` | Kebab-case name for your policy (e.g., `cert-manager`) |
| `subscription-name` | The exact OLM package name from Step 1 |
| `--channel` | Operator channel to subscribe to |
| `--namespace` | Target namespace for the operator |

### Optional Parameters

| Parameter | Description |
|-----------|-------------|
| `--version <csv>` | Pin to a specific operator version |
| `--namespace-scoped` | For operators that aren't cluster-scoped |
| `--add-to-autoshift` | Auto-add labels to all values files |
| `--values-files <list>` | Specific values files to update (e.g., `hub,sbx`) |
| `--source <source>` | Catalog source (default: `redhat-operators`) |
| `--source-namespace <ns>` | Source namespace (default: `openshift-marketplace`) |

### Examples

**Cluster-scoped operator (most common):**

```bash
./scripts/generate-operator-policy.sh cert-manager cert-manager-operator \
  --channel stable \
  --namespace cert-manager \
  --add-to-autoshift
```

**Operator with version pinning:**

```bash
./scripts/generate-operator-policy.sh cert-manager cert-manager-operator \
  --channel stable \
  --namespace cert-manager \
  --version cert-manager.v1.14.4 \
  --add-to-autoshift
```

**Namespace-scoped operator:**

```bash
./scripts/generate-operator-policy.sh my-operator my-operator-package \
  --channel stable \
  --namespace my-operator \
  --namespace-scoped \
  --add-to-autoshift
```

**Community operator:**

```bash
./scripts/generate-operator-policy.sh keycloak keycloak-operator \
  --channel fast \
  --namespace keycloak \
  --source community-operators \
  --add-to-autoshift
```

---

## Step 3: Understand Generated Files

After running the generator, you'll have a new directory under `policies/`:

```
policies/<component-name>/
├── Chart.yaml                                    # Helm chart metadata
├── values.yaml                                   # Default configuration values
├── README.md                                     # Policy documentation
└── templates/
    └── policy-<component>-operator-install.yaml  # RHACM Policy + Placement
```

### Chart.yaml

Contains Helm chart metadata. Usually doesn't need modification.

### values.yaml

Contains default values for the operator. Example:

```yaml
policy_namespace: open-cluster-policies

certManager:
  name: cert-manager-operator
  namespace: cert-manager
  channel: stable
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  operatorGroupName: cert-manager-operator
```

### Policy Template

The main policy file contains:

1. **ConfigurationPolicy** - Creates the operator namespace
2. **OperatorPolicy** - Installs the operator via OLM subscription
3. **Placement** - Targets clusters with the `autoshift.io/<component>: 'true'` label
4. **PlacementBinding** - Links the policy to the placement

The template uses "hub templates" to read cluster labels dynamically:

```yaml
channel: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/cert-manager-channel" | default "stable" {{ "hub}}" }}'
```

This allows per-cluster or per-clusterset overrides via labels.

---

## Step 4: Add Labels to Values Files

If you used `--add-to-autoshift`, labels are added automatically. Otherwise, add them manually.

### Label Structure

For each operator, add these labels to `autoshift/values.*.yaml`:

```yaml
hubClusterSets:
  hub:
    labels:
      ### <Component Name>
      <component>: 'true'                              # Enable the operator
      <component>-subscription-name: <package-name>    # OLM package name
      <component>-channel: <channel>                   # Operator channel
      <component>-source: redhat-operators             # Catalog source
      <component>-source-namespace: openshift-marketplace
      # <component>-version: '<csv-name>'              # Optional: pin version
```

### Example: Adding cert-manager Labels

```yaml
hubClusterSets:
  hub:
    labels:
      ### cert-manager
      cert-manager: 'true'
      cert-manager-subscription-name: cert-manager-operator
      cert-manager-channel: stable
      cert-manager-source: redhat-operators
      cert-manager-source-namespace: openshift-marketplace
      # cert-manager-version: 'cert-manager.v1.14.4'
```

### Which Values Files to Update

| File | Purpose |
|------|---------|
| `values.hub.yaml` | Main hub cluster configuration |
| `values.sbx.yaml` | Sandbox/development environment |
| `values.minimal.yaml` | Minimal configuration template |
| `values.hub.baremetal-sno.yaml` | Single-node OpenShift on bare metal |
| `values.hub.baremetal-compact.yaml` | Compact cluster on bare metal |

### Enable for Managed Clusters

To deploy the operator to managed (spoke) clusters:

```yaml
managedClusterSets:
  managed:
    labels:
      ### cert-manager
      cert-manager: 'true'
      cert-manager-subscription-name: cert-manager-operator
      cert-manager-channel: stable
      cert-manager-source: redhat-operators
      cert-manager-source-namespace: openshift-marketplace
```

### Per-Cluster Overrides

Override values for specific clusters:

```yaml
clusters:
  production-1:
    labels:
      cert-manager-channel: stable-1.14  # Use specific channel
      cert-manager-version: 'cert-manager.v1.14.4'  # Pin version
```

---

## Step 5: Add Operator Configuration (Optional)

Many operators require additional configuration (Custom Resources) after installation.

### Create Configuration Policy

Create a new template file for the operator configuration:

```bash
cat > policies/<component>/templates/policy-<component>-config.yaml << 'EOF'
{{- $policyName := "policy-<component>-config" }}
{{- $placementName := "placement-policy-<component>-config" }}

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
  # Optional: Wait for operator to be installed first
  dependencies:
    - name: policy-<component>-operator-install
      namespace: {{ .Values.policy_namespace }}
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: <component>-instance
        spec:
          remediationAction: enforce
          severity: high
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: <operator-api-group>/v1
                kind: <OperatorCR>
                metadata:
                  name: <instance-name>
                  namespace: {{ .Values.<componentCamel>.namespace }}
                spec:
                  # Your operator configuration here
---
# Include Placement and PlacementBinding (same pattern as install policy)
EOF
```

### Add Dependencies Between Policies

Use the `dependencies` field to ensure proper ordering:

```yaml
spec:
  dependencies:
    - name: policy-odf-operator-install
      namespace: {{ .Values.policy_namespace }}
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy
```

### Status Check Policy

Add a policy to verify successful deployment:

```yaml
- objectDefinition:
    apiVersion: policy.open-cluster-management.io/v1
    kind: ConfigurationPolicy
    metadata:
      name: <component>-status
    spec:
      remediationAction: inform  # Don't enforce, just report
      severity: high
      object-templates:
        - complianceType: musthave
          objectDefinition:
            apiVersion: operators.coreos.com/v1alpha1
            kind: ClusterServiceVersion
            metadata:
              namespace: {{ .Values.<componentCamel>.namespace }}
            spec:
              displayName: 'Your Operator Display Name'
            status:
              phase: Succeeded
```

---

## Step 6: Test Locally

### Validate Helm Template Rendering

```bash
# Render the templates to check for errors
helm template policies/<component>/

# Validate against Kubernetes API
helm template policies/<component>/ | oc apply --dry-run=client -f -
```

### Test All Policies

```bash
# Quick validation of all policies
for policy in policies/*/; do
  if [ -f "$policy/Chart.yaml" ]; then
    echo "Validating $policy..."
    helm template "$policy" > /dev/null && echo "✓ Valid" || echo "✗ Invalid"
  fi
done
```

### Deploy to Test Cluster

1. Ensure your test cluster has the required labels
2. Deploy AutoShift pointing to your branch
3. Monitor the policy status:

```bash
# Watch policies
oc get policies -A -w

# Check specific policy
oc describe policy policy-<component>-operator-install -n policies-autoshift

# Check ArgoCD application
oc get applications -n openshift-gitops | grep <component>
```

---

## Step 7: Contribute Upstream

### Fork and Clone

```bash
# Fork on GitHub, then clone your fork
git clone https://github.com/<YOUR-USERNAME>/autoshiftv2.git
cd autoshiftv2

# Add upstream remote
git remote add upstream https://github.com/auto-shift/autoshiftv2.git
```

### Create Feature Branch

```bash
git checkout -b feature/add-<operator>-policy
```

### Make Your Changes

1. Generate the policy
2. Add labels to values files
3. Add configuration policies if needed
4. Test locally

### Commit with Clear Message

```bash
git add policies/<component>/
git add autoshift/values.*.yaml

git commit -m "Add <operator> operator policy

- Generate policy for <operator> installation
- Add labels to hub and managed clusterset values
- Include configuration for <specific feature>"
```

### Push and Create Pull Request

```bash
git push origin feature/add-<operator>-policy
```

Then create a PR via GitHub web interface.

### PR Checklist

- [ ] Policy generated using `generate-operator-policy.sh`
- [ ] Subscription name and channel verified from OperatorHub
- [ ] Labels added to appropriate values files
- [ ] `helm template` renders without errors
- [ ] README.md included with policy documentation
- [ ] Tested on a real cluster (if possible)
- [ ] No hardcoded values (use hub templates for flexibility)

---

## Complete Example Walkthrough

Let's add the **Cert-Manager** operator end-to-end.

### 1. Research

```bash
oc get packagemanifests -n openshift-marketplace | grep cert
# Output: cert-manager-operator   Red Hat Operators   26d

oc describe packagemanifest cert-manager-operator -n openshift-marketplace | head -50
```

Key findings:
- Package: `cert-manager-operator`
- Channel: `stable`
- Source: `redhat-operators`
- Namespace: `cert-manager`

### 2. Generate Policy

```bash
./scripts/generate-operator-policy.sh cert-manager cert-manager-operator \
  --channel stable \
  --namespace cert-manager \
  --add-to-autoshift
```

### 3. Verify Generated Files

```bash
ls -la policies/cert-manager/
# Chart.yaml  README.md  templates/  values.yaml

cat policies/cert-manager/values.yaml
```

### 4. Validate

```bash
helm template policies/cert-manager/
```

### 5. Test (on a cluster with the label)

```bash
# Add label to test cluster
oc label managedcluster local-cluster autoshift.io/cert-manager=true

# Watch for policy compliance
oc get policies -A | grep cert-manager
```

### 6. Commit and Push

```bash
git add policies/cert-manager/
git add autoshift/values.hub.yaml

git commit -m "Add cert-manager operator policy

- Enable TLS certificate management across clusters
- Support for automatic certificate renewal
- Integrates with ACME providers"

git push origin feature/add-cert-manager
```

---

## Troubleshooting

### Common Issues

| Issue | Solution |
|-------|----------|
| `Policy not found by ArgoCD` | Ensure the policy directory name matches the component name |
| `Policy not applying to cluster` | Check cluster has `autoshift.io/<component>: 'true'` label |
| `Operator not installing` | Verify subscription name, channel, and source are correct |
| `helm template fails` | Check YAML syntax and Helm template expressions |
| `Version pinning not working` | Ensure the CSV name format is correct (e.g., `operator.v1.0.0`) |

### Debug Commands

```bash
# Check cluster labels
oc get managedcluster <cluster-name> -o yaml | grep autoshift

# Check policy status
oc describe policy policy-<component>-operator-install -n policies-autoshift

# Check OperatorPolicy on spoke cluster
oc get operatorpolicy -A
oc describe operatorpolicy install-<component> -n <cluster-name>

# Check subscription on spoke cluster
oc get sub -n <namespace>

# View ACM policy propagator logs
oc logs -n open-cluster-management deployment/grc-policy-propagator
```

### Getting Help

- Check existing policies in `policies/` for reference implementations
- Review the [Developer Guide](developer-guide.md) for architecture details
- Open an issue on [GitHub](https://github.com/auto-shift/autoshiftv2/issues)

---

## See Also

- [Developer Guide](developer-guide.md) - Full development documentation
- [Scripts README](../scripts/README.md) - Policy generator details
- [Gradual Rollout](gradual-rollout.md) - Multi-version deployment strategies
- [README](../README.md) - AutoShift cluster labels reference

