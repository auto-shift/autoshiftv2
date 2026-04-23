# Contributing to AutoShift

Thank you for your interest in contributing! AutoShift is an open-source IaC framework for managing OpenShift clusters at scale with RHACM and OpenShift GitOps.

## Terms

All contributions to the repository must be submitted under the terms of the [Apache Public License 2.0](https://www.apache.org/licenses/LICENSE-2.0).

## Certificate of Origin

By contributing to this project, you agree to the Developer Certificate of Origin (DCO). This is a simple statement that you have the legal right to make the contribution. See the [DCO](https://github.com/open-cluster-management-io/community/blob/main/DCO) file for details.

You must sign off your commit to certify the DCO. Add a line like the following at the end of your commit message:

```
Signed-off-by: Your Name <your-email-address>
```

Use `git commit --signoff` to do this automatically. To sign off an entire pull request:

```bash
git rebase --signoff main
```

## Code of Conduct

AutoShift is built on Open Cluster Management, a CNCF project. This project abides by the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md). See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for details.

## Ways to Contribute

- **New policies** — add support for additional OpenShift operators or cluster configurations
- **Bug fixes** — fix incorrect hub templates, label logic, or chart rendering issues
- **Documentation** — improve guides, add examples, clarify behavior
- **Testing** — expand the policy validation test suite in `tools/`

## Getting Started

1. **Fork** the repository on GitHub
2. **Clone** your fork locally:
   ```bash
   git clone https://github.com/YOUR-USERNAME/autoshiftv2.git
   cd autoshiftv2
   git remote add upstream https://github.com/auto-shift/autoshiftv2.git
   ```
3. **Install prerequisites**: `helm` (3.x), `oc`, `go` (see [Developer Guide](docs/developer-guide.md#developer-setup))

## Development Workflow

```bash
# Create a feature branch
git checkout -b feature/add-my-operator-policy

# Generate a new policy using the scaffolding scripts
./scripts/generate-operator-policy.sh my-operator my-operator-pkg --channel stable --namespace my-operator

# Validate rendering
helm template policies/stable/my-operator/

# Run the full policy validation suite
cd tools && go test -tags integration ./... -v
```

For detailed guidance on creating policies, hub template conventions, and label requirements, see the [Developer Guide](docs/developer-guide.md).

## Pull Request Guidelines

- One policy or feature per PR
- All CI checks must pass (`secret-scan`, `lint-and-test`, `validate-policies`)
- New `autoshift.io/<key>` labels must be declared in `autoshift/values/clustersets/_example.yaml`
- Include a README.md in new policy directories
- Commits must include a DCO sign-off line

Use the PR template — it has the full checklist.

## Issue and Pull Request Management

Anyone can comment on issues and submit reviews for pull requests. To be assigned an issue or pull request, leave a `/assign <your GitHub ID>` comment on it.

## Reporting Issues

Use [GitHub Issues](https://github.com/auto-shift/autoshiftv2/issues) with the appropriate template:
- **Bug report** — unexpected policy behavior, template rendering errors
- **Feature request** — new operator support, new configuration patterns

## Security Vulnerabilities

Do **not** open a public issue for security vulnerabilities. See [SECURITY.md](SECURITY.md) for the responsible disclosure process.
