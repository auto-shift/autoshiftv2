# dynatrace AutoShift Policy

## Overview
Installs the dynatrace-operator operator using AutoShift's ACM **PolicyGenerator** pattern. The
directory is a Kustomize source: `policy-generator-config.yaml` wraps the bare manifests under
`manifests/` into an ACM `Policy`, and pairs it with the hand-authored `placement.yaml`.

## Layout
```
dynatrace/
  kustomization.yaml            # entrypoint: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the PolicyGenerator (policy graph, remediation, eval interval)
  placement.yaml                # Placement predicate (autoshift.io/dynatrace) + tolerations
  manifests/
    operator-install/
      kustomization.yaml        # renders the shared components/operator-install chart (Namespace +
                                #   OperatorPolicy); per-operator ACM lookups are visible here
```
The operator-install renders the shared `components/operator-install` chart — edit the visible ACM
lookups in `manifests/operator-install/kustomization.yaml` to tune the subscription/version. Add
further CRs as new `policies[]` entries in `policy-generator-config.yaml` pointing at their own dirs.

## Test Locally
```bash
# Render the policy exactly as the CMP/CI does (needs: make install-policy-generator)
KUSTOMIZE_PLUGIN_HOME=$PWD/.tools/kustomize-plugin .tools/kustomize build \
  --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone \
  policies/stable/dynatrace

# Full validation (helm render + hub/spoke template resolution + label contract)
cd tools && go test -tags integration -count=1 ./internal/resolver/...
```
The `${POLICY_NAMESPACE}`, `${REMEDIATION}`, `${EVAL_COMPLIANT}`, `${EVAL_NONCOMPLIANT}` tokens are
substituted per-deployment by the repo-server CMP before `kustomize build` runs; leave them as-is.

## Enable on Clusters
Labels are defined in values files only — never directly on managed clusters. The cluster-labels
policy propagates them, and this policy's `placement.yaml` selects clusters with
`autoshift.io/dynatrace: 'true'`.

```yaml
# In autoshift/values/clustersets/hub.yaml (or another clusterset / per-cluster file)
hubClusterSets:
  hub:
    labels:
      dynatrace: 'true'
      dynatrace-subscription-name: 'dynatrace-operator'
      dynatrace-channel: 'alpha'
      dynatrace-source: 'certified-operators'
      dynatrace-source-namespace: 'openshift-marketplace'
      # dynatrace-version: 'dynatrace-operator.v1.x.x'  # Optional: pin to a specific CSV
```
`generate-operator-policy.sh --add-to-autoshift` adds these for you (and to `_example*.yaml`, which
the label-contract CI requires for every consumed `autoshift.io/*` key).

The ApplicationSet auto-discovers new directories under `policies/stable/*` (and `certified`/
`community`) — no manual registration required.

## Version Control
- **Automatic upgrades** within the channel by default.
- **Pin a version** with the `dynatrace-version` label — a pinned version sets a manual
  install plan (`startingCSV` + `versions`), so upgrades become explicit.
```bash
# Find available CSV versions
oc get packagemanifests dynatrace-operator -o jsonpath='{.status.channels[*].currentCSV}'
```

## Adding Configuration (operator CRs)
After the operator installs, add its Custom Resources as **bare manifests** — do NOT hand-write a
ConfigurationPolicy wrapper; PolicyGenerator generates it.

1. Explore the CRDs the operator installed:
   ```bash
   oc get pods -n dynatrace
   oc get crds | grep dynatrace
   oc explain <CustomResourceName>
   ```
2. For a **static** CR, drop a bare manifest into `manifests/`:
   ```yaml
   # manifests/dynatrace-config.yaml
   apiVersion: <operator apiVersion>
   kind: <CustomResource>
   metadata:
     name: dynatrace-config
     namespace: dynatrace
   spec:
     setting: value
   ```
3. For a CR that needs **hub templates / loops / conditionals**, use a bare `object-templates-raw`
   manifest instead (PG still wraps it into one ConfigurationPolicy; `complianceType` lives inside):
   ```yaml
   # manifests/dynatrace-config.yaml
   object-templates-raw: |
     - complianceType: musthave
       objectDefinition:
         apiVersion: <operator apiVersion>
         kind: <CustomResource>
         metadata:
           name: dynatrace-config
           namespace: dynatrace
         spec:
           setting: '{{hub index .ManagedClusterLabels "autoshift.io/dynatrace-setting" | default "default-value" hub}}'
   ```
4. If the config must be its **own** Policy (separate remediation, a dependency, or a different
   predicate), add a `policies[]` entry in `policy-generator-config.yaml` (with `dependencies:` on
   `policy-dynatrace-operator-install`) and point it at the new manifest with its own
   `placement.placementPath`. `generate-policy.sh --dir policies/stable/dynatrace` scaffolds this.

Document any new `autoshift.io/dynatrace-*` label in `autoshift/values/clustersets/_example.yaml`
or the label-contract CI (`go test -tags integration`) fails with `Missing`.

### Readiness / status checks
A status assertion (e.g. CSV `phase: Succeeded`) can only be observed, so give it its own
`policies[]` entry with `remediationAction: inform` — never fold an inform check into an enforce
manifest (ACM's root action would clobber it).

## Reference Examples
- **Simple operator**: `policies/stable/cert-manager/` — bare Namespace + OperatorPolicy (this shape)
- **Multiple related policies / placements**: `policies/stable/advanced-cluster-security/`
- **Multiple config types**: `policies/stable/metallb/`
- **Storage cluster config**: `policies/stable/openshift-data-foundation/`

## Troubleshooting
- **Policy not applied**: `oc get managedcluster <cluster> --show-labels`; check the Placement
  (`oc get placement -n policies-autoshift`) and `oc describe policy policy-dynatrace-operator-install`.
- **Operator install issues**: `oc get subscription,installplan -n dynatrace`;
  `oc get catalogsource -n openshift-marketplace`.
- **Render issues**: run the `kustomize build` above; hub templates use bare `{{hub … hub}}`
  delimiters (no Helm `{{ "{{hub" }}` escaping in PolicyGenerator manifests).

## Resources
- [Operator Documentation](https://operatorhub.io/operator/dynatrace-operator)
- [AutoShift Developer Guide](../../../docs/developer-guide.md)
- [ACM PolicyGenerator](https://github.com/open-cluster-management-io/policy-generator-plugin)
