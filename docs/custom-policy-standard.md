# Custom Policy Standards

AutoShift policies are grouped under an ACM Governance **standard** that appears in the ACM console's policy filters. By default, all policies use `NIST SP 800-53`. You can override this to group policies under your own standard name — for example, `Advanced Compute Platform`.

## Configuration

Set `autoshift.policyStandard` in your values:

```yaml
# autoshift/values/global.yaml (or any values file)
autoshift:
  policyStandard: "Advanced Compute Platform"
```

This single value flows to every policy through the existing substitution pipeline:

- **PolicyGenerator policies** receive it as `${POLICY_STANDARD}` via the CMP sidecar
- **Helm-templated policies** receive it as `.Values.autoshift.policyStandard`
- **OCI-rendered charts** receive it through the same `policyValuesObject`

If unset, the default `NIST SP 800-53` is used — no existing behavior changes.

## HA vs. Non-HA Storage (ODF / LVM)

A single standard covers both profiles. The HA vs. non-HA distinction is a **placement concern**, not a standards concern — policies for ODF and LVM already target different clusters via label selectors:

| Storage | Placement label | Use case |
|---------|----------------|----------|
| ODF | `autoshift.io/odf: 'true'` | HA clusters with dedicated storage nodes |
| LVM | `autoshift.io/lvm: 'true'` | Non-HA / single-node / compact clusters |

To assign a cluster to the right storage profile, set the appropriate label in your clusterset or per-cluster values file:

```yaml
# HA cluster — gets ODF
managedClusterSets:
  production:
    labels:
      autoshift.io/odf: 'true'

# Non-HA cluster — gets LVM
managedClusterSets:
  edge:
    labels:
      autoshift.io/lvm: 'true'
```

Both sets of policies appear under the same `Advanced Compute Platform` standard in the ACM Governance UI. You can filter further by **category** (`CM Configuration Management`, `IA Identification and Authentication`, etc.) to drill into specific policy domains.

## Example: Deploying the Advanced Compute Platform Standard

1. Set the standard in your global values:

   ```yaml
   # autoshift/values/global.yaml
   autoshift:
     policyStandard: "Advanced Compute Platform"
   ```

2. In your clusterset values, enable the storage profile per cluster type:

   ```yaml
   # autoshift/values/clustersets/ha-production.yaml
   managedClusterSets:
     ha-production:
       labels:
         autoshift.io/odf: 'true'
         autoshift.io/storage-nodes: 'true'
         # ... other labels
   ```

   ```yaml
   # autoshift/values/clustersets/edge-sites.yaml
   managedClusterSets:
     edge-sites:
       labels:
         autoshift.io/lvm: 'true'
         # ... other labels
   ```

3. Deploy or sync. All policies will show up as `Advanced Compute Platform` in the ACM console.

## How It Works

The standard is substituted at deploy time — the same mechanism used for `${POLICY_NAMESPACE}`, `${REMEDIATION}`, and other per-deployment tokens:

1. Helm renders the ApplicationSet with `POLICY_STANDARD` as a plugin env var
2. The CMP sidecar runs `sed` to replace `${POLICY_STANDARD}` in all policy-generator-config.yaml files before `kustomize build`
3. PolicyGenerator bakes the value into the `policy.open-cluster-management.io/standards` annotation on every generated Policy

For Helm-templated policies (openshift-gitops, cluster-labels, cluster-config-maps), the annotation reads the value directly from `.Values.autoshift.policyStandard`.
