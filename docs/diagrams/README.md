# Architecture diagrams

The diagrams in this directory are drawn in the Red Hat Open Source Solution Portfolio Architecture
house style. The reader-facing page that presents them is [docs/architecture.md](../architecture.md),
and three of them, the schematic, the logical view, and the hub-of-hubs topology, are also embedded
in the project `README.md`.

## The file is its own source

Each `.drawio.svg` file is both the published picture and its own editable source. The diagram XML
lives in a `content` attribute on the `<svg>` root element. Edit a file at
[app.diagrams.net](https://app.diagrams.net) or in the draw.io extension for Visual Studio Code,
save it in place, and commit it. There is no export step and no second file to keep in sync.

> [!WARNING]
> Never run an SVG optimizer over this directory. Tools such as `svgo`, image-minifying continuous
> integration steps, and some editors strip attributes they do not recognize, which destroys the
> embedded source. The picture keeps working, so the loss stays invisible until somebody opens the
> file to edit it and finds no diagram inside.

One file holds one page. Exporting a multi-page diagram embeds every page into every file: a
single-page editable export costs about 11 percent over a display-only export, while a five-page
export more than doubles it. To add a new diagram:

```bash
brew install --cask drawio
drawio --export --format svg --embed-diagram --output new.drawio.svg new.drawio
```

Extend a copy of an existing file rather than starting from a blank canvas. Rebuilding the canvas
loses the Red Hat logo, the color key band, and the line key, all of which are laid out by hand.

Themes are handled automatically. draw.io writes every fill as a CSS `light-dark()` pair and sets
`color-scheme: light dark`, so the diagrams invert correctly for the documentation site's dark theme
and for GitHub dark mode. Do not produce separate light and dark exports.

## House style

| Element | Value |
|---|---|
| Canvas | 1800 by 1011, background `#F1F1F1`, color key band at y=911 |
| Application | `#689B7A` |
| Infrastructure | `#43ADAF` |
| Application Services | `#1D4174` |
| Services | `#4C4C4C` |
| Schematic platform box | `#E6E6E6`, no stroke |
| Logical grouping box | `#e5e5e5`, 2px stroke in the color of the group's category |
| Network lines | `#316dc1`, `#4cb6d6`, `#368a8c`, `#f7823c`, `#faaa4f`, `#5e40be` |
| Font | Red Hat Text |

- **Logical tiles** are a group 70 units high with the service name on the left and a 70 by 70 icon
  anchored right by relative geometry. The prevailing tile width is 240, with 220 and 280 used where
  a column is narrower or wider. Rows start at y=200 and repeat on an 84 unit pitch. Columns start at
  x=50, 600, 1210, and 1520, and their headings sit at y=106 and y=150.
- **Schematic cards** use the kit's 204 by 125 shape. The colored header names the service, the body
  line names the specific technology deployed, and an instance count is optional. The kit ships
  left, right, and bottom anchors only. The cards here carry an added top row so that vertical flows
  route cleanly.
- **A line key** appears on every page that uses more than one line color. Every stroke color used
  must appear in the key, and every key entry must correspond to a stroke actually used. Entries are
  labeled line segments with no arrowheads.
- **Icons** are baked with the background color of their category, so an icon has to match the color
  of the tile or header it sits on. A teal icon on a dark blue header renders as a visible teal
  square. Swap one by editing its `image=data:image/svg+xml,…` payload. Reuse an icon already in
  these files rather than sourcing a new one.
- **Edge paths are pinned with waypoints.** The orthogonal auto-routing in draw.io takes the
  shortest path, which cuts straight across cards. Edges leaving a zone are routed through explicit
  corridors: the gap between card rows, and the channel between a platform box and the next zone.
  Parallel flows to the same targets share one trunk rather than running as separate verticals.
  Moving a card means rechecking the waypoints of every edge that passed it.

## Naming and text

- **Use prose names, not directory names.** Write `Cluster labels`, not `cluster-labels`. The
  literal chart or resource name belongs in the body line of the card, which is what that line is
  for.
- **Use Red Hat product names, never the upstream project name.** Write `Red Hat OpenShift GitOps`
  rather than Argo CD, `Red Hat OpenShift Data Foundation` rather than Ceph, `Red Hat Advanced
  Cluster Security for Kubernetes` rather than StackRox, `Red Hat Ansible Automation Platform`
  rather than AWX, `Red Hat OpenShift Logging` rather than Loki, and `Compliance Operator` rather
  than OpenSCAP. Spell the products out rather than abbreviating them.
- **API kinds keep their real casing**, because they are what a reader types: `ApplicationSet`,
  `ManagedCluster`, `ConfigurationPolicy`, `PlacementDecision`.
- **Environments** are NonProd, Prod, and Sandbox throughout.
- **No free-floating prose.** The cyan `#00B0DA` is the kit's color for placement guides that are
  deleted once placement is done, and it must never carry real content. A fact that the shapes
  cannot express belongs in a band, which is the kit's full-width white bar, or in the body line of
  a card.
- **Text describes the system, not the picture.** Avoid `above` and `below`, avoid `side by side`,
  avoid second-person instructions, and never frame a fact as a change from a previous version.

## Facts the diagrams have to keep straight

Each of the following was drawn incorrectly in an earlier revision.

- **The hub is self-managed** and receives the same Day 2 policies as any spoke.
  `autoshift/values/clustersets/hub.yaml` enables a large feature set on the hub itself. The hub is
  not a bare control plane.
- **There are two delivery modes.** The source mode renders through the PolicyGenerator plugin
  sidecar in the Red Hat OpenShift GitOps repository server. The OCI mode ships charts that are
  already rendered and involves no plugin.
- **There are three tiers of Argo CD instance**, and they are drawn separately: the default instance
  belonging to the operator, which is normally disabled; the infrastructure instance that AutoShift
  runs itself; and the per-team instances in `openshift-gitops-<team>` created by the `gitops-dev`
  policy. The `gitops-dev-team-<team>` label takes the value `hub` or `standalone`, which decides
  where that team's instance lands.
- **Provisioning a cluster is a policy** like any other, so it flows through Red Hat Advanced
  Cluster Management. Provisioning arrows originate from Red Hat Advanced Cluster Management rather
  than from the cluster-install card, and they reach every managed cluster.
- **Customer applications are out of scope.** They run on the same clusters, and AutoShift does not
  manage them.
