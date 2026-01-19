With these Autoshift policies, you can automate the deployment of the Trident operator, the Trident Orchestrator operand, and manage its objects directly from source control.

The workflow is straightforward: first, it will deploy the operator, then deploy the Orchestrator once the operator is available. You will have the ability to manage your Trident content such as Trident Backends, storage classes, storage profiles, etc.

⚠️ Warning: These policies include Machine Configs that will trigger node reboots.

In your `autoshift/values.hub.yaml` file, you can configure these toggles:

You will have to create the secret for your trident credentials on the ACM cluster in the policies-autoshift namespace. You also have the option to utilize Vault Secrets to pull in your secret. 

oc create secret generic cle-svm1 -n policies-autoshift --from-literal=username=vsadmin --from-literal=password='password'

* `trident: true` → Autoshift will deploy the policies and start installing the operator and the orchestrator.
* `trident-config-<file type>-*: config.yaml.example` → List the files you want deployed when Trident is available. Each label must be unique, and must match the file name from files folder in your policy.
* `trident-vault-secrets: true` → Autoshift will enable the Vault Secrets policy within your templates folder. Please be sure to follow the instructions in the README.md provided with the vault secrets operator policy to ensure it is configured and used correctly.

Once your configuration is ready, push your changes to your git repo.

Deployment typically takes around 15 minutes.