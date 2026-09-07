# manual-remediations

One policy per Security Technical Implementation Guide finding that the Compliance Operator ships
no automatic remediation for. Each needs a site decision the operator cannot make: which registries
are permitted, what the classification banner says, where audit logs are sent.

Enable the set with the `manual-remediations` label. Every policy is driven by
`config.manualRemediations` and renders nothing until its key is set, so the label alone changes
nothing and one label covers the whole set.

| Policy | Rules it satisfies |
|---|---|
| `policy-image-registries` | `ocp-allowed-registries`, `ocp-allowed-registries-for-import` |
| `policy-classification-banner` | `classification-banner` |
| `policy-motd` | `openshift-motd-exists` |
| `policy-oauth-customization` | `oauth-login-template-set`, `oauth-provider-selection-set`, `oauth-logout-url-set` |
| `policy-route-rate-limits` | `routes-rate-limit` |
| `policy-reject-unsigned-images` | `reject-unsigned-images-by-default` |
| `policy-project-template` | `project-config-and-template-network-policy`, `project-config-and-template-resource-quota` |
| `policy-quota-guard` | none; inform only, reports quotas that will reject pods |
| `policy-remove-samples-operator` | none; the Samples Operator pulls from a registry the list above blocks |

`policy-reject-unsigned-images` rolls every node and stops unsigned image pulls, so read it before
enabling it.

Quotas and network policies for application namespaces are not here. Both are per-application
decisions, and one platform-wide value for either breaks operator namespaces. `policy-project-template`
sets what a new project is born with, `policy-quota-guard` reports a quota that will reject pods, and
the rest belongs with whatever creates the application namespace.

Some findings live with the operator they concern rather than here: `cluster-logging-operator-exist`
and the two audit forwarding rules are covered by `logging`, and
`container-security-operator-exists` by `container-security`.

Findings that cannot be remediated at all are reported by `policy-stig-manual-review` in
`openshift-compliance-operator`.

Full walkthrough: [docs/compliance.md](../../../docs/compliance.md).
