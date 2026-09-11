# Compliance and STIG

What to set to scan a cluster, apply the remediations you choose, and see what is left for a person.
The examples use the DISA STIG because it needs the most manual work, but any profile the Compliance
Operator ships works the same way, and several can run side by side.

## What each setting controls

| Setting | Where | Effect |
|---|---|---|
| `compliance` | label | installs the Compliance Operator and creates the scan objects |
| `compliance-storage-class` | label | storage class for raw scan results, when the cluster default cannot bind on a control plane node |
| `compliance-auto-remediate` | label | set to `false` to keep a cluster scanned but never changed; overrides `autoApply` |
| `config.compliance.scans` | config | which profiles to scan, and which remediations to apply |
| `manual-remediations` | label | set gate for the policies covering findings the operator ships no fix for |
| `manual-remediations-<name>` | label | opts into one member; both labels are required to place it |
| `config.manualRemediations` | config | one key per finding; each renders nothing until set |
| `logging` | label | installs the logging operator, needed for audit log forwarding |
| `config.logging.forwarders` | config | `ClusterLogForwarder` objects, including the audit one |

## Scanning

```yaml
hubClusterSets:
  hub:
    labels:
      compliance: 'true'
      compliance-storage-class: 'gp3-csi'
    config:
      compliance:
        scans:
          - name: stig-v2r3
            profiles:
              - ocp4-stig-v2r3
              - ocp4-stig-node-v2r3
              - rhcos4-stig-v2r3
            scanSetting: default
            autoApply: false
            exclude: []
```

| Key | Meaning |
|---|---|
| `name` | names the `ScanSettingBinding` and the `ComplianceSuite`, and labels every remediation the scan produces. Put the profile version in it |
| `profiles` | pin the version. `ocp4-stig` follows whatever DISA publishes next; `ocp4-stig-v2r3` changes only when you edit this file |
| `scanSetting` | the `ScanSetting` to use. `default` is created for you and stores raw results on a 1Gi volume per scan, keeping three runs |
| `autoApply` | `false` scans and reports. `true` applies every remediation the scan produced |
| `exclude` | remediation names to hold at `apply: false`, even when `autoApply` is true |

Add a scan for a new release rather than editing the version in place, so adopting it is a change
you review.

### Running more than one benchmark

Each entry is independent, including `autoApply`, so a second benchmark can report while the first
remediates:

```yaml
        scans:
          - name: stig-v2r3
            profiles:
              - ocp4-stig-v2r3
              - ocp4-stig-node-v2r3
              - rhcos4-stig-v2r3
            autoApply: true
          - name: nist-moderate-rev4
            profiles:
              - ocp4-moderate-rev-4
              - ocp4-moderate-node-rev-4
              - rhcos4-moderate-rev-4
            autoApply: false
```

Each becomes its own `ScanSettingBinding` and `ComplianceSuite`, and remediations carry the suite
name, so `autoApply` and `exclude` only ever affect their own scan. List what the operator offers:

```console
oc get profile.compliance -n openshift-compliance
```

Alongside the STIG, that includes the NIST 800-53 moderate and high baselines
(`ocp4-moderate-rev-4`, `ocp4-high-rev-4`), CIS, PCI-DSS, BSI, Essential Eight and NERC-CIP. The
`-rev-4` and `-1-9` style suffixes are the pinned revisions; the unsuffixed names follow whatever
ships next.

## Applying remediations

Every shipped profile scans with `autoApply: false`, so a cluster reports its posture without being
changed. Hardening is a separate, deliberate step.

Layer `hub-hardened.yaml` on top of `hub.yaml` to apply it, by adding it to the AutoShift
Application's `valueFiles`. It goes last, so its values win:

```yaml
spec:
  source:
    path: autoshift
    helm:
      valueFiles:
        - values/global.yaml
        - values/clustersets/hub.yaml
        - values/clustersets/hub-hardened.yaml
```

Argo CD syncs the change; hardening starts when it does.

That profile turns on `autoApply` for both benchmarks, enables the manual remediation members, adds
file integrity monitoring, and pre-populates `manualReview` with the findings that cannot be fixed on
a running cluster. Lists replace rather than merge, so its `scans` block is the whole list.

Setting `autoApply: true` applies every remediation the scan produced except those in `exclude`.

Several remediations are `MachineConfig` objects, so **enabling this reboots every node**. Expect
more than one round: applying remediations rolls the nodes, the next scan finds more, and those roll
the nodes again. Some checks stay `FAIL` in between until the reboot that fixes them has happened.
Watch both, and treat the run as finished only when the pools are `Updated` and the suite is `DONE`:

```console
oc get mcp
oc get compliancesuite -n openshift-compliance
```

A remediation is not scored until the next scan:

```console
for s in $(oc get compliancescan -n openshift-compliance -o name); do
  oc annotate -n openshift-compliance "$s" compliance.openshift.io/rescan= --overwrite
done
```

Naming a remediation in `exclude` sets `apply: false` rather than leaving it unset, so adding a name
later reverts that remediation.

The example configuration excludes four `project-config-and-template` remediations. They rewrite the
default project template so every new project gets a deny-by-default `NetworkPolicy` and a
`ResourceQuota`, and they write only that restrictive half. The result breaks admission webhooks and
any pod that declares no resources. Use `manualRemediations.projectTemplate` instead. Write the
reason beside any exclusion you add.

## Findings with no automatic remediation

Two labels place each policy: the set gate, and one per member. A policy nobody asked for is not
deployed at all, rather than deployed and inert. Configuration carries content only, never an
on and off switch.

```yaml
      labels:
        manual-remediations: 'true'
        manual-remediations-motd: 'true'
        manual-remediations-classification-banner: 'true'
      config:
        manualRemediations:
          motd: |
            You are accessing a U.S. Government (USG) Information System (IS) ...
```

The member label is `manual-remediations-<policy name without the policy- prefix>`, so
`policy-motd` is enabled by `manual-remediations-motd`.

| Config key | Satisfies |
|---|---|
| `imageRegistries` | `ocp-allowed-registries`, `ocp-allowed-registries-for-import` |
| `classificationBanner` | `classification-banner` |
| `motd` | `openshift-motd-exists` |
| `oauth` | `oauth-login-template-set`, `oauth-provider-selection-set`, `oauth-logout-url-set` |
| `routeRateLimits` | `routes-rate-limit` |
| `projectTemplate` | `project-config-and-template-resource-quota` |
| `kubeletEviction` | `kubelet-eviction-thresholds-set-hard-imagefs-available`, `-nodefs-available` |
| `auditdConfig` | `auditd-data-disk-error-action`, `-disk-full-action`, `-retention-flush`, `-retention-space-left-action` |
| `sshdAccess` | `sshd-limit-user-access` |
| `clusterProxy` | `cluster-wide-proxy-set` |
| `rejectUnsignedImages` | `reject-unsigned-images-by-default` |
| `removeSamplesOperator` | none; stops the Samples Operator pulling from a blocked registry |

Every key and its default is in `autoshift/values/clustersets/_example.yaml` under
`manualRemediations`.

### imageRegistries

```yaml
          imageRegistries:
            allowed:
              - quay.io
              - registry.redhat.io
              - image-registry.openshift-image-registry.svc:5000
            allowedForImport:
              - domainName: quay.io
                insecure: false
```

`allowed` is enforced on every node, and anything not listed cannot be pulled. List every registry
your enabled policies use, not only the ones above. Running pods keep running, so a missing entry
appears at the next pull rather than at sync. Enumerate what the cluster uses:

```console
oc get pods -A -o jsonpath='{..image}' | tr ' ' '\n' | cut -d/ -f1 | sort -u
```

### classificationBanner and motd

```yaml
          classificationBanner:
            text: 'UNCLASSIFIED'
            location: 'BannerTopBottom'
            color: '#ffffff'
            backgroundColor: '#008000'
          motd: |
            You are accessing a U.S. Government (USG) Information System (IS) ...
```

### oauth

The templates are HTML held in Secrets that you create first. Key names are fixed:

```console
oc adm create-login-template > login.html
oc adm create-provider-selection-template > providers.html
oc adm create-error-template > errors.html

oc -n openshift-config create secret generic login-template --from-file=login.html
oc -n openshift-config create secret generic providerselect-template --from-file=providers.html
oc -n openshift-config create secret generic error-template --from-file=errors.html
```

Edit the generated HTML to carry your consent banner before applying it, and check you can still
sign in after each change.

```yaml
          oauth:
            loginTemplateSecret: 'login-template'
            providerSelectionTemplateSecret: 'providerselect-template'
            errorTemplateSecret: 'error-template'
            logoutRedirect: 'https://example.com/logged-out'
```

### routeRateLimits

The rule scores every route outside `kube-*` and `openshift-*`, so `allNamespaces` is what passes
it. `annotations` defaults to ten concurrent connections and ten per second.

```yaml
          routeRateLimits:
            allNamespaces: true
```

### projectTemplate

Owns the default project template, so new projects are created with the objects the rule looks for.
Existing namespaces are unaffected, as are namespaces an operator creates directly.

```yaml
          projectTemplate:
            enabled: true
            name: 'project-request'
            networkPolicies: false
            quota:
              requests.cpu: '4'
              requests.memory: '8Gi'
              limits.cpu: '8'
              limits.memory: '16Gi'
            limitRange:
              default:
                cpu: '500m'
                memory: '512Mi'
              defaultRequest:
                cpu: '100m'
                memory: '256Mi'
```

Set `quota` and a `LimitRange` is written alongside it. Keep the `LimitRange`: without it the quota
refuses any pod that declares no resources, with `failed quota: must specify limits.cpu`. Its
`default` is also the ceiling for those pods, so size it for the workloads you expect.

Leave `networkPolicies` off. It writes a deny-by-default policy into every new project, which stops
the API server reaching an admission webhook there and breaks anything serving one.

Changes to the project template apply only once `openshift-apiserver` reloads. Until then
`oc new-project` produces a bare namespace while the configuration already names the template:

```console
oc get pods -n openshift-apiserver
oc rollout restart deployment/apiserver -n openshift-apiserver
```

### Node and operating system settings

`kubeletEviction`, `auditdConfig` and `sshdAccess` each write node configuration and **roll every
node in the pools listed**. All three are off by default and the shipped values are the ones the
profiles check for.

```yaml
          kubeletEviction:
            enabled: true
          auditdConfig:
            enabled: true
          sshdAccess:
            enabled: true
            AllowGroups:
              - 'core'
```

Read these before turning them on:

- `auditdConfig` sets `disk_error_action` and `disk_full_action` to `single`, which drops a node to
  single-user mode rather than let auditing stop. That is the control's intent and it is disruptive.
  `space_left_action` of `email` needs a working local mailer, or the warning goes nowhere.
- `sshdAccess` naming a group nobody belongs to locks everyone out of SSH on those nodes. On Red Hat
  CoreOS the usual account is `core`.

### rejectUnsignedImages

```yaml
          rejectUnsignedImages:
            enabled: false
            pools:
              - 'master'
              - 'worker'
```

Writes a `MachineConfig` setting `/etc/containers/policy.json` to reject by default. This reboots
every node in the listed pools and stops unsigned image pulls. Leave it off until image signing is
in place.

## Audit log forwarding

Two rules need audit logs sent off the cluster. These use the logging operator, not
`manualRemediations`:

```yaml
      labels:
        logging: 'true'
      config:
        logging:
          forwarders:
            - name: 'audit'
              collectorRoles:
                - 'collect-audit-logs'
              spec:
                outputs:
                  - name: 'audit-remote'
                    type: 'syslog'
                    syslog:
                      url: 'tls://syslog.example.com:6514'
                      rfc: 'RFC5424'
                pipelines:
                  - name: 'audit-to-remote'
                    inputRefs:
                      - 'audit'
                    outputRefs:
                      - 'audit-remote'
```

`spec` is `ClusterLogForwarder` schema and is passed through as written, so filters, tuning and any
output type work without a change here. List several entries for several destinations. The collector
`ServiceAccount` is created for you, named `<name>-collector` unless the spec says otherwise, and
bound to `collectorRoles`.

`logging: 'true'` also covers `cluster-logging-operator-exist`.

## Partitioning, at install only

The `partition-for-var-log*` rules want the audit log directories on their own filesystems, so a
full disk cannot silently stop auditing. Partitioning Red Hat CoreOS is an install time operation,
so there is no day two remediation.

Every one of these rules reports MANUAL, on a partitioned cluster and an unpartitioned one alike.
The check does not inspect the filesystem, it asks a person to confirm, so partitioning satisfies
the control an auditor checks without turning the result green. Accept each rule in `manualReview`
either way, and treat the partitions as the evidence behind that decision.

There are five of them, and each checks its own path: `/var/log`, `/var/log/audit`,
`/var/log/kube-apiserver`, `/var/log/oauth-apiserver` and `/var/log/openshift-apiserver`. A separate
`/var/log` does not satisfy the four nested inside it.

The [installation documentation][sep-var] describes adding a single partition, at `/var` or a
subdirectory of it. Ignition itself allows arbitrary partitioning, as [Customizing nodes][cust]
describes.

Covering all five paths needs five partitions, so set `allowMultiple: true` to create more than
one. A `mountPath` outside `/var` is rejected.

Take `/var/log/audit`. It is the filesystem whose exhaustion stops auditing, and it is the only
partition rule the STIG has, so a single partition addresses that profile completely:

| Profile | Partition rules | Addressed by one `/var/log/audit` partition |
|---|---|---|
| DISA STIG V2R3 | 1 | all of them |
| NIST 800-53 moderate | 5 | one |

The remaining four NIST paths stay unpartitioned in that case. All five still need accepting in
`manualReview`, because the result is MANUAL whether the partition exists or not.

For clusters AutoShift provisions, set it in `config.clusterInstall`:

```yaml
        clusterInstall:
          diskPartitions:
            device: '/dev/sda'
            roles:
              - 'master'
              - 'worker'
            partitions:
              - label: 'varlogaudit'
                mountPath: '/var/log/audit'
                startMiB: 102400
                sizeMiB: 10000
                format: 'xfs'
```

That becomes a `MachineConfig` in the cluster's extra manifests, applied before a node first boots.
It works on bare metal through SiteConfig `extraManifestsRefs`, and on Amazon Web Services and
vSphere through Hive `provisioning.manifestsConfigMapRef`.

There is no day two equivalent. Ignition's disk and filesystem stages run once, in the initramfs on
first boot, and the marker that gates them is removed afterwards, so no amount of rebooting replays
them. Applying the same MachineConfig to a running node writes and enables the mount unit, which the
Machine Config Operator does handle, while the partition and filesystem never appear: the unit then
fails against a device that does not exist.
`device` must be the install disk. `startMiB` on the first partition sets the size of the root
filesystem, because root grows only as far as the next partition, so a small value is not a safe
default: it is the root filesystem. Container images live in `/var/lib/containers`, which stays on
root unless `/var` is a partition of its own, and a root that cannot hold the images fills up and
kubelet reports `DiskPressure` before the cluster operators finish starting. Give `/var` its own
partition with a `sizeMiB` of 0, which takes the rest of the disk and only makes sense on the last
entry. The full layout is in `autoshift/values/clusters/_example-cluster-install-baremetal.yaml`.

systemd orders nested mounts by path, so `/var/log` mounts before `/var/log/audit` without anything
extra. Mount unit names are escaped the way `systemd-escape --path` does it, because a hyphen inside
a path component is written `\x2d`: `/var/log/kube-apiserver` becomes
`var-log-kube\x2dapiserver.mount`.

On an existing cluster, accept these in `manualReview` and revisit at the next rebuild.

## The Container Security Operator

`container-security-operator-exists` asks for an operator that was deprecated in Red Hat Quay 3.16,
on OpenShift Container Platform 4.20, and is slated for removal. Red Hat Advanced Cluster Security
replaces it and shows vulnerability information in the web console, so AutoShift does not install it.

Enable `acs` and accept the rule naming the replacement:

```yaml
        compliance:
          manualReview:
            accepted:
              - name: 'ocp4-stig-v2r3-container-security-operator-exists'
                reason: 'Container Security Operator is deprecated. Red Hat Advanced Cluster Security provides the equivalent scanning and console integration.'
```

## What is left for a person

`policy-stig-manual-review` is inform only. It reports every `MANUAL` result, every `FAIL` with no
remediation, and any `INCONSISTENT` result that failed on at least one node, computed from the scan
on the cluster so it stays accurate as coverage changes. It stays NonCompliant while anything is
outstanding, which is the report rather than a fault.

`INCONSISTENT` means a check returned different results across nodes. Most are simply
`NOT-APPLICABLE` on one node role and are not worth anyone's time, so only those with a `FAIL` among
their sources are reported. The detail is on the result itself:

```console
oc get compliancecheckresult -n openshift-compliance <name> \
  -o jsonpath='{.metadata.annotations.compliance\.openshift\.io/inconsistent-source}'
```

```console
oc get configurationpolicy -n <cluster> policy-stig-manual-review \
  -o jsonpath='{.status.compliancyDetails[*].conditions[*].message}'
```

Two groups always appear. Review items, such as least privilege in role bindings, the security
context constraint reviews and `/var/log/audit` partitioning, need a person to look and record a
decision. And `fips-mode-enabled-on-all-nodes`, which is an install time flag in
`install-config.yaml` and cannot be turned on afterwards.

`policy-quota-guard` is also inform only. It reports any quota that will reject pods, meaning a
compute quota with no `LimitRange` in its namespace, wherever that quota came from.

## Checking

```console
oc get compliancesuite -n openshift-compliance
oc get compliancecheckresult -n openshift-compliance \
  -o jsonpath='{range .items[*]}{.status}{"\n"}{end}' | sort | uniq -c
oc get complianceremediation -n openshift-compliance \
  -o jsonpath='{range .items[*]}{.spec.apply}{"\n"}{end}' | sort | uniq -c
```

| Symptom | Cause |
|---|---|
| No `ScanSettingBinding` appears | `config.compliance.scans` is not set |
| Scan never leaves `LAUNCHING` | the storage class cannot bind on a control plane node; set `compliance-storage-class` |
| Remediations stay `apply: false` | `autoApply` is false, or `compliance-auto-remediate` is `false` on the cluster |
| A rule stays `FAIL` after remediating | the scan has not rerun, or the node reboot it needs has not happened |
| `oc new-project` ignores the template | `openshift-apiserver` has not reloaded |
| Pods rejected with `must specify limits.cpu` | a quota with no `LimitRange`; `policy-quota-guard` reports these |

## Reference

AutoShift:

- `policies/stable/openshift-compliance-operator/` scanning, remediation and the review policies
- `policies/stable/manual-remediations/` one policy per finding with no automatic remediation
- [Values reference](values-reference.md)

Red Hat OpenShift Container Platform, [Compliance Operator][co]. The chapter is a single page:

- 5.4.2 Understanding the Custom Resource Definitions
- 5.6.1 Supported compliance profiles, for which STIG version ships in which operator release
- 5.6.2 Compliance Operator scans, including rescanning and scheduling
- 5.6.4 Tailoring the Compliance Operator, for `TailoredProfile` and the exempt regex variables
- 5.6.6 Managing Compliance Operator result and remediation
- 5.6.9 Using the `oc-compliance` plugin

[co]: https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/security_and_compliance/compliance-operator
[sep-var]: https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/installing_on_bare_metal/user-provisioned-infrastructure
[cust]: https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/installation_configuration/installing-customizing
