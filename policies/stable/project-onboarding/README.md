# project-onboarding

Creates project Namespaces from `config.projectOnboarding.projects`, with their labels and
RoleBindings to existing ClusterRoles or Roles, and removes a Namespace or RoleBinding marked
`state: absent`. It installs no operator. Placement reads the `autoshift.io/project-onboarding`
label.

| Policy | What it does |
|---|---|
| `policy-project-onboarding` | Creates each Namespace with its labels and annotations, and one RoleBinding per `roleBindings` entry. Deletes the entries marked `state: absent` |
| `policy-project-onboarding-ready` | Inform only. Checks that each Namespace carries its labels, that each RoleBinding binds the configured role and subjects, and that each `state: absent` object is gone |

## Configuration

Projects are structured data, so they live under `config.projectOnboarding` and reach the managed
cluster through the `rendered-config` ConfigMap. Each key under `projects` is a Namespace name.

| Field | Type | Required | Notes |
|---|---|---|---|
| `projects.<name>` | map | yes | The Namespace name, a DNS-1123 label. `default`, `openshift`, `kube-*`, `openshift-*` and `open-cluster-management*` are refused |
| `displayName` | string | no | Written to the `openshift.io/display-name` annotation |
| `description` | string | no | Written to the `openshift.io/description` annotation |
| `labels` | map of strings | no | Namespace labels. Quote values such as `'true'`. Keys under `autoshift.io`, `k8s.ovn.org`, `kubernetes.io`, `k8s.io` and `openshift.io` are reserved. A label is also how a Namespace joins a network: see [Joining networks](#joining-networks) |
| `state` | string | no | `present`, the default, or `absent`. See [Removing projects and bindings](#removing-projects-and-bindings) |
| `roleBindings.<binding>` | map | no | Becomes the RoleBinding `autoshift-<binding>`. The key is a DNS-1123 label |
| `roleBindings.<binding>.clusterRole` | string | one of | An existing ClusterRole, such as `admin`, `edit` or `view` |
| `roleBindings.<binding>.role` | string | one of | An existing Role in the project Namespace. Set exactly one of `clusterRole` and `role`. No Role is created |
| `roleBindings.<binding>.groups` | list | one of | Group names. A `system:` name is refused |
| `roleBindings.<binding>.users` | list | one of | User names. A `system:` name is refused |
| `roleBindings.<binding>.serviceAccounts` | list | one of | Entries of `name`, a DNS-1123 subdomain, and an optional `namespace`, a DNS-1123 label that defaults to the project |
| `roleBindings.<binding>.state` | string | no | `present`, the default, or `absent`. An `absent` binding needs no role and no subjects |

Each binding that is not `absent` needs at least one subject across `groups`, `users` and
`serviceAccounts`.

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
              example.com/vm-net: 'true'
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
              deployer:
                role: payments-deployer
                serviceAccounts:
                  - name: pipeline
                  - name: argocd-application-controller
                    namespace: gitops-example
              contractors:
                state: absent
```

A ServiceAccount subject is rendered as `kind: ServiceAccount` with no `apiGroup`, as Kubernetes
expects. `role` binds a Role that already exists in the project; this policy does not create it.

### Why maps and lists

Projects and bindings are maps keyed by name, so a cluster can add a project, a binding, or a label
to what its clusterset declares without restating the rest. The rendered-config merge is a deep
merge of maps.

Groups, users and ServiceAccounts are lists, because the merge replaces a non-empty list whole. A
cluster that restates `groups` for a binding gets exactly its own list, which is what revoking
access needs. A map of `name: true` would look more merge friendly, but the merge cannot override an
inherited `true` with `false`, so a group could never be removed at a lower level.

The merge treats an empty value as unset and fills it from the layer below. An empty `groups: []`,
`clusterRole: ''` or label value therefore cannot clear what a clusterset or the global config sets,
and the validation rejects one that tries. Layers add to each other: to remove an inherited entry
from config, remove it from the layer that declares it. `state: absent` is a non-empty string, so it
does override an inherited `present`, and a cluster can remove a project or binding its clusterset
declares.

## Validation

The configuration is validated when the `autoshift` chart renders, by
`autoshift/templates/_validate-project-onboarding.tpl`. An invalid entry fails `helm template` and
the Argo CD sync, with a message naming the clusterset or cluster and the full path:

```text
Project onboarding validation failed (1 errors):
  - hubClusterSets.hub: config.projectOnboarding.projects.payments-api.roleBindings.admins needs clusterRole, the name of an existing ClusterRole such as admin, edit or view, or role, the name of an existing Role in the project
```

The check runs against the merged view each cluster receives: the cluster's own config over its
clusterset's over the global config. It covers names, labels, reserved namespaces and label
prefixes, `state` values, role names, subjects, wrong types and unknown fields. Role names follow
the Kubernetes rule for RBAC names, with no length limit; `{`, `}` and whitespace are also refused,
because the name is rendered into a hub template.

A `clusters` entry without `config.clusterSet` cannot be merged with a clusterset at render time,
because its clusterset is only known at runtime. For it, required fields are not checked, and any
empty value the cluster sets is refused, including empty `groups`, `users`, `serviceAccounts`,
`clusterRole`, `role`, `displayName`, `description`, `state` and label values. The merge could fill
such a value from the unknown clusterset. Remove the key, or set `config.clusterSet`.

The check also fails a clusterset or cluster that sets `project-onboarding: 'true'` with no
projects. At runtime, a placed policy whose rendered config has no projects reports NonCompliant
rather than Compliant with nothing applied.

## What it manages

**Namespaces use `musthave`.** The policy owns the labels and annotations it sets, not the whole
Namespace. OpenShift, Pod Security admission label synchronization, OpenShift GitOps and other tools
write labels and annotations of their own, and `mustonlyhave` would remove them. As a result, a label
removed from config stays on the Namespace. Every Namespace also carries
`autoshift.io/managed-by: project-onboarding`.

An existing Namespace with the same name is adopted: the policy adds its labels, annotations and
RoleBindings to it. A Namespace the policy creates also gets the annotation
`autoshift.io/project-onboarding-created: 'true'`, and an adopted one never does. See
[The deletion safeguard](#the-deletion-safeguard).

**RoleBindings use `mustonlyhave`.** The subjects are exactly the configured groups, users and
ServiceAccounts. `musthave` merges lists, so a group removed from config would keep its access while
the policy reported Compliant. See [compliance types](../../../docs/policy-behavior.md#compliance-types).
`roleRef` is immutable, so the binding sets `recreateOption: IfRequired`: when an update fails on an
immutable field, the controller deletes and re-creates the binding. Changing `clusterRole`, or
switching a binding from `clusterRole` to `role`, therefore replaces the binding. RoleBindings carry
`autoshift.io/managed-by: project-onboarding`.

**Removing an entry from config deletes nothing.** `pruneObjectBehavior` is `None`. Removing a
project or a binding from config, or removing the policy from a cluster, leaves the Namespace and
RoleBindings in place. To remove one, mark it `state: absent`.

## Removing projects and bindings

> [!WARNING]
> `state: absent` on a project **deletes the Namespace and everything in it**: virtual machines,
> PersistentVolumeClaims and the data on them, and every other workload, on every cluster that
> config applies to. Set it on a clusterset and it applies to every cluster in that clusterset.
> Under `dryRun` or inform it only reports. A Namespace this policy did not create is never
> deleted.

**A binding marked `absent`** renders `mustnothave` for the RoleBinding `autoshift-<binding>` in the
project Namespace. It needs no `clusterRole`, `role` or subjects.

**A project marked `absent`** renders nothing else: no labels and no RoleBindings. The policy
deletes the Namespace only when it carries `autoshift.io/project-onboarding-created: 'true'`, and
deleting the Namespace removes its RoleBindings with it. When the annotation is missing, the
enforcing policy renders nothing for that project, and `policy-project-onboarding-ready` reports
NonCompliant with a message saying that the Namespace was not created by project-onboarding and
will not be deleted.

### The deletion safeguard

The `autoshift.io/managed-by` label is not the safeguard, because `musthave` adds it to an adopted
Namespace too, so carrying it does not prove that this policy created the Namespace. The creation
annotation does:

1. On each evaluation, a managed-cluster `lookup` checks whether the Namespace exists.
2. When it does not, the object definition includes the annotation, and `musthave` under enforce
   creates the Namespace with it.
3. Once the Namespace exists, the object definition no longer names the annotation. `musthave`
   leaves fields it is not given alone, so the annotation persists.
4. An adopted Namespace already exists at the first evaluation, so its object definition never
   includes the annotation.

This follows from the `musthave` semantics in
[compliance types](../../../docs/policy-behavior.md#compliance-types). Two limits follow from it as
well. An annotation set by hand counts, because adoption keeps metadata the policy is not given. And
a Namespace that another tool creates in the moment between the lookup and the create receives the
annotation. Removing the annotation by hand protects a Namespace from deletion.

### How readiness reports removal

The readiness policy contains no `mustnothave`. Pressing Enforce on an inform policy in the console
makes it enforce, and a `mustnothave` there would then delete the object. Instead, it looks up each
`absent` object on the managed cluster. While the object still exists, the policy asserts a
ConfigMap that does not exist, named for the problem, for example
`project-onboarding-absent-namespace-<name>-still-exists`, with the explanation in its data. The
policy reports NonCompliant with that name in the message. Once the object is gone, the entry
contributes nothing.

When every project is `absent` and gone, the readiness policy has nothing left to check and fails
with a message saying so, rather than reporting Compliant with no check run. Remove those projects
from config, or set `project-onboarding` to `'false'`.

## Joining networks

A Namespace joins a network through an ordinary label in `labels`. A `ClusterUserDefinedNetwork`
selects Namespaces with its `namespaceSelector`, so labeling the project is enough. This policy does
not create the network.

The common case for virtual machines is a secondary localnet network on a bridge. The
`physicalNetworkName` must match an OVN bridge mapping on the nodes, which a
NodeNetworkConfigurationPolicy sets up. This network selects every Namespace labeled
`example.com/vm-net: 'true'`, such as `payments-api` in the example above:

```yaml
apiVersion: k8s.ovn.org/v1
kind: ClusterUserDefinedNetwork
metadata:
  name: vm-net
spec:
  namespaceSelector:
    matchLabels:
      example.com/vm-net: 'true'
  network:
    topology: Localnet
    localnet:
      role: Secondary
      physicalNetworkName: physnet-vm
      ipam:
        mode: Disabled
```

A secondary network adds an interface, needs no creation-time label, and a Namespace can join
several.

Primary user-defined networks are not supported. OVN-Kubernetes attaches a primary network only to a
Namespace that carries `k8s.ovn.org/primary-user-defined-network` when it is created, and cannot
attach one to an existing Namespace. That label is in a reserved prefix, so `labels` refuses it.

## What version 1 does not do

This version is deliberately small: Namespaces, labels, RoleBindings, and their removal with
`state: absent`. It does not:

- delete or prune Namespaces, RoleBindings or labels removed from config; only `state: absent`
  removes anything
- remove a label from a Namespace
- create Roles, ServiceAccounts, or a `ClusterUserDefinedNetwork`
- attach a Namespace to a primary user-defined network
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
    project-onboarding.yaml        # Namespaces and RoleBindings, and their removal
  test/
    project-onboarding-ready.yaml  # inform: the Namespaces and RoleBindings exist, absent ones are gone
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/project-onboarding

# Full validation: render, hub and spoke template resolution, label contract, chart validation
make test
```

`tools/testdata/project-onboarding.yaml` seeds the live objects the managed-cluster lookups read, so
the suite resolves the create, adopt, delete and still-exists branches.

## Verify

```bash
oc get namespaces -l autoshift.io/managed-by=project-onboarding --show-labels
oc get namespaces -o custom-columns='NAME:.metadata.name,CREATED-BY-POLICY:.metadata.annotations.autoshift\.io/project-onboarding-created'
oc get rolebindings -A -l autoshift.io/managed-by=project-onboarding
```
