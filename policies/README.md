# AutoShift Policies

Policies are organized into three tiers by operator catalog source, which makes the support
boundary explicit. Every policy lives in exactly one tier directory:

```
policies/
  stable/               Red Hat supported (redhat-operators)
  certified/            Partner certified (certified-operators)
  community/            Community maintained (community-operators)
```

## Red Hat operators (`policies/stable/`)

Operators from the `redhat-operators` catalog, plus the cluster configuration policies that are not
tied to an operator at all. Fully supported by Red Hat under your OpenShift subscription. This is
where most policies live and where a new policy belongs unless its operator ships in another
catalog. See [stable/README.md](stable/README.md).

## Certified operators (`policies/certified/`)

Operators from the `certified-operators` catalog. Tested and certified for OpenShift, but supported
by the technology partner rather than by Red Hat. See [certified/README.md](certified/README.md).

## Community operators (`policies/community/`)

Operators from the `community-operators` catalog. No vendor support. See
[community/README.md](community/README.md).

## Auto-Discovery

The ApplicationSet automatically discovers policies from all three directories. No manual registration is required. To exclude individual policies, use `excludePolicies` in your values with the policy folder name:

```yaml
excludePolicies:
  - openshift-data-foundation      # Red Hat operator
  - jfrog                          # Certified operator
  - my-operator                    # Community operator
```
