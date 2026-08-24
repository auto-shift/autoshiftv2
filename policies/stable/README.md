# Red Hat Operator Policies

Policies in this directory deploy operators from the **Red Hat Operator Catalog**
(`redhat-operators`), together with the cluster configuration policies that are not tied to an
operator at all, such as node configuration, labels, and machine config.

These operators are built and supported by Red Hat under your OpenShift subscription. This is the
default tier: a new policy belongs here unless its operator ships in the certified or community
catalog.

## Adding a policy here

```bash
./scripts/generate-operator-policy.sh my-operator my-operator-sub \
  --channel stable \
  --namespace my-operator-system \
  --add-to-autoshift
```

The generator defaults to this tier, so no extra flag is needed. For a configuration policy that
does not install an operator, use `generate-policy.sh` instead. Both are documented in
[scripts/README.md](../../scripts/README.md), and the full walkthrough is in the
[developer guide](../../docs/developer-guide.md).

## What lives here

Most of the four remaining Helm charts are in this tier rather than being PolicyGenerator
directories: `cluster-config-maps`, `cluster-labels`, `openshift-gitops`, and `policy-foundation`.
They keep the older chart layout because they generate resources that PolicyGenerator cannot
express. Everything else is a PolicyGenerator directory.

The ApplicationSet discovers every policy in this directory automatically. There is no list to
update. To exclude one, use `excludePolicies` in your values as described in
[the policies README](../README.md).
