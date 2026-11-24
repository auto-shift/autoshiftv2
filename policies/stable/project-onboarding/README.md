# project-onboarding

Creates project Namespaces from `config.projectOnboarding.projects`, with their labels, RoleBindings
to existing ClusterRoles, and optional membership of a primary user-defined network. It installs no
operator. Placement reads the `autoshift.io/project-onboarding` label.

| Policy | What it does |
|---|---|
| `policy-project-onboarding` | Creates each Namespace with its labels and annotations, and one RoleBinding per `roleBindings` entry |
| `policy-project-onboarding-ready` | Inform only. Checks that each Namespace carries its labels and that each RoleBinding binds the configured ClusterRole and subjects |

## Configuration

Projects are structured data, so they live under `config.projectOnboarding` and reach the managed
cluster through the `rendered-config` ConfigMap. Each key under `projects` is a Namespace name.

| Field | Type | Required | Notes |
|---|---|---|---|
| `projects.<name>` | map | yes | The Namespace name, a DNS-1123 label. `default`, `openshift`, `kube-*`, `openshift-*` and `open-cluster-management*` are refused |
| `displayName` | string | no | Written to the `openshift.io/display-name` annotation |
| `description` | string | no | Written to the `openshift.io/description` annotation |
| `labels` | map of strings | no | Namespace labels. Quote values such as `'true'`. Keys under `autoshift.io`, `k8s.ovn.org`, `kubernetes.io`, `k8s.io` and `openshift.io` are reserved |
| `roleBindings.<binding>` | map | no | Becomes the RoleBinding `autoshift-<binding>`. The key is a DNS-1123 label |
| `roleBindings.<binding>.clusterRole` | string | yes | An existing ClusterRole, such as `admin`, `edit` or `view`. No Role is created |
| `roleBindings.<binding>.groups` | list | one of | Group names. A `system:` name is refused |
| `roleBindings.<binding>.users` | list | one of | User names. A `system:` name is refused |
| `udn` | string | no | Name of the primary `ClusterUserDefinedNetwork` this Namespace joins. See [User-defined networks](#user-defined-networks) |

```yaml
hubClusterSets:
  hub:
    labels:
      project-onboarding: 'true'
    config:
      projectOnboarding:
        projects:
          payments-api:
            displayName: 'Payments API'
            description: 'Payment processing services'
            labels:
              example.com/team: 'payments'
            roleBindings:
              admins:
                clusterRole: admin
                groups:
                  - payments-admins
              developers:
                clusterRole: edit
                groups:
                  - payments-devs
                users:
                  - alice@example.com
            udn: payments-net
```

### Why maps and lists

Projects and bindings are maps keyed by name, so a cluster can add a project, a binding, or a label
to what its clusterset declares without restating the rest. The rendered-config merge is a deep
merge of maps.

Groups and users are lists, because the merge replaces a non-empty list whole. A cluster that
restates `groups` for a binding gets exactly its own list, which is what revoking access needs. A
map of `name: true` would look more merge friendly, but the merge cannot override an inherited
`true` with `false`, so a group could never be removed at a lower level.

The merge treats an empty value as unset and fills it from the layer below. An empty `groups: []`,
`clusterRole: ''` or label value therefore cannot clear what a clusterset or the global config sets,
and the validation rejects one that tries. Layers add to each other: to remove an inherited entry,
remove it from the layer that declares it.

## Validation

The configuration is validated when the `autoshift` chart renders, by
`autoshift/templates/_validate-project-onboarding.tpl`. An invalid entry fails `helm template` and
the Argo CD sync, with a message naming the clusterset or cluster and the full path:

```text
Project onboarding validation failed (1 errors):
  - hubClusterSets.hub: config.projectOnboarding.projects.payments-api.roleBindings.admins.clusterRole is required: the name of an existing ClusterRole, such as admin, edit or view
```

The check runs against the merged view each cluster receives: the cluster's own config over its
clusterset's over the global config. A `clusters` entry without `config.clusterSet` cannot be merged
with a clusterset at render time, so for it only formats and types are checked, not required fields.

The check also fails a clusterset or cluster that sets `project-onboarding: 'true'` with no
projects. At runtime, a placed policy whose rendered config has no projects reports NonCompliant
rather than Compliant with nothing applied.

## What it manages

**Namespaces use `musthave`.** The policy owns the labels and annotations it sets, not the whole
Namespace. OpenShift, Pod Security admission label synchronization, OpenShift GitOps and other tools
write labels and annotations of their own, and `mustonlyhave` would remove them. As a result, a label
removed from config stays on the Namespace. Every Namespace also carries
`autoshift.io/managed-by: project-onboarding`.

**RoleBindings use `mustonlyhave`.** The subjects are exactly the configured groups and users.
`musthave` merges lists, so a group removed from config would keep its access while the policy
reported Compliant. See [compliance types](../../../docs/policy-behavior.md#compliance-types).
`roleRef` is immutable, so the binding sets `recreateOption: IfRequired` and is replaced when its
`clusterRole` changes. RoleBindings carry `autoshift.io/managed-by: project-onboarding`.

**Nothing is deleted.** `pruneObjectBehavior` is `None`. Removing a project or a binding from config,
or removing the policy from a cluster, leaves the Namespace and RoleBindings in place. Deleting a
Namespace deletes its workloads, so it stays a deliberate act.

An existing Namespace with the same name is adopted: the policy adds its labels, annotations and
RoleBindings to it.

## User-defined networks

`udn: <name>` assigns the Namespace to a network by label. The policy sets two labels:

| Label | Purpose |
|---|---|
| `autoshift.io/udn: <name>` | What the `ClusterUserDefinedNetwork` selects |
| `k8s.ovn.org/primary-user-defined-network: ''` | Required by OVN-Kubernetes on every Namespace that uses a primary user-defined network |

This policy does not create the network. Create the `ClusterUserDefinedNetwork` yourself or with
another policy, selecting on `autoshift.io/udn`:

```yaml
apiVersion: k8s.ovn.org/v1
kind: ClusterUserDefinedNetwork
metadata:
  name: payments-net
spec:
  namespaceSelector:
    matchLabels:
      autoshift.io/udn: payments-net
  network:
    topology: Layer2
    layer2:
      role: Primary
      subnets:
        - 10.100.0.0/16
```

`udn` is for a **primary** network, and the distinction matters:

- A primary network replaces the cluster default network for the pods in the Namespace, and each
  Namespace has at most one, which is why `udn` is a single name.
- OVN-Kubernetes honors `k8s.ovn.org/primary-user-defined-network` only when the Namespace is
  created with it, and the label cannot be added later. When this policy creates the Namespace, the
  label is present at creation. Setting `udn` on a Namespace that already exists does not move it
  onto the network, and the Namespace has to be re-created.
- Create the `ClusterUserDefinedNetwork` before or with the Namespace. Pods in a Namespace that
  carries the primary label do not start until their primary network exists.

A **secondary** network adds an extra interface and needs no creation-time label, and a Namespace
can join several. Select those through the ordinary `labels` field, for example
`example.com/storage-net: 'true'`, in the secondary network's `namespaceSelector`.

## What version 1 does not do

This version is deliberately small: Namespaces, labels, RoleBindings, and network membership by
label. It does not:

- delete or prune Namespaces, RoleBindings or labels removed from config
- create Roles, ServiceAccounts, or the `ClusterUserDefinedNetwork` itself
- set ResourceQuotas or LimitRanges
- create NetworkPolicies, AdminNetworkPolicies, EgressFirewalls or EgressIPs

Each is a candidate for a follow-up that builds on this one.

## Placement

`placement.yaml` selects every cluster, hub or managed, labeled
`autoshift.io/project-onboarding: 'true'`. Both policies share it. Set the label in the values files,
never directly on a cluster.

## Layout

```
project-onboarding/
  kustomization.yaml               # entry point: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml     # the PolicyGenerator: policy graph, remediation, evaluation interval
  placement.yaml                   # Placement predicate and tolerations
  manifests/
    project-onboarding.yaml        # Namespaces and RoleBindings
  test/
    project-onboarding-ready.yaml  # inform: the Namespaces and RoleBindings exist
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/project-onboarding

# Full validation: render, hub and spoke template resolution, label contract, chart validation
make test
```

## Verify

```bash
oc get namespaces -l autoshift.io/managed-by=project-onboarding --show-labels
oc get rolebindings -A -l autoshift.io/managed-by=project-onboarding
```
