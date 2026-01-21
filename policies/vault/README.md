# AUTOSHIFT
# Vault Helm Chart

## Features
* Vault and vault-agent-injector deployed to hub cluster via policy
* HA w/ raft storage, no tls option (yet)







## Updating **more todo

To update to a specific release of vault, reference the `values.openshift.yaml` in the [vault-helm repo](https://github.com/hashicorp/vault-helm.git)

**TODO: Figure out a better way of updating templates. 

## Documentation
Please see the many options supported in the `values.yaml` file. These are also
fully documented directly on the [Vault
website](https://developer.hashicorp.com/vault/docs/platform/k8s/helm) along with more
detailed installation instructions.



> :warning: **Please note**: We take Vault's security and our users' trust very seriously. If 
you believe you have found a security issue in Vault Helm, _please responsibly disclose_ 
by contacting us at [security@hashicorp.com](mailto:security@hashicorp.com).

This repository contains the official HashiCorp Helm chart for installing
and configuring Vault on Kubernetes. This chart supports multiple use
cases of Vault on Kubernetes depending on the values provided.

For full documentation on this Helm chart along with all the ways you can
use Vault with Kubernetes, please see the
[Vault and Kubernetes documentation](https://developer.hashicorp.com/vault/docs/platform/k8s).