# RHOAI (Red Hat OpenShift AI) Policy

This policy installs and configures Red Hat OpenShift AI (RHOAI) 3.0+ on OpenShift clusters.

## What Gets Installed

| Policy | Description |
|--------|-------------|
| `policy-rhoai-operator-install` | Installs the rhods-operator |
| `policy-rhoai-config` | Creates DSCInitialization and DataScienceCluster |

## Dependencies

RHOAI requires these operators to be installed first:

| Dependency | Policy | Required For |
|------------|--------|--------------|
| OpenShift Serverless | `policies/serverless/` | KServe model serving |
| OpenShift Service Mesh 3 | `policies/servicemesh3/` | Traffic routing, mTLS |
| OpenShift Pipelines | `policies/openshift-pipelines/` | Data Science Pipelines |
| Node Feature Discovery | `policies/node-feature-discovery/` | GPU detection (optional) |

## Quick Start

Enable RHOAI with all dependencies:

```yaml
hubClusterSets:
  hub:
    labels:
      # Dependencies
      serverless: 'true'
      serverless-subscription-name: serverless-operator
      serverless-channel: stable
      serverless-source: redhat-operators
      serverless-source-namespace: openshift-marketplace
      
      servicemesh3: 'true'
      servicemesh3-subscription-name: servicemeshoperator3
      servicemesh3-channel: stable-3.2
      servicemesh3-source: redhat-operators
      servicemesh3-source-namespace: openshift-marketplace
      
      pipelines: 'true'
      pipelines-subscription-name: openshift-pipelines-operator-rh
      pipelines-channel: pipelines-1.20
      pipelines-source: redhat-operators
      pipelines-source-namespace: openshift-marketplace
      
      # RHOAI
      rhoai: 'true'
      rhoai-subscription-name: rhods-operator
      rhoai-channel: fast-3.x
      rhoai-source: redhat-operators
      rhoai-source-namespace: openshift-marketplace
```

## Configuration Labels

### Operator Configuration

| Label | Default | Description |
|-------|---------|-------------|
| `rhoai` | - | Enable RHOAI (`'true'` or `'false'`) |
| `rhoai-subscription-name` | `rhods-operator` | OLM package name |
| `rhoai-channel` | `fast-3.x` | Operator channel |
| `rhoai-version` | - | Pin to specific CSV version (optional) |
| `rhoai-source` | `redhat-operators` | Catalog source |
| `rhoai-source-namespace` | `openshift-marketplace` | Catalog namespace |

### DataScienceCluster Component Configuration

Each component can be set to `Managed` or `Removed`:

| Label | Default | Description |
|-------|---------|-------------|
| `rhoai-dashboard` | `Managed` | RHOAI Dashboard UI |
| `rhoai-workbenches` | `Managed` | Jupyter notebooks and workbenches |
| `rhoai-pipelines` | `Managed` | Data Science Pipelines (Kubeflow) |
| `rhoai-kserve` | `Managed` | KServe single-model serving |
| `rhoai-modelmesh` | `Managed` | ModelMesh multi-model serving |
| `rhoai-codeflare` | `Managed` | CodeFlare distributed training |
| `rhoai-ray` | `Managed` | Ray distributed computing |
| `rhoai-kueue` | `Managed` | Kueue job queuing |
| `rhoai-training` | `Managed` | Training Operator (PyTorchJob, etc.) |
| `rhoai-trustyai` | `Managed` | TrustyAI model explainability |

### Infrastructure Configuration

| Label | Default | Description |
|-------|---------|-------------|
| `rhoai-monitoring` | `Managed` | Monitoring stack |
| `rhoai-servicemesh` | `Managed` | Service Mesh integration |
| `rhoai-trustedca` | `Managed` | Trusted CA bundle management |

## Examples

### Minimal RHOAI (Dashboard + Workbenches only)

```yaml
rhoai: 'true'
rhoai-subscription-name: rhods-operator
rhoai-channel: fast-3.x
rhoai-source: redhat-operators
rhoai-source-namespace: openshift-marketplace
# Disable components not needed
rhoai-pipelines: 'Removed'
rhoai-kserve: 'Removed'
rhoai-modelmesh: 'Removed'
rhoai-codeflare: 'Removed'
rhoai-ray: 'Removed'
rhoai-kueue: 'Removed'
rhoai-training: 'Removed'
rhoai-trustyai: 'Removed'
```

### Full RHOAI with Model Serving

```yaml
rhoai: 'true'
rhoai-subscription-name: rhods-operator
rhoai-channel: fast-3.x
rhoai-source: redhat-operators
rhoai-source-namespace: openshift-marketplace
# All components managed (default)
rhoai-dashboard: 'Managed'
rhoai-workbenches: 'Managed'
rhoai-pipelines: 'Managed'
rhoai-kserve: 'Managed'
rhoai-modelmesh: 'Managed'
```

### Pin to Specific Version

```yaml
rhoai: 'true'
rhoai-subscription-name: rhods-operator
rhoai-channel: fast-3.x
rhoai-version: 'rhods-operator.3.0.0'
rhoai-source: redhat-operators
rhoai-source-namespace: openshift-marketplace
```

## Verification

Check RHOAI installation status:

```bash
# Check operator
oc get csv -n redhat-ods-operator | grep rhods

# Check DSCInitialization
oc get dscinitializations

# Check DataScienceCluster
oc get datascienceclusters

# Check RHOAI pods
oc get pods -n redhat-ods-applications

# Check Dashboard route
oc get route -n redhat-ods-applications rhods-dashboard
```

## Troubleshooting

### Policies not applying

```bash
# Check policy status
oc get policies -n policies-autoshift | grep rhoai

# Check operator policy on spoke
oc describe operatorpolicy install-rhoai -n <cluster-name>

# Check config policy
oc describe configurationpolicy rhoai-dsc -n <cluster-name>
```

### DSCInitialization stuck

```bash
# Check DSCI status
oc describe dscinitializations default-dsci

# Check operator logs
oc logs -n redhat-ods-operator -l app.kubernetes.io/name=rhods-operator --tail=100
```

### DataScienceCluster not ready

```bash
# Check DSC status
oc describe datascienceclusters default-dsc

# Check component status
oc get pods -n redhat-ods-applications
oc get pods -n redhat-ods-monitoring
```

## values.yaml Reference

```yaml
rhoai:
  # Operator settings
  name: rhods-operator
  namespace: redhat-ods-operator
  channel: fast-3.x
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  operatorGroupName: rhoai-operator-group

  # DSCInitialization settings
  dsci:
    name: default-dsci
    applicationsNamespace: redhat-ods-applications
    monitoringNamespace: redhat-ods-monitoring
    monitoringState: Managed
    serviceMeshState: Managed
    serviceMeshNamespace: istio-system
    serviceMeshControlPlaneName: data-science-smcp
    trustedCABundleState: Managed

  # DataScienceCluster settings
  dsc:
    name: default-dsc
    dashboard: Managed
    workbenches: Managed
    datasciencepipelines: Managed
    kserve: Managed
    modelmeshserving: Managed
    codeflare: Managed
    ray: Managed
    kueue: Managed
    trainingoperator: Managed
    trustyai: Managed
```
