# AutoShift Quick Start Guide

This guide walks through a complete AutoShift installation from start to finish.

## Prerequisites

* A Red Hat OpenShift cluster at 4.20+ to act as the **hub** cluster
* [helm](https://helm.sh/docs/intro/install/) installed locally
* The OpenShift CLI [oc](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#installing-openshift-cli) installed locally
* Fork or clone of this repository (for Source installation)

### Minimum Hub Cluster Requirements

All hub clusters **must** have the following configuration in their `hubClusterSets`:

* `gitops: 'true'` - OpenShift GitOps (ArgoCD) is required to deploy AutoShift
* ACM is automatically installed on all hub clustersets by policy (no labels required)

## Choose Your Installation Method

| | **Source (Git)** | **OCI (Registry)** |
|---|---|---|
| **Best for** | Development, customization, getting started | Production, version-pinned deployments |
| **Bootstrap from** | Local git clone | OCI artifacts from Quay |
| **Git clone required** | Yes | No |
| **Customizable policies** | Edit directly in repo | Fork or overlay |
| **Air-gapped support** | Mirror git repo | Mirror OCI registry |

---

## Installation from Source

### Step 1: Login to the Hub Cluster

Login to the **hub** cluster via the [`oc` utility](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#cli-logging-in_cli-developer-commands).

```console
oc login --token=sha256~lQ...dI --server=https://api.cluster.example.com:6443
```

> [!NOTE]
> Alternatively you can use the devcontainer provided by this repository. By default the container will install the stable version of `oc` and the latest Red Hat provided version of `helm`. These versions can be specified by setting the `OCP_VERSION` and `HELM_VERSION` variables before building. From the container you can login as usual with `oc login` or copy your kubeconfig into the container `podman cp ${cluster_dir}/auth/kubeconfig ${container-name}:/workspaces/.kube/config`.

If installing in a disconnected or internet-disadvantaged environment, update the values in `policies/openshift-gitops/values.yaml` and `policies/advanced-cluster-management/values.yaml` with the source mirror registry, otherwise leave these values as is.

If your clone of AutoShiftv2 requires credentials or you would like to add credentials to any other git repos you can do this in the `openshift-gitops/values` file before installing. This can also be done in the OpenShift GitOps GUI after install.

### Step 2: Install OpenShift GitOps

Using helm, install OpenShift GitOps:

```console
helm upgrade --install openshift-gitops openshift-gitops -f policies/openshift-gitops/values.yaml
```

> [!NOTE]
> If OpenShift GitOps is already installed manually on cluster and the default argo instance exists this step can be skipped. Make sure that argocd controller has cluster-admin

After the installation is complete, verify that all the pods in the `openshift-gitops` namespace are running. This can take a few minutes depending on your network to even return anything.

```console
oc get pods -n openshift-gitops
```

This command should return something like this:

```console
NAME                                                                READY   STATUS    RESTARTS   AGE
cluster-b5798d6f9-zr576                                             1/1     Running   0          65m
kam-69866d7c48-8nsjv                                                1/1     Running   0          65m
openshift-gitops-application-controller-0                           1/1     Running   0          53m
openshift-gitops-applicationset-controller-6447b8dfdd-5ckgh         1/1     Running   0          65m
openshift-gitops-dex-server-569b498bd9-vf6mr                        1/1     Running   0          65m
openshift-gitops-redis-74bd8d7d96-49bjf                             1/1     Running   0          65m
openshift-gitops-repo-server-c999f75d5-l4rsg                        1/1     Running   0          65m
openshift-gitops-server-5785f7668b-wj57t                            1/1     Running   0          53m
```

Verify that the pod/s in the `openshift-gitops-operator` namespace are running.

```console
oc get pods -n openshift-gitops-operator
```

This command should return something like this:

```
NAME                                                            READY   STATUS    RESTARTS   AGE
openshift-gitops-operator-controller-manager-664966d547-vr4vb   2/2     Running   0          65m
```

Test if OpenShift GitOps was installed correctly, this may take some time:

```console
oc get argocd -A
```

This command should return something like this:

```console
NAMESPACE          NAME               AGE
openshift-gitops   infra-gitops       29s
```

If this is not the case you may need to run `helm upgrade ...` command again.

### Step 3: Install Advanced Cluster Management (ACM)

Using helm, install OpenShift Advanced Cluster Management on the hub cluster:

```console
helm upgrade --install advanced-cluster-management advanced-cluster-management -f policies/advanced-cluster-management/values.yaml
```

Test if Red Hat Advanced Cluster Management has installed correctly, this may take some time:

```console
oc get mch -A -w
```

This command should return something like this:

```console
NAMESPACE                 NAME              STATUS       AGE     CURRENTVERSION   DESIREDVERSION
open-cluster-management   multiclusterhub   Installing   2m35s                    2.13.2
open-cluster-management   multiclusterhub   Installing   3m41s                    2.13.2
open-cluster-management   multiclusterhub   Installing   5m15s                    2.13.2
open-cluster-management   multiclusterhub   Running      6m28s   2.13.2           2.13.2
```

> [!NOTE]
> This does take roughly 10 min to install. You can proceed to installing AutoShift while this is installing but you will not be able to verify AutoShift or select a `clusterset` until this is finished.

### Step 4: Install AutoShift

> [!TIP]
> The previously installed OpenShift GitOps and ACM will be controlled by AutoShift after it is installed for version upgrading

Update your values file with desired feature flags and repo url as defined in the [Autoshift Cluster Labels Values Reference](values-reference.md).

Using helm and the values you set for cluster labels, install AutoShift. Here is an example using the hub values file:

```console
export APP_NAME="autoshift"
export REPO_URL="https://github.com/auto-shift/autoshiftv2.git"
export TARGET_REVISION="main"
export VALUES_FILE="values/global.yaml"
export VALUES_FILE_2="values/clustersets/hub.yaml"
export VALUES_FILE_3="values/clustersets/managed.yaml"
export ARGO_PROJECT="default"
export GITOPS_NAMESPACE="openshift-gitops"
cat << EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: $APP_NAME
  namespace: $GITOPS_NAMESPACE
spec:
  destination:
    namespace: ''
    server: https://kubernetes.default.svc
  source:
    path: autoshift
    repoURL: $REPO_URL
    targetRevision: $TARGET_REVISION
    helm:
      valueFiles:
        - $VALUES_FILE
        - $VALUES_FILE_2
        - $VALUES_FILE_3
      values: |-
        autoshiftGitRepo: $REPO_URL
        autoshiftGitBranchTag: $TARGET_REVISION
  sources: []
  project: $ARGO_PROJECT
  syncPolicy:
    automated:
      prune: false
      selfHeal: true
EOF
```

### Step 5: Assign Clusters to ClusterSets

Given the labels and cluster sets specified in the supplied values file, ACM cluster sets will be created. Add the hub cluster (`local-cluster`) to the appropriate clusterset:

```console
# Replace 'hub' with the name of your clusterset
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub --overwrite
```

For managed clusters, assign them to their clusterset the same way:

```console
oc label managedcluster <cluster-name> cluster.open-cluster-management.io/clusterset=managed --overwrite
```

Alternatively, you can assign clusters via the ACM Console at **All Clusters > Infrastructure > Clusters > Cluster Sets**. When provisioning a new cluster from ACM, you can also select the desired clusterset at time of creation.

### Step 6: Verify

```bash
# Check ArgoCD Application
oc get application autoshift -n openshift-gitops

# Check individual policy Applications
oc get applications -n openshift-gitops | grep autoshift

# Check ACM policies
oc get policies -A

# View policy compliance
oc get policies -n open-cluster-policies
```

That's it. Welcome to OpenShift Platform Plus and all of its many capabilities!

---

## Installation from OCI Release

For production or version-pinned deployments, AutoShift can be installed directly from OCI artifacts hosted on Quay — no git clone required.

### Option A: Using the Install Scripts

Download the scripts from the [latest release](https://github.com/auto-shift/autoshiftv2/releases) and run them:

```bash
curl -sL https://github.com/auto-shift/autoshiftv2/releases/latest/download/install-bootstrap.sh -O
curl -sL https://github.com/auto-shift/autoshiftv2/releases/latest/download/install-autoshift.sh -O
chmod +x install-*.sh

# Bootstrap GitOps and ACM
./install-bootstrap.sh

# Wait for ACM to be ready
oc get mch -A -w

# Install AutoShift (accepts: hub, minimal, sbx, hubofhubs)
./install-autoshift.sh hub
```

### Option B: Manual OCI Installation

If you prefer to run the commands directly without the scripts:

#### Step 1: Login to the Hub Cluster

```console
oc login --token=sha256~lQ...dI --server=https://api.cluster.example.com:6443
```

#### Step 2: Bootstrap GitOps from OCI

```bash
export OCI_REPO="oci://quay.io/autoshift"
# export VERSION="X.Y.Z"   # Uncomment to pin to a specific version

helm upgrade --install openshift-gitops ${OCI_REPO}/bootstrap/openshift-gitops \
    ${VERSION:+--version ${VERSION}} \
    --create-namespace \
    --wait \
    --timeout 10m
```

Verify GitOps is running:

```console
oc get pods -n openshift-gitops
oc get argocd -A
```

#### Step 3: Bootstrap ACM from OCI

```bash
helm upgrade --install advanced-cluster-management ${OCI_REPO}/bootstrap/advanced-cluster-management \
    ${VERSION:+--version ${VERSION}} \
    --create-namespace \
    --wait \
    --timeout 15m
```

Wait for ACM to be ready:

```console
oc get mch -A -w
```

> [!NOTE]
> This does take roughly 10 min to install. You can proceed to installing AutoShift while this is installing but you will not be able to verify AutoShift or select a `clusterset` until this is finished.

#### Step 4: Deploy AutoShift from OCI

Create the ArgoCD Application pointing to the OCI registry. The key difference from source mode is the OCI values (`autoshiftOciRegistry`, `autoshiftOciRepo`) which tell the ApplicationSet to pull policy charts from the registry instead of Git:

```console
export OCI_REGISTRY="quay.io/autoshift"
# export VERSION="X.Y.Z"   # Uncomment to pin to a specific version
cat << EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: autoshift
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: ${OCI_REGISTRY}
    chart: autoshift
    targetRevision: "${VERSION:-*}"
    helm:
      valueFiles:
        - values/global.yaml
        - values/clustersets/hub.yaml
        - values/clustersets/managed.yaml
      values: |
        autoshiftOciRegistry: true
        autoshiftOciRepo: oci://${OCI_REGISTRY}/policies
  destination:
    server: https://kubernetes.default.svc
    namespace: openshift-gitops
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
EOF
```

**Other composition examples:**

```yaml
# Minimal hub only:
valueFiles:
  - values/global.yaml
  - values/clustersets/hub-minimal.yaml

# Baremetal SNO + managed:
valueFiles:
  - values/global.yaml
  - values/clustersets/hub-baremetal-sno.yaml
  - values/clustersets/managed.yaml

# Hub of hubs:
valueFiles:
  - values/global.yaml
  - values/clustersets/hubofhubs.yaml
  - values/clustersets/hub1.yaml
  - values/clustersets/hub2.yaml
```

#### Step 5: Assign Clusters to ClusterSets

```console
# Replace 'hub' with the name of your clusterset
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub --overwrite
```

For managed clusters, assign them to their clusterset the same way:

```console
oc label managedcluster <cluster-name> cluster.open-cluster-management.io/clusterset=managed --overwrite
```

#### Step 6: Verify

```bash
# Check ArgoCD Application
oc get application autoshift -n openshift-gitops

# Check individual policy Applications
oc get applications -n openshift-gitops | grep autoshift

# Check ACM policies
oc get policies -A
```

For private registry credentials, custom CA certificates, and disconnected environments, see the [Release & OCI Guide](releases.md).

---

## Troubleshooting

### GitOps pods not starting
```bash
oc get pods -n openshift-gitops
oc get events -n openshift-gitops --sort-by=.lastTimestamp
```

### ACM not installing
```bash
oc get mch -A
oc get pods -n open-cluster-management
```

### Policies not applying
```bash
# Check cluster labels
oc get managedcluster local-cluster --show-labels

# Check placement
oc get placement -n open-cluster-policies

# Check policy status
oc describe policy <policy-name> -n open-cluster-policies
```

### ArgoCD Application not syncing
```bash
oc get application autoshift -n openshift-gitops -o yaml
oc describe application autoshift -n openshift-gitops
```

## Next Steps

- Review the [Autoshift Cluster Labels Values Reference](values-reference.md) for all available configuration labels
- See the [Developer Guide](developer-guide.md) for creating custom policies
- See the [Gradual Rollout Guide](gradual-rollout.md) for deploying multiple versions side-by-side
