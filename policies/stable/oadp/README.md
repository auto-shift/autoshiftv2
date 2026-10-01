# Data Protection Policy

Installs OpenShift APIs for Data Protection on a managed cluster and configures it: the operator, one
`DataProtectionApplication` and one `BackupStorageLocation`, all in `openshift-adp`.

That is the whole job. This policy creates no `Backup` and no `Schedule`, so enabling it on its own
protects nothing. It makes a cluster *able* to be backed up, and what to back up is a separate
decision.

That separation is the point. An administrator protecting applications AutoShift does not manage
needs Velero installed and pointed at an object store, without AutoShift also deciding what gets
backed up and when. Enable this policy, then create Velero `Backup` or `Schedule` resources from
wherever those applications are defined.

## Why this is its own policy

OpenShift APIs for Data Protection supports the `OwnNamespace` install mode only, and permits exactly
one `DataProtectionApplication` per installation namespace. A second one reports
`only one DPA CR can exist per OADP installation namespace` and is never reconciled.

```console
$ oc get csv oadp-operator.v1.6.1 -n openshift-adp \
    -o jsonpath='{range .spec.installModes[*]}{.type}={.supported}{"\n"}{end}'
OwnNamespace=true
SingleNamespace=false
MultiNamespace=false
AllNamespaces=false
```

So every consumer on a managed cluster shares one installation rather than bringing its own, and that
shared thing needs an owner. This policy is that owner.

The namespace is fixed at `openshift-adp`, not configurable. Four files would have to agree on it, and
because the install mode is `OwnNamespace`, a disagreement produces a `DataProtectionApplication` that
no operator is watching: it simply sits there, with no error and no storage location.

## Consumers

| Policy | What it adds |
|--------|--------------|
| [`vm-backup`](../vm-backup/README.md) | Velero `Schedule` resources for virtual machines |

A consumer depends on `policy-oadp-storage-test`, so its schedules are never created against a
storage location Velero cannot reach.

Adding a consumer takes two things:

1. A dependency on `policy-oadp-storage-test`.
2. A predicate in `placement-oadp.yaml` matching the consumer's own gate label.

The second is not optional. An Advanced Cluster Management dependency resolves against the *same*
cluster's copy of the named policy, so a consumer placed on a cluster where this policy is absent
stays `Pending` forever rather than reporting anything useful.

Predicates are ORed, which is what makes this work: enabling `autoshift.io/vm-backup` places this
policy too, so a cluster that wants only virtual machine backup does not have to set a second label.

## This is not for hubs

Hub backup is [`acm-failover`](../acm-failover/README.md), and it configures a **different**
installation: the one the `cluster-backup` MultiClusterHub component creates in
`open-cluster-management-backup`, with its own `OperatorGroup` scoped to that namespace.

Because the install mode is `OwnNamespace`, neither installation can reconcile anything belonging to
the other. They are not alternatives and they do not conflict.

A self-managed hub that also runs virtual machines therefore carries both, in both namespaces. That
is supported and expected, not a misconfiguration.

| Namespace | Installed by | Configured by | Protects |
|---|---|---|---|
| `open-cluster-management-backup` | `cluster-backup` MultiClusterHub component | `acm-failover` | Hub state: managed clusters, policies, applications, credentials |
| `openshift-adp` | `policy-oadp-operator-install` | `config.oadp` | Whatever runs on this cluster |

## Policies

| Policy | Description |
|--------|-------------|
| `policy-oadp-operator-install` | OpenShift APIs for Data Protection in `openshift-adp` |
| `policy-oadp-storage` | Credentials, and the `DataProtectionApplication` |
| `policy-oadp-credentials-test` | Inform: the credentials Secret exists here. Existence only, never contents |
| `policy-oadp-storage-test` | Inform: the `DataProtectionApplication` is Reconciled and the `BackupStorageLocation` Available |

## Credentials: one Secret for the fleet

`config.oadp.storage.credentialsFrom` names a Secret **on the hub**, which the policy copies to every
selected cluster. One Secret serves the whole fleet.

The alternative, `configSecretRef`, expects a Secret created separately on each cluster. That is fine
for one or two clusters and does not scale: every new cluster is another manual step, and a missing
Secret is a backup that silently never succeeds.

Either way the Secret is created out of band and credentials never appear in values files.

```bash
oc create secret generic oadp-cloud-credentials -n policies-<release> \
  --from-file=cloud=./credentials-velero
```

The file holds a Velero credentials block, which is the same format for any S3-compatible store:

```ini
[default]
aws_access_key_id=<key>
aws_secret_access_key=<secret>
```

### Building the credentials file instead of supplying one

`storage.sourceSecretRef` names a Secret on the hub holding a plain key pair, and the policy builds
the Velero credentials file from it on every selected cluster. This is usually easier to operate than
writing the INI file by hand, and the same Secret may carry `bucketnames` and `endpoint` so the bucket
and the S3 URL come from one place:

```bash
oc create secret generic oadp-s3 -n policies-<release> \
  --from-literal=access_key_id=<key> \
  --from-literal=access_key_secret=<secret> \
  --from-literal=bucketnames=<bucket> \
  --from-literal=endpoint=https://s3.example.com
```

`storage.profile` sets the section name when the object store expects a named profile rather than
`[default]`.

**`sourceSecretRef` and `credentialsFrom` are mutually exclusive.** Both write the Secret Velero
opens, and two object templates for one object do not conflict visibly: the first wins, the second is
dead code, and the policy still reports Compliant. So setting both fails the policy instead.

Two precedence details, matching `acm-failover` rather than diverging from it: a `bucketnames` key in
the Secret overrides `storage.bucket`, while `storage.endpoint` overrides an `endpoint` key in the
Secret. The two directions are inherited and not symmetrical, so set each value in one place only.

## Storage

`config.oadp.storage` takes the same shape as `config.acm-failover.storage`: `s3` (including any
S3-compatible appliance through `endpoint`), `azure`, `gcp`, a `caRef` trust bundle, and the
`provider`, `plugins` and `config` escape hatches for an object store the policy does not know about.

It is deliberately its own instance rather than a shared value with the hub. Workload backups are far
larger than hub state and usually want their own bucket, retention and credentials.

There is no `obc` backend. A claim against the cluster's own storage would not survive that cluster,
which defeats the purpose.

### The node agent

`storage.nodeAgent` defaults to `true` and runs the Velero node agent. Leave it on. A bare CSI
snapshot normally lives in the same storage system as the volume it came from, so it is lost along
with that storage. The node agent is what moves the snapshot contents into the object store, which is
what makes this disaster recovery rather than a local convenience. It is also what file system backup
goes through.

A `Schedule` that sets `snapshotMoveData` needs that agent running, so this switch and the schedules
have to agree.

The key is named for the field it sets, `configuration.nodeAgent.enable`, and matches
`config.acm-failover.storage.nodeAgent`.

## Disconnected environments

The `oadp-*` labels follow the standard operator convention, so a disconnected mirror picks the
operator up automatically: `generate-imageset-config.sh` discovers operators from
`*-subscription-name`, and `oadp-source` gains the `mirror-catalog-suffix` when
`autoshift.io/disconnected-mirror` is `true`.

A private object store endpoint usually needs its certificate trusted. Point
`config.oadp.storage.caRef` at a ConfigMap **on the managed cluster**: the trust bundle is read
locally, next to the credentials Velero opens.

## Nothing is allowed to fail quietly

| Missing or wrong | What happens |
|---|---|
| `storage.type` not `s3`, `azure` or `gcp` | Template fails, naming the value and pointing at `endpoint` for appliances |
| `storage.bucket` empty | Template fails rather than configure Velero with no bucket |
| `storage.azure.*` incomplete | Template fails, naming the missing field |
| `credentialsFrom` set without a name | Template fails |
| `caRef` set but the ConfigMap is absent | Template fails; a missing trust bundle would leave Velero unable to verify the endpoint |
| Credentials Secret absent | `policy-oadp-credentials-test` names it |
| `sourceSecretRef` names a Secret that is absent, or has no `data` | Template fails, naming the Secret and the keys it needs |
| `sourceSecretRef` and `credentialsFrom` both set | Template fails rather than let one silently win |
| Storage location not reachable | `policy-oadp-storage-test` reports it, and every consumer stays `Pending` rather than scheduling against it |

An empty `ConfigurationPolicy` reports Compliant, so a missing input has to fail the template rather
than render nothing.

## Verification

```bash
oc get dataprotectionapplication oadp -n openshift-adp
oc get backupstoragelocation -n openshift-adp
oc get pods -n openshift-adp
```

The `DataProtectionApplication` reports `Reconciled=True` and the `BackupStorageLocation` reaches
`Available`. With `nodeAgent` enabled there is a `node-agent` pod per node.

```bash
oc get dataprotectionapplication oadp -n openshift-adp \
  -o jsonpath='{.status.conditions}'
```

That is where the real error appears when the storage location will not come up. Usual causes: the
bucket does not exist, the endpoint is unreachable from this cluster, a private endpoint's trust
bundle is missing so `storage.caRef` is needed, or the credentials do not authorise that bucket.
