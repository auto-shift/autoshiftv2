{{/*
Validate naming length constraints to prevent ACM policy name limit violations.
ACM enforces: len(policy_namespace) + len(policy_name) <= 62
AutoShift enforces: namespace <= 20, clusterset/cluster names <= 20, policy names <= 40
policy_namespace = "policies-{Release.Name}", so Release.Name must be <= 11 chars.
*/}}
{{- define "autoshift.validate-naming" -}}
{{- $errors := list }}

{{/* Validate Release.Name produces a namespace <= 20 chars */}}
{{- $ns := printf "policies-%s" .Release.Name }}
{{- if gt (len $ns) 20 }}
  {{- $errors = append $errors (printf "Release name '%s' produces policy namespace 'policies-%s' (%d chars, max 20). Shorten the Helm release name to %d chars or fewer." .Release.Name .Release.Name (len $ns) (sub 11 0)) }}
{{- end }}

{{/* Validate hubClusterSets keys <= 20 chars */}}
{{- range $name, $_ := .Values.hubClusterSets }}
  {{- if gt (len $name) 20 }}
    {{- $errors = append $errors (printf "hubClusterSets key '%s' is %d chars (max 20)" $name (len $name)) }}
  {{- end }}
{{- end }}

{{/* Validate managedClusterSets keys <= 20 chars */}}
{{- range $name, $_ := .Values.managedClusterSets }}
  {{- if gt (len $name) 20 }}
    {{- $errors = append $errors (printf "managedClusterSets key '%s' is %d chars (max 20)" $name (len $name)) }}
  {{- end }}
{{- end }}

{{/* Validate clusters keys <= 20 chars */}}
{{- range $name, $_ := .Values.clusters }}
  {{- if gt (len $name) 20 }}
    {{- $errors = append $errors (printf "clusters key '%s' is %d chars (max 20)" $name (len $name)) }}
  {{- end }}
{{- end }}

{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nNaming validation failed (%d errors):\n  - %s\n\nACM enforces a 62-char combined limit on policy namespace + policy name.\nAutoShift reserves 20 chars for the namespace and 40 for policy names.\n" (len $errors) (join "\n  - " $errors)) }}
{{- end }}
{{- end -}}
