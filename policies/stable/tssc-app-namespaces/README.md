# tssc-app-namespaces AutoShift Policy

## Overview

Creates the namespaces a Red Hat Advanced Developer Suite (RHADS) application pipeline runs in, and
copies into them the secrets the pipelines read. One policy, `policy-tssc-app-namespaces`, placed by
the `tssc-app-namespaces` label.

## Labels

| Label | Values | Effect |
|---|---|---|
| `autoshift.io/tssc-app-namespaces` | `'true'` or `'false'` | Places the policy |
| `autoshift.io/tas` | `'true'` or `'false'` | Copies the cosign public key and the Trusted Artifact Signer integration |
| `autoshift.io/trusted-profile-analyzer` | `'true'` or `'false'` | Copies the Trusted Profile Analyzer integration |
| `autoshift.io/acs` | `'true'` or `'false'` | Copies the Advanced Cluster Security integration |

## Configuration

Each application is a key under `config.tssc-app-namespaces.<team>.<app>`:

```yaml
hubClusterSets:
  hub:
    config:
      tssc-app-namespaces:
        example:
          quarkus-app:
            environments:
              - ci
              - development
              - stage
              - prod
            gitProvider: gitlab     # gitlab or github
```

Every environment gets a namespace named `<team>-<app>-<env>`, with:

- the `argocd.argoproj.io/managed-by` label for the Argo CD instance named by
  `config.devHub.argocd.team`, or the infra instance when that is unset
- the `tssc-image-registry-auth` pull secret, copied from `openshift-config/pull-secret`
- a `pipeline` ServiceAccount that uses that pull secret

The `ci` namespace also gets `tssc-git-integration` and `tssc-argocd-integration`, plus
`cosign-pub`, `tssc-tas-integration`, `tssc-tpa-integration` and `tssc-acs-integration` while the
matching component label is on.

## Failing on missing input

The policy fails rather than writing empty values:

- with the label on and no application declared, it reports that
  `config.tssc-app-namespaces` declares no applications
- when a source Secret or Route does not exist yet, it lists each missing source and the policy
  that creates it

The sources are created by other policies, so a fresh cluster reports them as missing until those
policies are compliant.
