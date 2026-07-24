# Vault (production)

Deploys HashiCorp Vault from the upstream Helm chart, wrapped by PolicyGenerator, as a **production**
HA cluster:

- **HA**: raft integrated storage, 3 nodes, PodDisruptionBudget, `retry_join` auto-join
- **Persistence**: a `data` PVC per node (`autoshift.io/vault-storage-size`, `-storage-class`)
- **TLS**: serving cert from cert-manager (`vault-tls`), listener + raft peers use HTTPS
- **Injector**: the agent-injector sidecar is enabled for secret injection into workloads
- **Resources**: requests/limits on server + injector

## Requirements
- `autoshift.io/vault: 'true'` on the clusterset/cluster.
- **cert-manager** operator + a **ClusterIssuer** (default `autoshift-ca`; override with
  `autoshift.io/vault-tls-issuer`). The policy depends on `policy-cert-manager-operator-install`.

## Seal backend (auto-unseal) — `autoshift.io/vault-seal-type`

The seal backend is a **per-cluster label**, so the same chart serves every environment and both
topologies. Init is automated by a Job (`policy-vault-init`) on auto-unseal clusters; with an
auto-unseal seal, `vault operator init` also unseals — **no unseal keys are ever stored, no cron**.
Init produces only break-glass *recovery* keys (stashed in `secret/vault-init` for operators to retrieve,
secure, and delete).

| `vault-seal-type` | Topology | Unseal | Hub blast radius |
|---|---|---|---|
| `shamir` (default) | independent | **manual ceremony** (below) | none |
| `awskms` / `gcpckms` / `azurekeyvault` | independent | auto (cloud KMS + workload identity) | none |
| `transit` | two-tier | auto (via the hub Vault) | yes (opt-in) |

**Independent vs two-tier is a blast-radius choice.** Cloud-KMS and Shamir clusters have their own root
of trust and no hub dependency. `transit` clusters auto-unseal against a central hub Vault — convenient
and centrally recoverable, but a compromised/unavailable hub affects them. Pick per cluster.

### Two-tier (`transit`)
- Mark the hub Vault `autoshift.io/vault-transit-provider: 'true'` and set
  `autoshift.io/vault-external-hostname` to its external Route hostname. `policy-vault-transit-provider`
  configures the transit engine + `autoshift-unseal` key + a scoped token + a recovery KV; the chart
  exposes a **passthrough Route** at that hostname and adds it to the Vault TLS cert's SANs.
- **Spokes just set `vault-seal-type: 'transit'`** — the unseal address is **auto-discovered**: the seal
  config's `{{hub}}` template resolves on the hub and `lookup`s the hub Vault Route host there, so spokes
  need no address config and never drift from the hub's actual Route. (`vault-transit-address` remains an
  optional override for an external LB / custom DNS / a non-local hub.) The in-cluster Service is never
  used — it's unreachable cross-cluster. `policy-vault-transit-token` copies the provider's token onto
  each spoke; the spoke transit seal uses it. Spoke + hub share the **autoshift-ca** issuer and the Route
  is passthrough, so the spoke validates the hub cert against autoshift-ca (hence the Route hostname is in
  the hub cert SANs, handled above).
- Recovery keys for spokes are stored in the hub Vault's recovery KV — self-contained, no cloud dependency.

### Cloud KMS (`awskms` / `gcpckms` / `azurekeyvault`)
Set `vault-seal-type` + the `vault-seal-kms-*` labels (region, key id, project/key-ring, tenant/vault
name). Grant the KMS via **workload identity** (IRSA / GKE WI / Azure WI) on the Vault ServiceAccount —
no stored credentials. Fully independent auto-unseal.

### Shamir (default) — one-time manual ceremony
Shamir's unseal keys **are** the root of trust; storing them anywhere automated defeats the seal, so this
tier keeps a manual bootstrap (and manual unseal after each restart). Use it for air-gap or when a cluster
must not depend on a hub or cloud KMS. After the pods are Running (they stay `NotReady` until unsealed):

```sh
oc exec -n vault vault-0 -- vault operator init          # SAVE the unseal keys + root token securely
oc exec -n vault vault-0 -- vault operator unseal <key>  # x3 (threshold), on each pod
```

A **Shamir hub** that is *also* the transit provider: do the ceremony above, then put its root/admin token
in `secret/vault-bootstrap-token` (key `token`) so `policy-vault-transit-provider` can configure transit.

## Disconnected / air-gap

- **Seal choice**: use **`transit`** (two-tier — fleet-internal, no internet) or **`shamir`**. Cloud KMS
  (`awskms`/`gcpckms`/`azurekeyvault`) needs the cloud KMS API and does **not** work air-gapped. The
  `autoshift-ca` issuer is self-signed, so TLS trust needs no external CA.
- **Images** — mirror the tag-referenced chart images:
  - `docker.io/hashicorp/vault:1.17.2` (server + injector agent), `docker.io/hashicorp/vault-k8s:1.4.2`
    (injector). They are in `scripts/known-additional-images.json` (key `vault`), so
    `scripts/generate-imageset-config.sh` adds them to the imageset when `vault: 'true'`; `oc mirror`
    copies them to the mirror registry.
  - Add a **tag** redirect to your disconnected config `mirrorRegistry.tagMirrors`
    (`source: docker.io/hashicorp`) — tag-referenced images need an ImageTagMirrorSet, not IDMS.
  - Set `autoshift.io/vault-job-image` to a mirrored CLI image on baremetal/clusters without the internal
    image registry (the init/transit-provider Jobs default to the internal `openshift/cli`).
- **Deploy mode**: use **OCI mode**. The upstream Vault chart is rendered to plain manifests at release
  time (baked into the OCI artifact), so nothing pulls `helm.releases.hashicorp.com` on the cluster. Git
  mode (on-cluster CMP render) would pull the chart at deploy time and needs the chart vendored/mirrored.

## Versions & updating

Two **decoupled** version knobs:

| Knob | Where | Scope |
|---|---|---|
| **Helm chart** version | `manifests/vault/kustomization.yaml` `helmCharts[].version` | repo-level (build-time pull; **not** a per-cluster label) — controls the deployment scaffolding |
| **Vault server** image tag | `autoshift.io/vault-version` label (default in the chart's `server.image.tag` lookup) | per-cluster override; controls the running Vault binary |

The injector image (`vault-k8s`) has no explicit tag, so it follows the **chart** default.

### Bump the Vault server version
Set `autoshift.io/vault-version` on the clusterset/cluster (or change its default in the chart lookup +
`_example.yaml`). No chart change needed. Also update the mirror tag (below) for disconnected.

### Bump the Helm chart version (manual — no auto-bumper)
1. Edit `manifests/vault/kustomization.yaml` → `helmCharts[].version`.
2. Read the new chart's default image tags:
   `helm show values hashicorp/vault --version <new> | grep -B1 -A2 -E 'image:|tag:'`
3. **Keep these in sync with the new image tags** (drift here breaks disconnected mirroring):
   - `scripts/known-additional-images.json` (`vault` key) — server + `vault-k8s` tags. **Required** so
     `oc mirror` copies the right images.
   - `autoshift.io/vault-version` default (chart lookup + `_example.yaml`) — if the default server should track the chart.
   - `README.md` + `_example.yaml` mirror comment (docs).
4. Re-render to confirm the pull:
   `KUSTOMIZE_PLUGIN_HOME=.tools/kustomize-plugin POLICY_GEN_ENABLE_HELM=true .tools/kustomize build --enable-alpha-plugins --enable-helm --load-restrictor LoadRestrictionsNone manifests/vault`
   (repulls into the gitignored `charts/vault-<new>/`; the old cache is stale, safe to delete).
5. `cd tools && go test -tags integration ./internal/resolver/...`, then re-release (OCI).
