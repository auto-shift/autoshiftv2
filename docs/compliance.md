# Compliance and STIG

What to set to scan a cluster against the DISA STIG, apply the remediations you choose, and see what
is left for a person. Any profile the Compliance Operator ships works the same way.

## What each setting controls

| Setting | Where | Effect |
|---|---|---|
| `compliance` | label | installs the Compliance Operator and creates the scan objects |
| `compliance-storage-class` | label | storage class for raw scan results, when the cluster default cannot bind on a control plane node |
| `compliance-auto-remediate` | label | set to `false` to keep a cluster scanned but never changed; overrides `autoApply` |
| `config.compliance.scans` | config | which profiles to scan, and which remediations to apply |
| `manual-remediations` | label | enables the policies for findings the operator ships no fix for |
| `config.manualRemediations` | config | one key per finding; each renders nothing until set |
| `logging` | label | installs the logging operator, needed for audit log forwarding |
| `config.logging.forwarders` | config | `ClusterLogForwarder` objects, including the audit one |
| `container-security` | label | installs the Container Security Operator |

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

Add a scan for a new STIG release rather than editing the version in place, so adopting it is a
change you review.

## Applying remediations

Set `autoApply: true`. This applies every remediation the scan produced except those in `exclude`.

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

Set the label, then set only the keys you want. Everything else renders nothing.

```yaml
      labels:
        manual-remediations: 'true'
      config:
        manualRemediations:
          ...
```

| Config key | Satisfies |
|---|---|
| `imageRegistries` | `ocp-allowed-registries`, `ocp-allowed-registries-for-import` |
| `classificationBanner` | `classification-banner` |
| `motd` | `openshift-motd-exists` |
| `oauth` | `oauth-login-template-set`, `oauth-provider-selection-set`, `oauth-logout-url-set` |
| `routeRateLimits` | `routes-rate-limit` |
| `projectTemplate` | `project-config-and-template-resource-quota` |
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

Set `container-security: 'true'` for `container-security-operator-exists`, and `logging: 'true'`
covers `cluster-logging-operator-exist`.

## What is left for a person

`policy-stig-manual-review` is inform only. It reports every `MANUAL` result and every `FAIL` with no
remediation, computed from the scan on the cluster, so it stays accurate as coverage changes. It
stays NonCompliant while anything is outstanding, which is the report rather than a fault.

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
