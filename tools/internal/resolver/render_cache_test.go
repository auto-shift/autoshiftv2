//go:build integration

package resolver

import "sync"

// The integration tests render the same PolicyGenerator directories many times (project
// onboarding alone runs the pipeline hundreds of times), and kustomize with the PolicyGenerator
// plugin is the slowest step. The directories do not change during a run, so each directory is
// rendered once per REMEDIATION value and the output reused. A failed render is not cached.
var renderCache sync.Map // "<remediation>|<dir>" -> string

func cachedKustomizeBuild(policyDir, remediation string) (string, error) {
	key := remediation + "|" + policyDir
	if v, ok := renderCache.Load(key); ok {
		return v.(string), nil
	}
	out, err := KustomizeBuildWithRemediation(policyDir, remediation)
	if err != nil {
		return "", err
	}
	renderCache.Store(key, out)
	return out, nil
}

// cachedRender is the runPipelineTransform renderer for tests: KustomizeBuild (REMEDIATION=enforce)
// from the cache.
func cachedRender(policyDir string) (string, error) {
	return cachedKustomizeBuild(policyDir, "enforce")
}
