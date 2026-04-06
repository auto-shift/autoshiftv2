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
oc create secret generic artifactory-admin \
  -n jfrog-system \
  --from-literal=password="<your-password>"
```

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
jfrog-node-replicas: '1'          # Artifactory member node replicas
cnpg-instances: '2'               # PostgreSQL instances
cnpg-pooler-instances: '2'        # PgBouncer instances per pooler

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
```
