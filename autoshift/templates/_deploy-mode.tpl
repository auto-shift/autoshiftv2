{{/*
autoshift.ociMode returns "true" when policies come from an OCI registry, "" when they come from
git. Every template that renders a repoURL branches on this, so the two modes cannot disagree.

autoshiftOciRepo alone selects OCI: one value both chooses the mode and supplies the address, so
there is no way to set half of it.

autoshiftOciRegistry is the deprecated boolean that used to select the mode. While it is present it
still decides, including when it is false, so an existing deployment behaves exactly as before.
hasKey rather than truthiness, to tell "not set" from "set to false".
*/}}
{{- define "autoshift.ociMode" -}}
{{- $oci := "" -}}
{{- if hasKey .Values "autoshiftOciRegistry" -}}
  {{- if .Values.autoshiftOciRegistry -}}{{- $oci = "true" -}}{{- end -}}
{{- else if .Values.autoshiftOciRepo -}}
  {{- $oci = "true" -}}
{{- end -}}
{{/* Validated here rather than in one template, because Helm does not order template rendering:
     OCI mode with no registry renders repoURL "/cluster-labels", a broken Application, not an
     error. Any template asking for the mode gets the check. */}}
{{- if eq $oci "true" -}}
  {{- if not .Values.autoshiftOciRepo -}}
    {{- fail "\n\nOCI mode needs autoshiftOciRepo.\nSet it to the registry holding the policy charts, for example:\n\n  autoshiftOciRepo: oci://quay.io/autoshift/policies\n  autoshiftOciVersion: 0.0.5\n\n" -}}
  {{- end -}}
  {{- if not .Values.autoshiftOciVersion -}}
    {{- fail "\n\nOCI mode needs autoshiftOciVersion.\nPin the release; Argo CD does not reliably track a mutable tag, and no 'latest' tag is published.\nIn an ArgoCD Application, inject it from the Application's own source:\n\n  parameters:\n    - name: autoshiftOciVersion\n      value: $ARGOCD_APP_SOURCE_TARGET_REVISION\n\n" -}}
  {{- end -}}
{{- end -}}
{{- $oci -}}
{{- end -}}

{{/*
autoshift.deprecations renders a comment naming values that still work but should be replaced.
Helm has no warning mechanism, and Argo CD renders with `helm template`, so NOTES.txt is never
seen. A comment shows in `helm template` output and in the Argo CD manifest diff; the caller also
stamps an annotation, which shows in `oc get`.
*/}}
{{- define "autoshift.deprecations" -}}
{{- if hasKey .Values "autoshiftOciRegistry" -}}
# DEPRECATED: autoshiftOciRegistry still works and still decides the deploy mode, but
# autoshiftOciRepo alone now selects OCI. Delete autoshiftOciRegistry from your values.
{{ end -}}
{{- end -}}

{{/*
autoshift.deprecationAnnotation is the same notice in a form `oc get` will show. Empty when there
is nothing to say, so the annotation is absent rather than blank.
*/}}
{{- define "autoshift.deprecationAnnotation" -}}
{{- if hasKey .Values "autoshiftOciRegistry" -}}
autoshift.io/deprecated-values: "autoshiftOciRegistry is deprecated; autoshiftOciRepo alone selects OCI mode"
{{- end -}}
{{- end -}}
