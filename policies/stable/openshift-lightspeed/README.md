# openshift-lightspeed AutoShift Policy

## Overview
Installs the Red Hat OpenShift Lightspeed operator (`lightspeed-operator`) and deploys the
`OLSConfig` service using AutoShift's ACM **PolicyGenerator** pattern.

OpenShift Lightspeed is an AI assistant in the OpenShift web console. **The operator does not provide
an LLM.** You must already have a provider (OpenAI, Azure OpenAI, IBM watsonx, RHEL AI, OpenShift AI,
Google Vertex, AWS Bedrock) and an API token before enabling this policy.

| Constraint | Value |
|---|---|
| Supported OpenShift | 4.16 – 4.22 |
| Architecture | **x86_64 only** — the operator is filtered out of OperatorHub elsewhere |
| Install mode | **OwnNamespace only**, so the OperatorGroup must set `targetNamespaces` |
| Namespace | `openshift-lightspeed` |
| CR | `OLSConfig` (`ols.openshift.io/v1alpha1`), a singleton that must be named `cluster` |

## Layout
```
openshift-lightspeed/
  kustomization.yaml            # entrypoint: generators: [policy-generator-config.yaml]
  policy-generator-config.yaml  # the policy graph, remediation, eval interval
  placement.yaml                # Placement predicate (autoshift.io/openshift-lightspeed)
  manifests/
    operator-install/
      kustomization.yaml        # renders the shared components/operator-install chart
    llm-credentials.yaml        # optional hub -> spoke copy of the LLM API token
    olsconfig.yaml              # the OLSConfig singleton, driven from config
  test/
    olsconfig-ready.yaml        # inform-only readiness gate
```

### Policy graph
| Policy | Depends on | What it does |
|---|---|---|
| `policy-lightspeed-operator-install` | — | Namespace + OperatorPolicy |
| `policy-lightspeed-credentials` | operator-install | copies the LLM token from a hub Secret, if one is named |
| `policy-lightspeed-olsconfig` | operator-install | creates the `OLSConfig` |
| `policy-lightspeed-olsconfig-test` | olsconfig | informs on `status.overallStatus: Ready` |

`policy-lightspeed-olsconfig` deliberately depends on the operator install rather than on the
credentials policy: in reference-only mode the credentials policy emits nothing, and chaining on a
no-op policy would wedge the OLSConfig behind it. The two are eventually consistent instead — the
operator reports an error until the Secret lands, then heals.

## Enable on Clusters
Labels are defined in values files only — never directly on managed clusters.

```yaml
hubClusterSets:
  hub:
    labels:
      openshift-lightspeed: 'true'
      openshift-lightspeed-subscription-name: lightspeed-operator
      openshift-lightspeed-channel: stable          # stable | alpha
      openshift-lightspeed-source: redhat-operators
      openshift-lightspeed-source-namespace: openshift-marketplace
      # openshift-lightspeed-version: 'lightspeed-operator.v1.1.2'   # optional CSV pin
```

The enable label ships as `'false'` in every profile. Lightspeed has a hard external prerequisite (an
LLM endpoint and token), so it is opt-in rather than on-by-default.

## Configuration

Day-2 settings live under `config.openshift-lightspeed` (data), while the label above is the enable
gate. The config flows into each cluster's `<cluster>.rendered-config` ConfigMap on the hub, which the
manifests read via hub templates.

```yaml
hubClusterSets:
  hub:
    config:
      openshift-lightspeed:
        credentialsSecretName: lightspeed-llm-credentials
        hubSecret:                        # optional — see "Credentials" below
          namespace: lightspeed-secrets
          name: llm-apitoken
          key: apitoken
        defaultProvider: myOpenai         # must match a providers[].name
        defaultModel: gpt-4o-mini         # must match one of that provider's models[].name
        logLevel: INFO                    # DEBUG | INFO | WARNING | ERROR | CRITICAL
        introspectionEnabled: true        # Kubernetes MCP server
        userDataCollection:
          feedbackDisabled: true
          transcriptsDisabled: true
        deployment:
          api:
            replicas: 1
        providers:
          - name: myOpenai
            type: openai
            url: https://api.openai.com/v1
            credentialsSecretRef:
              name: lightspeed-llm-credentials
            models:
              - name: gpt-4o-mini
```

`providers` is passed to `spec.llm.providers` verbatim, so every provider type works without a
template change — `openai`, `azure_openai`, `watsonx`, `rhoai_vllm`, `rhelai_vllm`, `google_vertex`,
`google_vertex_anthropic`, Bedrock. Each type needs its own extra fields (`deploymentName` and
`apiVersion` for Azure, `projectID` for watsonx, and so on); see the OLSConfig API reference. Provider
URLs must end in `/v1`.

`deployment` is per-component — there is no top-level `replicas`. Valid components: `api`, `console`,
`database`, `mcpServer`, `rhokp`, `otelCollector`, `alertsAdapter`, `agenticConsole`, `dataCollector`.
RHOKP (documentation retrieval) is the heavy one: it wants roughly 75 GiB ephemeral storage, 2 CPU and
2 GiB memory, and indexes the docs corpus on first start, so expect `NotReady` for a while.

### Guards
The `OLSConfig` is created **only** when `providers`, `defaultProvider` and `defaultModel` are all
set — the CRD requires all three, and an incomplete CR is rejected by the API server. A cluster with
the label on but no config gets the operator and nothing else. This matters because `musthave` +
`enforce` *creates* absent objects, so an ungated template would materialize a broken singleton.

### Credentials
Two modes, chosen by whether `config.openshift-lightspeed.hubSecret` is set.

**Copy from the hub.** Create one Secret on the hub and AutoShift distributes it:

```bash
oc create secret generic llm-apitoken -n lightspeed-secrets --from-literal=apitoken='<token>'
```

`policy-lightspeed-credentials` then writes it to `credentialsSecretName` in `openshift-lightspeed` on
every selected cluster. The copy uses `fromSecret` (which requires
`hubTemplateOptions.serviceAccountName`), so ACM encrypts the token in the replicated policy rather
than leaving it in plaintext in each managed-cluster namespace. OLSConfig always reads the token from
the `apitoken` key, so `hubSecret.key` is remapped on the way in.

If the hub Secret is named but missing, the policy fails loudly — that is intentional.

**Reference only.** Omit the `hubSecret` block entirely. AutoShift creates no Secret; the OLSConfig
just references `credentialsSecretName`, which the cluster owner or External Secrets must provide in
`openshift-lightspeed`.

## Test Locally
```bash
# Render the policy exactly as the CMP/CI does (needs: make install-policy-generator)
KUSTOMIZE_PLUGIN_HOME=$PWD/.tools/kustomize-plugin .tools/kustomize build \
  --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone \
  policies/stable/openshift-lightspeed

# Full validation (helm render + hub/spoke template resolution + label contract)
cd tools && go test -tags integration -count=1 ./internal/resolver/...
```
`tools/testdata/lightspeed-secrets.yaml` stubs the hub Secret so the `fromSecret` call resolves in CI.

The `${POLICY_NAMESPACE}`, `${REMEDIATION}`, `${EVAL_COMPLIANT}`, `${EVAL_NONCOMPLIANT}` tokens are
substituted per-deployment by the repo-server CMP before `kustomize build` runs; leave them as-is.

## Version Control
- **Automatic upgrades** within the channel by default.
- **Pin a version** with the `openshift-lightspeed-version` label, or allow-list several with
  `config.openshift-lightspeed.versions` + `startingCSV`.

```bash
oc get packagemanifests lightspeed-operator -n openshift-marketplace \
  -o jsonpath='{range .status.channels[*]}{.name}{" -> "}{.currentCSV}{"\n"}{end}'
```

## Troubleshooting
- **Policy not applied**: `oc get managedcluster <cluster> --show-labels`; check
  `oc get placement -n policies-autoshift` and `oc describe policy policy-lightspeed-operator-install`.
- **Operator install issues**: `oc get subscription,installplan,csv -n openshift-lightspeed`.
  If the operator is missing from the catalog, confirm the cluster is x86_64.
- **No OLSConfig created**: the guard is doing its job — check that `providers`, `defaultProvider` and
  `defaultModel` are all present in the cluster's rendered config:
  `oc get cm <cluster>.rendered-config -n policies-autoshift -o yaml`.
- **502 Bad Gateway in the console**: the service pods are still starting; on a fresh install RHOKP
  indexing takes a while.
- **`Prompt is too long`**: lower `maxTokensForResponse` or raise `contextWindowSize` on the model.

## Resources
- [OpenShift Lightspeed documentation](https://docs.redhat.com/en/documentation/red_hat_openshift_lightspeed/)
- [OLSConfig API reference](https://docs.redhat.com/en/documentation/red_hat_openshift_lightspeed/1.1/html/configure/olsconfig-api)
- [AutoShift Developer Guide](../../../docs/developer-guide.md)
