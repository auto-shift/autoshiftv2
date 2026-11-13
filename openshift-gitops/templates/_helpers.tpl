{{/*
openshift-gitops.validate-gitops — validates the policy-generator CMP choice for the bootstrap ArgoCD.
gitops.policyGenerator.enabled must be a boolean:
  - true  for git/source bootstrap — the repo-server renders PolicyGenerator dirs via the CMP sidecar.
  - false for OCI-only bootstrap — AutoShift ships prerendered Helm charts, so no CMP is needed.
Mirrors the autoshift chart's autoshift.validate-gitops (which enforces the same flag for the running
deployment); here it guards the bootstrap ArgoCD that first renders those policies.
*/}}
{{- define "openshift-gitops.validate-gitops" -}}
{{- $pg := (.Values.gitops.policyGenerator | default dict) -}}
{{- if hasKey $pg "enabled" -}}
  {{- if not (kindIs "bool" $pg.enabled) -}}
    {{- fail (printf "\n\ngitops.policyGenerator.enabled must be a boolean, got %q (%s).\nSet it true for git/source bootstrap (installs the policy-generator CMP sidecar in the repo-server) or false for OCI-only bootstrap (prerendered Helm charts, no CMP).\n" (toString $pg.enabled) (kindOf $pg.enabled)) -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{/*
openshift-gitops.clusterConfigNamespaces — the value of the operator's ARGOCD_CLUSTER_CONFIG_NAMESPACES:
gitops.argoNamespace (the AutoShift infra Argo CD) first, then every namespace in
gitops.clusterScopedNamespaces, de-duplicated and comma-joined. The operator grants cluster scope only to
the instances listed here, so a pre-existing instance (for example the default one in openshift-gitops)
left out of the list loses its cluster-wide permissions when AutoShift sets this variable.
Fails the render unless gitops.clusterScopedNamespaces is a list of strings.
*/}}
{{- define "openshift-gitops.clusterConfigNamespaces" -}}
{{- $namespaces := list (.Values.gitops.argoNamespace | default "openshift-gitops") -}}
{{- $extra := .Values.gitops.clusterScopedNamespaces | default list -}}
{{- if not (kindIs "slice" $extra) -}}
  {{- fail (printf "\n\ngitops.clusterScopedNamespaces must be a list of namespaces, got %q (%s).\nExample: clusterScopedNamespaces: ['openshift-gitops']\n" (toString $extra) (kindOf $extra)) -}}
{{- end -}}
{{- range $ns := $extra -}}
  {{- if not (kindIs "string" $ns) -}}
    {{- fail (printf "\n\ngitops.clusterScopedNamespaces entries must be namespace names, got %q (%s).\n" (toString $ns) (kindOf $ns)) -}}
  {{- end -}}
  {{- if and $ns (not (has $ns $namespaces)) -}}
    {{- $namespaces = append $namespaces $ns -}}
  {{- end -}}
{{- end -}}
{{- join "," $namespaces -}}
{{- end -}}

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
