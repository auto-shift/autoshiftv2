# NVIDIA GPU Operator Policy

This policy installs and configures the NVIDIA GPU Operator on OpenShift clusters with NVIDIA GPUs.

## What Gets Installed

| Policy | Description |
|--------|-------------|
| `policy-nvidia-gpu-operator-install` | Installs the gpu-operator-certified operator |
| `policy-nvidia-gpu-config` | Creates ClusterPolicy to configure GPU stack |

## Prerequisites

- Nodes with NVIDIA GPUs
- Node Feature Discovery (NFD) operator installed (recommended)
- Entitled RHEL nodes (for driver compilation) OR pre-built driver containers

## Quick Start

Enable NVIDIA GPU operator:

```yaml
hubClusterSets:
  hub:
    labels:
      # Recommended: Enable NFD first
      node-feature-discovery: 'true'
      node-feature-discovery-subscription-name: nfd
      node-feature-discovery-channel: stable
      node-feature-discovery-source: redhat-operators
      node-feature-discovery-source-namespace: openshift-marketplace
      
      # NVIDIA GPU Operator
      nvidia-gpu: 'true'
      nvidia-gpu-subscription-name: gpu-operator-certified
      nvidia-gpu-channel: stable
      nvidia-gpu-source: certified-operators
      nvidia-gpu-source-namespace: openshift-marketplace
```

## Configuration Labels

### Operator Configuration

| Label | Default | Description |
|-------|---------|-------------|
| `nvidia-gpu` | - | Enable NVIDIA GPU operator (`'true'` or `'false'`) |
| `nvidia-gpu-subscription-name` | `gpu-operator-certified` | OLM package name |
| `nvidia-gpu-channel` | `stable` | Operator channel |
| `nvidia-gpu-version` | - | Pin to specific CSV version (optional) |
| `nvidia-gpu-source` | `certified-operators` | Catalog source |
| `nvidia-gpu-source-namespace` | `openshift-marketplace` | Catalog namespace |

### ClusterPolicy Component Configuration

| Label | Default | Description |
|-------|---------|-------------|
| `nvidia-gpu-driver` | `true` | Install GPU driver |
| `nvidia-gpu-driver-toolkit` | `true` | Use OpenShift Driver Toolkit |
| `nvidia-gpu-toolkit` | `true` | Install CUDA toolkit |
| `nvidia-gpu-device-plugin` | `true` | Enable Kubernetes device plugin |
| `nvidia-gpu-dcgm` | `true` | Enable DCGM exporter for metrics |
| `nvidia-gpu-gfd` | `true` | Enable GPU Feature Discovery |
| `nvidia-gpu-mig` | `true` | Enable MIG (Multi-Instance GPU) manager |
| `nvidia-gpu-mps` | `false` | Enable MPS (Multi-Process Service) |
| `nvidia-gpu-nfd` | `false` | Use built-in NFD (set false if using separate NFD) |
| `nvidia-gpu-sandbox` | `false` | Enable sandbox workloads |
| `nvidia-gpu-vgpu` | `false` | Enable vGPU support (requires license) |

## Examples

### Basic GPU Setup

```yaml
nvidia-gpu: 'true'
nvidia-gpu-subscription-name: gpu-operator-certified
nvidia-gpu-channel: stable
nvidia-gpu-source: certified-operators
nvidia-gpu-source-namespace: openshift-marketplace
```

### GPU with Separate NFD Operator

```yaml
# Use the separate NFD operator (recommended)
node-feature-discovery: 'true'
node-feature-discovery-subscription-name: nfd
node-feature-discovery-channel: stable
node-feature-discovery-source: redhat-operators
node-feature-discovery-source-namespace: openshift-marketplace

# GPU Operator with built-in NFD disabled
nvidia-gpu: 'true'
nvidia-gpu-subscription-name: gpu-operator-certified
nvidia-gpu-channel: stable
nvidia-gpu-source: certified-operators
nvidia-gpu-source-namespace: openshift-marketplace
nvidia-gpu-nfd: 'false'
```

### GPU with MIG Enabled

```yaml
nvidia-gpu: 'true'
nvidia-gpu-subscription-name: gpu-operator-certified
nvidia-gpu-channel: stable
nvidia-gpu-source: certified-operators
nvidia-gpu-source-namespace: openshift-marketplace
nvidia-gpu-mig: 'true'
```

### Minimal GPU (Driver + Device Plugin only)

```yaml
nvidia-gpu: 'true'
nvidia-gpu-subscription-name: gpu-operator-certified
nvidia-gpu-channel: stable
nvidia-gpu-source: certified-operators
nvidia-gpu-source-namespace: openshift-marketplace
nvidia-gpu-dcgm: 'false'
nvidia-gpu-mig: 'false'
nvidia-gpu-gfd: 'false'
```

## Integration with RHOAI

For RHOAI GPU workloads, enable both NFD and GPU operator:

```yaml
# NFD for hardware detection
node-feature-discovery: 'true'
node-feature-discovery-subscription-name: nfd
node-feature-discovery-channel: stable
node-feature-discovery-source: redhat-operators
node-feature-discovery-source-namespace: openshift-marketplace

# GPU Operator
nvidia-gpu: 'true'
nvidia-gpu-subscription-name: gpu-operator-certified
nvidia-gpu-channel: stable
nvidia-gpu-source: certified-operators
nvidia-gpu-source-namespace: openshift-marketplace

# RHOAI
rhoai: 'true'
rhoai-subscription-name: rhods-operator
rhoai-channel: fast-3.x
rhoai-source: redhat-operators
rhoai-source-namespace: openshift-marketplace
```

## Verification

Check GPU operator installation:

```bash
# Check operator
oc get csv -n nvidia-gpu-operator | grep gpu

# Check ClusterPolicy
oc get clusterpolicy

# Check ClusterPolicy status
oc describe clusterpolicy gpu-cluster-policy

# Check GPU operator pods
oc get pods -n nvidia-gpu-operator

# Check GPU nodes are labeled
oc get nodes -l nvidia.com/gpu.present=true

# Verify GPUs are detected
oc get nodes -o json | jq '.items[].status.allocatable | select(.["nvidia.com/gpu"] != null)'
```

## Troubleshooting

### Driver not installing

```bash
# Check driver pod logs
oc logs -n nvidia-gpu-operator -l app=nvidia-driver-daemonset

# Check if node has GPU hardware
oc describe node <node-name> | grep -i nvidia

# Verify NFD detected the GPU
oc get node <node-name> -o json | jq '.metadata.labels | with_entries(select(.key | startswith("feature.node.kubernetes.io/pci-10de")))'
```

### ClusterPolicy not ready

```bash
# Check ClusterPolicy status
oc describe clusterpolicy gpu-cluster-policy

# Check all GPU pods
oc get pods -n nvidia-gpu-operator

# Check events
oc get events -n nvidia-gpu-operator --sort-by='.lastTimestamp'
```

### Pods can't request GPUs

```bash
# Verify device plugin is running
oc get pods -n nvidia-gpu-operator -l app=nvidia-device-plugin-daemonset

# Check node allocatable resources
oc get nodes -o custom-columns="NAME:.metadata.name,GPU:.status.allocatable.nvidia\.com/gpu"
```

## values.yaml Reference

```yaml
nvidiaGpu:
  # Operator settings
  name: gpu-operator-certified
  namespace: nvidia-gpu-operator
  channel: stable
  source: certified-operators
  sourceNamespace: openshift-marketplace
  operatorGroupName: nvidia-gpu-operator-group

  # ClusterPolicy settings
  clusterPolicy:
    name: gpu-cluster-policy
    driver:
      enabled: true
      useOpenShiftDriverToolkit: true
    toolkit:
      enabled: true
    devicePlugin:
      enabled: true
    dcgmExporter:
      enabled: true
    migManager:
      enabled: true
    nfd:
      enabled: false  # Use separate NFD operator
    gfd:
      enabled: true
    mps:
      enabled: false
    sandboxWorkloads:
      enabled: false
    vgpuManager:
      enabled: false
    vgpuDeviceManager:
      enabled: false
```
