# Observability metrics

How to choose, add, and size metrics collected by Red Hat Advanced Cluster Management observability
across an AutoShift fleet.

This applies to a single hub managing spoke clusters and to a hub of hubs. Everything below is the
same in both, apart from two points that are called out where they arise: which hubs need the
configuration, and how the storage cost multiplies.

## Before you add anything

Two checks save most of the time people lose here.

**Is it already collected?** Red Hat Advanced Cluster Management ships a default allowlist. Adding a
metric that is already in it changes nothing, and the metric appearing in Grafana afterwards looks
like success:

```console
oc get cm observability-metrics-allowlist -n open-cluster-management-observability \
  -o jsonpath='{.data.metrics_list\.yaml}' | grep -w <metric>
```

**Which collector does the hub run?** This decides which mechanism is live, and it is the single
most common reason a correctly configured metric never arrives:

```console
oc get prometheusagent -A          # multicluster observability add-on
oc get deploy -A | grep metrics-collector   # classic collector
```

## The two collectors

| Collector | Mechanism | Present when |
|---|---|---|
| Classic MultiCluster Observability | `observability-metrics-custom-allowlist` config map | default installation |
| Multicluster observability add-on | `ScrapeConfig` federated by a `PrometheusAgent` | the add-on is enabled, including by `global-observability` |

The add-on **replaces** the classic collector. Where it is enabled the config map is still written
and is read by nothing. AutoShift renders both from one configuration block, so the same values work
either way and the inactive one has no effect. If a metric does not arrive, confirm the collector
before suspecting the configuration.

## Adding a metric

Two edits per hub cluster set:

```yaml
hubClusterSets:
  hub:                                        # the hub cluster set this applies to
    labels:
      acm-observability-custom-metrics: 'true'
    config:
      acm:
        observability:
          customMetrics:
            platform:
              names:
                - node_vmstat_pgfault
```

`names` become `{__name__="<metric>"}` federation selectors. `matches` are passed through as
written, so a label selector can be given directly. `platform` covers platform monitoring;
`userWorkload` covers user defined projects and requires user workload monitoring to be enabled on
the managed cluster first. AutoShift forwards those metrics, it does not enable their source.

### Removing a default metric

A metric name prefixed with `-` removes one of the default metrics instead of adding one, which is
worth knowing as a cost lever: the default set is collected from every managed cluster whether it is
used or not.

```yaml
            platform:
              names:
                - -cluster_infrastructure_provider
```

This only works on the classic collector, because the default set arrives through the add-on's own
`platform-metrics` ScrapeConfig, which AutoShift does not own. Removal entries are skipped when
building the add-on selectors rather than becoming a meaningless `{__name__="-metric"}` matcher.

### Which hubs need the configuration

A metric is collected by the agents belonging to one hub, so the configuration goes on the hub
cluster set of the hub that manages the clusters you want the metric from.

With a single hub managing spoke clusters, that is one place and there is nothing further to think
about.

With a hub of hubs it is one place per participating hub. Setting it only on the top hub collects
nothing from the clusters beneath an intermediate hub, because those clusters report to the
intermediate hub's agents rather than the top hub's. See
[Hub-of-Hubs Topology](hub-of-hubs.md).

## What a metric costs

The unit of cost is the time series, not the metric name. Measured on a fleet of 11 nodes:

| Scope | Series per metric | Example |
|---|---|---|
| Node | 1 per node | `node_vmstat_pgfault`, 11 series |
| Pod | 1 per pod | roughly 500 per hub |
| Container | 1 per container | 2 to 3 times the pod count |
| Histogram | 1 per pod per bucket | 5,000 to 20,000 |

A single pod scoped histogram can cost more than an entire default collection. High cardinality
labels are what drive this: `pod`, `container_id`, `path`, and `le` multiply series counts, while
node scoped metrics stay flat.

With a single hub the figures above are the whole cost. With a hub of hubs they are multiplied by
the number of participating hubs, because a metric collected on an intermediate hub is stored there
and again on the hub above it.

Check the cost before adding, by querying the managed cluster:

```
count(<metric>)
```

## Recording rules

A recording rule runs a query on the managed cluster and stores the result as a new metric. The
aggregation happens before anything crosses the network, which makes it the only effective control
on cardinality. Filtering at the hub does not help, because the series already exist by then.

```yaml
            platform:
              recording_rules:
                - record: cluster:my_app_requests:rate5m
                  expr: 'sum(rate(my_app_requests_total[5m]))'
```

Thousands of per pod series become one. Red Hat Advanced Cluster Management uses the same technique
for its own virtual machine metrics: of the 55 entries in its `platform-metrics-kubevirt`
collection, the per virtual machine values arrive as recorded aggregates such as
`cnv:vmi_status_running:count` rather than raw series.

AutoShift creates the rule as a `PrometheusRule` for the add-on, or writes it into the config map on
the classic path, and adds each `record` name to the `ScrapeConfig` selectors automatically so the
computed metric is federated without naming it twice.

Follow the upstream naming convention, `prefix:metric:operation`, and keep the `job` selector in the
expression. An aggregate over the wrong labels is cheap and wrong.

## Virtual machine metrics

Check `platform-metrics-kubevirt` before adding anything: it already collects 55 virtual machine and
virtualization metrics.

```console
oc get scrapeconfig platform-metrics-kubevirt -n open-cluster-management-observability \
  -o jsonpath='{.spec.params.match\[\]}'
```

Raw per virtual machine metrics scale with virtual machine count multiplied by disks and network
interfaces, so prefer a recording rule for anything per virtual machine. Red Hat Advanced Cluster
Management 2.17 also ships RightSizingRecommendation guides, including a variant for virtualization
workloads, which package this aggregation pattern already.

## Storage

Observability storage is set in the `MultiClusterObservability` custom resource and is owned by
`policy-acm-observability`. A representative allocation:

| Component | Default | Purpose |
|---|---|---|
| `receiveStorageSize` | 100Gi per replica | ingest, the first thing to fill |
| `compactStorageSize` | 100Gi | working space for compaction |
| `storeStorageSize` | 10Gi per shard | index cache |
| Object bucket | unbounded | long term storage under the retention policy |

The bucket grows according to `retentionConfig`, which defaults to 4 days raw, 30 days at 5 minute
resolution, and 365 days at 1 hour resolution.

Adding 100 pod scoped metrics to a 500 pod fleet is on the order of 100,000 new series, so raise
`receiveStorageSize` and size the bucket deliberately first. A full `thanos-receive` volume stops
ingest for every metric, not only the new ones. Shortening raw retention is the cheapest lever and
is usually overlooked.

## Platform guardrails

Cardinality from user defined projects is a workload risk rather than a configuration one, so
OpenShift Container Platform enforces limits at the scrape rather than relying on discipline.
`enforcedSampleLimit`, `enforcedTargetLimit`, and `enforcedLabelLimit` reject a scrape that exceeds
them, which is what protects a fleet from a metric nobody anticipated. They are unset by default.

AutoShift already manages user workload monitoring through the `uwm` label and `config.uwm`, which
covers retention, storage and resources for Prometheus, Thanos Ruler and Alertmanager. It does
**not** currently expose the enforcement limits, so setting them means editing the
`user-workload-monitoring-config` config map on the managed cluster directly, which AutoShift does
not reconcile. Extending `config.uwm` to carry them is the better long term answer if you intend to
open user workload collection widely.

The underlying problem the product calls an unbound attribute: a label whose value set is unlimited,
such as a customer or request identifier. Every distinct key and value pair is its own time series,
so a handful of unbound labels multiplies series exponentially. Limits contain the damage, but the
real fix belongs to whoever defines the metric, by binding labels to a known set of values.

See [Configuring performance and scalability for user workload monitoring][ocp-uwm]. Note that
monitoring moved out of the OpenShift Container Platform documentation into its own product,
Monitoring stack for Red Hat OpenShift, so older links do not resolve.

## Verifying

```console
# the ScrapeConfig exists on the hub
oc get scrapeconfig -n open-cluster-management-observability | grep autoshift

# it reached the managed cluster
oc get scrapeconfig -n open-cluster-management-agent-addon | grep autoshift

# the add-on is healthy, not stuck deploying
oc get managedclusteraddon multicluster-observability-addon -n <cluster> \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} {end}'

# the metric arrives, which is the only conclusive test
# query it in the Grafana instance in open-cluster-management-observability
```

## Troubleshooting

| Symptom | Cause |
|---|---|
| Policy is Compliant, metric never arrives | the hub runs the add-on and the config map path is inactive, or the metric is not exported on the managed cluster |
| Adding a metric changes nothing | it is already in the default allowlist |
| Metric arrives from one hub only (hub of hubs) | the label and configuration are missing from the other hub cluster set |
| Recorded metric never appears | the rule computes but nothing selects it, or the expression matches no series |
| Add-on stuck in `Deploying` | a configuration reference names an object that does not exist |
| Ingest stops for all metrics | a `thanos-receive` volume is full |

## Reference

AutoShift:

- [Values reference](values-reference.md) for `config.acm.observability.customMetrics`
- [Hub-of-Hubs Topology](hub-of-hubs.md) for the stacked case
- `policies/stable/advanced-cluster-management/README.md` for the policies that implement this

Red Hat Advanced Cluster Management, [Observability][acm-obs]. The guide is a single page, so the
sections below are numbered rather than linked separately:

- 1.3.1 Adding custom metrics, for the classic collector
- 1.11.9 Adding custom metrics for the multicluster observability add-on, for the `ScrapeConfig` path
- 1.8 RightSizingRecommendation guides, which package the aggregation pattern for namespace and
  virtualization workloads
- 1.3.3 Removing default metrics
- 1.11.12 Migrating custom allowlist, which documents an `allowlist-migration` command line tool that
  converts a classic allowlist into `ScrapeConfig` and `PrometheusRule` resources. AutoShift performs
  the same conversion from one configuration block, so the tool is useful mainly for auditing what a
  hand written allowlist becomes

Monitoring stack for Red Hat OpenShift. Monitoring is a separate documentation product now, not a
guide inside OpenShift Container Platform:

- [Configuring performance and scalability for user workload monitoring][ocp-uwm], which covers
  unbound metrics attributes, scrape sample limits and the alerts that warn before a limit is hit

[acm-obs]: https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes/2.17/html/observability/observing-environments-intro
[ocp-uwm]: https://docs.redhat.com/en/documentation/monitoring_stack_for_red_hat_openshift/4.22/html/configuring_user_workload_monitoring/configuring-performance-and-scalability-uwm
