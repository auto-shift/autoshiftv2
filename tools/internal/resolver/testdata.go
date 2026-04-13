package resolver

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

// LoadTestResources reads all YAML files from a directory and decodes them
// into Kubernetes unstructured objects. Each file can contain multiple
// documents separated by `---`.
//
// This is used to load synthetic Secrets, ConfigMaps, and other resources
// that hub templates look up via fromSecret/fromConfigMap/lookup. Adding
// a new test resource is as simple as dropping a YAML file in the directory.
func LoadTestResources(dir string) ([]unstructured.Unstructured, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no testdata directory is fine
		}
		return nil, fmt.Errorf("read testdata dir %s: %w", dir, err)
	}

	var resources []unstructured.Unstructured

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		objs, err := decodeMultiDocYAML(data)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", name, err)
		}

		resources = append(resources, objs...)
	}

	return resources, nil
}

// decodeMultiDocYAML decodes a multi-document YAML byte slice into a list
// of unstructured Kubernetes objects.
func decodeMultiDocYAML(data []byte) ([]unstructured.Unstructured, error) {
	var objs []unstructured.Unstructured

	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		obj := unstructured.Unstructured{}
		err := decoder.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if obj.Object == nil {
			continue
		}
		objs = append(objs, obj)
	}

	return objs, nil
}
