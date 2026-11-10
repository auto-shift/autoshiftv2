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
| Agent credentials | A Secret on each target cluster | Referenced by `sharedSecretNamespace` / `sourceSecretName`; the policy copies it into `openshift-workload-availability` |
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

### 1. Create the credential Secret

Create a Secret that holds the agent username and password on each cluster that runs FAR. The
policy reads it on the managed cluster and writes a FAR-shaped Secret into
`openshift-workload-availability`.

```bash
oc create secret generic bmc-shared-creds -n eso-shared \
  --from-literal=username='<bmc-user>' \
  --from-literal=password='<bmc-password>'
```

Point `config.far` at that Secret:

```yaml
config:
  far:
    sharedSecretNamespace: eso-shared
    sourceSecretName: bmc-shared-creds
    sharedSecretName: bmc-shared-creds
```

Use External Secrets Operator or another out-of-band process if the Secret must not be created by
hand. Do not put credentials in values files.

### 2. Define remediation templates

Each entry under `templates` becomes one `FenceAgentsRemediationTemplate` in
`openshift-workload-availability`. Structured fields cover the shared FAR knobs. Agent-specific
flags go in `sharedparameters` and `nodeparameters` and are passed through unchanged.

```yaml
config:
  far:
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
| `agent` | Fence agent binary name (default `fence_redfish`) |
| `sharedparameters` | Cluster-wide agent flags |
| `nodeparameters` | Per-node agent flags; nested maps use Kubernetes `Node` names as keys |
| `nodeSecretNames` | Per-node target Secret name in `openshift-workload-availability` |
| `sourceNodeSecrets` | Per-node source Secret name in `sharedSecretNamespace` |
| `autoNodeIPsFromBMH` | Optional Redfish helper: fill `nodeparameters.--ip` from BareMetalHost data when `--ip` is unset |

#### Per-node credentials

When some nodes use different credentials, create a Secret per node in
`sharedSecretNamespace`, then map those nodes under the template. Nodes omitted from the maps
continue to use the shared Secret.

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
