# openshift-compliance-operator

Installs the Compliance Operator, runs the scans named in `config.compliance.scans`, applies the
remediations you select, and reports the findings that need a person.

| Policy | What it does |
|---|---|
| `policy-compliance-operator-install` | namespace and `OperatorPolicy` |
| `policy-stig-scan` | one `ScanSettingBinding` per configured scan, plus the scan Role |
| `policy-stig-scan-test` | inform: each configured `ComplianceSuite` reached `DONE` |
| `policy-stig-manual-review` | inform: every `MANUAL` result and every `FAIL` with no remediation |
| `policy-auto-remediate` | sets `spec.apply` on each remediation from `autoApply` and `exclude` |

Profiles are pinned to a version and the scan name carries it, so adopting a new Security Technical
Implementation Guide release means adding an entry rather than inheriting one. `autoApply` defaults
to false: adding a scan gives you findings and no cluster changes.

`exclude` is a reject list. Naming a remediation sets `apply: false` rather than declining to set
it true, so adding a name after the fact reverts that remediation.

`policy-auto-remediate` reads the scan configuration on the hub but looks up
`ComplianceRemediation` objects spoke side, because each cluster has its own set produced by its
own scan.

Full walkthrough: [docs/compliance.md](../../../docs/compliance.md).
