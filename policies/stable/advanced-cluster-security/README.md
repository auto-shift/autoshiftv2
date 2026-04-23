# Advanced Cluster Security Policy

This policy automates the deployment and Day 2 configuration of Red Hat Advanced Cluster Security (RHACS) across hub and managed clusters.

## Overview

The ACS policy suite handles:

1. **Operator installation** - Deploys the RHACS operator via OLM
2. **Central server** - Creates the Central CR on the hub with Scanner V4, monitoring, and optional VM scanning
3. **Secured clusters** - Deploys SecuredCluster on hub and managed clusters with admission control, monitoring, and network policy options
4. **Init bundle** - Generates and distributes the sensor TLS bundle to managed clusters
5. **Declarative configuration** - Configures OpenShift SSO auth provider and RBAC via declarative ConfigMaps
6. **Security policies** - Deploys baseline SecurityPolicy CRDs for runtime and deploy-time checks
7. **Console link** - Adds an RHACS console link to the OpenShift dashboard

## Enabling ACS

Set the following label on your cluster or clusterset:

```yaml
acs: 'true'
```

## Operator Configuration

| Label | Description | Default |
|-------|-------------|---------|
| `acs` | Enable/disable ACS | |
| `acs-subscription-name` | Subscription name | `rhacs-operator` |
| `acs-channel` | Operator channel | `stable` |
| `acs-version` | Pin to specific CSV version | (latest) |
| `acs-source` | Catalog source | `redhat-operators` |
| `acs-source-namespace` | Catalog namespace | `openshift-marketplace` |
| `acs-egress-connectivity` | Connectivity mode | `Online` (`Offline` for disconnected) |

## Day 2 Configuration Labels

### Central and SecuredCluster

These labels apply to both hub and managed clusters:

| Label | Description | Default | Scope |
|-------|-------------|---------|-------|
| `acs-scanner-v4` | Scanner V4 component state | `Enabled` | Central only |
| `acs-monitoring` | OpenShift monitoring integration | `'true'` | Central + SecuredCluster |
| `acs-vm-scanning` | VM scanning (Developer Preview) | off | Central + SecuredCluster |
| `acs-admission-control` | Admission control enforcement | off | SecuredCluster only |
| `acs-network-policies` | Network policy generation | not set | Central + SecuredCluster |

**Scanner V4** (`acs-scanner-v4`): Controls the Scanner V4 component on Central. Set to `Enabled` or `Disabled`.

**Monitoring** (`acs-monitoring`): When `'true'`, enables the OpenShift monitoring integration on both Central and SecuredCluster CRs. This exposes RHACS metrics to the cluster's Prometheus instance.

**VM Scanning** (`acs-vm-scanning`): When `'true'`, enables the `ROX_VIRTUAL_MACHINES` feature flag on Central and SecuredCluster. This is a Developer Preview feature for scanning virtual machine workloads.

**Admission Control** (`acs-admission-control`): When `'true'`, enables admission control enforcement (`listenOnCreates`, `listenOnUpdates`, `listenOnEvents` all set to true). When off, only `listenOnEvents` is enabled. Use with caution as this can block deployments.

**Network Policies** (`acs-network-policies`): When set to `Enabled` or `Disabled`, explicitly controls network policy generation. Only set this when you need explicit control; leave unset for RHACS defaults.

### Declarative Configuration (Hub Only)

These labels configure the RHACS auth provider via the declarative configuration API:

| Label | Description | Default |
|-------|-------------|---------|
| `acs-auth-provider` | Auth provider type | `openshift` |
| `acs-auth-min-role` | Minimum role for authenticated users | `None` |
| `acs-auth-admin-group` | Group mapped to Admin role | `cluster-admins` |

When `acs-auth-provider` is set, the policy:
1. Adds `declarativeConfiguration` to the Central CR referencing a ConfigMap
2. Creates the `acs-declarative-configs` ConfigMap in the `stackrox` namespace with OpenShift OAuth configuration

The default configuration maps the `cluster-admins` group to the RHACS Admin role and sets the minimum role for all authenticated users to `None`.

### Security Policies (Hub Only)

| Label | Description | Default |
|-------|-------------|---------|
| `acs-default-policies` | Deploy baseline SecurityPolicy CRDs | off |

When `'true'`, deploys three baseline `SecurityPolicy` CRDs (`config.stackrox.io/v1alpha1`) to the Central namespace. These become "externally managed" in the RHACS UI:

| Policy | Lifecycle | Description |
|--------|-----------|-------------|
| No Privilege Escalation | DEPLOY | Detects containers with `allowPrivilegeEscalation: true` |
| No Root User Containers | DEPLOY | Detects containers running as UID 0 |
| No Shell Spawning at Runtime | RUNTIME | Detects shell execution (`/bin/sh`, `/bin/bash`, `/bin/dash`) in running containers |

All policies are **inform-only** by default (no enforcement actions). Users can add enforcement or additional SecurityPolicy CRDs via per-cluster overrides.

## Example Configuration

### Hub cluster (full Day 2)

```yaml
acs: 'true'
acs-subscription-name: rhacs-operator
acs-channel: stable
acs-source: redhat-operators
acs-source-namespace: openshift-marketplace
acs-scanner-v4: Enabled
acs-monitoring: 'true'
acs-auth-provider: openshift
acs-auth-min-role: None
acs-auth-admin-group: cluster-admins
# acs-default-policies: 'true'
```

### Managed cluster (minimal)

```yaml
acs: 'true'
acs-subscription-name: rhacs-operator
acs-channel: stable
acs-source: redhat-operators
acs-source-namespace: openshift-marketplace
acs-monitoring: 'true'
```

## Policy Templates

| Template | Scope | Description |
|----------|-------|-------------|
| `policy-acs-operator-install` | Hub + Managed | Installs the RHACS operator |
| `policy-acs-central` | Hub | Creates Central CR with Day 2 config |
| `policy-acs-secured-cluster` | Managed | Deploys SecuredCluster on managed clusters |
| `policy-acs-secured-cluster-hub` | Hub | Deploys SecuredCluster on the hub itself |
| `policy-acs-init-bundle` | Hub | Generates the sensor init bundle |
| `policy-acs-sync-bundle` | Managed | Syncs the init bundle to managed clusters |
| `policy-acs-declarative-config` | Hub | Creates auth provider ConfigMap |
| `policy-acs-security-policies` | Hub | Deploys SecurityPolicy CRDs |
| `policy-acs-console-link` | Hub | Adds RHACS console link |

## Further Reading

- [Values Reference](../../docs/values-reference.md#advanced-cluster-security) - Complete label reference table
- [Developer Guide](../../docs/developer-guide.md) - How to create and modify policies
- [Gradual Rollout](../../docs/gradual-rollout.md) - Version pinning and staged rollout
- [RHACS Documentation](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_security_for_kubernetes) - Red Hat Advanced Cluster Security documentation (select your version, then see *Configuring > Declarative Configuration* and *Operating > Managing Security Policies*)
