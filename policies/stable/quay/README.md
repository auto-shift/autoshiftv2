# quay AutoShift Policy

## Overview

Installs the Red Hat Quay operator, deploys a `QuayRegistry`, and optionally provisions its
databases through CloudNativePG. Label and `config.quay` reference lives in
[docs/values-reference.md](../../../docs/values-reference.md).

Enable it with `quay: 'true'` in a clusterset or cluster values file. Everything else is optional
and the shipped defaults reproduce a single all-managed registry.

## Policies

| Policy | Placement | Purpose |
|---|---|---|
| `policy-quay-operator-install` | `quay` | Operator subscription |
| `policy-cnpg-quay` | `quay` + `cloudnative-pg` + `quay-db-mode: managed` | `quay-db` and `clair-db` CloudNativePG clusters |
| `policy-cnpg-quay-test` | same | Database readiness gate (inform) |
| `policy-quay-deploy` | `quay` + `quay-db-mode` not `managed` | Config bundle, certificate, `QuayRegistry` |
| `policy-quay-deploy-cnpg` | `quay` + `quay-db-mode: managed` | Same manifests, additionally gated on database readiness |
| `policy-quay-test` | `quay` | Registry readiness gate (inform) |
| `policy-quay-configure` | `quay` | Console link and registry host ConfigMap |
| `policy-cnpg-quay-backup` | `quay` + `quay-db-mode: managed` + `quay-db-backups` + `odf` | Scheduled backups to a NooBaa bucket |

`policy-quay-deploy` and `policy-quay-deploy-cnpg` render the same manifests under mutually
exclusive placements, so exactly one lands on any cluster. The split exists so the database
readiness dependency applies only where CloudNativePG is actually in use.

## No Jobs

This chart runs no Jobs. Earlier versions shipped a bootstrap Job that created a `quayadmin` user,
a `quaydevel` user, a `devel` organization, and an `example` repository, calling the registry API
with certificate verification disabled. That Job is gone, along with the sample content and the
`quayadmin` Secret it produced.

Creating the first user is an API-only operation in Red Hat Quay, so there are two supported paths.

**OpenID Connect (recommended).** Configure a provider in `config.quay.config` and list the
administrator user names in `config.quay.superUsers`. Superusers named in `config.yaml` before
deployment receive administrative access on first login with no bootstrap step.

**Manual bootstrap (proof of concept).** Set both flags, wait for the registry to roll out, then
call the endpoint once:

```yaml
config:
  quay:
    bootstrap:
      userInitialize: true
      xhrOnly: false
```

```bash
HOST=$(oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}')
curl -X POST "https://${HOST}/api/v1/user/initialize" \
  -H 'Content-Type: application/json' \
  --data '{"username":"quayadmin","password":"<choose-one>","email":"quayadmin@example.com","access_token":true}'
```

Store the returned `access_token` somewhere safe, then set both flags back to their defaults. The
endpoint refuses to run once any user exists, so it cannot be replayed.

## Pushing to this registry

Use a robot account rather than a human account for automation.

1. Log in to the registry route as a superuser.
2. Create or open an organization, for example `autoshift`.
3. Select **Robot Accounts**, then **Create Robot Account**, for example `release`. The resulting
   account name is `autoshift+release`.
4. Grant it **Write** on the repositories it publishes, or **Admin** on the organization if it must
   create repositories on first push.
5. Open the robot account and select **Credentials**. The panel offers a Docker login command and a
   downloadable Kubernetes Secret.

```bash
HOST=$(oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}')
podman login "$HOST" -u 'autoshift+release' -p '<robot-token>'
helm registry login "$HOST" -u 'autoshift+release' -p '<robot-token>'
```

Red Hat Quay creates repositories on first push when the account has permission to do so, but it
does not create organizations. Create the organization before the first push.

For Argo CD to pull Helm charts from this registry, create a repository Secret rather than a pull
secret:

```bash
oc create secret generic quay-charts -n openshift-gitops \
  --from-literal=type=helm \
  --from-literal=url="$HOST" \
  --from-literal=enableOCI=true \
  --from-literal=username='autoshift+release' \
  --from-literal=password='<robot-token>'
oc label secret quay-charts -n openshift-gitops argocd.argoproj.io/secret-type=repository
```

## Trusting the registry across the cluster

Pulling images from this registry on every node needs two things: credentials in the global pull
secret, and trust for the registry certificate.

**Certificate trust** is already handled when the registry is used as a mirror. The
`disconnected-mirror` policy writes `config.disconnected.mirrorRegistry.ca` into the
`autoshift-registry-ca` ConfigMap in `openshift-config` and points
`image.config.openshift.io/cluster` at it. Set `config.disconnected.mirrorRegistry.host` to the
registry route and supply the certificate authority through `ca` or `caRef`. If the registry
certificate comes from a public authority, no trust configuration is required.

**Credentials** are not managed by AutoShift. Add the robot account to the global pull secret:

```bash
HOST=$(oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}')
oc get secret/pull-secret -n openshift-config \
  --template='{{index .data ".dockerconfigjson" | base64decode}}' > /tmp/pull-secret.json
oc registry login --registry="$HOST" \
  --auth-basic='autoshift+release:<robot-token>' --to=/tmp/pull-secret.json
oc set data secret/pull-secret -n openshift-config \
  --from-file=.dockerconfigjson=/tmp/pull-secret.json
rm -f /tmp/pull-secret.json
```

The Machine Config Operator distributes the updated secret to every node. Watch the rollout and
confirm the pools settle before relying on the new credentials:

```bash
oc get mcp -w
```

> [!NOTE]
> Whether this rollout restarts nodes depends on the OpenShift version. Check the release
> documentation for your version before running it on a production cluster, and schedule it
> accordingly.

Use a robot account with read permission only for the global pull secret. The account that pushes
releases should not be the account every node authenticates with.

## Upgrading from the earlier chart

Removing a manifest from a policy does not delete the object it created, so the bootstrap Job and
its supporting resources survive an upgrade. Remove them once:

```bash
oc delete job create-admin-user -n quay-enterprise --ignore-not-found
oc delete serviceaccount create-admin-user -n quay-enterprise --ignore-not-found
oc delete role create-admin-user -n quay-enterprise --ignore-not-found
oc delete rolebinding create-admin-user -n quay-enterprise --ignore-not-found
```

The `quayadmin`, `quaydevel`, `quay-pull-secret`, and `quay-integration` Secrets that the Job
created are also left behind. Keep them if something still consumes those credentials, otherwise
delete them.

The upgrade rewrites the config bundle to the secure defaults, which disables the unauthenticated
first user endpoint. Confirm you have another way in, through OpenID Connect or an existing
account, before upgrading a registry whose only administrator was created by the old Job.

> [!IMPORTANT]
> Switching `quay-db-mode` from `bundled` to `managed` provisions empty databases. It is a
> migration, not a live cutover: Red Hat Quay will not start against an empty schema that holds no
> registry data. Move the data with PostgreSQL tooling, or make the switch on a new registry.
