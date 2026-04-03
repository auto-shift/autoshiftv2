# Workload Partitioning

Workload partitioning dedicates a set of CPUs to the OpenShift control plane and platform services (reserved), making the remaining CPUs available exclusively for user workloads (isolated). This is done by deploying a `PerformanceProfile` CR, which the Node Tuning Operator consumes.

## When to Use

- **SNO (Single Node OpenShift)** — the single node runs everything; partitioning prevents user workloads from starving the control plane
- **Compact 3-node clusters** — masters are also workers; same reasoning
- **Masters doubling as infra nodes** — control plane + monitoring/logging/ingress share the same nodes alongside user workloads
- **Telco / RAN / HPC** — latency-sensitive workloads that need deterministic CPU allocation

Dedicated worker nodes do **not** need workload partitioning — they already run only user workloads.

## How It Works

The workload-partitioning policy reads configuration from the rendered-config ConfigMap and creates a `PerformanceProfile` on the managed cluster. The Node Tuning Operator then:

1. Creates a `MachineConfig` that configures CRI-O and kubelet CPU sets
2. Applies the CPU manager policy (`static`) to the targeted nodes
3. Pins platform pods to the reserved CPUs via CRI-O annotations
4. Makes isolated CPUs available exclusively for pods with CPU requests

### Install-Time vs Post-Install

| | Install-Time (`cpuPartitioning: AllNodes`) | Post-Install (PerformanceProfile only) |
|---|---|---|
| Platform pod pinning | Full — CRI-O pins all platform pods to reserved CPUs from first boot | Partial — existing platform pods may not be pinned until restart |
| When to set | In `clusterInstall` config before provisioning | Any time via the workload-partitioning policy |
| Can be changed later | No — install-time only | Yes — update the PerformanceProfile |

For new clusters, set **both** `cpuPartitioning: AllNodes` in the cluster-install config and configure the `workloadPartitioning` config block. For existing clusters, only the PerformanceProfile is needed.

## Configuration

### 1. Enable the Label

Add to your clusterset or per-cluster values:

```yaml
labels:
  workload-partitioning: 'true'
```

### 2. Add the Config Block

In your per-cluster values file under `config`:

```yaml
clusters:
  my-cluster:
    config:
      workloadPartitioning:
        reservedCpus: '0-3,16-19'
        isolatedCpus: '4-15,20-31'
        nodeSelector:
          node-role.kubernetes.io/master: ''
```

### 3. (Optional) Enable Install-Time Partitioning

For new baremetal clusters provisioned via cluster-install:

```yaml
      clusterInstall:
        cpuPartitioning: 'AllNodes'
```

## Config Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `reservedCpus` | string | **(required)** | CPU set for control plane, OS, and platform services |
| `isolatedCpus` | string | **(required)** | CPU set for user workloads |
| `nodeSelector` | map | `node-role.kubernetes.io/master: ''` | Which nodes the PerformanceProfile targets |
| `numaTopology` | string | *(none)* | Topology Manager policy — see [NUMA Topology Policy](#numa-topology-policy) |
| `realTimeKernel` | bool | `false` | Enable the real-time kernel |
| `globallyDisableIrqLoadBalancing` | bool | `false` | Disable IRQ load balancing on isolated CPUs |
| `hugepages.defaultSize` | string | *(none)* | Default huge page size (e.g., `1G`, `2M`) |
| `hugepages.pages` | list | *(none)* | List of `{size, count, node}` huge page allocations |

## Understanding CPU Sets

### Reserved vs Isolated

- **Reserved** (`reservedCpus`) — CPUs dedicated to the operating system, kubelet, CRI-O, etcd, API server, and other core platform components. Nothing else runs on these CPUs.
- **Isolated** (`isolatedCpus`) — CPUs available for everything else allowed to run on that node (user workloads, infra pods, operators, etc.). Core platform components will not run on these CPUs. The two sets must cover all CPUs on the node and must not overlap.

### Determining Your CPU Topology

CPU numbering varies by hardware vendor and socket count. Before configuring CPU sets, verify the actual layout on a target node:

```bash
# Show CPU topology (core ID, socket, NUMA node, HT sibling)
lscpu -e

# Show NUMA layout
numactl --hardware

# Show HT sibling pairs for a specific CPU
cat /sys/devices/system/cpu/cpu0/topology/thread_siblings_list
```

### Hyperthreading

When hyperthreading is enabled, each physical core exposes two logical CPUs (siblings). Always reserve **both siblings together**. Splitting siblings across reserved and isolated causes jitter — the OS scheduler on the reserved sibling can preempt work on the isolated sibling.

The sibling numbering depends on the hardware. Common patterns:

- **Intel**: On a 60-core system, core `N` and core `N+60` are typically siblings (e.g., CPU 0 and CPU 60)
- **AMD**: Siblings are often adjacent (e.g., CPU 0 and CPU 1)

Always verify with `lscpu -e` rather than assuming.

### NUMA Nodes

On multi-socket systems, each CPU socket has its own memory controller and local memory, forming a NUMA (Non-Uniform Memory Access) node. Accessing memory on the local NUMA node is fast; accessing memory on a remote NUMA node crosses the socket interconnect and adds latency.

**Key guideline**: Keep reserved CPUs on the **same NUMA node** so that etcd and the API server share a memory controller and avoid cross-socket latency.

A typical dual-socket layout:

```
Socket 0 (NUMA 0): physical cores + their HT siblings
Socket 1 (NUMA 1): physical cores + their HT siblings
```

The exact core-to-NUMA mapping varies — use `numactl --hardware` to confirm.

## NUMA Topology Policy

The `numaTopology` field controls how the kubelet assigns CPUs and memory to **individual pods**. This is the Topology Manager policy:

| Policy | Behavior |
|--------|----------|
| `single-numa-node` | Pod's CPUs and memory must all come from the **same** NUMA node. The pod is rejected if it can't fit on one node. Provides the lowest latency but limits pod size to what fits on a single NUMA node. |
| `best-effort` | Tries to align CPUs and memory on the same NUMA node, but will spread across nodes if needed. No pod rejection. Good default for general-purpose workloads. |
| `restricted` | Like `best-effort` but **rejects** pods that request topology-aware resources (CPU/memory) if they can't be aligned on the same NUMA node. A middle ground between the other two. |
| *(not set)* | No topology awareness. The kubelet schedules CPUs from any NUMA node without preference. |

On single-socket systems, this setting has no practical effect since all CPUs share one NUMA node.

## Sizing Reserved CPUs

The reserved set must be large enough for the platform components running on that node. The more services sharing the node, the more reserved CPUs are needed.

**Control plane services** (etcd, API server, controller-manager, scheduler) are the baseline. Etcd is typically the most resource-sensitive — it needs low-latency CPU access to maintain cluster health.

A minimum of 4 physical cores (8 with HT) is needed for the reserved set on a pure control plane node. If infra workloads (monitoring, logging, ingress, etc.) are also running on those nodes, the isolated set needs to be large enough to handle them alongside any user workloads — both compete for the same isolated CPUs.

## Verifying

After the policy is applied and the nodes reboot (MachineConfig rollout):

```bash
# Check PerformanceProfile status
oc get performanceprofile workload-partitioning -o yaml

# Verify CPU manager is active
oc get kubeletconfig -o yaml | grep cpuManagerPolicy

# Check reserved/isolated on a node
oc debug node/<node-name> -- chroot /host cat /etc/kubernetes/kubelet.conf | grep -A5 cpuManager

# Verify CRI-O workload pinning
oc debug node/<node-name> -- chroot /host cat /etc/crio/crio.conf.d/99-workload-pinning.conf
```

## Troubleshooting

### Node stuck in NotReady after PerformanceProfile applied
The MachineConfig rollout reboots nodes one at a time. If a node doesn't come back, the CPU set may be invalid (e.g., specifying CPUs that don't exist on the hardware). Check `oc describe node` and the MachineConfigPool status.

### Pods rejected with TopologyAffinityError
The `single-numa-node` topology policy rejects pods that can't fit entirely on one NUMA node. Either reduce the pod's CPU request or switch to `best-effort`.

### PerformanceProfile degraded
Check the Node Tuning Operator logs:
```bash
oc logs -n openshift-cluster-node-tuning-operator deploy/cluster-node-tuning-operator
```

Common causes: invalid CPU set syntax, overlapping reserved/isolated ranges, or specifying CPUs beyond the node's count.
