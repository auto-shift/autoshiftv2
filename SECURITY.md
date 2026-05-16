# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Previous minor  | Bug fixes only |
| Older           | No |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

To report a vulnerability, please use GitHub's private [Security Advisory](https://github.com/auto-shift/autoshiftv2/security/advisories/new) feature.

Please include:
- A description of the vulnerability and its potential impact
- Steps to reproduce or a proof of concept
- Any suggested mitigations if known

You can expect an acknowledgment within 72 hours and a resolution timeline within 14 days depending on severity.

## Scope

This project is an IaC framework — the primary security concerns are:

- **Secret exposure** — credentials or tokens embedded in Helm values or policy templates
- **Policy bypass** — hub template logic that could allow unauthorized cluster access
- **Supply chain** — malicious changes to the chart packaging or OCI release pipeline

The `gitleaks` scan in CI catches accidental secret commits. If you find a bypass or a gap in coverage, please report it privately.
