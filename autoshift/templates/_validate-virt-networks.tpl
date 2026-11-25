{{/*
Validate config.networking.cudn, the input to policy-virtualization-networks.

Checked the way rendered-config merges config: the cluster's own over its clusterset's over the
global config. cudn is a map, so its keys merge across layers; vlans is a list, so the highest layer
that sets it wins whole. A clusters entry names its clusterset only through config.clusterSet.
Without it the clusterset is known only at runtime, so the checks that need the merged view (a
physicalNetworkName on every entry, VLANs present when virt-networks is 'true') are skipped there.
Format and type checks always run.

The limits follow the ClusterUserDefinedNetwork API: physicalNetworkName 1-253 characters without
',' or ':', mtu 576-65536, VLAN id 1-4094.
*/}}
{{- define "autoshift.validate-virt-networks" -}}
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

{{- $dns1123Label := "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" }}
{{- $dnsSubdomain := "^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" }}
{{- $labelName := "^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$" }}
{{- $cudnKeys := list "physicalNetworkName" "mtu" "vlans" }}
{{- $vlanKeys := list "id" "name" "selectorKey" "physicalNetworkName" "mtu" }}

{{- range $t := $targets }}
  {{- /* Merge the layers: the first layer that sets a key wins, as the rendered-config merge does. */ -}}
  {{- $physNet := "" }}
  {{- $vlans := list }}
  {{- $vlansSet := false }}
  {{- $typesOk := true }}
  {{- range $layer := $t.layers }}
    {{- $cudn := dig "networking" "cudn" nil $layer }}
    {{- if kindIs "invalid" $cudn }}
      {{- continue }}
    {{- end }}
    {{- if not (kindIs "map" $cudn) }}
      {{- $errors = append $errors (printf "%s: config.networking.cudn must be a map" $t.where) }}
      {{- $typesOk = false }}
      {{- continue }}
    {{- end }}
    {{- range $key, $_ := $cudn }}
      {{- if not (has $key $cudnKeys) }}
        {{- $errors = append $errors (printf "%s: config.networking.cudn.%s is not a recognized field (valid: %s)" $t.where $key (join ", " $cudnKeys)) }}
      {{- end }}
    {{- end }}
    {{- if and (not $physNet) (index $cudn "physicalNetworkName") }}
      {{- $physNet = toString (index $cudn "physicalNetworkName") }}
    {{- end }}
    {{- $layerVlans := index $cudn "vlans" }}
    {{- if not (kindIs "invalid" $layerVlans) }}
      {{- if not (kindIs "slice" $layerVlans) }}
        {{- $errors = append $errors (printf "%s: config.networking.cudn.vlans must be a list" $t.where) }}
        {{- $typesOk = false }}
      {{- else if and (not $vlansSet) $layerVlans }}
        {{- $vlans = $layerVlans }}
        {{- $vlansSet = true }}
      {{- end }}
    {{- end }}
  {{- end }}
  {{- if not $typesOk }}
    {{- continue }}
  {{- end }}

  {{- $on := eq (toString (index $t.labels "virt-networks" | default "")) "true" }}
  {{- if and $t.complete $on (not $vlans) }}
    {{- $errors = append $errors (printf "%s: virt-networks is 'true' but config.networking.cudn.vlans is empty" $t.where) }}
  {{- end }}

  {{- range $layer := $t.layers }}
    {{- $cudn := dig "networking" "cudn" nil $layer }}
    {{- if and (kindIs "map" $cudn) (hasKey $cudn "physicalNetworkName") }}
      {{- $p := toString (index $cudn "physicalNetworkName") }}
      {{- if or (eq (len $p) 0) (gt (len $p) 253) (contains "," $p) (contains ":" $p) }}
        {{- $errors = append $errors (printf "%s: config.networking.cudn.physicalNetworkName must be 1-253 characters without ',' or ':'" $t.where) }}
      {{- end }}
    {{- end }}
    {{- if and (kindIs "map" $cudn) (hasKey $cudn "mtu") }}
      {{- $m := toString (index $cudn "mtu") }}
      {{- if or (not (regexMatch "^[0-9]+$" $m)) (lt (atoi $m) 576) (gt (atoi $m) 65536) }}
        {{- $errors = append $errors (printf "%s: config.networking.cudn.mtu must be an integer from 576 to 65536" $t.where) }}
      {{- end }}
    {{- end }}
  {{- end }}

  {{- $names := dict }}
  {{- range $i, $v := $vlans }}
    {{- $at := printf "%s: config.networking.cudn.vlans[%d]" $t.where $i }}
    {{- if not (kindIs "map" $v) }}
      {{- $errors = append $errors (printf "%s must be a map" $at) }}
      {{- continue }}
    {{- end }}
    {{- range $key, $_ := $v }}
      {{- if not (has $key $vlanKeys) }}
        {{- $errors = append $errors (printf "%s.%s is not a recognized field (valid: %s)" $at $key (join ", " $vlanKeys)) }}
      {{- end }}
    {{- end }}
    {{- $id := 0 }}
    {{- if hasKey $v "id" }}
      {{- $idStr := toString (index $v "id") }}
      {{- if or (not (regexMatch "^[0-9]+$" $idStr)) (lt (atoi $idStr) 1) (gt (atoi $idStr) 4094) }}
        {{- $errors = append $errors (printf "%s.id must be a VLAN ID from 1 to 4094" $at) }}
      {{- else }}
        {{- $id = atoi $idStr }}
      {{- end }}
    {{- end }}
    {{- $name := toString (index $v "name" | default "") }}
    {{- if and (not $name) (gt $id 0) }}
      {{- $name = printf "vlan-%d" $id }}
    {{- end }}
    {{- if not $name }}
      {{- if not (hasKey $v "id") }}
        {{- $errors = append $errors (printf "%s needs a name when it has no id" $at) }}
      {{- end }}
    {{- else }}
      {{- if or (gt (len $name) 63) (not (regexMatch $dns1123Label $name)) }}
        {{- $errors = append $errors (printf "%s.name '%s' must be a DNS-1123 label" $at $name) }}
      {{- else if hasKey $names $name }}
        {{- $errors = append $errors (printf "%s.name '%s' is used by another entry" $at $name) }}
      {{- end }}
      {{- $_ := set $names $name true }}
    {{- end }}
    {{- if hasKey $v "selectorKey" }}
      {{- $key := toString (index $v "selectorKey") }}
      {{- $parts := splitList "/" $key }}
      {{- $prefix := ternary (first $parts) "" (eq (len $parts) 2) }}
      {{- $lname := last $parts }}
      {{- if or (gt (len $parts) 2) (and (eq (len $parts) 2) (or (gt (len $prefix) 253) (not (regexMatch $dnsSubdomain $prefix)))) (gt (len $lname) 63) (not (regexMatch $labelName $lname)) }}
        {{- $errors = append $errors (printf "%s.selectorKey '%s' is not a valid label key" $at $key) }}
      {{- end }}
    {{- end }}
    {{- if hasKey $v "physicalNetworkName" }}
      {{- $p := toString (index $v "physicalNetworkName") }}
      {{- if or (eq (len $p) 0) (gt (len $p) 253) (contains "," $p) (contains ":" $p) }}
        {{- $errors = append $errors (printf "%s.physicalNetworkName must be 1-253 characters without ',' or ':'" $at) }}
      {{- end }}
    {{- else if and $t.complete (not $physNet) }}
      {{- $errors = append $errors (printf "%s needs physicalNetworkName, on the entry or on config.networking.cudn" $at) }}
    {{- end }}
    {{- if hasKey $v "mtu" }}
      {{- $m := toString (index $v "mtu") }}
      {{- if or (not (regexMatch "^[0-9]+$" $m)) (lt (atoi $m) 576) (gt (atoi $m) 65536) }}
        {{- $errors = append $errors (printf "%s.mtu must be an integer from 576 to 65536" $at) }}
      {{- end }}
    {{- end }}
  {{- end }}
{{- end }}

{{- if gt (len $errors) 0 }}
  {{- fail (printf "\n\nVirtual machine network validation failed (%d errors):\n  - %s\n\nSee policies/stable/openshift-virtualization/README.md for the configuration reference.\n" (len $errors) (join "\n  - " (uniq $errors))) }}
{{- end }}
{{- end -}}
