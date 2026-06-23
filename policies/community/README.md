# Community Operator Policies

Policies in this directory deploy operators from the **Community Operator Catalog** (`community-operators`).

Community operators are maintained by the open source community. They are **not certified or supported by Red Hat or any vendor**. Use at your own risk in production environments.

## Adding a Community Operator

```bash
./scripts/generate-operator-policy.sh my-operator my-operator-sub \
  --channel stable \
  --namespace my-operator-system \
  --source community-operators
```

The generator automatically places the policy in `policies/community/` based on `--source`.

## Excluding Community Policies

To exclude individual community operator policies from a cluster, add to your values:

```yaml
excludePolicies:
  - my-community-operator
```

## Disconnected Environments

Community operators are served from `registry.redhat.io/redhat/community-operator-index`. This catalog must be mirrored separately if community operators are needed in disconnected environments.
