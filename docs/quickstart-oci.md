# AutoShift OCI Quick Start

Deploy AutoShift from OCI registry in 5 minutes.

## Prerequisites

```bash
# Verify cluster access
oc whoami

# Verify operators are installed
oc get csv -n openshift-gitops | grep gitops
oc get csv -n open-cluster-management | grep advanced-cluster-management
```

## Installation

### Option 1: ArgoCD Application (Recommended)

```bash
# 1. Configure OCI credentials (if using private registry)
oc create secret docker-registry autoshift-oci-credentials \
  --docker-server=quay.io \
  --docker-username=YOUR_QUAY_USERNAME \
  --docker-password=YOUR_QUAY_TOKEN \
  -n openshift-gitops

# 2. Link credentials to ArgoCD
oc patch serviceaccount argocd-repo-server -n openshift-gitops \
  --type='json' \
  -p='[{"op":"add","path":"/imagePullSecrets/-","value":{"name":"autoshift-oci-credentials"}}]'

oc patch serviceaccount argocd-applicationset-controller -n openshift-gitops \
  --type='json' \
  -p='[{"op":"add","path":"/imagePullSecrets/-","value":{"name":"autoshift-oci-credentials"}}]'

# 2b. (Optional) For registries with custom CA, enable cluster CA bundle in values:
#     gitops:
#       repo:
#         cluster_ca_bundle: true
# See deploy-oci.md for detailed CA configuration

# 3. Deploy AutoShift
cat <<EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: oci://quay.io/autoshift
    chart: autoshift
    targetRevision: 1.0.0  # Pin to specific version
    helm:
      values: |
        autoshift:
          dryRun: false

        hubClusterSets:
          hub:
            labels:
              self-managed: 'true'
              openshift-version: '4.18.22'
              # GitOps is required for hub clusters
              gitops: 'true'
              # ACM is automatically installed on all hub clustersets by policy
              # Optional: Additional operators
              acs: 'true'
              acs-channel: 'stable'
              odf: 'true'
              odf-channel: 'stable-4.18'
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
EOF

# 4. Watch deployment
oc get application autoshift -n openshift-gitops -w
```

### Option 2: Helm CLI

```bash
# 1. Login to OCI registry (if using private registry)
helm registry login quay.io -u YOUR_QUAY_USERNAME -p YOUR_QUAY_TOKEN

# 2. Create values file
cat > my-values.yaml <<EOF
autoshift:
  dryRun: false

hubClusterSets:
  hub:
    labels:
      self-managed: 'true'
      openshift-version: '4.18.22'
      # GitOps is required for hub clusters
      gitops: 'true'
      # ACM is automatically installed on all hub clustersets by policy
      # Optional: Additional operators
      acs: 'true'
      acs-channel: 'stable'
      odf: 'true'
      odf-channel: 'stable-4.18'
EOF

# 3. Install
helm install autoshift oci://quay.io/autoshift/autoshift \
  --version 1.0.0 \
  --namespace openshift-gitops \
  --create-namespace \
  -f my-values.yaml

# 4. Verify
helm list -n openshift-gitops
```

## Verification

```bash
# Check ArgoCD Applications
oc get applications -n openshift-gitops

# Check ACM Policies
oc get policies -A

# Check policy compliance
oc get policies -n policies-autoshift

# View specific policy
oc describe policy policy-acs-operator-install -n policies-autoshift
```

## Upgrade

```bash
# With ArgoCD - just update the Application targetRevision
oc patch application autoshift -n openshift-gitops \
  --type=merge \
  -p '{"spec":{"source":{"targetRevision":"1.1.0"}}}'

# With Helm
helm upgrade autoshift oci://quay.io/autoshift/autoshift \
  --version 1.1.0 \
  -f my-values.yaml
```

## Rollback

```bash
# With Helm
helm rollback autoshift -n openshift-gitops

# With ArgoCD - update targetRevision to previous version
oc patch application autoshift -n openshift-gitops \
  --type=merge \
  -p '{"spec":{"source":{"targetRevision":"1.0.0"}}}'
```

## Enable Dry Run (Test Mode)

```bash
# Patch the Application to enable dry run
oc patch application autoshift -n openshift-gitops \
  --type=merge \
  -p '{"spec":{"source":{"helm":{"values":"autoshift:\n  dryRun: true\n"}}}}'

# All policies will now report violations without enforcing changes
oc get policies -n policies-autoshift
```

## Common Configurations

> **Note:** All examples below assume `gitops: 'true'` is set. ACM is automatically installed on all hub clustersets by policy.

### Enable Multiple Operators

```yaml
hubClusterSets:
  hub:
    labels:
      gitops: 'true'  # Required

      # Security
      acs: 'true'
      acs-channel: 'stable'

      # Storage
      odf: 'true'
      odf-channel: 'stable-4.18'

      # Logging
      logging: 'true'
      logging-channel: 'stable-6.3'
      loki: 'true'
      loki-channel: 'stable-6.3'

      # Monitoring
      coo: 'true'

      # Compliance
      compliance: 'true'
```

### Pin Operator Versions

```yaml
hubClusterSets:
  hub:
    labels:
      gitops: 'true'  # Required

      acs: 'true'
      acs-channel: 'stable'
      acs-version: 'rhacs-operator.v4.9.0'  # Pin to specific version

      odf: 'true'
      odf-channel: 'stable-4.18'
      odf-version: 'odf-operator.v4.18.11-rhodf'  # Pin version
```

### Configure Infrastructure Nodes

```yaml
hubClusterSets:
  hub:
    labels:
      gitops: 'true'  # Required

      infra-nodes: '3'
      infra-nodes-provider: aws
      infra-nodes-instance-type: 'm6i.2xlarge'
      infra-nodes-zone-1: 'us-east-2a'
      infra-nodes-zone-2: 'us-east-2b'
      infra-nodes-zone-3: 'us-east-2c'
```

## Troubleshooting

### ArgoCD Application stuck syncing

```bash
# Check application status
oc get application autoshift -n openshift-gitops -o yaml

# View sync errors
oc describe application autoshift -n openshift-gitops

# Check ArgoCD repo server logs
oc logs -n openshift-gitops deployment/argocd-repo-server --tail=100
```

### OCI registry authentication failed

```bash
# Verify secret exists
oc get secret autoshift-oci-credentials -n openshift-gitops

# Check secret has correct data
oc get secret autoshift-oci-credentials -n openshift-gitops -o yaml

# Verify service account has imagePullSecrets
oc get sa argocd-repo-server -n openshift-gitops -o yaml | grep -A2 imagePullSecrets
```

### Policy not applying

```bash
# Check if cluster has required label
oc get managedcluster local-cluster -o yaml | grep autoshift.io

# Check policy placement
oc get placement -n policies-autoshift

# View policy details
oc describe policy POLICY_NAME -n policies-autoshift
```

## Next Steps

- [Full Documentation](README.md)
- [Release Process](releases.md)
- [Configuration Guide](developer-guide.md)
- [GitHub Releases](https://github.com/auto-shift/autoshiftv2/releases)
