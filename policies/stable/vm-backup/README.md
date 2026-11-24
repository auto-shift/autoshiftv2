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

An unlabelled virtual machine is never backed up.

`policy-vm-backup-schedule` checks the `Schedule` specification only. It does not check that any
backup ran, that a backup completed, or that any virtual machine carries the label, so a schedule
whose every backup fails still reports Compliant. Check the backups by hand:

```bash
oc get backups.velero.io -n openshift-adp
```

Each backup that a schedule creates is named after it, for example `vm-backup-daily-20260101020000`,
and the `STATUS` column shows its phase.

## Policies

| Policy | Description |
|--------|-------------|
| `policy-vm-backup-snapshotclass-test` | Inform: at least one `VolumeSnapshotClass` exists, for any driver |
| `policy-vm-backup-schedule` | One `Schedule` per `config.vm-backup.schedules` entry, checked by specification only |

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
`policy-vm-backup-snapshotclass-test` reports a cluster with no `VolumeSnapshotClass` at all, because
without one backups appear to run and produce nothing usable.

The test checks existence only. It matches any `VolumeSnapshotClass`, whatever its driver, so a
cluster with a class for one CSI driver is Compliant even when the virtual machine disks use another.
Compare the two by hand: the provisioner of each storage class the virtual machine disks use must
appear as the `DRIVER` of a `VolumeSnapshotClass`.

```bash
oc get pvc -n my-vm-namespace -o custom-columns=NAME:.metadata.name,STORAGECLASS:.spec.storageClassName
oc get storageclass -o custom-columns=NAME:.metadata.name,PROVISIONER:.provisioner
oc get volumesnapshotclass -o custom-columns=NAME:.metadata.name,DRIVER:.driver
```

## Storage and credentials

Both belong to the [`oadp`](../oadp/README.md) policy, under `config.oadp.storage`. That is where the
bucket, the endpoint, the trust bundle and the credentials are set, and that policy's README covers
them.

## Restore is a runbook, not a policy

Restoring is deliberately out of scope, for the same reason `acm-failover` does not automate promotion
and the Red Hat OpenShift Data Foundation work does not own `DRPlacementControl`: it is
per-virtual-machine, keyed by UID, and operational. A policy that restores on reconcile would be a policy that overwrites a running virtual
machine.

### Why a restore cannot select one virtual machine

A Velero `Restore` filters by namespace, resource type and label. It has no filter for an object
name. The only label this policy relies on is `cluster.open-cluster-management.io/backup-vm`, and its
value is the schedule name, which every virtual machine on that schedule shares. A `Restore` that
selects `cluster.open-cluster-management.io/backup-vm: daily` therefore selects every virtual machine
on the `daily` schedule in the namespaces it includes. The label also sits on the `VirtualMachine`
only: OpenShift Virtualization does not copy it to the `DataVolume` and `PersistentVolumeClaim`
resources that hold the disks, so a label-selected restore is not a reliable way to bring the disks back either.

AutoShift adds no per-virtual-machine label, so there is no selector this policy can document that
restores exactly one virtual machine and its disks.

### Restore one virtual machine through a scratch namespace

Restore everything that backup holds for the namespace into a scratch namespace, keep the virtual
machine you need, and remove the rest.

1. Find the backup:

   ```bash
   oc get backups.velero.io -n openshift-adp | grep vm-backup-daily
   ```

2. Restore the namespace from that backup into a scratch namespace. With no `labelSelector`, the
   restore brings back each backed-up virtual machine together with its disks:

   ```yaml
   apiVersion: velero.io/v1
   kind: Restore
   metadata:
     name: restore-my-vm-namespace-scratch
     namespace: openshift-adp
   spec:
     backupName: vm-backup-daily-20260101020000
     includedNamespaces:
       - my-vm-namespace
     namespaceMapping:
       my-vm-namespace: my-vm-namespace-restore
   ```

3. Every virtual machine on that schedule in `my-vm-namespace` is now in `my-vm-namespace-restore`.
   A restored virtual machine whose run strategy starts it can come up next to the original, with the
   same MAC addresses on any secondary network. Stop or delete every virtual machine you do not need,
   and its `DataVolume` and `PersistentVolumeClaim` resources, as soon as the restore completes:

   ```bash
   oc get virtualmachines.kubevirt.io,datavolumes.cdi.kubevirt.io,pvc -n my-vm-namespace-restore
   ```

4. Run the virtual machine from the scratch namespace, or bring it back to its original namespace.
   Stop and delete the original first, because restoring over live disks is not safe. A
   `PersistentVolumeClaim` cannot move between namespaces, so clone each disk with a `DataVolume`
   whose `source.pvc` names the restored claim in `my-vm-namespace-restore`, then re-create the
   `VirtualMachine` in `my-vm-namespace` pointing at the clones.

5. Delete the scratch namespace.

When the virtual machine is the only one on that schedule in its namespace, restoring that namespace
with `includedNamespaces` alone, without `namespaceMapping`, restores just that virtual machine. Stop
and delete the original first.

## Nothing is allowed to fail quietly

| Missing or wrong | What happens |
|---|---|
| `schedules` empty | Template fails: a cluster opted in but nothing would ever be backed up |
| A schedule with no `name` or no `cron` | Template fails, naming the entry |
| Two schedules with the same `name` | Template fails, naming the duplicate, rather than applying only the first |
| No `VolumeSnapshotClass` | `policy-vm-backup-snapshotclass-test` reports it |
| A `VolumeSnapshotClass` for the wrong driver | Not detected: compare the drivers by hand, as described earlier |
| Backups that run and fail | Not detected: check `oc get backups.velero.io -n openshift-adp` |
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
