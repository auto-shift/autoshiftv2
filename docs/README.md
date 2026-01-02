# AutoShift Documentation

Complete documentation for AutoShift - Infrastructure as Code for OpenShift using GitOps and ACM.

## Quick Links

### Getting Started
- **[Quick Start from Source](quickstart-from-source.md)** - Deploy from Git for testing/development
- **[Quick Start (OCI)](quickstart-oci.md)** - Deploy from OCI registry in 15 minutes
- **[OCI Deployment Guide](deploy-oci.md)** - Full OCI deployment guide (recommended for production)

### Release & Operations
- **[Release Guide](releases.md)** - How to create and publish AutoShift releases
- **[Gradual Rollout](gradual-rollout.md)** - Deploy multiple versions side-by-side using ACM ClusterSets

### Development
- **[Developer Guide](developer-guide.md)** - Contributing to AutoShift and advanced configuration
- **[Adding New Operators](adding-new-operators.md)** - Step-by-step guide to add operators and contribute upstream

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

See `autoshift/values.minimal.yaml` for a minimal configuration example.

### For Contributors

#### Release Process
1. Review [Release Guide](releases.md) for the full release workflow
2. Use `make release VERSION=x.y.z` to create releases
3. Charts are published to `quay.io/autoshift` by default

#### Development Workflow
1. Review [Developer Guide](developer-guide.md) for contribution guidelines
2. Follow [Adding New Operators](adding-new-operators.md) to add operator policies
3. Use `values.minimal.yaml` as a starting point for new features
4. Test with Git mode before creating OCI releases

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
│  ├─ 28 ACM Policy Charts from OCI Registry             │
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
    └── ... (25 more policy charts)
```

## Values Files

Pre-configured values files for different scenarios:

| Values File | Description | Use Case |
|-------------|-------------|----------|
| `values.minimal.yaml` | Minimal config (GitOps + ACM only) | Starting point, learning |
| `values.hub.yaml` | Standard hub with common operators | Production hub clusters |
| `values.sbx.yaml` | Sandbox/spoke configuration | Development, testing |
| `values.hubofhubs.yaml` | Hub managing other hubs | Large deployments |
| `values.hub.baremetal-*.yaml` | Baremetal configurations | On-premise deployments |

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
