# Policy behavior at runtime

AutoShift renders policies, and Red Hat Advanced Cluster Management for Kubernetes enforces them.
This page covers the second half: what the governance controllers actually do with a policy once it
reaches a cluster, which behaviors surprise people, and how to recover a stuck policy.

The validation suite in `tools/` checks that a policy renders and that its templates resolve. It
cannot check any of the behavior on this page, because that behavior only exists at runtime. A
policy that renders perfectly can still enforce the wrong thing.

## Compliance types

Every object in a `ConfigurationPolicy` carries a `complianceType` that decides how the desired
state is compared against the live object.

| Type | Behavior |
|---|---|
| `musthave` | The live object must contain the fields given. Extra fields are left alone. Lists are merged |
| `mustonlyhave` | The live object must match exactly. Anything not given is removed |
| `mustnothave` | The object, or the fields given, must not be present. Under `enforce` the controller deletes what matches, so this one destroys state |

Two consequences of `musthave` cause most of the surprises.

**`musthave` merges into lists, it never replaces them.** Changing the value of an entry in an
object list leaves both the old entry and the new one in place, and the policy still reports
Compliant. This silently defeats configuration changes that look correct in git. When the desired
state has to override an existing list entry, use `mustonlyhave`.

**`musthave` combined with `enforce` creates objects that are absent.** A template guarded with a
condition such as `(or (empty $lookup) ...)` does not skip the object when the lookup returns
nothing. It creates an empty or partly populated resource instead. Guard the object out of the
generated list entirely rather than relying on an empty value.

## Remediation

`remediationAction` is either `inform`, which reports compliance and changes nothing, or `enforce`,
which reconciles the live cluster toward the desired state.

Policies use the `${REMEDIATION}` substitution variable rather than a literal value. It resolves to
`inform` when `autoshift.dryRun` is set, and to `enforce` otherwise, so an entire deployment can be
put into report-only mode from one value.

A `remediationAction` set at the root of a policy overrides the setting on its children. An
inform-only assertion therefore cannot be bundled inside an enforcing policy. Put readiness checks
and existence assertions in the policy directory's `test/` subdirectory as their own inform policy.

## Evaluation interval

Every `ConfigurationPolicy` carries an `evaluationInterval` block with both a `compliant` and a
`noncompliant` value. The AutoShift default for both is `watch`, which is the Red Hat Advanced
Cluster Management 2.17 default and replaces polling with event-driven Kubernetes API watches.

`watch` tracks the dependencies of `lookup`, `fromConfigMap`, and `fromSecret` calls, so it is safe
for the template-heavy policies in this repository and it lowers config-policy-controller load
across a large fleet. To poll instead, override `autoshift.evaluationInterval` with a duration such
as `10m`.

> [!IMPORTANT]
> The value must be a literal, either `watch` or a Go duration. The PolicyGenerator validates it at
> build time and rejects a hub template, so a per-cluster label cannot drive it.

## Template engine limits

Hub templates are resolved by the policy propagator on the hub before a policy is distributed.
Templates in `object-templates-raw` are resolved by the config-policy-controller on the managed
cluster. Neither engine is a full Helm environment, and which functions they offer depends on the
Red Hat Advanced Cluster Management version rather than on the validation suite.

- **The available Sprig function set depends on the Red Hat Advanced Cluster Management version.**
  Version 2.15 and earlier expose an explicit allowlist that omits `trimPrefix`, `trimSuffix`,
  `compact`, and `toString`. Version 2.16 and later expose the whole Sprig function map and deny
  only `env` and `expandenv`, so those four are available. Calling a function the hub does not have
  fails hub resolution for the entire policy, not just that expression, so every `{{hub}}` stays
  raw, the wrapped policy is never created, and an operator silently never installs.
- **Write for the oldest hub the deployment targets.** On 2.15 and earlier, use `replace` rather
  than `trimPrefix` or `trimSuffix`, `ternary (list) (list $v) (empty $v)` rather than `compact`,
  and `printf "%v" $bool` rather than `toString`. Each of those works on every version, so policies
  that have to span a mixed fleet should keep using them.
- **Version skew can hide a failure.** The validation suite links its own copy of the template
  library, pinned in `tools/go.mod`, rather than the one the hub runs. When that pin is newer than
  the target hub, a function the suite resolves cleanly can still fail on the cluster.
- **`lookup` calls are restricted to the policy namespace** unless the policy service account has
  been granted wider access. A cross-namespace lookup that works when tested by hand can return
  nothing when the policy runs.
- **`lookup` returns a Go map, not a string.** A missing resource returns nil, so pipe results
  through `| default dict` before indexing into them.

The escaping rules, trim markers, and the `autoindent` requirement are covered in the
[developer guide](developer-guide.md#hub-template-pitfalls).

## Recovering a stuck policy

**A replicated policy keeps stale content.** Replacing the source of a policy does not always
replace the copy already on the managed cluster. The old `ConfigurationPolicy` keeps reporting
Compliant against content that no longer exists in git, while the new template is never enforced.
Compare the policy on the hub against the replica, then delete the replica to force re-creation:

```bash
oc get policy <namespace>.<policy-name> -n <cluster-name> -o yaml
oc delete policy <namespace>.<policy-name> -n <cluster-name>
```

**A replicated policy is stuck Pending.** If a policy stays Pending while its dependency reports
Compliant, delete the replica in the managed cluster namespace with the preceding command.

**An Operator Lifecycle Manager subscription is stuck.** Subscriptions with no cluster service
version and no state block the operator policy. Delete them and let the `OperatorPolicy` re-create
them:

```bash
oc get sub -n <namespace> -o custom-columns=NAME:.metadata.name,CSV:.status.currentCSV,STATE:.status.state
oc delete sub --all -n <namespace>
```

**Forcing re-evaluation.** Annotate the policy to trigger an immediate pass:

```bash
oc annotate policy <name> -n <namespace> \
  policy.open-cluster-management.io/trigger-update="$(date +%s)" --overwrite
```

## Scaling the governance controllers

The governance components are sized for a modest fleet by default. At several hundred managed
clusters they need attention.

**On the hub, `grc-policy-propagator`** creates the replicated policies in each cluster namespace.
It requests 64Mi and has no memory limit by default. At 400 managed clusters it was measured at
962m CPU and 496Mi of memory. There is no supported custom resource for tuning it, because the
MultiClusterHub operator owns its Helm chart.

**On each managed cluster, `config-policy-controller`** evaluates the configuration policies and
runs in the `open-cluster-management-agent-addon` namespace. Its default 512Mi limit is tight once a
cluster carries a large policy set. Concurrency and API client rates are tuned through annotations
on the `ManagedClusterAddOn`. Resource requests and limits are tuned through an
`AddOnDeploymentConfig`, which is a separate mechanism.

**`governance-policy-framework`** syncs policy status back to the hub and is tuned the same way.

AutoShift exposes both through labels, so the tuning flows through values files like everything
else. Set `autoshift.io/acm-addon-tuning: 'true'` to enable the policy, then set any of:

| Label | Applied through | Effect |
|---|---|---|
| `acm-addon-cpc-eval-concurrency` | `ManagedClusterAddOn` annotation | Concurrent policy evaluations, default 2 |
| `acm-addon-cpc-client-qps` | `ManagedClusterAddOn` annotation | Kubernetes API client queries per second, default 30 |
| `acm-addon-cpc-client-burst` | `ManagedClusterAddOn` annotation | Kubernetes API client burst, default 45 |
| `acm-addon-cpc-mem-request`, `-cpu-request`, `-mem-limit` | `AddOnDeploymentConfig` | config-policy-controller resources |
| `acm-addon-gpf-eval-concurrency`, `-client-qps`, `-client-burst` | `ManagedClusterAddOn` annotation | The same three settings for governance-policy-framework |
| `acm-addon-gpf-mem-request`, `-cpu-request`, `-mem-limit` | `AddOnDeploymentConfig` | governance-policy-framework resources |

Every label carries the `autoshift.io/` prefix in the values file. The annotation written onto the
`ManagedClusterAddOn` is `policy-evaluation-concurrency`, and it is set by the
`advanced-cluster-management` policy. Declared values are in
`autoshift/values/clustersets/_example.yaml`.
