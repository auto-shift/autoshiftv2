package resolver

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WriteTestValues creates a temporary values file that provides the
// ApplicationSet-injected values that policy charts need to render their
// conditional templates (hubClusterSets, managedClusterSets, policy_namespace,
// etc.).
//
// Returns the path to the temp file. Caller must clean it up.
func WriteTestValues(tmpDir string) (string, error) {
	values := map[string]interface{}{
		"policy_namespace":  "policies-autoshift",
		"gitopsNamespace":   "openshift-gitops",
		"selfManagedHubSet": "hub",
		"clusterSetSuffix":  "",
		"autoshift": map[string]interface{}{
			"dryRun": false,
			"evaluationInterval": map[string]interface{}{
				"compliant":    "10m",
				"noncompliant": "30s",
			},
		},
		"hubClusterSets": map[string]interface{}{
			"hub": map[string]interface{}{
				"labels": map[string]interface{}{
					"self-managed": "true",
				},
			},
		},
		"managedClusterSets": map[string]interface{}{
			"managed": map[string]interface{}{
				"labels": map[string]interface{}{},
			},
		},
	}

	data, err := yaml.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal test values: %w", err)
	}

	path := filepath.Join(tmpDir, "lint-test-values.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write test values: %w", err)
	}

	return path, nil
}
