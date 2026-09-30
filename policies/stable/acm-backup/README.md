# Hub Backup and Restore Policy

Red Hat Advanced Cluster Management hub disaster recovery, from the ACM 2.17 *Business continuity*
guide. The cluster backup and restore operator runs on the hub, backed by OpenShift APIs for Data
Protection and Velero, and writes the hub's managed clusters, applications, policies and
credentials to an object store on a cron schedule. A standby hub reads the same store continuously,
so it is warm when the active hub is lost.

This policy covers **hub** loss only. It does not replicate application data: see the `odf-dr`
policy for workload failover and `volsync` for persistent volume replication.

## Mode

The `autoshift.io/acm-backup` label carries the mode, and its values are the keys of the
mode-specific blocks in `config.acm-backup`:

| Label value | Role | Gets | Reads |
|---|---|---|---|
| `active` | Writes the backups | `BackupSchedule` | `config.acm-backup.active` |
| `passive` | Standby, continuously syncing | `Restore` | `config.acm-backup.passive` |
| `false` | No hub disaster recovery | nothing | — |

Both modes get the object store and the `DataProtectionApplication`, because the standby restores
from the same bucket the active hub writes.

Mode is enforced at the **placement**, never inside a template. A template conditional that renders
nothing leaves an empty `ConfigurationPolicy`, and an empty `ConfigurationPolicy` reports
Compliant — which for a backup policy means a hub that appears healthy while backing up nothing.

## Policies

| Policy | Runs on | Description |
|--------|---------|-------------|
| `policy-acm-backup-storage` | Both modes | The `ObjectBucketClaim` (obc backend only) and the `DataProtectionApplication` that points Velero at the bucket |
| `policy-acm-backup-credentials-test` | Both modes | Inform: the credentials Secret Velero opens exists. Existence only, never contents |
| `policy-acm-backup-storage-test` | Both modes | Inform: the `DataProtectionApplication` is Reconciled and the `BackupStorageLocation` is Available |
| `policy-acm-backup-schedule` | `active` | The `BackupSchedule` |
| `policy-acm-backup-schedule-test` | `active` | Inform: the schedule phase is `Enabled` |
| `policy-acm-backup-restore` | `passive` | The continuously syncing `Restore` |
| `policy-acm-backup-restore-test` | `passive` | Inform: the restore phase is `Enabled` |

## Placement

| Placement | Criteria |
|-----------|----------|
| `placement-policy-acm-backup` | `cluster-type: 'hub'` AND `acm-backup` in (`active`, `passive`) |
| `placement-policy-acm-backup-active` | `cluster-type: 'hub'` AND `acm-backup: 'active'` |
| `placement-policy-acm-backup-passive` | `cluster-type: 'hub'` AND `acm-backup: 'passive'` |

## Dependencies

`policy-acm-backup-storage` depends on `policy-acm-mch-install`, because enabling the
`cluster-backup` component on the `MultiClusterHub` is what creates the
`open-cluster-management-backup` namespace and installs OpenShift APIs for Data Protection.
Everything else chains off the storage readiness gate, so no schedule or restore is created
against a storage location that does not work.

## Storage backends

`config.acm-backup.storage.type` picks the backend.

### `s3`

Amazon Simple Storage Service, or **any S3-compatible object store** reached through
`storage.endpoint`: NetApp StorageGRID, Pure Storage FlashBlade, Dell ECS, MinIO, Ceph RADOS
Gateway, Wasabi. Setting `endpoint` also turns on path-style addressing, which on-premises
appliances need because virtual-host style addressing requires wildcard DNS.

A bucket outside the fleet is the only correct choice for real hub recovery: backup storage has to
outlive the hub it protects.

Credentials are never in values. Create the Secret out of band:

```bash
oc create secret generic cloud-credentials -n open-cluster-management-backup \
  --from-file=cloud=./credentials-velero
```

where `credentials-velero` is a Velero credentials file:

```ini
[default]
aws_access_key_id=<key>
aws_secret_access_key=<secret>
```

Point `config.acm-backup.storage.configSecretRef` at it. Set `endpoint` for an S3-compatible store
such as MinIO or Ceph RADOS Gateway; leave it blank for AWS S3. A private endpoint's trust bundle
comes from a ConfigMap on the hub named by `caRef`.

If `bucket` is empty the policy fails with an explicit message rather than creating a
`DataProtectionApplication` with no bucket.

### `azure` and `gcp`

Azure Blob Storage and Google Cloud Storage. `azure` additionally needs
`storage.azure.resourceGroup`, `.storageAccount` and `.subscriptionId`; the policy fails loudly if
any is missing.

### Object stores this policy has never heard of

The backends above are conveniences, not a closed list. Three escape hatches keep an unforeseen
appliance configurable from values alone, with no change to this policy:

| Key | Effect |
|---|---|
| `storage.config` | Free-form map merged **last** into the Velero configuration, so it wins over everything derived |
| `storage.provider` | Overrides the Velero provider derived from `type` |
| `storage.plugins` | Overrides the derived `defaultPlugins` list |

The case that comes up most: several S3-compatible appliances reject the newer checksum headers
Velero sends, and need `checksumAlgorithm` set to an empty string.

```yaml
storage:
  type: s3
  bucket: acm-hub-backups
  region: us-east-1
  endpoint: https://storagegrid.example.com:8082
  caRef:
    name: storagegrid-ca
    namespace: open-cluster-management-backup
    key: ca-bundle.crt
  config:
    checksumAlgorithm: ''
```

### `obc`

An `ObjectBucketClaim` against the local ODF NooBaa, wired up automatically: the policy derives the
Velero credentials Secret from the claim's generated Secret and trusts the in-cluster endpoint
through the service CA.

**Lab and sandbox only.** A bucket that lives on the hub cannot survive the hub, which is the exact
failure this policy exists to cover. It is useful for exercising the workflow before a real bucket
exists.

While the claim is still binding the policy emits the claim but no `DataProtectionApplication`,
rather than one pointing at an empty bucket. `policy-acm-backup-storage-test` reports the
not-ready state.

## Nothing is allowed to fail quietly

Every input that the policy cannot do without stops it loudly rather than rendering something
harmless. An empty `ConfigurationPolicy` reports Compliant, so silence is the failure mode this
policy set is built to avoid.

| Missing or wrong | What happens |
|---|---|
| `storage.type` not one of the four | Template fails, naming the value it got |
| `storage.bucket` empty on any backend but `obc` | Template fails rather than configure Velero with no bucket |
| `storage.azure.*` incomplete | Template fails, naming the missing field |
| `caRef` set but the ConfigMap is absent or the key empty | Template fails; omitting `caCert` would leave Velero unable to verify the endpoint while the policy looked healthy |
| The credentials Secret is absent | `policy-acm-backup-credentials-test` reports it by name |
| `active.veleroSchedule` empty | Template fails; a schedule with no cron never produces a backup |
| `passive.cleanupBeforeRestore` invalid | Template fails, listing the accepted values |
| The `ObjectBucketClaim` has not bound | No `DataProtectionApplication` is written, and the claim itself keeps the policy non-empty so it cannot report Compliant having done nothing |

The remaining defaults are genuine documented fallbacks (`veleroTtl`, `restoreSyncInterval`, the
conventional Secret name and key), each with a check behind it.

## Promotion is manual, by design

The standby's `Restore` sets `veleroManagedClustersBackupName: skip`. It keeps pulling credentials
and hub resources so it stays warm, but it never restores the managed-cluster activation data, so
it never tries to take the fleet from a healthy active hub.

To promote a standby after losing the active hub:

1. Confirm the failed hub is really down and will not come back with its schedule running. Two hubs
   writing to one storage location put both `BackupSchedule` resources into `BackupCollision` and
   stop backups on both.
2. In values, set `acm-backup: 'false'` on the failed hub's clusterset and `acm-backup: 'active'`
   on the standby's.
3. Let GitOps reconcile. The standby loses its sync `Restore` and gains a `BackupSchedule`.
4. Activate the managed clusters. The policy does not do this for you: create a `Restore` with
   `veleroManagedClustersBackupName: latest` and `veleroCredentialsBackupName: skip` and
   `veleroResourcesBackupName: skip`. This is the irreversible step, which is why it is a
   deliberate human act rather than something a label flip triggers.

Setting `cleanupBeforeRestore: CleanupAll` additionally requires the
`cluster.open-cluster-management.io/restore-cleanup-all-confirmed` annotation on the `Restore`.

## AutoShift excludes itself from backups

`autoshift/templates/autoshift-app-set.yaml`, `cluster-config-configmaps.yaml` and
`cluster-labels-configmaps.yaml` all label their Argo CD objects
`velero.io/exclude-from-backup: "true"`. A restored hub therefore rebuilds AutoShift from Git
rather than from a Velero snapshot, which is what you want: the snapshot would carry the old hub's
Application state. Do not remove those labels, and add the same label to any new Argo CD object.

## Known limitation: enabling a MultiClusterHub component is not reversible from values

Enabling this policy adds `cluster-backup` to `spec.overrides.components` on the `MultiClusterHub`,
alongside `siteconfig` from the cluster provisioning flow.

`musthave` does not remove list entries, so setting `acm-backup` back to `false` leaves the
component enabled and the policy still reports Compliant. Disable it by editing the
`MultiClusterHub` directly.

This is pre-existing behaviour rather than something this policy introduced: `siteconfig` has the
same property. Making it reversible needs the read-modify-write plus `mustonlyhave` pattern that
`policy-acm-addon-tuning` already uses on `ClusterManagementAddOn`, which is a change to the most
load-bearing object on the hub and belongs in its own review.

## Verification

```bash
oc get schedules -A | grep acm
oc get backupschedule -n open-cluster-management-backup
oc get restore -n open-cluster-management-backup
oc get backupstoragelocation -n open-cluster-management-backup
```

A healthy active hub on Red Hat Advanced Cluster Management 2.17 produces five
`schedule.velero.io` resources, not the four the product documentation lists: credentials,
resources, generic resources, managed clusters, and validation policy.

A `BackupSchedule` phase of `BackupCollision` means a second hub is writing to the same storage
location and backups have stopped on both.
