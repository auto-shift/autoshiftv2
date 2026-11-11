# openshift-lightspeed

Installs the Red Hat OpenShift Lightspeed Operator and creates its `OLSConfig`, which connects the
console assistant to an LLM provider. The provider, the model and every other setting come from
`config.openshift-lightspeed`: a SaaS provider, or a model served on the same cluster.

## Policies

| Policy | Mode | What it does |
|---|---|---|
| `policy-ols-operator-install` | enforce | The `openshift-lightspeed` namespace, an OwnNamespace OperatorGroup and the `lightspeed-operator` subscription |
| `policy-ols-config` | enforce | The `OLSConfig` named `cluster`, built from `config.openshift-lightspeed`. Waits for the Operator |
| `policy-ols-ready` | inform | The `lightspeed-app-server` Deployment is `Available`, with a message naming the common causes when it is not |

`ols` abbreviates the policy names to stay within the 40-character limit.

## Enabling

```yaml
hubClusterSets:
  hub:
    labels:
      openshift-lightspeed: 'true'
      openshift-lightspeed-subscription-name: 'lightspeed-operator'
      openshift-lightspeed-channel: 'stable'
      openshift-lightspeed-source: 'redhat-operators'
      openshift-lightspeed-source-namespace: 'openshift-marketplace'
      # openshift-lightspeed-version: 'lightspeed-operator.v1.1.5'   # optional CSV pin
    config:
      openshift-lightspeed:
        provider:
          type: openai
          url: 'https://api.openai.com/v1'
          secretName: 'openai-key'
        model: 'gpt-4o-mini'
```

Settings live in `config.openshift-lightspeed`, the key the version pin already uses, because a
model id or a URL does not fit in a label.

| Key | Default | Notes |
|---|---|---|
| `provider.type` | | Required. The provider type the `OLSConfig` CRD accepts, for example `openai`, `azure_openai`, `watsonx` or `rhoai_vllm` |
| `provider.url` | | The provider endpoint. Exactly one of `url` and `ingressHost` |
| `provider.ingressHost`, `provider.path` | path `/v1` | For a model behind a Route on the same cluster: renders `https://<ingressHost>.<ingress domain><path>` |
| `provider.name` | `llm` | Also the default provider |
| `provider.secretName` | `llm-credentials` | The Secret holding the credential; see [The credential](#the-credential) |
| other `provider` fields | | Passed through as written, for example `deploymentName`, `projectID`, `apiVersion` |
| `model` | | Required. The model id the provider serves; also the default model |
| `mcpServers` | | Optional. More MCP servers; see [MCP servers](#mcp-servers) |
| `toolResultInspection` | operator default, enabled | Optional, `true` or `false`; see [Tool-result inspection](#tool-result-inspection) |
| `toolBudgetRatio` | CRD default, `0.5` | Optional, `0.1` to `0.5`. The share of the model's context window for tool results |
| `olsConfig` | | Optional. Any other `OLSConfig` spec field, merged last over what the keys above produce. Maps merge key by key and these values win; lists replace |

There is no default provider or model, because there is no neutral one. The policy fails, and names
the key, when `provider.type` or `model` is missing, and when any value above is malformed. It also
fails when the cluster's rendered-config ConfigMap is missing.

## The credential

The `OLSConfig` references a Secret by name. No policy creates it, because it holds a credential, and
config is a ConfigMap. Create it in `openshift-lightspeed`:

```bash
oc create secret generic llm-credentials -n openshift-lightspeed \
  --from-literal=apitoken=<key>
```

The key inside the Secret is always `apitoken`, whatever the provider calls it.

## A model on the same cluster

`provider.ingressHost` builds the URL on the managed cluster from the cluster `Ingress`
configuration, which is always populated, so the same values work on every cluster without naming
its domain. For a self-hosted vLLM endpoint behind a Route:

```yaml
config:
  openshift-lightspeed:
    provider:
      type: rhoai_vllm
      ingressHost: 'models'       # https://models.<ingress domain>/v1
    model: 'granite-3-8b-instruct'
```

Lightspeed requires the trailing `/v1` on the provider URL. Serve the model with tool calling
enabled, so the assistant can use its cluster tools.

## MCP servers

Lightspeed always has its built-in `openshift` MCP server. `mcpServers` adds more, which the
`OLSConfig` CRD supports as a Technology Preview behind the `MCPServer` feature gate; the policy
sets the gate whenever the list is not empty.

```yaml
config:
  openshift-lightspeed:
    mcpServers:
      - name: 'docs'
        ingressHost: 'mcp'            # https://mcp.<ingress domain>/mcp
        timeout: 60
        headers:
          - name: 'Authorization'
            valueFrom:
              type: 'kubernetes'      # the signed-in user's token
      - name: 'internal-tools'
        url: 'http://mcp-server.tools.svc.cluster.local:8000/mcp'
```

| Field | Default | Notes |
|---|---|---|
| `name` | | Required, unique. Not `openshift`, the built-in server's name |
| `url` | | A literal `http://` or `https://` URL. Exactly one of `url` and `ingressHost` |
| `ingressHost` | | A single DNS label. Renders `https://<ingressHost>.<ingress domain><path>`, with the domain read on the managed cluster |
| `path` | `/mcp` | With `ingressHost` only |
| `timeout` | `60` | Seconds. The CRD's own default is 5, shorter than a documentation search |
| `headers` | | Passed through. `valueFrom.type` is `secret` (with `secretRef.name`, a Secret in `openshift-lightspeed`), `kubernetes` (the user's token) or `client` |

`policy-ols-config` does not depend on the MCP servers it lists: a server that is down costs only
its tools. Enable whatever provides a server together with its entry here.

Removing an entry from `mcpServers` does not remove it from the `OLSConfig`, because `musthave`
never removes list entries. Remove it by hand:

```bash
oc get olsconfigs.ols.openshift.io cluster -o jsonpath='{range .spec.mcpServers[*]}{.name}{"\n"}{end}'
oc patch olsconfigs.ols.openshift.io cluster --type=json \
  -p '[{"op":"remove","path":"/spec/mcpServers/<index>"}]'
```

## Tool-result inspection

Lightspeed inspects each tool result before the model uses it, and stops the answer when a result
fails. `toolResultInspection: false` sets `spec.ols.guardrails.toolResultInspection.enabled: false`,
the documented workaround for the OpenShift Lightspeed 1.1.5 known issue "Tool-result inspection
might fail at the tool call step for certain models". Unset, the policy writes nothing and the
operator default applies, which is enabled.

It applies to every MCP server, the built-in `openshift` one included, so record turning it off as a
deliberate choice. To turn it back on, set `true`: removing the key leaves the last value in the
`OLSConfig`.

## Install mode

`lightspeed-operator` v1.1.5 declares `OwnNamespace=True` and `AllNamespaces=False`. The shared
operator-install chart defaults to an AllNamespaces OperatorGroup, which OLM refuses: the CSV sits in
`Failed` with `UnsupportedOperatorGroup` while the policy reports only that the CSV was not found.
This policy sets `targetNamespaces: [openshift-lightspeed]`.

## Dependencies

`policy-ols-config` depends on `policy-ols-operator-install`, because the `OLSConfig` CRD arrives
with the CSV, and a ConfigurationPolicy naming a CRD that does not exist yet reports Compliant.

## Verifying

```bash
oc get olsconfigs.ols.openshift.io cluster -o jsonpath='{.spec.llm.providers[0].url}{"\n"}{.spec.ols.defaultModel}{"\n"}'
oc get deployment lightspeed-app-server -n openshift-lightspeed
```
