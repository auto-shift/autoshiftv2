# AutoShift Quick Start (From Source)

Deploy AutoShift from the Git repository for testing and development.

## Prerequisites

- OpenShift 4.18+ cluster
- `oc` CLI installed and logged in
- `helm` CLI installed

```bash
# Verify you're logged in
oc whoami
```

---

## Step 1: Install OpenShift GitOps

```bash
helm upgrade --install openshift-gitops openshift-gitops
```

Wait for GitOps pods to be ready (~2 minutes):

```bash
oc get pods -n openshift-gitops -w
# Ctrl+C when you see pods in Running state
```

---

## Step 2: Install Advanced Cluster Management

```bash
helm upgrade --install advanced-cluster-management advanced-cluster-management
```

Wait for ACM to finish installing (~10 minutes):

```bash
oc get mch -A -w
# Ctrl+C when STATUS shows "Running"
```

---

## Step 3: Deploy AutoShift

Once ACM is running, deploy AutoShift:

```bash
cat << 'EOF' | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  destination:
    server: https://kubernetes.default.svc
  source:
    path: autoshift
    repoURL: https://github.com/auto-shift/autoshiftv2.git
    targetRevision: main
    helm:
      valueFiles:
        - values.minimal.yaml
      values: |
        hubClusterSets:
          hub:
            labels:
              self-managed: 'true'
              openshift-version: '4.20.0'
              gitops: 'true'
              gitops-subscription-name: openshift-gitops-operator
              gitops-channel: gitops-1.18
              gitops-source: redhat-operators
              gitops-source-namespace: openshift-marketplace
              acm-subscription-name: advanced-cluster-management
              acm-channel: release-2.14
              acm-source: redhat-operators
              acm-source-namespace: openshift-marketplace
  project: default
  syncPolicy:
    automated:
      prune: false
      selfHeal: true
EOF
```

> **Note:** Update `openshift-version` to match your cluster version (e.g., `4.18.28`, `4.20.0`).

---

## Step 4: Add Hub Cluster to ClusterSet

```bash
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub --overwrite
```

---

## Step 5: Verify Installation

```bash
# Watch ArgoCD applications spin up
oc get applications -n openshift-gitops -w

# Check policies are being created
oc get policies -A

# Check policy compliance
oc get policies -n policies-autoshift
```

---

## Adding Operators

### Option 1: Patch the Application

Add operators by patching the AutoShift application with updated labels:

```bash
oc patch applications.argoproj.io autoshift -n openshift-gitops --type=merge -p '
spec:
  source:
    helm:
      values: |
        hubClusterSets:
          hub:
            labels:
              self-managed: "true"
              openshift-version: "4.20.0"
              gitops: "true"
              gitops-subscription-name: openshift-gitops-operator
              gitops-channel: gitops-1.18
              gitops-source: redhat-operators
              gitops-source-namespace: openshift-marketplace
              acm-subscription-name: advanced-cluster-management
              acm-channel: release-2.14
              acm-source: redhat-operators
              acm-source-namespace: openshift-marketplace
              ### Add OpenShift Pipelines
              pipelines: "true"
              pipelines-subscription-name: openshift-pipelines-operator-rh
              pipelines-channel: pipelines-1.20
              pipelines-source: redhat-operators
              pipelines-source-namespace: openshift-marketplace
'
```

### Option 2: Use a Custom Values File

1. Create your own values file (e.g., `values.my-cluster.yaml`)
2. Update the AutoShift application to use it:

```bash
oc patch application autoshift -n openshift-gitops --type=merge -p '
spec:
  source:
    helm:
      valueFiles:
        - values.my-cluster.yaml
'
```

---

## Available Operators

Common operators you can enable:

| Operator | Label | Subscription Name |
|----------|-------|-------------------|
| OpenShift Pipelines | `pipelines: 'true'` | `openshift-pipelines-operator-rh` |
| Advanced Cluster Security | `acs: 'true'` | `rhacs-operator` |
| Compliance Operator | `compliance: 'true'` | `compliance-operator` |
| OpenShift Logging | `logging: 'true'` | `cluster-logging` |
| Loki | `loki: 'true'` | `loki-operator` |
| Cluster Observability | `coo: 'true'` | `cluster-observability-operator` |
| OpenShift Data Foundation | `odf: 'true'` | `odf-operator` |
| Developer Spaces | `dev-spaces: 'true'` | `devspaces` |
| Developer Hub | `dev-hub: 'true'` | `rhdh` |
| Quay | `quay: 'true'` | `quay-operator` |
| OpenShift Virtualization | `virt: 'true'` | `kubevirt-hyperconverged` |

For each operator, you need these labels:

```yaml
<operator>: 'true'
<operator>-subscription-name: <package-name>
<operator>-channel: <channel>
<operator>-source: redhat-operators
<operator>-source-namespace: openshift-marketplace
```

---

## Example: Enable Multiple Operators

```bash
oc patch application autoshift -n openshift-gitops --type=merge -p '
spec:
  source:
    helm:
      values: |
        hubClusterSets:
          hub:
            labels:
              self-managed: "true"
              openshift-version: "4.20.0"
              ### Required: GitOps
              gitops: "true"
              gitops-subscription-name: openshift-gitops-operator
              gitops-channel: gitops-1.18
              gitops-source: redhat-operators
              gitops-source-namespace: openshift-marketplace
              ### Required: ACM
              acm-subscription-name: advanced-cluster-management
              acm-channel: release-2.14
              acm-source: redhat-operators
              acm-source-namespace: openshift-marketplace
              ### OpenShift Pipelines
              pipelines: "true"
              pipelines-subscription-name: openshift-pipelines-operator-rh
              pipelines-channel: pipelines-1.20
              pipelines-source: redhat-operators
              pipelines-source-namespace: openshift-marketplace
              ### Advanced Cluster Security
              acs: "true"
              acs-subscription-name: rhacs-operator
              acs-channel: stable
              acs-source: redhat-operators
              acs-source-namespace: openshift-marketplace
              ### Compliance Operator
              compliance: "true"
              compliance-subscription-name: compliance-operator
              compliance-channel: stable
              compliance-source: redhat-operators
              compliance-source-namespace: openshift-marketplace
'
```

---

## Verify Operator Installation

```bash
# Check policies for your operator
oc get policies -A | grep pipelines

# Watch the operator install
oc get csv -n openshift-operators -w

# Check operator pods
oc get pods -n openshift-operators
```

---

## Troubleshooting

### Check ArgoCD Application Status

```bash
oc get application autoshift -n openshift-gitops -o yaml
oc describe application autoshift -n openshift-gitops
```

### Check Policy Status

```bash
# List all policies
oc get policies -A

# Describe a specific policy
oc describe policy policy-pipelines-operator-install -n policies-autoshift
```

### Check Cluster Labels

```bash
oc get managedcluster local-cluster -o yaml | grep -A50 labels
```

### View ACM Policy Propagator Logs

```bash
oc logs -n open-cluster-management deployment/grc-policy-propagator --tail=100
```

---

## Cleanup

To remove AutoShift:

```bash
# Delete the AutoShift application
oc delete application autoshift -n openshift-gitops

# Delete policies namespace
oc delete namespace policies-autoshift

# Remove cluster from clusterset
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset-
```

---

## Next Steps

- [Adding New Operators](adding-new-operators.md) - Create custom operator policies
- [Developer Guide](developer-guide.md) - Full development documentation
- [OCI Quick Start](quickstart-oci.md) - Deploy from OCI registry (production)

