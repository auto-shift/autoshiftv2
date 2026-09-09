# manual-remediations

One policy per Security Technical Implementation Guide finding that the Compliance Operator ships
no automatic remediation for. Each needs a site decision the operator cannot make: which registries
are permitted, what the classification banner says, where audit logs are sent.

Each policy has its own placement, gated on two labels: `manual-remediations` enables the set, and
`manual-remediations-<name>` opts into one member. Both are required, so a policy nobody asked for is
never placed rather than placed and vacuously compliant. `config.manualRemediations` carries content
only; there is no `enabled` flag, because the label is the switch.

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
| `policy-kubelet-eviction` | `kubelet-eviction-thresholds-set-hard-imagefs-available`, `-nodefs-available` |
| `policy-auditd-config` | `auditd-data-disk-error-action`, `-disk-full-action`, `-retention-flush`, `-retention-space-left-action` |
| `policy-audit-rule-order` | `audit-rules-unsuccessful-file-modification-open-rule-order`, `-openat-rule-order`, `-open-by-handle-at-rule-order` |
| `policy-cluster-proxy` | `cluster-wide-proxy-set`, only where egress goes through a proxy |
| `policy-sshd-access` | `sshd-limit-user-access` |
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
