# ServiceNow KVA Informer Policy

Deploys and manages the ServiceNow Kubernetes Visibility Agent (KVA) Informer.

## What this policy does

This chart defines ACM governance resources that:

- Create the target namespace for KVA on clusters labeled `autoshift.io/kva=true`
- Replicate a per-cluster ServiceNow credential secret from the hub
- Create one Argo CD `Application` per discovered managed cluster for the `informer-helm` chart
- Enforce `musthave` or `mustnothave` for each generated application based on cluster labels
- Validate that generated KVA Argo CD applications report healthy status

## Templates in this chart

| Template | Resource(s) | Purpose |
| --- | --- | --- |
| `templates/policy-kva-namespace.yml` | `Policy`, `Placement`, `PlacementBinding` | Ensures the KVA namespace exists where `autoshift.io/kva=true` |
| `templates/policy-kva-secret.yml` | `Policy`, `Placement`, `PlacementBinding` | Copies KVA credentials from hub secret namespace into the managed cluster KVA namespace (uses `hubTemplateOptions` with `secret-reader` service account) |
| `templates/policy-kva-application.yml` | `Policy`, `Placement`, `PlacementBinding` | Creates/updates KVA Argo CD applications based on cluster labels and checks app health (only rendered when `hubClusterSets` is configured) |
| `templates/policy-kva-policy-set.yml` | `PolicySet` (`kva-policies`), `Placement`, `PlacementBinding` | Groups all KVA policies under the `kva-policies` PolicySet |

## Placement and labels

All labels below are expected on `ManagedCluster` resources.

### Required labels

| Label | Meaning |
| --- | --- |
| `autoshift.io/kva=true` | Cluster should run KVA informer resources (namespace/secret policies and application generation target) |
| `autoshift.io/kva-hub=true` | Cluster is considered a KVA hub target (application policy placement and policy-set placement target) |

### Optional labels

| Label | Default behavior | Purpose |
| --- | --- | --- |
| `autoshift.io/kva-secret` | `k8s-informer-cred-<servicenow.instance.name>` | Overrides hub secret name used for KVA credentials |
| `autoshift.io/kva-hub-secret-namespace` | `cluster-install-secrets` | Overrides namespace on hub where KVA secret is read |
| `autoshift.io/gitops-namespace` | `openshift-gitops` | Overrides the namespace where KVA Argo CD `Application` resources are created |
| `autoshift.io/certs` | not set | When present, loads `user-ca-bundle` from `<cluster>.rendered-config` ConfigMap and injects it into Helm values (`customRootCA.certificate`) |
| `autoshift.io/owning-namespace` | `open-cluster-policies` | Namespace used to locate the `<cluster>.rendered-config` ConfigMap for per-cluster KVA and cert-bundle overrides |

## Key Helm values

From `values.yaml`:

| Value | Default | Purpose |
| --- | --- | --- |
| `policy_namespace` | `open-cluster-policies` | Namespace where ACM policy resources are created |
| `gitopsNamespace` | `openshift-gitops` | Default namespace for Argo CD `Application` resources (overridden by `autoshift.io/gitops-namespace` label) |
| `namespace` | `kva-informer` | Namespace where KVA is deployed on managed clusters |
| `acceptEula` | `Y` | Passed to ServiceNow informer chart |
| `kvaHubSecretNamespace` | `cluster-install-secrets` | Default hub namespace for KVA credentials |
| `kvaInformer.image.repository` | `docker.io/servicenowdocker/informer` | Default informer image repository passed to chart values |
| `kvaInformer.image.pullPolicy` | `Always` | Default informer image pull policy passed to chart values |
| `kvaInformer.image.tag` | `2.7.1` | Default informer image tag passed to chart values |
| `kvaInformer.image.dsRepository` | `docker.io/servicenowdocker/informer_ds` | Default informer_ds image repository passed to chart values |
| `kvaInformer.image.dsTag` | `2.7.1` | Default informer_ds image tag passed to chart values |
| `kvaInformer.releaseName` | `servicenow-kva-informer` | Helm release name inside Argo CD app values |
| `kvaInformer.helmURL` | ServiceNow helm repo URL | Source repo for `informer-helm` chart |
| `kvaInformer.appVersion` | `2.7.1` | Chart target revision |
| `servicenow.instance.name` | `vasubprod1` | ServiceNow instance name |
| `servicenow.instance.domain` | `servicenowservices.com` | ServiceNow instance domain |
| `customRootCA.use` | `true` | Enables custom root CA handling |
| `kubevirt.enabled` | `true` | Enables kubevirt integration flag in chart values |

## Dependencies

The KVA application policy depends on:

- `policy-gitops-systems-argocd` (must be compliant)

## Prerequisites

- KVA credential secret exists on the hub in `kvaHubSecretNamespace` (or label override namespace), containing:
  - `.user`
  - `.password`

## Behavior notes

- `policy-kva-application.yml` is only rendered when `hubClusterSets` is defined. It dynamically creates one Argo CD `Application` named `<cluster>-kva-informer` for each discovered managed cluster.
- The application policy contains two `ConfigurationPolicy` objects:
  - `setup-kva-gitops-applications` — enforces creation/removal of Argo CD applications.
  - `kva-check-pod-install` — informs on whether each KVA application reports `Healthy` status.
- If `autoshift.io/kva` is not set to `true` on a cluster, the application object is enforced as `mustnothave`.
- If `autoshift.dryRun` is set in parent values, policies switch to `inform` mode where implemented.
- The `kva-policies` PolicySet includes:
  - `policy-servicenow-kva-informer` (only when `hubClusterSets` is configured)
  - `policy-servicenow-kva-informer-namespace`
  - `policy-servicenow-kva-informer-secret`

## Per-cluster overrides via rendered-config

Several values can be overridden per managed cluster through the `<cluster>.rendered-config` ConfigMap (key `config`, YAML-formatted, under the `kva` key):

| Config key | Overrides value | Default |
| --- | --- | --- |
| `kva.namespace` | `namespace` | `kva-informer` |
| `kva.version` | `kvaInformer.appVersion` | `2.7.1` |
| `kva.eula` | `acceptEula` | `Y` |
| `kva.instance-name` | `servicenow.instance.name` | `vasubprod1` |
| `kva.instance-domain` | `servicenow.instance.domain` | `servicenowservices.com` |
| `kva.release-name` | `kvaInformer.releaseName` | `servicenow-kva-informer` |
| `kva.virt-enabled` | `kubevirt.enabled` | `true` |
| `kva.custom-root-ca` | `customRootCA.use` | `true` |
| `kva.helm-url` | `kvaInformer.helmURL` | ServiceNow helm repo URL |
| `kva.image-repo` | `kvaInformer.image.repository` | `docker.io/servicenowdocker/informer` |
| `kva.pull-policy` | `kvaInformer.image.pullPolicy` | `Always` |
| `kva.image-tag` | `kvaInformer.image.tag` | `2.7.1` |
| `kva.ds-repository` | `kvaInformer.image.dsRepository` | `docker.io/servicenowdocker/informer_ds` |
| `kva.ds-tag` | `kvaInformer.image.dsTag` | `2.7.1` |

The namespace and secret policies also read `kva.namespace` from the rendered-config to determine the target namespace on managed clusters.

## Quick validation

Render locally:

```bash
helm template policies/servicenow-kva-informer
```
