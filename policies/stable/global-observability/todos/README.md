# todos/

Tracking directory for follow-up hardening work on the `global-observability`
policy chart (and, going forward, other policy charts audited the same way).

Each markdown file here catalogs a class of latent issue found by a code audit,
with exact `file:line` locations and the concrete change required, so the work
can be picked up later without re-running the whole audit.

## Files

| File | Audit | Scope |
|------|-------|-------|
| `undefined-variable-audit.md` | Variables used without a definition-check / loud failure message | `policies/global-observability/templates/*` |

## Conventions

- **Location format:** `<file>:<line>` relative to the chart root
  (`policies/global-observability/`).
- **Priority:**
  - 🔴 **silent-empty** — undefined value renders as empty string / nil and
    produces a structurally-broken-but-accepted resource. No error surfaces.
    Highest priority; needs an explicit guard + `fail`/`required` message.
  - 🟠 **panic** — undefined value triggers a Go-template nil-pointer panic.
    Fails loudly but with an opaque message; wrap with `required` for clarity.
  - 🟡 **runtime-error** — ACM template function (`fromSecret`, `copySecretData`)
    errors at hub-evaluation time when the source resource is missing. Fails,
    but the message does not name which field/secret was missing.
  - ✅ **guarded** — already has a `default`/`if`/`| default dict` guard. Listed
    for completeness so the audit is re-runnable; no action.
- When an item is fixed, check it off and note the commit.
