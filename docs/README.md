# AutoShift Documentation

Complete documentation for AutoShift - Infrastructure as Code for OpenShift using GitOps and ACM.

## Quick Links

### Getting Started
- **[Quick Start Guide](quickstart.md)** - Full installation walkthrough (Source and OCI)
- **[Main README](../README.md)** - Architecture and values file composition

### Configuration
- **[Values Reference](values-reference.md)** - All cluster labels and configuration options

### Release & Operations
- **[Release & OCI Guide](releases.md)** - Release process, OCI mode, private registries, disconnected environments, version management
- **[Gradual Rollout](gradual-rollout.md)** - Deploy multiple versions side-by-side using ACM ClusterSets

### Development
- **[Developer Guide](developer-guide.md)** - Creating policies, contributing to AutoShift, and advanced configuration

## Architecture

AutoShift uses a three-phase deployment model:

```
┌─────────────────────────────────────────────────────────┐
│  Phase 1: Bootstrap (Helm direct install)               │
│  ├─ OpenShift GitOps Operator                           │
│  └─ Advanced Cluster Management Operator                │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│  Phase 2: Deploy AutoShift (via ArgoCD Application)     │
│  └─ AutoShift Chart → ApplicationSet                    │
└─────────────────────────────────────────────────────────┘
                         ↓
┌────────────────────────────────────────────────────────────┐
│  Phase 3: Policy Deployment (via ApplicationSet)           │
│  ├─ ACM Policy Charts (auto-discovered)                    │
│  ├─ policies/openshift-gitops (takes over GitOps)          │
│  └─ policies/advanced-cluster-management (takes over ACM)  │
└────────────────────────────────────────────────────────────┘
```

### Key Concepts
- **Hub Cluster**: OpenShift cluster running GitOps and ACM
- **Managed Clusters**: Spoke clusters managed by ACM policies
- **Labels**: Configured in values files only, propagated to clusters by the cluster-labels policy
- **OCI Mode**: Deploy all components from OCI registries (no Git dependency)
- **Git Mode**: Deploy from Git repository with auto-discovery via `policies/*` wildcard

### Minimum Requirements
All hub clusters must have:
- `gitops: 'true'` - OpenShift GitOps (ArgoCD) is required
- ACM is automatically installed on all hub clustersets by policy (no labels required)

## OCI Registry Structure

Charts are organized in a namespaced structure:

```
quay.io/autoshift/
├── bootstrap/
│   ├── openshift-gitops
│   └── advanced-cluster-management
├── autoshift
└── policies/
    ├── openshift-gitops
    ├── advanced-cluster-management
    ├── advanced-cluster-security
    └── ... (additional policy charts)
```

## Support & Contributing

- **Issues**: [GitHub Issues](https://github.com/auto-shift/autoshiftv2/issues)
- **Main Repository**: [auto-shift/autoshiftv2](https://github.com/auto-shift/autoshiftv2)
- **Contributing**: See [Developer Guide](developer-guide.md)
