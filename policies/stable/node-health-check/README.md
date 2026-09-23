# Node Health Check policy

Installs the Node Health Check Operator from Workload Availability for Red Hat OpenShift and creates
the `NodeHealthCheck` resources that watch node conditions and hand unhealthy nodes to a remediation
provider.

## Policies

| Policy | Does |
|---|---|
| `policy-nhc-operator-install` | Namespace `openshift-workload-availability` and the operator subscription |
| `policy-nhc-checks` | One `NodeHealthCheck` per entry in `config.nodeHealthCheck.checks`, and deletes any AutoShift-created check that is no longer declared |
| `policy-nhc-test` | Inform only. Reports a missing check, or a custom `MachineHealthCheck` that has disabled the operator |

## Enable

```yaml
labels:
  node-health-check: 'true'
  node-health-check-remediation: 'self-node-remediation'   # or fence-agents-remediation
  self-node-remediation: 'true'                            # the provider must be installed too
  machine-health-checks: 'false'                           # required, see below
```

Without `config.nodeHealthCheck.checks`, the policy creates two checks: `nhc-workers` for worker
nodes that are not control plane nodes, and `nhc-control-plane`. Both use `minHealthy: 51%` and treat
a node as unhealthy after `Ready` has been `False` or `Unknown` for 300 seconds. The full schema,
including `maxUnhealthy`, `escalatingRemediations`, `healthyDelay` and `pauseRequests`, is in
`autoshift/values/clustersets/_example.yaml`.

`remediation` accepts `self-node-remediation` or `fence-agents-remediation` and expands to that
provider's template. For another provider, set `remediationTemplate` to a full object reference.

## Machine health checks

The operator disables itself while any `MachineHealthCheck` other than the default
`machine-api-termination-handler` exists. Do not enable `machine-health-checks` on the same cluster.
`policy-nhc-test` reports the conflict by name.

## Constraints

- Keep control plane and worker nodes in separate checks, and never let two selectors match one node.
- The operator remediates at most one control plane node at a time and skips a remediation that
  would break etcd quorum.
- A check without a `selector` is rejected by the API rather than matching every node.
- Custom node conditions, such as those from a node problem detector, can be listed in
  `unhealthyConditions`.
