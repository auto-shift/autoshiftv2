# Custom Policy Standards

AutoShift policies are grouped under a Red Hat Advanced Cluster Management Governance **standard**,
which appears in the console's policy filters. By default every policy uses `NIST SP 800-53`. You can
replace that with your own standard names, and give hub policies a separate standard from managed
cluster policies:

| Value | Placeholder | Applies to |
|-------|-------------|------------|
| `autoshift.policyStandard` | `${POLICY_STANDARD}` | Policies that run on managed clusters |
| `autoshift.policyStandardHub` | `${POLICY_STANDARD_HUB}` | Policies placed only on hubs |

If `policyStandardHub` is unset, it falls back to `policyStandard`. If both are unset, every policy
uses `NIST SP 800-53`.

## Configuration

```yaml
# In your Helm values file
autoshift:
  policyStandard: "Advanced Compute Platform"
  policyStandardHub: "Advanced Compute Platform Hub"
```

In the Governance console this creates two filterable standards: one for hub infrastructure and one
for everything that runs on managed clusters.

### Allowed characters

A standard name is display text, but it is substituted into policy configurations by `sed` and
written into YAML, so the chart rejects names containing `"`, `'`, `\`, `|`, `&`, `{`, `}` or a line
break, with a message naming the value. Braces are rejected because the ApplicationSet would evaluate
them as a template. Everything else works, including colons, slashes and parentheses, for
example `ISO/IEC 27001:2022`. Every place the name is written is quoted, so YAML syntax in a name stays
part of the name.

## Hub and managed policies

The choice is made per policy, not per directory, from where the policy is placed:

- A policy whose placement requires `autoshift.io/cluster-type: hub` uses `${POLICY_STANDARD_HUB}`.
  This rule is enforced by the validation suite.
- A policy placed on every hub clusterset (the Helm-templated `openshift-gitops`, `cluster-labels` and
  `cluster-config-maps` policies) uses `policyStandardHub`.
- A few policies that reach only the hub through a hub-only label, such as
  `policy-vault-transit-provider`, set the hub standard explicitly.
- Every other policy uses `${POLICY_STANDARD}`.

One directory can contain both. MetalLB, for example, uses the managed standard by default, and its
IP address pool and L2 advertisement ConfigMap policies, which run on the hub, use the hub standard.
`openshift-dns` uses the managed standard.

In a PolicyGenerator configuration, the default goes in `policyDefaults.standards`, and a hub-only
policy in an otherwise managed directory overrides it:

```yaml
policyDefaults:
  standards:
    - "${POLICY_STANDARD}"
policies:
  - name: policy-example-hub
    standards:
      - "${POLICY_STANDARD_HUB}"
    placement:
      placementPath: placement-hub.yaml
```

Always quote the placeholder. The validation suite fails a configuration that writes a standard name
directly, leaves the placeholder unquoted, or gives a hub-only policy the managed standard.

## How it works

The standards are substituted at deploy time, by the same mechanism as `${POLICY_NAMESPACE}`,
`${REMEDIATION}` and the other per-deployment placeholders:

1. Helm renders the ApplicationSet with `POLICY_STANDARD` and `POLICY_STANDARD_HUB` as plugin
   environment variables, after validating both names.
2. The CMP sidecar replaces `${POLICY_STANDARD_HUB}` and `${POLICY_STANDARD}` with `sed`, escaping the
   characters `sed` treats specially. The bootstrap chart's CMP and the copy that the `openshift-gitops`
   policy keeps on every hub substitute the same placeholders; the validation suite checks that they,
   `scripts/pg-render.sh` and the suite itself agree.
3. PolicyGenerator writes the value into each policy's `policy.open-cluster-management.io/standards`
   annotation.

Helm-templated policies read `.Values.autoshift.policyStandard` or `.Values.autoshift.policyStandardHub`
from the values the ApplicationSet passes them. In OCI mode, each PolicyGenerator policy is packaged as a
plain Helm chart with the placeholders still in place, and the chart substitutes them at install time,
quoting the standards annotation so names with YAML syntax stay strings.
