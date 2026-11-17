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

{{/*
autoshift.policyStandard / autoshift.policyStandardHub = the effective ACM Governance standard names.
policyStandardHub falls back to policyStandard, and both default to "NIST SP 800-53". Every consumer
(the ApplicationSet plugin env, the valuesObject, both dedicated ConfigMap apps) must go through these,
so the fallback is written once and the value is always validated.

The name is a display string, but it is substituted into PolicyGenerator configs by sed in the CMP
sidecar and pg-render.sh, and emitted into YAML in several places. Characters that would change sed or
YAML structure are rejected here with a readable message: " ' \ | & { } and line breaks. Braces would
otherwise be evaluated as a template by the ApplicationSet (goTemplate, missingkey=error). Everything
else, including a colon (ISO/IEC 27001:2022), is fine because every emitted copy is quoted.
*/}}
{{- define "autoshift.validatePolicyStandard" -}}
{{- if not (trim .value) -}}
{{- fail (printf "%s must not be empty" .key) -}}
{{- end -}}
{{- if regexMatch "[\"'\\\\|&{}\\r\\n]" .value -}}
{{- fail (printf "%s %q contains a character AutoShift cannot pass through safely (one of \" ' \\ | & { } or a line break)" .key .value) -}}
{{- end -}}
{{- end -}}

{{- define "autoshift.policyStandard" -}}
{{- $v := toString (((.Values.autoshift).policyStandard) | default "NIST SP 800-53") -}}
{{- include "autoshift.validatePolicyStandard" (dict "key" "autoshift.policyStandard" "value" $v) -}}
{{- $v -}}
{{- end -}}

{{- define "autoshift.policyStandardHub" -}}
{{- $v := toString (((.Values.autoshift).policyStandardHub) | default (include "autoshift.policyStandard" .)) -}}
{{- include "autoshift.validatePolicyStandard" (dict "key" "autoshift.policyStandardHub" "value" $v) -}}
{{- $v -}}
{{- end -}}

{{- define "autoshift.policyValuesObject" -}}
{{- $clusterSetSuffix := include "autoshift.clusterSetSuffix" . -}}
gitopsNamespace: {{ include "autoshift.gitopsNamespace" . }}
policy_namespace: {{ printf "policies-%s" .Release.Name }}
clusterSetSuffix: {{ $clusterSetSuffix }}
autoshift:
  dryRun: {{ ((.Values.autoshift).dryRun) | default false }}
  policyStandard: {{ include "autoshift.policyStandard" . | quote }}
  policyStandardHub: {{ include "autoshift.policyStandardHub" . | quote }}
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
