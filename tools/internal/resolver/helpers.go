package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"
)

// KeysToConsumed converts the per-policy key sets accumulated during the pipeline
// into the map[string]*labels.Consumed that BuildReport expects.
//
// keysByPolicy maps policy path (e.g. "stable/cert-manager") to the set of bare
// label keys it was observed to consume during rendering.
func KeysToConsumed(keysByPolicy map[string]map[string]bool) map[string]*labels.Consumed {
	result := make(map[string]*labels.Consumed)
	for policy, keys := range keysByPolicy {
		for key := range keys {
			if _, exists := result[key]; !exists {
				result[key] = &labels.Consumed{Key: key}
			}
			result[key].References = append(result[key].References, labels.Reference{
				Key:    key,
				Policy: policy,
			})
		}
	}
	return result
}

// BuildSyntheticLabels constructs a ManagedClusterLabels map suitable for hub
// template resolution. Each declared key gets the autoshift.io/ prefix and its
// first example-file value (or empty string if no example value exists).
//
// Empty-string values are intentional — they exercise the | default "..." paths
// in hub templates, ensuring fallback logic is reachable during CI linting.
func BuildSyntheticLabels(declared map[string]*labels.Declared) map[string]string {
	result := make(map[string]string, len(declared))
	for key, d := range declared {
		val := ""
		for _, decl := range d.Declarations {
			if decl.FromExample && decl.Value != "" {
				val = decl.Value
				break
			}
		}
		result["autoshift.io/"+key] = val
	}
	return result
}

// LoadTestResources reads all .yaml files in testdataDir and returns them as a
// flat slice of unstructured Kubernetes objects. These objects are injected into
// the fake resolver clients so that hub/spoke templates that call lookup,
// fromConfigMap, or fromSecret can return realistic data without a real cluster.
//
// Returns nil (no error) when testdataDir is empty or does not exist, so callers
// do not need to guard against a missing testdata directory.
func LoadTestResources(testdataDir string) ([]unstructured.Unstructured, error) {
	if testdataDir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(testdataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read testdata dir %s: %w", testdataDir, err)
	}

	var resources []unstructured.Unstructured
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		path := filepath.Join(testdataDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read testdata %s: %w", entry.Name(), err)
		}

		for _, doc := range splitYAMLDocuments(string(data)) {
			doc = strings.TrimSpace(doc)
			if doc == "" {
				continue
			}

			var obj map[string]interface{}
			if err := sigsyaml.Unmarshal([]byte(doc), &obj); err != nil {
				continue // skip malformed docs
			}
			if len(obj) == 0 {
				continue
			}

			resources = append(resources, unstructured.Unstructured{Object: obj})
		}
	}

	return resources, nil
}
