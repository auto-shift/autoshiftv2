# GitLab Policy

Deploys GitLab on OpenShift with configurable backends for PostgreSQL, Redis, and object storage.

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
| `gitlab-db-mode` | managed / external / bundled | managed | PostgreSQL backend |
| `gitlab-redis-mode` | managed / external / bundled | managed | Redis backend |
| `gitlab-object-storage-mode` | managed / external / bundled | managed | Object storage backend |

| Mode | Behavior |
|------|----------|
| **managed** | AutoShift deploys CNPG/Redis Sentinel/NooBaa (requires their operators) |
| **external** | User provides connection secrets (see above) |
| **bundled** | GitLab's built-in components (not recommended for production) |

If `managed` is set but the required operator isn't enabled, GitLab falls back to bundled automatically.

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

# Database backups (requires managed db mode + odf)
# gitlab-db-backups: 'true'
```

### Config (in clusterset values under `config.gitlab`)

Values that contain `/` or spaces can't be Kubernetes labels. Set these in the `config` block:

```yaml
hubClusterSets:
  hub:
    config:
      gitlab:
        chartVersion: '9.10.1'                    # GitLab Helm chart version
        repoPath: autoshift/autoshiftv2            # GitLab group/project for ArgoCD repo
        siteConfigPath: autoshift/site-config      # GitLab group/project for site config
        dbBackupSchedule: '0 2 * * *'              # Cron schedule for CNPG base backups
        dbBackupRetention: '30d'                   # Backup retention period
```

## Managed Mode Details

### Redis Sentinel HA
- 3 Redis pods (1 master + 2 replicas) with AOF persistence
- 3 Sentinel pods for automatic failover
- Uses `rhel9/redis-7` image from the OpenShift GitOps operator (already mirrored for disconnected)

### CloudNativePG PostgreSQL
- HA cluster with configurable instance count (`cnpg-instances` label)
- PgBouncer connection pooling (RW + RO)
- TLS enabled by default (auto-generated certificates)
- Optional scheduled backups to NooBaa S3

### NooBaa Object Storage
- Auto-provisions ObjectBucketClaim
- Injects OpenShift service CA for TLS trust
- Single bucket with per-feature prefixes

## Troubleshooting

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
