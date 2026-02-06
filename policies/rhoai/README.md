# RHOAI (Red Hat OpenShift AI) Policy

This policy installs and configures Red Hat OpenShift AI (RHOAI) 3.0+ on OpenShift clusters.

## What Gets Installed

| Policy | Description |
|--------|-------------|
| `policy-rhoai-operator-install` | Installs the rhods-operator |
| `policy-rhoai-config` | Creates DSCInitialization and DataScienceCluster |

### ConfigurationPolicies Created

| ConfigurationPolicy | Description |
|---------------------|-------------|
| `rhoai-dsci` | Creates DSCInitialization with monitoring and service mesh config |
| `rhoai-dsc-bootstrap` | Creates DataScienceCluster with minimal spec |
| `rhoai-dsc` | Configures DSC components using v2 API (empty specs for enabled components) |
| `rhoai-knative-serving` | Creates KnativeServing for KServe |
| `rhoai-dashboard-route` | Creates Route to expose the dashboard |

> **RHOAI 3.0 v2 API Changes**:
> - Uses `datasciencecluster.opendatahub.io/v2` API
> - Components are enabled by including them with empty specs `{}`
> - No `managementState` field (removed in v2)
> - Component name changes: `datasciencepipelines` → `aipipelines`
> - Removed components: `codeflare`, `modelmeshserving` (deprecated)
> - `OdhDashboardConfig` CRD removed (dashboard config managed by Dashboard component)

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

### DataScienceCluster Component Configuration (v2 API)

Components are controlled via values.yaml (set to `true` to enable, `false` to disable):

| Component | Default | Description |
|-----------|---------|-------------|
| `dashboard` | `true` | RHOAI Dashboard UI |
| `workbenches` | `true` | Jupyter notebooks and workbenches |
| `aipipelines` | `true` | AI Pipelines (formerly datasciencepipelines) |
| `kserve` | `true` | KServe model serving |
| `modelregistry` | `true` | Model Registry for model versioning |
| `ray` | `true` | Ray distributed computing |
| `kueue` | `true` | Kueue job queuing |
| `trainingoperator` | `true` | Training Operator (PyTorchJob, etc.) |
| `trustyai` | `true` | TrustyAI model explainability |

**Removed in v2:**
- `codeflare` - functionality merged into other components
- `modelmeshserving` - use KServe instead
- `datasciencepipelines` - renamed to `aipipelines`

### Infrastructure Configuration

| Label | Default | Description |
|-------|---------|-------------|
| `rhoai-monitoring` | `Managed` | Monitoring stack |
| `rhoai-servicemesh` | `Managed` | Service Mesh integration |
| `rhoai-trustedca` | `Managed` | Trusted CA bundle management |

## Examples

### Minimal RHOAI (Dashboard + Workbenches only)

Configure via `values.yaml`:

```yaml
rhoai:
  dsc:
    dashboard: true
    workbenches: true
    # Disable other components
    aipipelines: false
    kserve: false
    modelregistry: false
    ray: false
    kueue: false
    trainingoperator: false
    trustyai: false
```

### Full RHOAI with Model Serving (Default)

All components enabled by default in `values.yaml`:

```yaml
rhoai:
  dsc:
    dashboard: true
    workbenches: true
    aipipelines: true
    kserve: true
    modelregistry: true
    ray: true
    kueue: true
    trainingoperator: true
    trustyai: true
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

### Components not deploying (RHOAI 3.0 v2)

If components are not deploying:

```bash
# Check DSC spec vs status
oc get datasciencecluster default-dsc -o yaml

# Check if components are present in spec (v2 API)
oc get datasciencecluster default-dsc -o jsonpath='{.spec.components}' | jq

# Check component status
oc get datasciencecluster default-dsc -o jsonpath='{.status.components}' | jq

# Check ConfigurationPolicy compliance
oc get configurationpolicy -n local-cluster | grep rhoai

# View policy details
oc describe configurationpolicy rhoai-dsc -n local-cluster

# Check Dashboard component CRD (v2 uses components.platform.opendatahub.io)
oc get dashboard -A
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

  # DataScienceCluster settings (v2 API)
  dsc:
    name: default-dsc
    # Set to true to enable, false to disable
    dashboard: true
    workbenches: true
    aipipelines: true        # Renamed from datasciencepipelines
    kserve: true
    modelregistry: true
    ray: true
    kueue: true
    trainingoperator: true
    trustyai: true
    # Removed in v2: codeflare, modelmeshserving

  # Dashboard configuration (v2 API)
  # Note: OdhDashboardConfig CRD removed in RHOAI 3.0
  # Dashboard settings managed via Dashboard component
  dashboard:
    # These settings may be used for future dashboard configuration
    disableTracking: false
    disableModelRegistry: false
    disableModelCatalog: false
    disableKServeMetrics: false
    genAiStudio: true
    modelAsService: true
    disableLMEval: false
    notebookControllerEnabled: true
    notebookNamespace: rhods-notebooks
    pvcSize: 20Gi
```
