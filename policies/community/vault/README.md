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

## One-time operational step (NOT declarative)
Vault comes up **sealed and uninitialized** — an imperative bootstrap that GitOps/ACM cannot express,
and whose output (unseal keys + root token) is sensitive. After the pods are Running (they stay
`NotReady` until unsealed), initialize and unseal **once**:

```sh
oc exec -n vault vault-0 -- vault operator init      # SAVE the unseal keys + root token securely
oc exec -n vault vault-0 -- vault operator unseal <key>   # x3 (threshold), on each pod
```

For **auto-unseal** (recommended in production, avoids manual unseal after every restart), add a KMS
`seal` stanza to the raft `config` in `manifests/vault/kustomization.yaml` (e.g. `seal "awskms"` with a
KMS key + IAM/IRSA on the Vault ServiceAccount). Then only `vault operator init` is needed once.
