# AutoShift Documentation

Complete documentation for AutoShift - Infrastructure as Code for OpenShift using GitOps and ACM.

## Quick Links

### Getting Started
- **[Quick Start Guide](quickstart-oci.md)** - Get started with AutoShift in 15 minutes
- **[OCI Deployment Guide](deploy-oci.md)** - Deploy AutoShift from OCI registries (recommended)

### Release & Operations
- **[Release Guide](releases.md)** - How to create and publish AutoShift releases
- **[Gradual Rollout](gradual-rollout.md)** - Deploy multiple versions side-by-side using ACM ClusterSets

### Development
- **[Developer Guide](developer-guide.md)** - Contributing to AutoShift and advanced configuration

## Documentation Overview

### For Users

#### Installation & Deployment
1. Start with the [Quick Start Guide](quickstart-oci.md) for a rapid introduction
2. Follow the [OCI Deployment Guide](deploy-oci.md) for production deployments
3. Review [Gradual Rollout](gradual-rollout.md) if you need staged deployments

#### Key Concepts
- **Hub Cluster**: OpenShift cluster running GitOps and ACM
- **Managed Clusters**: Spoke clusters managed by ACM policies
- **OCI Mode**: Deploy all components from OCI registries (no Git dependency)
- **Git Mode**: Traditional deployment using Git repository

#### Minimum Requirements
All hub clusters must have:
- `gitops: 'true'` - OpenShift GitOps (ArgoCD) is required
- ACM is automatically installed on all hub clustersets by policy (no labels required)

See `autoshift/values/clustersets/hub-minimal.yaml` for a minimal configuration example.

### For Contributors

#### Release Process
1. Review [Release Guide](releases.md) for the full release workflow
2. Use `make release VERSION=x.y.z` to create releases
3. Charts are published to `quay.io/autoshift` by default

#### Development Workflow
1. Review [Developer Guide](developer-guide.md) for contribution guidelines
2. Use `values/clustersets/hub-minimal.yaml` as a starting point for new features
3. Test with Git mode before creating OCI releases

## Architecture

AutoShift uses a three-phase deployment model:

```
┌─────────────────────────────────────────────────────────┐
│  Phase 1: Bootstrap (Helm direct install)              │
│  ├─ OpenShift GitOps Operator                          │
│  └─ Advanced Cluster Management Operator               │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│  Phase 2: Deploy AutoShift (via ArgoCD Application)    │
│  └─ AutoShift Chart → ApplicationSet                   │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│  Phase 3: Policy Deployment (via ApplicationSet)       │
│  ├─ ACM Policy Charts from OCI Registry                │
│  ├─ policies/openshift-gitops (takes over GitOps)      │
│  └─ policies/advanced-cluster-management (takes over)  │
└─────────────────────────────────────────────────────────┘
```

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

## Values Files

AutoShift uses a composable values directory structure. Combine files for your deployment:

| Values File | Description | Use Case |
|-------------|-------------|----------|
| `values/global.yaml` | Shared config (git repo, branch) | Always included |
| `values/clustersets/hub.yaml` | Standard hub cluster labels | Production hub clusters |
| `values/clustersets/hub-minimal.yaml` | Minimal hub (GitOps + ACM) | Starting point, learning |
| `values/clustersets/managed.yaml` | Managed spoke cluster labels | Spoke clusters |
| `values/clustersets/sbx.yaml` | Sandbox cluster labels | Development, testing |
| `values/clustersets/hubofhubs.yaml` | Hub-of-hubs config | Large deployments |
| `values/clustersets/hub-baremetal-*.yaml` | Baremetal hub configs | On-premise deployments |

## Support & Contributing

- **Issues**: [GitHub Issues](https://github.com/auto-shift/autoshiftv2/issues)
- **Main Repository**: [auto-shift/autoshiftv2](https://github.com/auto-shift/autoshiftv2)
- **Contributing**: See [Developer Guide](developer-guide.md)

## Version Information

AutoShift follows semantic versioning:
- **Major**: Breaking changes
- **Minor**: New features, backward compatible
- **Patch**: Bug fixes

Use release candidates (e.g., `1.0.0-rc.1`) for testing before production releases.
