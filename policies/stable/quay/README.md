# quay AutoShift Policy

## Overview

Installs the Red Hat Quay operator, deploys a `QuayRegistry`, and optionally provisions its
databases through CloudNativePG. Label and `config.quay` reference lives in
[docs/values-reference.md](../../../docs/values-reference.md).

Enable it with `quay: 'true'` in a clusterset or cluster values file. Everything else is optional
and the shipped defaults reproduce a single all-managed registry.

## Before you start

Everything except credentials is declared in values files. You create by hand only what carries a
secret, plus the operators Quay depends on.

| You need | When | How |
|---|---|---|
| Object storage | Always | `odf: 'true'` with `odf-multi-cloud-gateway: 'standalone'` for the object gateway only, `odf: 'true'` alone for full ODF, or bring your own S3-compatible bucket and set `components.objectstorage: false` |
| CloudNativePG | `quay-db-mode: managed` | `cloudnative-pg: 'true'` |
| cert-manager and an issuer | Only if you set `config.quay.tls` | `cert-manager: 'true'`. The policy emits the `Certificate` only when the named issuer already exists. `policy-quay-tls-issuer` reports if it does not |
| ODF | `quay-db-backups: 'true'` | Backups go to a NooBaa bucket through an ObjectBucketClaim |

## Getting started

### 1. Pick a database mode

`autoshift.io/quay-db-mode` decides which policies land on the cluster:

| Mode | Runs PostgreSQL | Also needs |
|---|---|---|
| `bundled` (default) | The Quay Operator | Nothing |
| `managed` | CloudNativePG, as `quay-db` and `clair-db` | `cloudnative-pg: 'true'`, and unlocks `quay-db-backups` |
| `external` | You | `DB_URI`, supplied in the Secret in step 3 |

### 2. Decide where images are stored

The Quay Operator manages object storage by default. Managed storage needs the `ObjectBucketClaim`
API, which is provided by NooBaa or by Red Hat OpenShift Data Foundation, so the full Ceph stack is
not required.

The lighter option is the object gateway on its own:

```yaml
labels:
  odf: 'true'
  odf-multi-cloud-gateway: 'standalone'
```

That deploys a NooBaa-only `StorageCluster` with no Ceph block or file storage. Set
`odf-multi-cloud-gateway: 'standard'` for full OpenShift Data Foundation instead. The two are
entitled differently, so pick the one your subscription covers. Either satisfies Quay, because both
create `ocs-storagecluster`, which is what the readiness gate Quay depends on checks.

To use your own bucket instead, set `components.objectstorage: false` and supply
`DISTRIBUTED_STORAGE_CONFIG` in the Secret in step 3. Managed and unmanaged components are explained
in [Introduction to the Red Hat Quay Operator](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/deploying_the_red_hat_quay_operator_on_openshift_container_platform/operator-concepts).

### 3. Create the configuration Secret

Non-sensitive keys go in `config.quay.config` in your values file. Anything secret goes in a Secret
you create on the cluster, because values files live in git. AutoShift merges that Secret into the
config bundle last, so it wins over everything declared in values.

This is where object-storage credentials, `DB_URI` for `quay-db-mode: external`, and OIDC client
secrets belong.

`policy-quay-deploy` creates the `quay-enterprise` namespace, so create it yourself first if you
want the Secret in place before Quay rolls out. The policy adopts an existing namespace.

```bash
oc create namespace quay-enterprise

cat > /tmp/quay-secrets.yaml <<'EOF'
DISTRIBUTED_STORAGE_CONFIG:
  default:
    - RadosGWStorage
    - access_key: '<key>'
      secret_key: '<secret>'
      bucket_name: 'quay-registry'
      hostname: 's3.example.com'
      is_secure: true
      port: 443
      storage_path: /datastorage/registry
DISTRIBUTED_STORAGE_PREFERENCE:
  - default
EOF

oc create secret generic quay-config-extra -n quay-enterprise \
  --from-file=config.yaml=/tmp/quay-secrets.yaml
rm -f /tmp/quay-secrets.yaml
```

Then point at it from your values file:

```yaml
config:
  quay:
    configSecretRef:
      name: quay-config-extra
      namespace: quay-enterprise
      key: config.yaml
```

The file is a `config.yaml` fragment, not a whole configuration. Field names and accepted values are
in [Required configuration fields](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-fields-required-intro).

### 4. Enable Quay

```yaml
labels:
  quay: 'true'
  # only when quay-db-mode is managed
  cloudnative-pg: 'true'
  quay-db-mode: 'managed'
```

Everything else is optional, and the shipped defaults produce a single all-managed registry.

### 5. Watch it come up

```bash
oc get policy -n policies-<release> | grep -E 'quay|cnpg'
oc get quayregistry registry -n quay-enterprise
oc get pods -n quay-enterprise
oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}{"\n"}'
```

`policy-quay-test` stays NonCompliant until the `QuayRegistry` reports available with every
component created, so it is the one to watch.

### 6. Sign in

No user exists yet. Creating the first one is an API-only operation in Red Hat Quay, so it happens
either through OIDC or through a one-time API call. Both paths are in [No Jobs](#no-jobs).

## Policies

| Policy | Placement | Purpose |
|---|---|---|
| `policy-quay-operator-install` | `quay` | Operator subscription |
| `policy-cnpg-quay` | `quay` + `cloudnative-pg` + `quay-db-mode: managed` | `quay-db` and `clair-db` CloudNativePG clusters |
| `policy-cnpg-quay-test` | same | Database readiness gate (inform) |
| `policy-quay-deploy` | `quay` + `quay-db-mode` not `managed` | Config bundle, certificate, `QuayRegistry` |
| `policy-quay-deploy-cnpg` | `quay` + `quay-db-mode: managed` | Same manifests, additionally gated on database readiness |
| `policy-quay-test` | `quay` | Registry readiness gate (inform) |
| `policy-quay-tls-issuer` | `quay` | Reports when cert-manager TLS was requested but is not in effect (inform) |
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

The `access_token` expires about 150 minutes after it is issued, as recorded in the Red Hat request
for enhancement [RFE-6177](https://issues.redhat.com/browse/RFE-6177). Use it for the setup that follows right
away, or create a long-lived token as described in
[Create the organization and robot accounts](#create-the-organization-and-robot-accounts).

## Pushing to this registry

Use robot accounts rather than human accounts for automation, and give each job its own account:

| Robot account | Used by | Access |
|---|---|---|
| `autoshift+release` | `make release`, to push the AutoShift charts | Writes to every repository in the organization |
| `autoshift+argocd` | Argo CD, to pull the AutoShift charts | Reads every repository in the organization, and nothing else |

Repository permissions in Red Hat Quay are set per repository, and new charts add repositories over
time. The write account therefore gets its access at the organization level, through a team with
the `creator` role, and both accounts get organization default permissions, which Red Hat Quay
applies to every repository created later.

### Create the organization and robot accounts

Every call needs a superuser OAuth token. The `access_token` returned when you created the first
user works until it expires, about 150 minutes after it was issued. For anything later, create a
long-lived token in the web console:

1. Log in as a superuser and open the organization.
2. Select **Applications**, then **Create New Application**, and name it, for example `automation`.
3. Open the application, select **Generate Token**, and grant the permissions to administer the
   organization, administer repositories, create repositories, and act as a superuser.
4. Store the token in a Secret. It is shown only once.

A request with an expired or missing token fails with `403` and
`CSRF token was invalid or missing`, because Red Hat Quay then treats it as a browser call.

Red Hat Quay never creates organizations on push, so this runs once before the first release.

```bash
HOST=$(oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}')
API="https://${HOST}/api/v1"
ORG=autoshift
AUTH="Authorization: Bearer ${QUAY_TOKEN}"   # superuser OAuth token
JSON='Content-Type: application/json'

# Organization
curl -sf -X POST "${API}/organization/" -H "$AUTH" -H "$JSON" \
  --data "{\"name\":\"${ORG}\",\"email\":\"${ORG}@example.com\"}"

# Robot accounts
curl -sf -X PUT "${API}/organization/${ORG}/robots/release" -H "$AUTH" -H "$JSON" \
  --data '{"description":"make release: pushes AutoShift charts"}'
curl -sf -X PUT "${API}/organization/${ORG}/robots/argocd" -H "$AUTH" -H "$JSON" \
  --data '{"description":"Argo CD: pulls AutoShift charts (read only)"}'

# Write account: organization-level team that may create repositories
curl -sf -X PUT "${API}/organization/${ORG}/team/releasers" -H "$AUTH" -H "$JSON" \
  --data '{"role":"creator","description":"Creates repositories on push"}'
curl -sf -X PUT "${API}/organization/${ORG}/team/releasers/members/${ORG}+release" -H "$AUTH"

# Default permissions for every repository created from now on
curl -sf -X POST "${API}/organization/${ORG}/prototypes" -H "$AUTH" -H "$JSON" \
  --data "{\"role\":\"write\",\"delegate\":{\"kind\":\"user\",\"name\":\"${ORG}+release\"}}"
curl -sf -X POST "${API}/organization/${ORG}/prototypes" -H "$AUTH" -H "$JSON" \
  --data "{\"role\":\"read\",\"delegate\":{\"kind\":\"user\",\"name\":\"${ORG}+argocd\"}}"
```

A repository pushed by `autoshift+release` is private, the release account becomes its
administrator, and `autoshift+argocd` gets read on it. The `write` default permission covers
repositories that someone else creates first.

Default permissions apply only to repositories created after them. For a repository that already
exists, grant the account directly:

```bash
curl -sf -X PUT "${API}/repository/${ORG}/<repository>/permissions/user/${ORG}+argocd" \
  -H "$AUTH" -H "$JSON" --data '{"role":"read"}'
```

Read a robot token back at any time:

```bash
curl -sf "${API}/organization/${ORG}/robots/release" -H "$AUTH" | jq -r .token
```

### Log in before a release

Log in with the write account before running `make release`, so the push uses it:

```bash
curl -sf "${API}/organization/${ORG}/robots/release" -H "$AUTH" | jq -r .token |
  helm registry login "$HOST" -u "${ORG}+release" --password-stdin
```

`--password-stdin` keeps the token out of the shell history and the process list. The same token
works with `podman login`.

### Create the repositories before the first release

`helm push` cannot create a repository in Red Hat Quay. Before it uploads, Helm checks whether the
chart already exists, and for a repository that does not exist yet Red Hat Quay answers
`401 Unauthorized` instead of `404`. The push stops there with:

```
failed to perform "Exists" on destination: HEAD ".../manifests/sha256:...": response status code 401: Unauthorized
```

Create every chart repository once, before the first `make release`. A dry run builds the charts, so
it gives the full list:

```bash
VERSION=X.Y.Z
make release VERSION="$VERSION" REGISTRY="$HOST" REGISTRY_NAMESPACE="$ORG" DRY_RUN=true

{ for f in .helm-charts/bootstrap/*-"$VERSION".tgz; do echo "bootstrap/$(basename "$f" -"$VERSION".tgz)"; done
  echo autoshift
  for f in .helm-charts/policies/*-"$VERSION".tgz; do echo "policies/$(basename "$f" -"$VERSION".tgz)"; done
} | while read -r repo; do
  curl -s -o /dev/null -w "%{http_code} ${repo}\n" -X POST "${API}/repository" -H "$AUTH" -H "$JSON" \
    --data "{\"namespace\":\"${ORG}\",\"repository\":\"${repo}\",\"visibility\":\"private\",\"description\":\"\",\"repo_kind\":\"image\"}"
done
```

`201` means created, `400` means the repository already exists, and `403` means the token has
expired. The organization default
permissions apply to these repositories as well, so `autoshift+release` can write and
`autoshift+argocd` can read. Later releases need this step only when a new policy adds a chart.

### Let Argo CD pull the charts

Argo CD reads registry credentials from a `repo-creds` Secret whose URL starts with `oci://`. Use
the read account:

```bash
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: autoshift-oci-creds
  namespace: openshift-gitops
  labels:
    argocd.argoproj.io/secret-type: repo-creds
stringData:
  type: helm
  url: oci://${HOST}/${ORG}
  enableOCI: "true"
  username: ${ORG}+argocd
EOF
oc set data secret/autoshift-oci-creds -n openshift-gitops \
  --from-file=password=<(curl -sf "${API}/organization/${ORG}/robots/argocd" -H "$AUTH" | jq -j .token)
```

The registry route usually carries the cluster wildcard certificate. If that certificate comes from
a private certificate authority, Argo CD also needs it. Both steps, and the second credential that
`install-autoshift.sh` needs, are in
[Private registry authentication](../../../docs/releases.md#private-registry-authentication).

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
  --auth-basic='autoshift+argocd:<robot-token>' --to=/tmp/pull-secret.json
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


## Red Hat documentation

- [Deploying the Red Hat Quay Operator on OpenShift Container Platform](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/deploying_the_red_hat_quay_operator_on_openshift_container_platform/index)
- [Introduction to the Red Hat Quay Operator](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/deploying_the_red_hat_quay_operator_on_openshift_container_platform/operator-concepts), for managed compared with unmanaged components and the config bundle Secret
- [Configure Red Hat Quay](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/index)
- [Required configuration fields](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-fields-required-intro), including object storage and database fields
- [Automation configuration options](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/configure_red_hat_quay/config-preconfigure-automation-intro), for `SUPER_USERS`, `FEATURE_USER_INITIALIZE` and `BROWSER_API_CALLS_XHR_ONLY`
- [Configuring OIDC for Red Hat Quay](https://docs.redhat.com/en/documentation/red_hat_quay/3.18/html/manage_red_hat_quay/configuring-oidc-authentication)
