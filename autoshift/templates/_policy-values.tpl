{{/*
autoshift.policyValuesObject renders the valuesObject passed to every Helm-sourced policy chart
(both OCI list-generator and Git files-generator branches of the ApplicationSet). Kept in one place
so the two branches never drift. Consumed via: {{- include "autoshift.policyValuesObject" . | nindent N }}
*/}}
{{/*
autoshift.selfManagedHubGitops returns the self-managed hub clusterset's config.gitops dict (the
hubClusterSets entry whose labels.self-managed == 'true'), or an empty dict. Serialized as YAML;
callers re-parse with `| fromYaml`. This is the clusterset-level home for the infra ArgoCD config
(config.gitops.infra), the gitops namespace (config.gitops.namespace), and the policyGenerator /
defaultInstance toggles. The infra ArgoCD lives only on the self-managed hub, so resolving from that
clusterset at Helm-render time is sufficient (no per-cluster runtime lookup needed).
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
{{- $gitopsCfg := include "autoshift.selfManagedHubGitops" . | fromYaml -}}
{{- /* Infra ArgoCD config forwarded to the openshift-gitops policy chart, deep-merged over its
       gitops.* defaults: (1) config.gitops.infra field overrides; (2) policyGenerator.enabled — the
       effective flag (self-managed hub override of the global default) that gates the policy-generator
       CMP sidecar in the ArgoCD repo-server; (3) the CMP sidecar image. Computed up front so the
       output lines below stay contiguous (chained {{- -}} assignments would eat the separators). */ -}}
{{- $pg := .Values.policyGenerator -}}
{{- if hasKey $gitopsCfg "policyGenerator" -}}{{- $pg = (index $gitopsCfg "policyGenerator") -}}{{- end -}}
{{- $gitopsOut := deepCopy ((index $gitopsCfg "infra") | default dict) -}}
{{- $pgDict := dict "enabled" ($pg | default false) -}}
{{- with .Values.gitopsPolicyGeneratorSidecarImage -}}{{- $_ := set $pgDict "sidecarImage" . -}}{{- end -}}
{{- $_ := set $gitopsOut "policyGenerator" $pgDict -}}
{{- /* config.gitops.defaultInstance -> gitops.disableDefaultArgoCD (disable = not defaultInstance);
       the operator-install policy label autoshift.io/gitops-disable-default-argocd still overrides. */ -}}
{{- if hasKey $gitopsCfg "defaultInstance" -}}{{- $_ := set $gitopsOut "disableDefaultArgoCD" (not (index $gitopsCfg "defaultInstance")) -}}{{- end -}}
gitopsNamespace: {{ include "autoshift.gitopsNamespace" . }}
policy_namespace: {{ printf "policies-%s" .Release.Name }}
clusterSetSuffix: {{ $clusterSetSuffix }}
gitops:
{{ $gitopsOut | toYaml | indent 2 }}
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
