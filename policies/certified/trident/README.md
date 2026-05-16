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
            secretName:         # Name of the secret containing SVM credentials
            secretNamespace:    # Namespace where the secret lives
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

The policy will copy `username` and `password` from a secret you pre-create on the **hub cluster**. The secret must exist in the namespace specified by `secretNamespace` and be named to match `secretName` before the policy runs. These credentials will be the credentials you use to reach the management LIF. The policy reads those values from the hub and propagates them as a new secret into the `trident` namespace on the managed cluster.

```yaml
- backendName: <tbc-name>
  secretName: <name-of-secret-on-hub>
  secretNamespace: <namespace-where-secret-lives-on-hub>   # namespace on the hub where your secret lives
  authMethod: password
```

The secret on the hub must have the following keys:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-svm-secret
  namespace: my-hub-namespace
type: Opaque
stringData:
  username: <svm-username>
  password: <svm-password>
```

### `cert` / `certs`

When using certificate-based authentication, the policy pulls the private key directly from the `api-tls` secret in the `openshift-config` namespace on the managed cluster. **This means a valid `api-tls` secret must already exist in `openshift-config`** — this is typically the cluster's API TLS certificate and is present on any standard OpenShift cluster.


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
