# leader-worker-set

Installs the Leader Worker Set (LWS) Operator and deploys its controller.
LeaderWorkerSet runs a group of pods as one unit, a leader with its workers, which is how
distributed inference spreads one model across several pods. Red Hat OpenShift AI needs it for
distributed inference with llm-d, which Models-as-a-Service uses.

## Policies

| Policy | Mode | What it does |
|---|---|---|
| `policy-lws-operator-install` | enforce | The `openshift-lws-operator` namespace, an OwnNamespace OperatorGroup and the `leader-worker-set` subscription |
| `policy-lws-instance` | enforce | The `LeaderWorkerSetOperator` named `cluster`, which tells the Operator to deploy the controller. Waits for the Operator |
| `policy-lws-ready` | inform | The `openshift-lws-operator` Deployment is `Available` |

`lws` abbreviates the policy names to stay within the 40-character limit.

## Enabling

```yaml
hubClusterSets:
  hub:
    labels:
      leader-worker-set: 'true'
      leader-worker-set-subscription-name: leader-worker-set
      leader-worker-set-channel: stable-v1.0
      leader-worker-set-source: redhat-operators
      leader-worker-set-source-namespace: openshift-marketplace
      # leader-worker-set-version: 'leader-worker-set.v1.0.1'   # optional CSV pin
```

Set labels in values files only. The `cluster-labels` policy stamps them onto each cluster. A
pinned version sets `startingCSV` and `versions`, so upgrades become explicit:

```bash
oc get packagemanifests leader-worker-set -o jsonpath='{.status.channels[*].currentCSV}'
```

## Installing the Operator is not enough

The subscription brings up the Operator, which does nothing until a `LeaderWorkerSetOperator` custom
resource tells it to deploy the controller. Without it the CSV is `Succeeded`, the install policy is
Compliant, and no LeaderWorkerSet workload can ever be scheduled. `policy-lws-instance` creates the
resource with the singleton name `cluster` and `managementState: Managed`, as the CSV's own example
does.

`policy-lws-ready` asserts the Operator Deployment, which is named after its namespace,
`openshift-lws-operator`. It depends on `policy-lws-instance` rather than on the install, so a
missing controller is reported against the custom resource rather than the subscription.

## Configuration

The custom resource's spec is read from `config.leader-worker-set.operator` in the cluster's
rendered-config, merged over a base of `managementState: Managed` and both log levels `Normal`. Any
spec field is accepted; maps merge key by key, the configured value wins, and a field removed from
config returns to its default.

```yaml
hubClusterSets:
  hub:
    config:
      leader-worker-set:
        operator:
          logLevel: Debug            # the controller: Normal, Debug, Trace, TraceAll
          operatorLogLevel: Normal   # the Operator itself
```

## Install mode

`leader-worker-set` v1.0.1 declares `OwnNamespace=True` and `AllNamespaces=False`. The shared
operator-install chart defaults to an AllNamespaces OperatorGroup, which OLM refuses: the CSV sits in
`Failed` with `UnsupportedOperatorGroup` while the policy reports only that the CSV was not found.
This policy sets `targetNamespaces: [openshift-lws-operator]`.

## Verifying

```bash
oc get csv -n openshift-lws-operator
oc get leaderworkersetoperator cluster
oc get deployment openshift-lws-operator -n openshift-lws-operator
oc get crd leaderworkersets.leaderworkerset.x-k8s.io
```
