With these Autoshift policies, you can automate the deployment of the Ansible Automation Platform (AAP) operator and manage its objects directly from source control. The policies will deploy the operator along with its controller, and optionally include the Hub or Ansible Lightspeed components.

The workflow is straightforward: first, it will deploy the operator, then deploy the AAP object once the operator is available. This AAP object manages the deployment of containers for the controller, Hub, and Lightspeed.

In your `autoshift/values.hub.yaml` file, you can configure these toggles:

* `aap: true` → Autoshift will deploy the policies and start installing the operator and its controller.
* `aap-hub-disabled: false` → Includes Hub in your AAP deployment.
* `aap-lightspeed-disabled: false` → Includes Lightspeed in your AAP deployment.
* `aap-file_storage: true` → Give a true or false on whether you want file storage to back your Hub content.
* `aap-file_storage_storage_class: ocs-storagecluster-cephfs` → Specify which storage class you will be using for that file storage.
* `aap-file_storage_size: 10G` → Specify the size you would like your PVC to claim for your hub content.
* `aap-s3-storage: false` → Give a true or false on whether you want ODF NooBa object storage to back your hub content. Your bucket will be created and attached to AAP automatically through Autoshift.


⚠️ **Important:** Hub requires a storage class with RWX access. Any other access mode will prevent Hub from storing content.

Once your configuration is ready, push your changes to your git repo.

Deployment typically takes around 30 minutes. After that:

1. Go to the console links in the top right hand corner and choose `Ansible Automation Platform`.
2. Retrieve your admin password: go to **Secrets → aap-admin-password**, scroll to **data**, and reveal the value.
3. Log in to AAP with username `admin` and the password from the secret.
4. Upload your manifest, and you’re ready to start using AAP!
