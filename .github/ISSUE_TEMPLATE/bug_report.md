---
name: Bug Report
about: Report incorrect policy behavior, template rendering errors, or unexpected cluster state
title: '[Bug] '
labels: bug
---

## Description

<!-- A clear description of the bug -->

## Environment

| Field | Value |
|-------|-------|
| AutoShift version | <!-- e.g. v1.2.3 or commit SHA --> |
| OpenShift version | |
| RHACM version | |
| Deployment method | <!-- Source / OCI --> |

## Affected Policy

<!-- Which policy chart is affected? e.g. policies/stable/openshift-gitops -->

## Steps to Reproduce

1. 
2. 
3. 

## Expected Behavior

<!-- What should happen -->

## Actual Behavior

<!-- What actually happens — include error messages, policy status output, or ArgoCD sync errors -->

```
# Paste relevant output here
oc describe policy <name> -n open-cluster-policies
```

## Additional Context

<!-- Hub template rendering issues, disconnected environment, hub-of-hubs topology, etc. -->
