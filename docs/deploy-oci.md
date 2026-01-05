# Deploying AutoShift from OCI Registry

This guide explains how to deploy AutoShift from an OCI registry (Quay, GHCR, Harbor, etc.) instead of Git.

## Why Use OCI Mode?

**Benefits:**
- ✅ **Version pinning**: Deploy specific tested versions to production
- ✅ **Immutable deployments**: Published charts cannot change
- ✅ **Faster syncs**: No Git polling, direct registry pulls
- ✅ **Offline support**: Mirror charts to disconnected registries
- ✅ **Rollback support**: Easy version rollbacks with Helm
- ✅ **No Git dependency**: Don't need Git repository access

## Prerequisites

1. **AutoShift charts published to OCI registry**
   ```bash
   # Release charts to registry
   ./scripts/release.sh --version 1.0.0 --namespace myorg/autoshift
   ```

2. **OpenShift cluster with**:
   - OpenShift GitOps operator
   - Red Hat ACM operator
   - Access to OCI registry (Quay, GHCR, etc.)

3. **OCI registry credentials** (if private)

## Deployment Steps

### Step 1: Configure OCI Registry Credentials

If your OCI registry is private, configure credentials for ArgoCD:

```bash
# Create secret for OCI registry authentication
oc create secret docker-registry autoshift-oci-creds \
  --docker-server=quay.io \
  --docker-username=myorg+robot \
  --docker-password=TOKEN \
  -n openshift-gitops

# Link to ArgoCD repo server (pulls Helm charts)
oc patch serviceaccount argocd-repo-server \
  -n openshift-gitops \
  --type='json' \
  -p='[{"op":"add","path":"/imagePullSecrets/-","value":{"name":"autoshift-oci-creds"}}]'

# Link to ApplicationSet controller (generates Applications)
oc patch serviceaccount argocd-applicationset-controller \
  -n openshift-gitops \
  --type='json' \
  -p='[{"op":"add","path":"/imagePullSecrets/-","value":{"name":"autoshift-oci-creds"}}]'

# Restart ArgoCD components to pick up credentials
oc rollout restart deployment/argocd-repo-server -n openshift-gitops
oc rollout restart deployment/argocd-applicationset-controller -n openshift-gitops
```

### Step 1b: Configure Custom CA Certificate (Optional)

If your OCI registry uses a custom CA certificate (e.g., private registry with self-signed certs), you need to configure ArgoCD to trust it.

**Option A: Use OpenShift's Cluster CA Bundle (Recommended)**

AutoShift can automatically inject the cluster's trusted CA bundle into ArgoCD. Enable this in your values file:

```yaml
gitops:
  repo:
    cluster_ca_bundle: true  # Injects cluster's trusted CA bundle into ArgoCD
```

This creates a ConfigMap with the `config.openshift.io/inject-trusted-cabundle: "true"` label, which OpenShift automatically populates with the cluster's CA certificates.

**Option B: Manual CA Configuration**

For manual configuration, create a ConfigMap with your CA certificate:

```bash
# Create ConfigMap with your custom CA
oc create configmap custom-ca-certs \
  --from-file=ca-bundle.crt=/path/to/your/ca-bundle.crt \
  -n openshift-gitops

# Or use OpenShift's automatic CA injection
oc create configmap custom-ca-certs \
  -n openshift-gitops
oc label configmap custom-ca-certs \
  config.openshift.io/inject-trusted-cabundle=true \
  -n openshift-gitops
```

Then reference it in your ArgoCD configuration by enabling `cluster_ca_bundle: true` in your values.

### Step 2: Install AutoShift from OCI Registry

#### Option A: Using Helm

```bash
# Create OCI-specific values file
cat > my-oci-values.yaml <<EOF
# Enable OCI mode
autoshiftOciRegistry: oci://quay.io/myorg/autoshift
autoshiftOciVersion: "1.0.0"

autoshift:
  dryRun: false

hubClusterSets:
  hub:
    labels:
      self-managed: 'true'
      openshift-version: '4.18.28'
      # GitOps is required for hub clusters
      gitops: 'true'
      # ACM is automatically installed on all hub clustersets by policy
      # Optional: Additional operators
      acs: 'true'
      acs-channel: 'stable'
      odf: 'true'
      odf-channel: 'stable-4.18'
EOF

# Install AutoShift main chart from OCI
helm registry login quay.io -u myorg+robot -p TOKEN

helm install autoshift oci://quay.io/myorg/autoshift/autoshift \
  --version 1.0.0 \
  --namespace openshift-gitops \
  --create-namespace \
  -f my-oci-values.yaml
```

#### Option B: Using ArgoCD Application

```bash
cat <<EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: oci://quay.io/myorg/autoshift
    chart: autoshift
    targetRevision: "1.0.0"
    helm:
      values: |
        # Enable OCI mode - this tells the ApplicationSet to deploy policies from OCI
        autoshiftOciRegistry: oci://quay.io/myorg/autoshift
        autoshiftOciVersion: "1.0.0"

        autoshift:
          dryRun: false

        hubClusterSets:
          hub:
            labels:
              self-managed: 'true'
              openshift-version: '4.18.28'
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
```

### Step 3: Verify Deployment

```bash
# Check AutoShift Application
oc get application autoshift -n openshift-gitops

# Check ApplicationSet (deploys individual policy charts)
oc get applicationset -n openshift-gitops

# Check individual policy Applications
oc get applications -n openshift-gitops | grep autoshift

# Verify policies are created
oc get policies -A
```

## How It Works

### Git Mode (Default)
```
AutoShift Chart
    ↓
ApplicationSet with Git Generator
    ↓
Discovers policies/ directories in Git
    ↓
Creates Applications pointing to Git paths
    ↓
Deploys policies from Git repository
```

### OCI Mode (When `autoshiftOciRegistry` is set)
```
AutoShift Chart from OCI
    ↓
ApplicationSet with List Generator
    ↓
Lists all policy names (embedded during release)
    ↓
Creates Applications pointing to OCI charts
    ↓
Deploys individual policy charts from OCI registry
```

**How it works:** The OCI chart includes a policy list that is **automatically generated during the release process** by discovering all policies in the `policies/` directory. This means:
- ✅ No manual maintenance of policy lists required
- ✅ New policies are automatically included when added to `policies/`
- ✅ Chart always matches the exact policies that were released
- ✅ Each release version has its own policy list snapshot

### Key Differences

| Aspect | Git Mode | OCI Mode |
|--------|----------|----------|
| **Generator** | `git:` - auto-discovers | `list:` - generated at release |
| **Source** | Git repository path | OCI chart reference |
| **Version** | Git branch/tag | Chart version (pinned) |
| **Sync** | Git polling | Registry pulls |
| **Dynamic** | Auto-discovers new policies | Fixed to released policies |
| **Maintenance** | Zero - Git-based | Zero - auto-generated |
| **Use Case** | Development/continuous deployment | Production releases |
| **Testing** | Direct from branch | Use RC versions (e.g., 1.0.0-rc.1) |

## Configuration

### Required Values for OCI Mode

```yaml
# These values enable OCI mode
autoshiftOciRegistry: oci://quay.io/myorg/autoshift
autoshiftOciVersion: "1.0.0"  # or use .Chart.Version
```

### Optional: Exclude Policies

You can exclude specific policies from deployment:

```yaml
excludePolicies:
  - infra-nodes      # Won't deploy this policy
  - worker-nodes     # Won't deploy this policy
```

This works in both Git and OCI modes.

## Upgrading

### Upgrade to New Version

```bash
# With Helm
helm upgrade autoshift oci://quay.io/myorg/autoshift/autoshift \
  --version 1.1.0 \
  -f my-oci-values.yaml

# With ArgoCD - update the Application
oc patch application autoshift -n openshift-gitops \
  --type=merge \
  -p '{"spec":{"source":{"targetRevision":"1.1.0"}}}'

# Also update the OCI version for policies
oc patch application autoshift -n openshift-gitops \
  --type=merge \
  -p '{"spec":{"source":{"helm":{"values":"autoshiftOciVersion: \"1.1.0\"\n"}}}}'
```

### Rollback to Previous Version

```bash
# With Helm
helm rollback autoshift -n openshift-gitops

# With ArgoCD - revert to previous version
oc patch application autoshift -n openshift-gitops \
  --type=merge \
  -p '{"spec":{"source":{"targetRevision":"1.0.0"}}}'
```

## Migrating from Git to OCI Mode

If you're currently using Git mode and want to migrate:

1. **Release charts to OCI registry**:
   ```bash
   ./scripts/release.sh --version 1.0.0 --namespace myorg/autoshift
   ```

2. **Update your values** (in existing Application):
   ```bash
   oc edit application autoshift -n openshift-gitops
   ```

   Add to `spec.source.helm.values`:
   ```yaml
   autoshiftOciRegistry: oci://quay.io/myorg/autoshift
   autoshiftOciVersion: "1.0.0"
   ```

3. **ArgoCD will automatically**:
   - Delete old Applications (from Git paths)
   - Create new Applications (from OCI charts)
   - Policies remain in place (no downtime)

## Troubleshooting

### ArgoCD can't pull charts from OCI registry

```bash
# Check if secret exists
oc get secret autoshift-oci-creds -n openshift-gitops

# Verify secret is linked to service accounts
oc get sa argocd-repo-server -n openshift-gitops -o yaml | grep -A2 imagePullSecrets
oc get sa argocd-applicationset-controller -n openshift-gitops -o yaml | grep -A2 imagePullSecrets

# Test credentials manually
helm registry login quay.io -u USERNAME -p TOKEN
helm pull oci://quay.io/myorg/autoshift/autoshift --version 1.0.0
```

### ApplicationSet not creating policy Applications

```bash
# Check ApplicationSet status
oc get applicationset autoshift-policies -n openshift-gitops -o yaml

# Check ApplicationSet controller logs
oc logs -n openshift-gitops deployment/argocd-applicationset-controller

# Verify autoshiftOciRegistry is set
oc get application autoshift -n openshift-gitops -o yaml | grep autoshiftOciRegistry
```

### Policy charts not found in registry

```bash
# Verify charts are published
helm search repo oci://quay.io/myorg/autoshift/

# List all published charts
curl -X GET "https://quay.io/api/v1/repository/myorg/autoshift/tag/" | jq '.'

# Pull specific chart to test
helm pull oci://quay.io/myorg/autoshift/advanced-cluster-security --version 1.0.0
```

### Chart version mismatch

If you see errors about version mismatches:

```bash
# Ensure autoshiftOciVersion matches published charts
oc get application autoshift -n openshift-gitops \
  -o jsonpath='{.spec.source.helm.values}' | grep autoshiftOciVersion

# All policy charts must have the same version
# Re-release if needed:
./scripts/release.sh --version 1.0.0 --namespace myorg/autoshift
```

## Best Practices

1. **Pin versions in production**:
   ```yaml
   autoshiftOciVersion: "1.0.0"  # Don't use "latest"
   ```

2. **Test in non-production first**:
   ```yaml
   autoshift:
     dryRun: true  # Test without enforcing
   ```

3. **Use semantic versioning**:
   ```
   1.0.0 → 1.0.1 (patch)
   1.0.0 → 1.1.0 (minor)
   1.0.0 → 2.0.0 (major - breaking)
   ```

4. **Document your registry**:
   - Keep registry URL in values file
   - Document credential requirements
   - Note any air-gap considerations

5. **Monitor deployments**:
   ```bash
   # Watch all policy Applications
   watch oc get applications -n openshift-gitops

   # Check policy compliance
   watch oc get policies -A
   ```

## Disconnected / Air-Gapped Environments

For disconnected environments:

1. **Mirror charts to internal registry**:
   ```bash
   # Pull all charts
   helm pull oci://quay.io/myorg/autoshift/autoshift --version 1.0.0
   for policy in $(cat policies.txt); do
     helm pull oci://quay.io/myorg/autoshift/$policy --version 1.0.0
   done

   # Push to internal registry
   for chart in *.tgz; do
     helm push $chart oci://harbor.internal.com/autoshift
   done
   ```

2. **Update values to use internal registry**:
   ```yaml
   autoshiftOciRegistry: oci://harbor.internal.com/autoshift
   autoshiftOciVersion: "1.0.0"
   ```

## Support

- [Release Documentation](releases.md)
- [Quick Start Guide](quickstart-oci.md)
- [Main Documentation](README.md)
