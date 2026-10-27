# quay AutoShift Policy

## Overview

Installs the Red Hat Quay operator, deploys a `QuayRegistry`, and optionally provisions its
databases through CloudNativePG. Label and `config.quay` reference lives in
[docs/values-reference.md](../../../docs/values-reference.md).

Enable it with `quay: 'true'` in a clusterset or cluster values file. Everything else is optional
and the shipped defaults reproduce a single all-managed registry.

## Before you start

Everything except credentials is declared in values files. You create by hand only what carries a
secret, plus the operators Quay depends on.

| You need | When | How |
|---|---|---|
| Object storage | Always | `odf: 'true'` with `odf-multi-cloud-gateway: 'standalone'` for the object gateway only, `odf: 'true'` alone for full ODF, or bring your own S3-compatible bucket and set `components.objectstorage: false` |
| CloudNativePG | `quay-db-mode: managed` | `cloudnative-pg: 'true'` |
| cert-manager and an issuer | Only if you set `config.quay.tls` | `cert-manager: 'true'`. The policy emits the `Certificate` only when the named issuer already exists. `policy-quay-tls-issuer` reports if it does not |
| ODF | `quay-db-backups: 'true'` | Backups go to a NooBaa bucket through an ObjectBucketClaim |
| Cluster Observability Operator | `quay-dashboard: 'true'` | `coo: 'true'`, which serves the Perses dashboard |

## Getting started

### 1. Pick a database mode

`autoshift.io/quay-db-mode` decides which policies land on the cluster:

| Mode | Runs PostgreSQL | Also needs |
|---|---|---|
| `bundled` (default) | The Quay Operator | Nothing |
| `managed` | CloudNativePG, as `quay-db` and `clair-db` | `cloudnative-pg: 'true'`, and unlocks `quay-db-backups` |
| `external` | You | `DB_URI` and `clair-config.yaml`, supplied in the Secret in step 3. See [External PostgreSQL](#external-postgresql) |

For a production registry, see [High availability](#high-availability) before you choose.

Choose the mode before the registry first deploys. Changing it later moves no data and creates no
schema: the Quay Operator creates the schema with its upgrade Job, which runs at first deployment
and on a version change, not when the database changes. A registry switched to an empty database
crash-loops on a missing table, and its repositories and users stay in the old database. To change
modes on a running registry, scale Red Hat Quay down, dump the old database, restore it into the
new one, then change the label.

### 2. Decide where images are stored

The Quay Operator manages object storage by default. Managed storage needs the `ObjectBucketClaim`
API, which is provided by NooBaa or by Red Hat OpenShift Data Foundation, so the full Ceph stack is
not required.

The lighter option is the object gateway on its own:

```yaml
labels:
  odf: 'true'
  odf-multi-cloud-gateway: 'standalone'
```

That deploys a NooBaa-only `StorageCluster` with no Ceph block or file storage. Set
`odf-multi-cloud-gateway: 'standard'` for full OpenShift Data Foundation instead. The two are
entitled differently, so pick the one your subscription covers. Either satisfies Quay, because both
create `ocs-storagecluster`, which is what the readiness gate Quay depends on checks.

To use your own bucket instead, set `components.objectstorage: false` and supply
`DISTRIBUTED_STORAGE_CONFIG` in the Secret in step 3. Managed and unmanaged components are explained
in [Introduction to the Red Hat Quay Operator](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/deploying_the_red_hat_quay_operator_on_openshift_container_platform/operator-concepts).

### 3. Create the configuration Secret

Non-sensitive keys go in `config.quay.config` in your values file. Anything secret goes in a Secret
you create on the cluster, because values files live in git. AutoShift merges that Secret into the
config bundle last, so it wins over everything declared in values.

This is where object-storage credentials, `DB_URI` for `quay-db-mode: external`, and OIDC client
secrets belong.

`policy-quay-deploy` creates the `quay-enterprise` namespace, so create it yourself first if you
want the Secret in place before Quay rolls out. The policy adopts an existing namespace.

```bash
oc create namespace quay-enterprise

cat > /tmp/quay-secrets.yaml <<'EOF'
DISTRIBUTED_STORAGE_CONFIG:
  default:
    - RadosGWStorage
    - access_key: '<key>'
      secret_key: '<secret>'
      bucket_name: 'quay-registry'
      hostname: 's3.example.com'
      is_secure: true
      port: 443
      storage_path: /datastorage/registry
DISTRIBUTED_STORAGE_PREFERENCE:
  - default
EOF

oc create secret generic quay-config-extra -n quay-enterprise \
  --from-file=config.yaml=/tmp/quay-secrets.yaml
rm -f /tmp/quay-secrets.yaml
```

Then point at it from your values file:

```yaml
config:
  quay:
    configSecretRef:
      name: quay-config-extra
      namespace: quay-enterprise
      key: config.yaml
```

The file is a `config.yaml` fragment, not a whole configuration. Field names and accepted values are
in [Required configuration fields](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-fields-required-intro).

### 4. Enable Quay

```yaml
labels:
  quay: 'true'
  # only when quay-db-mode is managed
  cloudnative-pg: 'true'
  quay-db-mode: 'managed'
```

Everything else is optional, and the shipped defaults produce a single all-managed registry.

### 5. Watch it come up

```bash
oc get policy -n policies-<release> | grep -E 'quay|cnpg'
oc get quayregistry registry -n quay-enterprise
oc get pods -n quay-enterprise
oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}{"\n"}'
```

`policy-quay-test` stays NonCompliant until the `QuayRegistry` reports available with every
component created, so it is the one to watch.

### 6. Sign in

No user exists yet. Creating the first one is an API-only operation in Red Hat Quay, so it happens
either through OIDC or through a one-time API call. Both paths are in [No Jobs](#no-jobs).

## High availability

The shipped defaults are not highly available. The registry pods scale, but the databases each run
as a single pod.

| Component | Default | For high availability |
|---|---|---|
| `quay`, `clair`, `mirror` | Scaled by the horizontal pod autoscaler the Quay Operator manages | Keep the autoscaler. To pin replica counts instead, set `overrides.<kind>.replicas` and `components.horizontalpodautoscaler: false` together |
| `postgres`, `clairpostgres` | One PostgreSQL pod each | `quay-db-mode: managed` for two CloudNativePG clusters, or `quay-db-mode: external` for databases you run |
| `redis` | One pod | Keep it. Red Hat Quay keeps only ephemeral data in Redis, so it does not need high availability. While it is down, build logs, builders, garbage collection and user events are unavailable, and the registry API keeps serving |
| `objectstorage` | An `ObjectBucketClaim` | Full OpenShift Data Foundation, or your own S3-compatible bucket with `components.objectstorage: false` |

The Quay Operator spreads the `quay`, `clair`, and `mirror` pods across nodes but not across zones,
so every replica of a component can land in one zone. Set `config.quay.zoneSpread: true` to prefer
separate zones as well:

```yaml
config:
  quay:
    zoneSpread: true
```

The preference is soft: a single-zone cluster still schedules every pod, and the autoscaler can add
more pods than there are zones. An explicit `overrides.<kind>.affinity` replaces it for that
component, and replaces the Quay Operator's node spreading too, so include a hostname term in your
own.

### Infra nodes

Red Hat Quay is an infrastructure workload, so it can run on infra nodes, which need no additional
OpenShift subscription. Set `config.quay.infraNodes: true` alongside the `infra-nodes` label:

```yaml
config:
  quay:
    infraNodes: true
```

AutoShift then annotates the `quay-enterprise` namespace with an `openshift.io/node-selector` for
`node-role.kubernetes.io/infra` and default tolerations for the infra taints, the method described in
[Deploying Red Hat Quay on infrastructure nodes](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/deploying_the_red_hat_quay_operator_on_openshift_container_platform/operator-deploy-infrastructure).
Every pod in the namespace follows it, including the CloudNativePG databases in `managed` mode.

- The annotations are written only while `infra-nodes` is set to a count other than `'0'` and at least
  one infra node is Ready, so pods are never pinned to nodes that do not exist.
- Pods move when they are next recreated. To move them at once, roll the deployments in
  `quay-enterprise`.
- Provision enough infra nodes, across zones, for the registry to stay available when one is drained.
- The Quay Operator itself stays where it is installed, in all namespaces mode, so the `monitoring`
  component keeps working. Red Hat's procedure installs the Operator into a single namespace on the
  infra nodes, which disables monitoring; AutoShift does not.

Turning `infraNodes` off does not move the pods back, because a policy cannot remove metadata from an
object. Remove the annotations by hand, then roll the deployments:

```bash
oc annotate namespace quay-enterprise openshift.io/node-selector- scheduler.alpha.kubernetes.io/defaultTolerations-
```

## Production settings

Keys in its `config.yaml` set how Red Hat Quay behaves. Put the non-sensitive ones in
`config.quay.config`; anything holding a credential goes in the Secret from step 3. The Quay
Operator reads the config bundle on every reconcile, so a change reaches the registry within about a
minute and restarts the Red Hat Quay pods one at a time.

The following clusterset or cluster values are a starting point for a production registry. Every
value is a choice, not a default: read the linked Red Hat documentation before you copy one.

```yaml
config:
  quay:
    zoneSpread: true
    overrides:
      clair:
        resources:
          requests:
            cpu: '2'
            memory: 4Gi
          limits:
            cpu: '4'
            memory: 16Gi
    config:
      # Time machine: how long a deleted or expired tag stays restorable before garbage collection
      # reclaims it. DEFAULT_TAG_EXPIRATION must be one of TAG_EXPIRATION_OPTIONS.
      DEFAULT_TAG_EXPIRATION: 2w
      TAG_EXPIRATION_OPTIONS:
        - 1d
        - 1w
        - 2w
        - 4w
      # Storage quotas: report usage per organization, and reject pushes once a namespace reaches
      # the system-wide default (1 TiB here).
      FEATURE_QUOTA_MANAGEMENT: true
      FEATURE_QUOTA_NOTIFICATIONS: true
      DEFAULT_SYSTEM_REJECT_QUOTA_BYTES: 1099511627776
      # Registry-wide auto-pruning: delete tags older than 90 days in every organization.
      FEATURE_AUTO_PRUNE: true
      DEFAULT_NAMESPACE_AUTOPRUNE_POLICY:
        method: creation_date
        value: 90d
      # Move action logs older than 30 days out of the database into object storage.
      # local_us is the storage location of the Operator-managed bucket; name your own location
      # instead when components.objectstorage is false.
      FEATURE_ACTION_LOG_ROTATION: true
      ACTION_LOG_ROTATION_THRESHOLD: 30d
      ACTION_LOG_ARCHIVE_LOCATION: local_us
      ACTION_LOG_ARCHIVE_PATH: archives/actionlogs
      # Must be longer than a rolling restart of the registry pods, or quota totals are recalculated
      # mid-rollout. 1800 is the Red Hat default.
      QUOTA_TOTAL_DELAY_SECONDS: 1800
      # Session cookies only over TLS, and links generated with https.
      SESSION_COOKIE_SECURE: true
      PREFERRED_URL_SCHEME: https
      # App-specific (CLI) tokens otherwise never expire.
      APP_SPECIFIC_TOKEN_EXPIRATION: 90d
```

| Setting | Why | Red Hat documentation |
|---|---|---|
| `zoneSpread` | Keeps one zone outage from taking every replica of a component | [High availability](#high-availability) |
| Clair `resources` | Clair uses about 3 GiB at steady state and more during a vulnerability database update, more than the 2 GiB request the Quay Operator sets. The autoscaler scales on 90% of the request, so the default keeps adding Clair pods. On a busy cluster the extra pods stay Pending, and the registry reports unavailable. A `resources` override replaces requests and limits together, so state both | [Horizontal Pod Autoscaling](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/deploying_the_red_hat_quay_operator_on_openshift_container_platform/advanced-configuration) |
| `DEFAULT_TAG_EXPIRATION`, `TAG_EXPIRATION_OPTIONS` | Garbage collection is on by default and needs no setting. These control how long deleted tags stay restorable, which is also how long Red Hat Quay keeps their storage | [Garbage collection](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/manage_red_hat_quay/garbage-collection) |
| `FEATURE_QUOTA_MANAGEMENT`, `DEFAULT_SYSTEM_REJECT_QUOTA_BYTES`, `FEATURE_QUOTA_NOTIFICATIONS` | Limits registry growth. The system-wide default rejects pushes, so set it to more than your largest organization needs | [Quota management and enforcement](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/manage_red_hat_quay/red-hat-quay-quota-management-and-enforcement) |
| `FEATURE_AUTO_PRUNE`, `DEFAULT_NAMESPACE_AUTOPRUNE_POLICY` | Deletes old tags in every organization, including new ones. The registry-wide worker runs every 24 hours. Pruned tags enter the time machine first | [Auto-pruning](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/manage_red_hat_quay/red-hat-quay-namespace-auto-pruning-overview), [registry-wide policy](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/manage_red_hat_quay/creating-registry-wide-auto-pruning-policy) |
| `FEATURE_ACTION_LOG_ROTATION` and `ACTION_LOG_*` | Keeps the audit log from growing the database without limit. For a registry with many logged operations, Red Hat recommends Elasticsearch or Splunk through `LOGS_MODEL`, whose credentials go in the Secret | [Action log storage fields](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-fields-actionlog) |
| `QUOTA_TOTAL_DELAY_SECONDS` | Red Hat says it must be longer than a rolling deployment takes | [Quota management and enforcement](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/manage_red_hat_quay/red-hat-quay-quota-management-and-enforcement) |
| `SESSION_COOKIE_SECURE`, `PREFERRED_URL_SCHEME` | Red Hat recommends `SESSION_COOKIE_SECURE: true` for every installation that uses TLS, and `https` in most cases | [Component and feature configuration fields](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-fields-optional-intro) |
| `APP_SPECIFIC_TOKEN_EXPIRATION` | Without it, tokens that users create for the command line never expire | [Component and feature configuration fields](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-fields-optional-intro) |

Auto-pruning and a reject quota both delete or refuse content. Turn them on deliberately, and tell
the teams that push to the registry first. The full list of keys is in
[Component and feature configuration fields](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-fields-optional-intro).

### Clair settings

The Quay Operator always writes Clair's full configuration itself and layers a `clair-config.yaml`
from the config bundle over it. AutoShift builds that overlay from two places, in this order:

1. `config.quay.clair` in your values file, for settings that are not secret.
2. A `clair-config.yaml` key in the Secret from step 3, for connection strings and anything else
   that holds a credential. It wins over `config.quay.clair`.

In `managed` mode the CloudNativePG connection is merged last and wins over both. Connection strings,
`auth.psk.key`, notifier broker credentials and tracing passwords are credentials, so they never go
in `config.quay.clair`.

On a disconnected cluster, Clair cannot reach the vulnerability databases it updates from, and its
updaters retry without end. Turn them off and load the data with `clairctl` instead:

```yaml
config:
  quay:
    clair:
      indexer:
        airgap: true
      matcher:
        disable_updaters: true
```

Other settings you might change:

| Setting | Default | Notes |
|---|---|---|
| `matcher.period` | `6h` | How often updaters fetch new vulnerability data |
| `matcher.update_retention` | `2` | Vulnerability database versions kept. 2 is the minimum that still lets updates be compared for notifications. Raise it only if the Clair database has room |
| `updaters.sets` | the default set | For RHEL content, Red Hat recommends at least `rhel`, `rhcc`, `clair.cvss` and `osv`. Setting a list runs only those updaters |
| `indexer.layer_scan_concurrency` | `5` | Layers scanned in parallel per index request |

Large images can fill Clair's 20 GiB scratch volume and fail the Clair pods. Raise it with
`overrides.clair.volumeSize`, as in the `_example.yaml` clusterset file.

Then export the vulnerability data from a connected Clair instance and import it, as described in
[Clair in disconnected environments](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/vulnerability_reporting_with_clair_on_red_hat_quay/clair-disconnected-environments).
Repeat the import regularly, or vulnerability reports go stale.

## External PostgreSQL

Use `quay-db-mode: external` when the databases run outside the cluster, for example on a
managed cloud database service. AutoShift marks the `postgres` and `clairpostgres` components
unmanaged, and you supply both connections. Red Hat Quay and Clair each need their own database,
and neither supports PgBouncer, so connect directly.

Prepare both databases before you set the mode:

- Enable the `pg_trgm` extension on the Red Hat Quay database with `CREATE EXTENSION IF NOT EXISTS pg_trgm;`.
  The schema migration fails without it. `managed` mode creates it for you.
- Do not share either database with other workloads. Red Hat Quay and Clair open many connections
  and can exhaust the PostgreSQL connection limit.
- Back up both databases yourself, and size their storage. The Quay Operator does neither for an
  external database, and `quay-db-backups` covers only `managed` mode.

Both connections go in the Secret from step 3, because they hold credentials:

| Key | Contents |
|---|---|
| `config.yaml` | A fragment with `DB_URI` for the Red Hat Quay database, plus any other sensitive keys |
| `clair-config.yaml` | The whole Clair configuration, with a `connstring` for the Clair database on `indexer`, `matcher` and `notifier`. Required, because the Quay Operator does not start Clair against an unmanaged database without it |
| `extra_ca_cert_<name>.pem` | Optional. The certificate authority of a database served by a private CA. The Operator mounts it in the Clair pods at `/run/certs/<name>.pem` |

AutoShift merges `config.yaml` into the config bundle and copies the other two keys into it
unchanged. It copies no other key from the Secret.

```bash
cat > /tmp/quay-secrets.yaml <<'EOF'
DB_URI: postgresql://quay:<password>@quay-db.example.com:5432/quay?sslmode=require
EOF

cat > /tmp/clair-config.yaml <<'EOF'
indexer:
  connstring: host=clair-db.example.com port=5432 dbname=clair user=clair password=<password> sslmode=verify-ca sslrootcert=/run/certs/db-ca.pem
  migrations: true
matcher:
  connstring: host=clair-db.example.com port=5432 dbname=clair user=clair password=<password> sslmode=verify-ca sslrootcert=/run/certs/db-ca.pem
  migrations: true
notifier:
  connstring: host=clair-db.example.com port=5432 dbname=clair user=clair password=<password> sslmode=verify-ca sslrootcert=/run/certs/db-ca.pem
  migrations: true
metrics:
  name: prometheus
EOF

oc create secret generic quay-config-extra -n quay-enterprise \
  --from-file=config.yaml=/tmp/quay-secrets.yaml \
  --from-file=clair-config.yaml=/tmp/clair-config.yaml \
  --from-file=extra_ca_cert_db-ca.pem=./db-ca.pem
rm -f /tmp/quay-secrets.yaml /tmp/clair-config.yaml
```

Then set the mode and point at the Secret:

```yaml
labels:
  quay: 'true'
  quay-db-mode: 'external'
config:
  quay:
    configSecretRef:
      name: quay-config-extra
```

If the reference, the Secret, `DB_URI` or, with Clair enabled, `clair-config.yaml` is missing,
`policy-quay-external-db` reports NonCompliant and names what is missing. The config bundle is still
written, and the Quay Operator reports the failure on the `QuayRegistry`, which keeps
`policy-quay-test` NonCompliant. Check it with:

```bash
oc get quayregistry registry -n quay-enterprise -o jsonpath='{.status.conditions}'
```

The policy reads the Secret each time it evaluates, so a rotated password reaches the config bundle
on the next evaluation. In `managed` mode AutoShift writes both connections from CloudNativePG, and
they win over the Secret: a `DB_URI` or Clair `connstring` left over from an external setup does not
keep either component on the old database. The rest of the Secret's `clair-config.yaml` still
applies, as described in [Clair settings](#clair-settings).

AutoShift adds keys to the config bundle but never removes them. After you delete an
`extra_ca_cert_*` key from the Secret, also remove it from `quay-registry-config-bundle`.

To verify the Red Hat Quay database certificate as well, rather than only encrypting the
connection, see [Certificate-based authentication between Red Hat Quay and SQL](https://docs.redhat.com/en/documentation/red_hat_quay/3/html/securing_red_hat_quay/cert-based-auth-quay-sql).
The Clair connection strings follow [Unmanaged Clair configuration](https://docs.redhat.com/en/documentation/red_hat_quay/3/html/vulnerability_reporting_with_clair_on_red_hat_quay/unmanaged-clair-configuration).

To skip vulnerability scanning instead, set `components.clair: false` and leave
`clair-config.yaml` out.

## Day 2 operations

What the registry owner has to understand and decide after the registry is running. Every setting
here is a `config.yaml` key in `config.quay.config` unless it says otherwise.

### Garbage collection

Garbage collection is on by default and needs no configuration. It runs continuously in the
registry pods, and Red Hat Quay has no command to start a run by hand.

| Setting | Default | What it decides |
|---|---|---|
| `DEFAULT_TAG_EXPIRATION` | `2w` | The time machine: how long a deleted or expired tag stays restorable. Its storage is reclaimed only after this window |
| `TAG_EXPIRATION_OPTIONS` | | The windows users can choose. Must include `DEFAULT_TAG_EXPIRATION` |
| `FEATURE_CHANGE_TAG_EXPIRATION` | `true` | Whether users and organizations can change the window |
| `PERMANENTLY_DELETE_TAGS` | `false` | Whether users can delete a tag from the time machine early |
| `GARBAGE_COLLECTION_FREQUENCY` | `30` | Seconds between garbage collection worker runs |
| `FEATURE_GARBAGE_COLLECTION`, `FEATURE_NAMESPACE_GARBAGE_COLLECTION`, `FEATURE_REPOSITORY_GARBAGE_COLLECTION` | `true` | Leave them on. With garbage collection off, deleted tags, repositories, namespaces and their layers are never removed |

What garbage collection does not reclaim:

- Anything still inside the time machine window.
- A blob that another manifest still references.
- Blobs from a push that is still within its temporary tag grace period. These go on the next run
  that an expired tag in the same repository triggers.

Red Hat Quay does not report how much space a run frees, and quota usage drops only after garbage
collection removes a manifest, so quota totals lag deletions. Watch the `quay_gc_iterations_total`
and `quay_gc_storage_blobs_deleted_total` metrics, and the
[alerts](#alerts), to see that it is working. Garbage collection takes its lock in Redis, so it
pauses while Redis is down. See [Red Hat Quay garbage collection](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/manage_red_hat_quay/garbage-collection).

To reclaim space on purpose, shorten the window, delete or auto-prune tags, and let garbage
collection run. Auto-pruning, quotas and log rotation are in [Production settings](#production-settings).

### Settings that quietly stop something

| Setting | Effect |
|---|---|
| `DISABLE_PUSHES: true` | Also stops garbage collection, so tags deleted in the meantime keep their storage until pushes are re-enabled |
| `ROBOTS_DISALLOW: true` | Breaks repository mirroring, which needs robot accounts |
| `FEATURE_RESTRICTED_USERS: true` without `RESTRICTED_USERS_WHITELIST` | Users who are not on a team cannot do basic tasks |
| `SSL_PROTOCOLS` or `SSL_CIPHERS` alone | With the `tls` component unmanaged, setting only one turns off the cluster TLS profile inheritance for both. Set both or neither |
| `DATABASE_SECRET_KEY` changed | Encrypted fields in the database, such as mirror credentials, can no longer be read. Never change it on a running registry |

Organization mirroring, new in 3.18, does not replicate deletions: the mirror keeps every tag it ever
copied, and grows until a quota or auto-pruning limits it. Repository mirroring does remove tags
that no longer match.

### Alerts

The Quay Operator's `monitoring` component exposes the registry metrics but alerts only on pods that
restart often. AutoShift adds a `PrometheusRule`, `autoshift-quay` in `quay-enterprise`, for the
Day 2 failures that otherwise go unnoticed:

| Alert | Fires when | Threshold key | Default |
|---|---|---|---|
| `QuayGarbageCollectionStalled` | A namespace or repository garbage collection queue has held items for the threshold | `garbageCollectionStalledFor` | `6h` |
| `QuayMirrorNotSynced` | Repositories configured for repository or organization mirroring have stayed unsynced for the threshold | `mirrorUnsyncedFor` | `2h` |
| `QuaySecurityScanStalled` | Images were pushed within the threshold and none were scanned. An idle registry does not fire it | `scanStalledFor` | `1h` |

Each threshold is a Prometheus duration under `config.quay.alerts`, and `severity` sets the label on
all three:

```yaml
config:
  quay:
    alerts:
      severity: critical
      garbageCollectionStalledFor: 12h
      scanStalledFor: 4h
```

The defaults are starting points rather than Red Hat values. The rules render only while the
`monitoring` component is managed. Set `alerts.enabled: false` to remove them.

### Dashboard

Set `quay-dashboard: 'true'`, with `coo: 'true'` on the same cluster, to add a Red Hat Quay health
dashboard to the console under **Observe** > **Dashboards (Perses)**, project `quay-enterprise`. The
policy enables Perses in the Cluster Observability Operator's `monitoring` console plugin and adds
the dashboard with a datasource that reads cluster metrics:

| Row | Shows |
|---|---|
| Health | Registry and Clair pods available, Quay alerts firing, organization and repository counts |
| Traffic | Requests per second by status, p95 request latency, push throughput |
| Garbage collection | Queued namespace and repository garbage collection, iterations and blobs deleted per minute |
| Security scanning | Clair index and vulnerability-report requests per minute and their p95 time, scan queue |
| Mirroring and database | Repositories waiting to mirror, active mirror workers, database pool connections |

Every panel except the pod counts reads the metrics of the `monitoring` component, so those panels
are empty while it is unmanaged. The scan latency panels stay empty until an image has been scanned.
Setting the label back to `'false'` stops managing the dashboard but leaves it in place.

### Upgrades

- Each Quay Operator version deploys exactly one Red Hat Quay version, so the `quay-channel` label and
  `config.quay.versions` decide the registry version. Nothing on the `QuayRegistry` pins it.
- Red Hat Quay 3.18 upgrades directly from 3.15, 3.16 or 3.17. From anything older, upgrade one minor
  version at a time.
- You cannot roll back to an earlier minor version, because upgrades migrate the database schema.
  Back up before changing the channel.
- An upgrade runs the `quay-app-upgrade` and database upgrade pods first, for about 10 minutes, before
  the registry pods roll.
- With the `tls` component unmanaged, 3.18 inherits the cluster TLS security profile unless both
  `SSL_PROTOCOLS` and `SSL_CIPHERS` are set. Set both before upgrading if clients need the earlier
  protocols.

See [Upgrade Red Hat Quay](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/upgrade_red_hat_quay/operator-upgrade).

### Backup and restore

Restoring a registry needs four things. AutoShift backs up only the second, and only in `managed`
mode.

| What | Why it matters | Backed up by |
|---|---|---|
| `registry-quay-registry-managed-secret-keys` Secret | Holds `SECRET_KEY` and `DATABASE_SECRET_KEY`. Without them, a restored database cannot be decrypted | You |
| The Red Hat Quay database | Repositories, tags, users, permissions | `quay-db-backups: 'true'` in `managed` mode, otherwise you |
| Object storage | The image layers themselves | You |
| The config bundle and your values | The rest of the configuration | Git for values; you for the Secret from step 3 |

The Clair database does not need a backup. It can be recreated, at the cost of rescanning every image.

Export the keys Secret once after the first deployment, and again after any change to it, and store
it where you keep other credentials, outside the cluster:

```bash
oc get secret registry-quay-registry-managed-secret-keys -n quay-enterprise -o yaml \
  | grep -v -E '^\s+(creationTimestamp|resourceVersion|uid):' > quay-managed-secret-keys.yaml
```

Red Hat's procedure scales the `quay`, `clair` and `mirror` components to 0, with the autoscaler
unmanaged, before it dumps the database and copies the bucket, even when both are external services.
That gives a consistent copy. Read-only mode avoids the downtime but needs a manual key-pair
procedure. Both are in [Backing up and restoring Red Hat Quay on OpenShift](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/red_hat_quay_operator_features/backing-up-and-restoring-intro).

To restore onto a new cluster, apply the keys Secret to `quay-enterprise` before the registry first
deploys, restore the database and bucket, then let `policy-quay-deploy` create the `QuayRegistry`.

## Policies

| Policy | Placement | Purpose |
|---|---|---|
| `policy-quay-operator-install` | `quay` | Operator subscription |
| `policy-cnpg-quay` | `quay` + `cloudnative-pg` + `quay-db-mode: managed` | `quay-db` and `clair-db` CloudNativePG clusters |
| `policy-cnpg-quay-test` | same | Database readiness gate (inform) |
| `policy-quay-deploy` | `quay` + `quay-db-mode` not `managed` | Config bundle, certificate, `QuayRegistry` |
| `policy-quay-deploy-cnpg` | `quay` + `quay-db-mode: managed` | Same manifests, additionally gated on database readiness |
| `policy-quay-test` | `quay` | Registry readiness gate (inform) |
| `policy-quay-external-db` | `quay` + `quay-db-mode: external` | Reports a missing `configSecretRef`, Secret, `DB_URI` or `clair-config.yaml` (inform) |
| `policy-quay-tls-issuer` | `quay` | Reports when cert-manager TLS was requested but is not in effect (inform) |
| `policy-quay-configure` | `quay` | Console link, registry host ConfigMap, and the [alerts](#alerts) |
| `policy-quay-dashboard` | `quay` + `quay-dashboard` + `coo` | The Perses [dashboard](#dashboard) and the `monitoring` console plugin that serves it |
| `policy-cnpg-quay-backup` | `quay` + `quay-db-mode: managed` + `quay-db-backups` + `odf` | Scheduled backups to a NooBaa bucket |

`policy-quay-deploy` and `policy-quay-deploy-cnpg` render the same manifests under mutually
exclusive placements, so exactly one lands on any cluster. The split exists so the database
readiness dependency applies only where CloudNativePG is actually in use.

## No Jobs

This chart runs no Jobs. Earlier versions shipped a bootstrap Job that created a `quayadmin` user,
a `quaydevel` user, a `devel` organization, and an `example` repository, calling the registry API
with certificate verification disabled. That Job is gone, along with the sample content and the
`quayadmin` Secret it produced.

Creating the first user is an API-only operation in Red Hat Quay, so there are two supported paths.

**OpenID Connect (recommended).** Configure a provider in `config.quay.config` and list the
administrator user names in `config.quay.superUsers`. Superusers named in `config.yaml` before
deployment receive administrative access on first login with no bootstrap step.

**Manual bootstrap (proof of concept).** Set both flags, wait for the registry to roll out, then
call the endpoint once:

```yaml
config:
  quay:
    bootstrap:
      userInitialize: true
      xhrOnly: false
```

```bash
HOST=$(oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}')
curl -X POST "https://${HOST}/api/v1/user/initialize" \
  -H 'Content-Type: application/json' \
  --data '{"username":"quayadmin","password":"<choose-one>","email":"quayadmin@example.com","access_token":true}'
```

Store the returned `access_token` somewhere safe, then set both flags back to their defaults. The
endpoint refuses to run once any user exists, so it cannot be replayed.

## Pushing to this registry

Use a robot account rather than a human account for automation.

1. Log in to the registry route as a superuser.
2. Create or open an organization, for example `autoshift`.
3. Select **Robot Accounts**, then **Create Robot Account**, for example `release`. The resulting
   account name is `autoshift+release`.
4. Grant it **Write** on the repositories it publishes, or **Admin** on the organization if it must
   create repositories on first push.
5. Open the robot account and select **Credentials**. The panel offers a Docker login command and a
   downloadable Kubernetes Secret.

```bash
HOST=$(oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}')
podman login "$HOST" -u 'autoshift+release' -p '<robot-token>'
helm registry login "$HOST" -u 'autoshift+release' -p '<robot-token>'
```

Red Hat Quay creates repositories on first push when the account has permission to do so, but it
does not create organizations. Create the organization before the first push.

For Argo CD to pull Helm charts from this registry, create a repository Secret rather than a pull
secret:

```bash
oc create secret generic quay-charts -n openshift-gitops \
  --from-literal=type=helm \
  --from-literal=url="$HOST" \
  --from-literal=enableOCI=true \
  --from-literal=username='autoshift+release' \
  --from-literal=password='<robot-token>'
oc label secret quay-charts -n openshift-gitops argocd.argoproj.io/secret-type=repository
```

## Trusting the registry across the cluster

Pulling images from this registry on every node needs two things: credentials in the global pull
secret, and trust for the registry certificate.

**Certificate trust** is already handled when the registry is used as a mirror. The
`disconnected-mirror` policy writes `config.disconnected.mirrorRegistry.ca` into the
`autoshift-registry-ca` ConfigMap in `openshift-config` and points
`image.config.openshift.io/cluster` at it. Set `config.disconnected.mirrorRegistry.host` to the
registry route and supply the certificate authority through `ca` or `caRef`. If the registry
certificate comes from a public authority, no trust configuration is required.

**Credentials** are not managed by AutoShift. Add the robot account to the global pull secret:

```bash
HOST=$(oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}')
oc get secret/pull-secret -n openshift-config \
  --template='{{index .data ".dockerconfigjson" | base64decode}}' > /tmp/pull-secret.json
oc registry login --registry="$HOST" \
  --auth-basic='autoshift+release:<robot-token>' --to=/tmp/pull-secret.json
oc set data secret/pull-secret -n openshift-config \
  --from-file=.dockerconfigjson=/tmp/pull-secret.json
rm -f /tmp/pull-secret.json
```

The Machine Config Operator distributes the updated secret to every node. Watch the rollout and
confirm the pools settle before relying on the new credentials:

```bash
oc get mcp -w
```

> [!NOTE]
> Whether this rollout restarts nodes depends on the OpenShift version. Check the release
> documentation for your version before running it on a production cluster, and schedule it
> accordingly.

Use a robot account with read permission only for the global pull secret. The account that pushes
releases should not be the account every node authenticates with.

## Upgrading from the earlier chart

Removing a manifest from a policy does not delete the object it created, so the bootstrap Job and
its supporting resources survive an upgrade. Remove them once:

```bash
oc delete job create-admin-user -n quay-enterprise --ignore-not-found
oc delete serviceaccount create-admin-user -n quay-enterprise --ignore-not-found
oc delete role create-admin-user -n quay-enterprise --ignore-not-found
oc delete rolebinding create-admin-user -n quay-enterprise --ignore-not-found
```

The `quayadmin`, `quaydevel`, `quay-pull-secret`, and `quay-integration` Secrets that the Job
created are also left behind. Keep them if something still consumes those credentials, otherwise
delete them.

The upgrade rewrites the config bundle to the secure defaults, which disables the unauthenticated
first user endpoint. Confirm you have another way in, through OpenID Connect or an existing
account, before upgrading a registry whose only administrator was created by the old Job.

> [!IMPORTANT]
> Switching `quay-db-mode` from `bundled` to `managed` provisions empty databases. It is a
> migration, not a live cutover: Red Hat Quay will not start against an empty schema that holds no
> registry data. Move the data with PostgreSQL tooling, or make the switch on a new registry.


## Red Hat documentation

- [Deploying the Red Hat Quay Operator on OpenShift Container Platform](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/deploying_the_red_hat_quay_operator_on_openshift_container_platform/index)
- [Introduction to the Red Hat Quay Operator](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/deploying_the_red_hat_quay_operator_on_openshift_container_platform/operator-concepts), for managed compared with unmanaged components and the config bundle Secret
- [Configure Red Hat Quay](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/index)
- [Required configuration fields](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-fields-required-intro), including object storage and database fields
- [Automation configuration options](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-preconfigure-automation-intro), for `SUPER_USERS`, `FEATURE_USER_INITIALIZE` and `BROWSER_API_CALLS_XHR_ONLY`
- [Configuring OIDC for Red Hat Quay](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/manage_red_hat_quay/configuring-oidc-authentication)
- [Manage Red Hat Quay](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/manage_red_hat_quay/index), for garbage collection, quotas, auto-pruning and other Day 2 tasks
- [Vulnerability reporting with Clair on Red Hat Quay](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/vulnerability_reporting_with_clair_on_red_hat_quay/index)
