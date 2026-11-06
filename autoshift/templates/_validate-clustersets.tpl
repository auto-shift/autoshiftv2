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

{{/*
Validate the Red Hat Advanced Cluster Management add-on infra mode.

gitops-infra: 'addon' makes the add-on's agent the cluster's infra agent. The add-on installs into
openshift-gitops on the managed cluster, a namespace fixed in the add-on itself, so a cluster whose
gitops-namespace names anything else would have AutoShift and the add-on disagree about where its
Argo CD lives.

A cluster's effective labels are its clusterset's labels with its own on top, so a cluster entry is
checked against the clusterset its config.clusterSet names. gitops-namespace falls back to
gitopsNamespace, as the policies do.
*/}}
{{- define "autoshift.validate-infra-agent" -}}
{{- $errors := list }}
{{- $defaultNs := .Values.gitopsNamespace | default "openshift-gitops" }}
{{- $setLabels := dict }}
{{- range $bucket := list "hubClusterSets" "managedClusterSets" }}
  {{- range $name, $value := (index $.Values $bucket | default dict) }}
    {{- $labels := ($value | default dict).labels | default dict }}
    {{- $_ := set $setLabels $name $labels }}
    {{- $ns := toString (index $labels "gitops-namespace" | default $defaultNs) }}
    {{- if and (eq (toString (index $labels "gitops-infra" | default "")) "addon") (ne $ns "openshift-gitops") }}
      {{- $errors = append $errors (printf "%s['%s'] sets gitops-infra: 'addon' with gitops-namespace '%s'" $bucket $name $ns) }}
    {{- end }}
  {{- end }}
{{- end }}
{{- range $name, $value := (.Values.clusters | default dict) }}
  {{- $own := ($value | default dict).labels | default dict }}
  {{- $setName := dig "config" "clusterSet" "" ($value | default dict) }}
  {{- $labels := merge (deepCopy $own) (deepCopy (index $setLabels $setName | default dict)) }}
  {{- $ns := toString (index $labels "gitops-namespace" | default $defaultNs) }}
  {{- if and (eq (toString (index $labels "gitops-infra" | default "")) "addon") (ne $ns "openshift-gitops") }}
    {{- $errors = append $errors (printf "clusters['%s'] ends up with gitops-infra: 'addon' and gitops-namespace '%s'" $name $ns) }}
  {{- end }}
{{- end }}
{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nInfra Argo CD add-on validation failed (%d errors):\n  - %s\n\ngitops-infra: 'addon' uses the Red Hat Advanced Cluster Management add-on's agent, which always installs into openshift-gitops on the managed cluster. Set gitops-namespace to 'openshift-gitops' (or remove it), or choose another gitops-infra mode.\n" (len $errors) (join "\n  - " $errors)) }}
{{- end }}
{{- end -}}
