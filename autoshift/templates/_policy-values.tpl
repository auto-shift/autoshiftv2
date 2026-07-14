{{/*
autoshift.policyValuesObject renders the valuesObject passed to every Helm-sourced policy chart
(both OCI list-generator and Git files-generator branches of the ApplicationSet). Kept in one place
so the two branches never drift. Consumed via: {{- include "autoshift.policyValuesObject" . | nindent N }}
*/}}
{{- define "autoshift.policyValuesObject" -}}
{{- $clusterSetSuffix := include "autoshift.clusterSetSuffix" . -}}
gitopsNamespace: {{ .Values.gitopsNamespace }}
policy_namespace: {{ printf "policies-%s" .Release.Name }}
clusterSetSuffix: {{ $clusterSetSuffix }}
{{- if .Values.gitopsPolicyGeneratorSidecarImage }}
gitops:
  policyGenerator:
    sidecarImage: {{ .Values.gitopsPolicyGeneratorSidecarImage }}
{{- end }}
autoshift:
  dryRun: {{ ((.Values.autoshift).dryRun) | default false }}
  evaluationInterval:
    compliant: {{ (((.Values.autoshift).evaluationInterval).compliant) | default "10m" }}
    noncompliant: {{ (((.Values.autoshift).evaluationInterval).noncompliant) | default "30s" }}
{{- if .Values.hubClusterSets }}
hubClusterSets:
{{- range $cluster, $clustervalue := .Values.hubClusterSets }}
  {{ $cluster }}{{ $clusterSetSuffix }}:
    labels:
      self-managed: '{{ index $clustervalue.labels "self-managed" | default "true" }}'
{{- end }}
{{- end }}
{{- if .Values.managedClusterSets }}
managedClusterSets:
{{- range $cluster, $clustervalue := .Values.managedClusterSets }}
  {{ $cluster }}{{ $clusterSetSuffix }}:
    labels: {}
{{- end }}
{{- end }}
{{- end -}}
