# htpasswd

Configures HTPasswd identity providers on `OAuth/cluster` from `config.htpasswd`, and grants
`cluster-admin` to the users listed in `config.htpasswd.clusterAdmins`. It installs no operator.
Placement reads the `autoshift.io/htpasswd` label.

| Policy | What it does |
|---|---|
| `policy-htpasswd` | Rebuilds the HTPasswd entries in `OAuth/cluster` from config, and manages the `autoshift-cluster-admins` Group and its `cluster-admin` binding |
| `policy-htpasswd-ready` | Inform only. Checks that each provider's Secret exists, that `OAuth/cluster` carries each provider, and that the `authentication` ClusterOperator is Available and not Degraded |

AutoShift does **not** create the htpasswd Secrets. Provision them out of band, through External
Secrets Operator or by hand, so no credential material lives in the values files.

## Ownership of HTPasswd providers

On a cluster with this policy placed, AutoShift owns **every** identity provider of type `HTPasswd`.
On each evaluation the policy reads the live `OAuth/cluster` on the managed cluster, carries over
every provider of another type (LDAP, OpenID Connect, and so on) unchanged, and replaces the
HTPasswd entries with exactly what `config.htpasswd.providers` declares. The rest of the OAuth spec,
such as `templates` and `tokenConfig`, is carried over as well.

- An HTPasswd provider added by hand is removed. Declare it in config instead; an entry only needs a
  name and a Secret name.
- Renaming a provider or changing its `secretName` replaces the old entry, rather than leaving a
  duplicate behind.
- Clusters without `autoshift.io/htpasswd: 'true'` are never touched.

The policy uses `mustonlyhave` because `musthave` merges into lists. Under `musthave`, a rename
appends a second entry, duplicate names degrade the authentication operator, and the policy still
reports Compliant. See [compliance types](../../../docs/policy-behavior.md#compliance-types).

A provider is written only once its Secret exists in `openshift-config`, and is removed again if the
Secret is deleted. A provider pointing at a missing Secret would otherwise degrade the authentication
operator. The order in which the Secret and the policy arrive does not matter. While a Secret is
missing, `policy-htpasswd-ready` reports it by name.

Removing a user from `clusterAdmins` removes them from the `autoshift-cluster-admins` Group, which
revokes their `cluster-admin`. AutoShift owns that Group, so a member added by hand is removed.

`pruneObjectBehavior` is `None`: disabling the policy leaves the identity providers and the
`cluster-admin` binding in place.

### When the policy fails

`policy-htpasswd` reports NonCompliant with a message naming the cause, rather than reporting
Compliant with nothing applied, when:

- `config.htpasswd.providers` is missing or empty
- a provider entry lacks `name` or `secretName`
- two providers share a name
- a provider name is already used by a non-HTPasswd identity provider on the cluster
- `OAuth/cluster` cannot be read, in which case the identity provider list is left untouched

## Placement

`placement.yaml` selects clusters labeled `autoshift.io/htpasswd: 'true'`. Both policies share it.
Labels are defined in values files only, never directly on managed clusters:

```yaml
hubClusterSets:
  hub:
    labels:
      htpasswd: 'true'
```

## Provision the Secrets

Each provider needs a Secret in `openshift-config` on the managed cluster, holding the htpasswd file
under the key `htpasswd`.

```bash
htpasswd -cBb users.htpasswd admin '<password>'
htpasswd -Bb  users.htpasswd developer '<password>'

oc -n openshift-config create secret generic cluster-admins-htpass \
  --from-file=htpasswd=users.htpasswd
```

For a fleet, provision them through External Secrets Operator: an `ExternalSecret` pointing at your
secret store, such as Vault or AWS Secrets Manager, with the `htpasswd` key. Rotating a password
then means updating the store, not the cluster.

## Configuration

The provider list is structured data, so it lives under `config.htpasswd` and reaches the managed
cluster through the `rendered-config` ConfigMap.

```yaml
hubClusterSets:
  hub:
    config:
      htpasswd:
        providers:                                # at least one is required
          - name: 'Cluster Admins'                # display name on the login page, unique
            secretName: 'cluster-admins-htpass'   # existing Secret in openshift-config
        clusterAdmins:                            # members of autoshift-cluster-admins
          - admin
```

## Layout

```
htpasswd/
  kustomization.yaml            # entry point: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator: policy graph, remediation, evaluation interval
  placement.yaml                # Placement predicate and tolerations
  manifests/
    htpasswd/
      oauth-htpasswd.yaml       # rebuilds the HTPasswd entries in OAuth/cluster
      cluster-admins.yaml       # autoshift-cluster-admins Group and its cluster-admin binding
  test/
    htpasswd-ready.yaml         # inform: Secrets, OAuth entries, authentication ClusterOperator
```

## Test locally

```bash
# Render this directory as the repo-server CMP does (needs: make install-policy-generator)
scripts/pg-render.sh policies/stable/htpasswd

# Full validation: render, hub and spoke template resolution, label contract
make test
```

`tools/testdata/htpasswd-resources.yaml` stubs `OAuth/cluster`, with an LDAP provider that must
survive and a stale HTPasswd provider that must be dropped, and the Secret named in `_example.yaml`.

## Verify

```bash
oc get oauth cluster -o jsonpath='{range .spec.identityProviders[*]}{.name}{"\t"}{.type}{"\n"}{end}'
oc get group autoshift-cluster-admins -o jsonpath='{.users}'
oc get clusteroperator authentication
```
