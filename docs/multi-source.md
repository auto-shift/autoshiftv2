# Multi-Source AutoShift: Platform + Team Config

AutoShift supports a two-repo ArgoCD Application pattern where the platform team owns the AutoShift codebase and a separate team-config repo supplies per-cluster and per-team values. This keeps platform code and operational config in separate repositories with separate access controls.

## Why Two Repos

A single-repo setup works for small environments but breaks down when:

- Teams need to onboard new apps without filing PRs against the platform repo
- Different groups own platform code vs cluster configuration
- You want per-team CODEOWNERS without giving teams write access to policy templates

The two-repo pattern addresses all three by splitting responsibilities:

| Repo | Contains | Owned By |
|---|---|---|
| `autoshiftv2` (platform) | Policy charts, ApplicationSet, clusterset profiles | Platform team |
| `site-config` (config) | Per-cluster values files, per-team `tssc-apps` config | Platform team + teams (via CODEOWNERS) |

## ArgoCD Application Setup

Replace the single `sources:` entry with two sources — one for the platform repo, one for the config repo. The config repo is assigned a `ref:` alias so its path can be used in `valueFiles`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  destination:
    namespace: open-cluster-policies
    server: https://kubernetes.default.svc
  sources:
    - repoURL: https://github.com/your-org/autoshiftv2
      targetRevision: main
      path: autoshift
      helm:
        valueFiles:
          # Clusterset profiles (ordered by prefix — loaded in filename order)
          - $config/values/clustersets/00-hub.yaml
          - $config/values/clustersets/01-tssc.yaml
          # Per-cluster overrides + per-team tssc-apps config
          - $config/values/clusters/*.yaml
          # Per-team app config (wildcard picks up all team files automatically)
          - $config/values/teams/*.yaml

    - repoURL: https://github.com/your-org/site-config
      targetRevision: main
      ref: config   # ← makes this repo available as "$config" in valueFiles above
```

The `ref:` source is not rendered as a Helm chart — it only makes the repo's files available for path interpolation in the other source's `valueFiles`.

## Config Repo Structure

```
site-config/
  values/
    clustersets/
      00-hub.yaml         # Hub clusterset labels and config (ordered prefix ensures correct load order)
      01-tssc.yaml        # TSSC service enablement on hub
    clusters/
      dev-cluster.yaml    # Per-cluster labels and config for the dev cluster
      test-cluster.yaml
      prod-cluster.yaml
    teams/
      team-alpha.yaml     # team-alpha's tssc-apps config across all clusters
      team-beta.yaml      # team-beta's tssc-apps config across all clusters
```

### Clusterset files

Clusterset files define which labels are applied to clusters in that set. Use filename prefixes to control load order — AutoShift merges values left to right:

```yaml
# values/clustersets/00-hub.yaml
hubClusterSets:
  hub:
    labels:
      keycloak: 'true'
      tas: 'true'
      # ...
```

```yaml
# values/clustersets/01-tssc.yaml
hubClusterSets:
  hub:
    labels:
      gitlab: 'true'
      dev-hub: 'true'
      # TSSC additions layered on top of 00-hub.yaml
```

### Per-cluster files

Cluster files contain labels that apply only to that cluster, plus the `config` block that feeds into `rendered-config`:

```yaml
# values/clusters/dev-cluster.yaml
clusters:
  dev-cluster:
    labels:
      pipelines: 'true'
      tssc-app-namespaces: 'true'
    config:
      tssc-apps: {}   # teams fill this in via their own files (deep-merged)
```

### Per-team files

Each team manages their own file under `values/teams/`. The `tssc-apps` key is keyed by team name, so files from different teams never conflict — Helm deep-merges them as independent map keys:

```yaml
# values/teams/team-alpha.yaml
clusters:
  dev-cluster:
    config:
      tssc-apps:
        team-alpha:            # ← unique map key; never overwritten by other team files
          frontend:
            environments: [ci, dev]
          backend:
            environments: [ci, dev, stage]
  prod-cluster:
    config:
      tssc-apps:
        team-alpha:
          frontend:
            environments: [prod]
          backend:
            environments: [prod]
```

```yaml
# values/teams/team-beta.yaml
clusters:
  dev-cluster:
    config:
      tssc-apps:
        team-beta:             # ← independent key; team-alpha entries untouched
          api-gateway:
            environments: [ci, dev, stage, prod]
```

After merging, the hub template sees a single `tssc-apps` map containing all teams' entries and ranges over them to create the correct namespaces on each cluster.

## CODEOWNERS

With CODEOWNERS, each team owns their own file and can merge changes without platform team review:

```
# .github/CODEOWNERS (or CODEOWNERS in the repo root)

# Platform team owns everything by default
*                           @your-org/platform-team

# Each team owns their own values file
/values/teams/team-alpha.yaml   @your-org/team-alpha
/values/teams/team-beta.yaml    @your-org/team-beta
```

This lets teams self-service their namespace config — adding a new app is a one-line PR against their own file, approved by their own teammates.

## Team Onboarding Flow

### New team

1. Platform team adds `gitops-dev-team-{team}: standalone` (or `hub`) and `gitops-dev-team-{team}-tssc: 'true'` labels to the appropriate clusterset file.
2. ACM deploys the team's ArgoCD instance (`openshift-gitops-{team}`) and creates `tssc-argocd-integration`.
3. Platform team creates `values/teams/{team}.yaml` with an empty `tssc-apps` block and adds a CODEOWNERS entry for the team.
4. Team can now self-serve: PRs to their own file add apps/environments with no platform team approval required.

### New app on an existing cluster

The team edits their `values/teams/{team}.yaml` and adds an entry under the target cluster:

```yaml
clusters:
  dev-cluster:
    config:
      tssc-apps:
        my-team:
          new-app:
            environments: [ci, dev]
```

When ArgoCD syncs, the AutoShift ApplicationSet re-renders the `tssc-app-namespaces` policy. ACM evaluates it within the `noncompliant` evaluation interval (default: 30 seconds) and creates the namespaces and secrets.

### New cluster

1. Platform team creates `values/clusters/{cluster-name}.yaml` with the cluster's labels and an empty `config.tssc-apps: {}`.
2. Teams add their cluster entries to their team files.
3. ArgoCD syncs and AutoShift provisions the cluster.

## Wildcard ValueFiles Ordering

Helm processes `valueFiles` left to right. When using wildcards, files are loaded in lexicographic order within each glob. Use filename prefixes to control clusterset ordering:

```
00-hub.yaml     ← base hub profile (loaded first)
01-tssc.yaml    ← TSSC additions (loaded second, merges on top)
```

Per-cluster and per-team wildcards (`clusters/*.yaml`, `teams/*.yaml`) are always processed after the explicit clusterset files in the list, so clusterset defaults are always set before per-cluster overrides apply.

## Relationship to the Rendered-Config Pipeline

Team config under `config.tssc-apps` flows through AutoShift's rendered-config pipeline before policies consume it:

```
site-config/values/teams/team-alpha.yaml
  └─ Helm deep-merge (multiple team files merged into one values set)
       └─ clusters.{name}.config → raw-config-maps.yaml ConfigMap (per cluster)
            └─ policy-rendered-config-maps.yaml merges with clusterset config
                 └─ {cluster}.rendered-config ConfigMap on hub
                      └─ policy-tssc-app-namespaces hub template reads it
                           └─ Namespaces + Secrets created on managed cluster
```

This means team config changes are picked up on the next ArgoCD sync + ACM evaluation cycle without any manual intervention.
