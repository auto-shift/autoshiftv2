# Certified Operator Policies

Policies in this directory deploy operators from the **Red Hat Certified Operator Catalog** (`certified-operators`).

These operators are developed and maintained by Red Hat technology partners. They are tested and certified to work on OpenShift, but **support is provided by the partner**, not Red Hat.

## Adding a New Certified Operator

```bash
./scripts/generate-operator-policy.sh my-operator my-operator-sub \
  --channel stable \
  --namespace my-operator-system \
  --source certified-operators
```

The generator automatically places the policy in `policies/certified/` based on `--source`.

## Excluding Certified Policies

To exclude individual certified operator policies from a cluster, add to your values:

```yaml
excludePolicies:
  - my-certified-operator
```
