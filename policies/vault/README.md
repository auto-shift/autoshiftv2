# AUTOSHIFT
# Vault Helm Chart

## Features
* Vault and vault-agent-injector deployed to hub cluster via policy
* HA w/ raft storage, no tls option **(yet)**
* Works with FIPS enabled clusters




## Currently requires manual initialization:
1) In the vault-0 pod, initialize vault: 
    `oc exec -n vault -ti vault-0 -- vault operator init --key-shares=1 --key-threshold=1`
    Save the Unseal Key and Root Token.
2) Unseal vault on vault-0:
    `oc exec -n vault -ti vault-0 -- vault operator unseal <unseal key>` 
3) Repeat unseal process on vault-1 and vault-2:
    `oc exec -n vault -ti vault-1 -- vault operator unseal <unseal key>` 
    `oc exec -n vault -ti vault-2 -- vault operator unseal <unseal key>` 
4) Log into vault via the route using the initial root token.

## TODO: Integrate tls
## TODO: Automate init


## Updating **more later** todo

To update to a specific release of vault, reference the `values.openshift.yaml` in the [vault-helm repo](https://github.com/hashicorp/vault-helm.git)
 

## Documentation
Please see the many options supported in the `values.yaml` file. These are also fully documented directly on the [Vault
website](https://developer.hashicorp.com/vault/docs/platform/k8s/helm) along with more detailed installation instructions.



> :warning: **Please note**: We take Vault's security and our users' trust very seriously. If you believe you have found a security issue in Vault Helm, _please responsibly disclose_ by contacting us at [security@hashicorp.com](mailto:security@hashicorp.com).

This repository contains the official HashiCorp Helm chart for installing and configuring Vault on Kubernetes. This chart supports multiple use cases of Vault on Kubernetes depending on the values provided.

For full documentation on this Helm chart along with all the ways you can use Vault with Kubernetes, please see the
[Vault and Kubernetes documentation](https://developer.hashicorp.com/vault/docs/platform/k8s).