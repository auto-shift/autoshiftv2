## CHART: cluster-install
#### Usage
- This chart must be built in the correct order to function properly as some resources within it reference others that should also be built by this chart beforehand
- bash/zsh: ```oc login <cluster-login-info> && for tpl in $(ls helm-charts/cluster-install/templates); do helm template helm-charts/cluster-install --show-only helm-charts/cluster-install/templates/$tpl | oc apply -f -; done```
- fish: ```oc login <cluster-login-info> && for tpl in (ls helm-charts/cluster-install/templates); do helm template helm-charts/cluster-install --show-only helm-charts/cluster-install/templates/$tpl | oc apply -f -; end```


#### Variables
| Name | Type | Default | Explaination |
|------|------|---------|--------------|
| clusterName | String | None | The name of the cluster to be installed. Does not include the base domain
| baseDomain | String | None | The base domain for the cluster. This will be appended to the cluster name for object naming as well as to the end of hostnames when creating host entries
| openshiftVersion | 
| pullSecret | JSON String | None | The pull secret for the images to be pulled from the openshift mirror, obtainable [here](https://console.redhat.com/openshift/install/pull-secret)
| bmcPrefix | String | None | the prefix for the eventual BMC URL, currently only supports 'idrac-virtualmedia' or 'redfish-virtualmedia'
| bmcEndpoint | String | None | the suffix/url path for the BMC. This value is defaulted to if hosts.host.bmcEndpoint is not supplied
| cpuArch | String | 'x86_64' | The cpu architecture of the target cluster
| networking | Key - Dictionary | N/A | The values under this key are all related to network configuration
| networking.defaultGateway | IPv4 | None | The default gateway for the machine network 
| networking.machinePrefixLength | integer | None | The length in bytes of the subnet prefix for the machine network in the cluster
| networking.culsterNetwork.cidr | IPv4 CIDR | None | The IPv4 CIDR definition for the cluster network
| networking.machineNetwork.cidr | IPv4 CIDR | None | The IPv4 CIDR definition of the machine network (network for the actual hosts)
| networking.dnsServers | IPv4 List | None | The list of DNS server ips the hosts will use
| networking.serviceNetwork | IPv4 CIDR List | None | A list of networks that services will be created on
| controlPaneAgents | Integer | None | The number of control plane agents/master nodes to create
| sshPublicKey | SSH Key string | None | The public ssh key to be put on the host via ignition
| hosts | Key - List | None | The values under this key are all related to individual host configurations 
| hosts.host.name | String | None | The base name for the host, this will be postfixed with the baseDomain value
| hosts.host.bmcUsername | String | None | The username for the BMC/IPMI login 
| hosts.host.bmcPassword | String | None | The password for the BMC/IPMI login 
| hosts.bmcIP | IPv4 String | None | The IP address for the BMC/IPMI interface
| hosts.host.bmcEndpoint | URL Path String | None | The URL path after the IP that the BMC is accessible from. This overrides the global bmcEndpoint for this host
| hosts.host.bootMACAddress | MAC Address String | None | The mac address that the host will attempt to boot from when installing 
| hosts.host.interfaces | Key - List | None | The list of defined interfaces for this host 
| hosts.host.interfaces.interface.macAddress | MAC Address String | None | The MAC address that this interface has 
| hosts.host.interfaces.interface.name | String | '\{\{ \"eno\" \(toString \(add1 \$idx\)\) \}\}' | The name to give the interface with the associate mac address
| hosts.host,.primaryMac | MAC Address String | None | The primary mac address for this host 
| hosts.host.IP | IPv4 String | None | The ip to assign to this actual host once it is installed (must be an IP in the machine network


