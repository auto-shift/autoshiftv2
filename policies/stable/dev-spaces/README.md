# dev-spaces

Installs the Red Hat OpenShift Dev Spaces operator and creates the `CheCluster` that runs the Dev
Spaces server, dashboard and gateway, so users can open workspaces.

| Policy | What it does |
|---|---|
| `policy-devspaces-operator-install` | namespace and `OperatorPolicy` |
| `policy-devspaces-instance` | the `openshift-devspaces` namespace and `CheCluster` `devspaces`, with `config.dev-spaces.checluster` merged over a small base spec |
| `policy-devspaces-ready-test` | inform only: the `CheCluster` reports `chePhase: Active` |

Enable it with `dev-spaces: 'true'` on the clusterset or cluster. Any `CheCluster` spec field can go
under `config.dev-spaces.checluster`; maps merge key by key and lists replace. The instance uses
`musthave` so the operator's own defaults are left alone, which means a key removed from config stays
on the cluster until it is removed by hand. OLM installs the DevWorkspace Operator as a dependency of
the Dev Spaces operator.
