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

## Placement labels

These four decide which policies a cluster receives, which is why they are labels rather than
`config.acs` entries. Everything else is configuration.

| Label | Description | Default |
|-------|-------------|---------|
| `acs-central` | Run Central on this hub. `false` runs `SecuredCluster` only and registers with an external Central | `true` |
| `acs-registration` | How secured clusters first authenticate: `crs`, `manual`, or `initBundle` (legacy) | `crs` |
| `acs-auth-provider` | Identity provider for Central. `openshift` configures OpenShift authentication; any other value, including no label at all, configures none | `none` |
| `acs-default-policies` | Deploy the baseline `SecurityPolicy` resources. Hub only | off |

## Day 2 configuration

Day 2 settings are **not labels**. They live in a `config.acs` block, documented in full in the
[values reference](../../../docs/values-reference.md#red-hat-advanced-cluster-security) and in
`autoshift/values/clustersets/_example.yaml`.

Earlier releases carried each of these as its own label. Those labels are no longer read, and
setting one has no effect:

| Removed label | Replacement |
|---------------|-------------|
| `acs-egress-connectivity` | `config.acs.egressConnectivity` |
| `acs-scanner-v4` | `config.acs.scannerV4` |
| `acs-monitoring` | `config.acs.monitoring` |
| `acs-vm-scanning` | `config.acs.vmScanning` |
| `acs-network-policies` | `config.acs.networkPolicies` |
| `acs-admission-control` | `config.acs.admissionControl.enabled` |
| `acs-auth-min-role` | `config.acs.auth.minimumRole` |
| `acs-auth-admin-group` | `config.acs.auth.adminGroup` |

`config.acs.auth.provider`, `config.acs.central.deploy` and `config.acs.defaultPolicies` moved the
other way, from configuration to the `acs-auth-provider`, `acs-central` and `acs-default-policies`
labels above, and are likewise no longer read.

### Admission control

`config.acs.admissionControl.enabled` defaults to `false`. When `true` it sets `enforcement:
Enabled` on the `SecuredCluster`, which can block deployments, so turn it on deliberately.

It sets `enforcement` and nothing else. The older `listenOnCreates`, `listenOnUpdates`,
`listenOnEvents` and `contactImageScanners` fields are deprecated in the 4.11 CRD and this policy no
longer emits them.

### Declarative authentication (hub only)

`acs-auth-provider: openshift` makes the policy add `declarativeConfiguration` to the Central
resource and create the `acs-declarative-configs` ConfigMap in the `stackrox` namespace with the
OpenShift OAuth configuration. Tune it with `config.acs.auth.minimumRole` (default `None`) and
`config.acs.auth.adminGroup` (default `cluster-admins`).

### Security policies (hub only)

`acs-default-policies: 'true'` deploys three baseline `SecurityPolicy` resources
(`config.stackrox.io/v1alpha1`) to the Central namespace. These become "externally managed" in the
Red Hat Advanced Cluster Security console:

| Policy | Lifecycle | Description |
|--------|-----------|-------------|
| No Privilege Escalation | DEPLOY | Detects containers with `allowPrivilegeEscalation: true` |
| No Root User Containers | DEPLOY | Detects containers running as UID 0 |
| No Shell Spawning at Runtime | RUNTIME | Detects shell execution (`/bin/sh`, `/bin/bash`, `/bin/dash`) in running containers |

All policies are **inform-only** by default, with no enforcement actions. Add enforcement or further
`SecurityPolicy` resources through per-cluster overrides.

## Example Configuration

### Hub cluster (full Day 2)

```yaml
labels:
  acs: 'true'
  acs-subscription-name: rhacs-operator
  acs-channel: stable
  acs-source: redhat-operators
  acs-source-namespace: openshift-marketplace
  acs-auth-provider: openshift
  # acs-default-policies: 'true'
config:
  acs:
    scannerV4: Enabled
    auth:
      minimumRole: None
      adminGroup: cluster-admins
```

### Managed cluster (minimal)

```yaml
labels:
  acs: 'true'
  acs-subscription-name: rhacs-operator
  acs-channel: stable
  acs-source: redhat-operators
  acs-source-namespace: openshift-marketplace
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
      roxctlImage: ''        # blank = the image Central itself is running
```

The mint Job runs `roxctl`, the documented way to generate a cluster registration secret. When
`roxctlImage` is blank the Job takes the image Central itself is running: the Operator has already
resolved that to a digest through `registryOverride` and the cluster mirrors, and the main image
ships `roxctl` alongside Central. That is what makes the Job work in a disconnected deployment,
because Red Hat's offline image list has no standalone `roxctl` image for `oc-mirror` to copy. If
Central is not visible to the policy, the Job falls back to
`registry.redhat.io/advanced-cluster-security/rhacs-roxctl-rhel9`, tagged from the `acs-version`
label or the installed Operator's current cluster service version. That fallback is a floating tag
and needs an ImageTagMirrorSet to resolve in a mirrored registry, so set `roxctlImage` explicitly if
you rely on it.

The other containers in the Job only run `oc`. They resolve their image from the cluster's own
`openshift/cli` ImageStream, with `config.images.cli` as an override. See the
[values reference](../../../docs/values-reference.md#helper-job-images).

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
