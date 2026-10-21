# GitLab Policy

Deploys GitLab on OpenShift with configurable backends for PostgreSQL, Redis, and object storage.

The policy installs the chart version that the installed GitLab Operator supports, so GitLab stays
current with the operator channel. The chart's major version is 9 behind GitLab's: chart 10.5.0 is
GitLab 19.5.0. See [GitLab version mappings](https://docs.gitlab.com/charts/installation/version_mappings/).

## Prerequisites

### Required Operators

| Operator | Label | Catalog | Required When |
|----------|-------|---------|---------------|
| GitLab | `gitlab: 'true'` | certified-operators | Always |
| CloudNativePG | `cloudnative-pg: 'true'` | certified-operators | db-mode: managed |
| OpenShift Data Foundation | `odf: 'true'` | redhat-operators | object-storage-mode: managed |

> **Note:** Cert-manager is not required when using edge-terminated routes (the default). OpenShift's router handles TLS termination. GitLab's `installCertmanager` is set to `false`.

### Secrets for External Mode

When using `external` mode for any service, create the corresponding secret on the target cluster before enabling.

**External PostgreSQL:**
```bash
oc create namespace gitlab-system
oc create secret generic gitlab-db-app \
  -n gitlab-system \
  --from-literal=host="my-postgres.example.com" \
  --from-literal=port="5432" \
  --from-literal=username="gitlab" \
  --from-literal=password="<password>" \
  --from-literal=dbname="gitlabhq_production"
```

**External Redis:**
```bash
oc create secret generic gitlab-redis-password \
  -n gitlab-system \
  --from-literal=redis-password="<password>"
```

Set the host via label: `gitlab-redis-host: 'my-redis.example.com'`

**External Object Storage:**
```bash
oc create secret generic gitlab-object-storage-connection \
  -n gitlab-system \
  --from-literal=connection="$(cat <<'EOF'
provider: AWS
region: us-east-1
aws_access_key_id: <key>
aws_secret_access_key: <secret>
aws_signature_version: 4
host: s3.example.com
endpoint: https://s3.example.com
path_style: true
EOF
)"

# Also create a ConfigMap with the bucket name
oc create configmap gitlab-objectstorage \
  -n gitlab-system \
  --from-literal=BUCKET_NAME="my-gitlab-bucket"
```

## Service Modes

Each backend defaults to `managed`. Override per-cluster via labels.

| Label | Values | Default | Description |
|-------|--------|---------|-------------|
| `gitlab-db-mode` | managed / external | managed | PostgreSQL backend |
| `gitlab-redis-mode` | managed / external | managed | Redis backend |
| `gitlab-object-storage-mode` | managed / external | managed | Object storage backend |

| Mode | Behavior |
|------|----------|
| **managed** | AutoShift deploys CNPG/Redis Sentinel/NooBaa (requires their operators) |
| **external** | User provides connection secrets (see above) |

GitLab 19 (chart 10) removed the bundled PostgreSQL, Redis and MinIO, so `bundled` is no longer a
mode: `policy-gitlab-instance` reports `NonCompliant` and names the label to change.

`policy-gitlab-instance` creates GitLab only when its inputs exist. Until then it reports
`NonCompliant` and names each missing Secret or ConfigMap and the policy that creates it. In managed
mode the usual causes are `cloudnative-pg` or `odf` not set to `'true'`, or a NooBaa backing store
that is not Ready.

## Policy Chain

```
gitlab-operator-install + cert-manager-operator-install
├── gitlab-redis (managed mode: Redis Sentinel HA, 3+3 pods)
├── cnpg-gitlab (managed mode: HA PostgreSQL)
│   └── cnpg-gitlab-pooler (PgBouncer RW + RO)
├── gitlab-object-storage (managed mode: NooBaa OBC)
└── gitlab-instance (configures GitLab CR based on mode labels)
    └── gitlab-instance-ready (inform - checks GitLab phase: Running)
```

## Labels

```yaml
# Required
gitlab: 'true'
gitlab-subscription-name: gitlab-operator-kubernetes
gitlab-channel: stable
gitlab-source: certified-operators
gitlab-source-namespace: openshift-marketplace

# Service modes (default: managed)
# gitlab-db-mode: 'managed'
# gitlab-redis-mode: 'managed'
# gitlab-object-storage-mode: 'managed'

# External mode overrides
# gitlab-db-host: 'my-postgres.example.com'
# gitlab-redis-host: 'my-redis.example.com'
# gitlab-redis-port: '6379'

# ArgoCD integration
# gitlab-argocd-integration: 'true'       # Create credential template for all GitLab repos

# Gitaly HA (Praefect)
# gitlab-gitaly-ha: 'true'              # Enable Praefect HA for git storage (default: false)
# gitlab-praefect-replicas: '3'         # Praefect proxy replicas (default: 3)
# gitlab-gitaly-replicas: '3'           # Gitaly replicas per virtual storage (default: 3)

# Storage classes (optional — omit to use cluster default)
# gitlab-gitaly-storage-class: 'ocs-storagecluster-cephfs'  # StorageClass for Gitaly PVC; use RWX class to avoid Multi-Attach errors on multi-node clusters

# Database sizing
# gitlab-db-instances: '2'              # PostgreSQL replicas (default: 2)
# gitlab-db-pooler-instances: '2'       # PgBouncer replicas (default: 2)

# Component replica scaling (HPA min/max)
# gitlab-webservice-min-replicas: '2'   # default: 2
# gitlab-webservice-max-replicas: '4'   # default: 4
# gitlab-sidekiq-min-replicas: '2'      # default: 2
# gitlab-sidekiq-max-replicas: '4'      # default: 4
# gitlab-shell-min-replicas: '2'        # default: 2
# gitlab-shell-max-replicas: '4'        # default: 4

# Database backups (requires managed db mode + odf)
# gitlab-db-backups: 'true'

# SSH access (see SSH access)
# gitlab-ssh-mode: 'internal'           # internal (default) | loadbalancer | disabled
# gitlab-ssh-host: 'ssh.gitlab.example.com'  # loadbalancer only: DNS name for clone URLs
```

### Config (in clusterset values under `config.gitlab`)

Values that contain `/` or spaces cannot be Kubernetes labels. Set these in the `config` block:

```yaml
hubClusterSets:
  hub:
    config:
      gitlab:
        dbBackupSchedule: '0 2 * * *'              # Cron schedule for CNPG base backups
        dbBackupRetention: '30d'                   # Backup retention period
        backupSchedule: '0 3 * * *'                # Application backup (see Application backups)
        backupMaximum: 7                           # Application backups kept in the bucket
        backupScratchSize: '50Gi'                  # Ephemeral volume a backup is built in
        zoneSpread: true                           # Spread GitLab pods and its database across zones
```

`zoneSpread` switches every GitLab component and the database cluster from preferring separate
nodes to preferring separate zones. The preference is soft, so a single-zone cluster still
schedules. Changing it rolls the database instances, one switchover.

## Application backups

The CloudNativePG backups cover only the database. GitLab also needs its repositories, uploads,
artifacts and Git Large File Storage (LFS) objects, which only the
[GitLab backup](https://docs.gitlab.com/charts/backup-restore/backup/) captures. AutoShift runs it
as the toolbox's scheduled backup, on `backupSchedule`, into the GitLab bucket, keeping the newest
`backupMaximum` backups.

The backup job reads its storage credentials from the `gitlab-backup-s3cfg` Secret, an `s3cmd`
configuration. In managed object storage mode `policy-gitlab-object-storage` writes it. With
external object storage, create it yourself under the key `config`. Without it there are no
application backups, and `policy-gitlab-instance-prereqs` reports the Secret as missing.

To run a backup now:

```bash
oc create job manual-backup --from=cronjob/gitlab-toolbox-backup -n gitlab-system
```

You cannot restore a backup without the `gitlab-rails-secret` Secret, which is not in the backup.
Export it and keep it with your backups:

```bash
oc get secret gitlab-rails-secret -n gitlab-system -o yaml > gitlab-rails-secret.yaml
```

## SSH access

The OpenShift router carries only HTTP and TLS, so Git over SSH needs its own way into GitLab Shell.
`gitlab-ssh-mode` chooses it:

| Mode | Behavior |
|---|---|
| `internal` (default) | GitLab Shell is reachable inside the cluster only. Clone over HTTPS from outside |
| `loadbalancer` | GitLab Shell gets a `LoadBalancer` Service. Clone URLs use `gitlab-ssh-host`, or the address of the load balancer until you give it a DNS name. Needs a cloud load balancer, or MetalLB on bare metal |
| `disabled` | No GitLab Shell. Git over HTTPS only |

On a cluster with Federal Information Processing Standards (FIPS) mode enabled, the OpenSSH client
refuses Ed25519 keys. Use Elliptic Curve Digital Signature Algorithm (ECDSA) or RSA keys.

## Redis password

In managed Redis mode the policy seeds a random password the first time, in the
`gitlab-redis-password` Secret. Earlier releases seeded `gitlabRedis<cluster_name>`, which anyone
who knows the cluster name can derive. To rotate to a random one, delete the Secret, let the
policy write a new one, then restart everything that reads it:

```bash
oc delete secret gitlab-redis-password -n gitlab-system
# wait until the Secret exists again, then:
oc rollout restart statefulset/gitlab-redis statefulset/gitlab-redis-sentinel -n gitlab-system
oc rollout restart deployment/gitlab-webservice-default deployment/gitlab-sidekiq-all-in-1-v2 \
  deployment/gitlab-kas deployment/gitlab-gitlab-exporter deployment/gitlab-toolbox -n gitlab-system
```

## Managed Mode Details

### Redis Sentinel HA
- 3 Redis pods (1 master + 2 replicas) with AOF persistence
- 3 Sentinel pods for automatic failover
- Uses `rhel9/redis-7` image sourced dynamically from the OpenShift GitOps operator's `ARGOCD_REDIS_IMAGE` env var. The GitOps operator ships this as a `relatedImage` in its CSV, so oc-mirror captures it automatically — no separate image mirroring config needed for disconnected environments

### CloudNativePG PostgreSQL
- HA cluster with configurable instance count (`gitlab-db-instances` label, default: 2)
- PgBouncer connection pooling (RW + RO)
- TLS enabled by default (autogenerated certificates)
- Optional scheduled backups to NooBaa S3

### NooBaa Object Storage
- Auto-provisions ObjectBucketClaim
- Injects OpenShift service CA for TLS trust
- Single bucket with per-feature prefixes

### Gitaly HA (Praefect) — `gitlab-gitaly-ha: 'true'`

By default GitLab runs a single Gitaly pod for git repository storage — a single point of failure. Enabling Praefect replaces it with a replicated setup:

- **Praefect** (`gitlab-praefect-replicas`, default 3) — gRPC proxy layer that handles write fanout and read distribution across Gitaly nodes
- **Gitaly StatefulSet** (`gitlab-gitaly-replicas`, default 3) — replicated git storage nodes, one per virtual storage replica
- **Praefect database** — a `praefect_production` database is created in the existing CNPG cluster by using the `gitlab` user. No new CNPG cluster or secrets are required.

When enabled, Praefect intercepts all git operations from GitLab Rails/Sidekiq. Writes are replicated to all healthy Gitaly nodes. If a Gitaly node fails, Praefect automatically routes to a replica. The Praefect DB tracks replication state and manages failover.

```bash
# Verify Praefect can reach all Gitaly nodes
oc exec -n gitlab-system gitlab-praefect-0 -- \
  /usr/local/bin/praefect -config /etc/gitaly/config.toml dial-nodes

# Check replication status
oc exec -n gitlab-system gitlab-praefect-0 -- \
  /usr/local/bin/praefect -config /etc/gitaly/config.toml list-storages
```

## Troubleshooting

### CNPG Backup / WAL Archiving

The `policy-cnpg-gitlab-backup` policy creates a `gitlab-db-backup-ca` Secret containing the OpenShift service-serving-signer CA. CNPG's `endpointCA` field is a `SecretKeySelector` — it can only reference a Secret, not a ConfigMap. The `openshift-service-ca` ConfigMap (injected by the service CA annotation) is used as the source and the content is copied into the Secret so CNPG can pass `--ca-certificate` to barman for S3 SSL validation.

If WAL archiving fails with `[Errno 2] No such file or directory` or SSL errors, check:
```bash
oc get secret gitlab-db-backup-ca -n gitlab-system
oc get cluster.postgresql.cnpg.io gitlab-db -n gitlab-system -o jsonpath='{.status.conditions}'
```

```bash
# Check all GitLab policies
oc get policy -A | grep gitlab

# Check GitLab CR status
oc get gitlab gitlab -n gitlab-system -o jsonpath='{.status}'

# Check pod health
oc get pods -n gitlab-system

# Check CNPG cluster
oc get cluster.postgresql.cnpg.io -n gitlab-system

# Check Redis Sentinel
oc exec -n gitlab-system gitlab-redis-sentinel-0 -- redis-cli -p 26379 sentinel masters

# Force policy re-evaluation
oc annotate policy <name> -n policies-autoshift \
  policy.open-cluster-management.io/trigger-update="$(date +%s)" --overwrite
```
