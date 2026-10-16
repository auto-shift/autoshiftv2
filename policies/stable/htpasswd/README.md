# htpasswd AutoShift Policy

## Overview
Configures one or more **HTPasswd identity providers** on the cluster's `OAuth/cluster` CR by using
AutoShift's ACM **PolicyGenerator** pattern. The directory is a Kustomize source:
`policy-generator-config.yaml` wraps the manifests under `manifests/` into an ACM `Policy`, and pairs
it with the hand-authored `placement.yaml`.

Unlike most policies here this installs no operator. It reads `config.htpasswd` from the cluster's
`rendered-config` ConfigMap and adds each provider to the OAuth CR, referencing an htpasswd
`Secret` that already exists in `openshift-config`. Usernames listed under `clusterAdmins` also get
a `cluster-admin` ClusterRoleBinding.

AutoShift does **not** create the htpasswd Secrets. They are provisioned out of band — via External
Secrets Operator (ESO) or manually — so no credential material lives in the values files.

## Layout
```
htpasswd/
  kustomization.yaml            # entrypoint: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator (policy graph, remediation, eval interval)
  placement.yaml                # Placement predicate (autoshift.io/htpasswd) + tolerations
  manifests/
    htpasswd/
      htpasswd.yaml             # object-templates-raw: OAuth patch + ClusterRoleBindings
                                #   from config.htpasswd
```

The OAuth identity providers are merged with `musthave`, so providers managed outside AutoShift are
left in place.

## Test Locally
```bash
# Render the policy exactly as the CMP/CI does (needs: make install-policy-generator)
KUSTOMIZE_PLUGIN_HOME=$PWD/.tools/kustomize-plugin .tools/kustomize build \
  --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone \
  policies/stable/htpasswd

# Full validation (helm render + hub/spoke template resolution + label contract)
cd tools && go test -tags integration -count=1 ./internal/resolver/...
```
The `${POLICY_NAMESPACE}`, `${REMEDIATION}`, `${EVAL_COMPLIANT}`, `${EVAL_NONCOMPLIANT}` tokens are
substituted per-deployment by the repo-server CMP before `kustomize build` runs; leave them as-is.

## Enable on Clusters
Labels are defined in values files only — never directly on managed clusters. The cluster-labels
policy propagates them, and this policy's `placement.yaml` selects clusters with
`autoshift.io/htpasswd: 'true'`.

```yaml
# In autoshift/values/clustersets/hub.yaml (or another clusterset / per-cluster file)
hubClusterSets:
  hub:
    labels:
      htpasswd: 'true'
```

## Provision the Secrets
Each provider needs an htpasswd `Secret` in `openshift-config` on the spoke cluster. Create it
before enabling the policy — AutoShift references it by name and never writes credential material.

```bash
# Generate the htpasswd file
htpasswd -cBb users.htpasswd admin '<password>'
htpasswd -Bb  users.htpasswd developer '<password>'

# Create the Secret on the spoke cluster
oc -n openshift-config create secret generic cluster-admins-htpass \
  --from-file=htpasswd=users.htpasswd
```

For fleet use, provision these through **External Secrets Operator** instead — an `ExternalSecret`
pointing at your secret store (Vault, AWS Secrets Manager, etc.) with the `htpasswd` key. Rotating
the password then means updating the store, not the cluster.

## Configuration
The provider list is data, not labels, so it lives under `config.htpasswd` and reaches the spoke
through the `rendered-config` ConfigMap.

```yaml
hubClusterSets:
  hub:
    config:
      htpasswd:
        providers:
          - name: 'Cluster Admins'               # display name on the login page
            secretName: 'cluster-admins-htpass'  # existing Secret in openshift-config
        clusterAdmins:                           # granted cluster-admin ClusterRoleBinding
          - admin
```

## Verify
```bash
oc get oauth cluster -o jsonpath='{.spec.identityProviders[*].name}'
oc get secret -n openshift-config | grep htpass
oc get clusterrolebinding | grep autoshift-cluster-admin-
```
