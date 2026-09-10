{{/*
autoshift.policyValuesObject renders the valuesObject passed to every Helm-sourced policy chart
(both OCI list-generator and Git files-generator branches of the ApplicationSet). Kept in one place
so the two branches never drift. Consumed via: {{- include "autoshift.policyValuesObject" . | nindent N }}
*/}}
{{/*
autoshift.selfManagedHubGitops returns the self-managed hub clusterset's config.gitops dict (the
hubClusterSets entry whose labels.self-managed == 'true'), or an empty dict. Serialized as YAML;
callers re-parse with `| fromYaml`. Used ONLY for the two things that must be resolved at Helm-render
time — before any policy runs, so they can't be read from the runtime rendered-config ConfigMap:
  - config.gitops.namespace       -> autoshift.gitopsNamespace (the ApplicationSet app destination ns)
  - config.gitops.policyGenerator -> autoshift.validate-gitops (git/OCI fail-fast check)
Everything else the openshift-gitops policy needs (infra tuning, the policyGenerator CMP gate,
defaultInstance) is read at enforcement time from the rendered-config ConfigMap, not here.
*/}}
{{- define "autoshift.selfManagedHubGitops" -}}
{{- $g := dict -}}
{{- range $name, $cs := (.Values.hubClusterSets | default dict) -}}
{{- if eq (toString (index ($cs.labels | default dict) "self-managed")) "true" -}}
{{- $g = (($cs.config | default dict).gitops | default dict) -}}
{{- end -}}
{{- end -}}
{{- $g | toYaml -}}
{{- end -}}

{{/*
autoshift.gitopsNamespace = the effective gitops namespace for the WHOLE deployment: the self-managed
hub clusterset's config.gitops.namespace, else the global .Values.gitopsNamespace. Use this everywhere
instead of .Values.gitopsNamespace so the ApplicationSet, dedicated apps, valuesObject, and infra
ArgoCD all move together when a hub overrides the namespace.
*/}}
{{- define "autoshift.gitopsNamespace" -}}
{{- $gitopsCfg := include "autoshift.selfManagedHubGitops" . | fromYaml -}}
{{- (index $gitopsCfg "namespace") | default .Values.gitopsNamespace -}}
{{- end -}}

{{- define "autoshift.policyValuesObject" -}}
{{- $clusterSetSuffix := include "autoshift.clusterSetSuffix" . -}}
gitopsNamespace: {{ include "autoshift.gitopsNamespace" . }}
policy_namespace: {{ printf "policies-%s" .Release.Name }}
clusterSetSuffix: {{ $clusterSetSuffix }}
autoshift:
  dryRun: {{ ((.Values.autoshift).dryRun) | default false }}
  evaluationInterval:
    compliant: {{ (((.Values.autoshift).evaluationInterval).compliant) | default "watch" }}
    noncompliant: {{ (((.Values.autoshift).evaluationInterval).noncompliant) | default "watch" }}
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
