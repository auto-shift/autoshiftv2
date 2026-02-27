## Autoshift Trident Automation ##

With these Autoshift policies, you can automate the deployment of the
Trident Operator, the Trident Orchestrator operand, and manage its
objects directly from source control.


**OVERVIEW**

The workflow is straightforward:

  - Deploy the Trident Operator
  - Deploy the Trident Orchestrator once the operator becomes available
  - Manage Trident resources such as:
      - TridentBackendConfig
      - StorageClass

Backends can be created using:

  - Label-driven configuration (recommended)
  - File-based configuration (legacy)


**IMPORTANT WARNING**

These policies include MachineConfigs that will trigger node reboots.

There is an OpenShift bug that sets the same NQN ID on each node in the
cluster. This causes issues in NetApp ONTAP when creating PVCs.

Bug reference:
https://issues.redhat.com/browse/RHEL-8041

To resolve this:

  - One MachineConfig ensures each node has a unique NQN reflecting its
    hostname.

      Example:
      nqn.2024-05.io.openshift:<HOSTNAME>

  - Another MachineConfig enables nvme-tcp in the kernel, which is
    required for Trident to provision NVMe storage.

Plan maintenance windows accordingly.


## CREDENTIALS

You must create the Trident credentials secret on the ACM hub cluster
in the policies-autoshift namespace.

Example:

oc create secret generic netapp-creds \
  -n policies-autoshift \
  --from-literal=username=vsadmin \
  --from-literal=password='password'

You may also integrate with Vault Secrets if desired.


## VALUES CONFIGURATION

**In autoshift/values.hub.yaml you must define:**

  trident: true <br>
  trident-creds-secret: netapp-creds <br>

**Enable label-driven configuration:**

  trident-label-config: true

**Optional legacy file-based configuration:**

**Example values.hub.yaml:**

  trident: true <br>
  trident-creds-secret: netapp-creds <br>
  trident-label-config: true <br>


## LABEL-DRIVEN CONFIGURATION (RECOMMENDED) ##

**Enable:**

  trident-label-config: true 

This dynamically creates TridentBackendConfig and StorageClass objects
using ManagedCluster labels.

**Example cluster labels:**

  trident-backend-1-svm: Data-NFS-SVM-OpenShift <br>
  trident-backend-1-managementlif: 192.168.1.1 <br>
  trident-backend-1-secret: netapp-creds

**Defaults:**

  storageDriverName = ontap-san <br>
  sanType = nvme <br>
  fsType = ext4 <br>
  allowVolumeExpansion = true <br>

**Generated objects:**

  TridentBackendConfig:
    backend-data-nfs-svm-openshift-nvme

  StorageClass:
    data-nfs-svm-openshift-nvme

**To create multiple backends, increment the number:**

  trident-backend-2-svm: Data-SVM-Backup <br>
  trident-backend-2-managementlif: 192.168.1.2 <br>
  trident-backend-2-secret: netapp-creds <br>
  trident-backend-2-santype: iscsi <br>

**Each increment creates:**

  - A separate backend
  - A separate StorageClass


## FILE-BASED CONFIGURATION (LEGACY FEATURE) ##


This is a legacy compatibility feature and is not the preferred method.

It allows you to drop raw YAML files into the policy's files directory
and have them applied automatically via ConfigMaps once Trident is
installed.

How it works:

  - Place a YAML file (backend, storageclass, etc.) in the files folder
  - Reference that file using a trident-config-* value
  - The policy wraps the file into a ConfigMap
  - The ConfigMap content is decoded and applied to the managed cluster

This method exists for:

  - Backward compatibility
  - Highly customized backend definitions
  - Advanced ONTAP configuration not covered by labels

**To pull in multiple files, increment the number:**

  trident-config-prodsvm: filename.yaml <br>

*New deployments should use label-driven automation.*


## DEPLOYMENT

Once your configuration is ready:

  1. Commit changes to your Git repository
  2. Push to the branch watched by your GitOps pipeline
  3. ACM will reconcile and deploy

**Typical deployment time: approximately 15 minutes.**


## SUMMARY

This policy stack provides:

  - Automated Trident operator installation
  - Automated NVMe kernel configuration
  - Automatic unique NQN enforcement
  - GitOps-driven backend management
  - Recommended label-based backend automation
  - Legacy file-based backend support
  - Automatic StorageClass generation
  - Multi-backend provisioning support

Everything is managed from source control.