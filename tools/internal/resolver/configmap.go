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
