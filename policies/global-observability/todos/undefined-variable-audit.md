# Undefined-variable audit — global-observability

**Audited:** 2026-06-15
**Scope:** `policies/global-observability/templates/*`
**Question:** does every variable used fail loudly if it isn't defined before use?

## Verdict

**No.** Most chart-value (`.Values.*`) scalars and **all** user-supplied
`additionalRemoteWrites` fields render as empty strings when undefined and
produce structurally-broken-but-accepted resources with **no error**. A few
inputs panic (loud but opaque), and the runtime ACM functions error with generic
messages that don't name the missing field. The control-flow helpers
(`$obsConfig`, `$capabilities`, `$scrapeInterval`, label reads, etc.) are already
guarded with `| default`.

Priority legend in [`README.md`](./README.md): 🔴 silent-empty · 🟠 panic ·
🟡 runtime-error · ✅ guarded (no action).

---

> **STATUS: all P1 items implemented 2026-06-15.** Verified with `helm template`
> (renders + parses as valid YAML), `helm lint` (clean), and a negative test
> (empty `globalHubRollup.secretName` → render fails with the expected message).
> Hub-template `fail` guards fire at ACM hub-evaluation time, not `helm template`
> time, so they were verified by inspecting the rendered `{{hub … hub}}` output.

## ✔ 🔴 P1 — silent-empty: user config (`additionalRemoteWrites`) required fields

These come from per-cluster rendered-config and are **completely uncontrolled**.
`index $x "key"` with no `| default` returns `nil` → renders as `''`. The
resulting `Secret`/`remoteWrite` is accepted by the API server but is broken
(empty name, empty URL, copySecretData called with `""`).

Add a `fail` guard per required field inside the `range $rw` loops.

`policy-global-observability-prometheus.yaml`

| Line | Expression | Broken result if undefined | Fix |
|------|-----------|----------------------------|-----|
| 135 | `index $secretRef "name"` (additional-secrets ConfigPolicy) | `Secret` with empty `metadata.name` | `fail "additionalRemoteWrites[].secretRef.name is required"` when empty |
| 138 | `copySecretData (index $secretRef "namespace") (index $secretRef "name")` | `copySecretData "" ""` → copies nothing / errors obscurely | guard both `secretRef.namespace` and `secretRef.name` non-empty before the call |
| 187 | `index $secretRef "name"` (PrometheusAgent `spec.secrets`) | empty entry in `spec.secrets` → MCOA mount name `secret-` (see truncation hazard) | same guard as 135 |
| 208 | `index $rw "name"` | `remoteWrite` entry with empty `name` | `fail "additionalRemoteWrites[].name is required"` |
| 212 | `index $rw "caFile"` | `tlsConfig.caFile: ''` → agent can't load CA, write fails at runtime | require non-empty |
| 214 | `index $rw "certFile"` | `tlsConfig.certFile: ''` | require non-empty |
| 215 | `index $rw "keyFile"` | `tlsConfig.keyFile: ''` | require non-empty |
| 216 | `index $rw "url"` | `remoteWrite.url: ''` → invalid endpoint | `fail "additionalRemoteWrites[].url is required"` |

> Note: `secretRef` itself (`index $rw "secretRef" | default dict`) and
> `onSelfManagedHub`/`remoteTimeout` are already guarded — only the inner
> required subfields above are unguarded.

---

> Implemented: `range $rw` validation guards (`{{hub- if not (index $rw "<f>") hub}}{{hub fail … hub}}{{hub- end hub}}`) added before the `onSelfManagedHub` gate so every configured entry is validated on every hub. `secretRef.name`/`secretRef.namespace` validated inside `if not (empty $secretRef)` (lines 135/138/187 covered; 187 also covered transitively). **Note:** Helm deep-merge means an *omitted* field inherits the chart/rendered-config layer — these guards catch fields explicitly set empty/null, and at runtime catch entries supplied without the field.

## ✔ 🔴 P1 — silent-empty: built-in rollup chart values (`$rollup`)

`$rollup := .Values.globalObservability.spokeAgent.globalHubRollup` (line 25).
The chart ships defaults in `values.yaml`, so these only go empty if an operator
overrides `spokeAgent.globalHubRollup` with a partial map. Each leaf is accessed
with no default; an absent leaf renders `''`.

`policy-global-observability-prometheus.yaml`

| Line | Expression | Broken result if undefined |
|------|-----------|----------------------------|
| 94 | `{{ $rollup.secretName }}` (rollup-secret-repl Secret name) | empty Secret name |
| 97 | `copySecretData "{{ $rollup.secretNamespace }}" "{{ $rollup.secretName }}"` | copies from `""` namespace — **also see the hardcoded-namespace coupling in the design doc appendix** |
| 179 | `{{ $rollup.secretName }}` (`spec.secrets`) | empty mount entry |
| 194 | `{{ $rollup.name }}` (`remoteWrite.name`) | empty name |
| 195 | `{{ $rollup.remoteTimeout }}` | empty timeout |
| 198 | `{{ $rollup.caFile }}` | empty caFile |
| 200 | `{{ $rollup.certFile }}` | empty certFile |
| 201 | `{{ $rollup.keyFile }}` | empty keyFile |

**Fix:** validate `$rollup` once at the top (e.g. `required` each leaf, or a
single `fail` if any of `secretName/secretNamespace/name/remoteTimeout/caFile/
certFile/keyFile` is empty) so a partial override is rejected at `helm template`
time rather than shipping a broken PrometheusAgent patch.

---

## ✔ 🟡 P2 — runtime-error: ACM functions on possibly-missing sources

> **DONE 2026-06-15.** Existence pre-checks added that `fail` with a message
> naming the missing secret/key:
> - secrets.yaml: `$signerCert` / `$mcCerts` `lookup` guards at top of the block.
> - prometheus.yaml: `$rollupSrc` (hub `lookup`) before the built-in rollup
>   `copySecretData`; `$rwSrc` (hub `lookup`) before the additional-secret
>   `copySecretData`; inline `$u := fromSecret …` guard on the `observatorium.url`
>   line. Note: these functions already hard-fail on a missing source — the
>   guards only improve the message, they do not change blast radius.


These fail at hub template evaluation (policy goes NonCompliant / template error)
but the message does **not** identify the missing secret/key. Wrap so the failure
names the resource.

`policy-global-observability-secrets.yaml`

| Line | Expression | Missing source → |
|------|-----------|------------------|
| 49 | `fromSecret "open-cluster-management-observability" "observability-controller-…-signer-client-cert" "tls.crt"` | generic template error if MCO signer cert not yet created |
| 50 | `fromSecret … "tls.key"` | same |
| 51 | `fromSecret "open-cluster-management-observability" "observability-managed-cluster-certs" "ca.crt"` | same |

`policy-global-observability-prometheus.yaml`

| Line | Expression | Missing source → |
|------|-----------|------------------|
| 97 | `copySecretData $rollup.secretNamespace $rollup.secretName` | errors if the coalesced secret isn't present on the (managing) hub yet |
| 138 | `copySecretData (secretRef.namespace) (secretRef.name)` | errors if the referenced secret is absent on the hub |
| 202 | `fromSecret <ns> $rollup.secretName "observatorium.url" \| base64dec` | errors / `base64dec ""` if key absent |

**Fix:** these are partly mitigated by policy ordering (`*-secrets` →
`*-prometheus`, and the `*-exists` gate), but add a clear precondition/failure
message (e.g. assert the secret exists, or document the dependency that
guarantees it) so a misconfiguration is diagnosable.

---

> Implemented: 8 `required` guards added after `$rollup` is bound (prometheus.yaml:25). Fail at `helm template` time with a named message. **Caveat learned during impl:** Helm deep-merges chart `values.yaml` defaults under any partial override, so a leaf can only go missing if explicitly set to `""`/`null` or if the `values.yaml` default itself is removed — the `required` guards now cover both.

## ✔ 🔴 P1 — silent-empty: `lookup` returning empty host

`policy-global-observability-secrets.yaml`

| Line | Expression | Broken result if undefined |
|------|-----------|----------------------------|
| 53 | `'https://{{ ((lookup "route.openshift.io/v1" "Route" "open-cluster-management-observability" "observatorium-api" \| default dict).spec).host }}/api/metrics/v1/default/api/v1/receive'` | if the Route is missing, `host` is empty → URL becomes `https:///api/metrics/...` and is stored in the coalesced secret with **no error**. Every downstream agent then writes to an invalid URL. |

**Fix:** `| default dict` prevents the panic but masks the real problem — add an
explicit check: if the resolved host is empty, `fail` (or skip emitting the
secret) so the broken URL is never persisted.

> Implemented: `$obsHost` is computed once at the top of the `object-templates-raw`
> block; `{{ if not $obsHost }}{{ fail … }}{{ end }}` aborts before the broken URL
> is persisted, and the URL line now references `$obsHost`. Runtime (managed-cluster)
> template, so it fires during ACM evaluation on the global hub.

---

## ✔ 🟠 P3 — panic / empty: required chart-value scalars

> **DONE 2026-06-15.** `policy_namespace` wrapped in `required` in all four
> policy templates and asserted at the top of `policysets.yaml`.
> `globalObservability.namespace` asserted via `required` in prometheus.yaml
> (`$obsNamespace`) and prometheus-exists.yaml; `globalHubRollup.secretName`
> asserted in secrets.yaml. mcoa.yaml capabilities parent made nil-safe with
> `$caps := dig "capabilities" dict (.Values.globalObservability | default dict)`
> and the six inline defaults switched to `index $caps "<x>" | default "true"`.
> Verified: `--set globalObservability.capabilities=null` now renders without a
> panic; empty `policy_namespace` / `globalObservability.namespace` fail with
> named messages.


`.Values.policy_namespace` and `.Values.globalObservability.namespace` are
supplied by the ApplicationSet `valuesObject` / chart `values.yaml`, so they're
present in normal operation. Out of that context (`helm template` of the bare
chart, or a trimmed `valuesObject`) they either panic (nil parent) or render an
empty namespace silently. Harden with `required "<msg>" <value>`.

| File | Line(s) | Expression |
|------|---------|-----------|
| `policy-global-observability-prometheus.yaml` | 24 | `$policyNamespace := .Values.policy_namespace` |
| `policy-global-observability-prometheus.yaml` | 95, 136, 172 | `$.Values.globalObservability.namespace` |
| `policy-global-observability-secrets.yaml` | 3, 46 | `.Values.policy_namespace` |
| `policy-global-observability-secrets.yaml` | 45 | `$.Values.globalObservability.spokeAgent.globalHubRollup.secretName` (deep chain — panics if any parent nil) |
| `policy-global-observability-mcoa.yaml` | 15 | `.Values.policy_namespace` |
| `policy-global-observability-prometheus-exists.yaml` | 15, 58 | `.Values.policy_namespace`, `$.Values.globalObservability.namespace` |
| `policysets.yaml` | multiple | `.Values.policy_namespace` (empty → PolicySet/Placement/Binding land in default ns) |

`policy-global-observability-mcoa.yaml:53-58` — `.Values.globalObservability.capabilities.<x> | default "true"`: the `| default` saves the value but **not** the parent access; if `globalObservability.capabilities` is nil the field read panics before `default` applies. Guard the parent.

---

## ✅ Already guarded (verified — no action)

Listed so the audit is re-runnable.

- `($.Values.autoshift).dryRun`, `((($.Values.autoshift).evaluationInterval).compliant|noncompliant) | default …` — parenthesized nil-safe + default. (all four templates)
- `if .Values.hubClusterSets` top-level guard — every template; nothing renders without it.
- `fromConfigMap … "config" | default "{}" | fromYaml`, then `index … "globalObservability" | default dict` → `$obsConfig`. (prometheus.yaml:122,159; mcoa.yaml:51)
- `$capabilities := index $obsConfig "capabilities" | default dict`. (mcoa.yaml:52)
- `$scrapeInterval`, `$logLevel`, `$additionalRW`, `$secretRef`, `$onSelfManaged`, `$isSelfManaged` — all `| default …`. (prometheus.yaml:123-127,160-163,182-184)
- `index .ManagedClusterLabels "autoshift.io/self-managed" | default "false"`. (prometheus.yaml:85,124,162)
- `.Values.globalObservability.spokeAgent.prometheusAgentNames | default (list …)`. (prometheus.yaml:164; prometheus-exists.yaml:17)
- `index $rw "remoteTimeout" | default "30s"`, `index $rw "onSelfManagedHub" | default false`. (prometheus.yaml:209,127,184,206)

---

## Implementation order — ✅ all complete (2026-06-15)

1. ✅ **P1 user config (additionalRemoteWrites)** — per-field `fail` guards in the `range $rw` loops.
2. ✅ **P1 `$rollup` leaves + lookup host** — `required` + `$obsHost` `fail` guard.
3. ✅ **P2 runtime functions** — existence pre-checks naming the missing secret/key.
4. ✅ **P3 chart scalars** — `required` wrappers + `$caps` nil-safe parent.

All verified with `helm template` (renders, 13 docs parse as valid YAML),
`helm lint` (clean), and targeted negative tests. Hub-template `fail` guards
fire at ACM hub-evaluation time and were verified by inspecting rendered output.
