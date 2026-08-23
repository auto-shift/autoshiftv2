# AutoShiftv2

**Build and run a fleet of OpenShift clusters from Git.**

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
![OpenShift Version](https://img.shields.io/badge/OpenShift-4.22-red?logo=redhatopenshift&logoColor=white)
![Red Hat Advanced Cluster Management Version](https://img.shields.io/badge/Advanced_Cluster_Management-2.17-blue?logo=redhat&logoColor=white)

AutoShiftv2 is an opinionated [Infrastructure-as-Code (IaC)](https://martinfowler.com/bliki/InfrastructureAsCode.html)
framework for managing OpenShift at scale with
[Red Hat Advanced Cluster Management for Kubernetes](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes) and
[Red Hat OpenShift GitOps](https://docs.redhat.com/en/documentation/red_hat_openshift_gitops).
It provides a modular, extensible model for the infrastructure components deployed on OpenShift,
particularly those in
[OpenShift Platform Plus](https://www.redhat.com/en/resources/openshift-platform-plus-datasheet).
It emphasizes ease of adoption, configurable features (taggable on/off), and production-ready
capabilities for installation, upgrades, and maintenance.

What AutoShift does is use OpenShift GitOps to declaratively manage Red Hat Advanced Cluster Management,
which in turn manages various OpenShift and Kubernetes cluster resources and components. This
eliminates much of the operator toil associated with installing and managing Day 2 tasks, by
letting declarative GitOps do that for you.

AutoShift is not limited to Day 2, though. **New clusters are provisioned from the same values
files that configure them**: baremetal through the Assisted Installer and SiteConfig, AWS and
vSphere through Hive. A cluster arrives with its operators and configuration already applied.

Once running, it stays at the state its values files describe. Adding a capability to a hundred
clusters is one label; adding it to a single cluster is the same label on that cluster.

**[Get started →](docs/quickstart.md)**  ·  [Browse the labels](docs/values-reference.md)  ·  [All documentation](#documentation)

## How it works

Three phases:

1. **Bootstrap**: install Red Hat Advanced Cluster Management and OpenShift GitOps with Helm.
2. **Deploy AutoShift**: an ArgoCD Application pointing at the `autoshift` chart, which creates
   an ApplicationSet.
3. **Policies**: the ApplicationSet discovers every policy under `policies/` and deploys it. The
   GitOps and Red Hat Advanced Cluster Management policies then take over managing the operators that
   bootstrapped them.

One commit reaches the whole fleet. Git or an Open Container Initiative (OCI) registry feeds
GitOps, the ApplicationSet fans out one Application per policy, and Red Hat Advanced Cluster
Management distributes the result to every cluster whose labels match, including the hub itself.

[![How AutoShift deploys configuration to a fleet](docs/diagrams/autoshift-schematic.drawio.svg)](docs/diagrams/autoshift-schematic.drawio.svg)

Red Hat Advanced Cluster Management provides visibility into OpenShift and Kubernetes clusters from a
single pane of glass, with built-in governance, cluster lifecycle management, application lifecycle
management, and observability. OpenShift GitOps provides declarative GitOps for multicluster
continuous delivery. The **hub** is the cluster running both; everything else is a
**managed cluster**.

The hub is self-managed, so it receives the same Day 2 policies as any spoke. Roughly fifty
capabilities ship as policies, from storage and security through to developer services, and each
one is a label in a values file.

[![What AutoShift manages on the hub and on a managed cluster](docs/diagrams/autoshift-logical.drawio.svg)](docs/diagrams/autoshift-logical.drawio.svg)

Hubs stack. A global hub manages other hubs, each running its own AutoShift instance and managing
only the clusters its own Red Hat Advanced Cluster Management instance can see. See
[Hub-of-Hubs Topology](docs/hub-of-hubs.md)
([MultiCluster Global Hub](https://www.youtube.com/watch?v=jg3Zr7hFzhM)).

[![Hub-of-hubs topology](docs/diagrams/autoshift-hub-of-hubs.drawio.svg)](docs/diagrams/autoshift-hub-of-hubs.drawio.svg)

Two more diagrams cover the label flow and gradual rollout. See
[Architecture diagrams](docs/architecture.md).

## Key concepts

**Cluster sets** group clusters that share configuration. A values file describes the set and
every member inherits it; per-cluster files override individual settings. Configuration is
composable: focused files under `autoshift/values/` that you combine in your ArgoCD Application.

**Labels are the interface.** Values files are the source of truth. You author configuration
there, not by editing clusters. The `cluster-labels` policy resolves them and stamps
`autoshift.io/*` labels onto each `ManagedCluster`, with the merged config written to ConfigMaps
on the hub. Every other policy uses a Placement to match those labels, so enabling a capability is
one label. Precedence runs cluster-specific over cluster-set.

Because the resolved state lands on the cluster, you can read back exactly what any cluster was
told and where each setting came from: `oc get managedcluster <name> --show-labels`, or the
rendered-config ConfigMap in the policy namespace.

**Clusters come from the same source as their config.** A cluster definition in a values file is
picked up by the cluster-install policies and provisioned: baremetal through Advanced Cluster
Management's Assisted Installer and the SiteConfig operator, AWS and vSphere through Hive. Because the
definition sits beside the labels, a cluster is configured as it comes up rather than afterwards.
See [Provisioning Clusters](docs/cluster-install.md).

**Git or OCI.** Deploy from a Git repository with live policy discovery, or from version-pinned
OCI artifacts with no Git dependency.

## Installing

Two ways in, both covered step by step in the
**[Quick Start Guide](docs/quickstart.md)**:

- **From source (Git)**: bootstrap Red Hat Advanced Cluster Management and OpenShift GitOps with Helm
  from a clone, then deploy AutoShift as an ArgoCD Application. Policies are discovered live from
  the repository.
- **From OCI**: run the install scripts from a release. Version-pinned, prerendered, no Git
  dependency. See the [Release and OCI Guide](docs/releases.md) for private registries and
  disconnected environments.

## Configuring

Everything a cluster gets is driven by `autoshift.io/*` labels and `config` blocks in your values
files. The [Values Reference](docs/values-reference.md) lists every one, including dry-run mode,
custom ArgoCD namespaces, and per-operator channels and versions.

To run a new AutoShift release against a few clusters before the whole fleet, see
[Gradual Rollout](docs/gradual-rollout.md) and
[ClusterSet Assignment](docs/cluster-set-assignment.md).

## Documentation

📚 Every page here is published at
**[auto-shift.github.io/autoshiftv2](https://auto-shift.github.io/autoshiftv2/)** and lives under
`docs/` in this repository.

- 🚀 [Quick Start Guide](docs/quickstart.md) - Full installation walkthrough (Source and OCI)
- 🏗️ [Provisioning Clusters](docs/cluster-install.md) - Install new clusters from AutoShift
- 📋 [Values Reference](docs/values-reference.md) - All cluster labels and configuration options
- 🗂️ [External Values Repository](docs/external-values-repo.md) - Keep site config in your own repo
- 📦 [Release and OCI Guide](docs/releases.md) - Release process, OCI mode, and version management
- 📊 [Gradual Rollout](docs/gradual-rollout.md) - Multi-version deployments
- 🔧 [Developer Guide](docs/developer-guide.md) - Contributing and advanced topics

## Contributing

Issues and discussions are on [GitHub](https://github.com/auto-shift/autoshiftv2/issues). See the
[Developer Guide](docs/developer-guide.md) to scaffold a policy and run the validation suite.

## References

* [Red Hat Advanced Cluster Management for Kubernetes documentation](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes)
* [Red Hat OpenShift GitOps documentation](https://docs.redhat.com/en/documentation/red_hat_openshift_gitops)
* [Red Hat OpenShift Container Platform documentation](https://docs.redhat.com/en/documentation/openshift_container_platform)
* [OpenShift Platform Plus data sheet](https://www.redhat.com/en/resources/openshift-platform-plus-datasheet)
* [DO480: Multicluster Management with Red Hat OpenShift Platform Plus](https://www.redhat.com/en/services/training/do480-multicluster-management-red-hat-openshift-platform-plus)
* [Infrastructure as Code, by Martin Fowler](https://martinfowler.com/bliki/InfrastructureAsCode.html)
* [Installing Helm](https://helm.sh/docs/intro/install/)
* [Installing the OpenShift CLI](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/cli_tools/openshift-cli-oc#installing-openshift-cli)
