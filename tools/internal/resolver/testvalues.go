package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	"gopkg.in/yaml.v3"
)

// ExampleConfigs holds data extracted from example values files for test rendering.
// The example file is the authoritative source for all labels and non-cluster-install
// config. The cluster-install example provides cluster provisioning config only.
type ExampleConfigs struct {
	// BareLabels: all labels from _example.yaml, bare keys (no autoshift.io/ prefix).
	// First non-empty value wins across all declarations in the file.
	BareLabels map[string]string
	// HubConfig: config section from _example.yaml hubClusterSets.*.config.
	HubConfig map[string]interface{}
	// ClusterInstallConfig: config from _example-cluster-install*.yaml files,
	// cluster name remapped to the lint cluster name.
	ClusterInstallConfig map[string]interface{}
}

// ExtractExampleConfigs reads the authoritative example files and returns
// merged config suitable for test rendering.
//
//   - _example.yaml (clustersets/) → all bare labels + clusterset config
//   - _example-cluster-install*.yaml (clusters/) → cluster provisioning config
//
// Both files must be fully populated (no commented-out keys) to give the test
// pipeline maximum coverage.
func ExtractExampleConfigs(valuesDir string) (*ExampleConfigs, error) {
	cfg := &ExampleConfigs{
		BareLabels: make(map[string]string),
	}

	// --- Hub example: labels + config -----------------------------------------
	clusterSetsDir := filepath.Join(valuesDir, "clustersets")
	csEntries, err := os.ReadDir(clusterSetsDir)
	if err != nil {
		return nil, fmt.Errorf("read clustersets dir: %w", err)
	}

	for _, entry := range csEntries {
		name := entry.Name()
		// Only process the canonical example file. Cluster-install variants
		// (_example-cluster-install*.yaml) are handled by the loop below.
		if name != "_example.yaml" {
			continue
		}

		path := filepath.Join(clusterSetsDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		var parsed map[string]interface{}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}

		// Extract hub clusterset config.
		if hubCS, ok := parsed["hubClusterSets"].(map[string]interface{}); ok {
			for _, csVal := range hubCS {
				if cs, ok := csVal.(map[string]interface{}); ok {
					if hubCfg, ok := cs["config"].(map[string]interface{}); ok {
						cfg.HubConfig = hubCfg
					}
				}
			}
		}

		// Extract bare labels using the declarations extractor so we reuse the
		// same YAML walk logic that the label contract checker uses.
		decls, err := labels.ExtractDeclaredFromFile(path)
		if err != nil {
			return nil, fmt.Errorf("extract labels from %s: %w", name, err)
		}
		for _, d := range decls {
			if _, exists := cfg.BareLabels[d.Key]; !exists {
				cfg.BareLabels[d.Key] = d.Value
			} else if cfg.BareLabels[d.Key] == "" && d.Value != "" {
				cfg.BareLabels[d.Key] = d.Value
			}
		}

		break // only the first (and only) hub example
	}

	// --- Cluster-install examples: provisioning config -----------------------
	// All _example-cluster-install*.yaml files are merged so that both
	// baremetal-specific (hosts, networking) and platform-specific (aws, etc.)
	// config sections are present in the synthetic rendered-config. Without
	// merging all files, platform-specific policies get empty config dicts and
	// silently fall back to defaults, masking broken code paths.
	clustersDir := filepath.Join(valuesDir, "clusters")
	clEntries, err := os.ReadDir(clustersDir)
	if err != nil {
		return nil, fmt.Errorf("read clusters dir: %w", err)
	}

	merged := map[string]interface{}{}
	for _, entry := range clEntries {
		name := entry.Name()
		if !strings.HasPrefix(name, "_example-cluster-install") || !strings.HasSuffix(name, ".yaml") {
			continue
		}

		path := filepath.Join(clustersDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		var parsed map[string]interface{}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}

		if clusters, ok := parsed["clusters"].(map[string]interface{}); ok {
			for _, clusterVal := range clusters {
				if cluster, ok := clusterVal.(map[string]interface{}); ok {
					if clCfg, ok := cluster["config"].(map[string]interface{}); ok {
						deepMergeYAML(merged, clCfg)
					}
				}
				break // only the first cluster entry per file
			}
		}
		// continue to next file — we merge ALL cluster-install examples
	}
	if len(merged) > 0 {
		cfg.ClusterInstallConfig = merged
	}

	return cfg, nil
}

// WriteTestValues creates a temporary values file that provides the
// ApplicationSet-injected values that policy charts need to render their
// conditional templates.
//
// cfg supplies the full hub labels and config extracted from example files.
// When cfg is nil a minimal fallback is used (for unit tests that don't need
// full rendering coverage).
//
// clusterName is used for the synthetic test cluster entry (lint-cluster).
func WriteTestValues(tmpDir, clusterName string, cfg *ExampleConfigs) (string, error) {
	hubLabels := map[string]interface{}{"self-managed": "true"}
	var hubConfig interface{}
	var clusterConfig interface{}

	if cfg != nil {
		hubLabels = stringsToInterface(cfg.BareLabels)
		if len(cfg.HubConfig) > 0 {
			hubConfig = cfg.HubConfig
		}
		if len(cfg.ClusterInstallConfig) > 0 {
			clusterConfig = cfg.ClusterInstallConfig
		}
	}

	hubEntry := map[string]interface{}{
		"labels": hubLabels,
	}
	if hubConfig != nil {
		hubEntry["config"] = hubConfig
	}

	managedEntry := map[string]interface{}{
		"labels": hubLabels, // hub is source of truth for managed clusters too
	}
	if hubConfig != nil {
		managedEntry["config"] = hubConfig
	}

	clusterEntry := map[string]interface{}{}
	if clusterConfig != nil {
		clusterEntry["config"] = clusterConfig
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
			"managed": managedEntry,
		},
		"clusters": map[string]interface{}{
			clusterName: clusterEntry,
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

// ExtractHubConfig is kept for backwards compatibility. New callers should use
// ExtractExampleConfigs which also captures labels and cluster-install config.
func ExtractHubConfig(valuesDir string) (map[string]interface{}, error) {
	cfg, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		return nil, err
	}
	return cfg.HubConfig, nil
}

// deepMergeYAML merges src into dst using the same semantics as deepMerge in
// configmap.go — maps are recursed, all other values overwrite. Used to merge
// multiple cluster-install example files so all platform-specific config
// sections (aws, networking, hosts, clusterInstall) are present together.
func deepMergeYAML(dst, src map[string]interface{}) {
	for k, srcVal := range src {
		dstVal, exists := dst[k]
		if !exists {
			dst[k] = srcVal
			continue
		}
		srcMap, srcIsMap := srcVal.(map[string]interface{})
		dstMap, dstIsMap := dstVal.(map[string]interface{})
		if srcIsMap && dstIsMap {
			deepMergeYAML(dstMap, srcMap)
		} else {
			dst[k] = srcVal
		}
	}
}

// stringsToInterface converts map[string]string to map[string]interface{} for
// YAML marshaling (yaml.v3 preserves string type from map[string]interface{}).
func stringsToInterface(m map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
