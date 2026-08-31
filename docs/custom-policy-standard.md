# Custom Policy Standards

AutoShift policies are grouped under an ACM Governance **standard** that appears in the ACM console's policy filters. By default, all policies use `NIST SP 800-53`. You can override this to group policies under your own standard names.

AutoShift supports two configurable standards to separate hub infrastructure policies from managed cluster policies:

| Value | Variable | Applies to |
|-------|----------|------------|
| `autoshift.policyStandard` | `${POLICY_STANDARD}` | Managed cluster policies (~40 policies) |
| `autoshift.policyStandardHub` | `${POLICY_STANDARD_HUB}` | Hub-targeting policies (ACM, ACS, GitOps, cluster-install, cluster-labels, cluster-config-maps, openshift-dns, metallb) |

If `policyStandardHub` is unset, it defaults to `policyStandard`. If both are unset, all policies use `NIST SP 800-53`.

## Configuration

```yaml
# In your Helm values file
autoshift:
  policyStandard: "Advanced Compute Platform"
  policyStandardHub: "Advanced Compute Platform Hub"
```

This flows through the existing substitution pipeline:

- **PolicyGenerator policies** receive `${POLICY_STANDARD}` or `${POLICY_STANDARD_HUB}` via the CMP sidecar
- **Helm-templated policies** receive `.Values.autoshift.policyStandard` or `.Values.autoshift.policyStandardHub`
- **OCI-rendered charts** receive it through the same `policyValuesObject`

## Hub vs. Managed Policy Classification

Hub policies are those whose placement targets hub clusters (via `autoshift.io/cluster-type: hub` or hub-only clusterSets):

| Standard | Policies |
|----------|----------|
| Hub | advanced-cluster-management, advanced-cluster-security, cluster-install, cluster-labels, cluster-config-maps, openshift-gitops, openshift-dns, metallb |
| Managed | All other policies (~40) — cert-manager, logging, lvm, odf, openshift-virtualization, etc. |

## HA vs. Non-HA Storage (ODF / LVM)

The HA vs. non-HA distinction is a **placement concern**, not a standards concern — policies for ODF and LVM target different clusters via label selectors:

| Storage | Placement label | Use case |
|---------|----------------|----------|
| ODF | `autoshift.io/odf: 'true'` | HA clusters with dedicated storage nodes |
| LVM | `autoshift.io/lvm: 'true'` | Non-HA / single-node / compact clusters |

Both appear under the same managed standard (`Advanced Compute Platform`) in the Governance UI.

## Example: Deploying the Advanced Compute Platform

```yaml
autoshift:
  policyStandard: "Advanced Compute Platform"
  policyStandardHub: "Advanced Compute Platform Hub"
```

In the ACM Governance UI this creates two filterable standards:
- **Advanced Compute Platform Hub** — hub infrastructure (ACM, ACS, GitOps, DNS, MetalLB)
- **Advanced Compute Platform** — managed cluster policies (storage, observability, security agents, operators)

## How It Works

The standards are substituted at deploy time — the same mechanism used for `${POLICY_NAMESPACE}`, `${REMEDIATION}`, and other per-deployment tokens:

1. Helm renders the ApplicationSet with `POLICY_STANDARD` and `POLICY_STANDARD_HUB` as plugin env vars
2. The CMP sidecar runs `sed` to replace both `${POLICY_STANDARD_HUB}` and `${POLICY_STANDARD}` tokens (`_HUB` is replaced first so the shorter token doesn't match it)
3. PolicyGenerator bakes the values into `policy.open-cluster-management.io/standards` annotations

For Helm-templated policies (openshift-gitops, cluster-labels, cluster-config-maps), the annotation reads directly from `.Values.autoshift.policyStandardHub`.
