{{/*
Validate global-hub-deploy-mode, the label that imports a managed hub into Global Hub.

default: Global Hub imports a hub in default mode only when that hub has self-management turned
off, which AutoShift sets with self-managed 'false'.

hosted: refused. Hosted mode disables the policyController, certPolicyController and
applicationManager add-ons for the imported hub, so the hub-of-hubs could no longer place AutoShift
policies on it.

Labels are resolved the way cluster-labels merges them at runtime: a cluster's own label wins over
its clusterset's, an empty value falls through to the clusterset's, and '_' removes the label. A
clusters entry names its clusterset only through config.clusterSet. Without it the clusterset is
whatever the ManagedCluster carries at runtime, so only the cluster's own labels are known, and a
check that needs an unknown label is skipped rather than guessed.
*/}}
{{- define "autoshift.validate-global-hub" -}}
{{- $errors := list }}
{{- $targets := list }}
{{- $clusterSets := dict }}
{{- range $bucket := list "hubClusterSets" "managedClusterSets" }}
  {{- range $name, $cs := (index $.Values $bucket | default dict) }}
    {{- $_ := set $clusterSets $name $cs }}
    {{- $targets = append $targets (dict "where" (printf "%s.%s" $bucket $name) "labels" ($cs.labels | default dict) "known" true) }}
  {{- end }}
{{- end }}
{{- range $name, $cluster := (.Values.clusters | default dict) }}
  {{- $csName := dig "config" "clusterSet" "" $cluster }}
  {{- $known := hasKey $clusterSets $csName }}
  {{- $labels := merge (dict) ($cluster.labels | default dict) ((index $clusterSets $csName | default dict).labels | default dict) }}
  {{- $targets = append $targets (dict "where" (printf "clusters.%s" $name) "labels" $labels "known" $known) }}
{{- end }}

{{- range $t := $targets }}
  {{- $mode := toString (index $t.labels "global-hub-deploy-mode" | default "") }}
  {{- if eq $mode "_" }}
    {{- $mode = "" }}
  {{- end }}
  {{- $selfManaged := toString (index $t.labels "self-managed" | default "") }}
  {{- if eq $mode "hosted" }}
    {{- $errors = append $errors (printf "%s: global-hub-deploy-mode 'hosted' is not supported; hosted mode disables the policy add-on for that hub, so the hub-of-hubs could no longer configure it. Use 'default' with self-managed 'false'" $t.where) }}
  {{- else if eq $mode "default" }}
    {{- if and (ne $selfManaged "false") (or $t.known (ne $selfManaged "")) }}
      {{- $errors = append $errors (printf "%s: global-hub-deploy-mode 'default' needs self-managed 'false'; Global Hub imports a hub in default mode only with self-management off" $t.where) }}
    {{- end }}
  {{- else if ne $mode "" }}
    {{- $errors = append $errors (printf "%s: global-hub-deploy-mode is %q; use 'default' or leave it unset" $t.where $mode) }}
  {{- end }}
{{- end }}

{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nGlobal Hub validation failed (%d errors):\n  - %s\n" (len $errors) (join "\n  - " $errors)) }}
{{- end }}
{{- end -}}
