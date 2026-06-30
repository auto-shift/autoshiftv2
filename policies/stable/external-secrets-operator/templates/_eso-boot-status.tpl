{{- /*
  eso.boot.statusReport — single-sourced replacement for `fail` in the ESO boot policies.

  WHY: calling `fail` in an ACM ConfigurationPolicy template aborts template PROCESSING, so the
  config-policy-controller reports a "template error" whose envelope embeds the ENTIRE hub-resolved
  policy (hundreds of lines) into status AND spec, twice, with the real reason truncated off the end —
  effectively unreadable in the ACM/OCP console. Instead we surface preconditions as DATA:
    * a per-policy status ConfigMap  <policy>-status  (full detail, persistent, `oc get cm -o yaml`)
  and let the calling policy decide compliance separately (action policies skip their action; readiness
  gates pair this with an inform `mustnothave` on the status CM to stay NonCompliant for dependents).
  We deliberately do NOT emit a custom Event: ACM already fires Events on every policy compliance
  transition, so a hand-rolled Event would just be redundant noise.

  Emits BOTH branches as RUNTIME conditionals on $errors, so the caller includes it unconditionally and
  the managed cluster decides: $errors non-empty -> write CM (mustonlyhave);
  empty -> remove it (mustnothave), self-clearing on recovery.

  Caller contract: $errors (a list) must be in scope at the include site (runtime template var). Pass:
    (dict "policy" "<configurationpolicy-name>" "ns" "<policy namespace>")
*/ -}}
{{- define "eso.boot.statusReport" -}}
{{- $policy := .policy -}}
{{- $ns := .ns -}}
{{ "{{-" }} if $errors {{ "}}" }}
##### PRECONDITION REPORT (replaces fail): full detail in a ConfigMap. ACM emits its own compliance #####
##### Events, so this carries the DETAIL only; the inform gate CP turns its presence into NonCompliant. #####
- complianceType: mustonlyhave
  objectDefinition:
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: {{ $policy }}-status
      namespace: {{ $ns }}
      labels:
        autoshift.io/eso-boot-status: "true"
        autoshift.io/eso-boot-policy: {{ $policy }}
    data:
      policy: {{ $policy }}
      errorCount: {{ "{{" }} $errors | len | quote {{ "}}" }}
      errors: |
        {{ "{{-" }} range $e := $errors {{ "}}" }}
        - {{ "{{" }} $e {{ "}}" }}
        {{ "{{-" }} end {{ "}}" }}
{{ "{{-" }} else {{ "}}" }}
##### No precondition errors: clear any stale status ConfigMap so the signal self-heals. #####
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: {{ $policy }}-status
      namespace: {{ $ns }}
{{ "{{-" }} end {{ "}}" }}
{{- end -}}
