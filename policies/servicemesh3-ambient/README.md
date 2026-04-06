# servicemesh3-ambient AutoShift Policy

## Overview

This policy configures OpenShift Service Mesh 3.x in ambient mode across hub and managed clusters. Ambient mode provides a sidecar-free mesh architecture with transparent L4 service mesh capabilities.

The policy suite handles:

1. **Namespaces** - Creates istio-system, istio-cni, ztunnel, and tracing-system namespaces
2. **IstioCNI** - Deploys CNI DaemonSet for transparent traffic redirection
3. **Istio control plane** - Creates Istio CR with ambient profile and OpenTelemetry integration
4. **ZTunnel** - Deploys per-node L4 proxy DaemonSet for mTLS and telemetry
5. **Kiali** - Configures Kiali dashboard with OpenShift OAuth and Prometheus integration
6. **Tempo tracing** - Deploys TempoStack with S3 storage (ODF) and Jaeger query frontend
7. **Monitoring** - Creates ServiceMonitor and PodMonitor for OpenShift monitoring

## Prerequisites

This policy has hard dependencies on 4 operators that must be installed first:

| Operator | Policy | Purpose |
|----------|--------|---------|
| servicemesh3operator | `policies/servicemesh3operator/` | Sail Operator (Istio, IstioCNI, ZTunnel CRDs) |
| kiali-operator | `policies/kiali/` | Kiali visualization CRDs |
| tempo-operator | `policies/tempo/` | Tempo distributed tracing CRDs |
| opentelemetry-operator | `policies/opentelemetry/` | OpenTelemetry Collector CRDs |

**Critical**: OpenShift Data Foundation (ODF) is required for Tempo S3 storage. The policy creates an ObjectBucketClaim that will fail without ODF.

## Enabling Ambient Mesh

Set the following label on your cluster or clusterset:

```yaml
servicemesh3-ambient: 'true'
```

## Configuration Labels

| Label | Description | Default |
|-------|-------------|---------|
| `servicemesh3-ambient` | Enable/disable ambient mesh configuration | |
| `servicemesh3operator-istio-version` | Istio version for all components | `v1.28-latest` |
| `servicemesh3operator-tempo` | Enable Tempo tracing in Kiali UI | off |

## Example Configuration

### Hub cluster with tracing enabled

```yaml
# Operators
servicemesh3operator: 'true'
servicemesh3operator-subscription-name: 'servicemeshoperator3'
servicemesh3operator-channel: 'stable'
servicemesh3operator-source: 'redhat-operators'
servicemesh3operator-source-namespace: 'openshift-marketplace'

kiali: 'true'
kiali-subscription-name: 'kiali-ossm'
kiali-channel: 'stable'

tempo: 'true'
tempo-subscription-name: 'tempo-product'
tempo-channel: 'stable'

opentelemetry: 'true'
opentelemetry-subscription-name: 'opentelemetry-product'
opentelemetry-channel: 'stable'

# Storage (required for Tempo)
storage-nodes: 'true'
openshift-data-foundation: 'true'

# Ambient mesh configuration
servicemesh3-ambient: 'true'
servicemesh3operator-tempo: 'true'
# servicemesh3operator-istio-version: 'v1.28-latest'  # Optional: pin version
```

### Managed cluster (minimal)

```yaml
# Same operator labels as hub
servicemesh3operator: 'true'
kiali: 'true'
tempo: 'true'
opentelemetry: 'true'
storage-nodes: 'true'
openshift-data-foundation: 'true'

# Ambient mesh
servicemesh3-ambient: 'true'
```

## Deployed Components

### Core Istio (ambient profile)
- **IstioCNI**: CNI plugin for transparent traffic capture (istio-cni namespace)
- **Istio control plane**: istiod for xDS configuration (istio-system namespace)
- **ZTunnel**: Per-node L4 proxy DaemonSet for mTLS (ztunnel namespace)

### Observability
- **Kiali**: Service mesh visualization with OpenShift OAuth (istio-system namespace)
- **Tempo**: Distributed tracing with ODF/S3 backend (tracing-system namespace)
- **OpenTelemetry Collector**: Trace ingestion from Istio to Tempo (istio-system namespace)
- **ServiceMonitor**: Prometheus scraping for istiod metrics
- **PodMonitor**: Prometheus scraping for ZTunnel metrics

## Values Configuration

Key values in `policies/servicemesh3-ambient/values.yaml`:

```yaml
servicemesh3operator:
  namespace: openshift-operators
  istioNamespace: istio-system
  istioCNINamespace: istio-cni
  ztunnelNamespace: ztunnel
  istioVersion: v1.28-latest
  updateStrategy: InPlace
  tempoNamespace: tracing-system
  pilot:
    cpuRequest: "100m"
    memoryRequest: "256Mi"
  tempo:
    storageSize: 10Gi
```

## Using the Mesh

Add namespaces to the ambient mesh:

```bash
kubectl label namespace myapp istio.io/dataplane-mode=ambient
```

Access Kiali dashboard:

```bash
oc get route kiali -n istio-system
```

## Verification

```bash
# Check core components
kubectl get istio,istiocni,ztunnel
kubectl get pods -n istio-system
kubectl get pods -n istio-cni
kubectl get pods -n ztunnel

# Check observability
kubectl get kiali -n istio-system
kubectl get tempostack -n tracing-system
kubectl get otelcol -n istio-system

# Verify Tempo storage
kubectl get objectbucketclaim tempo-bucket-odf -n tracing-system
```

## Troubleshooting

### Policy Not Applied
1. Check cluster labels: `oc get managedcluster <cluster> --show-labels | grep servicemesh3-ambient`
2. Verify placement: `oc get placement -n open-cluster-policies`
3. Check policy status: `oc describe policy policy-servicemesh3-ambient -n open-cluster-policies`
4. Verify operator dependencies are Compliant:
   ```bash
   oc get policy -n open-cluster-policies | grep -E 'servicemesh3operator|kiali|tempo|opentelemetry'
   ```

### Tempo Storage Issues
```bash
# Check ODF is installed
oc get storagecluster -n openshift-storage

# Check ObjectBucketClaim status
kubectl describe objectbucketclaim tempo-bucket-odf -n tracing-system

# Verify generated secret exists
kubectl get secret tempo-s3-secret -n tracing-system
```

### CNI or ZTunnel Issues
```bash
# Check CNI pods logs
kubectl logs -n istio-cni -l k8s-app=istio-cni-node

# Check ZTunnel logs
kubectl logs -n ztunnel -l app=ztunnel

# Common issues:
# - Certificate issues with istiod
# - Network policy blocking control plane traffic
# - CNI conflicts with other network plugins
```

### Kiali Not Showing Traces
Ensure the `servicemesh3operator-tempo: 'true'` label is set on the cluster to enable Tempo integration in Kiali.

## Resources

- **Istio Ambient Mode**: https://istio.io/latest/docs/ambient/
- **Sail Operator**: https://github.com/istio-ecosystem/sail-operator
- **Kiali Documentation**: https://kiali.io/docs/
- **Tempo Operator**: https://grafana.com/docs/tempo/latest/setup/operator/
- **AutoShift Developer Guide**: `../../docs/developer-guide.md`
