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

{{- /*
  eso.boot.statusReportPerStore — structured variant of eso.boot.statusReport for the per-store
  policies (secret-stores). Same two-branch mustonlyhave/mustnothave CM lifecycle, but the errors
  are keyed BY STORE and then BY TEMPLATE LAYER instead of a flat list:

      errors: |
        vault-backend:
          hub:
            - caProvider.name is required to deliver caSource
        remote-cluster:
          hub:
            - unsupported authSecretConfig.fromRef "foo"

  so a diagnostician sees at a glance WHICH store failed and on WHICH side (hub vs spoke) it was
  produced — the store name and side are top-level structural keys, not buried in the message text
  (the flat helper's `[hub]`/`[spoke]` string prefix). This is the prod fault-isolation contract:
  a broken store is the only thing reported broken; the healthy stores are provisioned regardless.

  Caller contract: $storeErrors (a dict keyed storeName -> dict keyed side ("hub"/"spoke") -> list of
  message strings) must be in scope at the include site (runtime template var). Pass:
    (dict "policy" "<configurationpolicy-name>" "ns" "<policy namespace>")
*/ -}}
{{- define "eso.boot.statusReportPerStore" -}}
{{- $policy := .policy -}}
{{- $ns := .ns -}}
{{ "{{-" }} if $storeErrors {{ "}}" }}
##### PER-STORE PRECONDITION REPORT (replaces fail): errors.<store>.[hub|spoke] — store + layer are #####
##### structural keys so the console shows which store, and which side, produced each problem. #####
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
      storeCount: {{ "{{" }} $storeErrors | len | quote {{ "}}" }}
      errors: |
        {{ "{{-" }} range $storeName, $sides := $storeErrors {{ "}}" }}
        {{ "{{" }} $storeName {{ "}}" }}:
        {{ "{{-" }} range $side, $msgs := $sides {{ "}}" }}
          {{ "{{" }} $side {{ "}}" }}:
        {{ "{{-" }} range $m := $msgs {{ "}}" }}
            - {{ "{{" }} $m {{ "}}" }}
        {{ "{{-" }} end {{ "}}" }}
        {{ "{{-" }} end {{ "}}" }}
        {{ "{{-" }} end {{ "}}" }}
{{ "{{-" }} else {{ "}}" }}
##### No precondition errors on any store: clear any stale status ConfigMap so the signal self-heals. #####
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: {{ $policy }}-status
      namespace: {{ $ns }}
{{ "{{-" }} end {{ "}}" }}
{{- end -}}
