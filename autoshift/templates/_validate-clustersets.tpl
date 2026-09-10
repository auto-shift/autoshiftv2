{{/*
Validate clusterset label declarations.

self-managed marks a hub as registering itself as a ManagedCluster in its own ACM. It is only
meaningful for a hub clusterset, so declaring it under managedClusterSets is a misconfiguration
rather than a no-op — fail the render instead of propagating a label that nothing will honor.
*/}}
{{- define "autoshift.validate-clustersets" -}}
{{- $errors := list }}

{{- range $name, $value := .Values.managedClusterSets }}
  {{- if hasKey ($value.labels | default dict) "self-managed" }}
    {{- $errors = append $errors (printf "managedClusterSets['%s'] declares 'self-managed: %v'" $name (index $value.labels "self-managed")) }}
  {{- end }}
{{- end }}

{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nClusterset validation failed (%d errors):\n  - %s\n\n'self-managed' is only valid on a hubClusterSets entry — it registers a hub as a self-managed cluster in its own ACM.\nMove the clusterset to hubClusterSets, or remove the label.\nUse 'autoshift.io/cluster-type' (hub|spoke, derived automatically from the bucket) to select clusters by type.\n" (len $errors) (join "\n  - " $errors)) }}
{{- end }}
{{- end -}}
