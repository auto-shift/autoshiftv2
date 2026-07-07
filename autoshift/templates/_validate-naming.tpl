{{/*
Validate naming length constraints to prevent ACM policy name limit violations.

ACM enforces: len(policy_namespace) + len(policy_name) <= 62
AutoShift enforces:
  - policy namespace (policies-{Release.Name}): <= 20 chars
      → Release.Name must be <= 11 chars
  - clusterset/cluster keys (values file keys): <= 20 chars each
  - clusterset names with versioning suffix: <= 63 chars
      (Kubernetes label value limit for cluster.open-cluster-management.io/clusterset)
  - policy names: <= 40 chars
*/}}

{{/*
Compute the clusterSet suffix for versioned deployments.
Returns "-{sanitized-version-tag}" when versionedClusterSets is true, empty string otherwise.
This is the single source of truth — autoshift-app-set.yaml and the configmap templates use this.
*/}}
{{- define "autoshift.clusterSetSuffix" -}}
{{- if .Values.versionedClusterSets -}}
  {{- $versionTag := "" -}}
  {{- if .Values.autoshiftOciRegistry -}}
    {{- $versionTag = .Values.autoshiftOciVersion | default "latest" -}}
  {{- else -}}
    {{- $versionTag = .Values.autoshiftGitBranchTag | default "main" -}}
  {{- end -}}
  {{- printf "-%s" ($versionTag | replace "." "-" | replace "/" "-" | lower) -}}
{{- end -}}
{{- end -}}

{{- define "autoshift.validate-naming" -}}
{{- $errors := list }}
{{- $suffix := include "autoshift.clusterSetSuffix" . }}

{{/* Validate Release.Name produces a namespace <= 20 chars */}}
{{- $ns := printf "policies-%s" .Release.Name }}
{{- if gt (len $ns) 20 }}
  {{- $errors = append $errors (printf "Release name '%s' produces policy namespace '%s' (%d chars, max 20). Shorten the Helm release name to %d chars or fewer." .Release.Name $ns (len $ns) (sub 20 (len "policies-"))) }}
{{- end }}

{{/* Validate hubClusterSets keys <= 20 chars, and key+suffix <= 63 chars when versioning is enabled */}}
{{- range $name, $_ := .Values.hubClusterSets }}
  {{- if gt (len $name) 20 }}
    {{- $errors = append $errors (printf "hubClusterSets key '%s' is %d chars (max 20)" $name (len $name)) }}
  {{- end }}
  {{- $full := printf "%s%s" $name $suffix }}
  {{- if and $suffix (gt (len $full) 63) }}
    {{- $maxKey := sub 63 (len $suffix) }}
    {{- if gt $maxKey 0 }}
      {{- $errors = append $errors (printf "hubClusterSets key '%s' + version suffix '%s' = '%s' (%d chars, max 63). Shorten the key to %d chars or fewer, or use a shorter branch/tag name." $name $suffix $full (len $full) $maxKey) }}
    {{- else }}
      {{- $errors = append $errors (printf "hubClusterSets key '%s' + version suffix '%s' = '%s' (%d chars, max 63). The branch/tag name is too long — use a shorter branch/tag (max %d chars after sanitization)." $name $suffix $full (len $full) (sub 62 (len $name))) }}
    {{- end }}
  {{- end }}
{{- end }}

{{/* Validate managedClusterSets keys <= 20 chars, and key+suffix <= 63 chars when versioning is enabled */}}
{{- range $name, $_ := .Values.managedClusterSets }}
  {{- if gt (len $name) 20 }}
    {{- $errors = append $errors (printf "managedClusterSets key '%s' is %d chars (max 20)" $name (len $name)) }}
  {{- end }}
  {{- $full := printf "%s%s" $name $suffix }}
  {{- if and $suffix (gt (len $full) 63) }}
    {{- $maxKey := sub 63 (len $suffix) }}
    {{- if gt $maxKey 0 }}
      {{- $errors = append $errors (printf "managedClusterSets key '%s' + version suffix '%s' = '%s' (%d chars, max 63). Shorten the key to %d chars or fewer, or use a shorter branch/tag name." $name $suffix $full (len $full) $maxKey) }}
    {{- else }}
      {{- $errors = append $errors (printf "managedClusterSets key '%s' + version suffix '%s' = '%s' (%d chars, max 63). The branch/tag name is too long — use a shorter branch/tag (max %d chars after sanitization)." $name $suffix $full (len $full) (sub 62 (len $name))) }}
    {{- end }}
  {{- end }}
{{- end }}

{{/* Validate clusters keys <= 20 chars */}}
{{- range $name, $_ := .Values.clusters }}
  {{- if gt (len $name) 20 }}
    {{- $errors = append $errors (printf "clusters key '%s' is %d chars (max 20)" $name (len $name)) }}
  {{- end }}
{{- end }}

{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nNaming validation failed (%d errors):\n  - %s\n\nACM enforces a 62-char combined limit on policy namespace + policy name.\nAutoShift reserves 20 chars for the namespace and 40 for policy names.\nClusterset names with versioning suffix must fit Kubernetes label values (max 63 chars).\n" (len $errors) (join "\n  - " $errors)) }}
{{- end }}
{{- end -}}
