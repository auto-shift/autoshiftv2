{{/*
autoshift.cliImage — the image for the CRD-wait Pod, which only needs oc. Highest priority first:
  1. .Values.image, so `--set image=...` or CLI_IMAGE in the installer still wins outright.
  2. The cluster's own openshift/cli ImageStream, read at install time. That entry is a digest
     against the release payload, so an ImageDigestMirrorSet rewrites it in a disconnected
     deployment and the pull needs no ImageTagMirrorSet.
  3. The in-cluster registry, which does not exist wherever the registry Operator is Removed
     (bare metal and most disconnected installs).
`helm template` has no cluster, so lookup returns empty there and rendering falls through to 3.
*/}}
{{- define "autoshift.cliImage" -}}
{{- if .Values.image -}}
{{- .Values.image -}}
{{- else -}}
{{- $fallback := "image-registry.openshift-image-registry.svc:5000/openshift/cli:latest" -}}
{{- $fromIS := "" -}}
{{- $is := lookup "image.openshift.io/v1" "ImageStream" "openshift" "cli" -}}
{{- if $is -}}
{{- range $t := (dig "spec" "tags" (list) $is) -}}
{{- if eq (dig "name" "" $t) "latest" -}}
{{- $fromIS = (dig "from" "name" "" $t) -}}
{{- end -}}
{{- end -}}
{{- if not $fromIS -}}
{{- range $t := (dig "status" "tags" (list) $is) -}}
{{- if eq (dig "tag" "" $t) "latest" -}}
{{- $items := (dig "items" (list) $t) -}}
{{- if $items -}}
{{- $fromIS = (dig "dockerImageReference" "" (index $items 0)) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if $fromIS }}{{ $fromIS }}{{ else }}{{ $fallback }}{{ end -}}
{{- end -}}
{{- end -}}
