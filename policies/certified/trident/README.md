# Trident Storage Policy

This policy deploys and configures [NetApp Trident](https://docs.netapp.com/us-en/trident/) as a storage provisioner on OpenShift clusters managed by AutoShift. It creates the necessary Trident backends, secrets, and StorageClasses to enable dynamic persistent volume provisioning.

> **Note:** This policy only supports **NVMe/TCP** configurations. Other transport protocols (iSCSI, FC, NFS) are not supported.

## Enabling Trident

To enable Trident for a ClusterSet, add the following to your ClusterSet section. Set `trident` to `'true'` to enable installation:

```yaml
### Trident
trident: 'true'
trident-name: trident-operator
trident-install-plan-approval: Automatic
trident-source: certified-operators
trident-source-namespace: openshift-marketplace
trident-channel: stable
```

| Field | Description |
|---|---|
| `trident` | Set to `'true'` to enable, `'false'` to disable |
| `trident-name` | Name of the Trident operator subscription |
| `trident-install-plan-approval` | Operator install plan approval mode (`Automatic` or `Manual`) |
| `trident-source` | Operator catalog source |
| `trident-source-namespace` | Namespace of the catalog source |
| `trident-channel` | Operator update channel |

## Storage Configuration

Once enabled, add a `trident` block under your cluster's `config` section to configure storage backends:

```yaml
clusters:
  cluster-name:
    config:
      trident:
        storage:
          - backendName:        # Name for the Trident backend
            secretName:         # Secret name: hub-bootstrap remoteRef.key and the target in namespace trident
            svmLif:             # SVM NVMe/TCP data LIF IP address
            svmLif:             # SVM NVMe/TCP data LIF IP address
            storageClassName:   # Name of the StorageClass to create
            defaultStorageClass: # Set to "true" to make this the default StorageClass
            useREST:            # Must be "true" — ONTAP REST API is required
            authMethod:         # Authentication method (e.g. vsaadmin, cert)
```

Multiple storage backends can be defined by adding additional list entries under `storage`.

## Authentication

The `authMethod` field in each storage backend entry controls how Trident authenticates to the ONTAP SVM. Two methods are supported:

### `password` (default)

The policy does not copy credentials through a hub template. On every cluster it lands on (hub or spoke) it creates an `ExternalSecret` in namespace `trident` that pulls the whole remote object through ClusterSecretStore `hub-bootstrap` (`dataFrom.extract`, `key: secretName`). ESO writes the Kubernetes Secret that `TridentBackendConfig` references.

The Secret must already exist on the **parent hub** in the bootstrap store `remoteNamespace` (chart default `eso-shared`) under that same name. Land it there with `config.eso.secrets` on the parent hub, or seed it by hand. Set `secretStoreRef` on the storage entry only to use a store other than `hub-bootstrap`.

```yaml
- backendName: <tbc-name>
  secretName: trident-svm    # name on the parent hub, and the Secret name in trident
  authMethod: password
```

The Secret on the parent hub must have the following keys:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: trident-svm
  namespace: eso-shared   # hub-bootstrap remoteNamespace; do not set this in trident config
type: Opaque
stringData:
  username: <svm-username>
  password: <svm-password>
```

### `cert` / `certs`

When using certificate-based authentication, the policy pulls the private key from the `api-tls` Secret in `openshift-config` **on the managed cluster** (spoke `fromSecret`, not a hub copy).


```yaml
- backendName: <backend-name>
  secretName: <name-of-secret-on-hub>
  authMethod: certs
  secretNamespace: <namespace-where-secret-lives-on-hub>
```

### CA Bundle

Regardless of auth method, the storage config policy always pulls the trusted CA bundle from the `user-ca-bundle` ConfigMap in the `openshift-config` namespace on the managed cluster:

```
openshift-config/user-ca-bundle → ca-bundle.crt
```

**This ConfigMap must exist.** On OpenShift, it is managed by the cluster and populated when a custom PKI is configured. If your ONTAP SVM uses a certificate signed by a custom or internal CA, ensure that CA is included in the cluster's trusted bundle so Trident can verify the SVM's identity.
