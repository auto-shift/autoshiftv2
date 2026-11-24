# project-onboarding

Onboards OpenShift projects declared in the AutoShift values files. For each entry in
`config.projectOnboarding.projects` the policy creates a Namespace with OpenShift Project annotations, labels and
annotations built from templates, RoleBindings for the project's admin, write, and read groups, and
a ResourceQuota and LimitRange from a named size, NetworkPolicies, an EgressFirewall, and an
AdminNetworkPolicy from a named network profile and the project's own rules, and an EgressIP from
the project's own addresses. Shared conventions,
such as group-name patterns, the label catalog, the sizes, and the network profiles, are defined
once in `config.projectOnboarding.defaults`. Each project supplies only what is specific to it. Admin network
policies in `config.projectOnboarding.defaults` apply rules that no project can override.

A project that cannot be onboarded is skipped and reported by name in a status ConfigMap, and every
other project still applies. See [Status](#status).

## Enable

| Label | Default | Description |
|---|---|---|
| `autoshift.io/project-onboarding` | `'false'` | Set `'true'` to onboard the projects in `config.projectOnboarding.projects` |
| `autoshift.io/project-onboarding-prune` | `'false'` | Set `'true'` to delete owned objects and remove catalog labels that are no longer declared. Any other value only reports them. See [Pruning](#pruning) |

Set them in a clusterset or cluster values file, without the `autoshift.io/` prefix. Never put them
on a ManagedCluster by hand.

## Configuration

Projects live at `config.projectOnboarding.projects.<project-name>`. Shared defaults live at `config.projectOnboarding.defaults`,
typically in `global.yaml` or a clusterset file. The normal precedence applies: cluster over
clusterset over global, as a deep merge. See `autoshift/values/clustersets/_example.yaml` for a
complete example.

```yaml
config:
  projectOnboarding:
    defaults:
      groups:                         # group-name template per access level
        admin: 'K8S_{team}_ADMINS_{environment}'
        write: 'K8S_{team}_DEVELOPERS_{environment}'
        read: 'K8S_{team}_VIEWERS_{environment}'
      roles:                          # optional, these are the defaults
        admin: admin
        write: edit
        read: view
      labels:                         # label catalog, keyed by entry name
        team:
          enabled: true
          key: 'example.com/team'
          value: '{team}'
        cost-center:
          enabled: true
          key: 'example.com/cost-center'
          value: 'cc-{costCenter}'    # skipped when the project has no costCenter
        vlan:
          enabled: true
          key: 'network.example.com/vlan-{vlan}'
          value: 'true'
      annotations: {}                 # same shape as labels
      sizes:                          # named quota bundles, see Quotas
        small:
          quota:
            requests.cpu: '4'
            requests.memory: 8Gi
            limits.memory: 16Gi
            pods: '50'
          limitRange:
            default: {cpu: 500m, memory: 512Mi}
            defaultRequest: {cpu: 100m, memory: 128Mi}
      networkProfiles:                # named network rule bundles, see Network rules
        strict:
          defaultDeny: {ingress: true, egress: true}
          rules:
            dns: {direction: egress, action: allow, to: {dns: true}}
        permissive:
          defaultDeny: {ingress: true, egress: false}
          rules:
            same-namespace: {direction: ingress, action: allow, from: {sameNamespace: true}}
            router: {direction: ingress, action: allow, from: {router: true}}
            monitoring: {direction: ingress, action: allow, from: {monitoring: true}}
    projects:
      payments-api:
        displayName: Payments API     # optional, becomes openshift.io/display-name
        team: payments                # placeholder values, free-form keys
        environment: NONPROD
        costCenter: '4711'
        vlan: '70'
        groups:
          read: K8S_SHARED_AUDITORS   # overrides the resolved name for one level
        labels:
          cost-center: {enabled: false}   # switches off a catalog entry for this project
        size: small                   # ResourceQuota and LimitRange from projectOnboarding.defaults.sizes.small
        quota:
          pods: '80'                  # overrides one key of the size
        networkProfile: strict        # NetworkPolicies from projectOnboarding.defaults.networkProfiles.strict
        network:
          rules:
            from-frontend:
              direction: ingress
              action: allow
              from: {namespaces: {labels: {example.com/team: web}}}
              ports: [8443]           # no protocol: TCP
            dns: {enabled: false}     # removes the profile's dns rule
        egressIP: ['198.51.100.50']   # optional, EgressIP autoshift-payments-api, see EgressIP
      legacy-app:
        team: legacy
        environment: PROD
        adopt: true                   # may take over an existing namespace, see Mode and adoption
```

Project keys:

| Key | Meaning |
|---|---|
| `namespace` | Namespace name, a DNS-1123 label. Defaults to the project key, also when it is `null`. `''` uses the project key too, with a note in the status. A value that is not a string, such as `false` (also YAML `no`) or a number, is reported and skips the project |
| `displayName`, `description`, `requester` | Become the `openshift.io/display-name`, `openshift.io/description`, and `openshift.io/requester` annotations |
| `groups.<level>` | Overrides the resolved group name for `admin`, `write`, or `read`. `''`, `null`, or `false` drops the level for this project. Any other level is reported |
| `labels.<entry>`, `annotations.<entry>` | Overrides a catalog entry by name, or adds a project-only entry |
| `size` | Name of an entry in `projectOnboarding.defaults.sizes`. See [Quotas](#quotas) |
| `quota.<resource>`, `limitRange.<field>` | Overrides keys of the selected size |
| `networkProfile` | Name of an entry in `projectOnboarding.defaults.networkProfiles`. See [Network rules](#network-rules) |
| `network.rules.<rule>`, `network.defaultDeny` | Overrides the profile's rules by name and its default deny, or adds rules |
| `egressIP` | Optional list of IPv4 or IPv6 addresses for the project's egress traffic. See [EgressIP](#egressip) |
| `adopt` | `true` lets the project take over an existing namespace that no project owns. `true`, `false`, `'true'`, and `'false'` are accepted; any other value is reported and counts as `false` |
| `mode` | `enforce` or `inform`. Any other value, including `false` or `0` (YAML reads `mode: off` as `false`), is reported and treated as `inform` |
| any other key | A placeholder value for templates, for example `team` or `costCenter` |

Collections are maps keyed by name, because AutoShift replaces lists whole when it merges values.
A lower level can therefore override one entry without restating the others.

### Configuration reference

Every key of `config.projectOnboarding.defaults`. A `projectOnboarding.defaults` value of the wrong type stops every
project, because it affects them all: the hub template fails and the live objects stay as they are.
`adminNetworkPolicies` is the exception: its errors stop only the admin tier.

| Key | Type | Default | Example | Invalid input |
|---|---|---|---|---|
| `groups.<level>` | map of `admin`, `write`, `read` to a group-name template | none: a project with no resolvable `admin` group is skipped | `admin: 'K8S_{team}_ADMINS'` | Not a map: hub template fails. A level that does not resolve gets no RoleBinding |
| `roles.<level>` | map of level to a ClusterRole name | `admin: admin`, `write: edit`, `read: view` | `write: edit` | Not a map: hub template fails |
| `labels.<entry>` | map of entry name to `{enabled, key, value}` | `{}` | `team: {enabled: true, key: example.com/team, value: '{team}'}` | See [Templates and skip rules](#templates-and-skip-rules): the entry is skipped and reported |
| `annotations.<entry>` | same shape as `labels` | `{}` | `owner: {enabled: true, key: example.com/owner, value: '{team}'}` | As for `labels` |
| `allowedPlatformKeys.<key>` | map of an exact label or annotation key to `true` | `{}`, every platform key refused | `pod-security.kubernetes.io/warn: true` | Not a map: hub template fails |
| `sizes.<size>` | map of size name to `{quota, limitRange}` | `{}` | `small: {quota: {pods: '50'}}` | See [Quotas](#quotas) |
| `networkProfiles.<profile>` | map of profile name to `{defaultDeny, rules}` | `{}` | `strict: {defaultDeny: {ingress: true, egress: true}}` | See [Network rules](#network-rules): the projects that select it are skipped |
| `adminNetworkPolicies.<entry>` | map of entry name to `{appliesTo, direction, action, to or from}` | `{}`, no admin tier | see [Admin network policies](#admin-network-policies) | Reported under `projectOnboarding.defaults.adminNetworkPolicies` in the status of shard 0; the whole tier stops |

Every key of `config.projectOnboarding.projects.<project>`. An invalid project is skipped and reported by name in the
[status](#status), and every other project still applies.

| Key | Type | Default | Example | Invalid input |
|---|---|---|---|---|
| `<project>` (the map key) | DNS-1123 label | required | `payments-api` | Project skipped and reported |
| `namespace` | DNS-1123 label | the project key | `payments-prod` | Not a string, or not a DNS-1123 label: project skipped. A platform namespace: project skipped, no override |
| `displayName`, `description`, `requester` | string | not set | `displayName: Payments API` | A list or a map: that annotation skipped and reported |
| `groups.<level>` | string, or `''`, `null`, `false` to drop the level | the `projectOnboarding.defaults.groups` template | `read: K8S_SHARED_AUDITORS` | Not a string: the level gets no RoleBinding. Unknown level: ignored and reported |
| `labels.<entry>`, `annotations.<entry>` | map, same shape as the catalog | the catalog entry | `cost-center: {enabled: false}` | Entry skipped and reported |
| `size` | string, the name of a size | no ResourceQuota and no LimitRange | `small` | Undefined or not a string: project skipped |
| `quota.<resource>` | map of resource name to quantity string | the size's value | `pods: '80'` | Set without `size`: ignored and reported. Bad key or quantity: no ResourceQuota rendered |
| `limitRange.<field>` | map of `default`, `defaultRequest`, `max`, `min`, `maxLimitRequestRatio` to resource maps | the size's value | `default: {memory: 1Gi}` | Unknown field, bad key or quantity: no LimitRange rendered |
| `networkProfile` | string, the name of a profile | no profile | `strict` | Undefined or not a string: project skipped |
| `network.defaultDeny.<direction>` | `true` or `false` per `ingress`, `egress` | the profile's value, else `false` | `egress: true` | Not a boolean: project skipped |
| `network.rules.<rule>` | map of rule name to a rule | the profile's rules | see [Network rules](#network-rules) | Project skipped and reported |
| `egressIP` | list of IPv4 or IPv6 addresses | no EgressIP | `['198.51.100.50']` | See [EgressIP](#egressip): project skipped |
| `adopt` | `true` or `false` | `false` | `true` | Any other value: reported, counts as `false` |
| `mode` | `enforce` or `inform` | `enforce`, or `inform` with `adopt: true` | `inform` | Any other value: reported, treated as `inform` |
| any other key | string | not set | `team: payments` | A list, a map, or a blank value leaves the placeholder unresolved |

The project key and the namespace must be DNS-1123 labels: lowercase letters, digits, and `-`,
starting and ending with a letter or digit, at most 63 characters. A project whose key or namespace
is not, or whose entry is not a map, is skipped whole and reported. A project entry that is
malformed never affects the other projects. An entry that is not a map claims no namespace, so a
valid project can declare the namespace of its key. Its live objects outside the namespaces other
projects declare still carry its `autoshift.io/project` label, so they belong to a project in error
and are never pruned.

The policy never manages a platform namespace. A project whose namespace, the project key by
default, is `openshift`, `default`, `multicluster-engine`, or `hive`, or starts with `kube-`,
`openshift-`, or `open-cluster-management`, is skipped whole and reported as `namespace <ns> is a
platform namespace, which project onboarding never manages`. That covers `kube-system`,
`kube-public`, `openshift-dns`, and `open-cluster-management-agent-addon`, which holds the
policy's own status. There is no override, with or without `adopt: true`.

### Templates and skip rules

A template is a string with `{field}` placeholders, filled from the merged project fields.
`{name}` is always the project name. Label keys and values, annotation keys and values, and group
names are templates.

A placeholder is any `{...}` without nested braces, so `{cost-center}`, `{team.name}`, and
`{team name}` are placeholders too. Filling is one pass over the template's own placeholders: a
value is inserted as written and is never expanded again, so `team: '{zone}'` renders `{zone}`. A
placeholder is unresolved when no project field of that name holds a single value with at least one
character other than a space. An empty, blank, list, or map value leaves it unresolved. Nested
braces are not placeholders: in `{a{b}}` only `{b}` is one, and the braces around it stay as
written. A label cannot contain braces, so such a label is reported as invalid, and an annotation
value or a group name keeps them literally.

- A label or annotation is skipped when a placeholder in its key or value is unresolved. An
  explicitly empty value, `value: ''`, is kept: `k8s.ovn.org/primary-user-defined-network` takes an
  empty value. An entry without `value`, or with `value: null`, renders no empty value.
- An access level whose group name has an unresolved placeholder, or is blank, gets no RoleBinding.
  When that name is a project override of `write` or `read`, the status lists it as a note, for
  example `group read name "K8S_{tem}_AUDIT" has an unresolved placeholder, so the level gets no
  RoleBinding`. A note is not an error: the project still renders and its stale objects are still
  pruned.
- Optional fields that are absent are skipped: labels, annotations, `displayName`, `description`,
  `requester`, and `size`.
- A project with no resolvable `admin` group is skipped whole, including its Namespace, and
  reported.
- A label whose key or value is not valid Kubernetes label syntax is skipped and reported. That
  covers values over 63 characters or with invalid characters, keys with more than one `/`, a key
  name over 63 characters, and a key prefix that is not a DNS subdomain of at most 253 characters.
- A catalog label that tries to set the ownership key `autoshift.io/project` or
  `autoshift.io/managed-by` is skipped and reported.
- A label or annotation with a platform key is skipped and reported, unless
  `projectOnboarding.defaults.allowedPlatformKeys` allows it. See [Platform keys](#platform-keys).
- A namespace that several projects declare belongs to the project named after it, otherwise to
  the first of them in name order. Every other project that declares it is skipped whole and
  reported. A new project that names an existing project's namespace by mistake therefore never
  displaces a project whose key is that namespace.
- A catalog entry's `enabled` is decided by the last level that sets it, so a project's `enabled:
  false` turns off an entry that the catalog sets to `enabled: true`.
- A project `labels`, `annotations`, or `groups` value that is not a map, a catalog entry that is
  not a map, a catalog `enabled` that is not `true` or `false`, a group name that is not a string, a
  group level other than `admin`, `write`, and `read`, and a `displayName`, `description`, or
  `requester` that is a list or a map are each skipped and reported. The rest of the project still
  renders.
- An annotation key must be a qualified name, as a label key is: an optional DNS subdomain prefix of
  at most 253 characters and `/`, then a name of at most 63 letters, digits, `-`, `_`, and `.`,
  starting and ending with a letter or digit. Any other key is skipped and reported as `annotation
  key <key> is not a valid qualified name`, since the API server would reject the whole Namespace.
- Two catalog entries that set the same label or annotation key to different values are reported,
  and the first entry by name is applied. A `displayName`, `description`, or `requester` and a
  catalog annotation that set the same annotation are reported, and the field is applied.

A skipped label is also left out of `autoshift.io/project-labels`.

Values are data, never template code. Hub templates run first, and their output is parsed again as
the managed-cluster template, so the policy never writes a user string into that template as text.
Group and role names, labels, annotations, and error messages travel as base64-encoded JSON and are
decoded on the managed cluster. Base64 matters: before parsing, the managed cluster rewrites
template text that looks like a `toInt`, `toBool`, or `autoindent` call, even inside a quoted
string, and base64 contains none of the characters those patterns need. A group name such as
`{{ fail "x" }}` is an unresolved placeholder. A value such as `x{{y` renders literally. Neither
can break or run code in the policy. Only constants, numbers, and values the hub has checked
against a strict pattern, such as DNS-1123 names and CIDRs, are written directly.

### Labels and annotations

Every Namespace carries `autoshift.io/project: <name>` and `autoshift.io/managed-by:
project-onboarding`. The annotation `autoshift.io/project-labels` lists the catalog label keys the
policy applied.

Labels are applied when the policy creates the Namespace and kept in sync afterwards. Some labels,
such as `k8s.ovn.org/primary-user-defined-network`, only take effect when present at creation.

Once the Namespace exists, the policy renders it with `metadataComplianceType: mustonlyhave`, which
makes its labels and annotations exact. To keep what other tools set (for example
`openshift.io/sa.scc.*`, `pod-security.kubernetes.io/*`, `kubernetes.io/metadata.name`), the managed
cluster reads the live Namespace, renders all of its labels and annotations, and then applies this
run's labels and annotations. Annotations are merged and never removed. A Namespace that does not
exist yet has nothing to preserve, so it is created with this run's metadata and no
`metadataComplianceType`.

A catalog label that is later disabled or removed is a dropped label: `autoshift.io/project-labels`
lists it from an earlier run, and this run no longer applies it. What happens to it depends on
`project-onboarding-prune`, see [Pruning](#pruning).

#### Platform keys

Some label and annotation keys change how the platform treats a namespace, so a project never sets
them by default:

| Key | Why it is reserved |
|---|---|
| `network.openshift.io/policy-group` | Makes the namespace the `router` or `monitoring` peer of every other project's rules and of the admin network policies |
| `pod-security.kubernetes.io/*` | Lifts Pod Security admission |
| `openshift.io/run-level` | Exempts the namespace from SCC admission |
| `openshift.io/sa.scc.*` | Sets the UID, supplemental group, and SELinux ranges that SCCs allow, for example a UID range that admits root |
| `security.openshift.io/*` | SCC settings of the namespace |
| `openshift.io/node-selector`, `scheduler.alpha.kubernetes.io/*` | Move workloads, for example onto control plane nodes |
| `kubernetes.io/metadata.name` | Set by the API server to the namespace name; any other value never complies |

A label or annotation with one of these keys is skipped and reported as `label <key> is reserved
for the platform; list it in projectOnboarding.defaults.allowedPlatformKeys to set it`. The project still
renders. To allow a key, list it in `projectOnboarding.defaults.allowedPlatformKeys`, a map of the exact key to
`true`. The map is values controlled, so a platform administrator decides which keys projects can
set:

```yaml
projectOnboarding:
  defaults:
    allowedPlatformKeys:
      pod-security.kubernetes.io/warn: true     # every project may set this exact key
```

An entry allows only its exact key, never a prefix: allowing `pod-security.kubernetes.io/warn`
leaves `pod-security.kubernetes.io/enforce` reserved. Other keys, such as
`k8s.ovn.org/primary-user-defined-network`, are not reserved. The live values that other tools set
on these keys are kept, as for any other label.

Do not edit `autoshift.io/project-labels` by hand. With pruning on, every key it lists that the
catalog no longer applies is removed from the Namespace on the next evaluation. With pruning off, a
dropped key stays listed. If another tool sets a label with that key in the meantime, turning
pruning on later removes that label too.

### Access (RBAC)

`projectOnboarding.defaults.groups` holds a group-name template per access level (`admin`, `write`, `read`).
Each level that resolves gets a RoleBinding named `autoshift-<level>` in the project namespace,
with the group as a `Group` subject (`apiGroup: rbac.authorization.k8s.io`). `projectOnboarding.defaults.roles`
maps levels to ClusterRoles and defaults to `admin`, `edit`, and `view`.

A role change re-creates the RoleBinding, because Kubernetes does not allow `roleRef` to change in
place. The RoleBindings are exact (`mustonlyhave`), so labels another tool adds to them are removed.
Only the enforcing shard Policy carries `recreateOption: IfRequired`: the `-test` Policy asserts the
same binding without it, so an Enforce set on a `-test` Policy by hand reports the mismatch and
never deletes a binding.

A role must be a string of at most 253 characters that the API server accepts as a name: not `.`
or `..`, and without `/` or `%`. Any other role is reported for every project that binds that level,
for example `projectOnboarding.defaults.roles.admin "team/admin" is not a valid ClusterRole name`.
That level's RoleBinding is not rendered and stays declared, so the live binding is kept as it is
and is never deleted to be created under a name the API server refuses.

A project can drop a level that `projectOnboarding.defaults.groups` defines by setting it to `''`, `null`, or
`false`, for example `groups: {write: ''}` for a read-only project. The level gets no RoleBinding,
and with pruning on its earlier RoleBinding is removed. Dropping `admin` leaves the project with no
resolvable admin group, so it is skipped and reported.

The policy binds group names only. The groups must exist in the identity source and reach
OpenShift, through the OIDC `groups` claim or LDAP group sync.

### Quotas

`projectOnboarding.defaults.sizes` holds named quota bundles. A project selects one with `size`, and the policy
renders two objects in the project namespace from it:

- The ResourceQuota `autoshift-quota`, whose `spec.hard` is the size's `quota` map.
- The LimitRange `autoshift-limits`, with one item of type `Container` whose `default`,
  `defaultRequest`, `max`, `min`, and `maxLimitRequestRatio` come from the size's `limitRange` map.

The project's own `quota` and `limitRange` maps are merged over the size key by key, so
`quota: {pods: '80'}` changes only `pods`. Nested maps merge the same way: `limitRange: {default:
{memory: 1Gi}}` keeps the size's default CPU. A project override changes only that project, never
the shared size.

- A project without `size` gets neither object. A `quota` or `limitRange` it sets anyway is reported
  as `quota is set without size` or `limitRange is set without size`. The rest of the project still
  renders, and while the error stands nothing of the project is pruned, so live quota objects stay.
- A size with no `quota` keys renders no ResourceQuota, and one with no `limitRange` keys renders no
  LimitRange. An empty `limitRange` field, such as `max: {}`, is left out.
- A project cannot remove or empty a key of its size. An override with an empty value, such as
  `pods: ''` or `pods: null`, is ignored and the size's value stays, in `quota` and inside each
  `limitRange` field alike. A `limitRange` field set to `null` is ignored the same way. To drop a
  key, use a different size.
- Write quantities as strings, for example `'4'`, `500m`, or `8Gi`. Numbers are accepted and rendered
  as strings. A negative quantity is rejected.
- A key without a `/` must be a standard resource name. For `quota` that is `pods`, `services`,
  `replicationcontrollers`, `resourcequotas`, `secrets`, `configmaps`, `persistentvolumeclaims`,
  `services.nodeports`, `services.loadbalancers`, `cpu`, `memory`, `ephemeral-storage`,
  `requests.` or `limits.` with `cpu`, `memory`, or `ephemeral-storage`, `requests.storage`, and
  `hugepages-<size>` or `requests.hugepages-<size>`. For `limitRange` that is `cpu`, `memory`,
  `ephemeral-storage`, and `hugepages-<size>`. Any other unprefixed key is reported, since the API
  server would reject the object. Prefixed keys, such as `count/deployments.apps` or
  `requests.nvidia.com/gpu`, are checked for syntax only.
- A quantity is rendered as written, and the API server stores it in canonical form. A value that
  is not certainly canonical is listed in the status as a note: `0.5` (stored as `500m`), `1000m`
  (`1`), `1000` (`1k`), `1024Mi` (`1Gi`), `+5`, `.5`, `5.`, and exponent forms such as `1e3`. A
  note is not an error, so the object still renders and pruning still runs. Write the canonical form
  to clear it: an integer that does not end in `000`, optionally followed by `n`, `u`, `m`, `k`,
  `M`, `G`, `T`, `P`, or `E`, or an integer that is not a multiple of 1024 followed by `Ki`, `Mi`,
  `Gi`, `Ti`, `Pi`, or `Ei`, or `0`.

The policy reports non-canonical quantities rather than rewriting them. A rewrite would have to
reproduce the API server's formatting exactly, including the choice between decimal and binary
suffixes and exponent forms, and any mismatch would make the exact objects differ from the stored
ones on every evaluation without a report. The check instead passes only forms whose stored value is
the same text, so a quantity that is not reported cannot drift, and every other one is visible.

A `size` that `projectOnboarding.defaults.sizes` does not define, a `size` that is set but not a string (such
as `true`, `1`, or `0.5`), and a size entry that is not a map are required-input errors: the
project is skipped whole and reported. A `quota` or `limitRange` value that is not a map, a
`limitRange` field that is set and not a map (such as `max: 0`), a key that is not a valid resource
name, a value that is not a valid quantity, and an unknown `limitRange` field are each reported,
and only the object affected is skipped. The project still renders, and the live object is left as
it is rather than replaced with a partial one.

Both objects are exact (`mustonlyhave`), so a key removed from the size or the override is removed
from the cluster, and labels another tool adds to them are removed. The LimitRange is rendered with
the defaulting the API server applies: a `max` key missing from `default` is copied to `default`,
then a `default` or `min` key missing from `defaultRequest` is copied to `defaultRequest`. The
rendered object therefore matches the stored one. A ResourceQuota or LimitRange spec
can change in place, so the policy updates them without re-creating them.

## Network rules

`projectOnboarding.defaults.networkProfiles` holds named network rule bundles. A project selects one with
`networkProfile`, and adds or overrides rules with its own `network.rules`. Users write one rule
format, and the policy picks the object that can express each rule: NetworkPolicies, the
EgressFirewall, and the project's AdminNetworkPolicy. Rules that no project can override belong in
[admin network policies](#admin-network-policies) instead.

A profile, and a project's `network`, have two keys:

- `defaultDeny`: `ingress` and `egress`, each `true` or `false`. A project's own `defaultDeny`
  overrides the profile's, direction by direction.
- `rules`: a map of rules keyed by rule name.

Any other key, such as a misspelled `defaultdeny` or `rule`, is an error that skips the project. The
values `true` and `false` here and in the peers below are YAML Boolean values: a string such as
`'true'` is an error.

A rule has these fields:

| Field | Meaning |
|---|---|
| `direction` | `ingress` or `egress`, required |
| `action` | `allow` or `deny`, required |
| `from` (ingress) or `to` (egress) | A map of peers, see below. Required, with at least one peer |
| `ports` | Optional list. An entry is a port number, a named port, or a map with `port` and an optional `protocol` (`TCP`, `UDP`, or `SCTP`). A port without a protocol is TCP. Without `ports`, every port is allowed. An empty list is an error, so an emptied list never opens every port |
| `enabled` | Default `true`. `false` removes an inherited rule of the same name |
| `description` | Optional free text, not rendered |

Peers, each one a separate source or destination of the rule:

| Peer | Selects |
|---|---|
| `sameNamespace: true` | Every pod in the project namespace |
| `namespaces: {labels: {...}}` | Namespaces with those labels. `names: [...]` selects namespaces by name instead, through `kubernetes.io/metadata.name`. Both together must both match |
| `pods: {labels: {...}}` | Pods with those labels in the project namespace |
| `router: true` | The OpenShift router: namespaces labeled `network.openshift.io/policy-group: ingress`, which covers `openshift-ingress` and, for host-network routers, `openshift-host-network`. Not in a `deny`, see [AdminNetworkPolicy](#adminnetworkpolicy) |
| `monitoring: true` | Cluster and user workload monitoring: namespaces labeled `network.openshift.io/policy-group: monitoring`. In a `deny`, host-network pods there, such as node-exporter, are not matched, see [AdminNetworkPolicy](#adminnetworkpolicy) |
| `dns: true` | Egress only. Cluster DNS: namespace `openshift-dns` on port 5353, TCP and UDP. The rule's `ports` do not apply to it |
| `cidrs: [...]` | IP ranges outside the cluster network and service network. An allow can also contain them. On ingress a NetworkPolicy `ipBlock` with those networks excepted, see [Ingress from CIDRs](#ingress-from-cidrs). On egress an EgressFirewall rule. An IPv4-mapped IPv6 range, such as `::ffff:a80:0/110`, is an error: write the IPv4 CIDR |
| `dnsNames: [...]` | Egress only. An EgressFirewall rule. A name may start with `*.`. Each label has at most 63 characters. An IP address, such as `198.51.100.7`, is an error: use `cidrs`. A cluster-internal name ending in `.svc`, `.svc.cluster.local`, or `.cluster.local` is an error, since an EgressFirewall does not match service IPs: use `namespaces` or `pods` |

A peer set to `false` is ignored, so a project can turn off one peer of an inherited rule.

### Rendering

All NetworkPolicies select every pod in the project namespace, carry the ownership labels, and are
exact (`mustonlyhave`). They follow the project's mode, like the other project objects.

- `defaultDeny.ingress` renders `autoshift-default-deny-ingress`, which allows no ingress.
- `defaultDeny.egress` renders `autoshift-default-deny-egress`, which allows egress only to
  destinations outside the cluster: `0.0.0.0/0` except the cluster network and service network
  CIDRs, plus `::/0` with the IPv6 ones on a dual-stack cluster. In-cluster egress is denied, and
  external egress is left to the EgressFirewall, which an `ipBlock` allow could not override.
- In a direction with `defaultDeny`, each `allow` rule with an in-cluster peer, or with an ingress
  `cidrs` peer, renders the NetworkPolicy `autoshift-rule-<rule>`. An egress rule whose only peers
  are `cidrs` or `dnsNames` renders no NetworkPolicy. In a direction without `defaultDeny`, no
  `allow` rule renders a NetworkPolicy, see [Allow rules without a default deny](#allow-rules-without-a-default-deny).
- Each egress rule with `cidrs` or `dnsNames`, `allow` or `deny`, renders entries of the
  EgressFirewall, see [EgressFirewall](#egressfirewall).
- Each `deny` rule with an in-cluster peer renders rules of the project's AdminNetworkPolicy, see
  [AdminNetworkPolicy](#adminnetworkpolicy).

NetworkPolicy allows are additive, so a project rule always opens what a default deny closed.

### Allow rules without a default deny

Kubernetes isolates every pod that any NetworkPolicy selects, in that policy's direction: once one
egress NetworkPolicy selects a pod, all egress that no NetworkPolicy allows is denied. An `allow`
rule therefore only matters in two cases:

- In a direction with `defaultDeny`, where it opens what the default deny closed.
- To override a profile `deny` by name or by peer, which the project's
  [AdminNetworkPolicy](#adminnetworkpolicy) does with an `Allow` ahead of the profile's `Deny`.

In a direction without `defaultDeny`, traffic is already allowed, so the policy renders no
NetworkPolicy for an `allow` rule there. Denies and overrides of a profile deny are still rendered
in the AdminNetworkPolicy, and egress `cidrs` and `dnsNames` rules in the
[EgressFirewall](#egressfirewall). An ingress `cidrs` allow in such a direction renders nothing, so
the checks of [Ingress from CIDRs](#ingress-from-cidrs) do not run for it until the direction gets a
default deny.

Earlier versions rendered `autoshift-rule-<rule>` in every direction. A project with a profile
`deny` and no `defaultDeny`, plus one project egress `allow` to override it, lost cluster DNS and
all other egress, although it never declared a default deny. Such a NetworkPolicy is no longer
declared, so the prune sweep handles it as it handles any other owned object that a project no
longer declares: deleted with pruning on in an enforce project, reported otherwise. Where it is only
reported, delete it by hand to restore the project's traffic.

The cluster network and service network CIDRs come from the managed cluster's `Network` config
`cluster` (`config.openshift.io/v1`, `spec.clusterNetwork` and `spec.serviceNetwork`). When the
cluster has none, the policy uses `config.networking.clusterNetwork` and
`config.networking.serviceNetwork` from the values, the cluster-install shape. When neither gives
both networks, `autoshift-default-deny-egress` is rendered without any egress allow, so it denies
all egress, external included, and the project is reported. An `ipBlock` without the cluster
networks would allow all in-cluster egress, so the policy closes the project rather than open it.
The report is informational: it is not a configuration error of the project, so the project's other
objects still render and its stale objects are still pruned with pruning on.

### Ingress from CIDRs

OVN-Kubernetes matches pod IPs with a NetworkPolicy `ipBlock`, so an `ipBlock` of `0.0.0.0/0` alone
would admit every pod in the cluster, not only external clients. The managed cluster therefore
renders each ingress `cidrs` entry as an `ipBlock` whose `except` lists every cluster network and
service network CIDR that lies inside it. For example, on a cluster with `10.128.0.0/14` and
`172.30.0.0/16`, `0.0.0.0/0` excepts both and `10.0.0.0/8` excepts the first. The checks match the
egress ones:

- A CIDR that lies inside a cluster network or the service network, such as `10.129.4.0/24`, is
  an error that skips the project, and points to `namespaces` and `pods` peers.
- When the cluster network and service network CIDRs cannot be read, an ingress `cidrs` allow is an
  error, since the `except` cannot be built.

Only the cluster network and service network are excepted. The OVN-Kubernetes internal subnets
(join, transit, and masquerade) and the machine network are not, so an ingress `cidrs` allow that
covers them, such as `0.0.0.0/0`, also admits traffic from those addresses, for example from
host-network pods and nodes. Write the external ranges you mean rather than `0.0.0.0/0` where that
matters.

Traffic through a Route arrives from the router pods, so a NetworkPolicy cannot filter Route
traffic by client IP. For an inbound allowlist on a Route, use the
`haproxy.router.openshift.io/ip_whitelist` annotation on the application's Route.

### EgressFirewall

External egress is controlled by one EgressFirewall per project, named `default` because
OVN-Kubernetes accepts only that name and only one per namespace. It is rendered when the project
has an egress rule with `cidrs` or `dnsNames`, or `defaultDeny.egress`. Like the NetworkPolicies it
carries the ownership labels, is exact (`mustonlyhave`), and follows the project's mode.

OVN-Kubernetes applies the first entry that matches, so the order matters:

1. The project's own rules, in rule-name order. A profile rule that the project overrides by name
   counts as a project rule.
2. The profile's rules, in rule-name order.
3. With `defaultDeny.egress`, a final `Deny` to `0.0.0.0/0`, and to `::/0` on a dual-stack cluster
   or when the cluster networks are unknown.

Each CIDR and each DNS name of a rule is one entry, `Allow` or `Deny` after the rule's `action`,
with the rule's `ports`. An EgressFirewall takes port numbers only, so a named port on such a rule
is an error. OVN-Kubernetes accepts at most 8,000 entries, final denies included. A project that
would need more is reported and skipped.

An EgressFirewall never matches destinations in the cluster network, and service IPs are matched
after they are translated to their endpoints, so an egress `cidrs` rule for in-cluster destinations
could not do what it says. An `allow` whose CIDR lies inside the cluster network or the service
network is an error that skips the project and points to `namespaces` and `pods` peers, which
express in-cluster destinations. A wider `allow`, such as `0.0.0.0/0` or `10.0.0.0/8` around the
cluster network, renders: the EgressFirewall applies it to the external part only, and in-cluster
egress stays governed by the NetworkPolicies. A `deny` whose CIDR overlaps the cluster network or
the service network in any way is an error, since its in-cluster part would silently not apply. The
managed cluster checks the CIDRs against the ones that `autoshift-default-deny-egress` uses. When
those CIDRs are unknown, the check cannot run: a `deny` to `cidrs` is then an error, since it might
not apply, while an `allow` still renders, since an allow never opens what a deny closes and the
NetworkPolicy then denies all egress anyway.

Node IPs and the machine network are outside the cluster network, so for the EgressFirewall they
are external destinations. `autoshift-default-deny-egress` allows them, and the final `Deny` of the
EgressFirewall closes them again, because OVN-Kubernetes excludes only the cluster network from its
matches. The result is never more open than declared, and a `cidrs` rule for node IPs, allow or
deny, works as written. One consequence: with `defaultDeny.egress`, pods cannot reach the API
server, through the `kubernetes` service as well, because its endpoints are node IPs. A project
whose pods call the API needs an egress `allow` rule with the API server or node IPs in `cidrs`,
ports `6443`. Host-network pods and traffic arriving through Routes are not subject to the
EgressFirewall.

The EgressFirewall CRD (`egressfirewalls.k8s.ovn.org`) is part of OVN-Kubernetes. When a project
needs an EgressFirewall and the managed cluster lacks the CRD, the project is reported and nothing
of it renders, rather than leaving its external egress open. The prune sweep skips the kind on such
a cluster.

### Rule order

OVN-Kubernetes evaluates the rules of an EgressFirewall and of an AdminNetworkPolicy in order, so a
broad `Allow` ahead of a narrower `Deny` changes what the policy does. Red Hat Advanced Cluster
Management compares lists without regard to order, even with `mustonlyhave`, so a live object with
the right rules in the wrong order would otherwise stay Compliant.

The project EgressFirewall and the project AdminNetworkPolicy therefore carry the annotation
`autoshift.io/rules-order`: the Adler-32 sum of the desired rule order. For an EgressFirewall each
rule counts with every field that makes it distinct, its type, destination, and ports with their
protocols, so two rules that differ only in ports are told apart. For an AdminNetworkPolicy a rule
counts with its direction, name, action, and peers. The annotation is added with
`metadataComplianceType: musthave`, so labels and annotations other tools set on these objects are
kept. The managed cluster reads the live object. When it holds the same rules in
a different order, the annotation becomes that sum followed by the live `resourceVersion`, a value
the stored annotation can never equal, so the object mismatches and is rewritten with the desired
list, in order; the next evaluation sets the plain sum again. Under `inform` the mismatch is
reported instead. Rules that differ in content are a mismatch already and need nothing extra. The
admin network policies have at most two rules each, with the same action, so their order carries no
meaning and they have no annotation.

### AdminNetworkPolicy

A NetworkPolicy can only allow, so in-cluster `deny` rules are rendered as one AdminNetworkPolicy
per project, named `autoshift-project-<project>`. It is cluster scoped, selects the project
namespace by its `autoshift.io/project: <project>` label, and has priority 50. It is rendered only
when the project or its profile has at least one `deny` rule with an in-cluster peer. As with the
other network objects, it carries the ownership labels, is exact (`mustonlyhave`), and follows the
project's mode. Any namespace that a cluster administrator labels `autoshift.io/project: <project>`
becomes a subject of that project's AdminNetworkPolicy as well, and inherits its rules, including
the `Allow` rules that override a profile `deny`.

AdminNetworkPolicy rules are evaluated before any NetworkPolicy, and within one AdminNetworkPolicy
the first rule that matches applies. The rules of each direction are, in order:

1. The project's own `deny` rules, in rule-name order. A profile rule that the project overrides by
   name counts as a project rule.
2. The project's `allow` rules with an in-cluster peer, as `Allow`, when the profile has an
   in-cluster `deny` in the same direction. The policy cannot tell at render time whether two label
   selectors overlap, so it repeats every such allow rather than miss one. This is what lets a
   project rule override a profile `deny`: its `Allow` matches first. In a direction with
   `defaultDeny`, its NetworkPolicy still renders as well.
3. The profile's `deny` rules, in rule-name order.

A project `deny` therefore beats a project `allow` for the same traffic, and a profile `deny` beats
a profile `allow`.

Peers map as follows. `sameNamespace` selects the project namespace by
`kubernetes.io/metadata.name`, `pods` selects those pods in the project namespace, `namespaces`
keeps its selector, and `dns` becomes its own rule to namespace `openshift-dns` on port 5353, TCP
and UDP. Ingress `cidrs` are not carried over: an AdminNetworkPolicy has no ingress IP peer, so an
`allow` from `cidrs` stays a NetworkPolicy `ipBlock` and does not override a profile `deny`.

An AdminNetworkPolicy matches only pods on the pod network. Host-network pods in a namespace that a
`deny` selects are not denied. For example, node-exporter in `openshift-monitoring` listens on the
node IP, port 9100, so a `deny` to the `monitoring` peer does not block traffic to it. Host-network
pods in the project namespace are not subject to the AdminNetworkPolicy either. To deny egress to
node IPs, use an egress `deny` to `cidrs`, which the EgressFirewall applies.

An AdminNetworkPolicy rule cannot express everything a NetworkPolicy can, so these are errors that
skip the project:

- A `deny` with the `router` peer. Router pods can use the host network, and an AdminNetworkPolicy
  does not match host-network pods, so the deny would silently not apply. For inbound allowlists on
  a Route, use the `haproxy.router.openshift.io/ip_whitelist` annotation.
- A named port on a rule rendered into the AdminNetworkPolicy. The named-port field takes no
  protocol, so it could match more than the rule declares. Use a port number.
- More than 100 rules in one direction, or more than 100 ports on one rule, the limits of the
  AdminNetworkPolicy API.

Every project AdminNetworkPolicy shares priority 50. The API leaves the order of two
AdminNetworkPolicies with the same priority undefined, but each one here has only the pods of its
own project namespace as its subject, so no pod is the subject of two of them and their order never
matters. OVN-Kubernetes programs every one of them and only emits a warning event about the shared
priority. That behavior was verified against the OVN-Kubernetes release-4.22 source, not on a live
cluster. OVN-Kubernetes supports priorities 0 to 99.

The AdminNetworkPolicy CRD (`adminnetworkpolicies.policy.networking.k8s.io`) is part of
OVN-Kubernetes. When a project needs an AdminNetworkPolicy and the managed cluster lacks the CRD,
the project is reported and nothing of it renders, rather than leaving its in-cluster traffic more
open than declared. The prune sweep skips the kind on such a cluster.

### Admin network policies

`projectOnboarding.defaults.adminNetworkPolicies` holds rules that no project can override, keyed by entry
name. It is empty unless defined. Each enabled entry renders one cluster-scoped AdminNetworkPolicy,
`autoshift-admin-<entry>`, once for the cluster rather than per project:

```yaml
projectOnboarding:
  defaults:
    adminNetworkPolicies:
      deny-metadata:
        appliesTo: {allProjects: true}
        direction: egress
        action: deny
        to: {cidrs: ['169.254.169.254/32']}
```

An entry has the fields of a [rule](#network-rules), `direction`, `action`, `from` or `to`, `ports`,
`enabled`, and `description`, plus `appliesTo`, which selects the namespaces it applies to. Set
exactly one of these:

| `appliesTo` | Subject |
|---|---|
| `allProjects: true` | Every onboarded project: namespaces with the `autoshift.io/project` label |
| `labels: {...}` | Onboarded projects whose namespace has all of these labels |
| `namespaceSelector: {...}` | Any namespaces, onboarded or not. A label selector with `matchLabels` and `matchExpressions` (`In`, `NotIn`, `Exists`, `DoesNotExist`), passed through as written. An empty selector selects every namespace on the cluster |

`allProjects` and `labels` match on the `autoshift.io/project` label, not on the declared projects.
They therefore also apply to the namespaces of `mode: inform` projects, and to the namespaces of
removed projects that still carry the label, since such a Namespace is kept.

> [!WARNING]
> `namespaceSelector: {}` selects every namespace on the cluster, including `openshift-*` and the
> other platform namespaces. An admin `deny` with it, for example egress to `0.0.0.0/0`, blocks
> platform traffic and breaks the cluster. Scope a `deny` with `allProjects`, `labels`, or a
> selector that excludes the platform namespaces.

Priorities run from 10 upward in entry-name order: the first entry by name gets 10, the next 11.
Every admin AdminNetworkPolicy therefore comes before the project AdminNetworkPolicies at 50,
and an admin `allow` or `deny` is final: neither a project AdminNetworkPolicy nor a NetworkPolicy
can change it. Between two admin entries, the one earlier by name wins, so name entries to set
their order. There is room for 40 entries, priorities 10 to 49. Adding or removing an entry
renumbers the entries after it by name. During a renumber, two admin AdminNetworkPolicies can
briefly share a priority until every object is updated. Other AdminNetworkPolicies on the cluster
that use priorities 10 to 49 share them with these, and the API leaves the order of two with the
same priority undefined.

Peers:

| Peer | Renders as |
|---|---|
| `namespaces: {labels: {...}}` or `{names: [...]}` | A `namespaces` peer, as for project rules |
| `router: true` | Namespaces labeled `network.openshift.io/policy-group: ingress`. Not in a `deny`, since router pods can use the host network |
| `monitoring: true` | Namespaces labeled `network.openshift.io/policy-group: monitoring` |
| `dns: true` | Egress only. Its own rule to namespace `openshift-dns` on port 5353, TCP and UDP. With other peers, the entry's `ports` apply to those peers only. With `dns` as the only peer, setting `ports` is an error |
| `cidrs: [...]` | Egress only. `networks` peers, up to 25 CIDRs each. Unlike an EgressFirewall, a `networks` peer also matches pod and service IPs inside the cluster, so `0.0.0.0/0` covers in-cluster traffic as well. A CIDR with host bits set, such as `198.51.100.7/24`, and an IPv4-mapped IPv6 range are errors |

`sameNamespace`, `pods`, and `dnsNames`, and ingress `cidrs`, have no AdminNetworkPolicy form for
an admin entry and are errors. Ports take port numbers only, and an empty `ports` list is an error.
`allProjects` and the peers `router`, `monitoring`, and `dns` take a YAML boolean, not a string. In
`namespaceSelector`, `values: null` is the same as no `values`. The host-network limits of
[AdminNetworkPolicy](#adminnetworkpolicy) apply as well: host-network pods are neither subjects nor
matched peers.

`enabled: false` removes an entry, for example in a cluster file to turn off a clusterset entry.
Removing or disabling an entry deletes its AdminNetworkPolicy only when `project-onboarding-prune`
is `'true'`. With pruning off, the stale AdminNetworkPolicy keeps enforcing at its old priority and
is reported by the inform ConfigurationPolicy. After the renumber it can share that priority with a
live entry, and their order is then undefined. When you remove admin entries, turn pruning on, or
keep the priorities stable, for example by removing only the last entry by name.

An invalid entry, such as an unknown key, a missing or ambiguous `appliesTo`, an unsupported peer, a
named port, `ports` with `dns` as the only peer, or a name that is not a DNS-1123 label, is an
error, and so are more than 40 enabled entries. Any such error stops the whole admin tier: no admin
AdminNetworkPolicy is rendered or pruned, so the live ones stay as they were and the priorities
never shift under a partial set. The errors are listed under the status key
`projectOnboarding.defaults.adminNetworkPolicies`, see [Status](#status). Projects are not affected and still
render.

The admin tier also needs a usable rendered config. When `projects` is missing, null, or not a
map, the config is broken and the status key `rendered-config` reports it: no admin
AdminNetworkPolicy is rendered or pruned, and the live ones stay as they were. An explicit
`projects: {}` is a valid empty set and keeps the tier.

An `autoshift-admin-<entry>` that already exists without `autoshift.io/managed-by:
project-onboarding` belongs to someone else. It is not changed, and it is reported under
`projectOnboarding.defaults.adminNetworkPolicies`, while the other entries still apply. To hand it
over, label it `autoshift.io/managed-by=project-onboarding`.

The admin AdminNetworkPolicies are rendered by the enforce ConfigurationPolicy only, which corrects
any drift. Under a dry run that ConfigurationPolicy is inform and reports the drift instead. They
carry `autoshift.io/managed-by: project-onboarding` but no `autoshift.io/project` label, are exact
(`mustonlyhave`), and join the [prune sweep](#pruning). When admin entries are declared and the
managed cluster lacks the AdminNetworkPolicy CRD, that is reported under the same status key.

### Profile and project rules

- Same name: the project rule is merged over the profile rule field by field, so `ports: [8080]`
  changes only the ports. Lists such as `ports` and `cidrs` are replaced whole. Peers merge by key,
  so to replace an inherited peer, set it to `false` and add the new one.
- `enabled: false` removes the inherited rule.
- A project rule with a new name is added to the profile's rules.
- An override in one project never changes the shared profile.

A `networkProfile` that `projectOnboarding.defaults.networkProfiles` does not define, a `networkProfile` that
is set but not a string (such as `false` or `0`), and a rule that cannot be rendered, are
required-input errors: the project is skipped whole and reported, because
rendering the rest could leave it more open than declared. A rule cannot be rendered when it has
a key other than `direction`, `action`, `from`, `to`, `ports`, `enabled`, and `description`, an
invalid `direction` or `action`, an unknown or malformed peer, no peers, an invalid port, protocol,
label, namespace name, or CIDR, or a name that is not a DNS-1123 label. A `deny` rule with an
in-cluster peer renders as an AdminNetworkPolicy `Deny`, with the limits listed in
[AdminNetworkPolicy](#adminnetworkpolicy). An egress `deny` to `cidrs` or `dnsNames` renders as an
EgressFirewall `Deny`. An ingress `deny` from `cidrs` cannot be expressed at all, and the error
points to Route allowlists.

## EgressIP

`egressIP` is an optional list of IP addresses. Traffic from the project's pods leaves the cluster
with one of them as its source address, so an external firewall can tell the project's traffic
apart. The policy renders the cluster-scoped EgressIP `autoshift-<project>` (`k8s.ovn.org/v1`):

```yaml
spec:
  egressIPs: ['198.51.100.50']
  namespaceSelector:
    matchLabels:
      autoshift.io/project: payments-api
```

The selector matches the project namespace through its ownership label. As with the other
project objects, the EgressIP carries the ownership labels, is exact (`mustonlyhave`), and follows
the project's mode. A project without `egressIP`, or with an empty list, gets no EgressIP.

The list is a leaf value. A lower values level replaces it whole and never adds to it. Because the
EgressIP is exact, a changed address replaces the earlier one on the cluster rather than being
added next to it. A project that drops `egressIP` has its EgressIP removed by the prune sweep, see
[Pruning](#pruning).

These are errors that skip the project whole and are reported in the [status](#status):

- `egressIP` is not a list.
- An entry is not an IPv4 address in dotted decimal form or an IPv6 address in hexadecimal groups.
  A prefix length, such as `/32`, is an error, since an EgressIP takes addresses, not ranges.
- An entry is the unspecified address, `0.0.0.0` or `::`.
- An entry is a special-purpose address that OVN-Kubernetes never assigns: loopback (`127.0.0.0/8`,
  `::1`), link-local (`169.254.0.0/16`, `fe80::/10`), multicast (a first octet of 224 to 239, or
  `ff00::/8`), `0.0.0.0/8`, `240.0.0.0/4`, or the broadcast address `255.255.255.255`.
- An entry lies inside the cluster network or the service network. The managed cluster checks this
  against the networks of its `Network` config, and nothing of the project renders. The check is
  skipped when those networks cannot be read.
- An entry is an IPv4-mapped IPv6 address in `::ffff:0:0/96`, such as `::ffff:c633:6432`.
  OVN-Kubernetes treats it as its IPv4 address, so write the IPv4 form instead. A mapped address
  with an embedded dotted IPv4 part, such as `::ffff:198.51.100.1`, is rejected as not an address.
- Two or more projects declare the same address. OVN-Kubernetes assigns an address to one EgressIP
  only, so every project that declares it is reported and skipped until all but one drop it. IPv6
  addresses are compared by value: `2001:db8::1` and `2001:db8:0:0:0:0:0:1` are the same address.
  The check covers the projects in `config.projectOnboarding.projects`, not EgressIPs created by other tools.

An address listed twice in one project is rendered once, in the spelling of its first entry.

When the managed cluster lacks the EgressIP CRD (`egressips.k8s.ovn.org`), which is part of
OVN-Kubernetes, a project that declares `egressIP` is reported and nothing of it renders. The
prune sweep skips the kind on such a cluster.

The policy does not prepare the nodes. Before you set `egressIP`:

- Label at least one node `k8s.ovn.org/egress-assignable: ""`. OVN-Kubernetes assigns egress IP
  addresses only to nodes with this label.
- Choose addresses that no other host uses, in a subnet of the labeled nodes: the subnet of their
  primary network interface or, on bare metal, of a secondary host network. OVN-Kubernetes does
  not assign an address that lies in no node subnet.

OVN-Kubernetes reports the node each address is assigned to in the EgressIP `status.items`. The
policy compares only what it renders, so a Compliant EgressIP can still be unassigned. Check the
status, for example with `oc get egressip autoshift-<project> -o yaml`.

## Structure

The projects are split across eight shards, so that the projects of one cluster fit in Policies
below the etcd size limit, see [Size limit](#size-limit). Each shard is two Policies built from the
same template, and all sixteen use the same Placement and one PlacementBinding,
`binding-policy-project-onboarding`:

| Policy | ConfigurationPolicy | Remediation | Renders |
|---|---|---|---|
| `policy-project-onboarding-<shard>` | `policy-project-onboarding-<shard>-enforce` | `${REMEDIATION}`: enforce, or inform under `autoshift.dryRun` | `manifests/projects.yaml`: the projects of the shard in effective mode `enforce`, the prune sweep with pruning on, the status ConfigMap |
| `policy-project-onboarding-<shard>-test` | `policy-project-onboarding-<shard>-test` | Always inform | `test/projects.yaml`: every project of the shard, in either mode, asserted with the objects the shard Policy applies, the admin network policies (shard 0), dropped labels with pruning off, and the errors and stale objects of its own evaluation in `project-onboarding-test-report-<shard>`. It never deletes anything |

The two `projects.yaml` files differ only in the `$want` line; keep them in sync. The enforce
ConfigurationPolicy applies the projects of its shard in effective mode `enforce`. The inform one
asserts every project of its shard: for a project in mode `inform` that is the preview, and for a
project in mode `enforce` it is the readiness check, the same objects the enforce ConfigurationPolicy
applies. A project gets one Namespace check: with pruning off and a catalog label to drop, it is the
check with the label dropped, which also reports the drop. Throughout this README, the enforce
ConfigurationPolicy of a shard is the one in `policy-project-onboarding-<shard>`, and the inform
ConfigurationPolicy is the one in `policy-project-onboarding-<shard>-test`.

The inform content is a policy of its own, as the
[developer guide](../../../docs/developer-guide.md#gating-on-cluster-state) requires: a
`remediationAction` on the root of one Policy overrides all of its children, so inform content
bundled into the enforcing Policy would start deleting what it should only report. The enforcing
Policies set `remediationAction` to the deployment `REMEDIATION`, and the `-test` Policies set
`inform`.

`policy-project-onboarding-<shard>-test` is Compliant only when every project of the shard exists
as declared, its Namespace and every object, without errors and, with pruning off, nothing would be
deleted or stripped, so depend on the `-test` Policies, not on the shard Policies, when another
policy needs the projects to be in place. It checks the objects themselves, so it is NonCompliant
while an enforce-mode Namespace is still missing.

It does not read the status ConfigMap either: under dry run nothing writes that ConfigMap. The
`-test` Policy reports what its own evaluation finds, its status entries under the same keys and each
stale object under `stale.<Kind>.<namespace>.<name>`, as the data of the ConfigMap
`project-onboarding-test-report-<shard>` in `open-cluster-management-agent-addon`. That ConfigMap
can never exist, because its key `_unsatisfiable` is in both `data` and `binaryData`, which the API
server refuses, so the assertion stays NonCompliant until the entries clear and its message names
the ConfigMap. A shard Policy reports Compliant once its objects and its status ConfigMap are written, even
when the status lists errors.

Neither Policy of a shard depends on the other:

- The shard Policy must not depend on its `-test` Policy. The `-test` Policy is NonCompliant
  exactly while the shard has errors or work to report, and it also previews the projects in
  `mode: inform`, so the shard would never run.
- The `-test` Policy does not depend on the shard Policy. A dependency would hold it Pending
  whenever the shard Policy is NonCompliant, which under dry run is whenever anything would change,
  and would hide the adoption preview and the stale reports exactly when they are wanted.

### Shards

A project belongs to shard `adler32(name) mod 8`: the Adler-32 checksum of the project key, as the
Sprig function `adler32sum` computes it in decimal, modulo 8. The mapping is stable: a project
changes shard only if its name changes. To find the shard of a project:

```bash
python3 -c 'import sys, zlib; print(zlib.adler32(sys.argv[1].encode()) % 8)' payments-api
```

All sixteen Policies render the same template. Each one reads its shard number from its own name
(`.PolicyMetadata.name`). A Policy rendering `manifests/` whose name is not
`policy-project-onboarding-0` to `-7`, or one rendering `test/` whose name is not
`policy-project-onboarding-0-test` to `-7-test`, fails hub template resolution. So
`policy-generator-config.yaml` must list exactly the sixteen Policies with the same manifests, and
the template must agree on the shard count.

Every shard reads the whole configuration, so the checks across projects see every project: an
egress IP or a namespace that two projects in different shards declare is reported on each of
them. A shard renders, reports, and prunes only its own projects:

- The objects, status entries, and size estimate of a project are in its shard only.
- An owned object belongs to the shard of its owning project, as described in
  [Pruning](#pruning): the project that declares its namespace, which can be in another shard,
  otherwise the project it belongs to: for the EgressIP `autoshift-<project>` and the
  AdminNetworkPolicy `autoshift-project-<project>`, the project in the name, and for any other
  object the project its `autoshift.io/project` label names. The managed cluster hashes that name
  the same way as the hub. An object without the label belongs to shard 0. A shard never
  prunes or reports another shard's objects.
- Admin network policies belong to no project and are rendered, reported, and pruned by shard 0
  only.
- A rendered configuration that cannot be read stops every shard, so every shard reports it.

Each shard writes its own status ConfigMap, `project-onboarding-status-<shard>`, see
[Status](#status).

## Mode and adoption

Each project is enforced by default. Set `mode: inform` on a project to have it report drift
without changing anything. A project with `adopt: true` and no `mode` starts as `inform`, so you can
review the drift first. Set `mode: enforce` to complete the adoption. A `mode` value other than
`enforce` or `inform`, such as a misspelling, `Inform`, or `false` (which YAML makes of `mode:
off`), is reported in the [status](#status) and
the project is treated as `inform`, so a typing error never skips the adoption preview.

The managed cluster checks the namespace before rendering anything for the project:

- It does not exist yet: the project is rendered. A new namespace needs no adoption.
- It carries `autoshift.io/project` naming this project: the project is rendered.
- It carries `autoshift.io/project` naming another project: nothing is rendered for this project,
  even with `adopt: true`. A namespace owned by another project is never taken over. To move a
  namespace between projects, remove the old project's ownership label first.
- It exists without `autoshift.io/project`: the project is rendered only with `adopt: true`. An
  `adopt` value other than `true` or `false` is reported and counts as `false`.

The generated names are checked too. When an object the project would apply already exists
without the label `autoshift.io/managed-by: project-onboarding`, nothing is rendered for the
project, with or without `adopt: true`. In an existing namespace that covers the RoleBindings
`autoshift-admin`, `autoshift-write` and `autoshift-read`, the ResourceQuota `autoshift-quota`, the
LimitRange `autoshift-limits`, the NetworkPolicies `autoshift-default-deny-<direction>` and
`autoshift-rule-<rule>`, and the EgressFirewall `default`. For every project, a new namespace
included, it covers the cluster-scoped AdminNetworkPolicy `autoshift-project-<project>` and EgressIP
`autoshift-<project>`, whose addresses and selector would otherwise be taken over. `adopt` takes
over the namespace, never an object someone else created in it: applying the project would replace
the rules of that EgressFirewall, and a RoleBinding whose `roleRef` differs would be deleted and
created again. An object that carries the label belongs to onboarding and keeps being updated. To
hand an existing object over, label it explicitly:

```bash
oc label egressfirewall default -n <namespace> autoshift.io/managed-by=project-onboarding
```

A blocked project is reported in the [status](#status).

Setting the deployment `REMEDIATION` to `inform` is a dry run: the enforce ConfigurationPolicy
follows it, and the inform ConfigurationPolicy is always inform, so every project is reported only.

## Pruning

Deletion is opt-in. Set `project-onboarding-prune: 'true'` in a clusterset or cluster values file to
let onboarding delete what it owns but no longer declares, and to remove dropped catalog labels. Any
other value, including the default `'false'`, only reports them. Pruning never deletes a Namespace.

Everything onboarding creates carries `autoshift.io/managed-by: project-onboarding`. On each
evaluation the managed cluster lists the owned objects of the kinds in the sweep list (RoleBinding,
ResourceQuota, LimitRange, NetworkPolicy, EgressFirewall, AdminNetworkPolicy, and EgressIP) and
picks those the current configuration no longer declares: a binding for an access level whose
group no longer resolves, the quota objects of a project that no longer sets `size`, the
NetworkPolicy of a removed or disabled rule, the EgressFirewall of a project with no external
egress rule and no egress default deny, the AdminNetworkPolicy of a project with no in-cluster
deny, the EgressIP of a project with no `egressIP`, the admin AdminNetworkPolicy of a removed or
disabled admin entry, or every owned object of a project removed from `config.projectOnboarding.projects`.

With pruning off, the default:

- Each stale object is listed in the `-test` report under `stale.<Kind>.<namespace>.<name>`, which
  shows NonCompliant without deleting anything. The object stays in place.
- A dropped label stays on the Namespace, and `autoshift.io/project-labels` lists the earlier keys
  and this run's keys together. The inform ConfigurationPolicy reports the drop with a second check
  of the Namespace that has the label removed. That check is inform and changes nothing.
- Neither is an error, so neither is listed in the status ConfigMap.

With pruning on:

- A stale object of an enforced project, or of no declared project, is deleted by the enforce
  ConfigurationPolicy, which follows the deployment `REMEDIATION`, so a dry run only reports it.
- A stale object of a project in `mode: inform` is only reported, by the inform ConfigurationPolicy.
- A dropped label is removed from the Namespace through `metadataComplianceType: mustonlyhave` on
  the live label set minus the dropped keys. Labels other tools own are kept, and
  `autoshift.io/project-labels` lists only this run's keys.

An owned object belongs to the project that declares its namespace, otherwise to the project its
`autoshift.io/project` label names, otherwise to no declared project. The AdminNetworkPolicy and
the EgressIP are cluster scoped, so they belong to the project in their name
(`autoshift-project-<project>`, `autoshift-<project>`), whatever their label says. An admin
AdminNetworkPolicy (`autoshift-admin-<entry>`) belongs to no project. The name also decides
the mode: with pruning on, an EgressIP `autoshift-<removed>` named for a removed project is deleted,
even when its label names a current project in `mode: inform`. AutoShift never renders such a
mismatch; only an edit on the cluster produces one. These safety guards apply with pruning on or
off:

- Nothing is deleted or reported for a declared project that has errors this run (for example no
  resolvable admin group, or an invalid label), while its configuration is being fixed. No label is
  removed from its Namespace either, including one that was applied before and is in error now.
- Nothing is deleted or reported in a declared namespace the project did not render, because the
  namespace is owned by another project or exists without `adopt: true`.
- An owned object outside its project's declared namespace, for example after the project's
  `namespace` changed, is a candidate only once the new namespace is ready. The project rendered it
  this run, and when this evaluation started it already existed with `autoshift.io/project` naming
  the project and held every RoleBinding `autoshift-<level>` the project declares, owned by
  onboarding, bound to the declared ClusterRole with the declared group as its only subject. The
  namespace merely existing is not enough: admission can still refuse a destination binding, and
  the old binding may be the team's only access. A move therefore takes at least two evaluations:
  one applies the new namespace's objects, a later one observes them and cleans up the old
  namespace. While it waits, the project's status says
  `waiting for destination objects in <namespace>`. Until then the old one keeps its default denies
  and RoleBindings, since it still carries the project label and the project's EgressIP and
  AdminNetworkPolicy still select it. A project moved to a namespace it cannot take keeps its
  objects in the old one.
- The same wait holds the project's cluster-scoped objects. A project that moves and drops its
  `egressIP`, or its in-cluster deny, in the same edit keeps the stale EgressIP or
  AdminNetworkPolicy while another namespace still carries its `autoshift.io/project` label and the
  new namespace is not ready, because that object still selects the old namespace. Without a move,
  the stale object is pruned as usual.
- An object of a project that is no longer declared, such as the cluster-scoped AdminNetworkPolicy
  and EgressIP, is never a candidate while a namespace that carries that project's label is
  declared by a current project. That happens after a rename, or when a new project names the
  namespace and is blocked by its old ownership label: the namespace keeps its deny rules and egress
  address until the label is resolved. Once no declared project names such a namespace, the objects
  are pruned as usual.
- A namespace declared by more than one project is never swept.
- A namespace with an empty `autoshift.io/project` value holds nothing back.
- No admin AdminNetworkPolicy is deleted or reported while the admin tier has errors.
- No sweep runs when the cluster's rendered configuration cannot be read, or when it has no
  `config.projectOnboarding.projects` map. A missing `projects` key, for example after a values restructure or a
  misplaced file, sweeps nothing and is reported in the [status](#status), as is a rendered
  configuration that cannot be read. An explicit `projects: {}` is a deliberate removal and sweeps
  every owned object.

After a project moves to another namespace, the old Namespace still carries `autoshift.io/project:
<project>`, so the project's EgressIP and AdminNetworkPolicy would keep selecting it. The release
applies to every namespace that carries the project's label and that no current project declares,
which includes a namespace someone labeled by hand, but never a platform namespace. Once the
project's new namespace is ready, as described above, and the project has no errors, such a
namespace is released: with pruning on, its `autoshift.io/project` and `autoshift.io/managed-by` labels are removed through
`metadataComplianceType: mustonlyhave`, in the ConfigurationPolicy of the project's mode, and its
other labels and its annotations are kept. With pruning off, the inform ConfigurationPolicy reports
the release without changing anything. The Namespace itself is never deleted, and an old namespace
that a current project declares is left alone.

Removing a project leaves its namespace and workloads in place. With pruning on, only its owned
objects are removed.

## Status

A project that is invalid or blocked is listed in the status ConfigMap of its
[shard](#shards), `project-onboarding-status-<shard>` in `open-cluster-management-agent-addon` on
the managed cluster, labeled `autoshift.io/project-onboarding-status: "true"`. Each shard Policy
manages its own ConfigMap the same way. To read every shard at once:

```bash
oc get configmap -n open-cluster-management-agent-addon \
  -l autoshift.io/project-onboarding-status=true -o yaml
```

A ConfigMap has one data key per failing project of its shard, whose value is that project's
errors joined by `; `:

```yaml
data:
  legacy-app: 'namespace legacy-app exists and is not owned by AutoShift; set adopt: true to take it over'
  reporting: namespace payments-api already declared by payments-api
```

| Error | Found on | Effect |
|---|---|---|
| `project entry is not a map` | hub | Project skipped |
| `project name <name> is not a valid DNS-1123 label` | hub | Project skipped |
| `namespace <ns> is not a valid DNS-1123 label`, `namespace <value> is not a string` | hub | Project skipped |
| `namespace <ns> is a platform namespace, which project onboarding never manages` | hub | Project skipped |
| `namespace "" is not a valid DNS-1123 label, so the project key <name> is used` | hub | Note only: the project key is the namespace |
| `mode <mode> is not enforce or inform` | hub | Project rendered as `inform` |
| `adopt <value> is not true or false, so it counts as false` | hub | Project rendered without adoption |
| `labels is not a map`, `annotations is not a map`, `groups is not a map` | hub | That value ignored |
| `labels entry <entry> is not a map`, `annotations entry <entry> is not a map`, `labels entry <entry>: enabled is not true or false` (or `annotations entry`) | hub | Entry skipped |
| `annotation key <key> is not a valid qualified name` | hub | Annotation skipped |
| `label <key> is set by entries <a> and <b> with different values; <a> is applied` (or `annotation`) | hub | The first entry by name applied |
| `annotation <key> is set by both <field> and annotations entry <entry>; <field> is applied` | hub | The field applied |
| `displayName is not a string`, `description is not a string`, `requester is not a string` | hub | That annotation skipped |
| `group <level> is not a string` | hub | Level gets no RoleBinding |
| `group <level> is not admin, write or read` | hub | That value ignored |
| `group <level> name "<name>" has an unresolved placeholder, so the level gets no RoleBinding`, `group <level> name is blank, so the level gets no RoleBinding` (a `write` or `read` project override) | hub | Note only: level gets no RoleBinding, the project is not in error |
| `size is not a string` | hub | Project skipped |
| `quota is set without size`, `limitRange is set without size` | hub | Value ignored, nothing of the project pruned |
| `size <size> is not defined in projectOnboarding.defaults.sizes` | hub | Project skipped |
| `projectOnboarding.defaults.sizes.<size> is not a map` | hub | Project skipped |
| `quota is not a map`, `projectOnboarding.defaults.sizes.<size>.quota is not a map` | hub | No ResourceQuota rendered |
| `quota key <key> is not a valid resource name`, `quota key <key> is not a standard ResourceQuota resource name, such as pods or requests.cpu`, `quota <key>=<value> is not a valid quantity` | hub | No ResourceQuota rendered |
| `limitRange is not a map`, `projectOnboarding.defaults.sizes.<size>.limitRange is not a map` | hub | No LimitRange rendered |
| `limitRange field <field> is not one of default, defaultRequest, max, min, maxLimitRequestRatio`, `limitRange.<field> is not a map` | hub | No LimitRange rendered |
| `limitRange.<field> key <key> is not a valid resource name`, `limitRange.<field> key <key> is not a container resource name, such as cpu or memory`, `limitRange.<field> <key>=<value> is not a valid quantity` | hub | No LimitRange rendered |
| `quota <key>=<value> is not in the canonical form the API server stores, ...` (or `limitRange.<field> <key>=<value>`) | hub | Note only: rendered as written, see [Quotas](#quotas) |
| `networkProfile is not a string`, `networkProfile <profile> is not defined in projectOnboarding.defaults.networkProfiles`, `projectOnboarding.defaults.networkProfiles.<profile> is not a map` | hub | Project skipped |
| `network is not a map`, `<where> key <key> is not defaultDeny or rules`, `<where>.rules is not a map`, `<where>.defaultDeny is not a map`, `<where>.defaultDeny.<direction> is not true or false` | hub | Project skipped |
| `rule <rule> is not a map`, `rule <rule>: <problem>`, `rule <rule> has no peers` | hub | Project skipped |
| `cannot read the cluster and service network CIDRs, so autoshift-default-deny-egress denies all egress` | managed cluster | That NetworkPolicy denies all egress; the project still renders and is not exempt from pruning |
| `rule <rule>: cidr <cidr> lies inside the cluster or service network <network>; use namespaces or pods peers for in-cluster destinations` | managed cluster | Nothing rendered for the project |
| `rule <rule>: cidr <cidr> overlaps the cluster or service network <network>, which an EgressFirewall does not match, so the deny may not apply; use namespaces or pods peers for in-cluster destinations` | managed cluster | Nothing rendered for the project |
| `rule <rule>: cannot check cidr <cidr> against the cluster and service networks, whose CIDRs cannot be read, so the deny may not apply` | managed cluster | Nothing rendered for the project |
| `rule <rule>: ingress cidr <cidr> lies inside the cluster or service network <network>; use namespaces or pods peers for in-cluster sources` | managed cluster | Nothing rendered for the project |
| `rule <rule>: cannot check ingress cidr <cidr> against the cluster and service networks, whose CIDRs cannot be read, so the allow could admit every pod in the cluster` | managed cluster | Nothing rendered for the project |
| `rule <rule>: cidr <cidr> is an IPv4-mapped IPv6 range, which OVN-Kubernetes treats as IPv4 (write the IPv4 CIDR instead)` | hub | Project skipped |
| `the EgressFirewall CRD (egressfirewalls.k8s.ovn.org) is not installed, so the external egress rules and default deny cannot be enforced` | managed cluster | Nothing rendered for the project |
| `the EgressFirewall would need <n> rules, more than the 8000 that OVN-Kubernetes allows` | managed cluster | Nothing rendered for the project |
| `rule <rule>: cannot deny the router peer: router pods can use the host network, which an AdminNetworkPolicy does not match` | hub | Project skipped |
| `rule <rule>: named port <port> cannot be used in an AdminNetworkPolicy rule; use a port number` | hub | Project skipped |
| `rule <rule>: <n> ports, more than the 100 an AdminNetworkPolicy rule allows`, `the AdminNetworkPolicy would need <n> <direction> rules, more than the 100 an AdminNetworkPolicy allows` | hub | Project skipped |
| `the AdminNetworkPolicy CRD (adminnetworkpolicies.policy.networking.k8s.io) is not installed, so the in-cluster deny rules cannot be enforced` | managed cluster | Nothing rendered for the project |
| `egressIP is not a list` | hub | Project skipped |
| `egressIP entry <entry> is not an IPv4 or IPv6 address (write it without a prefix length)` | hub | Project skipped |
| `egressIP entry <entry> is the unspecified address` | hub | Project skipped |
| `egressIP entry <entry> is an IPv4-mapped IPv6 address (write the IPv4 address instead)` | hub | Project skipped |
| `egressIP entry <entry> is a <kind> address, which OVN-Kubernetes never assigns as an egress IP` (loopback, link-local, multicast, broadcast, and the reserved ranges) | hub | Project skipped |
| `egressIP <address> lies inside the cluster or service network <network>, which OVN-Kubernetes never assigns as an egress IP` | managed cluster | Nothing rendered for the project |
| `egressIP <address> is also declared by project <project>` (each other project, comma-separated) | hub | Project skipped, on every project that declares the address |
| `the EgressIP CRD (egressips.k8s.ovn.org) is not installed, so the egress IP addresses cannot be assigned` | managed cluster | Nothing rendered for the project |
| `no resolvable admin group` | hub | Project skipped |
| `projectOnboarding.defaults.roles.<level> <problem>, so the RoleBinding autoshift-<level> is not changed`, where the problem is `is not a string`, `is longer than 253 characters`, or `"<role>" is not a valid ClusterRole name: ...` | hub | That RoleBinding not rendered, the live one kept, nothing of the project pruned |
| `label <key>=<value> is not a valid Kubernetes label` | hub | Label skipped |
| `label <key> is reserved for project ownership`, `annotation <key> is reserved for project ownership` | hub | Label or annotation skipped |
| `label <key> is reserved for the platform; list it in projectOnboarding.defaults.allowedPlatformKeys to set it`, `annotation <key> is reserved ...` | hub | Label or annotation skipped |
| `namespace <ns> already declared by <project>` | hub | Project skipped |
| `namespace <ns> is owned by project <owner>` | managed cluster | Nothing rendered for the project |
| `namespace <ns> exists and is not owned by AutoShift; set adopt: true to take it over`, or `...; adopt is not true or false, so it is not taken over` | managed cluster | Nothing rendered for the project |
| `RoleBinding autoshift-<level>, EgressFirewall default in namespace <ns> already exist and are not managed by project onboarding (...)`, one or more of the namespaced objects, or `the cluster-scoped AdminNetworkPolicy autoshift-project-<project> already exists ...` | managed cluster | Nothing rendered for the project, see [Mode and adoption](#mode-and-adoption) |
| `waiting for destination objects in <ns>: ...` | managed cluster | The old namespace keeps its objects and labels until the move completes, see [Pruning](#pruning) |

The admin network policy tier is global, so its errors are listed under one key,
`projectOnboarding.defaults.adminNetworkPolicies` in the status ConfigMap of shard 0, rather than under a
project. Any of them stops the whole
tier, see [Admin network policies](#admin-network-policies):

| Error | Found on |
|---|---|
| `projectOnboarding.defaults.adminNetworkPolicies is not a map` | hub |
| `entry <entry> is not a map`, `entry <entry>: enabled is not true or false` | hub |
| `entry <entry>: <problem>`, for example `appliesTo is not set` or `peer pods is not supported here: ...` | hub |
| `<n> enabled entries, more than the 40 that fit at priorities 10 to 49 (below the project AdminNetworkPolicies at 50)` | hub |
| `the AdminNetworkPolicy CRD (adminnetworkpolicies.policy.networking.k8s.io) is not installed, so the admin network policies cannot be enforced` | managed cluster |

Under the same key, `the AdminNetworkPolicy autoshift-admin-<entry> already exists and is not
managed by project onboarding (...)` is found on the managed cluster. It holds only that entry: the
foreign object is not changed, and the other entries still apply.

A project key that is not a valid ConfigMap key is listed under the key with each character other
than letters, digits, `-`, `_`, and `.` replaced by `_`. When two project keys map to the same
status key, the errors of both are listed under it. A message longer than 400 characters, for
example one that echoes a long value, is cut to 400 characters followed by `...`.

When a shard Policy would grow past its size limit, the key `project-onboarding.size` of that
shard's status names the projects that are not rendered, see [Size limit](#size-limit).

When the cluster's rendered configuration, `<cluster>.rendered-config`, cannot be read, or its
`config.projectOnboarding.projects` is missing, `null`, or not a map, nothing is rendered and the
status of every shard holds the key `rendered-config`, for example
`rendered-config: cannot read cluster1.rendered-config` or
`rendered-config: config.projectOnboarding.projects is not set in cluster1.rendered-config, so no project is onboarded on this cluster; set it to the projects, or to {} for none`.
The placement selects only clusters labeled `project-onboarding: 'true'`, so a missing key is a
broken configuration, never a request to onboard nothing. The status is written rather than
deleted, so every `-test` Policy stays NonCompliant while nothing can be checked. An explicit
`projects: {}` is valid: it has no projects and nothing to report.

The checks read the raw values. `projectOnboarding.defaults` or one of its sections, such as
`groups` or `sizes`, that is set to anything other than a map, for example `false`, `[]`, or `''`,
fails the hub template for every project, with the key named in the error. Only a missing key or
`null` counts as unset. A project's `labels`, `annotations`, `groups`, `quota`, and `limitRange` are
read the same way, so `labels: false` is reported as `labels is not a map` rather than read as no
labels.

The hub resolves the configuration errors and passes them to the managed cluster, which adds the
ownership checks that only it can make, then builds the status from both. The ConfigMap exists only
while there is at least one error. Stale objects and dropped labels reported with pruning off are
not errors and are not listed.

The status follows the pattern of `eso-secret-stores-status` in the External Secrets Operator
policy. The enforce ConfigurationPolicy writes the ConfigMap with `mustonlyhave` while there are
errors, and deletes it with `mustnothave` once there are none, but only while it is absent or
carries the label `autoshift.io/project-onboarding-status: "true"`. A ConfigMap of that name without
the label belongs to someone else: it is neither written nor deleted, and the collision is reported
under the key `project-onboarding.status` of the `-test` report. The same check guards shard 0's
removal of the single policy's `project-onboarding-status`. The inform ConfigurationPolicy reports
the same entries in its test report, which can never be satisfied. Errors therefore keep the `-test`
Policy of the shard NonCompliant, and it returns to Compliant on the evaluation after they are
fixed, unless something else keeps it NonCompliant: with pruning off, a stale object or a dropped
label is reported by the inform ConfigurationPolicy as well. The `-test` Policy
is NonCompliant while there are errors or while anything would be deleted or removed:

| Situation | Enforce ConfigurationPolicy (shard Policy) | Inform ConfigurationPolicy (`-test` Policy) |
|---|---|---|
| Errors, `REMEDIATION` enforce | Writes the ConfigMap, Compliant for it | NonCompliant: its report lists the errors |
| Errors, dry run (`REMEDIATION` inform) | NonCompliant: the ConfigMap is not found | NonCompliant: its report lists the errors |
| No errors | Deletes the ConfigMap (dry run: reports it if one is left over) | Compliant for it |

The project's own mode does not matter: an inform project's errors are written to the status as
well, because the ConfigMap is a report in the add-on namespace and not part of the project. Under
dry run nothing is written, so the shard Policy is NonCompliant and names the missing
`project-onboarding-status-<shard>` ConfigMap. The error text is in the `-test` Policy's report
assertion, in its compliance details, or check the values against the error table.

Errors in `config.projectOnboarding.defaults` itself, which would affect every project, fail hub template
resolution for the whole policy: `projectOnboarding.defaults`, or its `labels`, `annotations`, `groups`,
`roles`, `sizes`, `networkProfiles`, or `allowedPlatformKeys`, set to something other than a map.
A malformed `adminNetworkPolicies` is reported in the status instead, and stops only the admin
tier.

## Size limit

The hub resolves each [shard](#shards) Policy into one Policy object per managed cluster. Each
project adds its objects to the ConfigurationPolicy of its mode in its shard, and its Namespace
checks and status entry to both ConfigurationPolicies of that shard. etcd rejects an object over
1.5 MiB, and a Policy that large could no longer be written, so onboarding would stop for every
project of that shard. The hub therefore estimates the size of each resolved shard Policy as it goes
through the shard's projects in name order, and keeps it below 1,048,576 bytes (1 MiB), which leaves
room for the status that Red Hat Advanced Cluster Management adds to the Policy. The estimate counts
every project of the shard, including invalid ones, whose status entries take space too, and the
list of namespaces the other shards declare, which every shard carries for the sweep. Each
hub-resolved shard Policy carries its estimate in a comment, `project-onboarding size estimate:
<bytes> bytes, limit <bytes>`.

When the next project of a shard would take that shard's estimate over the limit, that project and
every later one of the shard in name order are not rendered. Each of them gets only the status entry
`not rendered: the policy reached its size limit, see the status key project-onboarding.size`,
without its own errors, and the key `project-onboarding.size` of the shard's status holds the
estimate for every project of the shard, the limit, the number of projects not rendered, and the
first of them. The `-test` Policy of that shard is therefore NonCompliant; the other shards are not
affected. A
project that is not rendered counts as one with errors: nothing of it is pruned or released, and its
live objects stay as they are. The projects before it still render.

The estimate is built from what each project renders: its objects, NetworkPolicy peers, label
selectors and ports, ingress `cidrs` rules, EgressFirewall entries, the data the managed cluster
checks, its labels, annotations, and status messages, and its name and namespace, which appear in
every object. A calibration test renders each of these features alone at several counts, with
pruning on and off and with names of up to 63 characters, and fails if the estimate of any shard is
ever below the measured size of that hub-resolved shard Policy, or if one is ever over the etcd
limit.

The fullest shard reaches its limit first, so the number of projects a cluster holds depends on how
the names spread over the shards. Measured with the validation suite for sequential names
(`p-0000`, `p-0001`, and so on, or 63-character names of that pattern), every project renders up to
about:

| Project shape | Name length | Pruning on | Pruning off |
|---|---|---|---|
| Groups only (three RoleBindings), the shape of most adopted projects | 6 characters | 730 | 558 |
| Groups only | 63 characters | 486 | 406 |
| Every feature: size, a strict network profile, four rules including an AdminNetworkPolicy deny and EgressFirewall entries, and an egress IP | 6 characters | 278 | 244 |
| Every feature | 63 characters | 197 | 180 |

One shard holds about 90 groups-only or 35 fully featured projects with short names, so a set of
names that happens to fall unevenly into the shards reaches the limit earlier. To see the spread,
count the shard of each project name, see [Shards](#shards).

Large rule lists count as well. One project fits about 2,000 ingress or egress CIDRs, 3,000
EgressFirewall DNS names, or 2,000 ports on a rule, and not 8,000 CIDRs.

Each project that is not rendered still adds bookkeeping to its shard, about 350 bytes with a
6-character name and about 1,100 bytes with a 63-character one. Backstops therefore fail hub
template resolution, so nothing is applied or pruned and no Policy can pass the etcd limit:

- More than 3,200 projects are declared for the cluster: `project-onboarding: <n> projects
  declared, maximum 3200 per cluster; split the projects across clusters`. Every shard fails, since
  every shard parses the whole configuration.
- The estimate of a shard, with the bookkeeping of its projects that are not rendered, passes
  1,415,577 bytes (1.35 MiB): `project-onboarding: the policy would be about <n> bytes even with
  <n> projects not rendered, ...; split the projects across clusters or shorten project names`. Only
  that shard fails. With 63-character names this happens at about 350 projects in one shard.
- The errors of `projectOnboarding.defaults.adminNetworkPolicies` alone pass that size: `project-onboarding:
  the policy would be about <n> bytes from the projectOnboarding.defaults.adminNetworkPolicies errors alone,
  ...; fix projectOnboarding.defaults.adminNetworkPolicies`. Only shard 0, which owns the admin tier, fails.
  Each admin error is cut to 400 characters, like project messages.

Names and other strings are charged at their JSON-encoded length, so characters that JSON escapes,
such as `<` and `>` in an invalid namespace, count several times.

As with a malformed `projectOnboarding.defaults`, a failed hub template leaves the live objects as they are and
keeps the Policies NonCompliant until the configuration is fixed.

At the limit, the `-test` Policy of the shard stays NonCompliant until its project set fits again. Nothing is
deleted or changed for the projects that are not rendered: their RoleBindings, network objects,
EgressIP, and namespace labels stay as they were. To hold more, split the projects across clusters,
or keep projects small.

## Prerequisites and minimums

- OpenShift 4.22 era APIs. This stage uses Namespace, RoleBinding, ResourceQuota, LimitRange,
  NetworkPolicy, and ConfigMap, which every supported release has, EgressFirewall and EgressIP
  (`k8s.ovn.org/v1`) and AdminNetworkPolicy (`policy.networking.k8s.io/v1alpha1`) from
  OVN-Kubernetes, and reads the `Network` config.
  The `router` and `monitoring` peers rely on the `network.openshift.io/policy-group` labels that
  OpenShift sets on its ingress and monitoring namespaces. A later stage uses
  ClusterUserDefinedNetwork (`v1`), verified against OpenShift 4.22.
- For `egressIP`, nodes labeled `k8s.ovn.org/egress-assignable` and addresses in their subnet, see
  [EgressIP](#egressip).
- Red Hat Advanced Cluster Management 2.16 or later is required, and 2.17 is tested. The hub
  templates use `regexSplit`, `regexFindAll`, and `slice`, which 2.15 and earlier do not expose:
  they offer a Sprig allowlist rather than the whole function map. `metadataComplianceType` is
  verified only against 2.17. Hub templates use `add`, `append`, `atoi`,
  `base64enc`, `concat`, `contains`, `deepCopy`, `default`, `dict`, `dig`, `div`, `fail`, `first`,
  `fromYaml`, `has`, `hasKey`, `hasPrefix`, `int`, `join`, `keys`, `kindIs`, `last`, `list`,
  `lookup`, `lower`, `max`, `mergeOverwrite`, `min`, `mod`, `mul`, `quote`, `regexFindAll`,
  `regexMatch`, `regexReplaceAll`, `regexSplit`, `set`, `sortAlpha`, `splitList`, `sub`, `ternary`,
  `toJson`, `trunc`, `uniq`, `unset`, and `until`, plus the template built-ins `index`, `len`,
  `printf`, and `slice`. Managed-cluster templates use `add`, `append`, `atoi`, `base64dec`,
  `concat`, `contains`, `default`, `dict`, `dig`, `div`, `first`, `fromJson`, `has`, `hasKey`,
  `hasPrefix`, `int`, `join`, `keys`, `kindIs`, `last`, `list`, `lookup`, `lower`, `min`, `mod`, `mul`,
  `adler32sum`, `regexMatch`, `set`, `sortAlpha`, `splitList`, `sub`, `ternary`, `toJson`, `unset`, and `until`,
  plus `index`, `len`, and `printf`. The validation suite
  links its own template library, so it does not prove these on an older hub.
- `metadataComplianceType` on ConfigurationPolicy object templates. The ConfigurationPolicy CRD on
  Red Hat Advanced Cluster Management 2.17 exposes it.
- The groups exist in the identity source and reach OpenShift, see [Access (RBAC)](#access-rbac).
- The namespace `open-cluster-management-agent-addon` exists on every managed cluster, including the
  hub, which Red Hat Advanced Cluster Management provides.

## Known limitations

These follow from what a ConfigurationPolicy can express, and are documented rather than fixed:

- **Namespace metadata is written from a snapshot.** An existing Namespace is rendered with
  `metadataComplianceType: mustonlyhave`, from the labels and annotations the managed cluster read
  at the start of the evaluation, its cached `lookup`. A label or annotation that anything else
  adds between that read and the write can be removed by the write. A controller that owns the key
  adds it back on its next reconcile. A key added once, by hand or by a one-shot job, is lost for
  good and has to be added again. The next evaluation reads the new state, so only a key added
  inside that window is affected. A ConfigurationPolicy has no patch of individual keys that also
  removes retired ones, so the exact write is what lets onboarding drop the catalog keys it retired.

## Limitations to check on a live cluster

The validation suite checks rendering and template resolution, not enforcement. Check these on the
cluster after the first rollout, with a throwaway project in `mode: inform`:

- `metadataComplianceType: mustonlyhave` on an existing Namespace removes exactly the dropped
  catalog keys and keeps the labels and annotations other tools own.
- With pruning off and a catalog label to drop, the inform ConfigurationPolicy carries one
  `musthave` check for the Namespace, the stripped set. Confirm that the controller reports it as
  NonCompliant without changing the Namespace.
- The status ConfigMap of each shard is written, gated, and deleted as described in
  [Status](#status), and the compliance messages name `project-onboarding-status-<shard>`.
- Each shard Policy resolves `.PolicyMetadata.name` to its own name on the hub, and the managed
  cluster's `adler32sum` routes a removed project's objects to the same shard as the hub does.
- The hub template values passed to the managed cluster, such as the project name and its `adopt`
  setting inside the namespace lookup, are substituted as expected.
- The ResourceQuota and LimitRange stay Compliant after they are created. The API server stores
  quantities in canonical form (`0.5` becomes `500m`). Confirm whether `mustonlyhave` reports a
  non-canonical value as drift. The policy lists such values as status notes, and writing them
  canonically avoids the question. LimitRange defaulting is applied by the
  policy itself, see [Quotas](#quotas).
- The NetworkPolicies stay Compliant after they are created, and `autoshift-default-deny-egress`
  carries the cluster network and service network CIDRs of the `Network` config. With it in place,
  a pod in the project can still reach external addresses and, with a `dns` rule, cluster DNS, but
  no other pod in the cluster.
- The EgressFirewall `default` is accepted by OVN-Kubernetes (its status reports the rules as
  applied) and stays Compliant. With `defaultDeny.egress`, a pod in the project reaches the external
  destinations its rules allow and nothing else, node IPs included, and the order of project and
  profile entries is kept.
- The AdminNetworkPolicy `autoshift-project-<project>` reports itself as ready in its status and
  stays Compliant. A profile `deny` blocks the traffic it names, and a project `allow` for the same
  peer still gets through. With two or more such projects, OVN-Kubernetes emits only a warning event
  about the shared priority 50 and still enforces every project AdminNetworkPolicy.
- The admin AdminNetworkPolicy `autoshift-admin-deny-metadata` reports itself as ready and stays
  Compliant, a pod in a project cannot reach `169.254.169.254`, and a project `allow` to it does not
  get through.
- A reordered EgressFirewall or AdminNetworkPolicy is rewritten in the declared order, see
  [Rule order](#rule-order).
- The EgressIP `autoshift-<project>` stays Compliant, its `status.items` names an egress-assignable
  node for each address, and traffic from a pod in the project reaches an external host with that
  source address. After a changed `egressIP`, only the new address is assigned.

## Placement

Every cluster, hub or managed, with the label `autoshift.io/project-onboarding: 'true'`. Set the
label in the values files, never directly on a cluster. All sixteen Policies share `placement.yaml`
and one PlacementBinding, `binding-policy-project-onboarding`.

## Layout

```
project-onboarding/
  kustomization.yaml            # entry point: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator: sixteen Policies, remediation, evaluation interval
  placement.yaml                # Placement predicate and tolerations
  manifests/
    projects.yaml               # object-templates-raw, $want enforce: policy-project-onboarding-<shard>
  test/
    projects.yaml               # the same template, $want inform: policy-project-onboarding-<shard>-test
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/project-onboarding

# Full validation: render, hub and spoke template resolution, label contract
make test
```
