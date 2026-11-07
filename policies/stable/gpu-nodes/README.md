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

## What the nodes get

- Label `node-role.kubernetes.io/gpu: ''` plus `node-role.kubernetes.io/worker: ''`
- Taint `nvidia.com/gpu:NoSchedule`, valueless, so an `operator: Exists` toleration matches

The taint means only workloads that ask for a GPU land on an expensive instance. The GPU operator's
own operands tolerate it, so the driver stack still installs.

## Two things that are easy to get wrong

**Subnets are inherited, not reconstructed.** The sibling node policies build an AWS subnet filter
from the infrastructure ID (`<infra-id>-subnet-private-<zone>`). That only holds for a VPC the
installer created. A cluster installed into a pre-existing VPC via `platform.aws.vpc.subnets`
references its subnet by raw ID, and the VPC owner chose the `Name` tags — so a reconstructed
filter matches nothing and every machine fails to provision with no subnet found. This policy
copies `subnet`, `securityGroups` and `blockDevices` verbatim from the base worker, which is
correct either way. When several zones are requested, each GPU MachineSet clones the worker that
already lives in that zone.

**Spot has no fallback.** `autoshift.io/gpu-nodes-spot: 'true'` sets `spotMarketOptions: {}`. If
spot capacity is unavailable — or denied by a service control policy, which looks identical from
inside the cluster — the machines never appear. There is no declarative way to fall back to
on-demand, so the failure surfaces as `policy-gpu-nodes-test` going NonCompliant rather than as a
quiet downgrade. That is the intended behaviour; a silent switch to on-demand would be a surprise
on the bill.

## Policies

| Policy | Mode | Asserts |
|--------|------|---------|
| `policy-gpu-nodes-aws` | enforce | The MachineSets exist with the requested shape |
| `policy-gpu-nodes-test` | inform | MachineSets reached their replica count; every GPU node is Ready **and** reports `nvidia.com/gpu` capacity |

The capacity assertion is the one worth having. A GPU node whose driver stack never finished looks
completely healthy — Ready, schedulable — while reporting no GPU, so every GPU workload sits
Pending with nothing obviously wrong. Both test assertions are skipped when the replica count is
`0`, so a parked MachineSet does not report NonCompliant.

## Extension points not implemented

- Root disk size. Inherited from the worker; large models pulled as OCI modelcars may want more.
- `cluster-api/accelerator` node label, used by the cluster autoscaler to understand GPU capacity.
- vSphere and bare metal providers.
