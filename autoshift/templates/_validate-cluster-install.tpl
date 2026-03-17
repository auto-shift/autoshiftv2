{{/*
Validate cluster-install config for clusters with createCluster: 'true'.
Runs at Helm render time to catch config errors before they reach ACM.
Collects all errors and reports them together.
*/}}
{{- define "autoshift.validate-cluster-install" -}}
{{- range $clusterName, $cluster := ($.Values.clusters | default dict) }}
  {{- $ci := (dig "config" "clusterInstall" dict $cluster) }}
  {{- if eq (toString ($ci.createCluster | default "")) "true" }}
    {{- $networking := (dig "config" "networking" dict $cluster) }}
    {{- $hosts := (dig "config" "hosts" dict $cluster) }}
    {{- $errors := list }}

    {{/* Required clusterInstall fields */}}
    {{- if not $ci.baseDomain }}
      {{- $errors = append $errors (printf "cluster %s: clusterInstall.baseDomain is required" $clusterName) }}
    {{- end }}
    {{- if not $ci.openshiftVersion }}
      {{- if not $ci.clusterImageSet }}
        {{- $errors = append $errors (printf "cluster %s: clusterInstall.openshiftVersion or clusterImageSet is required" $clusterName) }}
      {{- end }}
    {{- end }}
    {{- if not (or $ci.sshPublicKey $ci.sshPublicKeyRef) }}
      {{- $errors = append $errors (printf "cluster %s: clusterInstall.sshPublicKey or sshPublicKeyRef is required" $clusterName) }}
    {{- end }}

    {{/* Multi-node requires VIPs */}}
    {{- $cpCount := ($ci.controlPlaneAgents | default 3 | int) }}
    {{- if gt $cpCount 1 }}
      {{- if not $ci.apiVip }}
        {{- $errors = append $errors (printf "cluster %s: clusterInstall.apiVip is required for multi-node clusters" $clusterName) }}
      {{- end }}
      {{- if not $ci.ingressVip }}
        {{- $errors = append $errors (printf "cluster %s: clusterInstall.ingressVip is required for multi-node clusters" $clusterName) }}
      {{- end }}
    {{- end }}

    {{/* Required networking fields */}}
    {{- if empty $networking }}
      {{- $errors = append $errors (printf "cluster %s: config.networking is required" $clusterName) }}
    {{- else }}
      {{- if not (dig "clusterNetwork" "cidr" "" $networking) }}
        {{- $errors = append $errors (printf "cluster %s: networking.clusterNetwork.cidr is required" $clusterName) }}
      {{- end }}
      {{- if not (dig "machineNetwork" "cidr" "" $networking) }}
        {{- $errors = append $errors (printf "cluster %s: networking.machineNetwork.cidr is required" $clusterName) }}
      {{- end }}
      {{- if not $networking.serviceNetwork }}
        {{- $errors = append $errors (printf "cluster %s: networking.serviceNetwork is required" $clusterName) }}
      {{- end }}
    {{- end }}

    {{/* networking.interfaces is required for cluster-install */}}
    {{- $netInterfaces := (dig "interfaces" dict $networking) }}
    {{- if empty $netInterfaces }}
      {{- $errors = append $errors (printf "cluster %s: networking.interfaces is required" $clusterName) }}
    {{- end }}

    {{/* Validate host count matches topology */}}
    {{- if empty $hosts }}
      {{- $errors = append $errors (printf "cluster %s: config.hosts is required (at least one host)" $clusterName) }}
    {{- else }}
      {{- $hostCount := (len (keys $hosts)) }}
      {{- if lt $hostCount $cpCount }}
        {{- $errors = append $errors (printf "cluster %s: %d hosts defined but controlPlaneAgents requires at least %d" $clusterName $hostCount $cpCount) }}
      {{- end }}
      {{- $workerAgents := ($ci.workerAgents | default 0 | int) }}
      {{- if and (gt $workerAgents 0) (lt $hostCount (add $cpCount $workerAgents | int)) }}
        {{- $errors = append $errors (printf "cluster %s: %d hosts defined but %d required (%d control plane + %d workers)" $clusterName $hostCount (add $cpCount $workerAgents | int) $cpCount $workerAgents) }}
      {{- end }}
      {{- if and (eq $cpCount 1) (gt $hostCount 1) }}
        {{- $errors = append $errors (printf "cluster %s: SNO (controlPlaneAgents: 1) must have exactly 1 host, got %d" $clusterName $hostCount) }}
      {{- end }}
    {{- end }}

    {{/* Valid modes */}}
    {{- $validIpv4 := list "disabled" "dhcp" "static" }}
    {{- $validIpv6 := list "disabled" "dhcp" "autoconf" "static" }}
    {{- $validTypes := list "bond" "vlan" "ethernet" }}

    {{/* Build map of interface names for VLAN base validation */}}
    {{- $ifaceNames := dict }}
    {{- range $ifaceId, $iface := $netInterfaces }}
      {{- if $iface.name }}
        {{- $_ := set $ifaceNames $iface.name $ifaceId }}
      {{- end }}
    {{- end }}

    {{/* Validate each interface */}}
    {{- range $ifaceId, $iface := $netInterfaces }}
      {{- if not $iface.type }}
        {{- $errors = append $errors (printf "cluster %s interface %s: type is required (bond, vlan, ethernet)" $clusterName $ifaceId) }}
      {{- else if not (has (toString $iface.type) $validTypes) }}
        {{- $errors = append $errors (printf "cluster %s interface %s: type must be one of: bond, vlan, ethernet (got: %s)" $clusterName $ifaceId $iface.type) }}
      {{- end }}
      {{- if not $iface.name }}
        {{- $errors = append $errors (printf "cluster %s interface %s: name is required" $clusterName $ifaceId) }}
      {{- end }}

      {{/* Validate ipv4 mode */}}
      {{- $ipv4Mode := ($iface.ipv4 | default "disabled" | toString) }}
      {{- if not (has $ipv4Mode $validIpv4) }}
        {{- $errors = append $errors (printf "cluster %s interface %s: ipv4 must be one of: disabled, dhcp, static (got: %s)" $clusterName $ifaceId $ipv4Mode) }}
      {{- end }}

      {{/* Validate ipv6 mode */}}
      {{- $ipv6Mode := ($iface.ipv6 | default "disabled" | toString) }}
      {{- if not (has $ipv6Mode $validIpv6) }}
        {{- $errors = append $errors (printf "cluster %s interface %s: ipv6 must be one of: disabled, dhcp, autoconf, static (got: %s)" $clusterName $ifaceId $ipv6Mode) }}
      {{- end }}

      {{/* Bond-specific validation */}}
      {{- if eq (toString ($iface.type | default "")) "bond" }}
        {{- if not $iface.mode }}
          {{- $errors = append $errors (printf "cluster %s interface %s: mode is required for bond type (e.g., 802.3ad, active-backup)" $clusterName $ifaceId) }}
        {{- end }}
        {{- if not $iface.ports }}
          {{- $errors = append $errors (printf "cluster %s interface %s: ports is required for bond type" $clusterName $ifaceId) }}
        {{- end }}
      {{- end }}

      {{/* VLAN-specific validation */}}
      {{- if eq (toString ($iface.type | default "")) "vlan" }}
        {{- if not $iface.id }}
          {{- $errors = append $errors (printf "cluster %s interface %s: id is required for vlan type" $clusterName $ifaceId) }}
        {{- end }}
        {{- if not $iface.base }}
          {{- $errors = append $errors (printf "cluster %s interface %s: base is required for vlan type" $clusterName $ifaceId) }}
        {{- else if not (hasKey $ifaceNames (toString $iface.base)) }}
          {{- $errors = append $errors (printf "cluster %s interface %s: base '%s' does not match any interface name in the topology" $clusterName $ifaceId $iface.base) }}
        {{- end }}
      {{- end }}

      {{/* Static ipv4 requires per-host addresses from at least one host */}}
      {{- if eq $ipv4Mode "static" }}
        {{- $hasAddr := false }}
        {{- range $hostname, $host := $hosts }}
          {{- $hostIpv4 := (dig "networking" "interfaces" $ifaceId "ipv4" "addresses" list $host) }}
          {{- if (gt (len $hostIpv4) 0) }}
            {{- $hasAddr = true }}
          {{- end }}
        {{- end }}
        {{- if not $hasAddr }}
          {{- $errors = append $errors (printf "cluster %s interface %s: ipv4 is 'static' but no host has networking.interfaces.%s.ipv4.addresses" $clusterName $ifaceId $ifaceId) }}
        {{- end }}
      {{- end }}

      {{/* Static ipv6 requires per-host addresses from at least one host */}}
      {{- if eq $ipv6Mode "static" }}
        {{- $hasAddr := false }}
        {{- range $hostname, $host := $hosts }}
          {{- $hostIpv6 := (dig "networking" "interfaces" $ifaceId "ipv6" "addresses" list $host) }}
          {{- if (gt (len $hostIpv6) 0) }}
            {{- $hasAddr = true }}
          {{- end }}
        {{- end }}
        {{- if not $hasAddr }}
          {{- $errors = append $errors (printf "cluster %s interface %s: ipv6 is 'static' but no host has networking.interfaces.%s.ipv6.addresses" $clusterName $ifaceId $ifaceId) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Validate routes */}}
    {{- range $routeId, $route := (dig "routes" dict $networking) }}
      {{- if not $route.destination }}
        {{- $errors = append $errors (printf "cluster %s route %s: destination is required" $clusterName $routeId) }}
      {{- end }}
      {{- if not $route.gateway }}
        {{- $errors = append $errors (printf "cluster %s route %s: gateway is required" $clusterName $routeId) }}
      {{- end }}
      {{- if not $route.interface }}
        {{- $errors = append $errors (printf "cluster %s route %s: interface is required" $clusterName $routeId) }}
      {{- end }}
    {{- end }}

    {{/* Validate each host */}}
    {{- range $hostname, $host := $hosts }}
      {{- if not $host.bmcIP }}
        {{- $errors = append $errors (printf "cluster %s host %s: bmcIP is required" $clusterName $hostname) }}
      {{- end }}
      {{- if not $host.bmcPrefix }}
        {{- $errors = append $errors (printf "cluster %s host %s: bmcPrefix is required" $clusterName $hostname) }}
      {{- end }}
      {{- if not $host.bootMACAddress }}
        {{- $errors = append $errors (printf "cluster %s host %s: bootMACAddress is required" $clusterName $hostname) }}
      {{- end }}
      {{- if not $host.interfaces }}
        {{- $errors = append $errors (printf "cluster %s host %s: interfaces is required (at least one)" $clusterName $hostname) }}
      {{- end }}

      {{/* Validate per-host networking references topology interfaces */}}
      {{- range $ifaceId, $override := (dig "networking" "interfaces" dict $host) }}
        {{- if not (hasKey $netInterfaces $ifaceId) }}
          {{- $errors = append $errors (printf "cluster %s host %s: networking.interfaces.%s references unknown topology interface" $clusterName $hostname $ifaceId) }}
        {{- end }}
        {{/* Validate per-host ipv4 addresses */}}
        {{- range $idx, $addr := (dig "ipv4" "addresses" list $override) }}
          {{- if not (index $addr "ip") }}
            {{- $errors = append $errors (printf "cluster %s host %s interface %s: ipv4.addresses[%d].ip is required" $clusterName $hostname $ifaceId $idx) }}
          {{- end }}
          {{- if not (index $addr "prefixLength") }}
            {{- $errors = append $errors (printf "cluster %s host %s interface %s: ipv4.addresses[%d].prefixLength is required" $clusterName $hostname $ifaceId $idx) }}
          {{- end }}
        {{- end }}
        {{/* Validate per-host ipv6 addresses */}}
        {{- range $idx, $addr := (dig "ipv6" "addresses" list $override) }}
          {{- if not (index $addr "ip") }}
            {{- $errors = append $errors (printf "cluster %s host %s interface %s: ipv6.addresses[%d].ip is required" $clusterName $hostname $ifaceId $idx) }}
          {{- end }}
          {{- if not (index $addr "prefixLength") }}
            {{- $errors = append $errors (printf "cluster %s host %s interface %s: ipv6.addresses[%d].prefixLength is required" $clusterName $hostname $ifaceId $idx) }}
          {{- end }}
        {{- end }}
      {{- end }}

      {{/* Validate per-host routes */}}
      {{- range $routeId, $route := (dig "networking" "routes" dict $host) }}
        {{- if not $route.destination }}
          {{- $errors = append $errors (printf "cluster %s host %s route %s: destination is required" $clusterName $hostname $routeId) }}
        {{- end }}
        {{- if not $route.gateway }}
          {{- $errors = append $errors (printf "cluster %s host %s route %s: gateway is required" $clusterName $hostname $routeId) }}
        {{- end }}
        {{- if not $route.interface }}
          {{- $errors = append $errors (printf "cluster %s host %s route %s: interface is required" $clusterName $hostname $routeId) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Fail with all collected errors */}}
    {{- if gt (len $errors) 0 }}
      {{- fail (printf "\n\nCluster-install validation failed for '%s' (%d errors):\n  - %s\n" $clusterName (len $errors) (join "\n  - " $errors)) }}
    {{- end }}

  {{- end }}
{{- end }}
{{- end -}}
