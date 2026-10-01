# Virtual Machine Backup Policy

Backs up the Red Hat OpenShift Virtualization virtual machines running on a managed cluster, using
OpenShift APIs for Data Protection. Modelled on the `acm-dr-virt-*` policies in the Red Hat Advanced
Cluster Management 2.17 Virtualization guide.

This is **not** the same job as [`acm-failover`](../acm-failover/README.md). That protects hub state, so a
lost hub can be rebuilt knowing its fleet. This protects workload data on a managed cluster. Neither
covers the other, and most fleets need both.

## How a virtual machine opts in

The fleet operator defines schedules and retention in `config.vm-backup.schedules`. A virtual machine
owner chooses which one applies by labelling the `VirtualMachine`:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: my-vm
  labels:
    cluster.open-cluster-management.io/backup-vm: daily
```

An unlabelled virtual machine is never backed up. That is why `policy-vm-backup-schedule` proves the
schedules exist and run, but cannot prove any virtual machine uses them.

## Policies

| Policy | Description |
|--------|-------------|
| `policy-vm-backup-operator-install` | OpenShift APIs for Data Protection in `openshift-adp` |
| `policy-vm-backup-snapshotclass-test` | Inform: a `VolumeSnapshotClass` exists |
| `policy-vm-backup-storage` | Credentials, and the `DataProtectionApplication` |
| `policy-vm-backup-credentials-test` | Inform: the credentials Secret exists here. Existence only, never contents |
| `policy-vm-backup-storage-test` | Inform: the `DataProtectionApplication` is Reconciled and the `BackupStorageLocation` Available |
| `policy-vm-backup-schedule` | One `Schedule` per `config.vm-backup.schedules` entry |

Placement requires both `autoshift.io/vm-backup: 'true'` and `autoshift.io/virt: 'true'`. Data
protection and a schedule selecting `VirtualMachine` resources are pointless on a cluster with no
virtualization operator.

The operator is installed here because the `cluster-backup` MultiClusterHub component installs
OpenShift APIs for Data Protection on the hub only, and this policy runs on managed clusters.

## Backup method, and the prerequisite that disqualifies a cluster

CSI snapshots, optionally moved to the object store by the Data Mover.

`storage.dataMover` defaults to `true` and should stay there. A bare CSI snapshot normally lives in
the same storage system as the volume it came from, so it is lost along with that storage. The Data
Mover copies the snapshot contents into the object store, which is what makes this disaster recovery
rather than a local convenience.

File system backup and `VolumeSnapshotLocation` backups are not used. Those are *methods* rather than
descriptions of storage, so the choice is unrelated to whether a StorageClass is CSI-provisioned.

The real prerequisite is a **`VolumeSnapshotClass`**. A CSI-provisioned StorageClass is not enough on
its own: the driver must support snapshots and a `VolumeSnapshotClass` must reference it.
`policy-vm-backup-snapshotclass-test` reports this, because without it backups appear to run and
produce nothing usable.

```bash
oc get volumesnapshotclass
```

## Credentials: one Secret for the fleet

`config.vm-backup.storage.credentialsFrom` names a Secret **on the hub**, which the policy copies to
every selected cluster. One Secret serves the whole fleet.

The alternative, `configSecretRef`, expects a Secret created separately on each cluster. That is
fine for one or two clusters and does not scale: every new cluster is another manual step, and a
missing Secret is a backup that silently never succeeds.

Either way the Secret is created out of band and credentials never appear in values files.

```bash
oc create secret generic vm-backup-cloud-credentials -n policies-<release> \
  --from-file=cloud=./credentials-velero
```

## Storage

`config.vm-backup.storage` takes the same shape as `config.acm-failover.storage` — `s3` (including any
S3-compatible appliance via `endpoint`), `azure`, `gcp`, a `caRef` trust bundle, and the
`provider` / `plugins` / `config` escape hatches for an object store the policy does not know about.

It is deliberately its own instance rather than a shared value. Virtual machine backups are far
larger than hub state and usually want their own bucket, retention and credentials.

There is no `obc` backend. A claim against the cluster's own storage would not survive that cluster,
which defeats the purpose.

## Restore is a runbook, not a policy

Restoring is deliberately out of scope, for the same reason `acm-failover` does not automate promotion
and the ODF work does not own `DRPlacementControl`: it is per-virtual-machine, keyed by UID, and
operational. A policy that restores on reconcile would be a policy that overwrites a running virtual
machine.

To restore, create a Velero `Restore` referencing the backup and the virtual machine:

```bash
oc get backups.velero.io -n openshift-adp | grep vm-backup-daily
```

```yaml
apiVersion: velero.io/v1
kind: Restore
metadata:
  name: restore-my-vm
  namespace: openshift-adp
spec:
  backupName: vm-backup-daily-20260101020000
  includedNamespaces:
    - my-vm-namespace
  labelSelector:
    matchLabels:
      cluster.open-cluster-management.io/backup-vm: daily
```

Restore into a different namespace with `namespaceMapping`. Stop the virtual machine first if it is
running, because restoring over live disks is not safe.

## Nothing is allowed to fail quietly

| Missing or wrong | What happens |
|---|---|
| `storage.type` not `s3`, `azure` or `gcp` | Template fails, naming the value and pointing at `endpoint` for appliances |
| `storage.bucket` empty | Template fails rather than configure Velero with no bucket |
| `storage.azure.*` incomplete | Template fails, naming the missing field |
| `credentialsFrom` set without a name | Template fails |
| `caRef` set but the ConfigMap is absent | Template fails; a missing trust bundle would leave Velero unable to verify the endpoint |
| `schedules` empty | Template fails: a cluster opted in but nothing would ever be backed up |
| A schedule with no `name` or no `cron` | Template fails, naming the entry |
| No `VolumeSnapshotClass` | `policy-vm-backup-snapshotclass-test` reports it |
| Credentials Secret absent | `policy-vm-backup-credentials-test` names it |

## Verification

```bash
oc get dataprotectionapplication vm-backup -n openshift-adp
oc get backupstoragelocation -n openshift-adp
oc get schedules.velero.io -n openshift-adp
oc get backups.velero.io -n openshift-adp
```

A backup whose `status.phase` is `Completed` with `snapshotMoveData` enabled has its data in the
object store. `PartiallyFailed` most often means a volume had no `VolumeSnapshotClass` for its driver.
