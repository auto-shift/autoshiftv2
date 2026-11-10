# Node Feature Discovery

Installs the Node Feature Discovery (NFD) Operator and creates the `NodeFeatureDiscovery` instance
that labels each node with its hardware: PCI devices, CPU features, kernel and operating system.
GPU Operators and other workloads select nodes by these labels.

## Policies

| Policy | What it does |
|---|---|
| `policy-nfd-operator-install` | `openshift-nfd` namespace and the `nfd` Operator subscription |
| `policy-nfd-instance-deploy` | The `nfd-instance` NodeFeatureDiscovery and any NodeFeatureRules from configuration. Waits for the Operator |

## Enabling

```yaml
hubClusterSets:
  hub:
    labels:
      node-feature-discovery: 'true'
      node-feature-discovery-subscription-name: nfd
      node-feature-discovery-channel: stable
      node-feature-discovery-source: redhat-operators
      node-feature-discovery-source-namespace: openshift-marketplace
      # node-feature-discovery-version: 'nfd.4.22.0-202610140833'   # optional CSV pin
```

Set labels in values files only. The `cluster-labels` policy stamps them onto each cluster.

## Default configuration

With no `config.nodeFeatureDiscovery` block the instance uses the default from the 4.22 Operator's
own example:

```yaml
core:
  sleepInterval: 60s
sources:
  pci:
    deviceClassWhitelist:
      - "0200"    # network controllers
      - "03"      # display controllers, including GPUs
      - "12"      # processing accelerators
    deviceLabelFields:
      - vendor
```

Labeling PCI devices by vendor gives a node with an NVIDIA card
`feature.node.kubernetes.io/pci-10de.present=true`. The NVIDIA GPU Operator selects GPU nodes by
that label, so the default works for Red Hat OpenShift AI and for OpenShift Virtualization GPU
passthrough without further configuration.

OpenShift Virtualization does not need NFD for CPU models; it labels nodes with its own node
labeller. NFD matters for virtualization only when GPUs or other PCI devices are passed to virtual
machines.

The instance does not set `operand.image`. The Operator then runs the image named in its own CSV,
which is pinned by digest and listed among the bundle's related images, so `oc-mirror` copies it and
an `ImageDigestMirrorSet` redirects it on a disconnected cluster. A tagged image would bypass digest
mirroring.

## Custom configuration

Set `config.nodeFeatureDiscovery` in a clusterset or cluster values file. Every key is optional.

```yaml
config:
  nodeFeatureDiscovery:
    workerConfig:            # nfd-worker.conf; replaces the default outright
      core:
        sleepInterval: '60s'
      sources:
        pci:
          deviceClassWhitelist:
            - '0200'
            - '03'
            - '12'
          deviceLabelFields:
            - 'vendor'
    nodeFeatureRules:        # one NodeFeatureRule per key, in openshift-nfd
      nvidia-gpu:
        rules:
          - name: 'nvidia gpu present'
            labels:
              nvidia-gpu: 'true'
            matchFeatures:
              - feature: 'pci.device'
                matchExpressions:
                  vendor:
                    op: 'In'
                    value:
                      - '10de'
    topologyUpdater: false   # NFD topology updater; the NUMA Resources Operator is the supported path
    enableTaints: false      # lets NodeFeatureRules set taints
    extraLabelNs: []         # label prefixes allowed beyond feature.node.kubernetes.io
```

- `workerConfig` replaces the default rather than merging with it. Copy the default and extend it,
  or a node loses the vendor labels the GPU Operators depend on. Its lists, `deviceClassWhitelist`
  and `deviceLabelFields`, are replaced whole by a cluster that sets them, like every list in config.
- A rule label with no prefix lands in `feature.node.kubernetes.io/`. Any other prefix must be in
  `extraLabelNs`, or `nfd-master` drops the label.
- The instance and each rule are applied with `mustonlyhave`, so an edited rule replaces the old
  one and a removed `extraLabelNs` entry leaves the cluster. Removing an
  entry from `nodeFeatureRules` does not delete the NodeFeatureRule from the cluster; delete it
  with `oc delete nodefeaturerule <name> -n openshift-nfd`.
- Every rule needs `rules`, and its name must be a lowercase Kubernetes object name. The policy
  fails and names the entry otherwise.
- The policy also fails when the cluster's rendered-config ConfigMap is missing, rather than
  applying defaults to a cluster whose configuration never arrived.

### Rules per cluster and per node

NFD labels each node from its own hardware, so one fleet-wide set of rules gives a mixed fleet the
right labels node by node, with no per-node configuration. What varies by cluster is which rules
apply, and the form of `nodeFeatureRules` decides how that can be expressed:

| Form | Shape | A cluster that sets it |
|---|---|---|
| Map, keyed by NodeFeatureRule name | `<name>: {rules: [...], enabled: true}` | Merges with its clusterset by name: it can add a rule, override one, or switch one off with `enabled: false` |
| List | `- name: <name>` with `rules: [...]` | Replaces the clusterset's whole list, because config merges replace lists |

Use the map form. The list form is still accepted, for existing values files. Map keys render in
sorted order, and `enabled` defaults to `true`.

A cluster with no AMD hardware and one extra device, in `clusters/<name>.yaml`:

```yaml
config:
  nodeFeatureDiscovery:
    nodeFeatureRules:
      amd-gpu:
        enabled: false
      fpga-xilinx:
        rules:
          - name: 'xilinx fpga present'
            labels:
              xilinx-fpga: 'true'
            matchFeatures:
              - feature: 'pci.device'
                matchExpressions:
                  vendor: {op: 'In', value: ['10ee']}
```

Switching a rule off stops the policy managing it but does not delete the NodeFeatureRule already on
the cluster; delete it with `oc delete nodefeaturerule <name> -n openshift-nfd`.

### Accelerators other than NVIDIA

AMD Instinct and Intel Gaudi accelerators need NFD installed, and their Operators expect labels
that the default does not produce. The clusterset example carries starting rules, `amd-gpu` (PCI
vendor `1002`, display and accelerator classes) and `intel-gaudi` (vendor `1da3`, accelerator
class). Check each label against the node selector of the Operator release you install, and
override the rule by name where it differs.

## Verifying

```bash
oc get nodefeaturediscovery nfd-instance -n openshift-nfd
oc get pods -n openshift-nfd
oc get nodes -L feature.node.kubernetes.io/pci-10de.present
oc get nodefeaturerule -n openshift-nfd
```

## References

- [Node Feature Discovery Operator](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/specialized_hardware_and_driver_enablement/psap-node-feature-discovery-operator)
- [NVIDIA GPU architecture](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/hardware_accelerators/nvidia-gpu-architecture)
- [AMD GPU Operator](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/hardware_accelerators/amd-gpu-operator)
