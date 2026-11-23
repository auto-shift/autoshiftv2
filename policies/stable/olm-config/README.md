# OLM configuration

Turns off OLM's copied ClusterServiceVersions (`OLMConfig cluster`,
`spec.features.disableCopiedCSVs: true`). An operator installed for all namespaces otherwise has a
copy of its CSV in every namespace, which adds hundreds of large objects to etcd on a hub with many
namespaces.

| Policy | What it does |
|---|---|
| `policy-olm-config` | Sets `disableCopiedCSVs: true` on the `OLMConfig` singleton |
| `policy-olm-config-ready-test` | Inform: OLM reports the `DisabledCopiedCSVs` condition as True |

Enable it with the label `olm-disable-copied-csvs: 'true'`.

Users without cluster-wide rights stop seeing cluster-wide operators listed in their own namespaces
in the web console. Operators keep working. Remove the label and set the field back to `false` by
hand to turn copies on again, since AutoShift does not prune.
