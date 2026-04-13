package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// WriteTestValues creates a temporary values file that provides the
// ApplicationSet-injected values that policy charts need to render their
// conditional templates (hubClusterSets, managedClusterSets, policy_namespace,
// etc.).
//
// hubConfig is the config section from the hub example file
// (hubClusterSets.hub.config). If non-nil, it's included so the
// cluster-config-maps chart generates real ConfigMaps.
//
// clusterName is the name used for the synthetic test cluster entry
// (typically "lint-cluster" from HubContext.ManagedClusterName).
//
// Returns the path to the temp file. Caller must clean it up.
func WriteTestValues(tmpDir, clusterName string, hubConfig map[string]interface{}) (string, error) {
	hubEntry := map[string]interface{}{
		"labels": map[string]interface{}{
			"self-managed": "true",
		},
	}
	if hubConfig != nil {
		hubEntry["config"] = hubConfig
	}

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
			"hub": hubEntry,
		},
		"managedClusterSets": map[string]interface{}{
			"managed": map[string]interface{}{
				"labels": map[string]interface{}{},
			},
		},
	}

	// Add a synthetic cluster entry so cluster-config-maps generates
	// a managed-cluster-config.<clusterName> ConfigMap.
	if clusterName != "" {
		values["clusters"] = map[string]interface{}{
			clusterName: map[string]interface{}{
				"config": map[string]interface{}{
					"clusterSet": "hub",
				},
			},
		}
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

// ExtractHubConfig reads the first _example*.yaml file in the clustersets
// directory and returns the config section from the first hubClusterSets entry.
func ExtractHubConfig(valuesDir string) (map[string]interface{}, error) {
	clusterSetsDir := filepath.Join(valuesDir, "clustersets")
	entries, err := os.ReadDir(clusterSetsDir)
	if err != nil {
		return nil, fmt.Errorf("read clustersets dir: %w", err)
	}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "_example") || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		// Only look at hub examples (not managed).
		if !strings.Contains(entry.Name(), "hub") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(clusterSetsDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}

		var parsed map[string]interface{}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}

		hubCS, ok := parsed["hubClusterSets"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, csVal := range hubCS {
			cs, ok := csVal.(map[string]interface{})
			if !ok {
				continue
			}
			if cfg, ok := cs["config"].(map[string]interface{}); ok {
				return cfg, nil
			}
		}
	}

	return nil, nil // no config found, that's OK
}
