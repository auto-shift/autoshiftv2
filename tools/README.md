# tools/

Policy validation for AutoShift. Uses the ACM hub template resolver
(`go-template-utils/v7`) to validate policy templates offline without a live cluster.

## What it checks

1. **Helm rendering** — every chart under `policies/` renders without error
2. **Hub template resolution** — `{{hub ... hub}}` expressions resolve cleanly against
   synthetic ManagedClusterLabels and rendered-config ConfigMaps built from the example files
3. **Config section coverage** — strips `| default "..."` before spoke resolution so any
   config key the template consumes but the example file doesn't declare surfaces as `<no value>`
4. **Spoke template resolution** — `{{ }}` expressions resolve against ConfigMaps and
   Secrets from `testdata/`
5. **YAML validation** — all fully-resolved documents are valid YAML with no `<no value>` placeholders
6. **Output assertions** — specific strings must appear in rendered output (catches silent
   config omissions that produce no error but render an incomplete policy)
7. **Label contract** — every `autoshift.io/<key>` consumed by a policy template is declared
   in an `_example*.yaml` file

## Usage

```bash
cd tools
go test ./... -v
```

Requires Go 1.21+ and Helm 3.x on `$PATH`.

The label contract report is written to `$LABEL_REPORT_OUTPUT` if set (used by CI
to produce the uploadable artifact).

## Extending

**New policy** — add a chart under `policies/<category>/<name>/`. No registration needed.

**New label** — add it under `labels:` in `autoshift/values/clustersets/_example.yaml`. CI will fail with `Missing` until you do.

**New config section** — add it under `config:` in the same example files.

**Hub lookups (Secrets/ConfigMaps on the hub)** — drop mock YAML in `tools/testdata/`.
The lookup is matched by `(kind, namespace, name)`.

**New API group** — register in the fake discovery client in `resolver.go`.

## Testdata

Files in `tools/testdata/` are loaded automatically — drop a `.yaml` file and it is
available to `lookup`/`fromSecret`/`fromConfigMap` calls in spoke templates.
Each document is matched by `(apiVersion, kind, namespace, name)`.

## Testing

```bash
cd tools
go test ./...                          # unit tests only — no helm required
go test -tags integration ./...        # all tests — requires helm on $PATH
go test -tags integration ./... -v     # verbose (shows per-policy pass/fail)
```
