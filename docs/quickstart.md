# AutoShift quick start guide

This guide walks through a complete AutoShift installation from start to finish.

> [!TIP]
> **Just want to try AutoShift or see how a values change behaves?** There is a fast, local-only path that installs AutoShift with `helm` from your working copy — edit a values file and re-apply, no commit/push/ArgoCD sync required. It is for **development and demos, not production**. See [Alternative (development): Deploy directly with Helm](#alternative-development-deploy-directly-with-helm).

## Prerequisites

* A Red Hat OpenShift cluster at 4.20+ to act as the **hub** cluster
* [helm](https://helm.sh/docs/intro/install/) installed locally
* The OpenShift CLI [oc](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#installing-openshift-cli) installed locally
* Fork or clone of this repository (for Source installation)

### Minimum hub cluster requirements

All hub clusters **must** have the following configuration in their `hubClusterSets`:

* `gitops: 'true'` - OpenShift GitOps (ArgoCD) is required to deploy AutoShift
* Red Hat Advanced Cluster Management is automatically installed on all hub clustersets by policy (no labels required)

## Choose your installation method

| | **Source (Git)** | **OCI (Registry)** |
|---|---|---|
| **Best for** | Development, customization, getting started | Production, version-pinned deployments |
| **Bootstrap from** | Local git clone | OCI artifacts from Quay |
| **Git clone required** | Yes | No |
| **Customizable policies** | Edit directly in repo | Fork or overlay |
| **Air-gapped support** | Mirror git repo | Mirror OCI registry |

---

## Installation from source

### Step 1: login to the hub cluster

Login to the **hub** cluster through the [`oc` utility](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#cli-logging-in_cli-developer-commands).

```console
oc login --token=sha256~lQ...dI --server=https://api.cluster.example.com:6443
```

> [!NOTE]
> Alternatively you can use the devcontainer provided by this repository. By default the container will install the stable version of `oc` and the latest Red Hat provided version of `helm`. These versions can be specified by setting the `OCP_VERSION` and `HELM_VERSION` variables before building. From the container you can login as usual with `oc login` or copy your kubeconfig into the container `podman cp ${cluster_dir}/auth/kubeconfig ${container-name}:/workspaces/.kube/config`.

If installing in a disconnected or internet-disadvantaged environment, update the values in `policies/stable/openshift-gitops/values.yaml` and `advanced-cluster-management/values.yaml` (the Red Hat Advanced Cluster Management bootstrap chart's own values — the Red Hat Advanced Cluster Management policy is a PolicyGenerator dir and no longer carries a `values.yaml`) with the source mirror registry, otherwise leave these values as is.

If your clone of AutoShiftv2 requires credentials or you want to add credentials to any other git repos you can do this in the `openshift-gitops/values.yaml` file before installing. This can also be done in the OpenShift GitOps GUI after install.

### Step 2: install Red Hat Advanced Cluster Management for Kubernetes

> [!IMPORTANT]
> Install Red Hat Advanced Cluster Management **before** OpenShift GitOps. The GitOps bootstrap chart wires the PolicyGenerator plugin into
> Argo CD's repo-server by using an init-container image it reads from Red Hat Advanced Cluster Management's `multicluster-operators-hub-subscription`
> deployment at install time. If GitOps is installed first that image is empty and the Argo CD instance fails to
> reconcile (`Deployment "<argo>-repo-server" is invalid: spec.template.spec.initContainers[1].image: Required value`),
> so no policies ever render or sync. You do **not** need to wait for `MultiClusterHub` to reach `Running` — only for
> Red Hat Advanced Cluster Management's operator to be installed (the `multicluster-operators-hub-subscription` deployment to exist).

> [!IMPORTANT]
> Both bootstrap charts run a short-lived Job that waits for CRDs, using a CLI image that defaults to the
> in-cluster registry: `image-registry.openshift-image-registry.svc:5000/openshift/cli:latest`. On clusters
> where the internal image registry is **not** enabled (e.g. bare metal, or when the registry
> `managementState` is `Removed`), that image cannot be pulled and the bootstrap Job hangs in
> `ImagePullBackOff`. Override it with an external CLI image on **both** bootstrap installs (this step and the
> GitOps step):
> ```console
> helm upgrade --install advanced-cluster-management advanced-cluster-management \
>   --set image=registry.redhat.io/openshift4/ose-cli:latest
> helm upgrade --install openshift-gitops openshift-gitops -f policies/stable/openshift-gitops/values.yaml \
>   --set image=registry.redhat.io/openshift4/ose-cli:latest
> ```
> In a disconnected environment, use your mirrored equivalent of the `openshift4/ose-cli` image.

Using helm, install OpenShift Red Hat Advanced Cluster Management on the hub cluster:

```console
helm upgrade --install advanced-cluster-management advanced-cluster-management
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
> `MultiClusterHub` takes roughly 10 min to reach `Running`. You can proceed to the GitOps step as soon as the
> `multicluster-operators-hub-subscription` deployment exists (`oc get deploy multicluster-operators-hub-subscription -n open-cluster-management`),
> and you can install AutoShift while Red Hat Advanced Cluster Management finishes — but you will not be able to verify AutoShift or select a
> `clusterset` until `MultiClusterHub` is `Running`.

### Step 3: install OpenShift GitOps

> [!NOTE]
> Run this **after** the preceding Red Hat Advanced Cluster Management step — the GitOps repo-server needs Red Hat Advanced Cluster Management's subscription image (see the ordering note there).

Using helm, install OpenShift GitOps:

```console
helm upgrade --install openshift-gitops openshift-gitops -f policies/stable/openshift-gitops/values.yaml
```

> [!NOTE]
> If OpenShift GitOps is already installed manually on cluster and the default argo instance exists this step can be skipped. Make sure that argocd controller has cluster-admin

After the installation is complete, verify that all the pods in the `openshift-gitops` namespace are running. This can take a few minutes depending on your network to even return anything.

```console
oc get pods -n openshift-gitops
```

This command should return something like this:

```console
NAME                                                      READY   STATUS    RESTARTS   AGE
cluster-7978b47968-9zsts                                  1/1     Running   0          2m32s
gitops-plugin-5fd947c7f7-j2m2w                            1/1     Running   0          2m32s
infra-gitops-application-controller-0                     1/1     Running   0          2m31s
infra-gitops-applicationset-controller-569d557545-v6h9n   1/1     Running   0          2m31s
infra-gitops-dex-server-795959b7b6-2klqf                  1/1     Running   0          2m31s
infra-gitops-redis-648748fc4c-8s87h                       1/1     Running   0          2m31s
infra-gitops-repo-server-54b97c8bd5-fdx49                 2/2     Running   0          2m31s
infra-gitops-repo-server-54b97c8bd5-h2frr                 2/2     Running   0          2m31s
infra-gitops-repo-server-54b97c8bd5-kbnhp                 2/2     Running   0          2m31s
infra-gitops-server-7b67dbd55f-2rtkb                      1/1     Running   0          2m31s
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

### Step 4: install AutoShift

> [!TIP]
> The previously installed OpenShift GitOps and Red Hat Advanced Cluster Management will be controlled by AutoShift after it is installed for version upgrading

> [!TIP]
> **Just developing or demoing?** Skip the following ArgoCD Application and install AutoShift with `helm` straight from your working copy. Edit a values file and re-apply, no commit/push/ArgoCD sync required. See [Alternative (development): Deploy directly with Helm](#alternative-development-deploy-directly-with-helm) just below.

Update your values file with desired feature flags and repo URL as defined in the [Autoshift Cluster Labels Values Reference](values-reference.md).

Using helm and the values you set for cluster labels, install AutoShift. Here is an example that uses the hub values file:

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
    namespace: $GITOPS_NAMESPACE
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
      # Injected by Argo CD from this Application's own source, so the repository and revision
      # are declared once. Substitution works in parameters, not in a `values:` block.
      # The backslashes keep the shell from expanding these before oc sees them; in a plain
      # manifest file, write them as $ARGOCD_APP_SOURCE_REPO_URL with no backslash.
      parameters:
        - name: autoshiftGitRepo
          value: \$ARGOCD_APP_SOURCE_REPO_URL
        - name: autoshiftGitBranchTag
          value: \$ARGOCD_APP_SOURCE_TARGET_REVISION
  sources: []
  project: $ARGO_PROJECT
  syncPolicy:
    automated:
      prune: false
      selfHeal: true
EOF
```

#### Alternative (development): deploy directly with Helm

> [!WARNING]
> This path is for **development and demos only, not production**. It installs the AutoShift ApplicationSet with `helm` from your local working copy instead of creating the `autoshift` ArgoCD Application. Production deployments should use the preceding ArgoCD Application so that AutoShift is self-managed and reconciled by GitOps.

Instead of pointing an ArgoCD Application at values files committed in Git, you can render the `autoshift` chart directly against your **local** values files. This you can edit a values file and re-apply immediately — no commit, push, or ArgoCD sync round-trip — which is the fastest way to see how a values change behaves.

Run this from the **root of your clone**:

```console
helm upgrade --install autoshift ./autoshift \
  -n openshift-gitops \
  -f autoshift/values/global.yaml \
  -f autoshift/values/clustersets/hub.yaml \
  -f autoshift/values/clustersets/managed.yaml
```

Add any per-cluster override files the same way (for example `-f autoshift/values/clusters/my-cluster.yaml`). To iterate, edit a values file and re-run the same command — Helm re-renders the ApplicationSet with your new values.

> [!IMPORTANT]
> Only **values** are read locally. The ApplicationSet still pulls the policy **charts** from Git (`autoshiftGitRepo` / `autoshiftGitBranchTag`, default `auto-shift/autoshiftv2` @ `main`). So local edits to *values files* take effect on the next `helm upgrade`, but edits to *policy templates* only apply after you push them and point the chart at that branch:
> ```console
> helm upgrade --install autoshift ./autoshift -n openshift-gitops \
>   --set autoshiftGitRepo=https://github.com/<you>/autoshiftv2.git \
>   --set `autoshiftGitBranchTag`=<your-branch> \
>   -f autoshift/values/global.yaml \
>   -f autoshift/values/clustersets/hub.yaml \
>   -f autoshift/values/clustersets/managed.yaml
> ```

Because Helm owns the ApplicationSet in this mode, there is **no** `autoshift` ArgoCD Application — so `oc get application.argoproj.io autoshift` (used in Step 6) will not exist. Verify with the ApplicationSet and the generated per-policy Applications instead:

```console
oc get applicationset autoshift-policies -n openshift-gitops
oc get applications.argoproj.io -n openshift-gitops
```

To remove it: `helm uninstall autoshift -n openshift-gitops`.

### Step 5: assign clusters to ClusterSets

Given the labels and cluster sets specified in the supplied values file, Red Hat Advanced Cluster Management cluster sets will be created. Add the hub cluster (`local-cluster`) to the appropriate clusterset:

```console
# Replace 'hub' with the name of your clusterset
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub --overwrite
```

For managed clusters, assign them to their clusterset the same way:

```console
oc label managedcluster <cluster-name> cluster.open-cluster-management.io/clusterset=managed --overwrite
```

Alternatively, you can assign clusters through the Red Hat Advanced Cluster Management Console at **All Clusters > Infrastructure > Clusters > Cluster Sets**. When provisioning a new cluster from Red Hat Advanced Cluster Management, you can also select the desired clusterset at time of creation.

### Step 6: verify

```bash
# Check ArgoCD Application
oc get application.argoproj.io autoshift -n openshift-gitops

# Check individual policy Applications
oc get applications.argoproj.io -n openshift-gitops | grep autoshift

# Check ACM policies
oc get policies -A

# View policy compliance
oc get policies -n open-cluster-policies
```

that is it. Welcome to OpenShift Platform Plus and all of its many capabilities!

---

## Installation from OCI release

For production or version-pinned deployments, AutoShift can be installed directly from OCI artifacts hosted on Quay — no git clone required.

### Option a: using the install scripts

Download the scripts from the [latest release](https://github.com/auto-shift/autoshiftv2/releases) and run them:

```bash
curl -sL https://github.com/auto-shift/autoshiftv2/releases/latest/download/install-bootstrap.sh -O
curl -sL https://github.com/auto-shift/autoshiftv2/releases/latest/download/install-autoshift.sh -O
chmod +x install-*.sh

# Bootstrap Red Hat Advanced Cluster Management, then GitOps
./install-bootstrap.sh

# Wait for ACM to be ready
oc get mch -A -w

# Install AutoShift (accepts: hub, minimal, sbx, hubofhubs)
./install-autoshift.sh hub
```

### Option b: manual OCI installation

If you prefer to run the commands directly without the scripts:

#### Step 1: login to the hub cluster

```console
oc login --token=sha256~lQ...dI --server=https://api.cluster.example.com:6443
```

#### Step 2: bootstrap Red Hat Advanced Cluster Management from OCI

```bash
export OCI_REPO="oci://quay.io/autoshift"
# export VERSION="X.Y.Z"   # Uncomment to pin the bootstrap charts to a version

helm upgrade --install advanced-cluster-management ${OCI_REPO}/bootstrap/advanced-cluster-management \
    ${VERSION:+--version ${VERSION}} \
    --create-namespace \
    --wait \
    --timeout 15m
```

Wait for Red Hat Advanced Cluster Management to be ready:

```console
oc get mch -A -w
```

> [!NOTE]
> This does take roughly 10 min to install. You can proceed to installing AutoShift while this is installing but you will not be able to verify AutoShift or select a `clusterset` until this is finished.

#### Step 3: bootstrap GitOps from OCI

> [!NOTE]
> Red Hat Advanced Cluster Management is installed first in both installation methods, so there
> is one order to remember. OCI mode does not strictly require it: OCI deploys prerendered Helm
> charts rather than the PolicyGenerator `ConfigManagementPlugin`, so the GitOps repo-server has
> no dependency on the Red Hat Advanced Cluster Management image. That dependency, and therefore
> the hard ordering requirement, applies to Source installations.

```bash
helm upgrade --install openshift-gitops ${OCI_REPO}/bootstrap/openshift-gitops \
    ${VERSION:+--version ${VERSION}} \
    --create-namespace \
    --wait \
    --timeout 10m
```

> [!IMPORTANT]
> As with the source install, the bootstrap CRD-wait Job defaults to the in-cluster CLI image
> (`image-registry.openshift-image-registry.svc:5000/openshift/cli:latest`). On clusters without the
> internal image registry (e.g. bare metal), add `--set image=registry.redhat.io/openshift4/ose-cli:latest`
> (or your mirrored equivalent) to **both** bootstrap installs (Step 2 and Step 3).

Verify GitOps is running:

```console
oc get pods -n openshift-gitops
oc get argocd -A
```

#### Step 4: deploy AutoShift from OCI

Create the ArgoCD Application pointing to the OCI registry. The key difference from source mode is `autoshiftOciRepo`, which both selects OCI mode and says where the policy charts are published. Change it if you release your own charts. `autoshiftOciVersion` is required, because a mutable `latest` tag is unreliable with Argo CD OCI, and is injected from the Application's own `targetRevision`:

```console
export OCI_REGISTRY="quay.io/autoshift"
export VERSION="X.Y.Z"   # Required in OCI mode: pin the release version
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
    targetRevision: "${VERSION}"
    helm:
      valueFiles:
        - values/global.yaml
        - values/clustersets/hub.yaml
        - values/clustersets/managed.yaml
      values: |
        # Where the policy charts are published. Change this if you release your own charts.
        autoshiftOciRepo: oci://${OCI_REGISTRY}/policies
      # Injected from this Application's own targetRevision, so the release is pinned once.
      # The backslash keeps the shell from expanding it; in a plain manifest file, drop it.
      parameters:
        - name: autoshiftOciVersion
          value: \$ARGOCD_APP_SOURCE_TARGET_REVISION
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

#### Step 5: assign clusters to ClusterSets

```console
# Replace 'hub' with the name of your clusterset
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub --overwrite
```

For managed clusters, assign them to their clusterset the same way:

```console
oc label managedcluster <cluster-name> cluster.open-cluster-management.io/clusterset=managed --overwrite
```

#### Step 6: verify

```bash
# Check ArgoCD Application
oc get application.argoproj.io autoshift -n openshift-gitops

# Check individual policy Applications
oc get applications.argoproj.io -n openshift-gitops | grep autoshift

# Check ACM policies
oc get policies -A
```

For private registry credentials, custom CA certificates, and disconnected environments, see the [Release and OCI Guide](releases.md).

---

## Troubleshooting

### GitOps pods not starting
```bash
oc get pods -n openshift-gitops
oc get events -n openshift-gitops --sort-by=.lastTimestamp
```

### Red Hat Advanced Cluster Management not installing
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

### ArgoCD application not syncing
```bash
oc get application.argoproj.io autoshift -n openshift-gitops -o yaml
oc describe application.argoproj.io autoshift -n openshift-gitops
```

## Next steps

- Review the [Autoshift Cluster Labels Values Reference](values-reference.md) for all available configuration labels
- See the [Developer Guide](developer-guide.md) for creating custom policies
- See the [Gradual Rollout Guide](gradual-rollout.md) for deploying multiple versions side-by-side
