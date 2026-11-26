# Vertical Pod Autoscaler

Installs the Red Hat Vertical Pod Autoscaler Operator and, optionally, VerticalPodAutoscaler objects
from configuration.

| Policy | What it does |
|---|---|
| `policy-vpa-operator-install` | `openshift-vertical-pod-autoscaler` namespace, an OwnNamespace OperatorGroup (the CSV supports no other mode) and the `vertical-pod-autoscaler` subscription |
| `policy-vpa-ready-test` | Inform: the operator's admission plugin, recommender and updater Deployments are Available |
| `policy-vpa-rules` | One VerticalPodAutoscaler per entry in `config.vertical-pod-autoscaler.rules` |

Enable the operator with `vertical-pod-autoscaler: 'true'`. Rules are placed only where
`vertical-pod-autoscaler-rules: 'true'` is also set, and the policy fails if that label is set with
no rules. Each rule is keyed by the VPA's name and takes `namespace`, `targetRef` (apiVersion, kind,
name), `updateMode` (default `Initial`), an optional `resourcePolicy` passed through to the VPA, and
`enabled` (false deletes the VPA). See the example under `config` in
`autoshift/values/clustersets/_example.yaml`.

`Initial` sizes pods only when they are created, so nothing is evicted. A VPA changes the pod, not
the workload's template, which also makes it a way to size pods whose Deployment an operator owns.
