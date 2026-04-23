# Trusted Software Supply Chain (TSSC)

AutoShift deploys and configures the Red Hat Trusted Software Supply Chain — a set of integrated services that secure the entire software delivery lifecycle: build, sign, scan, and deploy. This guide covers the AutoShift policies that implement TSSC, how they depend on each other, and how to configure them for standalone and multi-cluster topologies.

## Components

| Component | AutoShift Policy | What It Provides |
|---|---|---|
| Red Hat Keycloak | `keycloak` | Identity and access management; OIDC realm for all TSSC services |
| Trusted Artifact Signer (TAS) | `trusted-artifact-signer` | Container image and artifact signing via Sigstore/Cosign |
| Trusted Profile Analyzer (TPA) | `trusted-profile-analyzer` | SBOM ingestion and continuous security posture assessment |
| Advanced Cluster Security (ACS) | `advanced-cluster-security` | Runtime security, image scanning, admission control |
| OpenShift Pipelines | `openshift-pipelines` | Tekton-based CI/CD; Tekton Chains image signing |
| GitLab | `gitlab` | Source code management; TSSC pipeline source repositories |
| Developer Hub | `developer-hub` | Self-service developer portal (Red Hat build of Backstage) |
| Developer GitOps | `gitops-dev` | Per-team ArgoCD instances; ArgoCD credentials propagated via ACM |
| TSSC App Namespaces | `tssc-app-namespaces` | Per-app namespace sets with secrets pre-populated for pipelines |

## Deployment Order

TSSC services must be deployed in dependency order. AutoShift enforces this via ACM Policy `dependencies`:

```
1. keycloak                  ← OIDC realm for all downstream services
2. trusted-artifact-signer   ← Sigstore stack (depends on: pipelines operator)
3. trusted-profile-analyzer  ← SBOM analyzer (depends on: keycloak)
4. advanced-cluster-security ← Runtime security (depends on: ACS operator)
5. openshift-pipelines       ← Tekton Chains (admin pre-creates signing-secrets on hub)
6. gitlab                    ← Source code management (admin creates tssc-gitlab-credentials)
7. gitops-dev                ← Team ArgoCD + credentials (hub template reads Route + admin secret)
8. developer-hub             ← Backstage portal (depends on: all above, including gitlab)
9. tssc-app-namespaces       ← App namespace provisioning (depends on: pipelines)
```

## Integration Secrets

Each Day 2 policy creates an integration secret that downstream policies consume via spoke template lookups. These are read-only by the pipeline runtime.

| Secret | Namespace | Created By | Consumed By |
|---|---|---|---|
| `tssc-tas-integration` | `openshift-operators` | `policy-tas-securesign` (ACM) | pipelines, developer-hub |
| `tssc-tpa-integration` | `trusted-profile-analyzer` | `policy-tpa-instance` (ACM) | developer-hub, app-namespaces |
| `tssc-acs-integration` | `stackrox` | `policy-acs-instance` Job | developer-hub, app-namespaces |
| `tssc-argocd-integration` | `openshift-gitops-<team>` | `policy-gitops-dev-tssc-config` (ACM hub template) | developer-hub, app-namespaces |
| `signing-secrets` | `openshift-pipelines` | **Admin pre-creates** → ACM propagates | app-namespaces (`cosign.pub`) |
| `rhdh-realm-clients` | `keycloak-system` | `policy-keycloak-config` (ACM) | developer-hub |
| `tssc-gitlab-credentials` | `gitlab-system` | **Admin pre-creates** → ACM propagates | developer-hub, app-namespaces |

All secrets except `tssc-acs-integration` are created or propagated by ACM's continuous reconciliation loop — no one-shot Jobs. ACM re-evaluates on every cycle, so credential rotation only requires updating the source secret; ACM pushes the change everywhere automatically.

`tssc-acs-integration` is the one remaining Job because the ACS API token requires an authenticated HTTP POST to the Central API — there is no ACM template equivalent for making outbound HTTP calls.

### Admin-managed secrets

Two secrets require an admin to create them on the hub before ACM can propagate them:

**`signing-secrets`** (cosign keypair) — generate externally and create in `openshift-pipelines` on the hub:
```bash
cosign generate-key-pair
oc create secret generic signing-secrets \
  --from-file=cosign.key=cosign.key \
  --from-file=cosign.pub=cosign.pub \
  --from-literal=cosign.password="" \
  -n openshift-pipelines
```
ACM propagates all three fields to every `pipelines: 'true'` cluster. Using a single keypair means image signatures are verifiable across the entire fleet.

**`tssc-gitlab-credentials`** — see the [GitLab Integration](#gitlab-integration) section below.

## Cluster Topologies

### Standalone (Single Cluster)

All TSSC services and app namespaces run on the same cluster. ACM hub templates read the ArgoCD Route and admin secret and write `tssc-argocd-integration` into the gitops namespace; the app namespace policy propagates it to `-ci` namespaces from there.

```
standalone cluster
├── keycloak, TAS, TPA, ACS, Developer Hub   ← hub services
├── gitops-dev (team ArgoCD)                  ← team ArgoCD instance
└── {team}-{app}-ci, {team}-{app}-development, ...  ← tssc-app-namespaces
```

Labels for a standalone cluster:
```yaml
keycloak: 'true'
tas: 'true'
trusted-profile-analyzer: 'true'
acs: 'true'
dev-hub: 'true'
pipelines: 'true'
gitops-dev: 'true'
gitops-dev-team-dev: 'standalone'
gitops-dev-team-dev-tssc: 'true'
tssc-app-namespaces: 'true'
```

App namespace configuration goes in the cluster's `config` section (in `values/clusters/<cluster>.yaml` or a team-config values file — see [multi-source.md](multi-source.md)):
```yaml
config:
  tssc-apps:
    team1:
      myapp:
        environments: [ci, dev]
      otherapp:
        environments: [ci, dev, stage]
```

### Multi-Cluster (Hub + Dev + Test + Prod)

TSSC services run on the hub. App namespaces are distributed across environment clusters. ACM hub templates read the ArgoCD Route hostname and admin password from the hub, then write `tssc-argocd-integration` into `openshift-gitops-<team>`. The app namespace policy propagates it to every `-ci` namespace on managed clusters — pipelines connect back to the hub's ArgoCD to trigger deployments.

```
hub cluster
├── keycloak, TAS, TPA, ACS, Developer Hub
└── gitops-dev (team ArgoCD — manages all env clusters)

dev cluster
├── {team}-{app}-ci            ← Tekton pipelines run here
└── {team}-{app}-development   ← first deployment target

test cluster
└── {team}-{app}-stage

prod cluster
└── {team}-{app}-prod
```

Hub cluster labels (add to hub clusterset):
```yaml
keycloak: 'true'
tas: 'true'
trusted-profile-analyzer: 'true'
acs: 'true'
dev-hub: 'true'
pipelines: 'true'
gitops-dev: 'true'
gitops-dev-team-dev: 'hub'
gitops-dev-team-dev-tssc: 'true'
```

Dev cluster — label + config (in `values/clusters/dev-cluster.yaml` or a team-config values file):
```yaml
# labels section (sets managed cluster labels via cluster-labels policy)
labels:
  pipelines: 'true'
  tssc-app-namespaces: 'true'

# config section (merged into rendered-config; read by hub templates)
config:
  tssc-apps:
    team1:
      myapp:
        environments: [ci, dev]
```

Test cluster config:
```yaml
config:
  tssc-apps:
    team1:
      myapp:
        environments: [stage]
```

Prod cluster config:
```yaml
config:
  tssc-apps:
    team1:
      myapp:
        environments: [prod]
```

The `environments` list per app controls which namespaces are created on each cluster. Using separate per-cluster config files makes the topology explicit — each cluster only sees the environments it hosts.

## App Namespace Layout

App namespaces are derived from `config.tssc-apps` in each cluster's rendered-config. The structure is:

```yaml
config:
  tssc-apps:
    {team}:           # team name → ArgoCD NS = openshift-gitops-{team}
      {app}:          # app name → namespace prefix = {team}-{app}
        environments: [ci, dev, stage, prod]  # controls which namespaces are created
```

| Environment | Namespace Created | Contents |
|---|---|---|
| `ci` | `{team}-{app}-ci` | All integration secrets + `cosign-pub` + `pipeline` SA |
| `dev` | `{team}-{app}-development` | `tssc-image-registry-auth` + `pipeline` SA |
| `stage` | `{team}-{app}-stage` | `tssc-image-registry-auth` + `pipeline` SA |
| `prod` | `{team}-{app}-prod` | `tssc-image-registry-auth` + `pipeline` SA |

Secrets in the `-ci` namespace:

| Secret | Source |
|---|---|
| `cosign-pub` | `openshift-pipelines/signing-secrets[cosign.pub]` (hub lookup) |
| `tssc-acs-integration` | `stackrox/tssc-acs-integration` (spoke lookup) |
| `tssc-tas-integration` | `openshift-operators/tssc-tas-integration` (spoke lookup) |
| `tssc-tpa-integration` | `{tpa-namespace}/tssc-tpa-integration` (spoke lookup) |
| `tssc-argocd-integration` | `openshift-gitops-{team}/tssc-argocd-integration` (spoke lookup) |
| `tssc-image-registry-auth` | `openshift-config/pull-secret` (spoke lookup) |
| `tssc-gitlab-integration` | `gitlab-system/tssc-gitlab-credentials` (hub lookup) — only when `autoshift.io/gitlab: 'true'` |

All namespaces are labeled `argocd.argoproj.io/managed-by: openshift-gitops-{team}` so the team's ArgoCD instance can deploy to them.

Multiple teams and apps co-exist on the same cluster without conflicts because the team name is the top-level map key — Helm deep-merges separate team files so entries never overwrite each other. See [multi-source.md](multi-source.md) for the recommended two-repo setup that enables teams to self-serve their namespace configuration.

## Label Reference

### Keycloak

| Label | Default | Description |
|---|---|---|
| `keycloak` | — | `'true'` to enable |
| `keycloak-subscription-name` | `rhbk-operator` | OLM subscription name |
| `keycloak-channel` | `stable-v26.4` | Operator channel |
| `keycloak-version` | — | Pin to specific CSV (optional) |
| `keycloak-source` | `redhat-operators` | Catalog source |
| `keycloak-source-namespace` | `openshift-marketplace` | Catalog namespace |
| `keycloak-namespace` | `keycloak-system` | Namespace for Keycloak CR |
| `keycloak-realm-name` | `tssc-iam` | OIDC realm name |
| `keycloak-db-external` | `'false'` | Use external PostgreSQL (skips CNPG) |
| `keycloak-db-cluster-name` | `keycloak-pgsql` | CNPG cluster name |
| `keycloak-db-instances` | `'1'` | CNPG replica count |
| `keycloak-db-storage` | `10Gi` | CNPG storage size |
| `keycloak-db-host` | `keycloak-pgsql-pooler-rw.keycloak-system.svc` | External DB host (if external) |
| `keycloak-db-secret` | `keycloak-pgsql-user` | Secret with DB credentials |
| `keycloak-db-name` | `keycloak` | Database name |

### Trusted Artifact Signer (TAS)

| Label | Default | Description |
|---|---|---|
| `tas` | — | `'true'` to enable |
| `tas-subscription-name` | `rhtas-operator` | OLM subscription name |
| `tas-channel` | `stable` | Operator channel |
| `tas-version` | — | Pin to specific CSV (optional) |
| `tas-source` | `redhat-operators` | Catalog source |
| `tas-source-namespace` | `openshift-marketplace` | Catalog namespace |

### Trusted Profile Analyzer (TPA)

| Label | Default | Description |
|---|---|---|
| `trusted-profile-analyzer` | — | `'true'` to enable |
| `trusted-profile-analyzer-subscription-name` | `rhtpa-operator` | OLM subscription name |
| `trusted-profile-analyzer-channel` | `stable-v1.1` | Operator channel |
| `trusted-profile-analyzer-version` | — | Pin to specific CSV (optional) |
| `trusted-profile-analyzer-namespace` | `trusted-profile-analyzer` | Namespace for TPA CR |
| `trusted-profile-analyzer-db-external` | `'false'` | Use external PostgreSQL |
| `trusted-profile-analyzer-db-cluster-name` | `tpa-pgsql` | CNPG cluster name |
| `trusted-profile-analyzer-db-instances` | `'2'` | CNPG replica count |
| `trusted-profile-analyzer-db-storage` | `32Gi` | CNPG storage size |
| `trusted-profile-analyzer-db-host` | — | External DB host (if external) |
| `trusted-profile-analyzer-db-name` | `tpa` | Database name |
| `trusted-profile-analyzer-db-secret` | — | Secret with DB credentials |

### Advanced Cluster Security (ACS)

| Label | Default | Description |
|---|---|---|
| `acs` | — | `'true'` to enable |
| `acs-subscription-name` | `rhacs-operator` | OLM subscription name |
| `acs-channel` | `stable` | Operator channel |
| `acs-version` | — | Pin to specific CSV (optional) |
| `acs-monitoring` | `'true'` | OpenShift monitoring integration |
| `acs-egress-connectivity` | `Online` | `Online` or `Offline` for air-gapped |
| `acs-scanner-v4` | `Enabled` | Scanner V4 component state |
| `acs-admission-control` | — | `'true'` to enable admission enforcement |
| `acs-vm-scanning` | — | `'true'` to enable VM scanning (Dev Preview) |
| `acs-auth-provider` | `openshift` | Auth provider type |
| `acs-auth-min-role` | `None` | Minimum role for authenticated users |
| `acs-auth-admin-group` | `cluster-admins` | Group mapped to Admin role |

### OpenShift Pipelines

| Label | Default | Description |
|---|---|---|
| `pipelines` | — | `'true'` to enable |
| `pipelines-subscription-name` | `openshift-pipelines-operator-rh` | OLM subscription name |
| `pipelines-channel` | `pipelines-1.21` | Operator channel |
| `pipelines-version` | — | Pin to specific CSV (optional) |

### Developer Hub

| Label | Default | Description |
|---|---|---|
| `dev-hub` | — | `'true'` to enable |
| `dev-hub-subscription-name` | `rhdh` | OLM subscription name |
| `dev-hub-channel` | `fast-1.9` | Operator channel |
| `dev-hub-version` | — | Pin to specific CSV (optional) |
| `dev-hub-instance-name` | `developer-hub` | Name of the Backstage CR |
| `dev-hub-instance-namespace` | `rhdh` | Namespace for the Backstage instance |
| `dev-hub-gitops-team` | `dev` | gitops-dev team whose ArgoCD to integrate |

### Developer GitOps (TSSC teams)

| Label | Default | Description |
|---|---|---|
| `gitops-dev` | — | `'true'` to enable team ArgoCD provisioning |
| `gitops-dev-team-{team}` | — | `'standalone'` or `'hub'` — deploy ArgoCD for this team |
| `gitops-dev-team-{team}-tssc` | — | `'true'` to enable TSSC token integration for this team |

ArgoCD sizing and RBAC are defined in the `config` section (not labels), keyed by team name under `config.gitops-dev`. See [values-reference.md](values-reference.md) for the full config schema.

### GitHub

| Label | Default | Description |
|---|---|---|
| `github` | — | `'true'` to enable GitHub integration in Developer Hub |

GitHub credentials (`host`, `token`, `org`) are read from `github-system/tssc-github-credentials` on the hub — not from labels. See [GitHub Integration](#github-integration) for setup.

### GitLab

| Label | Default | Description |
|---|---|---|
| `gitlab` | — | `'true'` to enable the GitLab operator |
| `gitlab-subscription-name` | `gitlab-operator-kubernetes` | OLM subscription name |
| `gitlab-channel` | `stable` | Operator channel |
| `gitlab-version` | — | Pin to specific CSV (optional) |
| `gitlab-source` | `certified-operators` | Catalog source |
| `gitlab-source-namespace` | `openshift-marketplace` | Catalog namespace |
| `gitlab-db-mode` | `managed` | `managed` (CNPG), `external` (BYO), or `bundled` |
| `gitlab-redis-mode` | `managed` | `managed` (Sentinel), `external` (BYO), or `bundled` |
| `gitlab-object-storage-mode` | `managed` | `managed` (NooBaa), `external` (BYO S3), or `bundled` |
| `gitlab-argocd-integration` | — | `'true'` to create ArgoCD credential template |
| `gitlab-db-instances` | `'2'` | CNPG replica count |
| `gitlab-db-pooler-instances` | `'2'` | PgBouncer replica count |

### TSSC App Namespaces

| Label | Default | Description |
|---|---|---|
| `tssc-app-namespaces` | — | `'true'` to enable namespace provisioning on this cluster |

App namespace configuration is driven by `config.tssc-apps` in the cluster's rendered-config, not labels. The config structure is:

```yaml
config:
  tssc-apps:
    {team}:
      {app}:
        environments: [ci, dev, stage, prod]
```

See [App Namespace Layout](#app-namespace-layout) for the full namespace mapping and [multi-source.md](multi-source.md) for how to organize this config across multiple teams and clusters.

## GitHub Integration

When `github: 'true'` is set, Developer Hub is configured to discover catalog entries from a GitHub organization and use GitHub as the SCM integration. This is an external integration — there is no GitHub operator to install.

**Developer Hub catalog and SCM integration** (`policy-developer-hub-instance`): The Developer Hub instance receives GitHub catalog discovery (`catalog.providers.github`) and SCM integration (`integrations.github`) configuration, plus the `@backstage/plugin-catalog-backend-module-github-dynamic` plugin. Developer Hub reads `tssc-github-credentials` directly from `github-system` on the managed cluster using spoke template lookups.

GitLab and GitHub can be enabled simultaneously — the `catalog.providers` and `integrations` blocks merge both providers when both labels are `'true'`.

### Admin Setup

Before enabling GitHub, create the credential secret on the hub cluster in `github-system`:

```bash
oc create namespace github-system
oc create secret generic tssc-github-credentials \
  --from-literal=host=github.com \
  --from-literal=token=<github-personal-access-token> \
  --from-literal=org=<your-github-org> \
  -n github-system
```

For GitHub Enterprise, set `host` to your GHE hostname (e.g. `github.example.com`). The token needs `repo` and `read:org` scopes for catalog discovery.

Then set the label on your cluster or clusterset:

```yaml
labels:
  github: 'true'
```

### Token Rotation

```bash
oc patch secret tssc-github-credentials -n github-system \
  --type=merge -p '{"data":{"token":"'$(echo -n <new-token> | base64)'"}}'
```

ACM propagates the new token to Developer Hub on the next policy evaluation cycle.

## GitLab Integration

When `gitlab: 'true'` is set, AutoShift deploys the GitLab operator and configures it as the TSSC source code manager. Two additional integrations are automatically activated based on that same label:

**Developer Hub catalog and SCM integration** (`policy-developer-hub-instance`): The Developer Hub instance receives GitLab catalog discovery (`catalog.providers.gitlab`) and SCM integration (`integrations.gitlab`) configuration, plus the `@backstage-community/plugin-gitlab` dynamic plugins. Developer Hub reads `tssc-gitlab-credentials` directly from `gitlab-system` on the managed cluster using spoke template lookups — no hub involvement needed.

**Pipeline `-ci` namespace secret** (`policy-tssc-app-namespaces`): Every `-ci` namespace receives a `tssc-gitlab-integration` secret containing the GitLab host, API token, and service account username. ACM hub templates read `tssc-gitlab-credentials` from `gitlab-system` on the hub and write the values into each managed cluster's `-ci` namespaces. ACM evaluates this on every policy cycle, so token rotation requires only updating the hub secret.

### Admin Setup

Before enabling GitLab, create the credential secret on the hub cluster in `gitlab-system`:

```bash
oc create namespace gitlab-system
oc create secret generic tssc-gitlab-credentials \
  --from-literal=host=gitlab.apps.<your-cluster>.<domain> \
  --from-literal=token=<gitlab-api-token> \
  --from-literal=username=tssc-service-account \
  -n gitlab-system
```

The secret must exist before ACM evaluates the `tssc-app-namespaces` and `developer-hub` policies. If applied after the policies are already Compliant, force re-evaluation:

```bash
oc annotate policy policy-tssc-app-namespaces -n open-cluster-policies \
  policy.open-cluster-management.io/trigger-update="$(date +%s)" --overwrite
oc annotate policy policy-developer-hub-instance -n open-cluster-policies \
  policy.open-cluster-management.io/trigger-update="$(date +%s)" --overwrite
```

### Token Rotation

To rotate the GitLab API token, update the hub secret:

```bash
oc patch secret tssc-gitlab-credentials -n gitlab-system \
  --type=merge -p '{"data":{"token":"'$(echo -n <new-token> | base64)'"}}'
```

ACM propagates the new token to all `-ci` namespaces and Developer Hub on the next policy evaluation cycle (default: every 10 minutes when Compliant).

## Values File

AutoShift ships a `tssc.yaml` clusterset values file as a starting point. Add it to your ArgoCD Application `valueFiles` after your existing hub/managed profiles:

```yaml
valueFiles:
  - values/global.yaml
  - values/clustersets/hub.yaml       # base hub profile
  - values/clustersets/tssc.yaml      # TSSC additions
  - values/clusters/*.yaml            # per-cluster overrides (labels + config)
```

The `tssc.yaml` file enables all TSSC operators and Day 2 services on the hub clusterset. Per-cluster app namespace configuration goes in `values/clusters/<cluster-name>.yaml` under the `config.tssc-apps` key.

For environments with many teams, see [multi-source.md](multi-source.md) for the recommended two-repo ArgoCD Application setup that lets teams manage their own `tssc-apps` config without touching the platform repo.

## Troubleshooting

### `tssc-argocd-integration` is empty or missing

ACM hub templates read the ArgoCD Route and admin secret from the hub to populate this secret. Check that both source resources exist:
```bash
oc get route argocd-dev-server -n openshift-gitops-dev
oc get secret argocd-dev-cluster -n openshift-gitops-dev
```
If the Route is missing, ArgoCD may still be starting up — wait for `policy-gitops-dev-argocd` to be Compliant. Then force re-evaluation of `policy-gitops-dev-tssc-config` to pick up the new Route.

### `signing-secrets` is missing on a managed cluster

This secret is propagated from the hub by `policy-pipelines-tssc-config`. Verify it exists on the hub first:
```bash
oc get secret signing-secrets -n openshift-pipelines
```
If missing, the admin needs to generate and create it (see [Admin-managed secrets](#admin-managed-secrets) above). Once it exists on the hub, force re-evaluation of `policy-pipelines-tssc-config`.

### App namespace policy is Compliant but secrets are empty

The hub or spoke template lookup found no source secret. Verify each upstream secret exists:
```bash
oc get secret tssc-argocd-integration -n openshift-gitops-dev
oc get secret tssc-acs-integration -n stackrox
oc get secret signing-secrets -n openshift-pipelines
```

### Force policy re-evaluation after a secret is created

```bash
oc annotate policy policy-tssc-app-namespaces -n open-cluster-policies \
  policy.open-cluster-management.io/trigger-update="$(date +%s)" --overwrite
```

### GitLab integration secret is missing from `-ci` namespaces

The `tssc-gitlab-integration` secret is only created when `autoshift.io/gitlab: 'true'` is set on the managed cluster. Verify the label is present:
```bash
oc get managedcluster <name> -o jsonpath='{.metadata.labels.autoshift\.io/gitlab}'
```

Also verify the `tssc-gitlab-credentials` secret exists in `gitlab-system` on the hub:
```bash
oc get secret tssc-gitlab-credentials -n gitlab-system
```

### Developer Hub does not show GitLab catalog entries

Check that the `GITLAB_HOST` and `GITLAB_TOKEN` env vars are populated in the `tssc-developer-hub-env` secret in the Developer Hub namespace:
```bash
oc get secret tssc-developer-hub-env -n rhdh -o jsonpath='{.data.GITLAB_HOST}' | base64 -d
```

If empty, the `tssc-gitlab-credentials` secret was missing when the policy evaluated. Create or update it, then force re-evaluation of `policy-developer-hub-instance`.
