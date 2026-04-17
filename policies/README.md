# AutoShift Policies

Policies are organized by operator catalog source to clarify support boundaries.

```
policies/
  *.*/                  Red Hat supported (redhat-operators)
  certified/            Partner certified (certified-operators)
  community/            Community maintained (community-operators)
```

## Red Hat Operators (`policies/`)

Operators from the `redhat-operators` catalog. Fully supported by Red Hat under your OpenShift subscription.

## Certified Operators (`policies/certified/`)

Operators from the `certified-operators` catalog. Tested and certified for OpenShift, but supported by the technology partner. See [certified/README.md](certified/README.md).

## Community Operators (`policies/community/`)

Operators from the `community-operators` catalog. No vendor support. See [community/README.md](community/README.md).

## Auto-Discovery

The ApplicationSet automatically discovers policies from all three directories. No manual registration is required. To exclude individual policies, use `excludePolicies` in your values with the relative path:

```yaml
excludePolicies:
  - openshift-data-foundation      # Red Hat operator
  - certified/jfrog                 # Certified operator
  - community/my-operator           # Community operator
```
