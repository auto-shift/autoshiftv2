# logging

Installs the Red Hat OpenShift Logging operator and creates the `ClusterLogForwarder` objects named
in `config.logging.forwarders`.

| Policy | What it does |
|---|---|
| `policy-logging-operator-install` | namespace and `OperatorPolicy` |
| `policy-logging-forwarders` | one `ClusterLogForwarder` per `config.logging.forwarders` entry |

Nothing is created until the list is set. Each entry's `spec` is `ClusterLogForwarder` schema passed
through verbatim, so inputs, filters, tuning, and any output type a later release adds work with no
change here, and several forwarders with different destinations and retention can run side by side.
AutoShift adds only the scaffolding a forwarder cannot run without: the collector `ServiceAccount`,
named `<name>-collector` unless the spec says otherwise, and the bindings to `collectorRoles`.

These are separate from the `logging` `ClusterLogForwarder` that the `loki` policy owns, which is
bound to a LokiStack and carries application and infrastructure logs. These need only this operator,
so a cluster forwarding to an external collector does not have to run Loki.

Together with the operator install, this satisfies the Security Technical Implementation Guide rules
`cluster-logging-operator-exist`, `audit-log-forwarding-enabled` and `audit-log-forwarding-uses-tls`.
See [docs/compliance.md](../../../docs/compliance.md).
