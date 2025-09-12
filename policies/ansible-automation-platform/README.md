With these Autoshift policies, you can automate the deployment of the Ansible Automation Platform (AAP) operator and manage its objects directly from source control. The policies will deploy the operator along with its controller, and optionally include the Hub or Ansible Lightspeed components.

The workflow is straightforward: first, it will deploy the operator, then deploy the AAP object once the operator is available. This AAP object manages the deployment of containers for the controller, Hub, and Lightspeed.

In your `autoshift/values.hub.yaml` file, you can configure these toggles:

* `aap: true` → Autoshift will deploy the policies and start installing the operator and its controller.
* `aap-hub-disabled: false` → Includes Hub in your AAP deployment.
* `aap-eda-disabled: false` → Includes Hub in your AAP deployment.
* `aap-file-storage: true` → Deploys AAP using file storage of your choice.
* `aap-s3-storage: true` → Deploys AAP using NooBa S3 object storage. 

⚠️ **Important:** Hub requires a storage class with RWX access. Any other access mode will prevent Hub from storing content.

Once your configuration is ready, push your changes to your git repo.

Deployment typically takes around 30 minutes. After that:

1. Go to **Routes** in the `ansible-automation-platform` namespace and open the AAP route URL.
2. Retrieve your admin password: go to **Secrets → aap-admin-password**, scroll to **data**, and reveal the value.
3. Log in to AAP with username `admin` and the password from the secret.
4. Upload your manifest, and you’re ready to start using AAP!
