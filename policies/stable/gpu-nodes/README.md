# gpu-nodes

Creates GPU MachineSets by cloning the cluster's own worker MachineSet, so a cluster can grow GPU
capacity without anyone hand-writing a MachineSet.

AWS only. The NVIDIA GPU Operator installs the driver stack; this policy only provides the hardware
for it to run on.

## Labels

| Label | Required | Example | Meaning |
|-------|----------|---------|---------|
| `autoshift.io/gpu-nodes` | yes | `'0'` | Replicas per zone. `0` is a valid resting state — the MachineSet exists and costs nothing until scaled. |
| `autoshift.io/gpu-nodes-provider` | yes | `aws` | Selects the placement. Only `aws` is implemented. |
| `autoshift.io/gpu-nodes-instance-type` | no | `g6.xlarge` | Defaults to the worker's instance type, which is almost never what you want for a GPU node — set it. |
| `autoshift.io/gpu-nodes-region` | no | `us-east-2` | Defaults to the worker's region. |
| `autoshift.io/gpu-nodes-zone-1` | no | `us-east-2a` | Add `-zone-2`, `-zone-3` for multiple zones. Defaults to the worker's zone. |
| `autoshift.io/gpu-nodes-spot` | no | `'true'` | Bid up to the on-demand price. See the caveat below. |
| `autoshift.io/gpu-nodes-accelerator` | no | `nvidia-l4` | The `cluster-api/accelerator` value. Blank derives it from the instance family: `g4dn` `nvidia-t4`, `g5` `nvidia-a10g`, `g6` `nvidia-l4`, `g6e` `nvidia-l40s`, `p4d`/`p4de` `nvidia-a100`, `p5` `nvidia-h100`, `p5e`/`p5en` `nvidia-h200`; any other family is used as is. |

## What the nodes get

- Label `node-role.kubernetes.io/gpu: ''` plus `node-role.kubernetes.io/worker: ''`
- Label `cluster-api/accelerator: <type>`, which the cluster autoscaler reads to know what GPU a new
  node adds. Its value must match a `resourceLimits.gpus[].type` in the `ClusterAutoscaler`.
- Taint `nvidia.com/gpu:NoSchedule`, valueless, so an `operator: Exists` toleration matches

The taint means only workloads that ask for a GPU land on an expensive instance. The GPU operator's
own operands tolerate it, so the driver stack still installs.

Node Feature Discovery has to run on these nodes too: its PCI vendor label is how the GPU Operator
finds them. On OpenShift 4.22 the default `nfd-worker` tolerations cover a `NoSchedule` taint, and
the vendor label appeared on tainted GPU nodes with no extra configuration. OpenShift 4.20 added
`spec.operand.workerTolerations` to the `NodeFeatureDiscovery` resource for clusters that need them
stated; to confirm a cluster's worker tolerates the taint:

```bash
oc get daemonset nfd-worker -n openshift-nfd -o jsonpath='{.spec.template.spec.tolerations}{"\n"}'
```

## Two things that are easy to get wrong

**Subnets are inherited, not reconstructed.** The sibling node policies build an AWS subnet filter
from the infrastructure ID (`<infra-id>-subnet-private-<zone>`). That only holds for a VPC the
installer created. A cluster installed into a pre-existing VPC via `platform.aws.vpc.subnets`
references its subnet by raw ID, and the VPC owner chose the `Name` tags — so a reconstructed
filter matches nothing and every machine fails to provision with no subnet found. This policy
copies `subnet`, `securityGroups` and `blockDevices` verbatim from the base worker, which is
correct either way. When several zones are requested, each GPU MachineSet clones the worker that
already lives in that zone. A requested zone with no worker fails with a message: a subnet belongs to
one zone, so borrowing another zone's would only fail later, when AWS creates the machine.

**Spot has no fallback.** `autoshift.io/gpu-nodes-spot: 'true'` sets `spotMarketOptions: {}`. If
spot capacity is unavailable — or denied by a service control policy, which looks identical from
inside the cluster — the machines never appear. There is no declarative way to fall back to
on-demand, so the failure surfaces as `policy-gpu-nodes-test` going NonCompliant rather than as a
quiet downgrade. That is the intended behaviour; a silent switch to on-demand would be a surprise
on the bill.

## With the cluster autoscaler

Once a `MachineAutoscaler` targets a GPU MachineSet, this policy stops writing `spec.replicas`. The
autoscaler owns the count from then on, and the two would otherwise overwrite each other. The signal
is the `autoscaling.openshift.io/machineautoscaler` annotation the autoscaler controller puts on the
MachineSet, so the switch needs no configuration. `autoshift.io/gpu-nodes` still sets the count when
the MachineSet is first created and whenever nothing autoscales it.

The readiness test follows the same rule: under a `MachineAutoscaler` it expects the replicas the
MachineSet currently asks for, not the label's count. It also skips GPU nodes on their way out (a
`deletionTimestamp`, the autoscaler's `ToBeDeletedByClusterAutoscaler` taint, or a Machine being
deleted), because such a node goes NotReady while it drains during a normal scale-down.

The test placement still keys on `autoshift.io/gpu-nodes`, so keep the autoscaler's minimum for the
GPU MachineSet at 1 or more while that label is nonzero. An autoscaler free to scale GPU nodes to 0
leaves the capacity check with no GPU node, which it reports as a failure.

Machines keep the node labels they were created with. A GPU node that existed before this policy
set `cluster-api/accelerator` has no such label until it is replaced or labeled by hand:
`oc label node <node> cluster-api/accelerator=<type>`.

Observed on Red Hat OpenShift Container Platform 4.22.18 with a `MachineAutoscaler` of 1 to 2 on the GPU MachineSet. On resume
from a stopped state, EC2 refused to start the existing `g6.xlarge` for lack of capacity, so its
node stayed NotReady and the model pod went Pending. The autoscaler scaled the MachineSet from 1 to 2,
the new node took the pod, and once the original node returned the autoscaler scaled back to 1.
`policy-gpu-nodes-aws` stayed Compliant throughout and did not reset the replica count. The suite
renders the autoscaled path from a fixture but does not assert on its output.

## Policies

| Policy | Mode | Asserts |
|--------|------|---------|
| `policy-gpu-nodes-aws` | enforce | The MachineSets exist with the requested shape |
| `policy-gpu-nodes-test` | inform | MachineSets reached their replica count; every GPU node is Ready **and** lists `nvidia.com/gpu` in `status.allocatable` |

The capacity check is the one worth having. A GPU node whose driver stack never finished looks
completely healthy (Ready, schedulable) while reporting no GPU, so every GPU workload sits Pending
with nothing obviously wrong. The policy fails and names the node.

`policy-gpu-nodes-test` is placed only on clusters whose `autoshift.io/gpu-nodes` is set and not `0`,
so a parked MachineSet is not checked at all. On a placed cluster, every case where there is
nothing to check fails with a message rather than reporting Compliant: the label missing, no zone to
check, or no GPU node yet.

## Extension points not implemented

- Root disk size. Inherited from the worker; large models pulled as OCI modelcars may want more.
- vSphere and bare metal providers.
