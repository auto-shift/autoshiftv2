{{/*
Validate naming length constraints to prevent ACM policy name limit violations.

ACM enforces: len(policy_namespace) + len(policy_name) <= 62
AutoShift enforces:
  - policy namespace (policies-{Release.Name}): <= 20 chars
      → Release.Name must be <= 11 chars
  - clusterset/cluster keys (values file keys): <= 20 chars each
  - clusterset names with versioning suffix: <= 63 chars
      (Kubernetes label value limit for cluster.open-cluster-management.io/clusterset)
  - policy names: <= 40 chars
  - config keys: must kebabcase to a valid RFC1123 name, and must not collide with each
      other, because each becomes a ConfigMap <cluster>.rendered-config-<key>
*/}}

{{/*
Compute the clusterSet suffix for versioned deployments.
Returns "-{sanitized-version-tag}" when versionedClusterSets is true, empty string otherwise.
This is the single source of truth — autoshift-app-set.yaml and the configmap templates use this.
*/}}
{{- define "autoshift.clusterSetSuffix" -}}
{{- if .Values.versionedClusterSets -}}
  {{- $versionTag := "" -}}
  {{- if eq (include "autoshift.ociMode" .) "true" -}}
    {{- $versionTag = .Values.autoshiftOciVersion -}}
  {{- else -}}
    {{- $versionTag = .Values.autoshiftGitBranchTag | default "main" -}}
  {{- end -}}
  {{- printf "-%s" ($versionTag | replace "." "-" | replace "/" "-" | lower) -}}
{{- end -}}
{{- end -}}

{{/*
Validate that every config key can become a Kubernetes object name.

Each top-level config key is emitted as a per-component ConfigMap named
<cluster>.rendered-config-<key|kebabcase> by policies/stable/cluster-config-maps. A key the API
server will not accept (a dot, or a leading/trailing hyphen — kebabcase handles case and underscores) produces an invalid name, and the failure surfaces as a confusing apply error on the MANAGED
cluster rather than here. Catching it at render time keeps it next to the values file that caused it.

Also catches two keys that collapse to the SAME name (certManager and cert-manager, Thing and
thing): both would emit the same ConfigMap and one would silently overwrite the other.

Called with dict "path" (error prefix) "config" (the config dict).
*/}}
{{- define "autoshift.validate-config-keys" -}}
  {{- $path := .path -}}
  {{- $seen := dict -}}
  {{- range $key, $_ := (.config | default dict) -}}
    {{- $dns := $key | kebabcase -}}
    {{- if not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" $dns) }}
{{ printf "%s: config key '%s' cannot be a Kubernetes object name (becomes '%s'). Config keys are emitted as ConfigMaps named <cluster>.rendered-config-<key>; use lowerCamelCase or kebab-case; dots and leading/trailing hyphens are not allowed." $path $key $dns }}
    {{- else if hasKey $seen $dns }}
{{ printf "%s: config keys '%s' and '%s' both become '%s', so they would produce the same ConfigMap <cluster>.rendered-config-%s and one would silently overwrite the other. Keep one spelling." $path (index $seen $dns) $key $dns $dns }}
    {{- else -}}
      {{- $_ := set $seen $dns $key -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "autoshift.validate-naming" -}}
{{- $errors := list }}
{{- $suffix := include "autoshift.clusterSetSuffix" . }}

{{/* Validate Release.Name produces a namespace <= 20 chars.
     helm lint hardcodes "test-release" (21 chars) with no way to override it, so skip it there;
     helm template and a real install still enforce this. */}}
{{- $ns := printf "policies-%s" .Release.Name }}
{{- if and (gt (len $ns) 20) (ne .Release.Name "test-release") }}
  {{- $errors = append $errors (printf "Release name '%s' produces policy namespace '%s' (%d chars, max 20). Shorten the Helm release name to %d chars or fewer." .Release.Name $ns (len $ns) (sub 20 (len "policies-"))) }}
{{- end }}

{{/* Validate hubClusterSets keys <= 20 chars, and key+suffix <= 63 chars when versioning is enabled */}}
{{/* Config keys become ConfigMap names (<cluster>.rendered-config-<key>), so they are a naming
       constraint like the ones above: valid RFC1123 after kebabcase, and unique after it. */}}
{{- range $bucket := (list "hubClusterSets" "managedClusterSets" "clusters") }}
  {{- range $name, $entry := (index $.Values $bucket | default dict) }}
    {{- $keyErr := (include "autoshift.validate-config-keys" (dict "path" (printf "%s.%s" $bucket $name) "config" (($entry).config | default dict))) | trim }}
    {{- if $keyErr }}
      {{- range splitList "\n" $keyErr }}
        {{- $errors = append $errors . }}
      {{- end }}
    {{- end }}
  {{- end }}
{{- end }}
{{- range $name, $_ := .Values.hubClusterSets }}
  {{- if gt (len $name) 20 }}
    {{- $errors = append $errors (printf "hubClusterSets key '%s' is %d chars (max 20)" $name (len $name)) }}
  {{- end }}
  {{- $full := printf "%s%s" $name $suffix }}
  {{- if and $suffix (gt (len $full) 63) }}
    {{- $maxKey := sub 63 (len $suffix) }}
    {{- if gt $maxKey 0 }}
      {{- $errors = append $errors (printf "hubClusterSets key '%s' + version suffix '%s' = '%s' (%d chars, max 63). Shorten the key to %d chars or fewer, or use a shorter branch/tag name." $name $suffix $full (len $full) $maxKey) }}
    {{- else }}
      {{- $errors = append $errors (printf "hubClusterSets key '%s' + version suffix '%s' = '%s' (%d chars, max 63). The branch/tag name is too long — use a shorter branch/tag (max %d chars after sanitization)." $name $suffix $full (len $full) (sub 62 (len $name))) }}
    {{- end }}
  {{- end }}
{{- end }}

{{/* Validate managedClusterSets keys <= 20 chars, and key+suffix <= 63 chars when versioning is enabled */}}
{{- range $name, $_ := .Values.managedClusterSets }}
  {{- if gt (len $name) 20 }}
    {{- $errors = append $errors (printf "managedClusterSets key '%s' is %d chars (max 20)" $name (len $name)) }}
  {{- end }}
  {{- $full := printf "%s%s" $name $suffix }}
  {{- if and $suffix (gt (len $full) 63) }}
    {{- $maxKey := sub 63 (len $suffix) }}
    {{- if gt $maxKey 0 }}
      {{- $errors = append $errors (printf "managedClusterSets key '%s' + version suffix '%s' = '%s' (%d chars, max 63). Shorten the key to %d chars or fewer, or use a shorter branch/tag name." $name $suffix $full (len $full) $maxKey) }}
    {{- else }}
      {{- $errors = append $errors (printf "managedClusterSets key '%s' + version suffix '%s' = '%s' (%d chars, max 63). The branch/tag name is too long — use a shorter branch/tag (max %d chars after sanitization)." $name $suffix $full (len $full) (sub 62 (len $name))) }}
    {{- end }}
  {{- end }}
{{- end }}

{{/* Validate clusters keys <= 20 chars */}}
{{- range $name, $_ := .Values.clusters }}
  {{- if gt (len $name) 20 }}
    {{- $errors = append $errors (printf "clusters key '%s' is %d chars (max 20)" $name (len $name)) }}
  {{- end }}
{{- end }}

{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nNaming validation failed (%d errors):\n  - %s\n\nACM enforces a 62-char combined limit on policy namespace + policy name.\nAutoShift reserves 20 chars for the namespace and 40 for policy names.\nClusterset names with versioning suffix must fit Kubernetes label values (max 63 chars).\n" (len $errors) (join "\n  - " $errors)) }}
{{- end }}
{{- end -}}

{{/*
autoshift.validate-gitops enforces the policyGenerator / deploy-mode contract:
  - Git/source mode renders PolicyGenerator dirs live via the CMP, so it REQUIRES policyGenerator:
    true (the ArgoCD repo-server must carry the policy-generator CMP sidecar). Without it the
    PolicyGenerator directories never render and most policies simply never deploy — a hard failure.
  - OCI mode ships prerendered Helm charts and never uses the CMP, so policyGenerator: false is
    RECOMMENDED but not required. Leaving it true only configures a sidecar nothing calls; the
    deploy mode is chosen by autoshiftOciRepo, not by this flag, so deployment is unaffected.
    It is therefore not validated — an OCI deployment that inherits the git default still works.
Effective value = global .Values.policyGenerator, overridden by the self-managed hub clusterset's
config.gitops.policyGenerator.
*/}}
{{- define "autoshift.validate-gitops" -}}
{{- $gitopsCfg := include "autoshift.selfManagedHubGitops" . | fromYaml -}}
{{- $pg := .Values.policyGenerator -}}
{{- if hasKey $gitopsCfg "policyGenerator" -}}
  {{- $pg = (index $gitopsCfg "policyGenerator") -}}
{{- end -}}
{{- if ne (include "autoshift.ociMode" .) "true" -}}
  {{- if not $pg -}}
    {{- fail "\n\nGit/source mode requires policyGenerator: true.\nThe ArgoCD repo-server needs the policy-generator CMP sidecar to render PolicyGenerator dirs from git.\nSet policyGenerator: true in global.yaml, or config.gitops.policyGenerator: true on the self-managed hub clusterset.\n" -}}
  {{- end -}}
{{- end -}}
{{- end -}}
