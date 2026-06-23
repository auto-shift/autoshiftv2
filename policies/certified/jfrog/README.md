# JFrog Artifactory HA Policy

Deploys JFrog Artifactory HA with CloudNativePG PostgreSQL and PgBouncer connection pooling.

## Prerequisites

### Required Operators

| Operator | Label | Catalog |
|----------|-------|---------|
| JFrog Artifactory | `jfrog: 'true'` | certified-operators |
| CloudNativePG | `cloudnative-pg: 'true'` | certified-operators |

### Required Secrets

The following secret must be created manually on each target cluster before Artifactory will deploy. The `policy-jfrog-keys` policy checks for this secret (inform-only) and the instance policy gates on it.

**Why this can't be automated:** Artifactory uses `master-key` to encrypt all sensitive data at rest (licenses, passwords, API keys). It must be stable across pod restarts and operator upgrades — if it changes, all encrypted data becomes unreadable. `join-key` authenticates HA cluster nodes. Any policy that auto-generated these would produce a new random value on each enforce cycle, breaking the installation. Generate them once, store them safely.

```bash
# Generate random keys
MASTER_KEY=$(openssl rand -hex 32)
JOIN_KEY=$(openssl rand -hex 32)

oc create namespace jfrog-system
oc create secret generic artifactory-keys \
  -n jfrog-system \
  --from-literal=master-key="$MASTER_KEY" \
  --from-literal=join-key="$JOIN_KEY"
```

### Optional Secrets

```bash
# JFrog license (optional - Artifactory runs without it, just limited)
oc create secret generic artifactory-license \
  -n jfrog-system \
  --from-literal=license-key="<your-license-key>"

# Admin password (optional - defaults to 'password')
# Provide the plain password - the policy derives the bootstrap credentials file format automatically
oc create secret generic artifactory-admin \
  -n jfrog-system \
  --from-literal=password="<your-password>"
```

### Managed Secrets

The `policy-jfrog-instance` policy creates and manages these secrets automatically (do not edit manually):

| Secret | Purpose |
|--------|---------|
| `artifactory-db-pooler-jdbc` | JDBC URL pointing to the PgBouncer pooler (used by the operator) |
| `artifactory-admin-creds` | Bootstrap credentials in `user=pass` format derived from `artifactory-admin` |

## Policy Chain

```
cloudnative-pg-operator-install
jfrog-operator-install
├── jfrog-keys (inform - verifies artifactory-keys secret exists)
├── cnpg-artifactory (creates HA PostgreSQL cluster)
│   ├── cnpg-artifactory-ready (inform - checks DB health)
│   └── cnpg-artifactory-pooler (creates PgBouncer RW + RO)
└── jfrog-instance (waits for DB + pooler + keys, then deploys)
    └── jfrog-instance-ready (inform - checks StatefulSet health)
```

## Labels

```yaml
# Required
jfrog: 'true'
jfrog-subscription-name: openshiftartifactoryha-operator
jfrog-channel: alpha
jfrog-source: certified-operators
jfrog-source-namespace: openshift-marketplace
cloudnative-pg: 'true'

# Sizing
jfrog-node-replicas: '2'          # Artifactory member node replicas (default: 2)
jfrog-db-instances: '2'           # PostgreSQL instances (default: 2)
jfrog-db-pooler-instances: '2'   # PgBouncer instances per pooler (default: 2)

# Backups (requires odf: 'true')
artifactory-db-backups: 'true'
```

### Config (in clusterset values under `config.jfrog`)

```yaml
hubClusterSets:
  hub:
    config:
      jfrog:
        dbBackupSchedule: '0 3 * * *'             # Cron schedule for CNPG base backups
        dbBackupRetention: '30d'                   # Backup retention period
```

## Troubleshooting

```bash
# Check policy status
oc get policy -A | grep jfrog

# Check Artifactory pods
oc get pods -n jfrog-system

# Check CNPG cluster health
oc get cluster.postgresql.cnpg.io -n jfrog-system

# Check if keys secret exists
oc get secret artifactory-keys -n jfrog-system

# Check Artifactory CR
oc get openshiftartifactoryha -n jfrog-system -o yaml

# Check access service startup (most common crash source)
oc logs <primary-pod> -n jfrog-system -c access --tail=50

# Check router join status
oc logs <primary-pod> -n jfrog-system -c router --tail=20
```

### CNPG Backup / WAL Archiving

The `policy-cnpg-artifactory-backup` policy creates an `artifactory-db-backup-ca` Secret containing the OpenShift service-serving-signer CA. CNPG's `endpointCA` field is a `SecretKeySelector` — it can only reference a Secret, not a ConfigMap. The content is copied from the `openshift-service-ca` ConfigMap so barman can validate the NooBaa S3 endpoint's certificate.

If WAL archiving fails with `[Errno 2] No such file or directory` or SSL errors, check:
```bash
oc get secret artifactory-db-backup-ca -n jfrog-system
oc get cluster.postgresql.cnpg.io artifactory-db -n jfrog-system -o jsonpath='{.status.conditions}'
```

### Common Startup Failures

**`Cluster join: Join key is missing`** — The `artifactory-keys` secret does not exist or was created after the policy enforced. Create the secret (see Prerequisites), then force policy re-evaluation:
```bash
oc annotate policy policy-jfrog-instance -n open-cluster-policies \
  policy.open-cluster-management.io/trigger-update="$(date +%s)" --overwrite
```

**`Illegal credentials file - each line is expected to be in the format: user=pass`** — The `artifactory-admin` secret was not present when the policy enforced, so `artifactory-admin-creds` was not created. Create `artifactory-admin` then trigger re-evaluation as above.

**`Cluster join: Access Service ping failed`** — The router cannot reach the access service on port 8040. This is a symptom of the access service crashing — check access container logs for the root cause rather than the router logs.
