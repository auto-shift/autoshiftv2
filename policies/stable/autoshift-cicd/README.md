# autoshift-cicd AutoShift Policy

## Overview

AutoShift's own continuous integration and delivery, built on the Red Hat Advanced Developer Suite
(RHADS) components. This first part delivers bootstrap pipelines: parameterized Tekton Pipelines in
the `autoshift-ci` namespace that create what the CI/CD needs in Quay and GitLab. AutoShift only
installs them. Nothing runs until you start one.

| Policy | What it does |
|---|---|
| `policy-autoshift-cicd-bootstrap` | The `autoshift-ci` namespace, the bootstrap Tasks and the bootstrap Pipelines |
| `policy-autoshift-cicd-endpoints` | Gathers what the Tasks read into `autoshift-ci`: endpoint URLs, the trust bundle and the GitLab token |

## Labels and configuration

Placed on hubs with `autoshift.io/autoshift-cicd: 'true'`. Pipeline defaults come from
`config.autoshift-cicd`, and every one can be overridden when a pipeline starts:

```yaml
config:
  autoshift-cicd:
    upstreamRepo: https://github.com/auto-shift/autoshiftv2.git
    upstreamRef: main
    gitlabGroup: autoshift
    quayOrg: autoshift
```

The Quay and GitLab endpoints are gathered only while the `quay` and `gitlab` labels are on.

## Pipelines

| Pipeline | Creates |
|---|---|
| `bootstrap-quay` | The Quay organization, a `<org>+ci` robot in a team with the creator role, and the robot's push Secret `quay-<org>-ci` |
| `bootstrap-gitlab` | The GitLab group with `autoshift` and `site-config` projects, the Pipelines as Code webhook, the upstream branches pushed into `autoshift`, and `site-config` seeded once from `autoshift/values` |
| `sync-upstream` | Pushes upstream branches and tags into the GitLab `autoshift` project again |

Every task checks before it creates, so each pipeline is safe to run again. `sync-upstream` does not
force unless you set `force=true`, so a branch changed in GitLab is reported rather than lost.
`seed-site-config` leaves a repository that already has a branch alone.

Start a pipeline from the console, or with the `tkn` CLI:

```bash
tkn pipeline start bootstrap-quay -n autoshift-ci --use-param-defaults --showlog
tkn pipeline start bootstrap-gitlab -n autoshift-ci -p upstreamRefs="main release-1.0" --showlog
```

## The Quay admin token

`bootstrap-quay` needs a Quay superuser token, which AutoShift never creates for you. Either:

- Create an OAuth application token as a Quay superuser, then store it:

  ```bash
  oc create secret generic quay-admin-token -n autoshift-ci --from-literal=token=<token>
  ```

- On a new registry with no users, start the pipeline with `initialize=true` instead. It calls the
  one-time user initialize endpoint, which works only while `config.quay.bootstrap.userInitialize`
  is on, and stores the new administrator in the `quay-admin` Secret. Turn the flag off afterward.

## Images

The Tasks use images the cluster already has, so nothing extra needs mirroring:

- `oc` and `python3` from the `openshift/cli` ImageStream, or `config.images.cli` when set
- `git` from the `git-clone` Task the Pipelines Operator installs

## Failing on missing input

`policy-autoshift-cicd-endpoints` fails with the list of what is missing, rather than writing empty
values: the Pipelines as Code Route, the ingress CA, and the Quay Route and GitLab token while their
labels are on. `policy-autoshift-cicd-bootstrap` fails when it cannot resolve the two images.
