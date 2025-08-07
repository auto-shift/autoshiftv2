# AutoShiftv2

## What is AutoShift?

AutoShiftv2 is an opinionated [Infrastructure-as-Code (IaC)](https://martinfowler.com/bliki/InfrastructureAsCode.html) framework designed to manage infrastructure components after an OpenShift installation using Advanced Cluster Management (ACM) and OpenShift GitOps. It provides a modular, extensible model to support infrastructure elements deployed on OpenShift — particularly those in [OpenShift Platform Plus](https://www.redhat.com/en/resources/openshift-platform-plus-datasheet). AutoShiftv2 emphasizes ease of adoption, configurable features (taggable on/off), and production-ready capabilities for installation, upgrades, and maintenance.

What AutoShift does is it uses OpenShift GitOps to declaratively manage RHACM which then manages various OpenShift and/or Kubernetes cluster resources and components. This eliminates much of the operator toil associated with installing and managing day 2 tasks, by letting declarative GitOps do that for you. 

## Architecture 

AutoShiftv2 is built on Red Hat Advanced Cluster Management for Kubernetes (RHACM) and OpenShift GitOps working in concert. 

RHACM provides visibility into OpenShift and Kubernetes clusters from a single pane of glass. RHACM provides built-in governance, cluster lifecycle management, application lifecycle management, and observability feature

OpenShift GitOps provides declarative GitOps for multicluster continuous delivery.

The hub cluster is the main cluster with RHACM with its core components installed on it, and is also hosting our OpenShift GitOps instance that we are using to declaratively manage RHACM.

### Hub Architecture
![alt text](images/AutoShiftv2-Hub.jpg)

### Hub of Hubs Architecture

[Red Hat YouTube: RHACM MultiCluster Global Hub](https://www.youtube.com/watch?v=jg3Zr7hFzhM)

![alt text](images/AutoShiftv2-HubOfHubs.jpg)

## Installation Instructions

### Assumptions / Prerequisites

* A Red Hat OpenShift cluster at 4.18+ to act as the **hub** cluster
* Fork or clone this repo on the machine from which you will be executing this repo
* [helm](https://helm.sh/docs/intro/install/) installed locally on the machine from which you will be executing this repo
* The OpenShift CLI [oc](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#installing-openshift-cli) client utility installed locally on the machine from which you will be executing this repo

### Prepping for Installation

1.  Login to the **hub** cluster via the [`oc` utility](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#cli-logging-in_cli-developer-commands).

    ```console
    oc login --token=sha256~lQ...dI --server=https://api.cluster.example.com:6443
    ```

> [!NOTE] 
> Alternatively you can use the devcontainer provided by this repository. By default the container will install the stable version of `oc` and the latest Red Hat provided version of `helm`. These versions can be specified by setting the `OCP_VERSION` and `HELM_VERSION` variables before building. From the container you can login as usual with `oc login` or copy your kubeconfig into the container `podman cp ${cluster_dir}/auth/kubeconfig ${container-name}:/workspaces/.kube/config`.

2.  If installing in a disconnected or internet-disadvantaged environment, update the values in `policies/openshift-gitops/values.yaml` and `policies/advanced-cluster-management/values.yaml` with the source mirror registry, otherwise leave these values as is.

3.  If your clone of AutoShiftv2 requires credentials or you would like to add credentials to any other git repos you can do this in the `openshift-gitops/values` file before installing. This can also be done in the OpenShift GitOps GUI after install.

### Installing OpenShift GitOps

1.  Using helm, install OpenShift GitOps

    ```console
    helm upgrade --install openshift-gitops openshift-gitops -f policies/openshift-gitops/values.yaml
    ```

> [!NOTE]  
> If OpenShift GitOps is already installed manually on cluster and the default argo instance exists this step can be skipped. Make sure that argocd controller has cluster-admin

2.  After the installation is complete, verify that all the pods in the `openshift-gitops` namespace are running. This can take a few minutes depending on your network to even return anything.

    ```console
    oc get pods -n openshift-gitops
    ```

    This command should return something like this:

    ```console
    NAME                                                      	      READY   STATUS    RESTARTS   AGE
    cluster-b5798d6f9-zr576                                   	      1/1 	  Running   0          65m
    kam-69866d7c48-8nsjv                                      	      1/1 	  Running   0          65m
    openshift-gitops-application-controller-0                 	      1/1 	  Running   0          53m
    openshift-gitops-applicationset-controller-6447b8dfdd-5ckgh       1/1 	  Running   0          65m
    openshift-gitops-dex-server-569b498bd9-vf6mr                      1/1     Running   0          65m
    openshift-gitops-redis-74bd8d7d96-49bjf                   	      1/1 	  Running   0          65m
    openshift-gitops-repo-server-c999f75d5-l4rsg              	      1/1 	  Running   0          65m
    openshift-gitops-server-5785f7668b-wj57t                  	      1/1 	  Running   0          53m
    ```

3.  Verify that the pod/s in the `openshift-gitops-operator` namespace are running.

    ```console
    oc get pods -n openshift-gitops-operator
    ```
    
    This command should return something like this:

    ```
    NAME                                                            READY   STATUS    RESTARTS   AGE
    openshift-gitops-operator-controller-manager-664966d547-vr4vb   2/2     Running   0          65m
    ```

4.  Now test if OpenShift GitOps was installed correctly, this may take some time

    ```console
    oc get argocd -A
    ```
    
    This command should return something like this:
    
    ```console
    NAMESPACE          NAME               AGE
    openshift-gitops   infra-gitops       29s
    ```

    If this is not the case you may need to run `helm upgrade ...` command again.

### Install Advanced Cluster Management (ACM)

1.  Using helm, install OpenShift Advanced Cluster Management on the hub cluster

    ```console
    helm upgrade --install advanced-cluster-management advanced-cluster-management -f policies/advanced-cluster-management/values.yaml
    ```

2.  Test if Red Hat Advanced Cluster Management has installed correctly, this may take some time

    ```console
    oc get mch -A -w
    ```

    This command should return something like this:

    ```console
    NAMESPACE                 NAME              STATUS       AGE     CURRENTVERSION   DESIREDVERSION
    open-cluster-management   multiclusterhub   Installing   2m35s                    2.13.2
    open-cluster-management   multiclusterhub   Installing   2m39s                    2.13.2
    open-cluster-management   multiclusterhub   Installing   3m12s                    2.13.2
    open-cluster-management   multiclusterhub   Installing   3m41s                    2.13.2
    open-cluster-management   multiclusterhub   Installing   4m11s                    2.13.2
    open-cluster-management   multiclusterhub   Installing   4m57s                    2.13.2
    open-cluster-management   multiclusterhub   Installing   5m15s                    2.13.2
    open-cluster-management   multiclusterhub   Installing   5m51s                    2.13.2
    open-cluster-management   multiclusterhub   Running      6m28s   2.13.2           2.13.2
    ```
> [!NOTE]  
> This does take roughly 10 min to install. You can proceed to installing AutoShift while this is installing but you will not be able to verify AutoShift or select a `clusterset` until this is finished.

### Install AutoShiftv2

> [!TIP]
> The previously installed OpenShift GitOps and ACM will be controlled by Autoshift after it is installed for version upgrading

1.  Update `autoshift/values.yaml` with desired feature flags and repo url as define in [Autoshift Cluster Labels Values Reference](#Autoshift-Cluster-Labels-Values-Reference)

2.  Using helm and the values you set for cluster labels, install autoshift. Here is an example using the extant hub values file:

    ```console
    helm template autoshift autoshift -f autoshift/values.hub.yaml | oc apply -f -
    ```

3.  Given the labels and cluster sets specified in the suplied values file, ACM cluster sets will be created. To view the cluster sets, In the OpenShift web console go to **All Clusters > Infrastructure > Clusters > Cluster Sets** in the ACM Console

    ![Cluster Sets in ACM Console](images/acm-cluster-sets.png)

4.  Manually select which cluster will belong to each cluster set, or when provisioning a new cluster from ACM you can select the desired cluster set from ACM at time of creation.

    ![Cluster Set Details in ACM Console](images/acm-add-hub-cluster.png)

5.  That's it. Welcome to OpenShift Platform Plus and all of it's many capabilities!

## Autoshift Cluster Labels Values Reference

Values can be set on a per cluster and clusterset level to decide what features of autoshift will be applied to each cluster. If a value is defined in helm values, a clusterset label and a cluster label precedence will be **cluster > clusterset > helm** values where helm values is the least. Helm values, `values.yaml` are meant to be defaults.


### Advanced Cluster Manager

> [!WARNING]  
> Hub Clusters Only

| Variable                    | Type      | Default Value             | 
|-----------------------------|-----------|---------------------------|
| `self-managed`              | bool      | `true` or `false`         |
| `acm-channel`               | string    | `release-2.13`            |
| `acm-install-plan-approval` | string    | `Automatic`               |
| `acm-source`                | string    | `redhat-operators`        |
| `acm-source-namespace`      | string    | `openshift-marketplace`   |
| `acm-availability-config`   | string    | `basic` or `high`         |

### OpenShift Gitops

| Variable                        | Type      | Default Value             | 
|---------------------------------|-----------|---------------------------|
| `gitops-channel`                | string    | `latest`                  |
| `gitops-install-plan-approval`  | string    | `Manual` or `Automatic`   |
| `gitops-source`                 | string    | `redhat-operators`        |
| `gitops-source-namespace`       | string    | `openshift-marketplace`   |

### Master Nodes

Single Node OpenShift clusters as well as Compact Clusters have to rely on their master nodes to handle workloads. You may have to increase the number of pods per node in these resource constrained environments.

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `master-nodes`                    | bool              | `false`                   |       |
| `master-max-pods`                 | Int               | `250`                     | The number of maximum pods per node. Up to 2500 supported dependent on hardware |

### Infra Nodes

| Variable                            | Type              | Default Value             | Notes |
|-------------------------------------|-------------------|---------------------------|-------|
| `infra-nodes`                       | int               |                           | Number of infra nodes min if autoscale. If not set infra nodes are not managed, if blank infra nodes will be deleted |
| `infra-nodes-numcpu`                | int               |                           | Number of cpu per infra node |
| `infra-nodes-memory-mib`            | int               |                           | Memory mib per infra node |
| `infra-nodes-numcores-per-socket`   | int               |                           | Number of CPU Cores per socket |
| `infra-nodes-zones`                 | <list<String>>    |                           | List of availability zones |

### Worker Nodes

| Variable                            | Type              | Default Value             | Notes |
|-------------------------------------|-------------------|---------------------------|-------|
| `worker-nodes`                      | int               |                           | Number of worker nodes min if autoscale. If not set worker nodes are not managed, if blank worker nodes will be deleted |
| `worker-nodes-numcpu`               | int               |                           | Number of cpu per worker node |
| `worker-nodes-memory-mib`           | int               |                           | Memory mib per worker node |
| `worker-nodes-numcores-per-socket`  | int               |                           | Number of CPU Cores per socket |
| `worker-nodes-zones`                | <list<String>>    |                           | list of availability zones

### Storage Nodes

| Variable                            | Type           | Default Value | Notes |
| ----------------------------------- | -------------- | ------------- | ----- |
| `storage-nodes`                     | int            |               | Number of storage nodes min if autoscale. If not set storage nodes are not managed, if blank storage nodes will be deleted. Local Storage Operator will be installed if Storage Nodes are enabled |
| `storage-nodes-numcpu`              | int            |               | Number of cpu per storage node  |
| `storage-nodes-memory-mib`          | int            |               | Memory mib per storage node |
| `storage-nodes-numcores-per-socket` | int            |               | Number of CPU Cores per socket |
| `storage-nodes-zones`               | <list<String>> |               | list of availability zones |
| `storage-nodes-instance-type`       | string         |               | Instance type for cloud provider |
| `storage-nodes-provider`            | string         |               | Provider type; valid choices: aws, vmware, baremetal |
| `storage-nodes-node-[iterator]`     | <list<String>> |               | List of node names to apply storage label to. Used for baremetal where MachineSets aren't used. |

### Advanced Cluster Security

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `acs`                             | bool              |                           | If not set Advanced Cluster Security will not be managed |
| `acs-channel`                     | String            | `stable`                  |       |
| `acs-install-plan-approval`       | String            | `Automatic`               |       |
| `acs-source`                      | String            | `redhat-operators`        |       |
| `acs-source-namespace`            | String            | `openshift-marketplace`   |       |

### Developer Spaces

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `dev-spaces`                          | bool              |                           | If not set Developer Spaces will not be managed |
| `dev-spaces-channel`                  | String            | `stable`                  |       |
| `dev-spaces-install-plan-approval`    | String            | `Automatic`               |       |
| `dev-spaces-source`                   | String            | `redhat-operators`        |       |
| `dev-spaces-source-namespace`         | String            | `openshift-marketplace`   |       |

### Developer Hub

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `dev-hub`                         | bool              |                           | If not set Developer Hub will not be managed |
| `dev-hub-channel`                 | String            | `fast`                    |       |
| `dev-hub-install-plan-approval`   | String            | `Automatic`               |       |
| `dev-hub-source`                  | String            | `redhat-operators`        |       |
| `dev-hub-source-namespace`        | String            | `openshift-marketplace`   |       |

### OpenShift Pipelines

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `pipelines`                       | bool              |                           | If not set OpenShift Pipelines will not be managed |
| `pipelines-channel`               | String            | `latest`                  |       |
| `pipelines-install-plan-approval` | String            | `Automatic`               |       |
| `pipelines-source`                | String            | `redhat-operators`        |       |
| `pipelines-source-namespace`      | String            | `openshift-marketplace`   |       |

### Trusted Artifact Signer

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `tas`                             | bool              |                           | If not set Trusted Artifact Signer will not be managed |
| `tas-channel`                     | String            | `latest`                  |       |
| `tas-install-plan-approval`       | String            | `Automatic`               |       |
| `tas-source`                      | String            | `redhat-operators`        |       |
| `tas-source-namespace`            | String            | `openshift-marketplace`   |       |

### Quay

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `quay`                            | bool              |                           | If not set Quay will not be managed |
| `quay-channel`                    | String            | `stable-3.13`             |       |
| `quay-install-plan-approval`      | String            | `Automatic`               |       |
| `quay-source`                     | String            | `redhat-operators`        |       |
| `quay-source-namespace`           | String            | `openshift-marketplace`   |       |

### Developer OpenShift Gitops

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `gitops-dev`                          | bool              |                           | If not set Developer OpenShift Gitops intances will not be managed |
| `gitops-dev-team-{INSERT_TEAM_NAME}`  | String        |                           | Team that can deploy onto cluster from dev team gitops. Must match a team in the `gitops-dev` helm chart values file |

### Loki

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `loki`                            | bool              |                           | If not set Loki will not be managed. Dependent on ODF Multi Object Gateway |
| `loki-channel`                    | String            | `stable-6.2`              |       |
| `loki-install-plan-approval`      | String            | `Automatic`               |       |
| `loki-source`                     | String            | `redhat-operators`        |       |
| `loki-source-namespace`           | String            | `openshift-marketplace`   |       |
| `loki-size`                       | String            | `1x.extra-small`          |       |
| `loki-storageclass`               | String            | `gp3-csi`                 |       |
| `loki-lokistack-name`             | String            | `logging-lokistack`       |       |

### OpenShift Logging

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `logging`                         | bool              |                           | If not set OpenShift Logging will not be managed, Dependent on Loki and COO |
| `logging-channel`                 | String            | `stable-6.2`              |       |
| `logging-install-plan-approval`   | String            | `Automatic`               |       |
| `logging-source`                  | String            | `redhat-operators`        |       |
| `logging-source-namespace`        | String            | `openshift-marketplace`   |       |

### Cluster Observability Operator

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `coo`                             | bool              |                           | If not set Cluster Observability Operator will not be managed |
| `coo-channel`                     | String            | `stable`                  |       |
| `coo-install-plan-approval`       | String            | `Automatic`               |       |
| `coo-source`                      | String            | `redhat-operators`        |       |
| `coo-source-namespace`            | String            | `openshift-marketplace`   |       |

### Compliance Operator STIG Apply

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `compliance`                          | bool              |                           | If not set Compliance Operator will not be managed. Helm chart config map must be set with profiles and remediations |
| `compliance-name`                     | String            | `compliance-operator`     |       |
| `compliance-install-plan-approval`    | String            | `Automatic`               |       |
| `compliance-source`                   | String            | `redhat-operators`        |       |
| `compliance-source-namespace`         | String            | `openshift-marketplace`   |       |
| `compliance-channel`                  | String            | `stable`                  |       |

### LVM Operator

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `lvm`                                 | bool              | `false`                   | If not set the LVM Operator will not be managed |
| `lvm-default`                         | bool              | `true`                    | Sets the lvm-operator as the default Storage Class |
| `lvm-fstype`                          | String            | `xfs`                     | Options `xfs` `ext4` |
| `lvm-size-percent`                    | Int               | `90`                      | Percentage of the Volume Group to use for the thinpool |
| `lvm-overprovision-ratio`             | Int               | `10`                      |       |

### Local Storage Operator

| Variable                              | Type              | Default Value             | Notes |
|---------------------------------------|-------------------|---------------------------|-------|
| `local-storage`                       | bool              |                           | if not set to true, local storage will not be managed or deployed. |
| `local-storage-channel`               | String            |                           |       |
| `local-storage-source`                | String            |                           |       |
| `local-storage-source-namespace`      | String            |                           |       |
| `local-storage-install-plan-approval` | String            |                           |       |

### OpenShift Data Foundation

| Variable                          | Type              | Default Value             | Notes |
|-----------------------------------|-------------------|---------------------------|-------|
| `odf`                             | bool              |                           | If not set OpenShift Data Foundation will not be managed. if Storage Nodes are enable will deploy ODF on local storage/ storage nodes |
| `odf-multi-cloud-gateway`         | String            |                           | values `standalone` or `standard`. Install ODF with only nooba object gateway or full odf |
| `odf-nooba-pvpool`                | bool              |                           | if not set nooba will be deployed with default settings. Recomended don't set for cloud providers. Use pv pool for storage |
| `odf-nooba-store-size`            | String            |                           | example `500Gi`. if pvpool set. Size of nooba backing store | 
| `odf-nooba-store-num-volumes`     | String            |                           | example `1`. if pvpool set. number of volumes |
| `odf-ocs-storage-class-name`      | String            |                           | if not using local-storage, storage class to use for ocs |
| `odf-ocs-storage-size`            | String            |                           | storage size per nvme |
| `odf-ocs-storage-count`           | String            |                           | number of replica sets of nvme drives, note total amount will count * replicas |
| `odf-ocs-storage-replicas`        | String            |                           | replicas, `3` is recommended |
| `odf-resource-profile`            | String            | `balanced`                | `lean`: suitable for clusters with limited resources, `balanced`: suitable for most use cases, `performance`: suitable for clusters with high amount of resources |
| `odf-channel`                     | String            | `stable-4.17`             |       |
| `odf-install-plan-approval`       | String            | `Automatic`               |       |
| `odf-source`                      | String            | `redhat-operators`        |       |
| `odf-source-namespace`            | String            | `openshift-marketplace`   |       |

### Kubernetes NMState Operator

The Kubernetes NMState Operator can be used to declaratively configure the Red Hat Core OS network settings. Common uses are adding bonds, vlans, and bridges. This helm chart works with AutoShift v2. This helm chart ingests every file in `policies/nmstate/files/` (with the exception of any file ending with the extension `.example`) and applies them as a ConfigMap on the hub cluster. Cluster labels are used to choose which cluster gets a configuration. If you want a cluster to have a config applied to it, you must label it with a label that starts with `autoshift.io/nmstate-nncp-` and has a value of the name of the file. For example the configuration file `example-1g-bond.yaml` can be applied to a cluster by applying label: `nmstate-nncp-example-bond1: example-1g-bond`. To further select specific nodes within a cluster, nodeSelectors can be used. To get started you can copy an example file such as `policies/nmstate/files/bond.yaml.example` and change the `nmstate:` configuration as required. The NMState YAML API is well documented at: [nmstate | A Declarative API for Host Network Management](https://nmstate.io/devel/yaml_api.html).

| Variable                        | Type           | Default Value         | Notes                                                                             |
| ------------------------------- | -------------- | --------------------- | --------------------------------------------------------------------------------- |
| `nmsate`                        | bool           | false                 | If not set the Kubernetes NMState Operator will not be managed                    |
| `nmstate-nncp`                  | <list<string>> | omitted               | Filename of NMState config that exists in files. Can be specified multiple times. |
| `nmstate-channel`               | string         | stable                |                                                                                   |
| `nmstate-install-plan-approval` | string         | Automatic             |                                                                                   |
| `nmstate-source`                | string         | redhat-operators      |                                                                                   |
| `nmstate-source-namespace`      | string         | openshift-marketplace |                                                                                   |

## References

* [OpenShift Platform Plus DataShift](https://www.redhat.com/en/resources/openshift-platform-plus-datasheet)
* [Red Hat Training: DO480: Multicluster Management with Red Hat OpenShift Platform Plus](https://www.redhat.com/en/services/training/do480-multicluster-management-red-hat-openshift-platform-plus)
* [Martin Fowler Blog: Infrastructure As Code](https://martinfowler.com/bliki/InfrastructureAsCode.html)
* [helm Utility Installation Instructions](https://helm.sh/docs/intro/install/)
* [OpenShift CLI Client `oc` Installation Instructions](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#installing-openshift-cli)
