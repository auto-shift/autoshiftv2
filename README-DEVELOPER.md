# AutoShiftv2 - Developer Guide

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![OpenShift](https://img.shields.io/badge/OpenShift-4.18%2B-red)](https://www.openshift.com/)
[![RHACM](https://img.shields.io/badge/RHACM-2.14%2B-purple)](https://www.redhat.com/en/technologies/management/advanced-cluster-management)

**Build and manage OpenShift Platform Plus infrastructure as code with policy-driven automation**

## 🚀 Quick Start - Create Your First Policy

Generate and deploy an operator policy in under 5 minutes:

```bash
# 1. Generate a new operator policy with AutoShift integration
./scripts/generate-operator-policy.sh cert-manager cert-manager-operator --channel stable --namespace cert-manager --add-to-autoshift

# 2. Validate the generated policy
helm template policies/cert-manager/

# 3. Commit and push - AutoShift will automatically deploy via GitOps
git add policies/cert-manager/
git commit -m "Add cert-manager operator policy"
git push
```

Your operator is now being deployed across your clusters! Check the ArgoCD dashboard to monitor progress.

## 📋 Table of Contents

- [Architecture Overview](#-architecture-overview)
- [Developer Setup](#-developer-setup)
- [Creating Your First Policy](#-creating-your-first-policy)
- [Policy Development Guide](#-policy-development-guide)
- [Common Development Tasks](#-common-development-tasks)
- [Testing and Validation](#-testing-and-validation)
- [Contributing](#-contributing)
- [Troubleshooting](#-troubleshooting)
- [Additional Resources](#-additional-resources)

## 🏗️ Architecture Overview

AutoShiftv2 orchestrates OpenShift infrastructure using three key technologies:

```
┌─────────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   OpenShift GitOps  │────▶│      RHACM       │────▶│ Target Clusters │
│     (ArgoCD)        │     │    (Policies)    │     │                 │
└─────────────────────┘     └──────────────────┘     └─────────────────┘
         ▲                           ▲                        ▲
         │                           │                        │
    Git Repository            ApplicationSet            Cluster Labels
```

**Key Concepts:**
- **GitOps-Driven**: All configurations stored in Git, deployed via ArgoCD
- **Policy-Based**: RHACM policies enforce desired state across clusters
- **Label Targeting**: Clusters are configured via `autoshift.io/` labels
- **Template-First**: Policies are Helm charts for maximum flexibility

The framework uses GitOps principles with RHACM policies to manage cluster configurations declaratively.

## 🛠️ Developer Setup

### Prerequisites

| Tool | Version | Installation |
|------|---------|-------------|
| OpenShift CLI | 4.18+ | [Download oc](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#installing-openshift-cli) |
| Helm | 3.x | [Install Helm](https://helm.sh/docs/intro/install/) |
| Git | 2.x+ | Pre-installed on most systems |
| Access to Hub Cluster | - | Admin or developer access required |

### Repository Setup

```bash
# Clone the repository
git clone https://github.com/auto-shift/autoshiftv2.git
cd autoshiftv2

# Verify the policy generator works
./scripts/generate-operator-policy.sh --help

# Test policy generation
./scripts/generate-operator-policy.sh test-operator test-operator --channel stable --namespace test-operator
helm template policies/test-operator/

# Clean up test
rm -rf policies/test-operator/
```

### First-Time Setup Validation

```bash
# Check existing policies
ls -la policies/

# Validate all existing policies (optional but recommended)
for policy in policies/*/; do
  if [ -f "$policy/Chart.yaml" ]; then
    echo "Validating $policy..."
    helm template "$policy" > /dev/null && echo "✓ Valid" || echo "✗ Invalid"
  fi
done
```

## 💡 Creating Your First Policy

### Step 1: Research Your Operator

Before generating a policy, gather key information:

```bash
# Search for operator in OperatorHub
oc get packagemanifests -n openshift-marketplace | grep -i your-operator

# Get operator details
oc describe packagemanifest your-operator -n openshift-marketplace
```

### Step 2: Generate the Policy

```bash
# For cluster-scoped operators (most common)
./scripts/generate-operator-policy.sh \
  my-component \
  my-operator-subscription \
  --channel stable \
  --namespace my-component \
  --add-to-autoshift

# For namespace-scoped operators
./scripts/generate-operator-policy.sh \
  my-component \
  my-operator-subscription \
  --channel stable \
  --namespace my-component \
  --namespace-scoped \
  --add-to-autoshift
```

### Step 3: Understand Generated Files

Your new policy directory (`policies/my-component/`) contains:

```
policies/my-component/
├── Chart.yaml                          # Helm chart metadata
├── values.yaml                         # Default configuration
├── README.md                           # Policy documentation
└── templates/
    └── policy-my-component-operator-install.yaml  # RHACM Policy
```

### Step 4: Add Operator Configuration

Most operators need additional configuration after installation:

```bash
# 1. Explore installed CRDs
oc get crds | grep my-component

# 2. Create configuration policy
cat > policies/my-component/templates/policy-my-component-config.yaml << 'EOF'
{{- $policyName := "policy-my-component-config" }}
{{- $placementName := "placement-policy-my-component-config" }}

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
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: my-component-instance
        spec:
          remediationAction: enforce
          severity: high
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: my-component.io/v1
                kind: MyComponentInstance
                metadata:
                  name: instance
                  namespace: {{ .Values.myComponent.namespace }}
                spec:
                  # Add your configuration here
                  replicas: 3
                  storage:
                    size: 10Gi
---
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
            - key: 'autoshift.io/my-component'
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
EOF
```

### Step 5: Test and Deploy

```bash
# Validate your policy renders correctly
helm template policies/my-component/

# Commit and push to deploy
git add policies/my-component/
git commit -m "Add my-component operator with configuration"
git push

# Monitor deployment in ArgoCD
oc get applications -n openshift-gitops | grep my-component
```

## 📚 Policy Development Guide

### Policy Development Workflow

```mermaid
graph LR
    A[Research Operator] --> B[Generate Policy]
    B --> C[Add Configuration]
    C --> D[Test Locally]
    D --> E[Deploy to Dev]
    E --> F[Validate]
    F --> G[Promote to Prod]
```

### Working with Hub Template Functions

AutoShiftv2 uses RHACM hub templates to access cluster labels dynamically:

```yaml
# Access cluster labels for dynamic configuration
channel: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/my-component-channel" | default "stable" {{ "hub}}" }}'

# Conditional configuration based on labels
'{{ "{{hub" }} $clusterType := index .ManagedClusterLabels "autoshift.io/cluster-type" | default "development" {{ "hub}}" }}'
'{{ "{{hub" }} if eq $clusterType "production" {{ "hub}}" }}'
  replicas: 5
'{{ "{{hub" }} else {{ "hub}}" }}'
  replicas: 1
'{{ "{{hub" }} end {{ "hub}}" }}'

# Using subscription name from labels
name: '{{ "{{hub" }} index .ManagedClusterLabels "autoshift.io/my-component-subscription-name" | default "my-component-operator" {{ "hub}}" }}'
```

### Label-Based Configuration

Labels are configured in AutoShift values files and propagated to clusters by the cluster-labels policy:

```yaml
# In autoshift/values.hub.yaml - configure labels for cluster sets
hubClusterSets:
  hub:
    labels:
      my-component: 'true'
      my-component-subscription-name: 'my-component-operator'
      my-component-channel: 'stable'

managedClusterSets:
  managed:
    labels:
      my-component: 'true'
      my-component-subscription-name: 'my-component-operator'
      my-component-channel: 'fast'  # Different channel for managed clusters

# Individual cluster overrides in same values file
clusters:
  prod-cluster-1:
    labels:
      my-component-channel: 'stable-1.2'  # Override for specific cluster
```

Configuration precedence: **Individual Cluster > ClusterSet > Default Values**

### Dependency Management

AutoShift handles dependencies through logical ordering and shared placement rules. For explicit dependencies, add to policy spec.dependencies section like the example below:

```yaml
# In policies/my-component/README.md
## Dependencies

This policy depends on:
- OpenShift Data Foundation (ODF) - provides storage for my-component
- Loki - provides logging infrastructure

apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-my-component-install
  namespace: {{ .Values.policy_namespace }}
spec:
  dependencies:
    - name: policy-storage-cluster-test
      namespace: {{ .Values.policy_namespace }}
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy
    - name: policy-loki-operator-install
      namespace: {{ .Values.policy_namespace }}
      apiVersion: policy.open-cluster-management.io/v1
      compliance: Compliant
      kind: Policy

## Deployment Order

1. ODF must be running before deploying my-component
2. Loki should be installed
```

## 🔧 Common Development Tasks

### Updating an Existing Policy

```bash
# 1. Make changes to policy templates
vi policies/my-component/templates/policy-my-component-config.yaml

# 2. Validate changes
helm template policies/my-component/

# 3. Update with different label values
vi autoshift/values.sbx.yaml
vi autoshift/values.my-prod-labels.yaml

# 4. Commit and deploy
git add policies/my-component/
git add autoshift/
git commit -m "Update my-component configuration"
git push

# 5. validate on sbx cluster that is pointing to your branch

```

### Debugging Policy Issues

```bash
# Check policy status
oc get policies -A | grep my-component

# View policy details - namespace can be found from previous command
oc describe policy policy-my-component-operator-install -n policies-{{AUTOSHIFT_DEPLOYMENT_NAME}}

# View ArgoCD sync status
oc get applications -n openshift-gitops my-component -o yaml
```

### Working with Disconnected Environments

```bash
# Generate ImageSet for disconnected environments (see oc-mirror/README.md)
cd oc-mirror
./generate-imageset-config.sh values.hub.yaml,values.sbx.yaml \
  --operators-only \
  --output imageset-multi-env.yaml
cd ..
```

## 🧪 Testing and Validation

### Local Validation

```bash
# Validate single policy
helm template policies/my-component/ | oc apply --dry-run=client -f -

# Validate all policies
find policies/ -name "Chart.yaml" -exec dirname {} \; | while read policy; do
  echo "Testing $policy..."
  helm template "$policy" > /dev/null 2>&1 || echo "FAILED: $policy"
done
```

### Compliance Validation

```bash
# Check policy compliance across clusters
oc get policies -A \
  -o custom-columns=NAME:.metadata.name,COMPLIANT:.status.compliant

# Get detailed compliance status
oc get policyreports -A
```

## 🤝 Contributing

### Contribution Workflow

1. **Fork and Clone**
   ```bash
   git clone https://github.com/YOUR-USERNAME/autoshiftv2.git
   cd autoshiftv2
   ```

2. **Create Feature Branch**
   ```bash
   git checkout -b feature/add-my-operator-policy
   ```

3. **Generate and Develop Policy**
   ```bash
   ./scripts/generate-operator-policy.sh my-operator my-operator --channel stable --namespace my-operator
   # Add operator-specific configuration
   ```

4. **Test Thoroughly**
   ```bash
   helm template policies/my-operator/
   # Deploy and validate in test environment
   ```

5. **Submit Pull Request**
   ```bash
   git add policies/my-operator/
   git commit -m "Add my-operator policy with configuration"
   git push origin feature/add-my-operator-policy
   ```

### Code Standards

- ✅ Use policy generator for all new operator policies
- ✅ Include comprehensive README.md for each policy
- ✅ Follow existing naming conventions
- ✅ Test with `helm template` before committing
- ✅ Add subscription-name labels for all operators
- ✅ Document any special configuration requirements

### Pull Request Checklist

- [ ] Policy generated using `generate-operator-policy.sh`
- [ ] Subscription name and channel specified
- [ ] Configuration policies added if needed
- [ ] README.md updated with usage instructions
- [ ] Tested with `helm template`
- [ ] Deployed and validated in test environment
- [ ] No hardcoded values (use templates)
- [ ] Labels follow `autoshift.io/` convention

## 🔍 Troubleshooting

### Common Issues and Solutions

| Issue | Solution |
|-------|----------|
| Policy not applying to cluster | Check cluster labels with `oc get managedcluster CLUSTER -o yaml` |
| Operator installation failing | Verify subscription name matches catalog: `oc get packagemanifests` |
| Template rendering errors | Escape special characters, check YAML indentation |
| ArgoCD sync failures | Check application logs: `oc logs -n openshift-gitops deployment/openshift-gitops-server` |
| Policy stuck in NonCompliant | Check events: `oc get events -n OPERATOR-NAMESPACE` |

### Debug Commands

```bash
# Check ArgoCD application status
oc get applications -n openshift-gitops

# View policy propagator logs
oc logs -n open-cluster-management deployment/grc-policy-propagator

# View policy controller logs
oc logs -n open-cluster-management-agent-addon deployment/config-policy-controller

# Check placement decisions
oc get placementdecisions -A

# View cluster import status
oc get managedclusters
```

## 📖 Additional Resources

### Documentation
- [Policy Quick Start Documentation](scripts/README.md)
- [OpenShift GitOps Documentation](https://docs.openshift.com/container-platform/latest/cicd/gitops/understanding-openshift-gitops.html)
- [RHACM Policy Framework](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/)

### Training
- [DO480: Multicluster Management with Red Hat OpenShift Platform Plus](https://www.redhat.com/en/services/training/do480-multicluster-management-red-hat-openshift-platform-plus)

### Community
- [GitHub Issues](https://github.com/auto-shift/autoshiftv2/issues) - Report bugs or request features
- [Discussions](https://github.com/auto-shift/autoshiftv2/discussions) - Ask questions and share ideas

---

**Ready to contribute?** Start by [creating your first policy](#-creating-your-first-policy) or explore our [existing policies](policies/) for examples!