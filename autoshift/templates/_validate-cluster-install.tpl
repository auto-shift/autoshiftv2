{{/*
Validate disconnected config block.
Called with dict "path" (string prefix for errors) "config" (the disconnected dict).
Returns newline-separated error strings (empty string = no errors).
*/}}
{{- define "autoshift.validate-disconnected" -}}
  {{- $path := .path -}}
  {{- $disconnected := .config -}}
  {{- $validDisconnectedKeys := list "mirrorRegistry" "useIDMS" "disableDefaultCatalogs" "catalogs" "osImages" -}}
  {{- $validMirrorRegKeys := list "host" "path" "ca" "caRef" "mirrors" "releaseImage" -}}
  {{- $validMirrorEntryKeys := list "source" "mirror" -}}
  {{- $validCaRefKeys := list "name" "key" "namespace" -}}
  {{- $validCatalogKeys := list "source" "imagePath" "tag" "publisher" "displayName" "updateInterval" -}}
  {{- $validOsImageKeys := list "openshiftVersion" "version" "cpuArchitecture" "url" "rootFSUrl" -}}
  {{- range $key, $_ := $disconnected -}}
    {{- if not (has $key $validDisconnectedKeys) }}
{{ printf "%s: disconnected.%s is not a recognized field (valid: %s)" $path $key (join ", " $validDisconnectedKeys) }}
    {{- end -}}
  {{- end -}}
  {{- $mirrorReg := ($disconnected.mirrorRegistry | default dict) -}}
  {{- range $key, $_ := $mirrorReg -}}
    {{- if not (has $key $validMirrorRegKeys) }}
{{ printf "%s: disconnected.mirrorRegistry.%s is not a recognized field (valid: %s)" $path $key (join ", " $validMirrorRegKeys) }}
    {{- end -}}
  {{- end -}}
  {{- $mirrorEntries := ($mirrorReg.mirrors | default list) -}}
  {{- if gt (len $mirrorEntries) 0 -}}
    {{- if not $mirrorReg.host }}
{{ printf "%s: disconnected.mirrorRegistry.host is required when mirrors are defined" $path }}
    {{- end -}}
    {{- range $idx, $entry := $mirrorEntries -}}
      {{- range $key, $_ := $entry -}}
        {{- if not (has $key $validMirrorEntryKeys) }}
{{ printf "%s: disconnected.mirrorRegistry.mirrors[%d].%s is not a recognized field (valid: %s)" $path $idx $key (join ", " $validMirrorEntryKeys) }}
        {{- end -}}
      {{- end -}}
      {{- if not (index $entry "source") }}
{{ printf "%s: disconnected.mirrorRegistry.mirrors[%d].source is required" $path $idx }}
      {{- end -}}
    {{- end -}}
  {{- end -}}
  {{- $caRef := ($mirrorReg.caRef | default dict) -}}
  {{- if not (empty $caRef) -}}
    {{- range $key, $_ := $caRef -}}
      {{- if not (has $key $validCaRefKeys) }}
{{ printf "%s: disconnected.mirrorRegistry.caRef.%s is not a recognized field (valid: %s)" $path $key (join ", " $validCaRefKeys) }}
      {{- end -}}
    {{- end -}}
    {{- if not (index $caRef "name") }}
{{ printf "%s: disconnected.mirrorRegistry.caRef.name is required" $path }}
    {{- end -}}
    {{- if not (index $caRef "key") }}
{{ printf "%s: disconnected.mirrorRegistry.caRef.key is required" $path }}
    {{- end -}}
  {{- end -}}
  {{- if and (gt (len $mirrorEntries) 0) (not (or $mirrorReg.ca $mirrorReg.caRef)) }}
{{ printf "%s: disconnected.mirrorRegistry.ca or caRef is required when sources are defined" $path }}
  {{- end -}}
  {{- $catalogs := ($disconnected.catalogs | default list) -}}
  {{- if and (gt (len $catalogs) 0) (not $mirrorReg.host) }}
{{ printf "%s: disconnected.mirrorRegistry.host is required when catalogs are defined" $path }}
  {{- end -}}
  {{- range $idx, $catalog := $catalogs -}}
    {{- range $key, $_ := $catalog -}}
      {{- if not (has $key $validCatalogKeys) }}
{{ printf "%s: disconnected.catalogs[%d].%s is not a recognized field (valid: %s)" $path $idx $key (join ", " $validCatalogKeys) }}
      {{- end -}}
    {{- end -}}
    {{- if not (index $catalog "source") }}
{{ printf "%s: disconnected.catalogs[%d].source is required" $path $idx }}
    {{- end -}}
    {{- if not (index $catalog "imagePath") }}
{{ printf "%s: disconnected.catalogs[%d].imagePath is required" $path $idx }}
    {{- end -}}
    {{- if not (index $catalog "tag") }}
{{ printf "%s: disconnected.catalogs[%d].tag is required" $path $idx }}
    {{- end -}}
  {{- end -}}
  {{- range $idx, $img := ($disconnected.osImages | default list) -}}
    {{- range $key, $_ := $img -}}
      {{- if not (has $key $validOsImageKeys) }}
{{ printf "%s: disconnected.osImages[%d].%s is not a recognized field (valid: %s)" $path $idx $key (join ", " $validOsImageKeys) }}
      {{- end -}}
    {{- end -}}
    {{- if not (index $img "openshiftVersion") }}
{{ printf "%s: disconnected.osImages[%d].openshiftVersion is required" $path $idx }}
    {{- end -}}
    {{- if not (index $img "version") }}
{{ printf "%s: disconnected.osImages[%d].version is required (RHCOS version string)" $path $idx }}
    {{- end -}}
    {{- if not (index $img "url") }}
{{ printf "%s: disconnected.osImages[%d].url is required (path to RHCOS live ISO)" $path $idx }}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{/*
Validate workloadPartitioning config block.
Called with dict "path" (string prefix for errors) "config" (the workloadPartitioning dict).
Returns newline-separated error strings (empty string = no errors).
*/}}
{{- define "autoshift.validate-workload-partitioning" -}}
  {{- $path := .path -}}
  {{- $wp := .config -}}
  {{- $validWpKeys := list "reservedCpus" "isolatedCpus" "nodeSelector" "numaTopology" "realTimeKernel" "globallyDisableIrqLoadBalancing" "hugepages" -}}
  {{- $validWpHugepagesKeys := list "defaultSize" "pages" -}}
  {{- $validWpPageKeys := list "size" "count" "node" -}}
  {{- $validNumaTopologies := list "single-numa-node" "best-effort" "restricted" -}}
  {{- range $key, $_ := $wp -}}
    {{- if not (has $key $validWpKeys) }}
{{ printf "%s: workloadPartitioning.%s is not a recognized field (valid: %s)" $path $key (join ", " $validWpKeys) }}
    {{- end -}}
  {{- end -}}
  {{- if and (index $wp "reservedCpus") (not (index $wp "isolatedCpus")) }}
{{ printf "%s: workloadPartitioning.isolatedCpus is required when reservedCpus is set" $path }}
  {{- end -}}
  {{- if and (index $wp "isolatedCpus") (not (index $wp "reservedCpus")) }}
{{ printf "%s: workloadPartitioning.reservedCpus is required when isolatedCpus is set" $path }}
  {{- end -}}
  {{- $wpNuma := (index $wp "numaTopology" | default "") -}}
  {{- if and (not (empty $wpNuma)) (not (has (toString $wpNuma) $validNumaTopologies)) }}
{{ printf "%s: workloadPartitioning.numaTopology must be one of: %s (got: %s)" $path (join ", " $validNumaTopologies) $wpNuma }}
  {{- end -}}
  {{- $wpHugepages := (index $wp "hugepages" | default dict) -}}
  {{- if not (empty $wpHugepages) -}}
    {{- range $key, $_ := $wpHugepages -}}
      {{- if not (has $key $validWpHugepagesKeys) }}
{{ printf "%s: workloadPartitioning.hugepages.%s is not a recognized field (valid: %s)" $path $key (join ", " $validWpHugepagesKeys) }}
      {{- end -}}
    {{- end -}}
    {{- range $idx, $page := ($wpHugepages.pages | default list) -}}
      {{- range $key, $_ := $page -}}
        {{- if not (has $key $validWpPageKeys) }}
{{ printf "%s: workloadPartitioning.hugepages.pages[%d].%s is not a recognized field (valid: %s)" $path $idx $key (join ", " $validWpPageKeys) }}
        {{- end -}}
      {{- end -}}
      {{- if not (index $page "size") }}
{{ printf "%s: workloadPartitioning.hugepages.pages[%d].size is required" $path $idx }}
      {{- end -}}
      {{- if not (index $page "count") }}
{{ printf "%s: workloadPartitioning.hugepages.pages[%d].count is required" $path $idx }}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{/*
Validate cluster-install config for clusters with createCluster: 'true'.
Runs at Helm render time to catch config errors before they reach ACM.
Collects all errors and reports them together.
*/}}
{{- define "autoshift.validate-cluster-install" -}}

{{/* ===== Valid key lists — add new fields here ===== */}}
{{- $validCiKeys := list "createCluster" "platform" "baseDomain" "openshiftVersion" "cpuArch" "clusterImageSet" "openshiftChannel" "controlPlaneAgents" "workerAgents" "apiVip" "ingressVip" "mastersSchedulable" "cpuPartitioning" "fips" "pullSecretRef" "bmcCredentialRef" "bmcEndpoint" "secretSourceNamespace" "sshPublicKey" "sshPublicKeyRef" "ntpSources" "klusterletAddons" }}
{{- $validHostKeys := list "role" "bmcIP" "bmcPrefix" "bmcEndpoint" "bmcCredentialRef" "bootMACAddress" "primaryMac" "rootDeviceHints" "interfaces" "networking" }}
{{- $validNetworkingKeys := list "clusterNetwork" "machineNetwork" "serviceNetwork" "interfaces" "routes" "dns" "ovsBridges" "ovnMappings" "nodeSelector" }}
{{- $validInterfaceKeys := list "type" "name" "state" "mode" "mtu" "mac" "miimon" "ports" "ipv4" "ipv6" "id" "base" }}
{{- $validRouteKeys := list "destination" "gateway" "interface" "metric" "tableId" }}
{{- $validSshRefKeys := list "name" "key" "namespace" }}
{{- $validAwsKeys := list "region" "credentialRef" "sshPrivateKeyRef" "sshPublicKey" "sshKeyRef" "fips" "networkType" "controlPlane" "workers" }}
{{- $validAwsCpKeys := list "instanceType" "rootVolume" }}
{{- $validAwsWorkerKeys := list "replicas" "instanceType" "rootVolume" }}
{{- $validAwsVolumeKeys := list "iops" "size" "type" }}

{{- range $clusterName, $cluster := ($.Values.clusters | default dict) }}
  {{- $ci := (dig "config" "clusterInstall" dict $cluster) }}
  {{- if eq (toString ($ci.createCluster | default "")) "true" }}
    {{- $networking := (dig "config" "networking" dict $cluster) }}
    {{- $hosts := (dig "config" "hosts" dict $cluster) }}
    {{- $errors := list }}
    {{- $path := (printf "cluster %s" $clusterName) }}

    {{/* Validate platform */}}
    {{- $validPlatforms := list "baremetal" "aws" }}
    {{- $platform := ($ci.platform | default "baremetal" | toString) }}
    {{- if not (has $platform $validPlatforms) }}
      {{- $errors = append $errors (printf "%s: clusterInstall.platform must be one of: %s (got: %s)" $path (join ", " $validPlatforms) $platform) }}
    {{- end }}

    {{/* ===== Validate unexpected keys (only sections this policy owns) ===== */}}
    {{- range $key, $_ := $ci }}
      {{- if not (has $key $validCiKeys) }}
        {{- $errors = append $errors (printf "%s: clusterInstall.%s is not a recognized field (valid: %s)" $path $key (join ", " $validCiKeys)) }}
      {{- end }}
    {{- end }}
    {{- range $key, $_ := $networking }}
      {{- if not (has $key $validNetworkingKeys) }}
        {{- $errors = append $errors (printf "%s: networking.%s is not a recognized field (valid: %s)" $path $key (join ", " $validNetworkingKeys)) }}
      {{- end }}
    {{- end }}
    {{- range $hostname, $host := $hosts }}
      {{- range $key, $_ := $host }}
        {{- if not (has $key $validHostKeys) }}
          {{- $errors = append $errors (printf "%s host %s: %s is not a recognized field (valid: %s)" $path $hostname $key (join ", " $validHostKeys)) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Validate disconnected config via shared template */}}
    {{- $disconnected := (dig "config" "disconnected" dict $cluster) }}
    {{- if not (empty $disconnected) }}
      {{- $dcErrorStr := (include "autoshift.validate-disconnected" (dict "path" $path "config" $disconnected)) | trim }}
      {{- if $dcErrorStr }}
        {{- range splitList "\n" $dcErrorStr }}
          {{- $errors = append $errors . }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Required clusterInstall fields */}}
    {{- if not $ci.baseDomain }}
      {{- $errors = append $errors (printf "%s: clusterInstall.baseDomain is required" $path) }}
    {{- end }}
    {{- if not $ci.openshiftVersion }}
      {{- if not $ci.clusterImageSet }}
        {{- $errors = append $errors (printf "%s: clusterInstall.openshiftVersion or clusterImageSet is required" $path) }}
      {{- end }}
    {{- end }}
    {{- if and (eq $platform "baremetal") (not (or $ci.sshPublicKey $ci.sshPublicKeyRef)) }}
      {{- $errors = append $errors (printf "%s: clusterInstall.sshPublicKey or sshPublicKeyRef is required" $path) }}
    {{- end }}

    {{/* Required secret references */}}
    {{- if not $ci.pullSecretRef }}
      {{- $errors = append $errors (printf "%s: clusterInstall.pullSecretRef is required" $path) }}
    {{- end }}

    {{/* ===== AWS-specific validations ===== */}}
    {{- if eq $platform "aws" }}
    {{- $aws := (dig "config" "aws" dict $cluster) }}
    {{- if empty $aws }}
      {{- $errors = append $errors (printf "%s: config.aws is required for platform 'aws'" $path) }}
    {{- else }}
      {{- if not $aws.region }}
        {{- $errors = append $errors (printf "%s: aws.region is required" $path) }}
      {{- end }}
      {{- if not $aws.credentialRef }}
        {{- $errors = append $errors (printf "%s: aws.credentialRef is required" $path) }}
      {{- end }}
      {{- if not $aws.sshPrivateKeyRef }}
        {{- $errors = append $errors (printf "%s: aws.sshPrivateKeyRef is required" $path) }}
      {{- end }}
      {{- range $key, $_ := $aws }}
        {{- if not (has $key $validAwsKeys) }}
          {{- $errors = append $errors (printf "%s: aws.%s is not a recognized field (valid: %s)" $path $key (join ", " $validAwsKeys)) }}
        {{- end }}
      {{- end }}
    {{- end }}
    {{- end }}

    {{/* ===== Baremetal-specific validations ===== */}}
    {{- if eq $platform "baremetal" }}
    {{- if not $ci.bmcCredentialRef }}
      {{- $errors = append $errors (printf "%s: clusterInstall.bmcCredentialRef is required (default BMC credential secret name)" $path) }}
    {{- end }}

    {{/* Multi-node requires VIPs */}}
    {{- $cpCount := ($ci.controlPlaneAgents | default 3 | int) }}
    {{- if gt $cpCount 1 }}
      {{- if not $ci.apiVip }}
        {{- $errors = append $errors (printf "%s: clusterInstall.apiVip is required for multi-node clusters" $path) }}
      {{- end }}
      {{- if not $ci.ingressVip }}
        {{- $errors = append $errors (printf "%s: clusterInstall.ingressVip is required for multi-node clusters" $path) }}
      {{- end }}
    {{- end }}

    {{/* Required networking fields */}}
    {{- if empty $networking }}
      {{- $errors = append $errors (printf "%s: config.networking is required" $path) }}
    {{- else }}
      {{- if not (dig "clusterNetwork" "cidr" "" $networking) }}
        {{- $errors = append $errors (printf "%s: networking.clusterNetwork.cidr is required" $path) }}
      {{- end }}
      {{- if not (dig "machineNetwork" "cidr" "" $networking) }}
        {{- $errors = append $errors (printf "%s: networking.machineNetwork.cidr is required" $path) }}
      {{- end }}
      {{- if not $networking.serviceNetwork }}
        {{- $errors = append $errors (printf "%s: networking.serviceNetwork is required" $path) }}
      {{- end }}
    {{- end }}

    {{- $netInterfaces := (dig "interfaces" dict $networking) }}

    {{/* Validate host count matches topology */}}
    {{- if empty $hosts }}
      {{- $errors = append $errors (printf "%s: config.hosts is required (at least one host)" $path) }}
    {{- else }}
      {{- $hostCount := (len (keys $hosts)) }}
      {{- if lt $hostCount $cpCount }}
        {{- $errors = append $errors (printf "%s: %d hosts defined but controlPlaneAgents requires at least %d" $path $hostCount $cpCount) }}
      {{- end }}
      {{- $workerAgents := ($ci.workerAgents | default 0 | int) }}
      {{- if and (gt $workerAgents 0) (lt $hostCount (add $cpCount $workerAgents | int)) }}
        {{- $errors = append $errors (printf "%s: %d hosts defined but %d required (%d control plane + %d workers)" $path $hostCount (add $cpCount $workerAgents | int) $cpCount $workerAgents) }}
      {{- end }}
      {{- if and (eq $cpCount 1) (gt $hostCount 1) }}
        {{- $errors = append $errors (printf "%s: SNO (controlPlaneAgents: 1) must have exactly 1 host, got %d" $path $hostCount) }}
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

    {{/* Validate interface keys */}}
    {{- range $ifaceId, $iface := $netInterfaces }}
      {{- range $key, $_ := $iface }}
        {{- if not (has $key $validInterfaceKeys) }}
          {{- $errors = append $errors (printf "%s interface %s: %s is not a recognized field (valid: %s)" $path $ifaceId $key (join ", " $validInterfaceKeys)) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Validate route keys */}}
    {{- range $routeId, $route := (dig "routes" dict $networking) }}
      {{- range $key, $_ := $route }}
        {{- if not (has $key $validRouteKeys) }}
          {{- $errors = append $errors (printf "%s route %s: %s is not a recognized field (valid: %s)" $path $routeId $key (join ", " $validRouteKeys)) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Validate sshPublicKeyRef keys */}}
    {{- if not (empty ($ci.sshPublicKeyRef | default dict)) }}
      {{- range $key, $_ := $ci.sshPublicKeyRef }}
        {{- if not (has $key $validSshRefKeys) }}
          {{- $errors = append $errors (printf "%s: clusterInstall.sshPublicKeyRef.%s is not a recognized field (valid: %s)" $path $key (join ", " $validSshRefKeys)) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Validate each interface */}}
    {{- range $ifaceId, $iface := $netInterfaces }}
      {{- if not $iface.type }}
        {{- $errors = append $errors (printf "%s interface %s: type is required (bond, vlan, ethernet)" $path $ifaceId) }}
      {{- else if not (has (toString $iface.type) $validTypes) }}
        {{- $errors = append $errors (printf "%s interface %s: type must be one of: bond, vlan, ethernet (got: %s)" $path $ifaceId $iface.type) }}
      {{- end }}
      {{- if not $iface.name }}
        {{- $errors = append $errors (printf "%s interface %s: name is required" $path $ifaceId) }}
      {{- end }}

      {{/* Validate ipv4 mode */}}
      {{- $ipv4Mode := ($iface.ipv4 | default "disabled" | toString) }}
      {{- if not (has $ipv4Mode $validIpv4) }}
        {{- $errors = append $errors (printf "%s interface %s: ipv4 must be one of: disabled, dhcp, static (got: %s)" $path $ifaceId $ipv4Mode) }}
      {{- end }}

      {{/* Validate ipv6 mode */}}
      {{- $ipv6Mode := ($iface.ipv6 | default "disabled" | toString) }}
      {{- if not (has $ipv6Mode $validIpv6) }}
        {{- $errors = append $errors (printf "%s interface %s: ipv6 must be one of: disabled, dhcp, autoconf, static (got: %s)" $path $ifaceId $ipv6Mode) }}
      {{- end }}

      {{/* Bond-specific validation */}}
      {{- if eq (toString ($iface.type | default "")) "bond" }}
        {{- if not $iface.mode }}
          {{- $errors = append $errors (printf "%s interface %s: mode is required for bond type (e.g., 802.3ad, active-backup)" $path $ifaceId) }}
        {{- end }}
        {{- if not $iface.ports }}
          {{- $errors = append $errors (printf "%s interface %s: ports is required for bond type" $path $ifaceId) }}
        {{- end }}
      {{- end }}

      {{/* VLAN-specific validation */}}
      {{- if eq (toString ($iface.type | default "")) "vlan" }}
        {{- if not $iface.id }}
          {{- $errors = append $errors (printf "%s interface %s: id is required for vlan type" $path $ifaceId) }}
        {{- end }}
        {{- if not $iface.base }}
          {{- $errors = append $errors (printf "%s interface %s: base is required for vlan type" $path $ifaceId) }}
        {{- else if not (hasKey $ifaceNames (toString $iface.base)) }}
          {{- $errors = append $errors (printf "%s interface %s: base '%s' does not match any interface name in the topology" $path $ifaceId $iface.base) }}
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
          {{- $errors = append $errors (printf "%s interface %s: ipv4 is 'static' but no host has networking.interfaces.%s.ipv4.addresses" $path $ifaceId $ifaceId) }}
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
          {{- $errors = append $errors (printf "%s interface %s: ipv6 is 'static' but no host has networking.interfaces.%s.ipv6.addresses" $path $ifaceId $ifaceId) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Validate routes */}}
    {{- range $routeId, $route := (dig "routes" dict $networking) }}
      {{- if not $route.destination }}
        {{- $errors = append $errors (printf "%s route %s: destination is required" $path $routeId) }}
      {{- end }}
      {{- if not $route.gateway }}
        {{- $errors = append $errors (printf "%s route %s: gateway is required" $path $routeId) }}
      {{- end }}
      {{- if not $route.interface }}
        {{- $errors = append $errors (printf "%s route %s: interface is required" $path $routeId) }}
      {{- end }}
    {{- end }}

    {{/* Validate each host */}}
    {{- range $hostname, $host := $hosts }}
      {{- if not $host.bmcIP }}
        {{- $errors = append $errors (printf "%s host %s: bmcIP is required" $path $hostname) }}
      {{- end }}
      {{- if not $host.bmcPrefix }}
        {{- $errors = append $errors (printf "%s host %s: bmcPrefix is required" $path $hostname) }}
      {{- end }}
      {{- if not $host.bootMACAddress }}
        {{- $errors = append $errors (printf "%s host %s: bootMACAddress is required" $path $hostname) }}
      {{- end }}
      {{- range $idx, $iface := ($host.interfaces | default list) }}
        {{- if not (index $iface "macAddress") }}
          {{- $errors = append $errors (printf "%s host %s: interfaces[%d].macAddress is required" $path $hostname $idx) }}
        {{- end }}
      {{- end }}

      {{/* Validate role */}}
      {{- $validRoles := list "master" "worker" }}
      {{- $role := ($host.role | default "master" | toString) }}
      {{- if not (has $role $validRoles) }}
        {{- $errors = append $errors (printf "%s host %s: role must be 'master' or 'worker' (got: %s)" $path $hostname $role) }}
      {{- end }}

      {{/* Validate rootDeviceHints keys */}}
      {{- $validHintKeys := list "deviceName" "serialNumber" "model" "vendor" "wwn" "wwnWithExtension" "wwnVendorExtension" "hctl" "rotational" "minSizeGigabytes" }}
      {{- range $hintKey, $_ := ($host.rootDeviceHints | default dict) }}
        {{- if not (has $hintKey $validHintKeys) }}
          {{- $errors = append $errors (printf "%s host %s: rootDeviceHints.%s is not a valid hint (valid: %s)" $path $hostname $hintKey (join ", " $validHintKeys)) }}
        {{- end }}
      {{- end }}

      {{/* Validate per-host networking references topology interfaces */}}
      {{- range $ifaceId, $override := (dig "networking" "interfaces" dict $host) }}
        {{- if not (hasKey $netInterfaces $ifaceId) }}
          {{- $errors = append $errors (printf "%s host %s: networking.interfaces.%s references unknown topology interface" $path $hostname $ifaceId) }}
        {{- end }}
        {{/* Validate per-host ipv4 addresses */}}
        {{- range $idx, $addr := (dig "ipv4" "addresses" list $override) }}
          {{- if not (index $addr "ip") }}
            {{- $errors = append $errors (printf "%s host %s interface %s: ipv4.addresses[%d].ip is required" $path $hostname $ifaceId $idx) }}
          {{- end }}
          {{- if not (index $addr "prefixLength") }}
            {{- $errors = append $errors (printf "%s host %s interface %s: ipv4.addresses[%d].prefixLength is required" $path $hostname $ifaceId $idx) }}
          {{- end }}
        {{- end }}
        {{/* Validate per-host ipv6 addresses */}}
        {{- range $idx, $addr := (dig "ipv6" "addresses" list $override) }}
          {{- if not (index $addr "ip") }}
            {{- $errors = append $errors (printf "%s host %s interface %s: ipv6.addresses[%d].ip is required" $path $hostname $ifaceId $idx) }}
          {{- end }}
          {{- if not (index $addr "prefixLength") }}
            {{- $errors = append $errors (printf "%s host %s interface %s: ipv6.addresses[%d].prefixLength is required" $path $hostname $ifaceId $idx) }}
          {{- end }}
        {{- end }}
      {{- end }}

      {{/* Validate per-host routes */}}
      {{- range $routeId, $route := (dig "networking" "routes" dict $host) }}
        {{- if not $route.destination }}
          {{- $errors = append $errors (printf "%s host %s route %s: destination is required" $path $hostname $routeId) }}
        {{- end }}
        {{- if not $route.gateway }}
          {{- $errors = append $errors (printf "%s host %s route %s: gateway is required" $path $hostname $routeId) }}
        {{- end }}
        {{- if not $route.interface }}
          {{- $errors = append $errors (printf "%s host %s route %s: interface is required" $path $hostname $routeId) }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Validate role counts match topology */}}
    {{- $masterCount := 0 }}
    {{- $workerCount := 0 }}
    {{- range $hostname, $host := $hosts }}
      {{- $role := ($host.role | default "master" | toString) }}
      {{- if eq $role "master" }}
        {{- $masterCount = add $masterCount 1 | int }}
      {{- else if eq $role "worker" }}
        {{- $workerCount = add $workerCount 1 | int }}
      {{- end }}
    {{- end }}
    {{- if ne $masterCount $cpCount }}
      {{- $errors = append $errors (printf "%s: %d hosts have role 'master' but controlPlaneAgents is %d" $path $masterCount $cpCount) }}
    {{- end }}

    {{- end }}{{/* end baremetal-specific validations */}}

    {{/* Fail with all collected errors */}}
    {{- if gt (len $errors) 0 }}
      {{- fail (printf "\n\nCluster-install validation failed for '%s' (%d errors):\n  - %s\n" $clusterName (len $errors) (join "\n  - " $errors)) }}
    {{- end }}

  {{- end }}

  {{/* ===== Validate shared config sections (applies to all clusters) ===== */}}
  {{- $wp := (dig "config" "workloadPartitioning" dict $cluster) }}
  {{- if not (empty $wp) }}
    {{- $wpErrorStr := (include "autoshift.validate-workload-partitioning" (dict "path" (printf "cluster %s" $clusterName) "config" $wp)) | trim }}
    {{- if $wpErrorStr }}
      {{- fail (printf "\n\nWorkload partitioning validation failed for '%s':\n  - %s\n" $clusterName (join "\n  - " (splitList "\n" $wpErrorStr))) }}
    {{- end }}
  {{- end }}

{{- end }}

{{/* ===== Validate clusterset configs ===== */}}
{{- $allClusterSets := dict }}
{{- range $name, $cs := ($.Values.hubClusterSets | default dict) }}
  {{- $_ := set $allClusterSets (printf "hubClusterSets.%s" $name) $cs }}
{{- end }}
{{- range $name, $cs := ($.Values.managedClusterSets | default dict) }}
  {{- $_ := set $allClusterSets (printf "managedClusterSets.%s" $name) $cs }}
{{- end }}
{{- range $csPath, $cs := $allClusterSets }}
  {{- $csConfig := ($cs.config | default dict) }}
  {{- if not (empty $csConfig) }}
    {{- $errors := list }}

    {{/* Validate disconnected config via shared template */}}
    {{- $csDisconnected := ($csConfig.disconnected | default dict) }}
    {{- if not (empty $csDisconnected) }}
      {{- $csDcErrorStr := (include "autoshift.validate-disconnected" (dict "path" $csPath "config" $csDisconnected)) | trim }}
      {{- if $csDcErrorStr }}
        {{- range splitList "\n" $csDcErrorStr }}
          {{- $errors = append $errors . }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{/* Validate workloadPartitioning config via shared template */}}
    {{- $csWp := ($csConfig.workloadPartitioning | default dict) }}
    {{- if not (empty $csWp) }}
      {{- $csWpErrorStr := (include "autoshift.validate-workload-partitioning" (dict "path" $csPath "config" $csWp)) | trim }}
      {{- if $csWpErrorStr }}
        {{- range splitList "\n" $csWpErrorStr }}
          {{- $errors = append $errors . }}
        {{- end }}
      {{- end }}
    {{- end }}

    {{- if gt (len $errors) 0 }}
      {{- fail (printf "\n\nClusterset config validation failed for '%s' (%d errors):\n  - %s\n" $csPath (len $errors) (join "\n  - " $errors)) }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end -}}
