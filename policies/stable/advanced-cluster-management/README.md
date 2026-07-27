# advanced-cluster-management (policy)

This policy installs and configures Red Hat Advanced Cluster Management. Like every other policy it is
rendered by **PolicyGenerator** (`kustomization.yaml` → `policy-generator-config.yaml`), generating the
policies for operator install, MultiClusterHub, observability, search-storage, addon-tuning, and
provisioning.

The operator install uses the **shared `components/operator-install` chart** (like quay/odf/etc.) —
`manifests/operator-install/kustomization.yaml`. Two things about the values it passes are worth noting:

## Own-namespace (SingleNamespace) install mode

ACM installs scoped to its own namespace, so the caller sets `targetNamespaces`:

```yaml
namespace: open-cluster-management
targetNamespaces:
  - open-cluster-management        # OperatorGroup scoped to its own namespace
```

The shared component emits `targetNamespaces` only when a caller supplies it; the default (omitted) is
an AllNamespaces / cluster-scoped OperatorGroup. ACM is one of the few operators that needs the scoped
form. (ACM was hand-written as a raw `OperatorPolicy` until the component gained `targetNamespaces`
support; it now uses the common chart.)

## Creates the policy namespace too

ACM's install also creates the AutoShift policy namespace as an extra namespace, via the component's
`namespaces` list:

```yaml
namespaces:
  - ${POLICY_NAMESPACE}
```

## Bootstrap note

ACM is a **bootstrap operator**: the root-level `advanced-cluster-management/` Helm chart is
`helm install`-ed during bootstrap phase 1 to stand up ACM before ArgoCD/PolicyGenerator exist. This
`policies/` version then manages ACM day-2 through PolicyGenerator once the CMP is available.

## Version pinning

Standard: `config.acm.versions` (and optional `config.acm.startingCSV`) pins the permitted CSV(s), else
the `autoshift.io/acm-version` label is used — the dual-mode block lives in the operator-install caller.
