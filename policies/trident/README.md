With these Autoshift policies, you can automate the deployment of the Trident operator, the Trident Orchestrator operand, and manage its objects directly from source control.

The workflow is straightforward: first, it will deploy the operator, then deploy the Orchestrator once the operator is available. You will have the ability to manage your Trident content such as Trident Backends, storage classes, storage profiles, etc.

⚠️ Warning: These policies include Machine Configs that will trigger node reboots. ⚠️  There is an openshift bug that sets the same NQN ID on each node in the cluster, which causes issues in NetApp ONTAP when creating pvcs. This machine config will ensure each node has it's own unique NQN that reflects it's hostname. Example: nqn.2024-05.io.openshift:<HOSTNAME>. The other Machine Config will enable nvme-tcp in the kernal which is required for Trident to provision nvme storage.

NQN bug: https://issues.redhat.com/browse/RHEL-8041

You will have to create the secret for your trident credentials on the ACM cluster in the policies-autoshift namespace. You also have the option to utilize Vault Secrets to pull in your secret. 

oc create secret generic netapp-creds -n policies-autoshift --from-literal=username=vsadmin --from-literal=password='password'

In your `autoshift/values.hub.yaml` file, you can configure these toggles:

* `trident: true` → Autoshift will deploy the policies and start installing the operator and the orchestrator.
* `trident-config-<file type>-*: config.yaml.example` → List the files you want deployed when Trident is available. Each label must be unique, and must match the file name from files folder in your policy.
* `trident-creds-secret: secret-name` → This value will be the name of your Trident secret that exists on the hub in the policies-autoshift namespace. 

Once your configuration is ready, push your changes to your git repo.

Deployment typically takes around 15 minutes.