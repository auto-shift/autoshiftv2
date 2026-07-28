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
