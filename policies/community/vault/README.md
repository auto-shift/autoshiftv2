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
