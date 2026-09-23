# Fence Agents Remediation policy

Installs the Fence Agents Remediation Operator from Workload Availability for Red Hat OpenShift and
builds the `FenceAgentsRemediationTemplate` that power-cycles an unhealthy node through its BMC.
`node-health-check` uses it when a check sets `remediation: fence-agents-remediation`.

## Policies

| Policy | Does |
|---|---|
| `policy-far-operator-install` | Namespace `openshift-workload-availability` and the operator subscription |
| `policy-far-credentials` | Copies the BMC credentials from a hub Secret. Not placed when `fence-agents-remediation-credentials: existing` |
| `policy-far-template` | `FenceAgentsRemediationTemplate` `fenceagentsremediationtemplate-default` |
| `policy-far-test` | Inform only. Reports a missing credentials Secret, or a node whose BMC failed the status check |

## Enable

```yaml
labels:
  fence-agents-remediation: 'true'
  fence-agents-remediation-credentials: 'hub'   # or existing
```

## Node parameters

Per-node parameters come from `config.hosts`, the same data the bare-metal cluster installation
uses:

| Host field | Becomes |
|---|---|
| `bmcIP` | `--ip` |
| `bmcEndpoint`, else `config.clusterInstall.bmcEndpoint` | `--systems-uri`, for `fence_redfish` only |
| `hostname`, else `<key>.<cluster base domain>` | the node name each value is keyed by |

Setting `--ip` or `--systems-uri` under `config.fenceAgentsRemediation.nodeParameters` replaces the
derived values. Shared parameters default to `--action: reboot`. The full schema is in
`autoshift/values/clustersets/_example.yaml`.

## Credentials

The operator reads fence-agent flags from the Secret keys, so the Secret holds `--username` and
`--password`.

- **`hub`** (default): the policy reads `username` and `password` from the hub Secret named by
  `config.fenceAgentsRemediation.credentials.sourceName`, falling back to
  `config.clusterInstall.bmcCredentialRef` and `secretSourceNamespace`. Hosts with their own
  `bmcCredentialRef` get a per-node Secret. The copy is a hub template, so it runs as
  `autoshift-policy-service-account` and ACM encrypts the values in transit.
- **`existing`**: create the Secret yourself in `openshift-workload-availability`, for example with an
  `ExternalSecret`, under the name in `credentials.secretName`. Map per-node Secrets with
  `credentials.nodeSecretNames`.

Never put credentials in values files.

## Validation

The template sets `statusValidationSample: '100%'` by default. The operator then runs the fence
agent's read-only `status` action against every node's BMC and records the result in the template
status. `policy-far-test` stays NonCompliant until every node passes, so bad BMC addresses or
credentials surface before a node fails, not during an outage. Read
`status.validationFailed` on the template for the per-node error.

## Constraints

- Write durations the way Kubernetes prints them (`1m0s`, not `60s`), or the policy sees a
  difference on every evaluation.
- Agents that use SSH or Telnet are not supported. `fence_redfish` and `fence_ipmilan` cover most
  bare metal; the Red Hat documentation lists the rest.
