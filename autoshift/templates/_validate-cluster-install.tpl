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

    {{/* Must have at least one host */}}
    {{- if empty $hosts }}
      {{- fail (printf "cluster %s: config.hosts is required (at least one host)" $clusterName) }}
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
    {{- end }}

  {{- end }}
{{- end }}
{{- end -}}
