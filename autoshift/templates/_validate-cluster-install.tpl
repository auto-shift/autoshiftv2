{{/*
Validate cluster-install config for clusters with createCluster: 'true'.
Runs at Helm render time to catch config errors before they reach ACM.
*/}}
{{- define "autoshift.validate-cluster-install" -}}
{{- range $clusterName, $cluster := ($.Values.clusters | default dict) }}
  {{- $ci := (dig "config" "clusterInstall" dict $cluster) }}
  {{- if eq (toString ($ci.createCluster | default "")) "true" }}
    {{- $networking := (dig "config" "networking" dict $cluster) }}
    {{- $hosts := (dig "config" "hosts" dict $cluster) }}

    {{/* Required clusterInstall fields */}}
    {{- if not $ci.baseDomain }}
      {{- fail (printf "cluster %s: clusterInstall.baseDomain is required" $clusterName) }}
    {{- end }}
    {{- if not $ci.openshiftVersion }}
      {{- if not $ci.clusterImageSet }}
        {{- fail (printf "cluster %s: clusterInstall.openshiftVersion or clusterImageSet is required" $clusterName) }}
      {{- end }}
    {{- end }}
    {{- if not (or $ci.sshPublicKey $ci.sshPublicKeyRef) }}
      {{- fail (printf "cluster %s: clusterInstall.sshPublicKey or sshPublicKeyRef is required" $clusterName) }}
    {{- end }}

    {{/* Multi-node requires VIPs */}}
    {{- $cpCount := ($ci.controlPlaneAgents | default 3 | int) }}
    {{- if gt $cpCount 1 }}
      {{- if not $ci.apiVip }}
        {{- fail (printf "cluster %s: clusterInstall.apiVip is required for multi-node clusters" $clusterName) }}
      {{- end }}
      {{- if not $ci.ingressVip }}
        {{- fail (printf "cluster %s: clusterInstall.ingressVip is required for multi-node clusters" $clusterName) }}
      {{- end }}
    {{- end }}

    {{/* Required networking fields */}}
    {{- if empty $networking }}
      {{- fail (printf "cluster %s: config.networking is required" $clusterName) }}
    {{- end }}
    {{- if not (dig "clusterNetwork" "cidr" "" $networking) }}
      {{- fail (printf "cluster %s: networking.clusterNetwork.cidr is required" $clusterName) }}
    {{- end }}
    {{- if not (dig "machineNetwork" "cidr" "" $networking) }}
      {{- fail (printf "cluster %s: networking.machineNetwork.cidr is required" $clusterName) }}
    {{- end }}
    {{- if not $networking.serviceNetwork }}
      {{- fail (printf "cluster %s: networking.serviceNetwork is required" $clusterName) }}
    {{- end }}

    {{/* networking.interfaces is required for cluster-install */}}
    {{- $netInterfaces := (dig "interfaces" dict $networking) }}
    {{- if empty $netInterfaces }}
      {{- fail (printf "cluster %s: networking.interfaces is required" $clusterName) }}
    {{- end }}

    {{/* Must have at least one host */}}
    {{- if empty $hosts }}
      {{- fail (printf "cluster %s: config.hosts is required (at least one host)" $clusterName) }}
    {{- end }}

    {{/* Valid ipv4 modes */}}
    {{- $validIpv4 := list "disabled" "dhcp" "static" }}
    {{/* Valid ipv6 modes */}}
    {{- $validIpv6 := list "disabled" "dhcp" "autoconf" "static" }}

    {{/* Validate each interface */}}
    {{- range $ifaceId, $iface := $netInterfaces }}
      {{- if not $iface.type }}
        {{- fail (printf "cluster %s interface %s: type is required (bond, vlan, ethernet)" $clusterName $ifaceId) }}
      {{- end }}
      {{- if not $iface.name }}
        {{- fail (printf "cluster %s interface %s: name is required" $clusterName $ifaceId) }}
      {{- end }}

      {{/* Validate ipv4 mode */}}
      {{- $ipv4Mode := ($iface.ipv4 | default "disabled" | toString) }}
      {{- if not (has $ipv4Mode $validIpv4) }}
        {{- fail (printf "cluster %s interface %s: ipv4 must be one of: disabled, dhcp, static (got: %s)" $clusterName $ifaceId $ipv4Mode) }}
      {{- end }}

      {{/* Validate ipv6 mode */}}
      {{- $ipv6Mode := ($iface.ipv6 | default "disabled" | toString) }}
      {{- if not (has $ipv6Mode $validIpv6) }}
        {{- fail (printf "cluster %s interface %s: ipv6 must be one of: disabled, dhcp, autoconf, static (got: %s)" $clusterName $ifaceId $ipv6Mode) }}
      {{- end }}

      {{/* Bond-specific validation */}}
      {{- if eq (toString $iface.type) "bond" }}
        {{- if not $iface.mode }}
          {{- fail (printf "cluster %s interface %s: mode is required for bond type (e.g., 802.3ad, active-backup)" $clusterName $ifaceId) }}
        {{- end }}
        {{- if not $iface.ports }}
          {{- fail (printf "cluster %s interface %s: ports is required for bond type" $clusterName $ifaceId) }}
        {{- end }}
      {{- end }}

      {{/* VLAN-specific validation */}}
      {{- if eq (toString $iface.type) "vlan" }}
        {{- if not $iface.id }}
          {{- fail (printf "cluster %s interface %s: id is required for vlan type" $clusterName $ifaceId) }}
        {{- end }}
        {{- if not $iface.base }}
          {{- fail (printf "cluster %s interface %s: base is required for vlan type" $clusterName $ifaceId) }}
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
          {{- fail (printf "cluster %s interface %s: ipv4 is 'static' but no host has networking.interfaces.%s.ipv4.addresses" $clusterName $ifaceId $ifaceId) }}
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
          {{- fail (printf "cluster %s interface %s: ipv6 is 'static' but no host has networking.interfaces.%s.ipv6.addresses" $clusterName $ifaceId $ifaceId) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Validate routes */}}
    {{- range $routeId, $route := (dig "routes" dict $networking) }}
      {{- if not $route.destination }}
        {{- fail (printf "cluster %s route %s: destination is required" $clusterName $routeId) }}
      {{- end }}
      {{- if not $route.gateway }}
        {{- fail (printf "cluster %s route %s: gateway is required" $clusterName $routeId) }}
      {{- end }}
      {{- if not $route.interface }}
        {{- fail (printf "cluster %s route %s: interface is required" $clusterName $routeId) }}
      {{- end }}
    {{- end }}

    {{/* Validate each host */}}
    {{- range $hostname, $host := $hosts }}
      {{- if not $host.bmcIP }}
        {{- fail (printf "cluster %s host %s: bmcIP is required" $clusterName $hostname) }}
      {{- end }}
      {{- if not $host.bmcPrefix }}
        {{- fail (printf "cluster %s host %s: bmcPrefix is required" $clusterName $hostname) }}
      {{- end }}
      {{- if not $host.bootMACAddress }}
        {{- fail (printf "cluster %s host %s: bootMACAddress is required" $clusterName $hostname) }}
      {{- end }}
      {{- if not $host.interfaces }}
        {{- fail (printf "cluster %s host %s: interfaces is required (at least one)" $clusterName $hostname) }}
      {{- end }}

      {{/* Validate per-host networking references topology interfaces */}}
      {{- range $ifaceId, $override := (dig "networking" "interfaces" dict $host) }}
        {{- if not (hasKey $netInterfaces $ifaceId) }}
          {{- fail (printf "cluster %s host %s: networking.interfaces.%s references unknown topology interface" $clusterName $hostname $ifaceId) }}
        {{- end }}
        {{/* Validate per-host ipv4 addresses */}}
        {{- range $idx, $addr := (dig "ipv4" "addresses" list $override) }}
          {{- if not (index $addr "ip") }}
            {{- fail (printf "cluster %s host %s interface %s: ipv4.addresses[%d].ip is required" $clusterName $hostname $ifaceId $idx) }}
          {{- end }}
          {{- if not (index $addr "prefixLength") }}
            {{- fail (printf "cluster %s host %s interface %s: ipv4.addresses[%d].prefixLength is required" $clusterName $hostname $ifaceId $idx) }}
          {{- end }}
        {{- end }}
        {{/* Validate per-host ipv6 addresses */}}
        {{- range $idx, $addr := (dig "ipv6" "addresses" list $override) }}
          {{- if not (index $addr "ip") }}
            {{- fail (printf "cluster %s host %s interface %s: ipv6.addresses[%d].ip is required" $clusterName $hostname $ifaceId $idx) }}
          {{- end }}
          {{- if not (index $addr "prefixLength") }}
            {{- fail (printf "cluster %s host %s interface %s: ipv6.addresses[%d].prefixLength is required" $clusterName $hostname $ifaceId $idx) }}
          {{- end }}
        {{- end }}
      {{- end }}

      {{/* Validate per-host routes */}}
      {{- range $routeId, $route := (dig "networking" "routes" dict $host) }}
        {{- if not $route.destination }}
          {{- fail (printf "cluster %s host %s route %s: destination is required" $clusterName $hostname $routeId) }}
        {{- end }}
        {{- if not $route.gateway }}
          {{- fail (printf "cluster %s host %s route %s: gateway is required" $clusterName $hostname $routeId) }}
        {{- end }}
        {{- if not $route.interface }}
          {{- fail (printf "cluster %s host %s route %s: interface is required" $clusterName $hostname $routeId) }}
        {{- end }}
      {{- end }}
    {{- end }}

  {{- end }}
{{- end }}
{{- end -}}
