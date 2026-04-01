# Ansible Automation for Cluster Provisioning

Ansible roles and playbooks for provisioning baremetal OpenShift clusters via ACM SiteConfig. Designed to run from AAP (Ansible Automation Platform) or the CLI.

## Architecture

```
Hub Cluster (OCI-deployed)
├── GitLab (on-cluster) ← Ansible pushes cluster values here
├── ArgoCD (multi-source) ← pulls chart from OCI + values from GitLab
└── ACM Policies ← provision spoke clusters from values
```

**Chart + base values** come from the OCI registry (immutable).
**Cluster-specific values** come from the GitLab site-config repo (managed by Ansible).

## Prerequisites

- `oc` CLI authenticated to the hub cluster
- `ansible` 2.15+ with `kubernetes.core` collection
- Hub cluster running with AutoShift deployed from OCI
- GitLab deployed on the hub (via the `gitlab` policy)

Install the collection:
```bash
ansible-galaxy collection install -r ansible/requirements.yaml
```

## Setup

### 1. Create the vault

```bash
cp ansible/vault.yaml.example ansible/vault.yaml
ansible-vault encrypt ansible/vault.yaml
ansible-vault edit ansible/vault.yaml
```

The vault contains:
| Variable | Purpose |
|---|---|
| `idrac_user` / `idrac_pass` | BMC/iDRAC credentials (discovery + install secrets) |
| `init_secrets_registry` / `_user` / `_pass` | Container registry credentials (pull secret) |
| `init_secrets_ssh_private_key` | Existing SSH key (or leave blank to generate) |
| `init_secrets_jfrog_license` | JFrog Artifactory license key |
| `init_secrets_jfrog_admin_password` | JFrog Artifactory admin password |
| `init_secrets_jfrog_master_key` | Existing masterKey for Artifactory migration |

### 2. Initialize GitLab site-config repo

```bash
ansible-playbook ansible/playbooks/init-gitlab.yaml -c local
```

Creates the `autoshift/site-config` project in the on-cluster GitLab. Idempotent — safe to run multiple times.

### 3. Initialize cluster install secrets

```bash
ansible-playbook ansible/playbooks/init-secrets.yaml -c local --ask-vault-pass
```

Creates in the `cluster-install-secrets` namespace:
- BMC credential secret
- Pull secret (from registry credentials)
- SSH public key ConfigMap (generates ECDSA-521 keypair if not provided)

If `init_secrets_jfrog_keys: true`, also creates in `jfrog-system`:
- `artifactory-keys` (masterKey + joinKey)
- `artifactory-license` (if license provided in vault)
- `artifactory-admin` (if admin password provided in vault)

## Provisioning Clusters

### Option A: From inventory (non-interactive)

Create an inventory file (see `ansible/inventory/example-baremetal.yaml`):

```bash
ansible-playbook ansible/playbooks/create-cluster.yaml \
  -i ansible/inventory/my-cluster.yaml \
  -c local
```

### Option B: Interactive with hardware discovery

Discovers hardware from Dell iDRACs via Redfish API, lets you select NICs and disks:

```bash
ansible-playbook ansible/playbooks/discover-and-create-cluster.yaml \
  -e '{"idrac_hosts": ["192.168.1.10", "192.168.1.11", "192.168.1.12"]}' \
  --ask-vault-pass
```

### Option C: From AAP

Configure a Job Template with:
- **Project**: pointing to this repo
- **Playbook**: `ansible/playbooks/create-cluster.yaml`
- **Survey**: maps to inventory variables
- **Credentials**: vault password, OpenShift cluster token

## What happens when you create a cluster

1. **Validates** inputs (cluster name, domain, VIPs, hosts)
2. **Generates** `clusters/<name>.yaml` values file from the Jinja2 template
3. **Pushes** the file to the GitLab site-config repo (git commit + push)
4. **Patches** the ArgoCD Application to include `$siteValues/clusters/<name>.yaml`
   - First cluster: converts ArgoCD Application from single-source to multi-source
   - Subsequent clusters: appends the valueFile to existing multi-source
5. **ArgoCD syncs** → cluster-install policies chain and provision the spoke cluster

## Directory Structure

```
ansible/
├── ansible.cfg
├── .ansible-lint
├── requirements.yaml           # kubernetes.core collection
├── vault.yaml.example          # Template for encrypted credentials
├── vars/
│   └── shared.yaml             # Shared variables across roles
├── playbooks/
│   ├── init-gitlab.yaml        # One-time: create site-config repo
│   ├── init-secrets.yaml       # One-time: create install secrets
│   ├── create-cluster.yaml     # Repeatable: provision a spoke cluster
│   └── discover-and-create-cluster.yaml  # Interactive: iDRAC discovery + create
├── roles/
│   ├── init-gitlab/            # Creates GitLab group/project via API
│   ├── init-secrets/           # Creates K8s secrets for cluster install
│   ├── create-cluster/         # Generates values, pushes, patches ArgoCD
│   │   ├── defaults/main.yaml
│   │   ├── tasks/
│   │   │   ├── main.yaml       # Orchestration
│   │   │   ├── prompts.yaml    # Interactive prompts
│   │   │   ├── prompt-host.yaml # Per-host prompts with cascading defaults
│   │   │   └── update-argocd.yaml # Single→multi-source conversion + append
│   │   ├── templates/
│   │   │   └── cluster-values.yaml.j2  # Values file template
│   │   └── molecule/           # Tests (default + dhcp-mac scenarios)
│   └── discover-hardware/      # iDRAC Redfish API discovery
│       ├── tasks/
│       │   ├── main.yaml       # Discovery loop
│       │   ├── discover-host.yaml # Per-host: system, NICs, disks
│       │   └── select-nics.yaml # Interactive NIC/disk selection
│       └── defaults/main.yaml
├── inventory/
│   └── example-baremetal.yaml  # Example inventory
└── tests/
    └── mock-redfish-server.py  # Mock iDRAC for local testing
```

## Networking Configuration

The playbook supports:
- **Cluster VLAN**: management/API/node traffic (static or DHCP)
- **Storage VLAN**: ODF/Ceph traffic (static or DHCP, shared or dedicated NICs)
- **Bond modes**: LACP (802.3ad) or active-backup
- **1 or 2 NICs**: single NIC or bonded pair per VLAN
- **Port selection**: by NIC name or by MAC address (`identifier: mac-address` in NNCPs)

## Testing

Run molecule tests (no cluster required):

```bash
cd ansible/roles/create-cluster
molecule test                    # Default: static IPs, 3m+2w, LACP, disconnected
molecule test -s dhcp-mac        # DHCP, MAC port selection, active-backup
```

Run ansible-lint:

```bash
cd ansible
ansible-lint playbooks/ roles/
```

## Shared Variables

`ansible/vars/shared.yaml` contains variables used across multiple roles:

| Variable | Default | Purpose |
|---|---|---|
| `gitlab_group` | `autoshift` | GitLab group name |
| `gitlab_project` | `site-config` | GitLab project name |
| `gitlab_namespace` | `gitlab-system` | GitLab operator namespace |
| `argocd_app_name` | `autoshift` | ArgoCD Application name |
| `argocd_namespace` | `openshift-gitops` | ArgoCD namespace |
| `oci_registry` | `oci://quay.io/autoshift` | OCI chart registry |
| `site_config_repo` | auto-detected | GitLab site-config repo URL |
| `site_config_branch` | `main` | Git branch |
