# tools/

Go-based CI validators for AutoShift. Uses the real ACM hub template resolver
(`github.com/stolostron/go-template-utils/v7`) to test policy templates offline.

## Build & run

```bash
cd tools

# Run a subcommand directly:
go run ./cmd/autoshift-ci lint-labels

# Or build a standalone binary:
go build -o autoshift-ci ./cmd/autoshift-ci
./autoshift-ci lint-labels
```

Requires Go 1.25+ and Helm 3.x on PATH.

## Subcommands

### `lint-labels` — label contract linter with ACM template resolution

Tests the complete template pipeline offline:

1. Parses `_example*.yaml` files to build a synthetic `ManagedClusterLabels` map
2. Runs `helm template` on every policy chart (activating `.example` config files)
3. Resolves `{{hub ... hub}}` expressions using the **real ACM template engine**
   with fake Kubernetes clients and the synthetic label values
4. Checks that every label in the examples is consumed by at least one chart,
   and every label referenced by a chart exists in the examples

Charts that use `{{hub lookup ... hub}}` (e.g. for ConfigMap reads) produce
resolution warnings — those calls return empty from the fake client. This is
expected until Phase 2 adds local ConfigMap injection.

### Usage

```bash
go run ./cmd/autoshift-ci lint-labels \
  --policies ../policies \
  --values ../autoshift/values \
  --allowlist ../.github/label-lint-allowlist.yaml \
  --strict-orphans \
  --verbose
```

### Flags

- `--policies` — path to the policies directory (default: `policies`)
- `--values` — path to autoshift/values (default: `autoshift/values`)
- `--allowlist` — YAML allowlist file for known exceptions
- `--strict-orphans` — fail if any labels are declared but not consumed
- `--verbose` — show per-key details
- `--show-policies` — show which policies consume each key
- `--format text|markdown` — output format
- `--output <path>` — write report to file (`-` for stdout)

### Exit codes

- `0` — all labels pass
- `1` — missing or orphaned labels found (or resolution failures)

## How it works

The linter discovers consumed labels by checking if `autoshift.io/<key>` appears
anywhere in each chart's rendered `helm template` output. This is simple and
correct because Helm has already resolved all Helm-level indirection (printf,
variables, conditionals). No regex parsing of template syntax needed.

For numbered-suffix keys (e.g. `worker-nodes-zone-1`), the linter also checks
if the base prefix (`worker-nodes-zone`) appears, matching how ACM's `hasPrefix`
iteration works at runtime.

Charts with `.example` files in their `files/` directory (e.g. metallb's
`files/bgp/internal.yml.example`) are rendered with those files activated in a
temporary copy, so `Files.Glob` guards in templates pass and all template paths
are exercised.

## Layout

```
tools/
  go.mod
  cmd/autoshift-ci/         CLI entry point
  internal/
    labels/                  label catalog and contract logic
      types.go               Consumed, Reference types
      declared.go            extract labels from _example*.yaml (YAML + comments)
      contract.go            diff: OK / missing / orphaned
      allowlist.go           YAML allowlist loader
      report.go              text + markdown output
    resolver/                ACM hub template resolution
      resolver.go            NewResolver (fake k8s clients), ResolvePolicy
      context.go             HubContext, BuildSyntheticLabels
      extract.go             KeysToConsumed (bridge to contract checker)
      pipeline.go            RunPipeline: discover → helm → resolve → validate
      testvalues.go          ApplicationSet-injected values for rendering
```

## Testing

```bash
go test ./...
```
