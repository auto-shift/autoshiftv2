# AutoShift CI/CD

AutoShift builds, signs and deploys itself on the Red Hat Advanced Developer Suite components that it
installs on a hub: Red Hat OpenShift Pipelines runs the pipelines, GitHub or GitLab hosts the
source, GitLab hosts the site configuration, Red Hat Quay stores the charts, and Red Hat Trusted Artifact Signer signs them with
an identity from the Red Hat build of Keycloak.

Most of the setup is policy. Steps that need a credential an administrator holds, or that change a
system AutoShift cannot reach through a Kubernetes resource, are bootstrap pipelines you start with
parameters. Every step also has a manual procedure on this page, for a hub where you prefer to run
it by hand or need to understand what the automation does.

## What runs where

| Step | Delivered by | Manual procedure |
|---|---|---|
| `autoshift-ci` namespace, Tasks and Pipelines | `policy-autoshift-cicd-bootstrap` | Not applicable |
| Endpoints, trust bundle and GitLab token in `autoshift-ci` | `policy-autoshift-cicd-endpoints` | [Endpoints and trust](#endpoints-and-trust) |
| Quay organization, CI robot and push Secret | `bootstrap-quay` pipeline | [Quay organization and robot](#quay-organization-and-robot) |
| GitLab group, projects and Pipelines as Code webhook | `bootstrap-gitlab` pipeline | [GitLab group, projects and webhook](#gitlab-group-projects-and-webhook) |
| Upstream branches in GitLab and the seeded `site-config` | `bootstrap-gitlab` and `sync-upstream` pipelines | [Upstream source and site configuration](#upstream-source-and-site-configuration) |
| Pipelines as Code Repository for the source on GitHub or GitLab | `policy-autoshift-cicd-source` | [Source repository](#source-repository) |
| Signing client secret and the `autoshift-ci` Keycloak client | `policy-keycloak-realm-secrets` and `policy-keycloak-realm-import` | [Signing identity](#signing-identity) |
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

## Quay organization and robot

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

`bootstrap-gitlab` creates a private `autoshift` group with the private projects `autoshift` and
`site-config`, and a Pipelines as Code webhook on `autoshift`. To do the same by hand, with
`GL_TOKEN` set to a token with the `api` scope:

1. Create the group, then the two projects in it:

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

## Upstream source and site configuration

`bootstrap-gitlab` pushes the upstream branches and tags into the `autoshift` project, and seeds an
empty `site-config` project once from `autoshift/values`, plus an empty `deployments/` directory.
`sync-upstream` repeats the push later, and never forces unless you set `force=true`. By hand:

```bash
git clone --mirror https://github.com/auto-shift/autoshiftv2.git autoshift.git
git -C autoshift.git push "${GITLAB}/autoshift/autoshift.git" 'refs/heads/main:refs/heads/main' --tags

git clone --depth 1 https://github.com/auto-shift/autoshiftv2.git upstream
mkdir -p site/deployments && cp -R upstream/autoshift/values/. site/
cd site && git init --initial-branch main && git add -A && git commit -m 'Seed site-config'
git push "${GITLAB}/autoshift/site-config.git" main
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
      provenance: source                  # source | default_branch
```

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
    pipelinerun_provenance: source
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

## Troubleshooting

- **`policy-autoshift-cicd-endpoints` is NonCompliant.** The message lists what is missing, for
  example the `Tuf` URL while the `Securesign` resource is still deploying, or the client Secret before
  `policy-keycloak-realm-secrets` has run.
- **`bootstrap-keycloak` reports that the client is not in the realm import.** Turn on both `tas`
  and `autoshift-cicd`, then wait for `policy-keycloak-realm-import` to become Compliant.
- **`bootstrap-keycloak` reports that Fulcio would refuse the token.** A mapper differs from the
  realm import. Run the pipeline again, which rewrites every mapper from the import.
