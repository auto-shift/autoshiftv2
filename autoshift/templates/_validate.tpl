{{/*
AutoShift Label Validator
Validates all label sources against conventions and schema.

Convention: Any label "{name}-subscription-name" marks {name} as an operator.
  Standard operator labels are auto-validated: enable, subscription-name,
  channel, source, source-namespace, version.

Schema: Everything beyond standard operator labels must be declared in
  _schema.tpl or validation fails with "unknown label".

Collects ALL errors and reports them together in a single fail.
*/}}
{{- define "autoshift.validate-labels" -}}
{{- $errors := list }}
{{- $schemas := (include "autoshift.schemas" . | fromYaml) }}

{{/* Guard against schema parse errors */}}
{{- if hasKey $schemas "Error" }}
  {{- fail (printf "Failed to parse label schemas: %s" (index $schemas "Error")) }}
{{- end }}

{{/* Standard operator label suffixes — auto-detected, no schema entry needed */}}
{{- $operatorSuffixes := list "subscription-name" "channel" "source" "source-namespace" "version" }}

{{/* Build combined map of all label sources for validation */}}
{{- $sources := dict }}
{{- range $name, $set := (.Values.hubClusterSets | default dict) }}
  {{- $_ := set $sources (printf "hubClusterSet '%s'" $name) (index $set "labels" | default dict) }}
{{- end }}
{{- range $name, $set := (.Values.managedClusterSets | default dict) }}
  {{- $_ := set $sources (printf "managedClusterSet '%s'" $name) (index $set "labels" | default dict) }}
{{- end }}
{{- range $name, $cluster := (.Values.clusters | default dict) }}
  {{- $_ := set $sources (printf "cluster '%s'" $name) (index $cluster "labels" | default dict) }}
{{- end }}

{{/* Extract global schema */}}
{{- $globalSchema := index $schemas "_global" | default dict }}

{{/* Pre-compute all dynamic prefixes from schemas (operator-name + prefix) */}}
{{- $allDynamicPrefixes := list }}
{{- range $schemaName, $schema := $schemas }}
  {{- if ne $schemaName "_global" }}
    {{- range $dp := (index $schema "dynamicPrefixes" | default list) }}
      {{- $allDynamicPrefixes = append $allDynamicPrefixes (printf "%s-%s" $schemaName $dp) }}
    {{- end }}
  {{- end }}
{{- end }}

{{/* Validate each label source independently */}}
{{- range $sourceName, $labels := $sources }}

  {{/* ================================================================ */}}
  {{/* Step 1: Detect operators by scanning for *-subscription-name     */}}
  {{/* ================================================================ */}}
  {{- $detectedOperators := dict }}
  {{- range $labelName, $_ := $labels }}
    {{- if hasSuffix "-subscription-name" $labelName }}
      {{- $opName := trimSuffix "-subscription-name" $labelName }}
      {{- $_ := set $detectedOperators $opName true }}
    {{- end }}
  {{- end }}

  {{/* ================================================================ */}}
  {{/* Step 2: Build the set of all known (allowed) label names         */}}
  {{/* ================================================================ */}}
  {{- $knownLabels := dict }}

  {{/* 2a: Global labels */}}
  {{- range $g := (index $globalSchema "optional" | default list) }}
    {{- $_ := set $knownLabels $g true }}
  {{- end }}

  {{/* 2b: Standard operator labels (6 per detected operator) */}}
  {{- range $opName, $_ := $detectedOperators }}
    {{- $_ := set $knownLabels $opName true }}
    {{- range $suffix := $operatorSuffixes }}
      {{- $_ := set $knownLabels (printf "%s-%s" $opName $suffix) true }}
    {{- end }}
  {{- end }}

  {{/* 2c: Operator extras from schema (entries WITHOUT "enable" field) */}}
  {{- range $schemaName, $schema := $schemas }}
    {{- if and (ne $schemaName "_global") (not (hasKey $schema "enable")) }}
      {{- range $opt := (index $schema "optional" | default list) }}
        {{- $_ := set $knownLabels (printf "%s-%s" $schemaName $opt) true }}
      {{- end }}
      {{- range $bl := (index $schema "boolLabels" | default list) }}
        {{- $_ := set $knownLabels (printf "%s-%s" $schemaName $bl) true }}
      {{- end }}
      {{- range $avKey, $_ := (index $schema "allowedValues" | default dict) }}
        {{- $_ := set $knownLabels (printf "%s-%s" $schemaName $avKey) true }}
      {{- end }}
    {{- end }}
  {{- end }}

  {{/* 2d: Feature labels from schema (entries WITH "enable" field) */}}
  {{- range $schemaName, $schema := $schemas }}
    {{- if and (ne $schemaName "_global") (hasKey $schema "enable") }}
      {{- $_ := set $knownLabels (index $schema "enable") true }}
      {{- range $opt := (index $schema "optional" | default list) }}
        {{- $_ := set $knownLabels (printf "%s-%s" $schemaName $opt) true }}
      {{- end }}
      {{- range $bl := (index $schema "boolLabels" | default list) }}
        {{- $_ := set $knownLabels (printf "%s-%s" $schemaName $bl) true }}
      {{- end }}
      {{- range $avKey, $_ := (index $schema "allowedValues" | default dict) }}
        {{- $_ := set $knownLabels (printf "%s-%s" $schemaName $avKey) true }}
      {{- end }}
      {{- range $req := (index $schema "required" | default list) }}
        {{- $_ := set $knownLabels (printf "%s-%s" $schemaName $req) true }}
      {{- end }}
    {{- end }}
  {{- end }}

  {{/* ================================================================ */}}
  {{/* Step 3: Reject unknown labels                                    */}}
  {{/* ================================================================ */}}
  {{- range $labelName, $_ := $labels }}
    {{- if not (hasKey $knownLabels $labelName) }}
      {{- $match := dict "found" false }}
      {{- range $prefix := $allDynamicPrefixes }}
        {{- if hasPrefix $prefix $labelName }}
          {{- $_ := set $match "found" true }}
        {{- end }}
      {{- end }}
      {{- if not (index $match "found") }}
        {{- $errors = append $errors (printf "%s: unknown label '%s' — add it to _schema.tpl" $sourceName $labelName) }}
      {{- end }}
    {{- end }}
  {{- end }}

  {{/* ================================================================ */}}
  {{/* Step 4: Validate enabled operators (required labels)             */}}
  {{/* ================================================================ */}}
  {{- range $opName, $_ := $detectedOperators }}
    {{- $enableVal := (index $labels $opName | default "" | toString) }}
    {{- if eq $enableVal "true" }}
      {{- range $req := list "channel" "source" "source-namespace" }}
        {{- $labelKey := (printf "%s-%s" $opName $req) }}
        {{- $labelVal := (index $labels $labelKey | default "" | toString) }}
        {{- if eq $labelVal "" }}
          {{- $errors = append $errors (printf "%s: operator '%s' is enabled but required label '%s' is missing or empty" $sourceName $opName $labelKey) }}
        {{- end }}
      {{- end }}
    {{- end }}
  {{- end }}

  {{/* ================================================================ */}}
  {{/* Step 5: Validate enabled features (required labels)              */}}
  {{/* ================================================================ */}}
  {{- range $schemaName, $schema := $schemas }}
    {{- if and (ne $schemaName "_global") (hasKey $schema "enable") }}
      {{- $enableLabel := (index $schema "enable") }}
      {{- $enableVal := (index $labels $enableLabel | default "" | toString) }}
      {{- $enabled := false }}
      {{- if eq (toString ((index $schema "enableCheck") | default "")) "nonempty" }}
        {{- if ne $enableVal "" }}
          {{- $enabled = true }}
        {{- end }}
      {{- else }}
        {{- if eq $enableVal "true" }}
          {{- $enabled = true }}
        {{- end }}
      {{- end }}
      {{- if $enabled }}
        {{- range $req := (index $schema "required" | default list) }}
          {{- $labelKey := (printf "%s-%s" $schemaName $req) }}
          {{- $labelVal := (index $labels $labelKey | default "" | toString) }}
          {{- if eq $labelVal "" }}
            {{- $errors = append $errors (printf "%s: '%s' is enabled but required label '%s' is missing or empty" $sourceName $schemaName $labelKey) }}
          {{- end }}
        {{- end }}
      {{- end }}
    {{- end }}
  {{- end }}

  {{/* ================================================================ */}}
  {{/* Step 6: Validate allowedValues and boolLabels (all schemas)      */}}
  {{/* ================================================================ */}}
  {{- range $schemaName, $schema := $schemas }}
    {{- if ne $schemaName "_global" }}
      {{/* allowedValues */}}
      {{- $allowedValues := (index $schema "allowedValues") }}
      {{- if $allowedValues }}
        {{- range $avLabel, $avList := $allowedValues }}
          {{- $labelKey := (printf "%s-%s" $schemaName $avLabel) }}
          {{- $labelVal := (index $labels $labelKey | default "" | toString) }}
          {{- if and (ne $labelVal "") (not (has $labelVal $avList)) }}
            {{- $errors = append $errors (printf "%s: '%s' label '%s' has invalid value '%s' (allowed: %s)" $sourceName $schemaName $labelKey $labelVal (join ", " $avList)) }}
          {{- end }}
        {{- end }}
      {{- end }}
      {{/* boolLabels */}}
      {{- $boolLabels := (index $schema "boolLabels") }}
      {{- if $boolLabels }}
        {{- range $bl := $boolLabels }}
          {{- $labelKey := (printf "%s-%s" $schemaName $bl) }}
          {{- $labelVal := (index $labels $labelKey | default "" | toString) }}
          {{- if and (ne $labelVal "") (ne $labelVal "true") (ne $labelVal "false") }}
            {{- $errors = append $errors (printf "%s: '%s' label '%s' must be 'true' or 'false' (got: '%s')" $sourceName $schemaName $labelKey $labelVal) }}
          {{- end }}
        {{- end }}
      {{- end }}
    {{- end }}
  {{- end }}

{{- end }}

{{/* Fail with all collected errors */}}
{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nLabel validation failed (%d errors):\n  - %s\n" (len $errors) (join "\n  - " $errors)) }}
{{- end }}
{{- end -}}

{{/*
Placeholder for cluster-install validation (future expansion).
*/}}
{{- define "autoshift.validate-cluster-install" -}}
{{- end -}}
