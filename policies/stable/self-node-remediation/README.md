# Self Node Remediation policy

Installs the Self Node Remediation Operator from Workload Availability for Red Hat OpenShift. It
reboots an unhealthy node through a software or watchdog reboot, with no out-of-band management
access, and is the default provider for `node-health-check`.

## Policies

| Policy | Does |
|---|---|
| `policy-snr-operator-install` | Namespace `openshift-workload-availability` and the operator subscription |

The operator creates its own defaults when it starts: the `SelfNodeRemediationConfig` named
`self-node-remediation-config` and the template
`self-node-remediation-automatic-strategy-template`, which `node-health-check` references.

## Enable

```yaml
labels:
  self-node-remediation: 'true'
```

The Node Health Check Operator no longer installs this operator for you. Enable it on every cluster
where a check uses `self-node-remediation`.
