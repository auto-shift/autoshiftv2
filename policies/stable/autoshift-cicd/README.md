# autoshift-cicd AutoShift Policy

## Overview

AutoShift's own continuous integration and delivery, built on the Red Hat Advanced Developer Suite
(RHADS) components. This first part delivers bootstrap pipelines: parameterized Tekton Pipelines in
the `autoshift-ci` namespace that create what the CI/CD needs in Quay, GitLab and Keycloak. AutoShift
only installs them. Nothing runs until you start one. Every step, automated or not, has a manual
procedure in [AutoShift CI/CD](../../../docs/autoshift-cicd.md).

| Policy | What it does |
|---|---|
| `policy-autoshift-cicd-bootstrap` | The `autoshift-ci` namespace, the bootstrap Tasks and Pipelines, and while `tas` is on, read access to the Keycloak admin Secret and realm import for `bootstrap-keycloak` |
| `policy-autoshift-cicd-source` | The Pipelines as Code Repository for the AutoShift source on GitHub or GitLab |
| `policy-autoshift-cicd-endpoints` | Gathers what the Tasks read into `autoshift-ci`: endpoint URLs, the trust bundle, the GitLab token, and while `tas` is on, the Trusted Artifact Signer URLs and the `autoshift-ci` signing client |

## Labels and configuration

Placed on hubs with `autoshift.io/autoshift-cicd: 'true'`. Pipeline defaults come from
`config.autoshift-cicd`, and every one can be overridden when a pipeline starts. `source` selects
the repository whose pushes run the `.tekton/` pipelines:

```yaml
config:
  autoshift-cicd:
    upstreamRepo: https://github.com/auto-shift/autoshiftv2.git
    upstreamRef: main
    gitlabGroup: autoshift
    quayOrg: autoshift
    source:
      provider: github                # gitlab (default) | github
      repo: https://github.com/auto-shift/autoshiftv2
      apiUrl: ''                      # set for GitHub Enterprise or another GitLab
      secret: autoshift-pac-github    # Secret in autoshift-ci: provider.token, webhook.secret
      provenance: default_branch      # default_branch (default) | source
      pollSchedule: ''                # cron schedule: mirror upstream into the GitLab project instead of a webhook
      pollRefs: ''                    # branches to watch, or *. Empty: upstreamRef
    registry:
      host: ''                        # your OCI registry. Empty: the hub's Quay
      namespace: ''                   # organization or project. Empty: quayOrg
      pushSecret: ''                  # dockerconfigjson Secret in autoshift-ci. Empty with the hub's Quay: the bootstrap-quay robot
    siteConfig:
      provider: gitlab                # gitlab (default) | github
      repo: ''                        # empty with gitlab: the site-config project bootstrap-gitlab creates
      branch: main
      secret: ''                      # Secret in autoshift-ci with a push token. Empty with gitlab: the GitLab token
      secretKey: token
```

With `provider: gitlab` and no `repo`, the source is the `autoshift` project `bootstrap-gitlab`
creates. Set `pollSchedule` there and the `poll-upstream` CronJob mirrors upstream into it on that
schedule, so GitHub needs no webhook and the hub needs no inbound access. That suits evaluation; in
production, `source`, `siteConfig` and `registry` point at repositories and a registry you own. See
[Production and evaluation](../../../docs/autoshift-cicd.md#production-and-evaluation). For GitHub, create the Secret and the repository webhook as described in
[Source repository](../../../docs/autoshift-cicd.md#source-repository).

The Quay and GitLab endpoints are gathered only while the `quay` and `gitlab` labels are on.

## Pipelines

| Pipeline | Creates |
|---|---|
| `bootstrap-quay` | The Quay organization, a `<org>+ci` robot in a team with the creator role, and the robot's push Secret `quay-<org>-ci` |
| `bootstrap-gitlab` | The GitLab group and the projects config places on the hub's GitLab: `autoshift` with the Pipelines as Code webhook and the upstream branches pushed into it, and `site-config` |
| `bootstrap-site-config` | Seeds the site configuration repository, on GitHub or GitLab, once from `autoshift/values` |
| `sync-upstream` | Pushes upstream branches and tags into the GitLab `autoshift` project again |
| `bootstrap-keycloak` | Copies the `autoshift-ci` signing client from the realm import into a realm imported before the client existed, then checks its token carries the claims Fulcio requires |

Every task checks before it creates, so each pipeline is safe to run again. `sync-upstream` does not
force unless you set `force=true`, so a branch changed in GitLab is reported rather than lost.
`bootstrap-site-config` leaves a repository that already has a branch alone.

Start a pipeline from the console, or with the `tkn` CLI:

```bash
tkn pipeline start bootstrap-quay -n autoshift-ci --use-param-defaults --showlog
tkn pipeline start bootstrap-gitlab -n autoshift-ci -p upstreamRefs="main release-1.0" --showlog
tkn pipeline start bootstrap-site-config -n autoshift-ci --use-param-defaults --showlog
tkn pipeline start bootstrap-keycloak -n autoshift-ci --use-param-defaults --showlog
```

## The Quay admin token

`bootstrap-quay` needs a Quay superuser token, which AutoShift never creates for you. Either:

- Create an OAuth application token as a Quay superuser, then store it:

  ```bash
  oc create secret generic quay-admin-token -n autoshift-ci --from-literal=token=<token>
  ```

- On a new registry with no users, start the pipeline with `initialize=true` instead. It calls the
  one-time user initialize endpoint, which works only while `config.quay.bootstrap.userInitialize`
  is on, and stores the new administrator in the `quay-admin` Secret. The endpoint refuses once any
  user exists, so the flag can stay on.

## Images

The Tasks use images the cluster already has, so nothing extra needs mirroring:

- `oc` and `python3` from the `openshift/cli` ImageStream, or `config.images.cli` when set
- `git` from the `git-clone` Task the Pipelines Operator installs

## Failing on missing input

`policy-autoshift-cicd-endpoints` fails with the list of what is missing, rather than writing empty
values: the Pipelines as Code Route, the ingress CA, the Quay Route and GitLab token while their
labels are on, and the Trusted Artifact Signer URLs, Keycloak host and signing client while `tas` is
on. `policy-autoshift-cicd-bootstrap` fails when it cannot resolve the two images.
