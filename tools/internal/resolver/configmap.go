package resolver

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"
)

// ParseConfigMaps extracts v1/ConfigMap objects from multi-document YAML
// (typically the output of `helm template cluster-config-maps`).
func ParseConfigMaps(rawYAML string) ([]unstructured.Unstructured, error) {
	var cms []unstructured.Unstructured

	for _, doc := range splitYAMLDocuments(rawYAML) {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &obj); err != nil {
			continue // skip non-YAML docs
		}

		kind, _ := obj["kind"].(string)
		apiVersion, _ := obj["apiVersion"].(string)
		if kind == "ConfigMap" && apiVersion == "v1" {
			cms = append(cms, unstructured.Unstructured{Object: obj})
		}
	}

	return cms, nil
}

// MergeRenderedConfig produces a synthetic "<clusterName>.rendered-config"
// ConfigMap by merging clusterset-level config with cluster-level config.
// This simulates the merge that policy-rendered-config-maps.yaml performs
// at runtime on the spoke cluster.
//
// The raw ConfigMaps are expected to follow the naming convention:
//   - cluster-set-config.<clusterset>  (clusterset-level config)
//   - managed-cluster-config.<cluster> (cluster-level config, optional)
//
// Merge order: clusterset config is the base, cluster config overrides.
func MergeRenderedConfig(
	clusterName, namespace string,
	rawCMs []unstructured.Unstructured,
) (unstructured.Unstructured, error) {
	// Collect all clusterset configs (merge them all as a base).
	mergedConfig := map[string]interface{}{}

	for _, cm := range rawCMs {
		name, _, _ := unstructured.NestedString(cm.Object, "metadata", "name")

		if strings.HasPrefix(name, "cluster-set-config.") {
			configStr, _, _ := unstructured.NestedString(cm.Object, "data", "config")
			if configStr != "" {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(configStr), &parsed); err == nil {
					deepMerge(mergedConfig, parsed)
				}
			}
		}
	}

	// Overlay cluster-specific config if present.
	for _, cm := range rawCMs {
		name, _, _ := unstructured.NestedString(cm.Object, "metadata", "name")

		if name == "managed-cluster-config."+clusterName {
			configStr, _, _ := unstructured.NestedString(cm.Object, "data", "config")
			if configStr != "" {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(configStr), &parsed); err == nil {
					deepMerge(mergedConfig, parsed)
				}
			}
		}
	}

	// Marshal the merged config as YAML for the rendered ConfigMap's data.config.
	configYAML, err := sigsyaml.Marshal(mergedConfig)
	if err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("marshal merged config: %w", err)
	}

	renderedCM := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      clusterName + ".rendered-config",
				"namespace": namespace,
				"labels": map[string]interface{}{
					"autoshift.io/rendered-config-map": "",
				},
			},
			"data": map[string]interface{}{
				"config": string(configYAML),
			},
		},
	}

	return renderedCM, nil
}

// GenerateSyntheticConfigMaps builds the ConfigMaps that the cluster-config-maps
// Helm chart would normally generate, directly from the example file data.
// This pre-seeds the resolver so downstream policy charts can look up config
// via hub templates before (or instead of) the helm-generated versions.
//
// Generates:
//   - cluster-set-config.hub          — hub clusterset config
//   - cluster-set-config.managed      — managed clusterset config (hub = source of truth)
//   - managed-cluster-config.<name>   — per-cluster config (cluster-install config)
//   - <name>.rendered-config          — merged view used by downstream policies
func GenerateSyntheticConfigMaps(cfg *ExampleConfigs, clusterName, namespace string) ([]unstructured.Unstructured, error) {
	if cfg == nil {
		return nil, nil
	}

	makeConfigMap := func(name, ns string, configData map[string]interface{}) (unstructured.Unstructured, error) {
		jsonBytes, err := json.Marshal(configData)
		if err != nil {
			return unstructured.Unstructured{}, fmt.Errorf("marshal config for %s: %w", name, err)
		}
		return unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": ns,
					"labels":    map[string]interface{}{"autoshift.io/cluster-set-configs": ""},
				},
				"data": map[string]interface{}{
					"config": string(jsonBytes),
				},
			},
		}, nil
	}

	var cms []unstructured.Unstructured

	// cluster-set-config.hub
	if len(cfg.HubConfig) > 0 {
		cm, err := makeConfigMap("cluster-set-config.hub", namespace, cfg.HubConfig)
		if err != nil {
			return nil, err
		}
		cms = append(cms, cm)
	}

	// cluster-set-config.managed (hub is source of truth for managed too)
	if len(cfg.HubConfig) > 0 {
		cm, err := makeConfigMap("cluster-set-config.managed", namespace, cfg.HubConfig)
		if err != nil {
			return nil, err
		}
		// Use the cluster-configs label so the rendered-config policy finds it.
		cm.Object["metadata"].(map[string]interface{})["labels"] = map[string]interface{}{
			"autoshift.io/cluster-set-configs": "",
		}
		cms = append(cms, cm)
	}

	// appendClusterCMs emits the managed-cluster-config.<name> and
	// <name>.rendered-config ConfigMaps for one synthetic cluster. The install
	// policies range over every rendered-config ConfigMap, so emitting one per
	// platform makes each platform's install policy body resolve in a single run.
	appendClusterCMs := func(name string, installCfg map[string]interface{}) error {
		clusterCfg := map[string]interface{}{"clusterSet": "hub"}
		for k, v := range installCfg {
			clusterCfg[k] = v
		}

		clusterCM := unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "managed-cluster-config." + name,
					"namespace": namespace,
					"labels":    map[string]interface{}{"autoshift.io/cluster-configs": ""},
				},
				"data": func() map[string]interface{} {
					b, _ := json.Marshal(clusterCfg)
					return map[string]interface{}{"config": string(b)}
				}(),
			},
		}
		cms = append(cms, clusterCM)

		// <name>.rendered-config — merge hub config base + cluster overrides
		renderedCfg := map[string]interface{}{}
		if len(cfg.HubConfig) > 0 {
			deepMerge(renderedCfg, cfg.HubConfig)
		}
		deepMerge(renderedCfg, clusterCfg)

		renderedJSON, err := json.Marshal(renderedCfg)
		if err != nil {
			return fmt.Errorf("marshal rendered config for %s: %w", name, err)
		}
		renderedCM := unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      name + ".rendered-config",
					"namespace": namespace,
					"labels":    map[string]interface{}{"autoshift.io/rendered-config-map": ""},
				},
				"data": map[string]interface{}{
					"config": string(renderedJSON),
				},
			},
		}
		cms = append(cms, renderedCM)
		return nil
	}

	// Primary cluster carries the merged cluster-install config. Example files
	// merge in sorted filename order, so the last one wins scalar keys like
	// clusterInstall.platform (currently vmware). Per-platform policy bodies are
	// exercised by the dedicated per-platform clusters below, not by this merged
	// blob.
	if err := appendClusterCMs(clusterName, cfg.ClusterInstallConfig); err != nil {
		return nil, err
	}

	// One dedicated cluster per additional platform (aws, vmware, ...) so every
	// platform's install policy body is exercised, not just the merge winner.
	for platform, installCfg := range cfg.ClusterInstallExtra {
		if err := appendClusterCMs(clusterName+"-"+platform, installCfg); err != nil {
			return nil, err
		}
	}

	return cms, nil
}

// deepMerge merges src into dst. For map values, it recurses. For all other
// types, src overwrites dst.
func deepMerge(dst, src map[string]interface{}) {
	for k, srcVal := range src {
		dstVal, exists := dst[k]
		if !exists {
			dst[k] = srcVal
			continue
		}
		srcMap, srcIsMap := srcVal.(map[string]interface{})
		dstMap, dstIsMap := dstVal.(map[string]interface{})
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)
		} else {
			dst[k] = srcVal
		}
	}
}
