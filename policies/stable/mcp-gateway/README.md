# mcp-gateway

Creates one shared MCP gateway on Red Hat Connectivity Link 1.5, registers MCP servers with it, and
controls which groups and users can list and call which tools. The MCP catalog in Red Hat OpenShift
AI 3.5 deploys MCP servers; this policy gives agents one authenticated endpoint that federates their
tools.

Enabled by the `autoshift.io/mcp-gateway` label. **What** it creates comes from `config.mcpGateway`
in the cluster's rendered-config, not from labels. A gateway, a server and an access rule are
structured records, and a label is a string.

The MCP Gateway is a Technology Preview feature.

## What it does not install

No Operator. Connectivity Link 1.5 deploys the MCP Gateway controller itself, with Authorino,
Limitador and DNS, through the `KuadrantControlPlane` named `default`. The
[connectivity-link](../connectivity-link/README.md) policy installs Connectivity Link; set its
channel to `stable-1.5`, because 1.4 has no MCP Gateway.

This policy reads `KuadrantControlPlane` in an inform policy and never creates, changes or deletes
it. Deleting it removes every Connectivity Link component.

## Enabling

```yaml
hubClusterSets:
  hub:
    labels:
      connectivity-link: 'true'
      connectivity-link-channel: stable-1.5
      mcp-gateway: 'true'
```

Set labels in values files only. The `cluster-labels` policy stamps them onto each cluster. Turning
the label off stops management and removes nothing.

## Configuration

```yaml
config:
  mcpGateway:
    gateway:
      name: 'mcp-gateway'
      namespace: 'mcp-gateway'
      className: 'openshift-default'
      hostname: ''
      tlsSecretName: ''
      internalPort: 8080
      logLevel: 'info'
      auth:
        type: 'kubernetes'
        audiences:
          - 'https://kubernetes.default.svc'
      toolAuthorization:
        enabled: true
        keySecretName: 'mcp-trusted-headers'
        kuadrantNamespace: 'kuadrant-system'
      extraRouteNamespaces: []
    servers:
      - name: 'openshift-mcp'
        namespace: 'mcp-servers'
        service: 'openshift-mcp'
        port: 8080
        path: '/mcp'
        prefix: 'ocp_'
        category:
          - 'platform'
        hint: 'Read-only OpenShift cluster tools'
        credentialSecret:
          name: 'openshift-mcp-credential'
          key: 'token'
        access:
          - groups: ['mcp-platform-admins']
            tools: ['pods_list', 'pods_log', 'events_list', 'resources_get']
          - groups: ['mcp-developers']
            users: ['system:serviceaccount:agents:triage-agent']
            tools: ['pods_list', 'events_list']
      - name: 'docs-search'
        namespace: 'mcp-servers'
        port: 8000
        prefix: 'docs_'
        access:
          - groups: ['mcp-platform-admins', 'mcp-developers']
            tools: ['search', 'fetch']
    virtualServers:
      - name: 'triage'
        description: 'Tools for incident triage'
        tools: ['ocp_pods_list', 'ocp_events_list', 'docs_search']
```

Every value in the sample is an example.

| Field | Default | Notes |
|-------|---------|-------|
| `gateway` | | Required |
| `gateway.name` | `mcp-gateway` | The `Gateway` and the `MCPGatewayExtension` |
| `gateway.namespace` | `mcp-gateway` | Created by the policy. The broker, the extension and the virtual servers run here |
| `gateway.className` | `openshift-default` | |
| `gateway.hostname` | `mcp.<ingress domain>` | The public endpoint. The domain is read on the managed cluster |
| `gateway.tlsSecretName` | the router's default certificate | The `Route`'s `externalCertificate`. Must be in `openshift-ingress`, and the router needs permission to read it |
| `gateway.internalPort` | `8080` | The port both listeners use |
| `gateway.logLevel` | `info` | Broker log level |
| `gateway.auth.type` | | Required: `none`, `kubernetes` or `jwt` |
| `gateway.auth.audiences` | `[https://kubernetes.default.svc]` | `kubernetes` only |
| `gateway.auth.issuerUrl` | | Required with `jwt`. Also advertised as the OAuth authorization server |
| `gateway.auth.groupsClaim` | `groups` | `jwt` only. The token claim holding the caller's groups |
| `gateway.auth.usernameClaim` | `preferred_username` | `jwt` only. The token claim matched against `users` |
| `gateway.authorinoTLS` | `true` | TLS from the gateway to Authorino, see "TLS to Authorino" |
| `gateway.toolAuthorization.enabled` | `false` | Enforce `servers[].access`. Needs `auth.type` `kubernetes` or `jwt` |
| `gateway.toolAuthorization.keySecretName` | `mcp-trusted-headers` | Names the key pair Secrets, see below |
| `gateway.toolAuthorization.kuadrantNamespace` | `kuadrant-system` | Where the private key Secret lives |
| `gateway.extraRouteNamespaces` | | More namespaces allowed to attach server routes |
| `servers` | | Optional. With none, the gateway alone is created |
| `servers[].name` | | Required. The registration name |
| `servers[].namespace` | | Required. Where the MCP server runs. Not created |
| `servers[].service` | `name` | The MCP server's Service |
| `servers[].port` | | Required. The Service port |
| `servers[].path` | `/mcp` | |
| `servers[].prefix` | | Required. Prepended to every tool name. Immutable |
| `servers[].category` | `[uncategorised]` | Used by the gateway's tool discovery |
| `servers[].hint` | | Short description for tool discovery |
| `servers[].credentialSecret.name` / `.key` | / `token` | A Secret created out of band |
| `servers[].access[].groups` / `.users` | | Who the entry grants. One of the two is required |
| `servers[].access[].tools` | | Required. Tool names without the prefix |
| `virtualServers[].name` / `.tools` | | Required. Tool names with the prefix |
| `virtualServers[].description` | | |

The policies fail, and name the problem, when `gateway` or `auth.type` is missing, a `jwt` gateway
has no `issuerUrl`, a server lacks `name`, `namespace`, `port` or `prefix`, a virtual server lacks
`name` or `tools`, or tool access is inconsistent: `access` without `toolAuthorization.enabled`,
`toolAuthorization` with `auth.type: none` or with no `access` entries at all, an entry with no
`tools` or with neither `groups` nor `users`, or a `*` among the tools.

## Policies

| Policy | Mode | Does |
|--------|------|------|
| `policy-mcp-gateway-prereqs` | inform | The MCP Gateway CRDs are established and `KuadrantControlPlane` is `Ready`. Waits for `policy-rhcl-ready` |
| `policy-mcp-gateway` | enforce | The namespace, `Gateway`, `Route`, `ReferenceGrant`, `MCPGatewayExtension`, the `AuthPolicy` objects and the Authorino TLS `EnvoyFilter`, and per server an `HTTPRoute` and an `MCPServerRegistration`, per virtual server an `MCPVirtualServer`. Also the shared `openshift-default` GatewayClass |
| `policy-mcp-gateway-ready` | inform | The EnvoyFilter CRD exists, the `Gateway` is `Programmed`, the extension and each registration `Ready`, each `AuthPolicy` `Enforced`, and each Secret created out of band is present |

The prerequisite gate matters because a `ConfigurationPolicy` naming a kind whose CRD does not
exist reports Compliant while doing nothing. On Connectivity Link 1.4 the chain stays Pending
instead.

## How the gateway is wired

One `Gateway`, `openshift-ingress/<name>`, with two plain-HTTP listeners on `internalPort`:

- `mcp`, with no hostname, is the authenticated endpoint. The MCP Gateway controller creates the
  HTTPRoute for `/mcp` on it, and the `<name>-mcp` `AuthPolicy` protects it.
- `mcp-internal` carries the `*.mcp.local` hostnames that server routes attach to. The broker
  reaches the servers through it, so `privateHost` points at it explicitly.

Both listeners share one port because the MCP Gateway controller matches a server's route to the
`MCPGatewayExtension` by the port of the listener the route attaches to. With the public listener
on 443 and the servers on 8080, every `MCPServerRegistration` stays `NotReady` with "no matching
mcpgatewayextensions for attached listener".

Clients outside the cluster reach the gateway through a `Route`, `openshift-ingress/<name>`, for
`gateway.hostname`, with edge TLS at the default router, as Connectivity Link 1.5 documents for
exposing a gateway. The `mcp` listener has no hostname, so the Ingress Operator publishes no DNS
record for it that would shadow the router's wildcard record. In-cluster clients can call
`http://<name>-<className>.openshift-ingress.svc.cluster.local:<internalPort>/mcp` directly.

The `Gateway` is in `openshift-ingress` because the Ingress Operator's Gateway API implementation
requires every `openshift-default` Gateway to be there. Kuadrant policies attach only to targets in
their own namespace, so the `AuthPolicy` objects are there too. The broker runs in its own
namespace, `gateway.namespace`, so it does not share a namespace with the router.

The internal listener accepts routes only from the gateway namespace, the namespaces of the
configured servers, and `extraRouteNamespaces`, never from all namespaces. It is served by the same
Gateway Service as the public listener.

If the `mcp` listener's port changes, Connectivity Link 1.5.0's MCP Gateway controller does not
update the router EnvoyFilter it created (`mcp-ext-proc-<name>-gateway` in `openshift-ingress`). It
keeps the old `portNumber`, the router no longer sees tool calls, and every `tools/call` reaches the
broker and fails with "does not forward tool calls". Delete that EnvoyFilter; the controller recreates
it from the current listener once the `MCPGatewayExtension` is Ready.

## Registering servers from the MCP catalog

The MCP Lifecycle Operator deploys a catalog server as an `MCPServer` with a Deployment and a
Service, and creates no route or registration. List the server under `config.mcpGateway.servers`
with its Service name and port; this policy adds an `HTTPRoute` with the hostname
`<namespace>-<name>.mcp.local` and an `MCPServerRegistration`.

To register a server by hand instead, add its namespace to `extraRouteNamespaces` so its route can
attach. With tool authorization on, a server that is not in `servers` has no access entries, so
its tools are neither listed nor callable.

A credential Secret is created out of band, never from values, and needs the label
`mcp.kuadrant.io/secret=true`. The broker uses it for tool discovery and never sends it with a
client's tool call.

## Authentication

`auth.type` has no default, so an unauthenticated tool endpoint is a decision someone wrote down.

- `kubernetes` validates OpenShift user tokens and ServiceAccount tokens through a TokenReview.
- `jwt` validates tokens from an OpenID Connect issuer, such as Red Hat build of Keycloak, sets
  `oauthProtectedResource` so MCP clients discover the issuer, and answers an unauthenticated
  request with a `WWW-Authenticate` header that points at it.
- `none` creates no `AuthPolicy`.

The public `AuthPolicy` excludes `/.well-known/oauth-protected-resource`, which clients read
before they have a token. Both `AuthPolicy` objects use `mustonlyhave`: under `musthave`, a changed
`auth.type` would leave the old method beside the new one, and a request passes when any method
succeeds.

### TLS to Authorino

With Red Hat OpenShift AI on the cluster, Authorino serves TLS on its gRPC authorization port.
Kuadrant generates a plain text connection to it for each Gateway, and RHOAI's
odh-model-controller patches that to TLS for the MaaS gateway only. Without the same patch every
authenticated request through this gateway fails with HTTP 500, the Wasm log reports "gRPC status
code is not OK", and Authorino logs nothing. When `auth.type` is not `none` and
`gateway.authorinoTLS` is true (the default), this policy creates `EnvoyFilter` `<name>-authn-ssl`
in `openshift-ingress`, which gives the connection TLS, trusting the service CA. It uses Authorino's
Service in `toolAuthorization.kuadrantNamespace`. On a cluster where Authorino serves plain gRPC,
set `gateway.authorinoTLS` to `false`, which skips the EnvoyFilter.

## Tool authorization

With `toolAuthorization.enabled`, each `servers[].access` entry grants its `groups` and `users`
the listed tools of that server. A tool no entry lists is hidden from and refused to everyone. The
mapping lives in Git, in config, and applies on the next policy evaluation; nothing in the identity
provider has to change.

Who the caller is depends on `auth.type`:

| `auth.type` | Groups | User |
|---|---|---|
| `kubernetes` | the TokenReview's groups: OpenShift groups, and `system:serviceaccounts:<namespace>` for a ServiceAccount | the user name, `system:serviceaccount:<namespace>:<name>` for a ServiceAccount |
| `jwt` | the `groupsClaim` claim | the `usernameClaim` claim |

Two enforcement points, both rendered from the same access entries:

1. **`tools/list`.** The public `AuthPolicy` computes the caller's allowed tools per server and
   sends them to the broker as `x-authorized-tools`, a JWT that Authorino signs with the private
   key and that expires after five minutes. The broker verifies it with the public key named in the
   `MCPGatewayExtension` `trustedHeadersKey` and lists only those tools. A client cannot forge
   the header without the private key.
2. **`tools/call`.** The router routes a tool call through the internal listener, adding
   `x-mcp-servername` (`<namespace>/<registration name>`) and `x-mcp-toolname` (the tool name
   without its prefix). The `<name>-tools` `AuthPolicy` on that listener authenticates the caller
   again and allows the call only when an access entry for that server lists the tool and names the
   caller or one of the caller's groups. Anything else on that listener is denied.

Tools are named in `access` as the server names them, without the prefix. The broker filters by
exact name and has no wildcard, so every tool is listed.

`virtualServers` select a curated `tools/list` with the header
`X-Mcp-Virtualserver: <gateway namespace>/<name>`. They grant nothing: with tool authorization the
list a client sees is the intersection of the virtual server and the caller's access.

### The signing key

The key pair is a secret, so it never comes from values and no policy creates it. Create both
halves out of band, with the default names:

```bash
openssl ecparam -name prime256v1 -genkey -noout -out private-key.pem
openssl ec -in private-key.pem -pubout -out public-key.pem
oc create secret generic mcp-trusted-headers-private -n kuadrant-system \
  --from-file=key.pem=private-key.pem
oc create secret generic mcp-trusted-headers -n mcp-gateway --from-file=key=public-key.pem
```

The private key goes in the Kuadrant namespace because Authorino reads a signing key from the
namespace of the `AuthConfig` that Kuadrant generates, not from the `AuthPolicy` namespace. The
extension's own `trustedHeadersKey.generate: Enabled` is not used: it would put the private key in
the gateway namespace, where Authorino cannot read it. `policy-mcp-gateway-ready` reports either
Secret missing; until both exist, expect neither the extension nor the public `AuthPolicy` to become ready.

To rotate, replace both Secrets together.

## Disconnected clusters

No images are named here. The MCP Gateway controller and broker images are related images of the
Connectivity Link 1.5 bundle, so mirroring the `rhcl-operator` package on channel `stable-1.5`
carries them.

## Verifying

```bash
oc get kuadrantcontrolplane default -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}{"\n"}'
oc get mcpgatewayextensions.mcp.kuadrant.io -A
oc get mcpserverregistrations.mcp.kuadrant.io -A
oc get mcpvirtualservers.mcp.kuadrant.io -A
oc get gateways.gateway.networking.k8s.io -n openshift-ingress
oc get authpolicies.kuadrant.io -n openshift-ingress
```

### Call the gateway

The gateway's `/mcp` route answers only for the public host, so in-cluster or port-forwarded calls need that Host header:

```bash
oc port-forward -n openshift-ingress svc/<name>-openshift-default 8080:8080 &
TOKEN=$(oc create token <serviceaccount> -n <namespace> --duration=10m)
H=(-H "Host: mcp.<ingress domain>" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream")
curl -s -D - -o /dev/null "${H[@]}" http://localhost:8080/mcp -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}' | grep -i mcp-session-id
# then send notifications/initialized and tools/list with -H "Mcp-Session-Id: <id>"
```

Without a token the endpoint answers 401. A `tools/call` answered by the broker with "does not forward
tool calls" means the router EnvoyFilter is missing (see below).
