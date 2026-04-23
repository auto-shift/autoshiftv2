package labels

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExtractDeclaredFromFile parses a single values YAML file and returns all
// label declarations found within labels: blocks at any nesting depth.
//
// Each key inside a labels: mapping is one Declaration. The search recurses
// through arbitrary nesting (hubClusterSets, managedClusterSets, clusters, etc.)
// but does NOT recurse into the values inside a labels: block — a key named
// "labels" found inside a labels: block is treated as a literal label key, not
// a nested block.
func ExtractDeclaredFromFile(path string) ([]Declaration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	filename := filepath.Base(path)
	fromExample := strings.HasPrefix(filename, "_example")

	return walkForLabels(parsed, "", filename, fromExample), nil
}

// walkForLabels recursively traverses a parsed YAML map, collecting label
// declarations from every labels: block it finds. parentPath is the
// dot-joined path of ancestor keys (empty at the root).
func walkForLabels(obj map[string]interface{}, parentPath, filename string, fromExample bool) []Declaration {
	var result []Declaration

	for k, v := range obj {
		currentPath := k
		if parentPath != "" {
			currentPath = parentPath + "." + k
		}

		if k == "labels" {
			// If the value is a map, collect its entries as label declarations.
			// Non-map values (e.g. labels: "string") are silently ignored.
			if labelsMap, ok := v.(map[string]interface{}); ok {
				for labelKey, labelVal := range labelsMap {
					var valStr string
					if s, ok := labelVal.(string); ok {
						valStr = s
					} else if labelVal != nil {
						valStr = fmt.Sprintf("%v", labelVal)
					}
					result = append(result, Declaration{
						Key:         labelKey,
						Value:       valStr,
						Path:        currentPath,
						File:        filename,
						FromExample: fromExample,
					})
				}
			}
			// Do NOT recurse into the values within a labels: block.
		} else if subMap, ok := v.(map[string]interface{}); ok {
			// Recurse into nested maps looking for more labels: blocks.
			result = append(result, walkForLabels(subMap, currentPath, filename, fromExample)...)
		}
	}

	return result
}

// ExtractDeclaredFromTree walks root recursively, scanning YAML files for
// label declarations. When includeProfiles is false, only _example*.yaml files
// are scanned; all other .yaml files (profile files like hub.yaml, managed.yaml)
// are skipped.
//
// Returns a map from bare label key to *Declared. A key may have multiple
// Declarations if it appears in more than one file.
func ExtractDeclaredFromTree(root string, includeProfiles bool) (map[string]*Declared, error) {
	result := make(map[string]*Declared)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".yaml") {
			return nil
		}

		isExample := strings.HasPrefix(d.Name(), "_example")
		if !includeProfiles && !isExample {
			return nil
		}

		decls, err := ExtractDeclaredFromFile(path)
		if err != nil {
			return err
		}

		for _, decl := range decls {
			if _, exists := result[decl.Key]; !exists {
				result[decl.Key] = &Declared{Key: decl.Key}
			}
			result[decl.Key].Declarations = append(result[decl.Key].Declarations, decl)
		}

		return nil
	})

	return result, err
}
