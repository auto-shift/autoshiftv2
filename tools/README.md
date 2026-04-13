# tools/

CI validators for AutoShift. Uses the ACM hub template resolver
(`go-template-utils/v7`) to test policy templates offline.

## Usage

```bash
cd tools
go run ./cmd/autoshift-ci lint-labels \
  --policies ../policies \
  --values ../autoshift/values \
  --testdata testdata \
  --strict-orphans
```

Requires Go 1.25+ and Helm 3.x.

## How it works

1. Parses `_example*.yaml` to build synthetic `ManagedClusterLabels` and config
2. Runs `helm template` on every policy chart (activating `.example` files)
3. Resolves `{{hub ... hub}}` using the real ACM template engine with fake k8s clients
4. Injects rendered-config ConfigMaps and test resources from `testdata/`
5. Validates: label contract, YAML structure, template resolution

## Extending

**Labels** — add to `_example-hub.yaml` / `_example-managed.yaml` under `labels:`

**Config** — add to the same files under `config:`

**Hub lookups** — drop mock Secrets/ConfigMaps as YAML files in `tools/testdata/`

**API groups** — register in the fake discovery client in `resolver.go`

## Flags

| Flag | Default | Description |
|---|---|---|
| `--policies` | `policies` | Policies directory |
| `--values` | `autoshift/values` | Values directory |
| `--testdata` | `testdata` | Mock resources directory |
| `--allowlist` | `.github/label-lint-allowlist.yaml` | Allowlist file |
| `--strict-orphans` | `false` | Fail on declared-but-unconsumed labels |
| `--format` | `text` | Output: `text` or `markdown` |
| `--output` | `-` | Output file (`-` for stdout) |
| `--verbose` | `false` | Show per-key details |

## Testing

```bash
go test ./...
```
