# AutoShift CI/CD

AutoShift builds, signs and deploys itself on the Red Hat Advanced Developer Suite components that it
installs on a hub: Red Hat OpenShift Pipelines runs the pipelines, GitHub or GitLab hosts the source
and the site configuration, Red Hat Quay stores the charts, and Red Hat Trusted Artifact Signer
signs them with an identity from the Red Hat build of Keycloak. The source and the site
configuration are chosen separately, so a customer's own GitHub repository can hold the site
configuration while AutoShift builds from upstream, or from the hub's GitLab.

## Production and evaluation

In production you own your copy of AutoShift. Clone it into a repository on your GitHub or GitLab,
add a webhook from that repository to your hub, keep your site configuration in a second repository,
and publish to your own OCI registry. Every one of those is set in `config.autoshift-cicd`, and none
of them has to run on the hub.

To evaluate the CI/CD without any of that, the hub can do it all itself: it polls the upstream
AutoShift repository into its own GitLab, keeps the site configuration in that GitLab, and publishes
to its own Quay. `clustersets/rhads.yaml` is set up this way.

| | Production | Evaluation |
|---|---|---|
| Source | Your repository, with a webhook to the hub: `source.provider`, `source.repo`, `source.secret` | The upstream repository, polled into the hub's GitLab: `source.pollSchedule` |
| Site configuration | Your repository: `siteConfig.provider`, `siteConfig.repo`, `siteConfig.secret` | The hub's GitLab `site-config` project |
| Registry | Your OCI registry: `registry.host`, `registry.namespace`, `registry.pushSecret` | The hub's Quay, with the `bootstrap-quay` robot |
| Pipelines run from | `.tekton/` on the default branch: `source.provenance: default_branch` | `.tekton/` on the pushed branch: `source.provenance: source` |

Most of the setup is policy. Steps that need a credential an administrator holds, or that change a
system AutoShift cannot reach through a Kubernetes resource, are bootstrap pipelines you start with
parameters. Every step also has a manual procedure on this page, for a hub where you prefer to run
it by hand or need to understand what the automation does.

## What runs where

| Step | Delivered by | Manual procedure |
|---|---|---|
| `autoshift-ci` namespace, Tasks and Pipelines | `policy-autoshift-cicd-bootstrap` | Not applicable |
| Endpoints, trust bundle and GitLab token in `autoshift-ci` | `policy-autoshift-cicd-endpoints` | [Endpoints and trust](#endpoints-and-trust) |
| The registry the pipelines publish to, and its push credential | `policy-autoshift-cicd-endpoints` | [Publishing registry](#publishing-registry) |
| Quay organization, CI robot and push Secret, when publishing to the hub's Quay | `bootstrap-quay` pipeline | [Quay organization and robot](#quay-organization-and-robot) |
| Pipelines as Code Repository for the source on GitHub or GitLab | `policy-autoshift-cicd-source` | [Source repository](#source-repository) |
| The site configuration repository, its branch and push credential, on GitHub or GitLab | `policy-autoshift-cicd-endpoints` | [Site configuration repository](#site-configuration-repository) |
| GitLab group and the projects config places on the hub's GitLab, with the Pipelines as Code webhook | `bootstrap-gitlab` pipeline | [GitLab group, projects and webhook](#gitlab-group-projects-and-webhook) |
| Upstream branches mirrored into the GitLab `autoshift` project | `bootstrap-gitlab` and `sync-upstream` pipelines | [Upstream mirror](#upstream-mirror) |
| Polling upstream, for evaluation | `poll-upstream` CronJob, from `policy-autoshift-cicd-bootstrap` | [Poll instead of a webhook](#poll-instead-of-a-webhook) |
| The site configuration seeded from the AutoShift values | `bootstrap-site-config` pipeline | [Site configuration repository](#site-configuration-repository) |
| Signing client secret and the `autoshift-ci` Keycloak client | `policy-keycloak-realm-secrets` and `policy-keycloak-realm-import` | [Signing identity](#signing-identity) |
| Test, publish, sign and verify every branch push | `.tekton/autoshift-push.yaml`, run by Pipelines as Code | [Push pipeline](#push-pipeline) |
| Deploy each signed build as its own AutoShift, on versioned ClusterSets | `policy-autoshift-cicd-deployments` and the push pipeline's deploy task | [Deployments](#deployments) |
| The signing client in a realm imported before the client existed | `bootstrap-keycloak` pipeline | [Add the client to an existing realm](#add-the-client-to-an-existing-realm) |

## Prerequisites

Turn these labels on for the hub clusterset in your values file. `clustersets/rhads.yaml` sets all
of them:

```yaml
hubClusterSets:
  hub:
    labels:
      pipelines: 'true'
      quay: 'true'
      gitlab: 'true'
      dev-hub: 'true'
      keycloak: 'true'
      tas: 'true'
      autoshift-cicd: 'true'
```

`dev-hub` is required because the GitLab access token the pipelines use comes from the GitLab
policy's Red Hat Developer Hub integration. The pipeline defaults and the source repository come
from `config.autoshift-cicd`; the [policy README](../policies/stable/autoshift-cicd/README.md) lists
every key.

Run the procedures as a user with the `cluster-admin` role, with `oc`, `curl`, `jq` and `git`
installed. The examples use these variables:

```bash
export DOMAIN=$(oc get ingress.config.openshift.io cluster -o jsonpath='{.spec.domain}')
export QUAY=$(oc get route registry-quay -n quay-enterprise -o jsonpath='{.spec.host}')
export GITLAB=https://gitlab.${DOMAIN}
export KEYCLOAK=https://$(oc get keycloak keycloak -n keycloak-system -o jsonpath='{.spec.hostname.hostname}')
export REALM=tssc-iam
```

## Starting a bootstrap pipeline

Start a pipeline from the console, under **Pipelines** in the `autoshift-ci` project, or with the
`tkn` CLI. Every parameter has a default, and every task checks before it creates, so each pipeline is
safe to run again:

```bash
tkn pipeline start bootstrap-quay -n autoshift-ci -p initialize=true --use-param-defaults --showlog
tkn pipeline start bootstrap-gitlab -n autoshift-ci --use-param-defaults --showlog
tkn pipeline start bootstrap-site-config -n autoshift-ci --use-param-defaults --showlog
tkn pipeline start bootstrap-keycloak -n autoshift-ci --use-param-defaults --showlog
```

## Endpoints and trust

`policy-autoshift-cicd-endpoints` gathers what the tasks read, so a task never looks outside
`autoshift-ci`. To create the same objects by hand:

1. Create the `autoshift-ci-endpoints` ConfigMap. Include only the keys for the components you run:

    | Key | Value |
    |---|---|
    | `pacUrl` | `https://` and the host of Route `openshift-pipelines/pipelines-as-code-controller` |
    | `quayHost` | The host of Route `quay-enterprise/registry-quay` |
    | `gitlabUrl` | `https://` and the `host` key of Secret `gitlab-system/tssc-gitlab-credentials` |
    | `fulcioUrl`, `rekorUrl`, `tufUrl` | `status.url` of the `Fulcio`, `Rekor` and `Tuf` resources in `tssc-tas` |
    | `oidcIssuer` | `${KEYCLOAK}/realms/${REALM}` |
    | `signerIdentity` | `autoshift-ci@${DOMAIN}` |
    | `registryHost`, `registryNamespace` | The registry and the organization or project the pipelines publish to; see [Publishing registry](#publishing-registry) |
    | `registryPushSecret` | The `kubernetes.io/dockerconfigjson` Secret in `autoshift-ci` that pushes there |
    | `siteConfigUrl`, `siteConfigBranch` | The site configuration repository and branch; see [Site configuration repository](#site-configuration-repository) |
    | `siteConfigSecret`, `siteConfigSecretKey` | The Secret in `autoshift-ci`, and its key, holding a token that can push there |
    | `siteConfigUser` | The HTTPS user sent with that token: `oauth2` for GitLab, any value for GitHub |

2. Create the `autoshift-ci-ca` ConfigMap with the ingress CA, so the tasks verify TLS against the
   cluster routes:

    ```bash
    oc get configmap default-ingress-cert -n openshift-config-managed \
      -o jsonpath='{.data.ca-bundle\.crt}' > ingress-ca.crt
    oc create configmap autoshift-ci-ca -n autoshift-ci --from-file=ingress-ca.crt
    ```

3. Copy the GitLab token into `autoshift-ci` as Secret `gitlab-autoshift-token`, with the `host` and
   `token` keys of `gitlab-system/tssc-gitlab-credentials`. Any GitLab personal access token with the
   `api` scope works.

## Publishing registry

The pipelines publish the AutoShift charts, their signatures and attestations to an OCI registry
set in `config.autoshift-cicd.registry`. Any registry that accepts OCI artifacts works:

```yaml
config:
  autoshift-cicd:
    registry:
      host: registry.example.com          # empty: the hub's Quay
      namespace: autoshift                # organization or project
      pushSecret: autoshift-registry      # Secret in autoshift-ci
```

With a `host`, create the push Secret from an account that can create and push repositories in
that namespace. `policy-autoshift-cicd-endpoints` fails, naming the Secret, until it exists:

```bash
oc create secret docker-registry autoshift-registry -n autoshift-ci \
  --docker-server=registry.example.com --docker-username=<user> --docker-password=<token>
```

With no `host`, the pipelines publish to the hub's Quay, in the organization `quayOrg`, with the
robot account and Secret that `bootstrap-quay` creates.

## Quay organization and robot

This section applies when the pipelines publish to the hub's Quay.

`bootstrap-quay` creates an organization, a robot account that can create and push repositories in
it, and the robot's push Secret. It needs a Quay superuser OAuth token in Secret
`autoshift-ci/quay-admin-token`, key `token`. On a new registry with no users, start it with
`initialize=true` instead: it creates the first administrator through the user initialize endpoint,
which works while `config.quay.bootstrap.userInitialize` is on, and stores that user in Secret
`autoshift-ci/quay-admin`.

To do the same by hand, with `TOKEN` set to a superuser OAuth token:

1. Create the organization:

    ```bash
    curl -sf -X POST "https://${QUAY}/api/v1/organization/" -H "Authorization: Bearer ${TOKEN}" \
      -H 'Content-Type: application/json' -d '{"name": "autoshift"}'
    ```

2. Create the robot account:

    ```bash
    curl -sf -X PUT "https://${QUAY}/api/v1/organization/autoshift/robots/ci" \
      -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
      -d '{"description": "AutoShift CI push robot"}'
    ```

3. Create a team with the `creator` role and add the robot, so it can create the repositories it
   pushes:

    ```bash
    curl -sf -X PUT "https://${QUAY}/api/v1/organization/autoshift/team/cicreators" \
      -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
      -d '{"role": "creator", "description": "AutoShift CI robots"}'
    curl -sf -X PUT "https://${QUAY}/api/v1/organization/autoshift/team/cicreators/members/autoshift+ci" \
      -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d '{}'
    ```

4. Store the robot credential as the push Secret:

    ```bash
    ROBOT_TOKEN=$(curl -sf "https://${QUAY}/api/v1/organization/autoshift/robots/ci" \
      -H "Authorization: Bearer ${TOKEN}" | jq -r .token)
    oc create secret docker-registry quay-autoshift-ci -n autoshift-ci \
      --docker-server="${QUAY}" --docker-username='autoshift+ci' --docker-password="${ROBOT_TOKEN}"
    ```

## GitLab group, projects and webhook

`bootstrap-gitlab` creates a private `autoshift` group on the hub's GitLab with a private project
for each repository that config places there, and does nothing on GitHub:

- `autoshift`, with a Pipelines as Code webhook, while `source.provider` is `gitlab` with no `repo`.
- `site-config`, while `siteConfig.provider` is `gitlab` with no `repo`.

To do the same by hand, with `GL_TOKEN` set to a token with the `api` scope:

1. Create the group, then the projects in it:

    ```bash
    GROUP_ID=$(curl -sf -X POST "${GITLAB}/api/v4/groups" -H "PRIVATE-TOKEN: ${GL_TOKEN}" \
      -H 'Content-Type: application/json' \
      -d '{"name": "autoshift", "path": "autoshift", "visibility": "private"}' | jq .id)
    for p in autoshift site-config; do
      curl -sf -X POST "${GITLAB}/api/v4/projects" -H "PRIVATE-TOKEN: ${GL_TOKEN}" \
        -H 'Content-Type: application/json' \
        -d "{\"name\": \"${p}\", \"path\": \"${p}\", \"namespace_id\": ${GROUP_ID}, \"visibility\": \"private\"}"
    done
    ```

2. Generate a webhook secret and store it with the token, for the Pipelines as Code Repository:

    ```bash
    HOOK_SECRET=$(openssl rand -hex 24)
    oc create secret generic autoshift-pac-webhook -n autoshift-ci \
      --from-literal=webhook.secret="${HOOK_SECRET}" --from-literal=provider.token="${GL_TOKEN}"
    ```

3. Add the webhook to the `autoshift` project:

    ```bash
    PAC=https://$(oc get route pipelines-as-code-controller -n openshift-pipelines -o jsonpath='{.spec.host}')
    curl -sf -X POST "${GITLAB}/api/v4/projects/autoshift%2Fautoshift/hooks" \
      -H "PRIVATE-TOKEN: ${GL_TOKEN}" -H 'Content-Type: application/json' \
      -d "{\"url\": \"${PAC}\", \"token\": \"${HOOK_SECRET}\", \"push_events\": true,
           \"tag_push_events\": true, \"merge_requests_events\": true, \"note_events\": true,
           \"enable_ssl_verification\": true}"
    ```

## Upstream mirror

While the source is the GitLab `autoshift` project, `bootstrap-gitlab` pushes the upstream branches
and tags into it, and `sync-upstream` repeats the push later. It never forces unless you set
`force=true`, so a branch changed in GitLab is reported rather than lost. By hand:

```bash
git clone --mirror https://github.com/auto-shift/autoshiftv2.git autoshift.git
git -C autoshift.git push "${GITLAB}/autoshift/autoshift.git" 'refs/heads/main:refs/heads/main' --tags
```

## Site configuration repository

The site configuration repository holds the values files each AutoShift deployment reads and the
`deployments/` files the pipelines write. It can be on GitHub or on GitLab, independent of the
source, and is set in `config.autoshift-cicd.siteConfig`:

```yaml
config:
  autoshift-cicd:
    siteConfig:
      provider: github                    # gitlab (default) | github
      repo: https://github.com/my-org/autoshift-site-config
      branch: main
      secret: autoshift-site-config       # Secret in autoshift-ci with a token that can push
      secretKey: token
```

With `provider: gitlab` and no `repo`, it is the `site-config` project that `bootstrap-gitlab`
creates, pushed with the GitLab token in `gitlab-autoshift-token`. `policy-autoshift-cicd-endpoints`
resolves the repository, branch and credential into `autoshift-ci-endpoints`, and fails with what is
missing: a GitHub repository needs `repo`, and `secret` must name a Secret with that key.

### GitHub

1. Create an empty repository. AutoShift does not create repositories on GitHub.
2. Create a fine-grained personal access token restricted to that repository, with Contents read and
   write. A classic token needs the `repo` scope.
3. Store it in `autoshift-ci`, under the name and key set in `siteConfig`:

    ```bash
    oc create secret generic autoshift-site-config -n autoshift-ci --from-literal=token=<token>
    ```

### Seed it

`bootstrap-site-config` reads the repository from `autoshift-ci-endpoints`, copies `autoshift/values`
from the upstream ref into it with an empty `deployments/` directory, and pushes the branch. A
repository that already has a branch is left alone, so it never overwrites site edits. By hand:

```bash
git clone --depth 1 https://github.com/auto-shift/autoshiftv2.git upstream
mkdir -p site/deployments && cp -R upstream/autoshift/values/. site/
cd site && git init --initial-branch main && git add -A && git commit -m 'Seed site-config'
git push <site configuration repository URL> main
```

## Source repository

`policy-autoshift-cicd-source` creates the Pipelines as Code Repository `autoshift-ci/autoshift`
from `config.autoshift-cicd.source`. A push or merge request to that repository runs the pipelines
in its `.tekton/` directory in `autoshift-ci`:

```yaml
config:
  autoshift-cicd:
    source:
      provider: github                    # gitlab (default) | github
      repo: https://github.com/auto-shift/autoshiftv2
      apiUrl: ''                          # set for GitHub Enterprise
      secret: autoshift-pac-github        # Secret in autoshift-ci: provider.token, webhook.secret
      provenance: default_branch          # default_branch (default) | source
      pollSchedule: ''                    # cron schedule; see Poll instead of a webhook
      pollRefs: ''                        # branches to watch, or *
```

Pipelines as Code starts a run only when the Git provider sends it an event, so the provider has to
reach the cluster. In production that is the webhook on your repository. Polling is for
evaluation:

| | Webhook | Polling |
|---|---|---|
| Configuration | `provider: github` and `repo` | `provider: gitlab`, no `repo`, and `pollSchedule` |
| Inbound access to the hub from the internet | Required | Not required |
| Rights on the GitHub repository | Administrator, to add the webhook, and a token | None for a public repository |
| A run starts | When the change is pushed | Within one schedule interval |
| Pipeline status is shown on | The GitHub commit and pull request | The commit in the hub's GitLab mirror |

`provenance: source` runs `.tekton/` from the branch that was pushed, which a branch needs before
its pipelines reach the default branch. `default_branch` runs only what is on the default branch, so
a branch cannot change the pipeline it runs under. Prefer it once the pipelines are on the default
branch.

The policy fails, listing what is missing, until the repository URL and the Secret exist.

### GitHub

GitHub is reached through a repository webhook, the setup the Red Hat OpenShift Pipelines
documentation describes for Pipelines as Code without a GitHub App. With a webhook, task status is
reported as pull request comments rather than check runs, and the `/retest` and `/ok-to-test`
comments are not available: push a new commit to run a pipeline again.

1. Create a fine-grained personal access token restricted to the repository, with these
   permissions: Administration, Metadata and Contents read-only; Commit statuses, Pull requests and
   Webhooks read and write. A classic token needs the `public_repo` scope for a public repository,
   or `repo` for a private one. Give it a short expiration.

2. Generate a webhook secret and store both in `autoshift-ci`, under the name set in `source.secret`:

    ```bash
    HOOK_SECRET=$(openssl rand -hex 20)
    oc create secret generic autoshift-pac-github -n autoshift-ci \
      --from-literal=provider.token=<token> --from-literal=webhook.secret="${HOOK_SECRET}"
    ```

3. In the repository settings on GitHub, under **Webhooks**, add a webhook:
    - **Payload URL**: the Pipelines as Code controller URL, from
      `echo https://$(oc get route pipelines-as-code-controller -n openshift-pipelines -o jsonpath='{.spec.host}')`
    - **Content type**: `application/json`
    - **Secret**: the value of `HOOK_SECRET`
    - **Events**: select individual events: Commit comments, Issue comments, Pull requests and
      Pushes

GitHub must reach the controller route, so the hub's ingress has to be reachable from the internet.

### GitLab

With `provider: gitlab` and no `repo`, the Repository points at the `autoshift` project that
`bootstrap-gitlab` creates, on the hub's GitLab, and uses the `autoshift-pac-webhook` Secret that
pipeline writes. The manual equivalent is in
[GitLab group, projects and webhook](#gitlab-group-projects-and-webhook). For another GitLab, set
`repo` and `apiUrl`, and create the Secret and the project webhook with the same keys and events.

### Poll instead of a webhook

Polling lets a hub build the upstream AutoShift repository with no webhook, which suits evaluation
and testing rather than production, where the repository is your own. With `pollSchedule` set, the `poll-upstream` CronJob in `autoshift-ci` watches `upstreamRepo` on
that schedule. It reads the branch and tag references of upstream and of the GitLab `autoshift`
project over the Git HTTP protocol, and starts the `sync-upstream` pipeline only when a watched
branch moved or a tag is new. The push into GitLab fires that project's Pipelines as Code webhook,
which `bootstrap-gitlab` created, inside the cluster. A sync that is still running is left to
finish, and only the latest five poll runs are kept.

```yaml
config:
  autoshift-cicd:
    source:
      pollSchedule: '*/5 * * * *'
      pollRefs: main feature/my-change    # space-separated branches, or *. Empty: upstreamRef
```

Polling needs the source to be the GitLab mirror, `provider: gitlab` with no `repo`. With another
source, `policy-autoshift-cicd-bootstrap` fails and says so. Removing `pollSchedule` removes the
CronJob. `sync-upstream` never forces, so a branch rewritten upstream stops syncing until you run
`sync-upstream` with `force=true`. Watching `*` builds every upstream branch, so list the branches
you want deployed.

To poll by hand, run `sync-upstream` whenever upstream changes:

```bash
tkn pipeline start sync-upstream -n autoshift-ci -p refs="main feature/my-change" --use-param-defaults --showlog
```

### Create the Repository by hand

```yaml
apiVersion: pipelinesascode.tekton.dev/v1alpha1
kind: Repository
metadata:
  name: autoshift
  namespace: autoshift-ci
spec:
  url: https://github.com/auto-shift/autoshiftv2
  git_provider:
    type: github
    secret:
      name: autoshift-pac-github
      key: provider.token
    webhook_secret:
      name: autoshift-pac-github
      key: webhook.secret
  settings:
    pipelinerun_provenance: default_branch
```

For GitLab, set `type: gitlab` and `git_provider.url` to the GitLab URL.

## Signing identity

The pipelines sign keyless through Trusted Artifact Signer. Fulcio issues a short-lived certificate
to an identity from an OpenID Connect issuer it trusts, and the `Securesign` resource that AutoShift deploys
trusts the Keycloak `tssc-iam` realm with client ID `trusted-artifact-signer` and type `email`. The
pipelines prove their identity with a confidential Keycloak client, `autoshift-ci`, through the
client credentials grant, which is the pattern the Trusted Artifact Signer documentation gives for
CI systems with no token of their own.

While both `tas` and `autoshift-cicd` are on, the policies do the following:

1. `policy-keycloak-realm-secrets` generates the client secret once in Secret
   `keycloak-system/autoshift-ci-realm-clients`, key `autoshiftCi`, and never rotates it.
2. `policy-keycloak-realm-import` adds the `autoshift-ci` client to the realm import:
    - Confidential, with only service accounts enabled.
    - An audience mapper that adds `trusted-artifact-signer` to the access token.
    - Fixed `email` (`autoshift-ci@` and the apps domain) and `email_verified` (`true`) claims,
      because Fulcio's `email` issuer type accepts only a verified email.
3. `policy-autoshift-cicd-endpoints` copies the client into Secret `autoshift-ci/keycloak-autoshift-ci`
   with keys `clientId`, `clientSecret` and `tokenUrl`, and publishes the Fulcio, Rekor and TUF URLs,
   the issuer and the signer identity in `autoshift-ci-endpoints`.
4. `policy-autoshift-cicd-bootstrap` creates a Role and RoleBinding in `keycloak-system` that let
   the `autoshift-ci` pipeline ServiceAccount read Secret `keycloak-initial-admin` and the realm
   import, by name and nothing else.

Every certificate the pipelines receive names `autoshift-ci@` and the apps domain, issued by
`${KEYCLOAK}/realms/${REALM}`. Verify signatures against exactly that identity and issuer.

### Add the client to an existing realm

Keycloak imports a realm only when it first creates it, so a realm imported before the
`autoshift-ci` client was added never receives it. `bootstrap-keycloak` copies the client from the
realm import into the running realm, updates it when it differs, and then requests a token and checks
that it carries the claims Fulcio requires. A realm created after the client was added already has
it, and the pipeline reports the client as updated.

To do the same by hand:

1. Get an administrator token from the master realm:

    ```bash
    ADMIN_TOKEN=$(curl -sf "${KEYCLOAK}/realms/master/protocol/openid-connect/token" \
      -d grant_type=password -d client_id=admin-cli \
      --data-urlencode "username=$(oc extract secret/keycloak-initial-admin -n keycloak-system --keys=username --to=-)" \
      --data-urlencode "password=$(oc extract secret/keycloak-initial-admin -n keycloak-system --keys=password --to=-)" \
      | jq -r .access_token)
    ```

2. Create the client from its definition in the realm import. The definition includes the client
   secret, so the example pipes it rather than writing a file:

    ```bash
    oc get keycloakrealmimport "${REALM}" -n keycloak-system -o json \
      | jq '.spec.realm.clients[] | select(.clientId == "autoshift-ci")' \
      | curl -sf -X POST "${KEYCLOAK}/admin/realms/${REALM}/clients" \
          -H "Authorization: Bearer ${ADMIN_TOKEN}" -H 'Content-Type: application/json' -d @-
    ```

### Check the signing token

Request a token as the client and read its claims. The `aud` claim must include
`trusted-artifact-signer`, `email` must be set and `email_verified` must be `true`:

```bash
CI_SECRET=$(oc extract secret/autoshift-ci-realm-clients -n keycloak-system --keys=autoshiftCi --to=-)
curl -sf "${KEYCLOAK}/realms/${REALM}/protocol/openid-connect/token" \
  -d grant_type=client_credentials -d client_id=autoshift-ci --data-urlencode "client_secret=${CI_SECRET}" \
  | jq -r .access_token | cut -d. -f2 | tr '_-' '/+' | base64 -d 2>/dev/null \
  | jq '{iss, aud, email, email_verified}'
```

## Push pipeline

`.tekton/autoshift-push.yaml` runs on every push to a branch of the source repository. Everything
specific to a hub comes from `autoshift-ci-endpoints`, so the file is the same on every hub:

1. **endpoints**: reads the registry and push Secret, and sets the version to
   `0.0.0-<branch>.<commit>`, with the branch lowercased and cut to 40 characters.
2. **fetch**: clones the pushed commit.
3. **build**: on Red Hat's `ubi9/go-toolset` image, downloads helm and yq at the versions
   `versions.yaml` pins, runs `make test`, then runs `make release` without its push. It pushes every
   chart to `<registry>/<namespace>/dev` itself and records the digest each one was stored under. Go
   follows `tools/go.mod` through `GOTOOLCHAIN=auto`.
4. **sign**: requests a token as `autoshift-ci`, signs every digest with one short-lived certificate,
   and verifies each signature against the signer identity and issuer. A chart that does not verify
   fails the run.
5. **deploy**: writes `deployments/b-<id>.yaml` in the site configuration repository, naming the
   version just verified; see [Deployments](#deployments). `<id>` is six hex digits of the branch
   name's hash, so each branch keeps one file.

The pipeline runs only on stock Red Hat images, so there is no CI image to build or maintain. It
needs egress to `get.helm.sh`, `github.com` and the Go module proxy, which `make test` needs anyway.

The signing step runs Red Hat's cosign image, `registry.redhat.io/rhtas/cosign-rhel9`, which the
cluster pulls with its own pull secret. cosign takes the Fulcio and Rekor URLs from the signing
configuration that the hub's TUF repository publishes.

To do the same by hand from a clone, with `TOKEN` set to an `autoshift-ci` token (see
[Check the signing token](#check-the-signing-token)):

```bash
make install-policy-generator && make test
VERSION=0.0.0-my-branch.$(git rev-parse --short=7 HEAD)
REG=registry.example.com/autoshift/dev
make release VERSION="${VERSION}" REGISTRY="${REG%%/*}" REGISTRY_NAMESPACE="${REG#*/}" INCLUDE_MIRROR=false DRY_RUN=true
for c in .helm-charts/policies/*.tgz; do helm push "$c" "oci://${REG}/policies"; done
cosign initialize --mirror "${TUF_URL}" --root "${TUF_URL}/root.json"
cosign sign --yes --identity-token "${TOKEN}" "${REG}/policies/<chart>@<digest>"
cosign verify --certificate-identity "autoshift-ci@${DOMAIN}" \
  --certificate-oidc-issuer "${KEYCLOAK}/realms/${REALM}" "${REG}/policies/<chart>@<digest>"
```

Push the bootstrap charts to `${REG}/bootstrap` and `autoshift-${VERSION}.tgz` to `${REG}` the same
way.

## Deployments

Each file in `deployments/` of the site configuration repository is one AutoShift deployment. The
push pipeline writes one per branch after its build verifies:

```yaml
name: b-1f2e3d                            # Application and release name, at most 11 characters
branch: feature/my-change
commit: 9c698cb...
chartRepo: registry.example.com/autoshift/dev
chartVersion: 0.0.0-feature-my-change.9c698cb
valuesRevision: main                      # site configuration branch or tag the values come from
```

`policy-autoshift-cicd-deployments` creates the `autoshift-deployments` ApplicationSet in the infra
Argo CD, which turns each file into an Application with two sources: the site configuration
repository at `valuesRevision`, and the signed `autoshift` chart at `chartVersion`. The Application
reads the files in `config.autoshift-cicd.deployments.valueFiles`, in order, and sets
`autoshiftOciRepo`, `autoshiftOciVersion` and `versionedClusterSets: true`:

```yaml
config:
  autoshift-cicd:
    deployments:
      valueFiles:
        - global.yaml
        - clustersets/hub.yaml
```

With versioned ClusterSets, a deployment owns copies of the ClusterSets in its values, named with its
version, such as `hub-0-0-0-feature-my-change-9c698cb`. They start with no clusters, so the
deployment's policies render on the hub and reach no cluster until you move one in. To test a
build on a cluster, set that cluster's ClusterSet:

```bash
oc label managedcluster <cluster> cluster.open-cluster-management.io/clusterset=hub-0-0-0-feature-my-change-9c698cb --overwrite
```

Removing a file removes its Application, and the Application's finalizer removes its policies.

The policy also copies two credentials into the infra Argo CD namespace: `autoshift-site-config`
for the site configuration repository, from the Secret named in `siteConfig`, and
`autoshift-registry`, a credential template for every repository under the registry namespace, from
the push Secret. Argo CD must trust both endpoints: when they use certificates from the cluster's
own CA, add that CA to the Argo CD instance.

To do the same by hand, create the two repository Secrets in `openshift-gitops`, with the label
`argocd.argoproj.io/secret-type` set to `repository` and `repo-creds`, and the keys `type`, `url`,
`username` and `password`. Then create one Application per deployment:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: b-1f2e3d
  namespace: openshift-gitops
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  sources:
    - repoURL: https://github.com/my-org/autoshift-site-config
      targetRevision: main
      ref: siteValues
    - repoURL: oci://registry.example.com/autoshift/dev/autoshift
      path: .
      targetRevision: 0.0.0-feature-my-change.9c698cb
      helm:
        releaseName: b-1f2e3d
        valueFiles:
          - $siteValues/global.yaml
          - $siteValues/clustersets/hub.yaml
        valuesObject:
          autoshiftOciRepo: oci://registry.example.com/autoshift/dev/policies
          autoshiftOciVersion: 0.0.0-feature-my-change.9c698cb
          versionedClusterSets: true
          policyGenerator: false
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

## Troubleshooting

- **`policy-autoshift-cicd-endpoints` is NonCompliant.** The message lists what is missing, for
  example the `Tuf` URL while the `Securesign` resource is still deploying, or the client Secret before
  `policy-keycloak-realm-secrets` has run.
- **`bootstrap-keycloak` reports that the client is not in the realm import.** Turn on both `tas`
  and `autoshift-cicd`, then wait for `policy-keycloak-realm-import` to become Compliant.
- **`bootstrap-keycloak` reports that Fulcio would refuse the token.** A mapper differs from the
  realm import. Run the pipeline again, which rewrites every mapper from the import.
