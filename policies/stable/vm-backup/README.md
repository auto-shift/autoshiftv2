# Virtual Machine Backup Policy

Backs up the Red Hat OpenShift Virtualization virtual machines running on a managed cluster, using
OpenShift APIs for Data Protection. Modelled on the `acm-dr-virt-*` policies in the Red Hat Advanced
Cluster Management 2.17 Virtualization guide.

This policy is the Velero `Schedule` resources and nothing else. The operator, the
`DataProtectionApplication` and the backup storage location belong to [`oadp`](../oadp/README.md),
which every consumer on a managed cluster shares. Enabling `autoshift.io/vm-backup` places that policy
too, so there is no second label to set, and the storage is configured in `config.oadp.storage` rather
than here.

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
| `policy-vm-backup-snapshotclass-test` | Inform: a `VolumeSnapshotClass` exists |
| `policy-vm-backup-schedule` | One `Schedule` per `config.vm-backup.schedules` entry |

Placement requires both `autoshift.io/vm-backup: 'true'` and `autoshift.io/virt: 'true'`. Data
protection and a schedule selecting `VirtualMachine` resources are pointless on a cluster with no
virtualization operator.

`policy-vm-backup-schedule` depends on `policy-oadp-storage-test`, so a schedule is never created
against a storage location Velero cannot reach. That dependency is why `placement-oadp.yaml` carries a
predicate matching `autoshift.io/vm-backup`: an Advanced Cluster Management dependency resolves
against the same cluster's copy of the named policy, so a schedule placed where the `oadp` policy is
absent would stay `Pending` forever.

## Backup method, and the prerequisite that disqualifies a cluster

CSI snapshots, optionally moved to the object store by the Velero node agent.

`config.oadp.storage.nodeAgent` defaults to `true` and should stay there. A bare CSI snapshot normally
lives in the same storage system as the volume it came from, so it is lost along with that storage.
The node agent is what moves the snapshot contents into the object store, which is what makes this
disaster recovery rather than a local convenience. A schedule's `snapshotMoveData` follows that same
switch, because moving snapshot data is the node agent's work.

File system backup and `VolumeSnapshotLocation` backups are not used. Those are *methods* rather than
descriptions of storage, so the choice is unrelated to whether a StorageClass is CSI-provisioned.

The real prerequisite is a **`VolumeSnapshotClass`**. A CSI-provisioned StorageClass is not enough on
its own: the driver must support snapshots and a `VolumeSnapshotClass` must reference it.
`policy-vm-backup-snapshotclass-test` reports this, because without it backups appear to run and
produce nothing usable.

```bash
oc get volumesnapshotclass
```

## Storage and credentials

Both belong to the [`oadp`](../oadp/README.md) policy, under `config.oadp.storage`. That is where the
bucket, the endpoint, the trust bundle and the credentials are set, and that policy's README covers
them.

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
| `schedules` empty | Template fails: a cluster opted in but nothing would ever be backed up |
| A schedule with no `name` or no `cron` | Template fails, naming the entry |
| No `VolumeSnapshotClass` | `policy-vm-backup-snapshotclass-test` reports it |
| Storage not ready | `policy-vm-backup-schedule` stays `Pending` on its dependency rather than creating a schedule that cannot run |

Everything about the object store itself fails in the [`oadp`](../oadp/README.md) policy, which owns
it.

## Verification

```bash
oc get dataprotectionapplication oadp -n openshift-adp
oc get backupstoragelocation -n openshift-adp
oc get schedules.velero.io -n openshift-adp
oc get backups.velero.io -n openshift-adp
```

A backup whose `status.phase` is `Completed` with `snapshotMoveData` enabled has its data in the
object store. `PartiallyFailed` most often means a volume had no `VolumeSnapshotClass` for its driver.
