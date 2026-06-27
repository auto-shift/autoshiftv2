## Description

<!-- What does this PR do? Why is it needed? -->

## Type of Change

- [ ] New policy (new operator or cluster configuration)
- [ ] Bug fix (incorrect template, label logic, chart rendering)
- [ ] Documentation update
- [ ] CI/tooling change
- [ ] Other: <!-- describe -->

## Checklist

- [ ] Policy generated using `generate-operator-policy.sh` or `generate-policy.sh` (if applicable)
- [ ] `helm template policies/stable/<name>/` renders without errors
- [ ] `cd tools && go test -tags integration ./...` passes
- [ ] New `autoshift.io/<key>` labels declared in `autoshift/values/clustersets/_example.yaml`
- [ ] `subscription-name`, `channel`, `source`, and `source-namespace` labels defined for any new operator
- [ ] Policy README.md included or updated
- [ ] No hardcoded values — hub templates used where cluster-specific values are needed
- [ ] Tested in a dev/sandbox environment
- [ ] Commits signed off with `git commit --signoff` (DCO)

## Related Issues

<!-- Closes #123 -->
