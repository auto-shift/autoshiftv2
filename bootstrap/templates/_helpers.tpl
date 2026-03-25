{{/*
Resolve the hub clusterset labels from selfManagedHubSet.
Returns the labels map for the hub clusterset.
*/}}
{{- define "bootstrap.hubLabels" -}}
  {{- $hubSetName := .Values.selfManagedHubSet | default "hub" -}}
  {{- $hubSet := index .Values.hubClusterSets $hubSetName -}}
  {{- $hubSet.labels | toYaml -}}
{{- end -}}

{{/*
GitOps operator namespace
*/}}
{{- define "bootstrap.gitops.namespace" -}}
  {{- "openshift-gitops-operator" -}}
{{- end -}}

{{/*
GitOps ArgoCD namespace
*/}}
{{- define "bootstrap.gitops.argoNamespace" -}}
  {{- .Values.gitopsNamespace | default "openshift-gitops" -}}
{{- end -}}

{{/*
ACM namespace
*/}}
{{- define "bootstrap.acm.namespace" -}}
  {{- "open-cluster-management" -}}
{{- end -}}

