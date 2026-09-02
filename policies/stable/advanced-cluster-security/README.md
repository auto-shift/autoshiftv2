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

> [!WARNING]
> `config.acs.defaultPolicies: true` depends on the Config-as-Code component. The
> `configAsCode` Central setting deploys a `config-controller` pod whose only role grants access to
> `securitypolicies` in the `config.stackrox.io` API group, which is what reconciles `SecurityPolicy`
> custom resources into Central. Setting `configAsCode: Disabled` leaves those resources applied to
> the cluster and reporting compliant while Central never receives them. Leave `configAsCode` unset
> unless you also set `defaultPolicies: false`.

## Cluster registration

Secured clusters authenticate to Central for the first time with a **cluster registration
secret (CRS)**. A CRS is a single bootstrap token: Central issues each cluster its own service
certificates on registration and renews them automatically, and the CRS can be revoked afterwards
without disconnecting any cluster that already registered. Init bundles, the older mechanism, ship
long-lived service certificates that are copied to the whole fleet, so one cluster cannot be
revoked without breaking the others. Init bundles are deprecated as of Red Hat Advanced Cluster
Security 4.10.

The mode is the `autoshift.io/acs-registration` label, because it selects which policies are
placed on a cluster rather than how one behaves. Clusters with no such label get the `crs` path.

| Label value | Behaviour | Policies placed |
|-------------|-----------|-----------------|
| unset or `crs` | The hub mints a CRS with a Job and syncs it to every secured cluster | mint Job, readiness test, CRS sync |
| `manual` | No Job runs. You supply `cluster-registration-secret` yourself | readiness test, CRS sync |
| `initBundle` | Legacy. Mints an init bundle and syncs the three certificate secrets | init bundle Job, bundle sync |

```yaml
labels:
  acs-registration: 'crs'    # crs (default when unset) | manual | initBundle

config:
  acs:
    registration:
      validFor: 8760h        # CRS lifetime; roxctl's own default is only 24h
      maxClusters: 0         # 0 = no limit
      roxctlImage: ''        # blank = tag matched to the installed operator version
```

The mint Job runs `roxctl`, the documented way to generate a CRS. When `roxctlImage` is blank the
tag is taken from the `acs-version` label, or from the installed operator's current cluster service
version when that label is unset. **In a disconnected environment set `roxctlImage` to the digest
recorded in the operator's related images**, because that is what `oc-mirror` copies; a floating tag
might not resolve in a mirrored registry.

### Creating a CRS by hand

Use `manual` mode when policy should not hold Central credentials, or when the CRS is issued by a
Central this deployment does not manage. Either method produces the same
`cluster-registration-secret`, which you apply to the `stackrox` namespace on the hub.

From the ACS Console: **Platform Configuration > Clusters**, then **Create cluster registration
secret**, name it, and download the YAML.

With the CLI, from a machine that can reach Central:

```bash
export ROX_API_TOKEN=<api token with the Admin role>
roxctl -e "<central-host>:443" central crs generate autoshift \
  --valid-for 8760h --output crs.yaml
oc apply -n stackrox -f crs.yaml
```

> [!IMPORTANT]
> A CRS cannot be retrieved after it is generated, so store the file securely. The Job takes the
> same care: it never re-mints while a live `cluster-registration-secret` exists. Rotating one is a
> deliberate act, delete the secret and the `acs-crs-generate` Job.

## Where Central runs

`config.acs.central` decides whether this deployment runs its own Central or registers with
someone else's. This matters at fleet scale, because one Central is sized by the total number of
monitored deployments across every cluster connected to it.

```yaml
config:
  acs:
    central:
      deploy: true      # false = no Central here, register with an external one
      endpoint: ''      # blank = discover Central's route on this hub
```

With `deploy: false` the Central custom resource, its declarative configuration, the security
policies and the CRS Job are all skipped, and the cluster runs `SecuredCluster` only. That supports
a single Central for the whole fleet, a Central on each spoke hub, or a Central on the hub-of-hubs
only. When `deploy` is `false`, set `endpoint` and label the cluster
`acs-registration: manual`, because there is no local Central to mint from.

`centralEndpoint` resolves in this order: an explicit `endpoint`, then the in-cluster service on a
hub that runs Central, then a lookup of Central's route on the owning hub.

## Policy Templates

| Template | Scope | Description |
|----------|-------|-------------|
| `policy-acs-operator-install` | Hub + Managed | Installs the RHACS operator |
| `policy-acs-central` | Hub | Creates Central CR with Day 2 config |
| `policy-acs-secured-cluster` | Managed | Deploys SecuredCluster on managed clusters |
| `policy-acs-secured-cluster-hub` | Hub | Deploys SecuredCluster on the hub itself |
| `policy-acs-crs` | Hub | Mints the cluster registration secret (`acs-registration: crs`) |
| `policy-acs-sync-crs` | Managed | Syncs the CRS to managed clusters (`crs` and `manual`) |
| `policy-acs-init-bundle` | Hub | Legacy. Generates the sensor init bundle (`acs-registration: initBundle`) |
| `policy-acs-sync-bundle` | Managed | Legacy. Syncs the init bundle certificates to managed clusters |
| `policy-acs-declarative-config` | Hub | Creates auth provider ConfigMap |
| `policy-acs-security-policies` | Hub | Deploys SecurityPolicy CRs (requires the Config-as-Code component) |
| `policy-acs-console-link` | Hub | Adds RHACS console link |

## Further Reading

- [Values Reference](../../../docs/values-reference.md#red-hat-advanced-cluster-security) - Complete label reference table
- [Developer Guide](../../../docs/developer-guide.md) - How to create and modify policies
- [Gradual Rollout](../../../docs/gradual-rollout.md) - Version pinning and staged rollout
- [RHACS Documentation](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_security_for_kubernetes) - Red Hat Advanced Cluster Security documentation (select your version, then see *Configuring > Declarative Configuration* and *Operating > Managing Security Policies*)
