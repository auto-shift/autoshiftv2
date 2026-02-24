With these Autoshift policies, you can automate the deployment of the Trident operator, the Trident Orchestrator operand, and manage its objects directly from source control.

The workflow is straightforward:

- Deploy the Trident Operator.
- Deploy the Trident Orchestrator once the operator becomes available.
- Automatically manage Trident resources such as:
  - TridentBackendConfig
  - StorageClass
  - Storage Profiles
  - Additional Trident CRs

You can also create Trident backends and StorageClasses dynamically using ManagedCluster labels (see label section below).

------------------------------------------------------------
IMPORTANT WARNING
------------------------------------------------------------

WARNING: These policies include MachineConfigs that will trigger node reboots.

There is an OpenShift bug that sets the same NQN ID on each node in the cluster. This causes issues in NetApp ONTAP when creating PVCs.

Bug reference:
https://issues.redhat.com/browse/RHEL-8041

To resolve this:

- One MachineConfig ensures each node has a unique NQN reflecting its hostname.
  Example:
  nqn.2024-05.io.openshift:<HOSTNAME>

- Another MachineConfig enables nvme-tcp in the kernel, which is required for Trident to provision NVMe storage.

Plan maintenance windows accordingly.

------------------------------------------------------------
CREDENTIALS
------------------------------------------------------------

You must create the Trident credentials secret on the ACM hub cluster in the:

policies-autoshift namespace

Example:

oc create secret generic netapp-creds \
  -n policies-autoshift \
  --from-literal=username=vsadmin \
  --from-literal=password='password'

You may also integrate with Vault Secrets if desired.

------------------------------------------------------------
VALUES CONFIGURATION
------------------------------------------------------------

In autoshift/values.hub.yaml you must define:

trident: true

This enables the Trident deployment policies.

You must also define:

trident-creds-secret: netapp-creds

This is the name of the Trident credentials secret that exists on the hub
cluster in the policies-autoshift namespace.

Example values.hub.yaml:

trident: true
trident-creds-secret: netapp-creds

------------------------------------------------------------
DEPRECATED: trident-config-*-*
------------------------------------------------------------

This option is deprecated but still supported.

It allows you to drop raw YAML files into the policy's files directory and have them applied automatically via ConfigMaps once Trident is installed.

How it works (briefly):

- You place a YAML file (backend, storageclass, etc.) in the files folder.
- You reference that file using a trident-config-* value.
- The policy wraps the file into a ConfigMap.
- The ConfigMap content is decoded and applied to the managed cluster.

This provides flexibility for advanced or custom configurations.

However, label-driven backend automation is now the preferred and cleaner method.

------------------------------------------------------------
LABEL-DRIVEN BACKEND CREATION
------------------------------------------------------------

You can dynamically create TridentBackendConfig and StorageClass objects using ManagedCluster labels.


**Enable on a cluster:**

trident=true


**Create a backend (example increment 1):**

trident-backend-1-svm: Data-NFS-SVM-OpenShift

trident-backend-1-managementlif: 192.168.1.1

trident-backend-1-secret: netapp-creds

**That is the minimum required configuration.**

## Defaults:

storageDriverName = ontap-san 

sanType = nvme 

fsType = ext4 

allowVolumeExpansion = true 


**Generated Objects:**

TridentBackendConfig:
backend-data-nfs-svm-openshift-nvme

StorageClass:
data-nfs-svm-openshift-nvme


**To create multiple backends, increment the number:**

trident-backend-2-svm: Data-SVM-Backup

trident-backend-2-managementlif: 192.168.1.2

trident-backend-2-secret: netapp-creds

trident-backend-2-santype: iscsi

**Each increment creates:**

- A separate backend
- A separate StorageClass (with your trident backend as the storage pool)

------------------------------------------------------------
DEPLOYMENT
------------------------------------------------------------

Once your configuration is ready:

1. Commit changes to your Git repository.
2. Push to the branch watched by your GitOps pipeline.
3. ACM will reconcile and deploy.

Typical deployment time:
Approximately 15 minutes.

------------------------------------------------------------
SUMMARY
------------------------------------------------------------

This policy stack provides:

- Automated Trident operator installation
- Automated NVMe kernel configuration
- Automatic unique NQN enforcement
- GitOps-driven backend management
- Automatic StorageClass generation
- Fully label-driven multi-backend provisioning
- Optional legacy file-based injection via ConfigMaps

Everything is managed from source control.