# Red Hat build of OpenTelemetry Operator

AutoShift policy for installing and managing the Red Hat build of OpenTelemetry operator.

## What is OpenTelemetry?

OpenTelemetry provides observability for cloud-native applications through:
- **Distributed Tracing**: Track requests across microservices
- **Metrics Collection**: Gather performance and resource metrics  
- **Log Correlation**: Connect logs with traces and spans

## Prerequisites

- OpenShift 4.12 or later
- Cluster admin permissions
- At least one namespace for collector deployment

## Installation

Add these labels to your AutoShift clusterset or cluster values:

```yaml
hubClusterSets:
  hub:
    labels:
      opentelemetry: 'true'
      opentelemetry-subscription-name: 'opentelemetry-product'
      opentelemetry-channel: 'stable'
      opentelemetry-source: 'redhat-operators'
      opentelemetry-source-namespace: 'openshift-marketplace'
```

## Configuration

After operator installation, you can deploy collectors:

### Basic Collector

```yaml
apiVersion: opentelemetry.io/v1alpha1
kind: OpenTelemetryCollector
metadata:
  name: otel-collector
  namespace: openshift-observability
spec:
  mode: deployment
  config: |
    receivers:
      otlp:
        protocols:
          grpc:
          http:
    processors:
      batch:
    exporters:
      logging:
    service:
      pipelines:
        traces:
          receivers: [otlp]
          processors: [batch]
          exporters: [logging]
```

### Sidecar Injection

Enable automatic sidecar injection for application namespaces:

```yaml
apiVersion: opentelemetry.io/v1alpha1
kind: Instrumentation
metadata:
  name: auto-instrumentation
  namespace: my-app-namespace
spec:
  exporter:
    endpoint: http://otel-collector:4317
  propagators:
    - tracecontext
    - baggage
  sampler:
    type: parentbased_traceidratio
    argument: "1.0"
```

## Integration with Tempo

For full observability stack with Tempo:

```yaml
hubClusterSets:
  hub:
    labels:
      tempo: 'true'
      opentelemetry: 'true'
```

Configure collector to export to Tempo:

```yaml
exporters:
  otlp:
    endpoint: tempo-gateway.tempo-namespace:4317
    tls:
      insecure: true
```

## Validation

Check operator installation:

```bash
oc get csv -n openshift-operators | grep opentelemetry
oc get pods -n openshift-operators | grep opentelemetry
```

Verify collector deployment:

```bash
oc get opentelemetrycollector -A
oc get instrumentation -A
```

## Resources

- [Red Hat OpenTelemetry Documentation](https://docs.openshift.com/container-platform/latest/observability/otel/otel-installing.html)
- [OpenTelemetry Project](https://opentelemetry.io/)
- [Operator SDK](https://github.com/open-telemetry/opentelemetry-operator)

## Support

For issues with this AutoShift policy, open an issue in the repository.
For OpenTelemetry operator issues, contact Red Hat support or open upstream issues.
