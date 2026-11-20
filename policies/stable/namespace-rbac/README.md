# namespace-rbac

Creates ServiceAccounts, Roles, and RoleBindings in namespaces that already exist, from the
AutoShift values files. It is meant for workload identities, such as a CI pipeline that starts
builds in one namespace, that need a few exact permissions there and nothing else.

For each namespace in `config.namespaceRbac` the policy renders:

- a ServiceAccount for each name set to `true`;
- a Role for each entry in `roles`, with exactly the declared rules;
- a RoleBinding for each entry in `roleBindings`, binding one of those Roles, or the built-in
  ClusterRole `edit` or `view`, to ServiceAccounts.

Every object carries the label `autoshift.io/managed-by: namespace-rbac`. The policy never creates
a namespace, a ClusterRole, or a ClusterRoleBinding. ServiceAccount tokens come from the
TokenRequest API. The one exception is a long-lived token Secret for a minter-only ServiceAccount,
which must be requested explicitly and passes a strict guard. See
[Token Secret exception](#token-secret-exception).

The policy grants Kubernetes permissions, so every rule passes a [safety gate](#safety-gate). A
refused entry is skipped and reported by name in a status ConfigMap, and every other entry still
applies. See [Status](#status).

## Policies

| Policy | Remediation | What it does |
|---|---|---|
| `policy-namespace-rbac` | `${REMEDIATION}`: enforce, or inform under `autoshift.dryRun` | Creates and corrects the declared ServiceAccounts, Roles, and RoleBindings from `manifests/namespace-rbac.yaml`, and writes the `namespace-rbac-status` ConfigMap while there are errors |
| `policy-namespace-rbac-test` | Always inform | From `test/namespace-rbac.yaml`: reports objects that are no longer declared and asserts that the status ConfigMap is absent. It never changes anything |

Both files are the same template and differ only in the `$want` line, which a test enforces. The
inform-only checks are a policy of their own, as the [developer
guide](../../../docs/developer-guide.md#gating-on-cluster-state) requires: a `remediationAction` on
the root of one Policy overrides all of its children, so a check bundled into the enforcing Policy
would start deleting what it should only report.

`policy-namespace-rbac-test` is Compliant only when every declared entry is applied and nothing
stale is left, so depend on it, not on `policy-namespace-rbac`, when another policy needs the
ServiceAccounts and RoleBindings to be in place. `policy-namespace-rbac` reports Compliant once its
objects and the status ConfigMap are written, even when the status lists errors.

Neither Policy depends on the other:

- `policy-namespace-rbac` must not depend on `policy-namespace-rbac-test`. The test policy is
  NonCompliant exactly while the enforcing policy has work to do or errors to report, so the
  enforcing policy would never run.
- `policy-namespace-rbac-test` does not depend on `policy-namespace-rbac`. A dependency would hold
  the test policy Pending whenever the enforcing one is NonCompliant, which under dry run is
  whenever anything would change, and would hide its reports exactly when they are wanted.

## Enable

| Label | Default | Description |
|---|---|---|
| `autoshift.io/namespace-rbac` | `'false'` | `'true'` manages `config.namespaceRbac` |

Set it in a clusterset or cluster values file, without the `autoshift.io/` prefix. Never put it on
a ManagedCluster by hand.

## Configuration

Entries live at `config.namespaceRbac.<namespace>`, a map keyed by namespace. The normal precedence
applies: cluster over clusterset over global, as a deep merge. Every level is a map, so a cluster
file can set one ServiceAccount or one subject to `false` that a clusterset file declares.

```yaml
config:
  namespaceRbac:
    example-team:                         # must already exist
      serviceAccounts:
        example-ci: true                  # true creates and keeps it
        example-old-ci: false             # false stops declaring it
      roles:
        example-ci-builds:
          rules:
            - apiGroups: [build.openshift.io]
              resources: [buildconfigs]
              verbs: [get, create, patch]
            - apiGroups: ['']
              resources: [secrets]
              verbs: [get]
              resourceNames: [example-registry-token]
      roleBindings:
        example-ci-builds:
          role: example-ci-builds         # a Role declared in roles above
          serviceAccounts:                # <namespace>/<serviceaccount>: true or false
            example-team/example-ci: true
```

- `serviceAccounts`: a ServiceAccount name, a DNS-1123 subdomain, to `true`, `false`, or
  `{tokenSecret: <name>}`, which is `true` plus a long-lived token Secret. See
  [Token Secret exception](#token-secret-exception).
- `roles.<name>.rules`: a non-empty list of rules. A rule has `apiGroups`, `resources`, and `verbs`,
  each a non-empty list of strings, and an optional non-empty `resourceNames` list. `apiGroups` can
  contain `''`, the core group. `nonResourceURLs` is rejected, because a Role cannot use it.
- `roleBindings.<name>.role`: the name of a Role declared in `roles` of the same namespace. A Role
  declared elsewhere cannot be bound.
- `roleBindings.<name>.clusterRole`: `edit` or `view`, in place of `role`. Set exactly one of `role`
  and `clusterRole`. See [ClusterRole bindings](#clusterrole-bindings).
- `roleBindings.<name>.serviceAccounts`: `<namespace>/<serviceaccount>` to `true` or `false`. Each
  `true` ServiceAccount is a subject. The subject can live in another namespace, which is how one
  namespace grants another a narrow permission. Subjects are ServiceAccounts only, never users or
  groups.

The namespace name must be a DNS-1123 label. ServiceAccount, Role, and RoleBinding names must be
DNS-1123 subdomains.

| Key | Type | Default | Example | Invalid input |
|---|---|---|---|---|
| `<namespace>` | DNS-1123 label, an existing namespace that is not a platform namespace | required | `example-team` | Whole namespace skipped and reported in the [status](#status) |
| `serviceAccounts.<name>` | `true`, `false`, or `{tokenSecret: <name>}` | not declared | `example-ci: true` | That ServiceAccount skipped and reported |
| `roles.<name>.rules` | non-empty list of rules with `apiGroups`, `resources`, `verbs`, optional `resourceNames` | not declared | see above | That Role skipped and reported, see [Safety gate](#safety-gate) |
| `roleBindings.<name>.role` | the name of a Role in `roles` of the same namespace | one of `role` and `clusterRole` is required | `example-ci-builds` | That RoleBinding skipped and reported |
| `roleBindings.<name>.clusterRole` | `edit` or `view` | one of `role` and `clusterRole` is required | `view` | That RoleBinding skipped and reported, see [ClusterRole bindings](#clusterrole-bindings) |
| `roleBindings.<name>.serviceAccounts.<namespace>/<name>` | `true` or `false` | not a subject | `example-team/example-ci: true` | That RoleBinding skipped and reported |

An unknown key, or a section that is not a map, skips the whole namespace. Every other namespace
still applies.

Roles and RoleBindings are enforced with `mustonlyhave`, so the rules and the subjects are replaced,
not merged: a rule or a subject removed from the values is removed from the cluster. The `roleRef`
of a RoleBinding cannot change, so a RoleBinding whose `role` or `clusterRole` changes is deleted
and created again (`recreateOption: IfRequired`). ServiceAccounts are enforced with `musthave`, because OpenShift adds
image pull secrets to them that `mustonlyhave` would remove.

A RoleBinding binds its subjects to the Role whatever the owner of the ServiceAccount, so a subject
does not need a `serviceAccounts` entry in its namespace.

## ClusterRole bindings

A RoleBinding can grant the built-in ClusterRole `edit` or `view` in its namespace, for an identity
that manages ordinary workloads there and would otherwise need a long list of rules:

```yaml
config:
  namespaceRbac:
    example-apps:
      roleBindings:
        example-deployer:
          clusterRole: edit               # edit or view only
          serviceAccounts:
            example-ci/example-deployer: true
```

The RoleBinding is namespaced, so the grant stops at the namespace of the entry. The policy never
creates or changes a ClusterRole or a ClusterRoleBinding. The allowlist is fixed to `edit` and
`view`, the `$clusterRoles` variable in both templates, and every other value is refused:

- `admin` manages Roles and RoleBindings in the namespace, so a subject could grant itself, or
  anyone, any permission there, past the [safety gate](#safety-gate);
- `cluster-admin` grants every permission in the namespace, RBAC included;
- any other ClusterRole is defined outside the values files, by the cluster or an operator, and can
  change without a review of this policy.

The rules of `edit` and `view` are not checked by the safety gate, because the cluster defines them,
and operators aggregate their own rules into both. `edit` creates and changes workloads, reads and
writes every Secret, runs `pods/exec`, impersonates and requests tokens for every ServiceAccount of
its namespace, so it carries what any ServiceAccount of that namespace can do. Grant it only in a
namespace whose ServiceAccounts hold no wider rights. `view` reads most objects but not Secrets.

Every other guard applies unchanged: subjects are ServiceAccounts only, a platform namespace or the
namespace of a ManagedCluster is refused as the namespace of the entry or of a subject, the
`default` ServiceAccount is never a subject, a subject in a namespace that does not exist is left
out, and a RoleBinding of the same name without the managed-by label is held and reported.

The guards below keep `clusterRole` from widening the
[Token Secret exception](#token-secret-exception). The threat model is what an attacker holding a
long-lived token can act as, not which ServiceAccounts share its namespace. A ServiceAccount is
token-reachable when it has a `tokenSecret`, when a token-reachable identity can request tokens
for it (`serviceaccounts/token` `create` with its name in `resourceNames`), or when its namespace
is reachable. A namespace is reachable when a token-reachable identity can do any of these there:
create or change pods or workload controllers (the workload list of the safety gate, including
builds and build configurations); create, change, or read Secrets, since every ServiceAccount has a
pull Secret that carries a token; use `pods/exec`, `pods/attach`, or `pods/ephemeralcontainers`;
request tokens without `resourceNames`; `bind` or `escalate` RBAC objects; or impersonate
ServiceAccounts, users, or groups. Granted through a ClusterRoleBinding, any of these makes every
namespace reachable. On the hub, a ClusterRole other than `view` and a Role that cannot be inspected
count as all of these; on the managed cluster the live rules of each Role and ClusterRole are read,
aggregated rules included. The set is computed as a closure, repeated until a round adds nothing,
for at most 6 rounds; when it is still growing after that, every `clusterRole` binding is refused
(fail closed). Declared and live bindings are combined in every round on the managed cluster.

The group `system:authenticated` is not matched: OpenShift binds it by default to the build
strategy and webhook ClusterRoles, so matching it would make every namespace reachable on every
cluster, while the rights that start a build, `builds` and `buildconfigs` create, are matched. The
workload list is a deny list: a custom resource that runs pods, such as a Knative `services` or a
notebook, and writes to `imagestreamtags` that re-image a Deployment with an image trigger are not
counted. Review such grants in a namespace a token can reach.

- a token-reachable ServiceAccount is never a subject of an `edit` or `view` RoleBinding. On the
  hub the set is derived from `config.namespaceRbac`;
- on the managed cluster it is derived again from live objects: every live Secret of type
  `kubernetes.io/service-account-token` labeled `autoshift.io/managed-by: namespace-rbac`, and every
  live RoleBinding and ClusterRoleBinding with the rules of its Role or ClusterRole. A subject counts
  as `kind: ServiceAccount`, as `kind: User` named `system:serviceaccount:<namespace>:<name>`, and as
  the groups `system:serviceaccounts:<namespace>` and `system:serviceaccounts`. An `edit` or `view`
  RoleBinding with a token-reachable subject is held, and reported;
- `edit` is refused in a namespace with a ServiceAccount that has a `tokenSecret`, accepted or
  refused, because it reads the token Secret and creates workloads that mount it. `view` is allowed
  there;
- an `edit` RoleBinding is held, and reported, while its namespace holds such a live token Secret;
- a token Secret is not created or changed while a live labeled `edit` RoleBinding is in its
  namespace, or while any live RoleBinding or ClusterRoleBinding binds `edit` or `view` to a
  ServiceAccount, or a ServiceAccount group, that its ServiceAccount can act as.

The guards act before creation only. When reachability arises after an `edit` or `view` RoleBinding
or a token Secret is already live, for example through a binding created by hand, the object is
held, reported, and `policy-namespace-rbac-test` is NonCompliant, but nothing is removed: delete
the RoleBinding or the token Secret by hand. Only `edit` and `view` bindings are checked against
the reachable set; rights a reachable ServiceAccount gets from other bindings add to what the token
can do, as described under [Token Secret exception](#token-secret-exception).

A subject with `edit` can also create a Secret of type `kubernetes.io/service-account-token` for any
ServiceAccount of its namespace, and so give itself a long-lived token that this policy neither
creates nor revokes.

## Token Secret exception

A long-lived token is a credential that does not expire and is stored in a Secret, so anyone who
can read that Secret, or a copy of it, acts as the ServiceAccount until the Secret is deleted. The
policy refuses one in every case but this: an external system, such as a secrets engine that mints
short-lived tokens, needs a fixed identity to call the TokenRequest API, and that identity can do
nothing else.

```yaml
config:
  namespaceRbac:
    example-vault:
      serviceAccounts:
        example-minter:
          tokenSecret: example-minter-token
    example-team:
      serviceAccounts:
        example-ci: true
      roles:
        example-ci-minter:
          rules:
            - apiGroups: ['']
              resources: [serviceaccounts/token]
              verbs: [create]
              resourceNames: [example-ci]
            - apiGroups: ['']
              resources: [serviceaccounts]
              verbs: [get]
              resourceNames: [example-ci]
      roleBindings:
        example-ci-minter:
          role: example-ci-minter
          serviceAccounts:
            example-vault/example-minter: true
```

The policy renders the Secret `<name>` in the namespace of the ServiceAccount, of type
`kubernetes.io/service-account-token`, with the annotation `kubernetes.io/service-account.name` and
the label `autoshift.io/managed-by: namespace-rbac`, and enforces it with `musthave`. It never
renders a token or any other data, and never reads the data of a Secret: the cluster fills in the
token. The token Secret is refused, and reported, unless:

- the ServiceAccount is declared in the same entry, in a namespace that is not a platform namespace;
- every RoleBinding in `config.namespaceRbac`, in any namespace, that binds the ServiceAccount
  points to a minter-only Role: a Role declared in the same namespace as the RoleBinding whose every
  rule is exactly `serviceaccounts/token` with `create` or `serviceaccounts` with `get`, in the core
  group, one resource and one verb per rule, with `resourceNames` that are ServiceAccounts set to
  `true` in `serviceAccounts` of that namespace;
- no other ServiceAccount of the namespace names the same `tokenSecret`;
- every part of `config.namespaceRbac` can be inspected: while any namespace entry, any
  `serviceAccounts` or `roleBindings` section, or any RoleBinding or its `serviceAccounts` is not a
  map, no token Secret is rendered, because a malformed RoleBinding could name the ServiceAccount
  in a way the guard cannot read.

Because the token never expires and is never deleted by the policy, the guard also protects the
ServiceAccount from gaining rights later. Whenever a ServiceAccount has a `tokenSecret`, accepted or
refused:

- a RoleBinding that binds it to a Role that is not minter-only is refused, and so is any Role bound
  to it that has a rule that is not minter-only;
- a Role that reads the token Secret through `secrets` `resourceNames` is refused;
- a Role that requests tokens for that ServiceAccount through `serviceaccounts/token` is refused,
  so a minter cannot mint tokens for itself or for another minter.

A workload created in the namespace of the token Secret can mount it, so creating one is equivalent
to reading the token. The token Secret is therefore refused while its namespace has any Role,
declared in `config.namespaceRbac` (accepted or refused) or live on the cluster, that:

- creates or changes workloads: `create`, `update`, `patch`, or `*` on `pods`, `podtemplates`,
  `deployments`, `statefulsets`, `daemonsets`, `replicasets`, `jobs`, `cronjobs`,
  `replicationcontrollers`, `deploymentconfigs` and their `instantiate` and `rollback`
  subresources, `builds` and their `clone`, `docker`, and `source` subresources, `buildconfigs` and
  their `instantiate`, `instantiatebinary`, and `webhooks` subresources, `taskruns` and
  `pipelineruns` (Red Hat OpenShift Pipelines), `virtualmachines` and `virtualmachineinstances`
  (Red Hat OpenShift Virtualization), `workflows` (Argo Workflows), or any resource that contains
  `*`, such as `*/instantiate`;
- can read the token Secret: `secrets` or `*` without `resourceNames`, or with the name of the token
  Secret.

The workload list is not exhaustive: an operator can add a custom resource that runs pods. Review
every Role in the namespace of a token Secret. The order of changes does not matter: a Role that
creates workloads and is added after the token Secret exists is held on the managed cluster, and
reported, while a live token Secret managed by namespace-rbac is in its namespace.

On the managed cluster:

- before the token Secret is rendered, the policy lists every live RoleBinding and
  ClusterRoleBinding on the cluster, and every live Role in the namespace of the Secret. The token
  Secret is not created or changed, and the cause is reported, when a ClusterRoleBinding names the
  ServiceAccount, when a RoleBinding names it without pointing to a minter-only Role that the
  policy renders in that evaluation (a held Role, a ClusterRole, and a RoleBinding created by hand
  all count), or when a live Role in that namespace creates workloads or can read the Secret. A
  subject names the ServiceAccount as `kind: ServiceAccount` or as `kind: User` with the name
  `system:serviceaccount:<namespace>:<name>`;
- a RoleBinding to a Role that is not minter-only is held, and reported, when one of its subjects
  has a live Secret of type `kubernetes.io/service-account-token` labeled
  `autoshift.io/managed-by: namespace-rbac`;
- a Role that is not minter-only is held, and reported, while a live RoleBinding in its namespace
  binds it to a ServiceAccount that has such a Secret.

The last two cover a `tokenSecret` that was removed from the values while its Secret, and its
token, still exist: the live minter Role and RoleBinding keep their rules and subjects until the
Secret is deleted by hand.

On the managed cluster the token Secret is held, and reported, when the ServiceAccount exists
without the managed-by label, when a Secret of that name exists without the label, or when a
labeled Secret of that name is of another type or for another ServiceAccount. A Role that names a
token Secret in its `secrets` resourceNames is held, as for any token Secret.

Every token Secret that is rendered is listed under the `token-secret-exceptions` key of the status
ConfigMap, as an informational line. On its own it does not make `policy-namespace-rbac-test`
NonCompliant. A token
Secret that is refused or no longer declared is not deleted: a labeled one is reported by the
`policy-namespace-rbac-test`, and its token stays valid until it is deleted by hand.

Know the risk before you use the exception:

- What the token can do is the combined rights of every ServiceAccount it can mint tokens for. A
  minter for a build ServiceAccount carries everything that build ServiceAccount can reach,
  including the Secrets that build configurations can read in its namespace.
- The guard checks RoleBindings and ClusterRoleBindings that name the ServiceAccount directly. A
  binding to a group that includes it, such as `system:authenticated`, `system:serviceaccounts`, or
  `system:serviceaccounts:<namespace>`, is not checked, and neither is access granted outside RBAC
  objects, for example by an admission webhook or an external authorizer. Keep the minter
  ServiceAccount out of every other binding.
- A refusal never revokes anything. A token Secret that is already live keeps its token, and a
  held Role or RoleBinding keeps its live rules and subjects. A NonCompliant
  `policy-namespace-rbac-test` needs action: read `namespace-rbac-status` and fix the cause, or
  delete the object by hand.
- Anyone who can read Secrets in the namespace of the ServiceAccount, which includes the `edit` and
  `admin` roles of that namespace, can read the token. Put the minter in a namespace where few
  identities can read Secrets.

## Safety gate

Each rule is checked after its strings are trimmed and lower-cased. A Role is refused when any rule
contains:

- `*` anywhere in `apiGroups`, `resources`, `verbs`, or `resourceNames`, including a subresource
  wildcard such as `pods/*`;
- the verbs `escalate`, `bind`, or `impersonate`, which grant more than the Role itself holds;
- the resources `roles`, `rolebindings`, `clusterroles`, `clusterrolebindings`, or
  `rolebindingrestrictions`, with any subresource, because RBAC objects grant permissions;
- the resource `securitycontextconstraints`, because the `use` verb on an SCC changes the security
  posture of pods. Grant SCC use with the `custom-sccs` policy;
- the resource `nodes`, with any subresource such as `nodes/proxy`, which reaches the host;
- `pods/exec`, `pods/attach`, `pods/portforward`, `pods/proxy`, `pods/ephemeralcontainers`, or
  `services/proxy`, which reach into running workloads;
- `builds/custom`, because custom builds run privileged;
- `manifestworks`, `policies`, `placementbindings`, `placements`, `managedclusteractions`, or
  `managedclusterviews`, in any API group and with any subresource, because they act on managed
  clusters through Red Hat Advanced Cluster Management;
- `secrets` or `serviceaccounts/token` without `resourceNames`;
- `serviceaccounts/token` with a resource name that is not set to `true` in `serviceAccounts` of
  the same namespace. Being declared does not make a ServiceAccount owned: if it already exists
  without the label `autoshift.io/managed-by: namespace-rbac`, the Role is held on the managed
  cluster, as described below;
- `serviceaccounts` with the verb `create` without `resourceNames`;
- `namespaces` or `projects`, with any subresource, with a verb other than `get`, `list`, and
  `watch`, because the labels and annotations of a namespace set its pod security admission level
  and its UID range;
- a `resourceNames` list that is set but empty, which would mean every name.

The policy also refuses:

- a platform namespace, as the namespace of an entry or of a subject: `kube-*` (including
  `kube-system` and `kube-public`), `openshift`, `openshift-*`, `default`,
  `open-cluster-management*`, `multicluster-engine`, `hive`, `local-cluster`, `policies-*` (the
  AutoShift policy namespace), `hypershift`, `clusters`, and `clusters-*` (the default namespaces of
  hosted clusters and of their hosted control planes);
- the namespace of a ManagedCluster, as the namespace of an entry or of a subject, because a hub
  keeps the configuration and credentials of that cluster there. This check runs on the managed
  cluster, so it follows the ManagedClusters of the cluster that the RoleBinding lands on: on a
  managed hub in a hub of hubs, its own clusters, not those of its parent. ManagedClusters are
  listed only where their CRD exists, and a namespace labeled
  `cluster.open-cluster-management.io/managedCluster` or `open-cluster-management.io/cluster-name`
  is refused too, so a cluster namespace is refused even when the list is empty. A list that fails,
  for example as Forbidden, fails the template, and nothing is applied;
- `default` as a key of `serviceAccounts` or as a subject ServiceAccount: OpenShift creates it in
  every namespace, and it runs every pod of its namespace that names no ServiceAccount;
- a RoleBinding whose `role` is not declared in the same namespace, or has an error itself;
- a RoleBinding whose `clusterRole` is not `edit` or `view`, or that sets both `role` and
  `clusterRole`;
- template delimiters, `{{` or `}}`, in any name or value;
- an invalid name, an unknown key, or a value of the wrong type.

On the managed cluster, a Role is held, not created or changed, and reported, when:

- a `secrets` resource name names a Secret of type `kubernetes.io/service-account-token` or
  `kubernetes.io/dockercfg` in its namespace, because reading it hands out the credentials of a
  ServiceAccount;
- a `serviceaccounts/token` resource name names a ServiceAccount that exists without the label
  `autoshift.io/managed-by: namespace-rbac`, because a token for it carries the permissions that
  someone else granted it.

Every RoleBinding to a held Role is held too. The checks run on every evaluation, so a Secret or
ServiceAccount that is created later holds the Role from then on.

A subject whose namespace does not exist on the managed cluster is left out of its RoleBinding and
reported, so that no grant waits for whoever creates that namespace first. When no subject is left,
the RoleBinding is not created or changed.

Holding or skipping an object never changes what is live. A held Role keeps its earlier rules, and
a held RoleBinding, or one with no subject left, keeps its earlier subjects, until the cause is
fixed or the object is changed or deleted by hand. Check the status ConfigMap after every refusal.

The hub templates of this policy read only the rendered configuration in the policy namespace, so
they run without a hub template service account (`hubTemplateOptions.serviceAccountName`).

The gate is a deny list, so it cannot know every resource of every operator. Review each new rule
for what it allows, in particular verbs that create workloads: a subject that can create pods or
build configurations in a namespace can run them as any ServiceAccount of that namespace.

Granting `create` or `patch` on `buildconfigs` in a namespace, with `buildconfigs/instantiate` or
`buildconfigs/instantiatebinary`, effectively grants read access to every Secret in that
namespace and write access to its ImageStreams. A build configuration can mount any Secret of the
namespace as a build input and push the result to an external registry, and its output can
overwrite any ImageStream tag. A `resourceNames` limit on `secrets` does not bound that. Put build
identities in a namespace that holds only the Secrets they need.

The lists are the `$denied`, `$badVerbs`, and `$platformNs` variables in both `namespace-rbac.yaml`
templates. Keep them in sync with this page.

## Status

Errors are reported in the ConfigMap `namespace-rbac-status` in the
`open-cluster-management-agent-addon` namespace of the managed cluster, one data key per namespace,
messages joined by `; `. Each message names the entry, for example `roles."example-ci-builds":
rules[0].verbs escalate is never granted: it escalates privileges`. The key is the namespace with
invalid characters replaced by `_`, a leading `.` replaced by `_`, an empty name written as `_`,
cut to 240 characters, and a suffix such as `-2` when two names produce the same key. The ConfigMap
exists only while there are errors, and `policy-namespace-rbac-test` is NonCompliant while it
exists.

A namespace-level error, such as an invalid or platform namespace, an unknown key, or a section
that is not a map, skips the whole namespace. Any other error skips only its ServiceAccount, Role,
or RoleBinding. On the managed cluster:

- a namespace that does not exist is reported, and nothing is created in it. A namespace that
  another policy creates, such as `project-onboarding`, can be reported as missing until it exists;
  the next evaluation then creates the declared objects and the report clears;
- a ServiceAccount, Role, or RoleBinding of a declared name that exists without the label
  `autoshift.io/managed-by: namespace-rbac` belongs to someone else. It is reported and never
  changed;
- a RoleBinding whose Role belongs to someone else is not created or changed, because it would
  grant the rules of that Role.

Objects that a skipped entry created earlier stay as they are. A hub template `fail` is not used,
because one bad entry would then block every valid entry.

```bash
oc get configmap namespace-rbac-status -n open-cluster-management-agent-addon -o yaml
```

## Removal

Removal is manual. An object labeled `autoshift.io/managed-by: namespace-rbac` that is no longer
declared is not deleted. That covers a removed namespace, Role, or RoleBinding, a ServiceAccount set
to `false`, and a RoleBinding whose subjects are all `false`. `policy-namespace-rbac-test` reports
each one as a `mustnothave` violation, so it stays NonCompliant until the object is removed
by hand:

```bash
oc delete rolebinding <name> -n <namespace>
oc delete role <name> -n <namespace>
oc delete serviceaccount <name> -n <namespace>
oc delete secret <token-secret> -n <namespace>   # a reported token Secret: revokes its token
```

To revoke one subject of a RoleBinding that keeps others, set it to `false`: `mustonlyhave` removes
it. To revoke the last subject, set it to `false` or remove the RoleBinding, then delete the
RoleBinding by hand.

Objects of an entry that has errors are not reported, because the entry is still declared, and
nothing in a namespace with a namespace-level error is reported.

## Dependencies

None. Namespaces must exist already; a namespace that another policy creates, such as
`project-onboarding`, is reported as missing until it does. See [Policies](#policies) for why
the two Policies of this directory do not depend on each other.

## Placement

Every cluster, hub or managed, with the label `autoshift.io/namespace-rbac: 'true'`. Set the label
in the values files, never directly on a cluster. Both Policies share `placement.yaml` and one
PlacementBinding, `binding-policy-namespace-rbac`.

## Layout

```
namespace-rbac/
  kustomization.yaml            # entry point: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator: policy graph, remediation, evaluation interval
  placement.yaml                # Placement predicate and tolerations
  manifests/
    namespace-rbac.yaml         # object-templates-raw, $want enforce: policy-namespace-rbac
  test/
    namespace-rbac.yaml         # the same template, $want inform: policy-namespace-rbac-test
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/namespace-rbac

# Full validation: render, hub and spoke template resolution, label contract
make test
```
