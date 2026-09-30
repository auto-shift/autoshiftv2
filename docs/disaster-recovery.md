# Disaster recovery

Disaster recovery in Red Hat Advanced Cluster Management covers two unrelated failures, and they
need different tools:

* **Hub loss.** The hub cluster is gone. Managed clusters keep running, but you cannot see or
  govern them. Recovery means standing up a hub that knows about the same fleet. This is what the
  `acm-backup` policy does, and it is the subject of this page.
* **Workload loss.** An application and its data need to run somewhere else. That is a storage and
  placement problem, handled by Red Hat OpenShift Data Foundation Regional or Metro disaster
  recovery, and by VolSync for individual persistent volumes.

Protecting the hub does not protect application data, and replicating application data does not
help you rebuild a hub. Most fleets need both.

## Hub backup and restore

The cluster backup and restore operator runs on the hub and depends on OpenShift APIs for Data
Protection, which installs Velero and connects the hub to an object store. On a cron schedule it
writes the hub's managed clusters, applications, policies and credentials to that store as four
Velero backups.

AutoShift models this as a mode on the `autoshift.io/acm-backup` label, with the settings for each
mode under the matching key in `config.acm-backup`:

| Label value | Role |
|---|---|
| `active` | This hub writes the backups |
| `passive` | This hub is a standby, continuously restoring from the same object store |
| `false` | No hub disaster recovery on this cluster |

Both modes configure the object store and the `DataProtectionApplication`, because the standby
restores from the same bucket the active hub writes.

### Choosing where the backups live

`config.acm-backup.storage.type` selects the backend.

Use `s3` for anything real. The bucket must outlive the hub it protects, so it has to sit outside
the fleet: Amazon Simple Storage Service, or an S3-compatible store such as MinIO or Ceph RADOS
Gateway reached through `storage.endpoint`. Credentials never appear in values files. An
administrator creates the Secret on the hub and AutoShift references it:

```bash
oc create secret generic cloud-credentials -n open-cluster-management-backup \
  --from-file=cloud=./credentials-velero
```

Use `obc` only in a laboratory or sandbox. It provisions an `ObjectBucketClaim` against the local
Red Hat OpenShift Data Foundation NooBaa instance and wires Velero to it automatically, which is
convenient for rehearsing the workflow. It is not disaster recovery: a bucket that lives on the hub
disappears with the hub.

### A minimal active and passive pair

On the active hub's clusterset:

```yaml
hubClusterSets:
  hub:
    labels:
      acm-backup: 'active'
    config:
      acm-backup:
        storage:
          type: s3
          bucket: acme-acm-backups
          region: us-east-2
          configSecretRef:
            name: cloud-credentials
            key: cloud
        active:
          veleroSchedule: '0 */2 * * *'
          veleroTtl: 720h
```

On the standby hub's clusterset, the same storage block with a different mode:

```yaml
hubClusterSets:
  dr-hub:
    labels:
      acm-backup: 'passive'
    config:
      acm-backup:
        storage:
          type: s3
          bucket: acme-acm-backups
          region: us-east-2
          configSecretRef:
            name: cloud-credentials
            key: cloud
        passive:
          restoreSyncInterval: 30m
```

Quote the cron expression. A value beginning with an asterisk is a YAML alias when it is unquoted,
and the policy then fails to render.

### Only one hub may hold the schedule

Two hubs writing to one storage location put both `BackupSchedule` resources into
`BackupCollision`, and backups stop on both. AutoShift enforces this at the placement rather than
inside a template, so a standby cannot receive a `BackupSchedule` at all.

The reason it is a placement and not a template condition matters: a template that renders nothing
leaves an empty `ConfigurationPolicy`, and an empty `ConfigurationPolicy` reports Compliant. A
backup policy that reports Compliant while backing up nothing is worse than no policy.

### Promotion is deliberate

The standby's `Restore` sets `veleroManagedClustersBackupName: skip`. It stays warm by restoring
credentials and hub resources, but it never restores the managed cluster activation data, so it
never takes the fleet from a hub that is still healthy.

Promoting a standby after losing the active hub:

1. Confirm the failed hub is down and will not return with its schedule running.
2. Set `acm-backup` to `false` on the failed hub's clusterset and to `active` on the standby's.
3. Let GitOps reconcile. The standby loses its sync `Restore` and gains a `BackupSchedule`.
4. Activate the managed clusters by creating a `Restore` with
   `veleroManagedClustersBackupName: latest`, `veleroCredentialsBackupName: skip` and
   `veleroResourcesBackupName: skip`.

Step 4 is irreversible, which is why AutoShift does not trigger it from a label flip.

### AutoShift rebuilds itself from Git

AutoShift labels its own Argo CD `ApplicationSet` and `Application` resources
`velero.io/exclude-from-backup: "true"`, so a restored hub reconstructs them from the repository
rather than from a snapshot of the old hub. Keep that label on any Argo CD object you add.

### Checking that it works

```bash
oc get schedules -A | grep acm
oc get backupschedule -n open-cluster-management-backup
oc get restore -n open-cluster-management-backup
oc get backupstoragelocation -n open-cluster-management-backup
```

A healthy active hub shows four `schedule.velero.io` resources and a `BackupSchedule` in phase
`Enabled`. A healthy standby shows a `Restore` in phase `Enabled`. The inform policies
`policy-acm-backup-storage-test`, `policy-acm-backup-schedule-test` and
`policy-acm-backup-restore-test` report the same conditions through the governance dashboard.

## Related pages

* [Values reference](values-reference.md) for every `acm-backup` label and configuration key.
* [Policy behavior](policy-behavior.md) for why an empty `ConfigurationPolicy` reports Compliant.
* [Hub-of-hubs topology](hub-of-hubs.md) for fleets with more than one hub.
