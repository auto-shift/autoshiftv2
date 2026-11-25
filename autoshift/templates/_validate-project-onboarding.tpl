{{/*
Validate config.projectOnboarding, the input to policies/stable/project-onboarding.

This is the only validation that input gets: the policy templates assume what passes here, so every
rule the policy depends on lives in this file.

Config is checked the way rendered-config merges it, the cluster's own over its clusterset's over
the global config, so a cluster can override part of a project its clusterset declares. A clusters
entry names its clusterset only through config.clusterSet. Without it the clusterset is whatever the
ManagedCluster carries at runtime, so the merged view is incomplete and the checks for required
fields are skipped rather than guessed. Format and type checks still run, and any empty value the
cluster sets is refused, because the merge could fill it from the unknown clusterset.
*/}}
{{- define "autoshift.validate-project-onboarding" -}}
{{- $errors := list }}
{{- $global := (.Values.config | default dict) }}
{{- $targets := list }}
{{- $clusterSets := dict }}
{{- range $bucket := list "hubClusterSets" "managedClusterSets" }}
  {{- range $name, $cs := (index $.Values $bucket | default dict) }}
    {{- $_ := set $clusterSets $name $cs }}
    {{- $targets = append $targets (dict "where" (printf "%s.%s" $bucket $name) "layers" (list ($cs.config | default dict) $global) "labels" ($cs.labels | default dict) "complete" true) }}
  {{- end }}
{{- end }}
{{- range $name, $cluster := (.Values.clusters | default dict) }}
  {{- $csName := dig "config" "clusterSet" "" $cluster }}
  {{- $cs := (index $clusterSets $csName | default dict) }}
  {{- $labels := merge (dict) ($cluster.labels | default dict) ($cs.labels | default dict) }}
  {{- $targets = append $targets (dict "where" (printf "clusters.%s" $name) "layers" (list ($cluster.config | default dict) ($cs.config | default dict) $global) "labels" $labels "complete" (hasKey $clusterSets $csName)) }}
{{- end }}
{{- if not $targets }}
  {{- $targets = list (dict "where" "config" "layers" (list $global) "labels" dict "complete" true) }}
{{- end }}

{{- $dns1123Label := "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" }}
{{- $dnsSubdomain := "^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" }}
{{- $labelName := "^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$" }}
{{- $labelValue := "^([A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?)?$" }}
{{- $reservedNamespace := "^(default|openshift|kube-.*|openshift-.*|open-cluster-management.*)$" }}
{{- $reservedLabelPrefix := "^((.*\\.)?autoshift\\.io|k8s\\.ovn\\.org|kubernetes\\.io|k8s\\.io|openshift\\.io)$" }}
{{- /* RBAC role names are path segments with no length limit; braces and whitespace are refused too, because the name is rendered into a hub template */}}
{{- $roleName := "^[^/%{}\\s]+$" }}
{{- $subjectName := "^[^/%{}\\s]([^/%{}]*[^/%{}\\s])?$" }}
{{- $projectKeys := list "displayName" "description" "labels" "roleBindings" "state" }}
{{- $bindingKeys := list "clusterRole" "role" "groups" "users" "serviceAccounts" "state" }}
{{- $serviceAccountKeys := list "name" "namespace" }}
{{- $states := list "present" "absent" }}

{{- range $t := $targets }}
  {{- $merged := dict }}
  {{- $typesOk := true }}
  {{- range $layer := $t.layers }}
    {{- $po := index $layer "projectOnboarding" }}
    {{- if not (kindIs "invalid" $po) }}
      {{- if not (kindIs "map" $po) }}
        {{- $errors = append $errors (printf "%s: config.projectOnboarding must be a map" $t.where) }}
        {{- $typesOk = false }}
      {{- else if not (or (kindIs "invalid" (index $po "projects")) (kindIs "map" (index $po "projects"))) }}
        {{- $errors = append $errors (printf "%s: config.projectOnboarding.projects must be a map keyed by namespace name" $t.where) }}
        {{- $typesOk = false }}
      {{- else }}
        {{- $merged = merge $merged (deepCopy $po) }}
      {{- end }}
    {{- end }}
  {{- end }}
  {{- if not $typesOk }}{{- continue }}{{- end }}
  {{- /* merge fills an empty value from a lower layer, so an empty value cannot clear an inherited one */}}
  {{- range $i, $layer := $t.layers }}
    {{- $paths := list }}
    {{- range $name, $p := (dig "projectOnboarding" "projects" (dict) $layer | default dict) }}
      {{- if not (kindIs "map" $p) }}{{- continue }}{{- end }}
      {{- range $f := list "displayName" "description" "state" }}
        {{- if hasKey $p $f }}{{- $paths = append $paths (list $name $f) }}{{- end }}
      {{- end }}
      {{- if kindIs "map" $p.labels }}
        {{- range $key, $_ := $p.labels }}{{- $paths = append $paths (list $name "labels" $key) }}{{- end }}
      {{- end }}
      {{- if kindIs "map" $p.roleBindings }}
        {{- range $bname, $rb := $p.roleBindings }}
          {{- if not (kindIs "map" $rb) }}{{- continue }}{{- end }}
          {{- range $f := $bindingKeys }}
            {{- if hasKey $rb $f }}{{- $paths = append $paths (list $name "roleBindings" $bname $f) }}{{- end }}
          {{- end }}
        {{- end }}
      {{- end }}
    {{- end }}
    {{- range $path := $paths }}
      {{- $full := concat (list "projectOnboarding" "projects") $path }}
      {{- if include "autoshift.project-onboarding.non-empty-at" (dict "root" $layer "path" $full) }}
        {{- continue }}
      {{- end }}
      {{- if and (eq $i 0) (not $t.complete) }}
        {{- $errors = append $errors (printf "%s: config.%s is empty, and this cluster sets no config.clusterSet, so the clusterset it inherits from is known only at runtime and the merge could fill the empty value from it; remove the key, or set config.clusterSet" $t.where (join "." $full)) }}
        {{- continue }}
      {{- end }}
      {{- range $lower := slice $t.layers (add1 $i) }}
        {{- if include "autoshift.project-onboarding.non-empty-at" (dict "root" $lower "path" $full) }}
          {{- $errors = append $errors (printf "%s: config.%s is empty but a lower layer sets it, and the merge keeps the inherited value; an empty value cannot clear it, so remove the entry from the lower layer instead" $t.where (join "." $full)) }}
          {{- break }}
        {{- end }}
      {{- end }}
    {{- end }}
  {{- end }}
  {{- range $key, $_ := $merged }}
    {{- if ne $key "projects" }}
      {{- $errors = append $errors (printf "%s: config.projectOnboarding.%s is not a recognized field (valid: projects)" $t.where $key) }}
    {{- end }}
  {{- end }}
  {{- $projects := (index $merged "projects" | default dict) }}
  {{- if and $t.complete (eq (toString (index $t.labels "project-onboarding" | default "")) "true") (not $projects) }}
    {{- $errors = append $errors (printf "%s: project-onboarding is 'true' but config.projectOnboarding.projects is empty" $t.where) }}
  {{- end }}

  {{- range $name, $p := $projects }}
    {{- $at := printf "%s: config.projectOnboarding.projects.%s" $t.where $name }}
    {{- if or (gt (len $name) 63) (not (regexMatch $dns1123Label $name)) }}
      {{- $errors = append $errors (printf "%s: the project name must be a DNS-1123 label (lowercase alphanumerics and '-', 63 characters or fewer)" $at) }}
    {{- else if regexMatch $reservedNamespace $name }}
      {{- $errors = append $errors (printf "%s: '%s' is a reserved platform namespace (default, openshift, kube-*, openshift-*, open-cluster-management*)" $at $name) }}
    {{- end }}
    {{- if not (kindIs "map" $p) }}
      {{- $errors = append $errors (printf "%s must be a map" $at) }}
      {{- continue }}
    {{- end }}
    {{- range $key, $_ := $p }}
      {{- if not (has $key $projectKeys) }}
        {{- $errors = append $errors (printf "%s.%s is not a recognized field (valid: %s)" $at $key (join ", " $projectKeys)) }}
      {{- end }}
    {{- end }}

    {{- range $field := list "displayName" "description" }}
      {{- $v := index $p $field }}
      {{- if not (kindIs "invalid" $v) }}
        {{- if not (kindIs "string" $v) }}
          {{- $errors = append $errors (printf "%s.%s must be a string" $at $field) }}
        {{- else if or (contains "{{" $v) (contains "}}" $v) }}
          {{- $errors = append $errors (printf "%s.%s must not contain '{{' or '}}'" $at $field) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{- $labels := index $p "labels" }}
    {{- if not (or (kindIs "invalid" $labels) (kindIs "map" $labels)) }}
      {{- $errors = append $errors (printf "%s.labels must be a map" $at) }}
    {{- else }}
      {{- range $key, $value := ($labels | default dict) }}
        {{- $parts := splitList "/" $key }}
        {{- $prefix := ternary (first $parts) "" (eq (len $parts) 2) }}
        {{- $lname := last $parts }}
        {{- if or (gt (len $parts) 2) (and (eq (len $parts) 2) (or (gt (len $prefix) 253) (not (regexMatch $dnsSubdomain $prefix)))) (gt (len $lname) 63) (not (regexMatch $labelName $lname)) }}
          {{- $errors = append $errors (printf "%s.labels: '%s' is not a valid label key" $at $key) }}
        {{- else if and $prefix (regexMatch $reservedLabelPrefix $prefix) }}
          {{- $errors = append $errors (printf "%s.labels: '%s' uses the reserved prefix '%s/' (autoshift.io, k8s.ovn.org, kubernetes.io, k8s.io, openshift.io)" $at $key $prefix) }}
        {{- end }}
        {{- if not (kindIs "string" $value) }}
          {{- $errors = append $errors (printf "%s.labels.%s must be a string; quote it, as in '%v'" $at $key $value) }}
        {{- else if or (gt (len $value) 63) (not (regexMatch $labelValue $value)) }}
          {{- $errors = append $errors (printf "%s.labels.%s: '%s' is not a valid label value" $at $key $value) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{- $projectAbsent := false }}
    {{- $state := index $p "state" }}
    {{- if not (kindIs "invalid" $state) }}
      {{- if not (and (kindIs "string" $state) (has $state $states)) }}
        {{- $errors = append $errors (printf "%s.state must be 'present' or 'absent'" $at) }}
      {{- else if eq $state "absent" }}
        {{- $projectAbsent = true }}
      {{- end }}
    {{- end }}

    {{- $bindings := index $p "roleBindings" }}
    {{- if not (or (kindIs "invalid" $bindings) (kindIs "map" $bindings)) }}
      {{- $errors = append $errors (printf "%s.roleBindings must be a map keyed by binding name" $at) }}
      {{- continue }}
    {{- end }}
    {{- range $bname, $rb := ($bindings | default dict) }}
      {{- $bat := printf "%s.roleBindings.%s" $at $bname }}
      {{- if or (gt (len $bname) 63) (not (regexMatch $dns1123Label $bname)) }}
        {{- $errors = append $errors (printf "%s: the binding name must be a DNS-1123 label" $bat) }}
      {{- end }}
      {{- if not (kindIs "map" $rb) }}
        {{- $errors = append $errors (printf "%s must be a map" $bat) }}
        {{- continue }}
      {{- end }}
      {{- range $key, $_ := $rb }}
        {{- if not (has $key $bindingKeys) }}
          {{- $errors = append $errors (printf "%s.%s is not a recognized field (valid: %s)" $bat $key (join ", " $bindingKeys)) }}
        {{- end }}
      {{- end }}
      {{- $bindingAbsent := $projectAbsent }}
      {{- $bstate := index $rb "state" }}
      {{- if not (kindIs "invalid" $bstate) }}
        {{- if not (and (kindIs "string" $bstate) (has $bstate $states)) }}
          {{- $errors = append $errors (printf "%s.state must be 'present' or 'absent'" $bat) }}
        {{- else if eq $bstate "absent" }}
          {{- $bindingAbsent = true }}
        {{- end }}
      {{- end }}
      {{- $roleRefs := 0 }}
      {{- range $kind := list "clusterRole" "role" }}
        {{- $role := index $rb $kind }}
        {{- if kindIs "invalid" $role }}
          {{- continue }}
        {{- end }}
        {{- if $role }}
          {{- $roleRefs = add1 $roleRefs }}
        {{- end }}
        {{- if or (not (kindIs "string" $role)) (not (regexMatch $roleName (toString $role))) (has (toString $role) (list "." "..")) }}
          {{- $errors = append $errors (printf "%s.%s is not a valid %s name (non-empty, not '.' or '..', no '/', '%%', '{', '}' or whitespace)" $bat $kind (ternary "ClusterRole" "Role" (eq $kind "clusterRole"))) }}
        {{- end }}
      {{- end }}
      {{- if gt $roleRefs 1 }}
        {{- $errors = append $errors (printf "%s sets both clusterRole and role; set exactly one. A lower layer that sets the other cannot be cleared by the merge, so change it in the layer that declares it" $bat) }}
      {{- else if and $t.complete (not $bindingAbsent) (eq $roleRefs 0) }}
        {{- $errors = append $errors (printf "%s needs clusterRole, the name of an existing ClusterRole such as admin, edit or view, or role, the name of an existing Role in the project" $bat) }}
      {{- end }}
      {{- $subjects := 0 }}
      {{- range $kind := list "groups" "users" }}
        {{- $names := index $rb $kind }}
        {{- if kindIs "invalid" $names }}
          {{- continue }}
        {{- end }}
        {{- if not (kindIs "slice" $names) }}
          {{- $errors = append $errors (printf "%s.%s must be a list of names" $bat $kind) }}
          {{- $subjects = add1 $subjects }}{{/* the type error is the one to report, not a missing subject */}}
          {{- continue }}
        {{- end }}
        {{- range $i, $n := $names }}
          {{- $subjects = add1 $subjects }}
          {{- if or (not (kindIs "string" $n)) (gt (len (toString $n)) 253) (not (regexMatch $subjectName (toString $n))) (has (toString $n) (list "." "..")) }}
            {{- $errors = append $errors (printf "%s.%s[%d]: '%v' is not a valid name (non-empty, no '/', '%%', '{' or '}')" $bat $kind $i $n) }}
          {{- else if hasPrefix "system:" $n }}
            {{- $errors = append $errors (printf "%s.%s[%d]: '%s' is reserved by Kubernetes; system: names cannot be bound" $bat $kind $i $n) }}
          {{- end }}
        {{- end }}
      {{- end }}
      {{- $sas := index $rb "serviceAccounts" }}
      {{- if not (kindIs "invalid" $sas) }}
        {{- if not (kindIs "slice" $sas) }}
          {{- $errors = append $errors (printf "%s.serviceAccounts must be a list of {name, namespace} entries" $bat) }}
          {{- $subjects = add1 $subjects }}
        {{- else }}
          {{- range $i, $sa := $sas }}
            {{- $subjects = add1 $subjects }}
            {{- $sat := printf "%s.serviceAccounts[%d]" $bat $i }}
            {{- if not (kindIs "map" $sa) }}
              {{- $errors = append $errors (printf "%s must be a map with name and an optional namespace" $sat) }}
              {{- continue }}
            {{- end }}
            {{- range $key, $_ := $sa }}
              {{- if not (has $key $serviceAccountKeys) }}
                {{- $errors = append $errors (printf "%s.%s is not a recognized field (valid: %s)" $sat $key (join ", " $serviceAccountKeys)) }}
              {{- end }}
            {{- end }}
            {{- $saName := index $sa "name" }}
            {{- if or (not (kindIs "string" $saName)) (gt (len (toString $saName)) 253) (not (regexMatch $dnsSubdomain (toString $saName))) }}
              {{- $errors = append $errors (printf "%s.name is required and must be a ServiceAccount name, a DNS-1123 subdomain of 253 characters or fewer" $sat) }}
            {{- end }}
            {{- $saNamespace := index $sa "namespace" }}
            {{- if not (kindIs "invalid" $saNamespace) }}
              {{- if or (not (kindIs "string" $saNamespace)) (gt (len (toString $saNamespace)) 63) (not (regexMatch $dns1123Label (toString $saNamespace))) }}
                {{- $errors = append $errors (printf "%s.namespace must be a Namespace name, a DNS-1123 label; omit it for the project's own Namespace" $sat) }}
              {{- end }}
            {{- end }}
          {{- end }}
        {{- end }}
      {{- end }}
      {{- if and $t.complete (not $bindingAbsent) (eq $subjects 0) }}
        {{- $errors = append $errors (printf "%s needs at least one entry in groups, users or serviceAccounts" $bat) }}
      {{- end }}
    {{- end }}
  {{- end }}
{{- end }}

{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nProject onboarding validation failed (%d errors):\n  - %s\n\nSee policies/stable/project-onboarding/README.md for the configuration reference.\n" (len $errors) (join "\n  - " (uniq $errors))) }}
{{- end }}
{{- end -}}

{{/* Prints "true" when .path walks .root through maps to a non-empty value. */}}
{{- define "autoshift.project-onboarding.non-empty-at" -}}
{{- $cur := .root }}
{{- $found := true }}
{{- range $key := .path }}
  {{- if and $found (kindIs "map" $cur) (hasKey $cur $key) }}
    {{- $cur = index $cur $key }}
  {{- else }}
    {{- $found = false }}
  {{- end }}
{{- end }}
{{- if and $found (not (empty $cur)) }}true{{- end }}
{{- end -}}
