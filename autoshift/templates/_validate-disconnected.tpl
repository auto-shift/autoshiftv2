{{/*
Validate that disconnected-mirror 'true' has catalogs to subscribe to.

The label points every operator Subscription at <source>-<mirror-catalog-suffix>, and only
config.disconnected.catalogs creates those CatalogSources. Without catalogs, the catalog policy
renders empty and reports Compliant while no operator can resolve. Catalogs are resolved the way
rendered-config merges them: the cluster's own, then its clusterset's, then the global config.
*/}}
{{- define "autoshift.validate-disconnected-catalogs" -}}
{{- $errors := list }}
{{- $globalCatalogs := (dig "disconnected" "catalogs" (list) (.Values.config | default dict)) }}
{{- $clusterSets := dict }}
{{- range $bucket := list "hubClusterSets" "managedClusterSets" }}
  {{- range $name, $cs := (index $.Values $bucket | default dict) }}
    {{- $_ := set $clusterSets $name $cs }}
    {{- $mirrored := eq (toString (index ($cs.labels | default dict) "disconnected-mirror" | default "false")) "true" }}
    {{- $catalogs := (dig "config" "disconnected" "catalogs" (list) $cs) | default $globalCatalogs }}
    {{- if and $mirrored (not $catalogs) }}
      {{- $errors = append $errors (printf "%s.%s" $bucket $name) }}
    {{- end }}
  {{- end }}
{{- end }}

{{- range $name, $cluster := (.Values.clusters | default dict) }}
  {{- $cs := (index $clusterSets (dig "config" "clusterSet" "" $cluster) | default dict) }}
  {{- $label := (index ($cluster.labels | default dict) "disconnected-mirror") }}
  {{- if kindIs "invalid" $label }}
    {{- $label = (index ($cs.labels | default dict) "disconnected-mirror" | default "false") }}
  {{- end }}
  {{- $catalogs := (dig "config" "disconnected" "catalogs" (list) $cluster) | default (dig "config" "disconnected" "catalogs" (list) $cs) | default $globalCatalogs }}
  {{- if and (eq (toString $label) "true") (not $catalogs) }}
    {{- $errors = append $errors (printf "clusters.%s" $name) }}
  {{- end }}
{{- end }}

{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nDisconnected validation failed (%d errors):\n  - %s\n\nThese set disconnected-mirror 'true' with no config.disconnected.catalogs in their own, their clusterset's or the global config.\nEvery operator Subscription on them uses <source>-<mirror-catalog-suffix>, and only config.disconnected.catalogs creates those CatalogSources.\nAdd the mirrored catalogs, or set disconnected-mirror 'false'.\n" (len $errors) (join "\n  - " $errors)) }}
{{- end }}
{{- end -}}
