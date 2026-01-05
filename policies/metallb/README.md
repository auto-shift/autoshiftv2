With these Autoshift policies you can automate the deployment of the MetalLB operator and manage its objects from source control. These policies will deploy the operator with its controller, and give you the ability to drop in your manifests to configure L2 Advertisement, IP Pools, BGP Mode, and Resource Quotas for the metall-lb namespace.

In your MetalLB policy folder, you will see a directory called files.

├── files
│   ├── bgp
│   ├── ippools
│   ├── l2advertisements
│   └── peers

These directories will store any of your manifest files based on what you want to configure. When MetalLB is deployed through Autoshift, it will detect these files and apply them as needed. Autoshift will convert these files into data on a ConfigMap, and that ConfigMap will be applied to the metallb-system namespace as an object.

In your autoshift/values.hub.yaml file, you will specify which files you’ve added and want to process.


This true or false flag controls if MetalLB should be installed or not.
metallb: 'false'
metallb-source: redhat-operators
metallb-source-namespace: openshift-marketplace
metallb-version: 'metallb-operator.v4.18.0-202509240837'  # Optional: pin to specific CSV version
metallb-channel: stable

This true or false flag controls if you want to apply a quota to your namespace
metallb-quota: 'false'

If quota is set to true, set your memory and cpu as needed. The default is 2 cpu, and 2Gi memory.
metallb-quota-cpu: ''
metallb-quota-memory: ''


Based on name, you will set these values to the name of your manifest file in the policies/metallb/files directory. For example, if your IP Pool file is called internal.yaml, your value would be “internal”. You can copy more values by increasing the number value on each line!

IP Pool Files:
metallb-ippool-1: ''

L2 Advertisement Files:
metallb-l2-1: ''

BGP Advertisement Files:
metallb-bgp-1: ''

BGP Peer Files:
metallb-peer-1: ''

Once you have everything set, you can apply the new labels through Autoshift.

helm template autoshift autoshift -f autoshift/values.hub.yaml | oc apply -f -
