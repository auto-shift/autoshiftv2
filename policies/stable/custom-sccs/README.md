# custom-sccs

Creates SecurityContextConstraints (SCCs) declared in the AutoShift values files, and grants their
use to named ServiceAccounts through RBAC. For each entry in `config.securityContextConstraints`
the policy renders:

- the SecurityContextConstraints `<name>`, with the entry's `spec` over the `nonroot-v2` posture;
- the ClusterRole `autoshift-scc-<name>`, with the `use` verb on that SCC only;
- one RoleBinding `autoshift-scc-<name>` in each namespace listed in `serviceAccounts`, binding the
  ClusterRole to the ServiceAccounts of that namespace.

Every object carries the labels `autoshift.io/managed-by: custom-sccs` and `autoshift.io/scc:
<name>`, and is enforced with `mustonlyhave`, so a list such as `allowedCapabilities` is replaced,
not appended to. The `users` and `groups` fields of the SCC are always empty.

An entry that cannot be rendered is skipped and reported by name in a status ConfigMap, and every
other entry still applies. See [Status](#status).

## Policies

| Policy | Remediation | What it does |
|---|---|---|
| `policy-custom-sccs` | `${REMEDIATION}`: enforce, or inform under `autoshift.dryRun` | Creates and corrects the declared SCCs, their ClusterRoles and RoleBindings from `manifests/sccs.yaml`, and writes the `custom-sccs-status` ConfigMap while there are errors |
| `policy-custom-sccs-test` | Always inform | From `test/sccs.yaml`: reports objects that are no longer declared and asserts that the status ConfigMap is absent. It never changes anything |

Both files are the same template and differ only in the `$want` line. The
inform-only checks are a policy of their own, as the [developer
guide](../../../docs/developer-guide.md#gating-on-cluster-state) requires: a `remediationAction` on
the root of one Policy overrides all of its children, so a check bundled into the enforcing Policy
would start changing what it should only report.

`policy-custom-sccs-test` is Compliant only when every declared SCC is applied and nothing stale is
left, so depend on it, not on `policy-custom-sccs`, when another policy needs that state.
`policy-custom-sccs` reports Compliant once its objects and its status ConfigMap are written, even
when the status lists errors.

Neither Policy depends on the other:

- `policy-custom-sccs` must not depend on `policy-custom-sccs-test`. The test policy is NonCompliant
  exactly while the enforcing policy has work to do or errors to report, so the enforcing policy
  would never run.
- `policy-custom-sccs-test` does not depend on `policy-custom-sccs`. A dependency would hold the
  test policy Pending whenever the enforcing one is NonCompliant, which under dry run is whenever
  anything would change, and would hide its reports exactly when they are wanted.

## Enable

| Label | Default | Description |
|---|---|---|
| `autoshift.io/custom-sccs` | `'false'` | `'true'` manages `config.securityContextConstraints` |

Set it in a clusterset or cluster values file, without the `autoshift.io/` prefix. Never put it on
a ManagedCluster by hand.

## Configuration

Entries live at `config.securityContextConstraints.<name>`, a map keyed by SCC name. The normal
precedence applies: cluster over clusterset over global, as a deep merge. Because entries and
ServiceAccounts are map keys, a cluster file can disable one entry or revoke one ServiceAccount
that a clusterset file declares.

```yaml
config:
  securityContextConstraints:
    example-build:
      enabled: true                 # optional, default true
      spec:                         # optional, fields not set keep the nonroot-v2 posture
        allowPrivilegeEscalation: true
        allowedCapabilities:
          - SETUID
          - SETGID
        requiredDropCapabilities:
          - KILL
          - MKNOD
      serviceAccounts:              # <namespace>/<serviceaccount>: true or false
        example-ci/builder: true
        example-ci/legacy-builder: false
```

- `enabled`: `true` (default) or `false`. A disabled entry renders nothing.
- `spec`: SCC fields, merged over the [defaults](#defaults) key by key.
- `serviceAccounts`: `<namespace>/<serviceaccount>` to `true` or `false`. Each `true`
  ServiceAccount is a subject of the RoleBinding in its namespace. `false` keeps it out. A `true`
  ServiceAccount in a platform namespace is an error, with no override, even with
  `allowPrivileged: true`: `kube-*` (including `kube-system` and `kube-public`), `openshift`,
  `openshift-*`, `default`, `open-cluster-management*`, `multicluster-engine`, and `hive`. Those
  namespaces belong to the platform, and `default/default` is the ServiceAccount of every pod in
  `default` that names none.
- `allowPrivileged`: `true` acknowledges a privileged entry. See [Safety gate](#safety-gate).

The SCC name must be a DNS-1123 label and must not be the name of a built-in OpenShift SCC, such as
`restricted-v2`, `nonroot-v2`, `anyuid`, or `privileged`.

| Key | Type | Default | Example | Invalid input |
|---|---|---|---|---|
| `<name>` | DNS-1123 label, not a built-in SCC name | required | `example-build` | Entry skipped and reported in the [status](#status) |
| `enabled` | `true` or `false` | `true` | `false` | Entry skipped and reported |
| `spec.<field>` | SCC field, see [Defaults](#defaults) | the `nonroot-v2` value | `readOnlyRootFilesystem: true` | Unknown field, wrong type, `users` or `groups`: entry skipped and reported. A privileged setting without `allowPrivileged`: see [Safety gate](#safety-gate) |
| `serviceAccounts.<namespace>/<name>` | `true` or `false` | not granted | `example-ci/builder: true` | Malformed key, a value other than a boolean, or a platform namespace: entry skipped and reported |
| `allowPrivileged` | `true` or `false` | `false` | `true` | Entry skipped and reported |

An entry with an error renders none of its objects, and every other entry still applies.

### Defaults

Fields that `spec` does not set take the values of the built-in `nonroot-v2` SCC:

| Field | Default |
|---|---|
| `allowHost*` (`DirVolumePlugin`, `IPC`, `Network`, `PID`, `Ports`) | `false` |
| `allowPrivilegeEscalation`, `allowPrivilegedContainer` | `false` |
| `readOnlyRootFilesystem` | `false` |
| `allowedCapabilities` | `[NET_BIND_SERVICE]` |
| `defaultAddCapabilities` | `[]` |
| `requiredDropCapabilities` | `[ALL]` |
| `runAsUser` | `type: MustRunAsNonRoot` |
| `seLinuxContext` | `type: MustRunAs` |
| `fsGroup`, `supplementalGroups` | `type: RunAsAny` |
| `seccompProfiles` | `[runtime/default]` |
| `volumes` | see below |
| `allowedUnsafeSysctls`, `forbiddenSysctls`, `allowedFlexVolumes` | `[]` |
| `defaultAllowPrivilegeEscalation`, `priority` | `null` |
| `userNamespaceLevel` | not rendered, see below |
| `users`, `groups` | `[]`, always |

The default `volumes` are `configMap`, `csi`, `downwardAPI`, `emptyDir`, `ephemeral`, `image`,
`persistentVolumeClaim`, `projected`, and `secret`.

The other accepted fields are `defaultAllowPrivilegeEscalation`, `allowedUnsafeSysctls`,
`forbiddenSysctls`, `allowedFlexVolumes` (a list of `driver` maps), `priority` (an integer from 0
to 2147483647), and `userNamespaceLevel` (`AllowHostLevel` or `RequirePodLevel`). A strategy field
(`runAsUser`, `seLinuxContext`, `fsGroup`, `supplementalGroups`) replaces its default whole and
must have a valid `type`. Capability, sysctl, seccomp, and volume fields must be lists of strings.

These are errors as well, with or without `allowPrivileged`, because the API server would refuse
the SCC or ignore the setting:

- `defaultAllowPrivilegeEscalation: true` without `allowPrivilegeEscalation: true`
- a `runAsUser` `uid`, `uidRangeMin`, or `uidRangeMax` that is not an integer, such as `" 1000"`
- a volume that differs from an SCC volume type only in case, such as `HostPath` or `Secret`.
  Volume types are case-sensitive.
- `seLinuxContext.seLinuxOptions` that is not a map of `user`, `role`, `type`, and `level` strings

A `priority` makes this SCC win admission over the built-in SCCs, such as `restricted-v2`, for pods
that validate against it and whose ServiceAccount, or the user who creates them, can use it.

`userNamespaceLevel` is rendered only when `spec` sets it, so the policy also applies on OpenShift
versions whose SCC schema lacks the field. Where the field exists, the API server default
`AllowHostLevel` applies when it is not set; the field and that default are verified on OpenShift
4.22. Because it is not rendered when unset, removing it from the config does not reset it on the
live SCC: set it to `AllowHostLevel` instead.

Every other field in the table is rendered on the SCC, including the empty and `null` ones, in
the form the API server returns. With `mustonlyhave`, a field that is left out would be compared
against the server default on every evaluation, and a field removed from the config would stay on
the live SCC. On the first rollout, verify:

- the policy reaches Compliant and stays Compliant, without repeated updates to the SCC;
- `oc get scc <name> -o yaml` shows the fields of the table and no others, apart from a
  server-defaulted `userNamespaceLevel` on releases that have the field;
- removing a field such as `allowedUnsafeSysctls` from the config clears it on the live SCC.

### Safety gate

These settings are rejected unless the entry sets `allowPrivileged: true`:

- `allowPrivilegedContainer: true`
- any `allowHost*` field set to `true`
- `runAsUser.type: RunAsAny` or `seLinuxContext.type: RunAsAny`
- `runAsUser.type: MustRunAs` with `uid: 0`, or `runAsUser.type: MustRunAsRange` with
  `uidRangeMin: 0`, negative values included
- `seLinuxContext.seLinuxOptions.type` set to an unconfined domain, in any case: `spc_t`,
  `unconfined_t`, `container_runtime_t`, `unconfined_service_t`, `kernel_t`, or `init_t`
- `seLinuxContext.seLinuxOptions.level` that weakens the MCS separation between containers, in any
  case: a level with no categories, such as `s0`, any range, such as `s0-s0:c1,c2` or
  `s0-s0:c0.c1023`, a level with more than two categories, such as `s0:c0.c511` or `s0:c1,c2,c3`,
  and `SystemLow` and `SystemHigh`. A level with more categories than a pod's own pair dominates
  the labels of other pods. One or two categories, such as `s0:c26,c5`, need no acknowledgment.
- `seccompProfiles` containing `*` or `unconfined`, in any case, or set to an empty list, which lets
  pods run without a seccomp profile
- `volumes` containing `*`, `hostPath`, or a volume type that mounts node or network storage
  directly, bypassing PersistentVolume access control: `awsElasticBlockStore`, `azureDisk`,
  `azureFile`, `cephFS`, `cinder`, `fc`, `flexVolume`, `flocker`, `gcePersistentDisk`, `gitRepo`,
  `glusterfs`, `iscsi`, `nfs`, `photonPersistentDisk`, `portworxVolume`, `quobyte`, `rbd`,
  `scaleIO`, `storageOS`, and `vsphere`
- `allowedCapabilities` or `defaultAddCapabilities` containing a privileged capability, with or
  without the `CAP_` prefix, in any case
- any `allowedUnsafeSysctls`

The privileged capabilities are `*`, `ALL`, `SYS_ADMIN`, `SYS_MODULE`, `SYS_PTRACE`, `SYS_RAWIO`,
`SYS_BOOT`, `SYS_TIME`, `NET_ADMIN`, `NET_RAW`, `BPF`, `PERFMON`, `DAC_READ_SEARCH`, `MAC_ADMIN`,
`MAC_OVERRIDE`, `SYSLOG`, and the capabilities that affect the host: `AUDIT_CONTROL`,
`SYS_RESOURCE`, `SYS_PACCT`, `SYS_NICE`, `LINUX_IMMUTABLE`, `SETFCAP`, `CHECKPOINT_RESTORE`,
`IPC_LOCK`, `BLOCK_SUSPEND`, `WAKE_ALARM`, and `SYS_TTY_CONFIG`. The list is the `$privCaps`
variable in both `sccs.yaml` templates.
Other capabilities, such as `SETUID`, `SETGID`, and `SYS_CHROOT`, need no acknowledgment.

An unset `uid` or `uidRangeMin` is not gated: SCC admission fills it from the
`openshift.io/sa.scc.uid-range` annotation of the namespace, not with `0`. The SELinux types and
seccomp profiles are the `$privSELinux` and `$privSeccomp` variables, and the volume types
`$privVolumes`, in both templates.

`allowPrivileged` is a deliberate acknowledgment, so that a privileged SCC is visible in review.
It is not part of the SCC.

## Status

Errors are reported in the ConfigMap `custom-sccs-status` in the
`open-cluster-management-agent-addon` namespace of the managed cluster, one data key per entry,
messages joined by `; `. The key is the entry name with invalid characters replaced by `_`, a
leading `.` replaced by `_`, an empty name written as `_`, cut to 240 characters, and a suffix such
as `-2` when two names produce the same key, so every key is a valid ConfigMap key. The ConfigMap
exists only while there are errors, and `policy-custom-sccs-test` is NonCompliant while it exists.
The same
pattern is used by `project-onboarding`.

An entry with an error renders none of its objects; objects that it created earlier stay in place.
Errors include an invalid name or field, an unknown `spec` field, `users` or `groups` in `spec`, a
`serviceAccounts` key that is not `<namespace>/<serviceaccount>`, a value that is not `true` or
`false`, a platform namespace in `serviceAccounts`, and a privileged setting without
`allowPrivileged: true`. On the managed cluster, an object of a generated name that exists without
the `autoshift.io/managed-by: custom-sccs` label is reported and never taken over. An SCC `<name>`
or ClusterRole `autoshift-scc-<name>` owned elsewhere holds the whole entry. A RoleBinding
`autoshift-scc-<name>` owned elsewhere, which any admin of that namespace can create, skips only
that RoleBinding, as a namespace that does not exist does.

A hub template `fail` is not used, because one bad entry would then block every valid entry.

```bash
oc get configmap custom-sccs-status -n open-cluster-management-agent-addon -o yaml
```

## Removal

Removal is manual. When an entry is removed or disabled, or a namespace leaves `serviceAccounts`,
its objects are not deleted. Each one is reported by `policy-custom-sccs-test` as a `mustnothave`
violation, so it stays NonCompliant until the object is removed by hand:

```bash
oc delete rolebinding autoshift-scc-<name> -n <namespace>
oc delete clusterrole autoshift-scc-<name>
oc delete scc <name>
```

Before deleting an SCC, check that no running pod uses it. The SCC that admitted a pod is recorded
in its `openshift.io/scc` annotation.

Objects of an entry that has errors are not reported, because the entry is still declared.

## Dependencies

None. See [Policies](#policies) for why the two Policies of this directory do not depend on each
other.

## Placement

Every cluster, hub or managed, with the label `autoshift.io/custom-sccs: 'true'`. Set the label
in the values files, never directly on a cluster. The Policies share `placement.yaml` and one
PlacementBinding, `binding-policy-custom-sccs`.

## Layout

```
custom-sccs/
  kustomization.yaml            # entry point: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator: policy graph, remediation, evaluation interval
  placement.yaml                # Placement predicate and tolerations
  manifests/
    sccs.yaml                   # object-templates-raw, $want enforce: policy-custom-sccs
  test/
    sccs.yaml                   # the same template, $want inform: policy-custom-sccs-test
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/custom-sccs

# Full validation: render, hub and spoke template resolution, label contract
make test
```
