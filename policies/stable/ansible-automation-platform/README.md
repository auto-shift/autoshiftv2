With these Autoshift policies, you can automate the deployment of the Ansible Automation Platform (AAP) operator and manage its objects directly from source control. The policies will deploy the operator along with its controller, and optionally include the Hub or Ansible Lightspeed components.

The workflow is straightforward: first, it will deploy the operator, then deploy the AAP object once the operator is available. This AAP object manages the deployment of containers for the controller, Hub, and Lightspeed.

Labels in your clusterset values file (for example, `autoshift/values/clustersets/hub.yaml`) install the operator and select modes:

* `aap: 'true'` → AutoShift deploys the policies and installs the operator and its controller.
* `aap-noobaa-s3-storage: 'true'` → Creates an ODF NooBaa bucket in the AAP namespace and backs Hub content with it.
* `aap-custom-cabundle: 'true'` → Injects the cluster CA bundle into AAP.

## Configuring the instance

Everything else about the `AnsibleAutomationPlatform` goes in `config.aap.instance`, which accepts any field of its spec:

```yaml
config:
  aap:
    instance:
      eda:
        disabled: false
      hub:
        storage_type: file
        file_storage_size: 10Gi
        file_storage_storage_class: ocs-storagecluster-cephfs
```

The spec is built in layers, each merged over the one before:

1. A base: controller and Hub on, EDA and Lightspeed off, `redis_mode: standalone`.
2. Deprecated labels, only when set (see below).
3. The fields the mode labels add, such as the NooBaa S3 Secret.
4. `config.aap.instance`.

* Maps merge key by key, and the later value wins, including `false`.
* Lists replace the earlier list whole.
* A field an earlier layer adds cannot be removed from config, only changed.
* The policy reads the cluster's `rendered-config` ConfigMap and reports an error if it is missing, rather than rendering the base alone.

Fields use the custom resource's own snake_case names, not AutoShift's lowerCamelCase. List them with `oc explain ansibleautomationplatform.spec --recursive`.

* A misspelled top-level field is rejected. `policy-aap-instance` goes NonCompliant with `unknown field` and applies nothing.
* A misspelled field inside `hub`, `controller` or `eda` is accepted and stored, but the operator ignores it and nothing reports it.

### Checking the result

`policy-aap-instance` confirms only that the spec was applied. `policy-aap-ready-test` is an inform policy that stays NonCompliant until the instance reports its last reconcile `Successful`, which covers every enabled component. After changing the configuration, check that one.

Disabling a component that has already been deployed, for example setting `hub.disabled: true` after Hub was running, does not remove its custom resource. The operator keeps it, and if it was failing, the instance stays not `Successful` and `policy-aap-ready-test` stays NonCompliant. Delete the leftover resource, such as `oc delete automationhub aap-hub -n ansible-automation-platform`.

### Bringing your own S3 bucket

Create a Secret in `ansible-automation-platform` on the cluster, never in a values file, with `s3-access-key-id`, `s3-secret-access-key` and `s3-bucket-name`, plus `s3-region` or `s3-endpoint`. Then name it:

```yaml
config:
  aap:
    instance:
      hub:
        storage_type: S3
        object_storage_s3_secret: aap-s3
```

`policy-aap-hub-s3-test` is an inform policy that reports NonCompliant when Hub uses S3 and the Secret it names is missing or lacks a required key. It reads the live instance, so it covers NooBaa as well.

### Deprecated labels

These labels still work when set, and `config.aap.instance` wins over them. Move each to its config field:

| Label | `config.aap.instance` field |
|-------|-----------------------------|
| `aap-hub-disabled` | `hub.disabled` |
| `aap-eda-disabled` | `eda.disabled` |
| `aap-lightspeed-disabled` | `lightspeed.disabled` |
| `aap-file-storage: 'true'` | `hub.storage_type: file` |
| `aap-storage-type` | `hub.storage_type` |
| `aap-file_storage_size` | `hub.file_storage_size` |
| `aap-file_storage_storage_class` | `hub.file_storage_storage_class` |

⚠️ **Important:** Hub requires a storage class with RWX access. Any other access mode will prevent Hub from storing content.

Once your configuration is ready, push your changes to your git repo.

Deployment typically takes around 30 minutes. After that:

1. Go to the console links in the top right hand corner and choose `Ansible Automation Platform`.
2. Retrieve your admin password: go to **Secrets → aap-admin-password**, scroll to **data**, and reveal the value.
3. Log in to AAP with username `admin` and the password from the secret.
4. Upload your manifest, and you’re ready to start using AAP!
