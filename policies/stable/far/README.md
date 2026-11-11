# far AutoShift Policy

## Overview

Installs the fence-agents-remediation operator and creates `FenceAgentsRemediationTemplate`
resources so Red Hat OpenShift can fence nodes with any Fence Agents Remediation (FAR)
supported agent. Configuration lives under `config.far` in AutoShift values files.
Credentials stay in a Secret you create on the cluster; values files never hold BMC passwords.

Enable the policy with `far: 'true'` on the clusterset or cluster that should run FAR.

## Before you start

| You need | Where | Notes |
|---|---|---|
| Agent credentials | A Secret on each target cluster | `secretsMode=copy` reads `sharedSecretNamespace` / `sourceSecretName` and writes into `openshift-workload-availability`; `secretsMode=external` expects FAR-shaped Secrets already there |
| Agent parameters | `config.far.templates[].sharedparameters` and `nodeparameters` | Passed through to the FAR template; shape depends on the agent |
| `far: 'true'` label | Clusterset or per-cluster values | Labels are defined in values only; never set `autoshift.io/*` on a `ManagedCluster` by hand |

## Enable on clusters

```yaml
# autoshift/values/clustersets/hub.yaml (or managed / per-cluster overrides)
hubClusterSets:
  hub:
    labels:
      far: 'true'
      far-subscription-name: 'fence-agents-remediation'
      far-channel: 'stable'
      far-source: 'redhat-operators'
      far-source-namespace: 'openshift-marketplace'
      # far-version: 'fence-agents-remediation.v1.x.x'  # Optional: pin to a specific CSV
```

## Configure FAR

Put the FAR block under `config` for the same clusterset or cluster. A full example lives in
`autoshift/values/clustersets/_example.yaml`.

### 1. Deliver the credential Secret

FAR reads `--username` and `--password` from Secrets in `openshift-workload-availability`.
Choose how those Secrets get there with `far.secretsMode`.

#### `secretsMode: copy` (default)

Create a Secret with `username` and `password` keys on each cluster. The policy copies it into
`openshift-workload-availability` and remaps the keys to the FAR shape.

```bash
oc create secret generic bmc-shared-creds -n eso-shared \
  --from-literal=username='<bmc-user>' \
  --from-literal=password='<bmc-password>'
```

```yaml
config:
  far:
    secretsMode: copy
    sharedSecretNamespace: eso-shared
    sourceSecretName: bmc-shared-creds
    sharedSecretName: bmc-shared-creds
```

#### `secretsMode: external`

Use External Secrets Operator or another out-of-band process to land the Secret directly in
`openshift-workload-availability`. The Secret must already use FAR keys `--username` and
`--password`. The enforce secrets policy does not copy; `policy-far-secrets-ready` informs until
the Secret exists and blocks remediation templates until it is Compliant.

```bash
oc create secret generic bmc-shared-creds -n openshift-workload-availability \
  --from-literal=--username='<bmc-user>' \
  --from-literal=--password='<bmc-password>'
```

```yaml
config:
  far:
    secretsMode: external
    sharedSecretName: bmc-shared-creds
```

Do not put credentials in values files.

### 2. Define remediation templates

Each entry under `templates` becomes one `FenceAgentsRemediationTemplate` in
`openshift-workload-availability`. Structured fields cover the shared FAR knobs. Agent-specific
flags go in `sharedparameters` and `nodeparameters` and are passed through unchanged.

```yaml
config:
  far:
    secretsMode: copy
    sharedSecretNamespace: eso-shared
    sourceSecretName: bmc-shared-creds
    sharedSecretName: bmc-shared-creds
    templates:
      # Redfish: per-node BMC hosts in nodeparameters.--ip
      - name: far-template-redfish
        agent: fence_redfish
        remediationStrategy: OutOfServiceTaint
        retrycount: 15
        retryinterval: '5s'
        timeout: '60s'
        sharedparameters:
          '--action': reboot
          '--ssl-insecure': ''
          '--systems-uri': '/redfish/v1/Systems/1'
        nodeparameters:
          '--ip':
            worker-0: '10.0.0.11'
            worker-1: '10.0.0.12'
      # Cisco UCS: shared UCS Manager IP, per-node plugs
      - name: far-template-cisco-ucs
        agent: fence_cisco_ucs
        remediationStrategy: OutOfServiceTaint
        retrycount: 15
        retryinterval: '5s'
        timeout: '60s'
        sharedparameters:
          '--action': reboot
          '--ip': ucs-manager.example.com
          '--ssl-insecure': ''
        nodeparameters:
          '--plug':
            worker-0: 'org-root/ls-worker-0'
            worker-1: 'org-root/ls-worker-1'
```

Use one template entry per agent configuration you need on the cluster. Agent flags in
`sharedparameters` and `nodeparameters` are passed through unchanged.

| Field | Purpose |
|---|---|
| `secretsMode` | `copy` (default) remaps from a source namespace; `external` expects FAR-shaped Secrets already in `openshift-workload-availability` |
| `agent` | Fence agent binary name (default `fence_redfish`) |
| `sharedparameters` | Cluster-wide agent flags |
| `nodeparameters` | Per-node agent flags; nested maps use Kubernetes `Node` names as keys |
| `nodeSecretNames` | Per-node target Secret name in `openshift-workload-availability` |
| `sourceNodeSecrets` | Per-node source Secret name in `sharedSecretNamespace` (`copy` mode only) |
| `autoNodeIPsFromBMH` | Optional Redfish helper: fill `nodeparameters.--ip` from BareMetalHost data when `--ip` is unset |

#### Per-node credentials

When some nodes use different credentials under `secretsMode: copy`, create a Secret per node in
`sharedSecretNamespace`, then map those nodes under the template. Nodes omitted from the maps
continue to use the shared Secret. Under `secretsMode: external`, land each per-node Secret in
`openshift-workload-availability` with `--username` / `--password` and list them in
`nodeSecretNames` only.

```bash
oc create secret generic eso-worker-0-creds -n eso-shared \
  --from-literal=username='<bmc-user-worker-0>' \
  --from-literal=password='<bmc-password-worker-0>'
```

```yaml
templates:
  - name: shutdown-template
    sharedparameters:
      '--action': reboot
      '--ssl-insecure': ''
      '--systems-uri': '/redfish/v1/Systems/1'
    nodeparameters:
      '--ip':
        worker-0: '10.0.0.11'
        worker-1: '10.0.0.12'
    # worker-0 uses its own credentials; worker-1 keeps the shared Secret
    nodeSecretNames:
      worker-0: bmc-worker-0-creds
    sourceNodeSecrets:
      worker-0: eso-worker-0-creds
```

### 3. Optional: auto-fill Redfish `--ip` from BareMetalHost

Set `autoNodeIPsFromBMH: true` when using an agent that needs per-node BMC hosts in
`nodeparameters.--ip` and you want those values derived from `metal3.io/v1alpha1`
BareMetalHost data. The helper runs only when `nodeparameters.--ip` is not already set.
When present, the Assisted Installer hostname annotation
`bmac.agent-install.openshift.io/hostname` is preferred over the BareMetalHost name.

```yaml
templates:
  - name: shutdown-template
    agent: fence_redfish
    autoNodeIPsFromBMH: true
    sharedparameters:
      '--action': reboot
      '--ssl-insecure': ''
      '--systems-uri': '/redfish/v1/Systems/1'
```

On managed clusters, AutoShift reads BareMetalHosts from the hub in that cluster's namespace
first, then falls back to `openshift-machine-api` on the managed cluster. On the self-managed
hub, set `nodeparameters.--ip` explicitly.

Before enabling auto mode on a hub-provisioned bare-metal cluster, confirm the BareMetalHost
exists on the hub:

```bash
oc get baremetalhost -n <cluster-name>
oc get baremetalhost -n <cluster-name> -o jsonpath='{.items[*].spec.bmc.address}{"\n"}'
```

If that list is empty, or `spec.bmc.address` is missing, set `nodeparameters.--ip` instead.

## Observability

`policy-far-secrets-ready` is inform-only and must be Compliant before remediation templates apply.
`policy-far-active-remediations` is inform-only and becomes NonCompliant while any
`FenceAgentsRemediation` exists in `openshift-workload-availability`, which means Node Health Check
is remediating a node. It does not block template policies.
