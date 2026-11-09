{{/*
Validate the acm-failover label.

Its placements match the exact values 'active' and 'passive' on a hub, so any other spelling, such
as 'Active', or either value on a managed clusterset, selects no cluster: hub disaster recovery is
off and every policy stays silent. Fail the render instead.
*/}}
{{- define "autoshift.validate-acm-failover" -}}
{{- $errors := list }}
{{- $allowed := list "active" "passive" "false" "" }}
{{- $buckets := dict }}

{{- range $bucket := list "hubClusterSets" "managedClusterSets" }}
  {{- range $name, $cs := (index $.Values $bucket | default dict) }}
    {{- $_ := set $buckets $name $bucket }}
    {{- $labels := ($cs.labels | default dict) }}
    {{- if hasKey $labels "acm-failover" }}
      {{- $mode := toString (index $labels "acm-failover") }}
      {{- if not (has $mode $allowed) }}
        {{- $errors = append $errors (printf "%s.%s sets acm-failover '%s'" $bucket $name $mode) }}
      {{- else if and (eq $bucket "managedClusterSets") (has $mode (list "active" "passive")) }}
        {{- $errors = append $errors (printf "%s.%s sets acm-failover '%s' on a managed clusterset" $bucket $name $mode) }}
      {{- end }}
    {{- end }}
  {{- end }}
{{- end }}

{{- range $name, $cluster := (.Values.clusters | default dict) }}
  {{- $labels := ($cluster.labels | default dict) }}
  {{- if hasKey $labels "acm-failover" }}
    {{- $mode := toString (index $labels "acm-failover") }}
    {{- $bucket := (index $buckets (dig "config" "clusterSet" "" $cluster) | default "") }}
    {{- if not (has $mode $allowed) }}
      {{- $errors = append $errors (printf "clusters.%s sets acm-failover '%s'" $name $mode) }}
    {{- else if and (eq $bucket "managedClusterSets") (has $mode (list "active" "passive")) }}
      {{- $errors = append $errors (printf "clusters.%s sets acm-failover '%s' but its clusterset is a managed clusterset" $name $mode) }}
    {{- end }}
  {{- end }}
{{- end }}

{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nacm-failover validation failed (%d errors):\n  - %s\n\nThe acm-failover label accepts exactly 'active', 'passive' or 'false', in lowercase, and 'active' and 'passive' only on a hub.\nAny other value matches no placement, so hub disaster recovery would be silently off.\n" (len $errors) (join "\n  - " $errors)) }}
{{- end }}
{{- end -}}
