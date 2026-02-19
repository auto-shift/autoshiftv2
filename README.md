# AutoShiftv2

![OpenShift Version](https://img.shields.io/badge/OpenShift-4.20.12-red?logo=redhatopenshift&logoColor=white)
![ACM Version](https://img.shields.io/badge/ACM-2.15-blue?logo=redhat&logoColor=white)

## What is AutoShift?

AutoShiftv2 is an opinionated [Infrastructure-as-Code (IaC)](https://martinfowler.com/bliki/InfrastructureAsCode.html) framework designed to manage infrastructure components after an OpenShift installation using Advanced Cluster Management (ACM) and OpenShift GitOps. It provides a modular, extensible model to support infrastructure elements deployed on OpenShift — particularly those in [OpenShift Platform Plus](https://www.redhat.com/en/resources/openshift-platform-plus-datasheet). AutoShiftv2 emphasizes ease of adoption, configurable features (taggable on/off), and production-ready capabilities for installation, upgrades, and maintenance.

What AutoShift does is it uses OpenShift GitOps to declaratively manage RHACM which then manages various OpenShift and/or Kubernetes cluster resources and components. This eliminates much of the operator toil associated with installing and managing day 2 tasks, by letting declarative GitOps do that for you.

## Documentation

📚 **[Complete Documentation](docs/)** - Start here for guides and tutorials

**Quick Links:**
- 🚀 [Quick Start Guide](docs/quickstart.md) - Full installation walkthrough (Source and OCI)
- 📦 [Release & OCI Guide](docs/releases.md) - Release process, OCI mode, and version management
- 📊 [Gradual Rollout](docs/gradual-rollout.md) - Multi-version deployments
- 📋 [Values Reference](docs/values-reference.md) - All cluster labels and configuration options
- 🔧 [Developer Guide](docs/developer-guide.md) - Contributing and advanced topics

## Architecture

AutoShiftv2 is built on Red Hat Advanced Cluster Management for Kubernetes (RHACM) and OpenShift GitOps working in concert. RHACM provides visibility into OpenShift and Kubernetes clusters from a single pane of glass, with built-in governance, cluster lifecycle management, application lifecycle management, and observability features. OpenShift GitOps provides declarative GitOps for multicluster continuous delivery.

The hub cluster is the main cluster with RHACM and its core components installed on it, and is also hosting the OpenShift GitOps instance that declaratively manages RHACM.

### Hub Architecture
![alt text](images/AutoShiftv2-Hub.jpg)

### Hub of Hubs Architecture

[Red Hat YouTube: RHACM MultiCluster Global Hub](https://www.youtube.com/watch?v=jg3Zr7hFzhM)

![alt text](images/AutoShiftv2-HubOfHubs.jpg)

## Values File Architecture

AutoShift uses a **composable values file** pattern. Configuration is split into focused files under `autoshift/values/` that you combine in your ArgoCD Application. See the [Values Reference](docs/values-reference.md) for the full file structure, composition details, precedence rules, and all available cluster labels.

## Quick Install

For full step-by-step instructions, see the [Quick Start Guide](docs/quickstart.md).

### From Source (Git)

```bash
# 1. Bootstrap GitOps and ACM
helm upgrade --install openshift-gitops openshift-gitops -f policies/openshift-gitops/values.yaml
helm upgrade --install advanced-cluster-management advanced-cluster-management -f policies/advanced-cluster-management/values.yaml

# 2. Deploy AutoShift via ArgoCD Application
# See Quick Start Guide for the full Application manifest

# 3. Assign clusters to clustersets
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset=hub --overwrite
```

### From OCI (Registry)

```bash
# 1. Download release artifacts
VERSION=0.1.0
curl -sL https://github.com/auto-shift/autoshiftv2/releases/download/v${VERSION}/install-bootstrap.sh -O
curl -sL https://github.com/auto-shift/autoshiftv2/releases/download/v${VERSION}/install-autoshift.sh -O
chmod +x install-*.sh

# 2. Bootstrap and install
./install-bootstrap.sh
oc get mch -A -w  # Wait for ACM to be ready
./install-autoshift.sh
```

For full details, see the [Quick Start Guide](docs/quickstart.md) and [Release & OCI Guide](docs/releases.md).

## Dry Run Mode

AutoShift supports **dry run mode** for safe testing of policy deployments. Policies report violations without enforcing changes:

```yaml
autoshift:
  dryRun: true  # Default: false
```

Add the `dryRun` override in your ArgoCD Application's `helm.values` field. See the [Quick Start Guide](docs/quickstart.md) for the full Application manifest.

## References

* [OpenShift Platform Plus DataShift](https://www.redhat.com/en/resources/openshift-platform-plus-datasheet)
* [Red Hat Training: DO480: Multicluster Management with Red Hat OpenShift Platform Plus](https://www.redhat.com/en/services/training/do480-multicluster-management-red-hat-openshift-platform-plus)
* [Martin Fowler Blog: Infrastructure As Code](https://martinfowler.com/bliki/InfrastructureAsCode.html)
* [helm Utility Installation Instructions](https://helm.sh/docs/intro/install/)
* [OpenShift CLI Client `oc` Installation Instructions](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#installing-openshift-cli)
